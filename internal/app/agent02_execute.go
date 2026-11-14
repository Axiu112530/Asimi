package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"asimi/internal/ai"
	"asimi/internal/command"
	"asimi/internal/developer"
)

func (s *Service) planAgent02(id, hash string) (AgentTask, error) {
	epoch := s.mvpGeneration()
	if !s.mvpAdmission(epoch, id) {
		return AgentTask{}, codedError("ACTION_CANCELLED")
	}
	preview, err := s.previewAgent02(id)
	if err != nil {
		return AgentTask{}, err
	}
	if preview.Hash != hash {
		return AgentTask{}, codedError("TASK_CONTEXT_CHANGED")
	}
	s.mvp.mu.Lock()
	defer s.mvp.mu.Unlock()
	t, err := loadAgentTask(s.db, id)
	if err != nil {
		return t, err
	}
	if s.lifetime.Err() != nil || t.Status != "NEEDS_REVIEW" || t.UpdatedAt != preview.task.UpdatedAt || s.activeMVPControl("task:"+id) {
		return t, codedError("TASK_STATE")
	}
	if !t.Agent.Deadline.After(time.Now()) {
		return t, codedError("AGENT_DEADLINE_EXCEEDED")
	}
	if !s.mvpAdmission(epoch, id) {
		return t, codedError("ACTION_CANCELLED")
	}
	if t.Agent.Acceptance == nil {
		t.Agent.Acceptance = preview.task.Agent.Acceptance
	}
	round := agent02Round{Skill: preview.LocalSkill, Version: len(t.Agent.Rounds) + 1, Status: "RUNNING", ContextHash: hash, Files: preview.Files}
	t.Agent.Rounds = append(t.Agent.Rounds, round)
	t.Agent.ReservedUSD += preview.MaxRequestUSD
	step := AgentStep{ID: newID(), Kind: "agent.plan", Title: "AGENT_PLANNING", Status: "RUNNING"}
	t.Steps = append(t.Steps, step)
	t.Status = "RUNNING"
	t.CurrentStep = len(t.Steps) - 1
	t.Summary = "AGENT_PLANNING"
	t.UpdatedAt = now()
	if preview.LocalSkill != nil {
		t.Steps[t.CurrentStep].Title = preview.LocalSkill.Namespace
	}
	if err = s.transaction(func(tx *sql.Tx) error {
		current, err := cognitionRevision(tx)
		if err != nil {
			return err
		}
		if current != preview.knowledgeRevision {
			return codedError("TASK_CONTEXT_CHANGED")
		}
		if err := saveAgentTask(tx, t); err != nil {
			return err
		}
		return recordCognitionUsage(tx, t.ID, preview.matchedSkills)
	}); err != nil {
		return t, err
	}
	ctx, cancel := context.WithDeadline(s.lifetime, minTime(t.Agent.Deadline, time.Now().Add(time.Duration(preview.limits.TimeoutSeconds)*time.Second)))
	s.registerMVPControl("task:"+id, cancel, epoch, id)
	s.wg.Add(1)
	go s.runAgent02Round(ctx, preview, round.Version, step.ID, epoch)
	s.changed("", id)
	return t, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

type agent02Outcome struct {
	plan         agent02Plan
	steps        []AgentStep
	paths        []string
	action       *ProductAction
	final        string
	verification *agentVerification
}

func (s *Service) runAgent02Round(ctx context.Context, p agent02Preview, version int, planStep string, epoch uint64) {
	defer s.wg.Done()
	defer s.removeMVPControl("task:" + p.TaskID)
	var outcome agent02Outcome
	var response ai.Response
	var err error
	if p.LocalSkill != nil && ctx.Err() == nil {
		err = validateAgentSkillDispatch(s.db, p.workspace.ID, p.LocalSkill)
		if err == nil {
			plan := agent02Plan{Objective: p.task.Goal, RiskLevel: "process", Steps: []agent02PlanStep{{
				Type: "PROPOSE_COMMAND", CommandID: p.LocalSkill.Tool, Args: p.LocalSkill.Args, Summary: p.LocalSkill.Tool,
			}}}
			outcome, err = s.evaluateAgent02(ctx, p, version, plan)
		}
	} else if ctx.Err() == nil {
		response, err = s.requestAgent02Plan(ctx, p)
	}
	if p.LocalSkill == nil && err == nil && ctx.Err() == nil {
		var plan agent02Plan
		plan, err = parseAgent02Plan(response.Content)
		if err == nil {
			outcome, err = s.evaluateAgent02(ctx, p, version, plan)
		}
	}
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	s.finishAgent02Round(p.TaskID, version, planStep, epoch, outcome, response, p.model.Pricing, p.limits.Requests, err)
}

func (s *Service) evaluateAgent02(ctx context.Context, p agent02Preview, version int, plan agent02Plan) (agent02Outcome, error) {
	result := agent02Outcome{plan: plan, paths: slices.Clone(p.task.Agent.Paths)}
	if len(plan.Steps) > p.limits.StepsPerPlan || version == p.limits.Requests && plan.Steps[len(plan.Steps)-1].Type != "FINALIZE" {
		return result, codedError("AGENT_BUDGET_EXHAUSTED")
	}
	if err := agent02CheckRoot(p.task, p.workspace.Root); err != nil {
		return result, err
	}
	options := developer.Options{Exclusions: p.task.Agent.Exclusions}
	for _, planned := range plan.Steps {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		step := AgentStep{ID: newID(), Kind: "agent.observe", Title: boundedAgentText(planned.Summary, p.workspace.Root, 450), Status: "RUNNING"}
		if step.Title == "" {
			step.Title = planned.Type
		}
		switch planned.Type {
		case "READ_WORKSPACE":
			step.Kind = "agent.workspace"
			step.Result = json.RawMessage(encode(map[string]any{
				"contextHash": p.Hash, "observedAt": now(), "project": p.project.Project,
			}))
		case "ANALYZE_CODE":
			type analyzedFile struct {
				Path      string   `json:"path"`
				SHA256    string   `json:"sha256"`
				Symbols   []string `json:"symbols"`
				Truncated bool     `json:"truncated"`
			}
			var files []analyzedFile
			for _, file := range p.project.Files {
				files = append(files, analyzedFile{file.Path, file.SHA256, file.Symbols, file.Truncated})
			}
			step.Kind = "agent.analysis"
			if len(files) == 0 {
				step.Status, step.Error = "FAILED", "AGENT_EVIDENCE_MISSING"
			} else {
				step.Result = json.RawMessage(encode(files))
			}
		case "VERIFY_RESULT", "FINALIZE":
			verification, err := s.verifyAgent02(ctx, p, result.steps)
			if err != nil {
				return result, err
			}
			result.verification = &verification
			step.Kind = "agent.verify"
			if planned.Type == "FINALIZE" {
				step.Kind = "agent.finalize"
				result.final = boundedAgentText(planned.Summary, p.workspace.Root, 1500)
			}
			step.Result = json.RawMessage(encode(verification))
			if !verification.Passed {
				step.Status, step.Error = "FAILED", "AGENT_EVIDENCE_MISSING"
			}
		case "READ_FILE":
			if !developer.AgentContextPathAllowed(planned.Path) {
				return result, codedError("PATH_DENIED")
			}
			file, err := developer.ReadContext(ctx, p.workspace.Root, planned.Path, options)
			if err != nil {
				return result, err
			}
			if !slices.Contains(result.paths, file.Path) {
				if len(result.paths) >= agent02MaxFiles {
					return result, codedError("AGENT_FILE_SCOPE_LIMIT")
				}
				result.paths = append(result.paths, file.Path)
			}
			step.Kind = "agent.read"
			step.Result = json.RawMessage(encode(map[string]any{"path": file.Path, "sha256": file.Hash, "bytes": file.Bytes}))
		case "SEARCH_FILES":
			found, err := developer.AnalyzeContext(ctx, p.workspace.Root, planned.Query, options)
			if err != nil {
				return result, err
			}
			paths := []string{}
			for _, match := range found.Matches {
				if developer.AgentContextPathAllowed(match.Path) && !slices.Contains(paths, match.Path) && len(paths) < 12 {
					paths = append(paths, match.Path)
				}
			}
			step.Kind = "agent.search"
			step.Result = json.RawMessage(encode(paths))
		case "ANALYZE_ENVIRONMENT":
			checks := command.InspectAvailability(p.workspace.Root)
			step.Kind = "agent.environment"
			step.Result = json.RawMessage(encode(checks))
		case "PROPOSE_FILE_CHANGE", "PROPOSE_COMMAND":
			action, err := prepareAgent02Action(ctx, p, version, step.ID, planned)
			if err != nil {
				return result, err
			}
			result.action = &action
			step.Kind = action.Kind
			step.ActionID = action.ID
			step.Status = "WAITING_APPROVAL"
		default:
			return result, codedError("AGENT_STEP_UNSUPPORTED")
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if step.Status == "RUNNING" {
			step.Status = "COMPLETED"
		}
		result.steps = append(result.steps, step)
	}
	if err := agent02CheckRoot(p.task, p.workspace.Root); err != nil {
		return result, err
	}
	return result, nil
}

func prepareAgent02Action(ctx context.Context, p agent02Preview, version int, stepID string, step agent02PlanStep) (ProductAction, error) {
	w := p.workspace
	proposal := ProductProposal{Skill: p.LocalSkill, WorkspaceID: w.ID, Root: w.Root, PolicyVersion: agent02Policy, PlanVersion: version, StepID: stepID}
	kind := "file"
	if step.Type == "PROPOSE_FILE_CHANGE" {
		path := strings.ReplaceAll(step.Path, `\`, "/")
		editable := false
		for _, file := range p.project.Files {
			if file.Path == path && file.SHA256 == step.BaseSHA256 && file.Editable && !file.Redacted && !file.Truncated {
				editable = true
				break
			}
		}
		if !editable {
			return ProductAction{}, codedError("AGENT_FILE_NOT_REVIEWED")
		}
		content := *step.ProposedContent
		if developer.RedactAgentText(content) != content {
			return ProductAction{}, codedError("AGENT_SENSITIVE_PATCH")
		}
		change, err := developer.PrepareWithOptions(w.Root, path, content, developer.Options{Exclusions: p.task.Agent.Exclusions})
		if err != nil {
			return ProductAction{}, err
		}
		if change.Operation != "replace" || change.BeforeHash != step.BaseSHA256 || change.RootIdentity != p.task.Agent.BoundWorkspace {
			return ProductAction{}, codedError("STALE_TARGET")
		}
		if change.BeforeHash == change.AfterHash {
			return ProductAction{}, codedError("AGENT_EMPTY_PATCH")
		}
		proposal.File = &change
		recordGitFileContext(ctx, &proposal)
	} else {
		kind = "command"
		var spec *command.Spec
		for _, item := range command.Catalog() {
			if item.ID == step.CommandID && item.AgentAllowed && !item.RequiresDownload {
				candidate := item
				spec = &candidate
				break
			}
		}
		if spec == nil {
			return ProductAction{}, codedError("COMMAND_DENIED")
		}
		commandPlan, err := command.Prepare(step.CommandID, step.Args, w.Root, spec.Network)
		if err != nil {
			return ProductAction{}, err
		}
		if commandPlan.DirectoryID != p.task.Agent.BoundWorkspace {
			return ProductAction{}, codedError("STALE_WORKSPACE")
		}
		proposal.Command = &commandPlan
	}
	stamp := now()
	action := ProductAction{ID: newID(), TaskID: p.TaskID, Kind: kind, Status: "PENDING", CreatedAt: stamp, UpdatedAt: stamp, ExpiresAt: minTime(p.task.Agent.Deadline, time.Now().UTC().Add(5*time.Minute)).Format(time.RFC3339Nano), Proposal: proposal}
	action.Hash = productHash(action)
	return action, nil
}

func (s *Service) finishAgent02Round(id string, version int, stepID string, epoch uint64, outcome agent02Outcome, response ai.Response, price *ai.Pricing, maxRequests int, roundErr error) {
	s.mvp.mu.Lock()
	defer s.mvp.mu.Unlock()
	t, err := loadAgentTask(s.db, id)
	if err != nil || t.Agent == nil || len(t.Agent.Rounds) != version || t.Status != "RUNNING" || !s.mvpAdmission(epoch, id) {
		return
	}
	round := &t.Agent.Rounds[version-1]
	if round.Status != "RUNNING" || t.CurrentStep >= len(t.Steps) || t.Steps[t.CurrentStep].ID != stepID {
		return
	}
	round.Usage = response.Usage
	round.DurationMS = response.DurationMS
	round.EstimatedUSD = estimateCost(response.Usage, price)
	t.Status = "NEEDS_REVIEW"
	t.Summary = "AGENT_CONTEXT_REVIEW"
	t.UpdatedAt = now()
	if roundErr == nil && round.Skill != nil {
		roundErr = validateAgentSkillDispatch(s.db, t.WorkspaceID, round.Skill)
	}
	if roundErr == nil && !t.Agent.Deadline.After(time.Now()) {
		roundErr = context.DeadlineExceeded
	}
	if roundErr != nil {
		round.Status = "FAILED"
		round.Error = errorCode(roundErr)
		if errors.Is(roundErr, context.DeadlineExceeded) {
			round.Error = "REQUEST_TIMEOUT"
			if !t.Agent.Deadline.After(time.Now()) {
				round.Error = "AGENT_DEADLINE_EXCEEDED"
			}
		}
		if errors.Is(roundErr, context.Canceled) {
			round.Error = "REQUEST_CANCELLED"
		}
		t.Summary = round.Error
		t.Steps[t.CurrentStep].Status = "FAILED"
		t.Steps[t.CurrentStep].Error = round.Error
		if version == maxRequests || !t.Agent.Deadline.After(time.Now()) {
			t.Status = "FAILED"
		}
	} else {
		round.Status = "COMPLETED"
		// Proposed content is already held by the existing action journal.
		// Plan history retains only the step metadata and binding.
		for i := range outcome.plan.Steps {
			step := &outcome.plan.Steps[i]
			step.ProposedContent = nil
			step.Summary = developer.RedactAgentText(step.Summary)
		}
		outcome.plan.Objective = developer.RedactAgentText(outcome.plan.Objective)
		for i, v := range outcome.plan.Assumptions {
			outcome.plan.Assumptions[i] = developer.RedactAgentText(v)
		}
		for i, v := range outcome.plan.ExpectedResults {
			outcome.plan.ExpectedResults[i] = developer.RedactAgentText(v)
		}
		round.Plan = &outcome.plan
		t.Agent.Paths = outcome.paths
		t.Steps[t.CurrentStep].Status = "COMPLETED"
		t.Steps = append(t.Steps, outcome.steps...)
		t.CurrentStep = len(t.Steps) - 1
		if outcome.action != nil {
			round.ActionID = outcome.action.ID
			t.Status = "WAITING_APPROVAL"
			t.Summary = "AGENT_ACTION_REVIEW"
		}
		t.Agent.Verification = outcome.verification
		if outcome.final != "" && outcome.verification != nil {
			if outcome.verification.Passed {
				t.Status = "COMPLETED"
				t.Summary = outcome.final
			} else {
				t.Summary = "AGENT_EVIDENCE_MISSING"
				if version == maxRequests {
					t.Status = "FAILED"
				}
			}
		}
		if version == maxRequests && t.Status == "NEEDS_REVIEW" {
			t.Status = "FAILED"
			t.Summary = "AGENT_BUDGET_EXHAUSTED"
		}
	}
	err = s.transaction(func(tx *sql.Tx) error {
		if err := saveAgentTask(tx, t); err != nil {
			return err
		}
		if roundErr == nil && outcome.action != nil {
			return savePreparedProduct(tx, *outcome.action, id)
		}
		return nil
	})
	if err != nil {
		s.agentStorageFaultLocked(id, err)
		return
	}
	if roundErr == nil && outcome.action != nil {
		s.mvp.immutable[outcome.action.ID] = encode(*outcome.action)
	}
	s.changed("", id)
}
