package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"asimi/internal/command"
	"asimi/internal/developer"
)

func (s *Service) createAgentTask(workspaceID, goal string) (AgentTask, error) {
	goal = strings.TrimSpace(goal)
	if goal == "" || len(goal) > 8192 {
		return AgentTask{}, codedError("INVALID_REQUEST")
	}
	s.mvp.mu.Lock()
	defer s.mvp.mu.Unlock()
	if s.lifetime.Err() != nil {
		return AgentTask{}, codedError("SERVICE_CLOSED")
	}
	if _, e := s.workspace(workspaceID); e != nil {
		return AgentTask{}, e
	}
	t := AgentTask{ID: newID(), WorkspaceID: workspaceID, Goal: goal, Status: "RUNNING", CreatedAt: now(), UpdatedAt: now(), Steps: []AgentStep{
		{ID: newID(), Kind: "project", Title: "PROJECT_OVERVIEW", Status: "PENDING"},
		{ID: newID(), Kind: "search", Title: "SEARCH_GOAL_CONTEXT", Status: "PENDING"},
		{ID: newID(), Kind: "environment", Title: "ENVIRONMENT_DIAGNOSIS", Status: "PENDING"},
	}, Summary: "READ_ONLY_PLAN"}
	if e := s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, t) }); e != nil {
		return t, e
	}
	s.launchAgentLocked(t.ID)
	s.changed("", t.ID)
	return t, nil
}
func (s *Service) launchAgentLocked(id string) {
	ctx, cancel := context.WithCancel(s.lifetime)
	s.registerMVPControl("task:"+id, cancel, s.mvpGeneration(), id)
	s.wg.Add(1)
	go s.runAgent(ctx, id)
}
func (s *Service) runAgent(ctx context.Context, id string) {
	defer s.wg.Done()
	defer s.removeMVPControl("task:" + id)
	for {
		s.mvp.mu.Lock()
		t, e := loadAgentTask(s.db, id)
		if e != nil || t.Status != "RUNNING" || ctx.Err() != nil {
			s.mvp.mu.Unlock()
			return
		}
		w, e := s.workspace(t.WorkspaceID)
		if e != nil {
			s.mvp.mu.Unlock()
			s.failAgent(id, e)
			return
		}
		step := -1
		for i := range t.Steps {
			if t.Steps[i].Status == "PENDING" && (t.Steps[i].Kind == "project" || t.Steps[i].Kind == "search" || t.Steps[i].Kind == "environment") {
				step = i
				break
			}
		}
		if step < 0 {
			t.Status = "COMPLETED"
			t.Summary = "READ_ONLY_ANALYSIS_COMPLETE"
			t.CurrentStep = len(t.Steps)
			for _, v := range t.Steps {
				if v.Status == "RECOVERY_REQUIRED" {
					t.Status = "PAUSED"
					t.Summary = "RECOVERY_REQUIRED"
				}
			}
			t.UpdatedAt = now()
			e = s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, t) })
			if e != nil {
				s.agentStorageFaultLocked(id, e)
			}
			s.mvp.mu.Unlock()
			s.changed("", id)
			return
		}
		t.CurrentStep = step
		t.Steps[step].Status = "RUNNING"
		t.UpdatedAt = now()
		e = s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, t) })
		if e != nil {
			s.agentStorageFaultLocked(id, e)
			s.mvp.mu.Unlock()
			return
		}
		s.mvp.mu.Unlock()
		s.changed("", id)
		var report any
		if ctx.Err() != nil {
			return
		}
		switch t.Steps[step].Kind {
		case "project":
			report, e = developer.Analyze(w.Root, "")
		case "search":
			report, e = developer.Analyze(w.Root, goalSearch(t.Goal))
		case "environment":
			report = command.InspectAvailability(w.Root)
		}
		s.mvp.mu.Lock()
		current, readErr := loadAgentTask(s.db, id)
		if readErr != nil {
			s.agentStorageFaultLocked(id, readErr)
			s.mvp.mu.Unlock()
			return
		}
		if current.Status != "RUNNING" || ctx.Err() != nil {
			s.mvp.mu.Unlock()
			return
		}
		current.UpdatedAt = now()
		current.Steps[step].Result = json.RawMessage(encode(report))
		current.Steps[step].Status = "COMPLETED"
		if e != nil {
			current.Status = "FAILED"
			current.Summary = errorCode(e)
			current.Steps[step].Status = "FAILED"
			for i := step + 1; i < len(current.Steps); i++ {
				if current.Steps[i].Status == "PENDING" {
					current.Steps[i].Status = "SKIPPED"
				}
			}
		}
		saveErr := s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, current) })
		if saveErr != nil {
			s.agentStorageFaultLocked(id, saveErr)
		}
		s.mvp.mu.Unlock()
		s.changed("", id)
		if e != nil || saveErr != nil {
			return
		}
	}
}

// Select one useful bounded search term from a natural-language goal. Project
// structure and manifests are always included even when no term matches.
func goalSearch(goal string) string {
	stop := map[string]bool{"the": true, "and": true, "this": true, "that": true, "please": true, "project": true, "code": true, "analyze": true, "explain": true, "review": true, "help": true, "find": true, "understand": true}
	best := ""
	for _, word := range strings.FieldsFunc(goal, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r > 127)
	}) {
		if len(word) > len(best) && len(word) <= 80 && !stop[strings.ToLower(word)] {
			best = word
		}
	}
	return best
}
func (s *Service) agentStorageFaultLocked(id string, e error) {
	s.mvp.faults["task:"+id] = true
	s.mvp.faultErr = errors.Join(s.mvp.faultErr, e)
	s.cancelMVPControl("task:" + id)
}
func (s *Service) failAgent(id string, failure error) {
	s.mvp.mu.Lock()
	defer s.mvp.mu.Unlock()
	t, e := loadAgentTask(s.db, id)
	if e != nil {
		s.agentStorageFaultLocked(id, e)
		return
	}
	if t.Status != "RUNNING" {
		return
	}
	t.Status = "FAILED"
	t.Summary = errorCode(failure)
	t.UpdatedAt = now()
	if e = s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, t) }); e != nil {
		s.agentStorageFaultLocked(id, e)
	}
	s.changed("", id)
}

func (s *Service) changeAgent(id, mode string) (AgentTask, error) {
	if mode != "resume" {
		defer s.beginMVPTaskStop(id)()
	}
	s.mvp.mu.Lock()
	defer s.mvp.mu.Unlock()
	t, e := loadAgentTask(s.db, id)
	if e != nil {
		return t, e
	}
	if mode == "resume" {
		if t.Agent != nil {
			return s.resumeAgent02Locked(t, false)
		}
		if t.Status != "PAUSED" {
			return t, codedError("TASK_STATE")
		}
		if s.activeMVPControl("task:" + id) {
			return t, codedError("REQUEST_ACTIVE")
		}
		if s.activeMVPControl("effect:" + id) {
			return t, codedError("REQUEST_ACTIVE")
		}
		// Reread facts on resume. Consumed effects keep their evidence and are never queued again.
		for i := range t.Steps {
			switch t.Steps[i].Kind {
			case "project", "search", "environment":
				t.Steps[i].Status = "PENDING"
				t.Steps[i].Result = nil
			}
		}
		t.Status = "RUNNING"
		t.Summary = "REFRESHING_READ_ONLY_FACTS"
		t.CurrentStep = 0
		t.UpdatedAt = now()
		if e = s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, t) }); e != nil {
			return t, e
		}
		s.launchAgentLocked(id)
		s.changed("", id)
		return t, nil
	}
	if t.Status == "CANCELLED" || t.Status == "COMPLETED" || t.Status == "FAILED" {
		return t, codedError("TASK_STATE")
	}
	for actionID, raw := range s.mvp.immutable {
		var proposal ProductAction
		if json.Unmarshal([]byte(raw), &proposal) == nil && proposal.TaskID == id {
			delete(s.mvp.immutable, actionID)
		}
	}
	s.cancelMVPControl("task:" + id)
	rows, e := s.db.Query("SELECT id FROM product_actions WHERE task_id=? AND status IN ('PENDING','EXECUTING')", id)
	if e != nil {
		return t, e
	}
	var actions []string
	for rows.Next() {
		var aid string
		if e = rows.Scan(&aid); e != nil {
			break
		}
		actions = append(actions, aid)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return t, e
	}
	for _, aid := range actions {
		s.cancelMVPControl("action:" + aid)
		delete(s.mvp.immutable, aid)
		a, err := loadProductAction(s.db, aid)
		if err != nil {
			return t, err
		}
		if a.Status == "PENDING" {
			if _, e = s.finishPendingAction(aid, "CANCELLED", "TASK_"+strings.ToUpper(mode)); e != nil {
				return t, e
			}
		}
	}
	t, e = loadAgentTask(s.db, id)
	if e != nil {
		return t, e
	}
	t.Status = "PAUSED"
	t.Summary = "TASK_PAUSED"
	if mode == "cancel" {
		t.Status = "CANCELLED"
		t.Summary = "TASK_CANCELLED"
	}
	t.UpdatedAt = now()
	for i := range t.Steps {
		if t.Steps[i].Status == "RUNNING" || t.Steps[i].Status == "PENDING" {
			t.Steps[i].Status = "PENDING"
			if mode == "cancel" {
				t.Steps[i].Status = "SKIPPED"
			}
		}
	}
	e = s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, t) })
	if e == nil {
		s.changed("", id)
	}
	return t, e
}

func (s *Service) signalMVPStop() {
	s.mvp.signalMu.Lock()
	defer s.mvp.signalMu.Unlock()
	s.mvp.stopped++
	for _, cancel := range s.mvp.controls {
		cancel()
	}
}

func (s *Service) beginMVPStop() func() {
	s.mvp.signalMu.Lock()
	s.mvp.stopping++
	s.mvp.stopped++
	for _, cancel := range s.mvp.controls {
		cancel()
	}
	s.mvp.signalMu.Unlock()
	return func() { s.mvp.signalMu.Lock(); s.mvp.stopping--; s.mvp.signalMu.Unlock() }
}

func (s *Service) beginMVPTaskStop(id string) func() {
	s.mvp.signalMu.Lock()
	if s.mvp.stoppingTasks == nil {
		s.mvp.stoppingTasks = map[string]int{}
	}
	s.mvp.stoppingTasks[id]++
	for _, key := range []string{"task:" + id, "effect:" + id} {
		if cancel := s.mvp.controls[key]; cancel != nil {
			cancel()
		}
	}
	s.mvp.signalMu.Unlock()
	return func() {
		s.mvp.signalMu.Lock()
		s.mvp.stoppingTasks[id]--
		if s.mvp.stoppingTasks[id] == 0 {
			delete(s.mvp.stoppingTasks, id)
		}
		s.mvp.signalMu.Unlock()
	}
}

func (s *Service) stopMVP() error {
	defer s.beginMVPStop()()
	s.mvp.mu.Lock()
	defer s.mvp.mu.Unlock()
	clear(s.mvp.immutable)
	// Pause active tasks before invalidating approvals so each cancelled approval
	// preserves its task's resumable state rather than converting it to failure.
	if e := s.pauseMVPActiveTasks(); e != nil {
		return e
	}
	rows, e := s.db.Query("SELECT id FROM product_actions WHERE status='PENDING'")
	if e != nil {
		return e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			break
		}
		ids = append(ids, id)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return e
	}
	for _, id := range ids {
		if _, e = s.finishPendingAction(id, "CANCELLED", "STOP_ALL"); e != nil {
			return e
		}
	}
	return nil
}

func (s *Service) pauseMVPActiveTasks() error {
	rows, e := s.db.Query("SELECT json FROM agent_tasks WHERE status IN ('RUNNING','WAITING_APPROVAL','NEEDS_REVIEW')")
	if e != nil {
		return e
	}
	var tasks []AgentTask
	for rows.Next() {
		var raw string
		var t AgentTask
		if e = rows.Scan(&raw); e != nil {
			break
		}
		if e = json.Unmarshal([]byte(raw), &t); e != nil {
			break
		}
		tasks = append(tasks, t)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return e
	}
	return s.transaction(func(tx *sql.Tx) error {
		for _, t := range tasks {
			t.Status = "PAUSED"
			t.Summary = "STOP_ALL"
			t.UpdatedAt = now()
			for i := range t.Steps {
				if t.Steps[i].Status == "RUNNING" {
					t.Steps[i].Status = "PENDING"
				}
			}
			if e = saveAgentTask(tx, t); e != nil {
				return e
			}
		}
		return nil
	})
}
