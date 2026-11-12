package app

import (
	"database/sql"
	"strings"
	"time"

	"deskpilot/internal/ai"
	"deskpilot/internal/configuration"
	"deskpilot/internal/developer"
	"deskpilot/internal/domain"
)

const agent02MaxFiles = 4

type agent02Run struct {
	ProviderID     string                     `json:"providerId"`
	ModelID        string                     `json:"modelId"`
	BoundWorkspace string                     `json:"boundWorkspace"`
	Paths          []string                   `json:"paths"`
	Deadline       time.Time                  `json:"deadline"`
	ReservedUSD    float64                    `json:"reservedUsd"`
	Rounds         []agent02Round             `json:"rounds"`
	Limits         *configuration.AgentLimits `json:"limits,omitempty"`
	Exclusions     []string                   `json:"exclusions,omitempty"`
}

type agent02Round struct {
	Version      int          `json:"version"`
	Status       string       `json:"status"`
	ContextHash  string       `json:"contextHash"`
	Files        []string     `json:"files"`
	Plan         *agent02Plan `json:"plan,omitempty"`
	ActionID     string       `json:"actionId,omitempty"`
	Error        string       `json:"error,omitempty"`
	DurationMS   int64        `json:"durationMs"`
	Usage        ai.Usage     `json:"usage"`
	EstimatedUSD *float64     `json:"estimatedUsd"`
}

type agent02CreateInput struct {
	WorkspaceID string   `json:"workspaceId"`
	Goal        string   `json:"goal"`
	ProviderID  string   `json:"providerId"`
	ModelID     string   `json:"modelId"`
	Paths       []string `json:"paths"`
	AutoSelect  bool     `json:"autoSelect,omitempty"`
	Exclusions  []string `json:"exclusions,omitempty"`
}

func (s *Service) callAgent02(method, payload string) (domain.Reply, bool) {
	done := func(value any, err error) (domain.Reply, bool) { return result(value, err), true }
	switch method {
	case "CreateAgentRun":
		var q agent02CreateInput
		if err := decode(payload, &q); err != nil {
			return done(nil, err)
		}
		value, err := s.createAgent02(q)
		return done(value, err)
	case "PreviewAgentRun", "PlanAgentRun", "ResumeAgentRun":
		var q struct {
			ID                  string `json:"id"`
			ContextHash         string `json:"contextHash,omitempty"`
			AcknowledgeRecovery bool   `json:"acknowledgeRecovery,omitempty"`
		}
		if err := decode(payload, &q); err != nil {
			return done(nil, err)
		}
		if q.ID == "" {
			return done(nil, codedError("INVALID_REQUEST"))
		}
		switch method {
		case "PreviewAgentRun":
			value, err := s.previewAgent02(q.ID)
			return done(value, err)
		case "PlanAgentRun":
			if !validAgentSHA(q.ContextHash) {
				return done(nil, codedError("CONFIRM_SEND_REQUIRED"))
			}
			value, err := s.planAgent02(q.ID, q.ContextHash)
			return done(value, err)
		default:
			s.mvp.mu.Lock()
			defer s.mvp.mu.Unlock()
			task, err := loadAgentTask(s.db, q.ID)
			if err != nil {
				return done(nil, err)
			}
			value, err := s.resumeAgent02Locked(task, q.AcknowledgeRecovery)
			return done(value, err)
		}
	}
	return domain.Reply{}, false
}

func (s *Service) createAgent02(input agent02CreateInput) (AgentTask, error) {
	goal := strings.TrimSpace(input.Goal)
	if goal == "" || !validAgentText(goal, 8192) || len(input.Paths) > agent02MaxFiles {
		return AgentTask{}, codedError("INVALID_REQUEST")
	}
	profile, _, _, err := s.agent02Provider(input.ProviderID, input.ModelID)
	if err != nil {
		return AgentTask{}, err
	}
	if profile.ID == "" {
		return AgentTask{}, codedError("PROVIDER_UNAVAILABLE")
	}
	resolved, err := s.resolvedConfiguration()
	if err != nil {
		return AgentTask{}, err
	}
	w, err := s.workspace(input.WorkspaceID)
	if err != nil {
		return AgentTask{}, err
	}
	identity, err := developer.RootIdentity(w.Root)
	if err != nil {
		return AgentTask{}, err
	}
	if input.AutoSelect && len(input.Paths) == 0 {
		input.Paths, err = developer.SuggestAgentPaths(w.Root, goal, input.Exclusions)
		if err != nil {
			return AgentTask{}, err
		}
	}
	seen := map[string]bool{}
	for _, path := range input.Paths {
		key := strings.ToLower(path)
		if path == "" || len(path) > 512 || seen[key] {
			return AgentTask{}, codedError("INVALID_REQUEST")
		}
		seen[key] = true
	}
	s.mvp.mu.Lock()
	defer s.mvp.mu.Unlock()
	if s.lifetime.Err() != nil {
		return AgentTask{}, codedError("SERVICE_CLOSED")
	}
	stamp := now()
	t := AgentTask{ID: newID(), WorkspaceID: w.ID, Goal: goal, Status: "NEEDS_REVIEW", Summary: "AGENT_CONTEXT_REVIEW", CreatedAt: stamp, UpdatedAt: stamp, Steps: []AgentStep{}, Agent: &agent02Run{ProviderID: input.ProviderID, ModelID: input.ModelID, BoundWorkspace: identity, Paths: input.Paths, Deadline: time.Now().UTC().Add(time.Duration(resolved.Agent.RunSeconds) * time.Second), Rounds: []agent02Round{}, Limits: &resolved.Agent, Exclusions: input.Exclusions}}
	err = s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, t) })
	if err == nil {
		s.changed("", t.ID)
	}
	return t, err
}

// The legacy resume path reruns its local plan. Model-driven runs instead
// rebuild a review preview, preserving every prior effect and spent request.
func (s *Service) resumeAgent02Locked(t AgentTask, acknowledge bool) (AgentTask, error) {
	if t.Agent == nil || t.Status != "PAUSED" {
		return t, codedError("TASK_STATE")
	}
	if s.activeMVPControl("task:"+t.ID) || s.activeMVPControl("effect:"+t.ID) {
		return t, codedError("REQUEST_ACTIVE")
	}
	if !s.mvpAdmission(s.mvpGeneration(), t.ID) {
		return t, codedError("ACTION_CANCELLED")
	}
	for i := range t.Steps {
		step := &t.Steps[i]
		if step.ActionID != "" {
			action, err := loadProductAction(s.db, step.ActionID)
			if err != nil {
				return t, err
			}
			if action.TaskID != t.ID {
				return t, codedError("APPROVAL_MISMATCH")
			}
			if action.Status == "EXECUTING" {
				return t, codedError("RECOVERY_REQUIRED")
			}
			if action.Status == "RECOVERY_REQUIRED" || action.Status == "EXPIRED" {
				step.Status = action.Status
			}
		}
		if step.Status == "RECOVERY_REQUIRED" && !acknowledge {
			return t, codedError("AGENT_ACKNOWLEDGE_RECOVERY")
		}
		if step.Status == "RUNNING" || step.Status == "PENDING" {
			step.Status = "CANCELLED"
		}
	}
	for i := range t.Agent.Rounds {
		if t.Agent.Rounds[i].Status == "RUNNING" {
			t.Agent.Rounds[i].Status = "INTERRUPTED"
		}
	}
	t.Status = "NEEDS_REVIEW"
	t.Summary = "AGENT_CONTEXT_REVIEW"
	t.UpdatedAt = now()
	err := s.transaction(func(tx *sql.Tx) error { return saveAgentTask(tx, t) })
	if err == nil {
		s.changed("", t.ID)
	}
	return t, err
}
