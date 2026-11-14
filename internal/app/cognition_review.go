package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"asimi/internal/developer"
)

type cognitionCandidate struct {
	ID          string               `json:"id"`
	TaskID      string               `json:"taskId"`
	WorkspaceID string               `json:"workspaceId"`
	Manifest    cognitionSkillInput  `json:"manifest"`
	Previous    *cognitionSkillInput `json:"previous,omitempty"`
	BaseVersion int                  `json:"baseVersion"`
	Status      string               `json:"status"`
	CreatedAt   string               `json:"createdAt"`
}

func queueCognitionCandidate(tx *sql.Tx, task AgentTask, goal string, checks, workflow []string) error {
	if len(workflow) < 2 || len(checks) == 0 {
		return nil
	}
	workflow = workflow[:min(len(workflow), 12)]
	checks = checks[:min(len(checks), 12)]
	anchor := strings.ReplaceAll(checks[0], ".", "-")
	if !cognitionSlug.MatchString("workflow-" + anchor) {
		return nil
	}
	in := cognitionSkillInput{Scope: "project", WorkspaceID: task.WorkspaceID, Slug: "workflow-" + anchor,
		Name: "Verified workflow · " + checks[0], Description: "A project workflow derived from a completed task and its verified actions.",
		Instructions:  "Review the task's relevant source before proposing a change. Use the recorded workflow as guidance and verify the result with the current project's approved checks.",
		Applicability: goal, Prerequisites: "A reviewed workspace and user authorization for each effect.",
		Workflow: workflow, ExpectedOutputs: "An independently verified result and a summary of the checks run.",
		Validation:       "Inspect the actual exit status and report any remaining failures.",
		Limitations:      "Observed on one completed task. Adapt to the current workspace and objective.",
		SourceExperience: task.ID, Triggers: []string{checks[0]}, AllowedTools: checks}
	if task.Agent != nil && task.Agent.Acceptance != nil && task.Agent.Acceptance.Kind == "repair" &&
		task.Agent.Verification != nil && task.Agent.Verification.Passed && slices.Contains(checks, "go.test") {
		in.Execution = []cognitionSkillStep{{Tool: "go.test", Target: "changed-package"}}
	}
	if len(goal) > 0 && len(goal) <= 100 {
		in.Triggers = append(in.Triggers, goal)
	}
	if err := normalizeCognitionSkill(&in); err != nil {
		return err
	}
	return persistCognitionCandidate(tx, task, in)
}

func persistCognitionCandidate(tx *sql.Tx, task AgentTask, in cognitionSkillInput) error {
	var base int
	var checksum, previous string
	err := tx.QueryRow(`SELECT p.version,v.checksum,v.manifest FROM skill_packages p JOIN skill_versions v
 ON v.package_id=p.id AND v.version=p.version WHERE COALESCE(p.workspace_id,'')=? AND p.slug=?`, in.WorkspaceID, in.Slug).Scan(&base, &checksum, &previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if base > 0 {
		var old cognitionSkillInput
		if err := json.Unmarshal([]byte(previous), &old); err != nil {
			return err
		}
		if sameCognitionWorkflow(old, in) {
			return nil
		}
	}
	var pending string
	err = tx.QueryRow(`SELECT manifest FROM cognition_skill_candidates WHERE workspace_id=? AND status='pending'
 AND json_extract(manifest,'$.slug')=? ORDER BY created_at DESC LIMIT 1`, task.WorkspaceID, in.Slug).Scan(&pending)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if pending != "" {
		var old cognitionSkillInput
		if err := json.Unmarshal([]byte(pending), &old); err != nil {
			return err
		}
		if sameCognitionWorkflow(old, in) {
			return nil
		}
	}
	_, err = tx.Exec(`INSERT OR IGNORE INTO cognition_skill_candidates(id,task_id,workspace_id,manifest,base_version,base_checksum,status,created_at)
 VALUES(?,?,?,?,?,?,'pending',?)`, newID(), task.ID, task.WorkspaceID, encode(in), base, checksum, now())
	return err
}

func queuePersonalCognitionCandidate(tx *sql.Tx, task AgentTask) (bool, error) {
	if task.Agent == nil {
		return false, nil
	}
	directive, method, ok := strings.Cut(strings.Replace(task.Agent.CognitionCorrection, "：", ":", 1), ":")
	if !ok || (!strings.EqualFold(strings.TrimSpace(directive), "personal workflow") && strings.TrimSpace(directive) != "个人工作流") {
		return false, nil
	}
	method = strings.TrimSpace(method)
	if !cognitionText(method, 1200) {
		return false, nil
	}
	// Personal scope is explicit. Only the user's instruction crosses the
	// project boundary, while the original task stays in the review queue.
	slug := "personal-" + cognitionChecksum(strings.ToLower(method))[:12]
	in := cognitionSkillInput{Scope: "user", Slug: slug, Name: "Personal workflow", Description: "A reusable workflow explicitly requested by the user.",
		Instructions: method, Applicability: "Use when relevant to the current task.", SourceExperience: task.ID}
	if len(method) <= 100 {
		in.Triggers = []string{method}
	}
	if normalizeCognitionSkill(&in) != nil {
		return false, nil
	}
	return true, persistCognitionCandidate(tx, task, in)
}

func sameCognitionWorkflow(a, b cognitionSkillInput) bool {
	return a.Instructions == b.Instructions && encode(a.Workflow) == encode(b.Workflow) && encode(a.AllowedTools) == encode(b.AllowedTools) && slices.Equal(a.Execution, b.Execution)
}

func (s *Service) cognitionCandidates(project string) ([]cognitionCandidate, error) {
	rows, err := s.db.Query(`SELECT c.id,c.task_id,c.workspace_id,c.manifest,c.base_version,c.status,c.created_at,
 COALESCE((SELECT v.manifest FROM skill_packages p JOIN skill_versions v ON v.package_id=p.id
 WHERE COALESCE(p.workspace_id,'')=COALESCE(json_extract(c.manifest,'$.workspaceId'),'') AND p.slug=json_extract(c.manifest,'$.slug') AND v.version=c.base_version
 AND v.checksum=c.base_checksum),'')
 FROM cognition_skill_candidates c WHERE c.workspace_id=? ORDER BY c.status='pending' DESC,c.created_at DESC LIMIT 50`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []cognitionCandidate{}
	for rows.Next() {
		var c cognitionCandidate
		var manifest, previous string
		if err := rows.Scan(&c.ID, &c.TaskID, &c.WorkspaceID, &manifest, &c.BaseVersion, &c.Status, &c.CreatedAt, &previous); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(manifest), &c.Manifest); err != nil {
			return nil, err
		}
		if previous != "" {
			c.Previous = new(cognitionSkillInput)
			if err := json.Unmarshal([]byte(previous), c.Previous); err != nil {
				return nil, err
			}
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

func (s *Service) reviewCognitionCandidate(payload string) error {
	var in struct {
		ID          string `json:"id"`
		WorkspaceID string `json:"workspaceId"`
		Approve     bool   `json:"approve"`
	}
	if err := decode(payload, &in); err != nil {
		return err
	}
	if in.ID == "" || in.WorkspaceID == "" {
		return codedError("INVALID_REQUEST")
	}
	s.cognitionMu.Lock()
	defer s.cognitionMu.Unlock()
	return s.transaction(func(tx *sql.Tx) error {
		var raw, baseChecksum, taskID string
		var baseVersion int
		err := tx.QueryRow(`SELECT manifest,base_version,base_checksum,task_id FROM cognition_skill_candidates
  WHERE id=? AND workspace_id=? AND status='pending'`, in.ID, in.WorkspaceID).Scan(&raw, &baseVersion, &baseChecksum, &taskID)
		if err != nil {
			return mvpNotFound(err)
		}
		status := "rejected"
		if in.Approve {
			var skill cognitionSkillInput
			if err := json.Unmarshal([]byte(raw), &skill); err != nil {
				return err
			}
			if (skill.Scope == "project" && skill.WorkspaceID != in.WorkspaceID) || skill.SourceExperience != taskID {
				return codedError("COGNITION_INVALID_SKILL")
			}
			if err := normalizeCognitionSkill(&skill); err != nil {
				return err
			}
			var current int
			var currentChecksum string
			err := tx.QueryRow(`SELECT p.version,v.checksum FROM skill_packages p JOIN skill_versions v ON v.package_id=p.id
   AND v.version=p.version WHERE COALESCE(p.workspace_id,'')=? AND p.slug=?`, skill.WorkspaceID, skill.Slug).Scan(&current, &currentChecksum)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if current != baseVersion || currentChecksum != baseChecksum {
				return codedError("COGNITION_REVIEW_STALE")
			}
			if err := saveCognitionSkill(tx, skill, "reviewed:"+taskID); err != nil {
				return err
			}
			status = "approved"
		}
		if _, err := tx.Exec("UPDATE cognition_skill_candidates SET status=?,reviewed_at=? WHERE id=?", status, now(), in.ID); err != nil {
			return err
		}
		return audit(tx, "", "COGNITION_SKILL_REVIEWED", map[string]string{"id": in.ID, "status": status, "sourceTaskId": taskID})
	})
}

func cognitionEvidencePath(value string) bool {
	return value != "" && developer.AgentContextPathAllowed(value) && cognitionText(value, 240)
}
