package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"

	"asimi/internal/developer"
	"strings"
	"time"

	"asimi/internal/command"
)

func queueCognitionTask(q querier, task AgentTask) error {
	if task.Status != "COMPLETED" && task.Status != "FAILED" {
		return nil
	}
	settings, err := loadCognitionSettings(q)
	if err != nil || !settings.Memory || !settings.AutomaticLearning || !settings.ProjectLearning {
		return err
	}
	_, err = q.Exec(`INSERT OR IGNORE INTO learning_jobs(task_id,status,updated_at) VALUES(?,
 CASE WHEN EXISTS(SELECT 1 FROM cognition_learning_control WHERE paused=1) THEN 'paused' ELSE 'pending' END,?)`, task.ID, now())
	return err
}

func (s *Service) cognitionLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.lifetime.Done():
			return
		case <-ticker.C:
			if s.lifetime.Err() == nil {
				s.processCognitionJob()
				s.syncCognitionPackage()
			}
		}
	}
}

func (s *Service) processCognitionJob() {
	s.cognitionMu.Lock()
	defer s.cognitionMu.Unlock()
	s.mvp.signalMu.Lock()
	defer s.mvp.signalMu.Unlock()
	if s.lifetime.Err() != nil || s.mvp.stopping > 0 {
		return
	}
	start := time.Now()
	var id string
	err := s.transaction(func(tx *sql.Tx) error {
		settings, err := loadCognitionSettings(tx)
		if err != nil || !settings.Memory || !settings.AutomaticLearning || !settings.ProjectLearning {
			return err
		}
		if _, err := tx.Exec(`UPDATE cognition_learning_history SET status='expired',completed_at=?
		 WHERE status='queued' AND julianday(created_at)<julianday('now','-7 days')`, now()); err != nil {
			return err
		}
		err = tx.QueryRow(`SELECT j.task_id FROM learning_jobs j JOIN cognition_learning_history h ON h.task_id=j.task_id
		 WHERE j.status='pending' AND h.status='queued' AND (h.next_attempt_at='' OR julianday(h.next_attempt_at)<=julianday('now'))
		 ORDER BY j.updated_at,j.task_id LIMIT 1`).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		task, err := loadAgentTask(tx, id)
		if err != nil {
			return err
		}
		if task.Status == "COMPLETED" || task.Status == "FAILED" {
			if err := reflectCognitionTask(tx, task, settings); err != nil {
				return err
			}
		}
		if s.lifetime.Err() != nil {
			return s.lifetime.Err()
		}
		if _, err = tx.Exec("UPDATE learning_jobs SET status='done',updated_at=? WHERE task_id=?", now(), id); err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE cognition_learning_history SET status='done',attempts=attempts+1,last_error='',
		 duration_ms=?,completed_at=? WHERE task_id=?`, time.Since(start).Milliseconds(), now(), id)
		return err
	})
	if err != nil && s.lifetime.Err() == nil {
		_ = recordCognitionFailure(s.db, id, time.Since(start), err)
	}
}

func reflectCognitionTask(tx *sql.Tx, task AgentTask, settings cognitionSettings) error {
	var root string
	if err := tx.QueryRow("SELECT root FROM workspaces WHERE id=?", task.WorkspaceID).Scan(&root); err != nil {
		return err
	}
	goal := boundedAgentText(task.Goal, root, 650)
	if !cognitionText(goal, 750) {
		goal = ""
	}
	text := "Task ended with status " + task.Status + "."
	if goal != "" {
		text += " Goal: " + goal
	}
	verified := false
	commands := []string{}
	workflow := []string{}
	failures := []string{}
	for _, step := range task.Steps[max(0, len(task.Steps)-24):] {
		if step.ActionID == "" {
			if step.Status == "FAILED" && cognitionText(step.Error, 100) {
				failures = append(failures, step.Kind+" ("+step.Error+")")
			}
			continue
		}
		action, err := loadProductAction(tx, step.ActionID)
		if errorCode(err) == "NOT_FOUND" {
			continue
		}
		if err != nil {
			return err
		}
		if action.TaskID != task.ID || action.Proposal.WorkspaceID != task.WorkspaceID {
			continue
		}
		if action.Status == "FAILED" || action.Status == "RECOVERY_REQUIRED" {
			detail := action.Kind + " (" + action.Status + ")"
			if code := action.Error; cognitionText(code, 100) {
				detail += ": " + code
			}
			failures = append(failures, detail)
			continue
		}
		if action.Status != "SUCCEEDED" {
			if action.Status == "EXECUTING" {
				failures = append(failures, action.Kind+" (unresolved execution)")
			}
			continue
		}
		if action.Kind == "file" {
			var result struct {
				Verified     bool   `json:"verified"`
				RelativePath string `json:"relativePath"`
			}
			if json.Unmarshal(action.Result, &result) == nil && result.Verified {
				verified = true
				text += " An approved file change was independently verified."
				if cognitionEvidencePath(result.RelativePath) {
					workflow = append(workflow, "Review and propose the relevant change to "+result.RelativePath+".")
				} else {
					workflow = append(workflow, "Review and propose the relevant file change.")
				}
			}
		}
		if action.Kind == "command" && action.Proposal.Command != nil {
			var result struct {
				ExitCode  *int `json:"exitCode"`
				Cancelled bool `json:"cancelled"`
				TimedOut  bool `json:"timedOut"`
			}
			if json.Unmarshal(action.Result, &result) == nil && result.ExitCode != nil && *result.ExitCode == 0 && !result.Cancelled && !result.TimedOut {
				verified = true
				id := action.Proposal.Command.ID
				if cognitionCommandPattern(id) && !slices.Contains(commands, id) {
					commands = append(commands, id)
					workflow = append(workflow, "After approval, run the registered "+id+" check and verify the actual result.")
				}
			}
		}
	}
	if len(commands) > 0 {
		text += " Verified checks (exit code 0): " + strings.Join(commands, ", ") + "."
	}
	if len(failures) > 0 {
		text += " Observed failures: " + strings.Join(failures, ", ") + "."
	}
	if err := saveCognitionMemory(tx, task.WorkspaceID, "episode", task.ID, text, "task:"+task.ID, task.Status == "COMPLETED" && verified && len(failures) == 0); err != nil {
		return err
	}
	if task.Status != "COMPLETED" || !verified || len(failures) > 0 {
		return nil
	}
	if len(workflow) > 0 {
		text = boundedAgentText(text+" Observed workflow: "+strings.Join(workflow, " "), root, 2150)
		if err := saveCognitionMemory(tx, task.WorkspaceID, "episode", task.ID, text, "task:"+task.ID, true); err != nil {
			return err
		}
	}
	if settings.AutomaticSkillCreation {
		personal, err := queuePersonalCognitionCandidate(tx, task)
		if err != nil {
			return err
		}
		if !personal {
			safeGoal := developer.RedactAgentWorkspaceText(root, goal)
			if err := queueCognitionCandidate(tx, task, safeGoal, commands, workflow); err != nil && errorCode(err) != "COGNITION_UNSAFE_SKILL" {
				return err
			}
		}
	}
	for _, id := range commands {
		instructions := "Review the current project and identify the appropriate test scope. Use the existing " + id + " template when applicable and request its required approval. Inspect the actual exit code and report whether the check passed."
		if _, err := tx.Exec("INSERT OR IGNORE INTO cognition_patterns(pattern,task_id,workspace_id) VALUES(?,?,?)", id, task.ID, task.WorkspaceID); err != nil {
			return err
		}
		if err := saveCognitionMemory(tx, task.WorkspaceID, "procedure", id, instructions, "task:"+task.ID, true); err != nil {
			return err
		}

	}
	return nil
}

var cognitionPreference = regexp.MustCompile(`(?i)^(?:(?:please )?remember(?: that)? (?:i prefer |my preference is )?|(?:correction|preference)[:：][ \t]*|(?:请)?记住(?:我偏好|我喜欢)?[：:，, \t]*|(?:更正|偏好)[:：][ \t]*)(.+)$`)

func captureCognitionPreference(q querier, messageID, content string) error {
	match := cognitionPreference.FindStringSubmatch(strings.TrimSpace(content))
	if len(match) != 2 || !cognitionText(match[1], 480) {
		return nil
	}
	settings, err := loadCognitionSettings(q)
	if err != nil || !settings.Memory || !settings.AutomaticLearning {
		return err
	}
	key, text := "preference", strings.TrimSpace(match[1])
	if name, value, ok := strings.Cut(text, "="); ok && cognitionText(name, 100) && cognitionText(value, 480) {
		key, text = strings.ToLower(strings.TrimSpace(name)), strings.TrimSpace(value)
	}
	return saveCognitionMemory(q, "", "user", key, text, "correction:"+messageID, true)
}

func recordCognitionUsage(q querier, taskID string, skills []cognitionSkill) error {
	for _, skill := range skills {
		if skill.Scope == "official" {
			continue
		}
		if _, err := q.Exec(`INSERT OR IGNORE INTO skill_usage(package_id,version,task_id,created_at)
  SELECT package_id,version,?,? FROM skill_versions WHERE package_id=? AND version=?`, taskID, now(), skill.ID, skill.Version); err != nil {
			return err
		}
	}
	return nil
}

func cognitionCommandPattern(id string) bool {
	for _, spec := range command.Catalog() {
		if spec.ID == id {
			return spec.AgentAllowed && !spec.RequiresDownload
		}
	}
	return false
}

func (s *Service) changeCognitionLearning(pause bool) error {
	s.cognitionMu.Lock()
	defer s.cognitionMu.Unlock()
	return s.transaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO cognition_learning_control(id,paused) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET paused=excluded.paused", pause); err != nil {
			return err
		}
		from, to := "paused", "pending"
		if pause {
			from, to = to, from
		}
		if _, err := tx.Exec("UPDATE learning_jobs SET status=?,updated_at=? WHERE status=?", to, now(), from); err != nil {
			return fmt.Errorf("update learning state: %w", err)
		}
		return nil
	})
}
