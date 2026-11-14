package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strings"

	"asimi/internal/developer"
)

type cognitionSkillStep struct {
	Tool   string `json:"tool"`
	Target string `json:"target"`
}

type agentSkillDispatch struct {
	ID        string   `json:"id"`
	Namespace string   `json:"namespace"`
	Version   int      `json:"version"`
	Checksum  string   `json:"checksum"`
	Tool      string   `json:"tool"`
	Args      []string `json:"args"`
	After     string   `json:"afterActionId"`
}

func validateCognitionSteps(in cognitionSkillInput) error {
	if len(in.Execution) == 0 {
		return nil
	}
	if len(in.Execution) != 1 || in.Execution[0] != (cognitionSkillStep{Tool: "go.test", Target: "changed-package"}) ||
		!slices.Contains(in.AllowedTools, in.Execution[0].Tool) {
		return codedError("COGNITION_INVALID_SKILL")
	}
	return nil
}

func (s *Service) selectAgentSkillDispatch(ctx context.Context, task AgentTask, workspace Workspace, skills []cognitionSkill) (*agentSkillDispatch, error) {
	rule := task.Agent.Acceptance
	if rule == nil || rule.Kind != "repair" || rule.CommandID != "" || len(rule.Paths) > 1 {
		return nil, nil
	}
	var skill *cognitionSkill
	for i := range skills {
		candidate := &skills[i]
		if strings.HasPrefix(candidate.Source, "reviewed:") && candidate.Enabled &&
			len(candidate.Execution) == 1 && validateCognitionSteps(candidate.cognitionSkillInput) == nil {
			skill = candidate
			break
		}
	}
	if skill == nil {
		return nil, nil
	}
	var edited ProductAction
	for i := len(task.Steps) - 1; i >= 0; i-- {
		step := task.Steps[i]
		if step.ActionID == "" {
			continue
		}
		if edited.ID != "" || step.Kind != "file" || step.Status != "COMPLETED" {
			return nil, nil
		}
		var err error
		edited, err = loadProductAction(s.db, step.ActionID)
		if err != nil {
			return nil, err
		}
	}
	file := edited.Proposal.File
	if file == nil || edited.TaskID != task.ID || edited.Status != "SUCCEEDED" ||
		edited.Proposal.WorkspaceID != workspace.ID || file.RootIdentity != task.Agent.BoundWorkspace ||
		!strings.EqualFold(path.Ext(file.RelativePath), ".go") ||
		(len(rule.Paths) == 1 && rule.Paths[0] != file.RelativePath) {
		return nil, nil
	}
	for _, round := range task.Agent.Rounds {
		if round.Skill != nil && round.Skill.After == edited.ID {
			return nil, nil
		}
	}
	var witness developer.ChangeResult
	if json.Unmarshal(edited.Result, &witness) != nil || !witness.Applied || !witness.Verified || witness.AfterHash != file.AfterHash {
		return nil, nil
	}
	options := developer.Options{Exclusions: slices.Clone(task.Agent.Exclusions)}
	current, err := developer.ReadContext(ctx, workspace.Root, file.RelativePath, options)
	if err != nil || current.Hash != witness.AfterHash || current.Identity != witness.Identity {
		return nil, ctx.Err()
	}
	if agentGoPackage(ctx, workspace.Root, file.RelativePath, options) == "" {
		return nil, ctx.Err()
	}
	target := path.Dir(file.RelativePath)
	if target != "." {
		target = "./" + target
	}
	return &agentSkillDispatch{ID: skill.ID, Namespace: skill.Namespace, Version: skill.Version, Checksum: skill.Checksum,
		Tool: skill.Execution[0].Tool, Args: []string{target}, After: edited.ID}, nil
}

func validateAgentSkillDispatch(q querier, workspaceID string, dispatch *agentSkillDispatch) error {
	if dispatch == nil {
		return nil
	}
	var source, checksum, raw string
	err := q.QueryRow(`SELECT p.source,v.checksum,v.manifest FROM skill_packages p
		JOIN skill_versions v ON v.package_id=p.id AND v.version=p.version
		WHERE p.id=? AND p.version=? AND p.enabled=1 AND (p.workspace_id IS NULL OR p.workspace_id=?)`,
		dispatch.ID, dispatch.Version, workspaceID).Scan(&source, &checksum, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return codedError("COGNITION_REVIEW_STALE")
	}
	if err != nil {
		return err
	}
	var manifest cognitionSkillInput
	if !strings.HasPrefix(source, "reviewed:") || checksum != dispatch.Checksum || cognitionChecksum(raw) != checksum ||
		json.Unmarshal([]byte(raw), &manifest) != nil || validateCognitionSteps(manifest) != nil ||
		len(manifest.Execution) != 1 || manifest.Execution[0].Tool != dispatch.Tool || validateCognitionDependencies(q, manifest) != nil {
		return codedError("COGNITION_REVIEW_STALE")
	}
	return nil
}
