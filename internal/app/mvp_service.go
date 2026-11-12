package app

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"

	"deskpilot/internal/command"
	"deskpilot/internal/developer"
	"deskpilot/internal/domain"
)

func (s *Service) callMVP(method, payload string) (domain.Reply, bool) {
	if reply, handled := s.callConfig021(method, payload); handled {
		return reply, true
	}
	if reply, handled := s.callAgent02(method, payload); handled {
		return reply, true
	}
	if reply, handled := s.callMVPAnalysis(method, payload); handled {
		return reply, true
	}
	switch method {
	case "GetMVPState", "PickWorkspace", "RemoveWorkspace", "AnalyzeWorkspace", "InspectDeveloperProject", "ReadWorkspaceFile", "PrepareFileChange", "PrepareCommand", "ApproveProductAction", "RejectProductAction", "CreateAgentTask", "GetAgentTask", "PauseAgentTask", "ResumeAgentTask", "CancelAgentTask", "SetSkillEnabled", "CompleteOnboarding", "DiagnoseEnvironment":
	default:
		return domain.Reply{}, false
	}
	done := func(v any, e error) (domain.Reply, bool) { return result(v, e), true }
	if s.lifetime.Err() != nil {
		return done(nil, codedError("SERVICE_CLOSED"))
	}
	switch method {
	case "GetMVPState", "PickWorkspace", "DiagnoseEnvironment":
		if e := decode(payload, &struct{}{}); e != nil {
			return done(nil, e)
		}
		switch method {
		case "GetMVPState":
			v, e := s.mvpState()
			v.CommandCatalog = command.Catalog()
			return done(v, e)
		case "PickWorkspace":
			v, e := s.pickWorkspace()
			return done(v, e)
		default:
			v, e := s.diagnoseEnvironment()
			return done(v, e)
		}
	case "InspectDeveloperProject":
		var q struct {
			WorkspaceID string   `json:"workspaceId"`
			Exclusions  []string `json:"exclusions,omitempty"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		value, err := s.InspectDeveloperProject(q.WorkspaceID, q.Exclusions)
		return done(value, err)
	case "AnalyzeWorkspace":
		var q struct {
			WorkspaceID string   `json:"workspaceId"`
			Query       string   `json:"query"`
			Exclusions  []string `json:"exclusions,omitempty"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		w, e := s.workspace(q.WorkspaceID)
		if e != nil {
			return done(nil, e)
		}
		if len(q.Query) > 4096 {
			return done(nil, codedError("INVALID_REQUEST"))
		}
		v, e := developer.AnalyzeWithOptions(w.Root, q.Query, developer.Options{Exclusions: q.Exclusions})
		return done(v, e)
	case "ReadWorkspaceFile":
		var q struct {
			WorkspaceID string   `json:"workspaceId"`
			Path        string   `json:"path"`
			Exclusions  []string `json:"exclusions,omitempty"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		w, e := s.workspace(q.WorkspaceID)
		if e != nil {
			return done(nil, e)
		}
		v, e := developer.ReadWithOptions(w.Root, q.Path, developer.Options{Exclusions: q.Exclusions})
		return done(v, e)
	case "PrepareFileChange":
		var q struct {
			WorkspaceID string   `json:"workspaceId"`
			Path        string   `json:"path"`
			Content     string   `json:"content"`
			TaskID      string   `json:"taskId,omitempty"`
			Exclusions  []string `json:"exclusions,omitempty"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		v, e := s.prepareProduct(q.WorkspaceID, q.TaskID, "file", q.Path, q.Content, "", nil, false, q.Exclusions)
		return done(v, e)
	case "PrepareCommand":
		var q struct {
			WorkspaceID string   `json:"workspaceId"`
			CommandID   string   `json:"commandId"`
			Args        []string `json:"args"`
			Network     bool     `json:"network"`
			TaskID      string   `json:"taskId,omitempty"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		v, e := s.prepareProduct(q.WorkspaceID, q.TaskID, "command", "", "", q.CommandID, q.Args, q.Network)
		return done(v, e)
	case "ApproveProductAction":
		var q struct {
			ID            string `json:"id"`
			Hash          string `json:"hash"`
			AcceptNetwork bool   `json:"acceptNetwork,omitempty"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		v, e := s.approveProduct(q.ID, q.Hash, q.AcceptNetwork)
		return done(v, e)
	case "RejectProductAction":
		var q struct {
			ID string `json:"id"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		s.mvp.mu.Lock()
		v, e := s.finishPendingAction(q.ID, "REJECTED", "ACTION_REJECTED")
		s.mvp.mu.Unlock()
		return done(v, e)
	case "CreateAgentTask":
		var q struct {
			WorkspaceID string `json:"workspaceId"`
			Goal        string `json:"goal"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		v, e := s.createAgentTask(q.WorkspaceID, q.Goal)
		return done(v, e)
	case "GetAgentTask", "PauseAgentTask", "ResumeAgentTask", "CancelAgentTask":
		var q struct {
			ID string `json:"id"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		if method == "GetAgentTask" {
			s.mvp.mu.Lock()
			v, e := loadAgentTask(s.db, q.ID)
			if s.mvp.faults["task:"+q.ID] {
				v.Status = "PAUSED"
				v.Summary = "RECOVERY_REQUIRED"
			}
			s.mvp.mu.Unlock()
			return done(v, e)
		}
		mode := "pause"
		if method == "ResumeAgentTask" {
			mode = "resume"
		}
		if method == "CancelAgentTask" {
			mode = "cancel"
		}
		v, e := s.changeAgent(q.ID, mode)
		return done(v, e)
	case "RemoveWorkspace":
		var q struct {
			ID string `json:"id"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		s.mvp.mu.Lock()
		defer s.mvp.mu.Unlock()
		_, e := s.workspace(q.ID)
		if e != nil {
			return done(nil, e)
		}
		var n int
		e = s.db.QueryRow("SELECT (SELECT COUNT(*) FROM agent_tasks WHERE workspace_id=?)+(SELECT COUNT(*) FROM product_actions WHERE workspace_id=?)", q.ID, q.ID).Scan(&n)
		if e != nil {
			return done(nil, e)
		}
		if n > 0 {
			return done(nil, codedError("WORKSPACE_IN_USE"))
		}
		_, e = s.db.Exec("DELETE FROM workspaces WHERE id=?", q.ID)
		if e == nil {
			s.changed("", "")
		}
		return done(e == nil, e)
	case "SetSkillEnabled":
		var q struct {
			ID      string `json:"id"`
			Enabled bool   `json:"enabled"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		s.mvp.mu.Lock()
		defer s.mvp.mu.Unlock()
		for _, skill := range builtinSkills() {
			if skill.ID == q.ID {
				_, e := s.db.Exec("UPDATE skill_settings SET enabled=?,updated_at=? WHERE id=?", q.Enabled, now(), q.ID)
				skill.Enabled = q.Enabled
				if e == nil {
					s.changed("", "")
				}
				return done(skill, e)
			}
		}
		return done(nil, codedError("NOT_FOUND"))
	case "CompleteOnboarding":
		var q struct {
			Completed bool `json:"completed"`
		}
		if e := decode(payload, &q); e != nil {
			return done(nil, e)
		}
		if !q.Completed {
			return done(nil, codedError("INVALID_REQUEST"))
		}
		_, e := s.db.Exec("UPDATE onboarding_state SET completed=1,updated_at=? WHERE id='onboarding'", now())
		if e == nil {
			s.changed("", "")
		}
		return done(map[string]bool{"completed": e == nil}, e)
	}
	return done(nil, codedError("INVALID_REQUEST"))
}

func (s *Service) pickWorkspace() (Workspace, error) {
	s.mvp.mu.Lock()
	picker := s.mvp.picker
	epoch := s.mvpGeneration()
	s.mvp.mu.Unlock()
	if picker == nil {
		return Workspace{}, codedError("NATIVE_PICKER_UNAVAILABLE")
	}
	chosen, e := picker(s.lifetime)
	if e != nil {
		return Workspace{}, e
	}
	if strings.TrimSpace(chosen) == "" {
		return Workspace{}, codedError("WORKSPACE_SELECTION_CANCELLED")
	}
	root, e := developer.ValidateRoot(chosen)
	if e != nil {
		return Workspace{}, e
	}
	s.mvp.mu.Lock()
	defer s.mvp.mu.Unlock()
	if epoch != s.mvpGeneration() || s.lifetime.Err() != nil {
		return Workspace{}, codedError("ACTION_CANCELLED")
	}
	w := Workspace{ID: newID(), Name: filepath.Base(root), Root: root, CreatedAt: now(), LastUsedAt: now(), TrustStatus: "USER_SELECTED"}
	e = s.transaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO workspaces(id,name,root,created_at,last_used_at,trust_status) VALUES(?,?,?,?,?,?) ON CONFLICT(root) DO UPDATE SET last_used_at=excluded.last_used_at", w.ID, w.Name, w.Root, w.CreatedAt, w.LastUsedAt, w.TrustStatus); err != nil {
			return err
		}
		return tx.QueryRow("SELECT id,name,root,created_at,last_used_at,trust_status FROM workspaces WHERE root=?", root).Scan(&w.ID, &w.Name, &w.Root, &w.CreatedAt, &w.LastUsedAt, &w.TrustStatus)
	})
	if e == nil {
		s.changed("", "")
	}
	return w, e
}
func (s *Service) diagnoseEnvironment() ([]command.Diagnostic, error) {
	ctx, cancel := context.WithCancel(s.lifetime)
	epoch := s.mvpGeneration()
	key := "diagnose:" + newID()
	s.registerMVPControl(key, cancel, epoch)
	defer s.removeMVPControl(key)
	v := command.Diagnose(ctx)
	if ctx.Err() != nil {
		return v, codedError("ACTION_CANCELLED")
	}
	return v, nil
}
