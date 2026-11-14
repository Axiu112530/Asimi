package app

import (
	"asimi/internal/domain"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

const cognitionEvolutionSchema = `
CREATE TABLE IF NOT EXISTS cognition_memory_maintenance(id INTEGER PRIMARY KEY CHECK(id=1), last_run_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS cognition_memory_lifetime(
 memory_id TEXT PRIMARY KEY REFERENCES memory_records(id) ON DELETE CASCADE, expires_at TEXT NOT NULL DEFAULT ''
);
CREATE TRIGGER IF NOT EXISTS cognition_lifetime_insert AFTER INSERT ON cognition_memory_lifetime BEGIN
 UPDATE cognition_revision SET revision=revision+1 WHERE id=1; END;
CREATE TRIGGER IF NOT EXISTS cognition_lifetime_update AFTER UPDATE ON cognition_memory_lifetime BEGIN
 UPDATE cognition_revision SET revision=revision+1 WHERE id=1; END;
`

func (s *Service) initializeEvolution() error {
	if s.lifetime != nil && s.lifetime.Err() != nil {
		return nil
	}
	if _, err := s.db.Exec(cognitionEvolutionSchema); err != nil {
		return err
	}
	return s.automaticMemoryMaintenance(time.Now())
}
func (s *Service) callEvolution(method, payload string) (domain.Reply, bool) {
	done := func(v any, e error) (domain.Reply, bool) { return result(v, e), true }
	switch method {
	case "GetEvolutionInsights":
		var in struct {
			WorkspaceID string `json:"workspaceId"`
		}
		if e := decode(payload, &in); e != nil {
			return done(nil, e)
		}
		v, e := s.evolutionInsights(in.WorkspaceID)
		return done(v, e)
	case "RunMemoryMaintenance":
		v, e := s.runMemoryMaintenance(payload)
		return done(v, e)
	case "ComposeCognitionSkills":
		v, e := s.composeCognitionSkills(payload)
		return done(v, e)
	case "ProposeSkillImprovement":
		v, e := s.proposeSkillImprovement(payload)
		return done(v, e)
	}
	return domain.Reply{}, false
}

type cognitionSkillDependency struct {
	ID       string `json:"id"`
	Version  int    `json:"version"`
	Checksum string `json:"checksum"`
}
type evolutionSkillMetric struct {
	ID                  string `json:"id"`
	Namespace           string `json:"namespace"`
	Version             int    `json:"version"`
	ContextUses         int    `json:"contextUses"`
	Executions          int    `json:"executions"`
	Successes           int    `json:"successes"`
	Failures            int    `json:"failures"`
	TotalDurationMS     int64  `json:"totalDurationMs"`
	ModelRequests       int    `json:"modelRequests"`
	SelectionAdjustment int    `json:"selectionAdjustment"`
}
type evolutionFailure struct {
	TaskID  string `json:"taskId"`
	SkillID string `json:"skillId"`
	Version int    `json:"version"`
	Code    string `json:"code"`
	Summary string `json:"summary"`
}
type evolutionMemoryMetric struct {
	Expired    int `json:"expired"`
	Conflicted int `json:"conflicted"`
	Archived   int `json:"archived"`
	Active     int `json:"active"`
}
type evolutionInsights struct {
	Skills   []evolutionSkillMetric `json:"skills"`
	Failures []evolutionFailure     `json:"failures"`
	Memory   evolutionMemoryMetric  `json:"memory"`
	Boundary string                 `json:"boundary"`
}

func evolutionKey(id string, v int) string { return fmt.Sprintf("%s:%d", id, v) }
func evolutionAdjustment(successes, failures int) int {
	total := successes + failures
	if total < 5 {
		return 0
	}
	rate := float64(successes) / float64(total)
	if rate >= 0.8 {
		return 2
	}
	if rate <= 0.2 {
		return -2
	}
	return 0
}

// Outcomes describe an approved consumed action, never mere context inclusion or plan generation.
func (s *Service) evolutionInsights(project string) (evolutionInsights, error) {
	out := evolutionInsights{Skills: []evolutionSkillMetric{}, Failures: []evolutionFailure{}, Boundary: "local-profile"}
	if project != "" {
		if _, e := s.workspace(project); e != nil {
			return out, e
		}
	}
	rows, e := s.db.Query(`SELECT p.id,COALESCE(p.workspace_id,''),p.slug,v.version,
 (SELECT COUNT(*) FROM skill_usage u JOIN agent_tasks t ON t.id=u.task_id WHERE u.package_id=p.id AND u.version=v.version AND t.workspace_id=?)
 FROM skill_packages p JOIN skill_versions v ON v.package_id=p.id WHERE p.workspace_id IS NULL OR p.workspace_id=? ORDER BY p.id,v.version`, project, project)
	if e != nil {
		return out, e
	}
	indexes := map[string]int{}
	for rows.Next() {
		var m evolutionSkillMetric
		var scope, slug string
		if e = rows.Scan(&m.ID, &scope, &slug, &m.Version, &m.ContextUses); e != nil {
			break
		}
		m.Namespace = "project:" + slug
		if scope == "" {
			m.Namespace = "user:" + slug
		}
		indexes[evolutionKey(m.ID, m.Version)] = len(out.Skills)
		out.Skills = append(out.Skills, m)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return out, e
	}
	rows, e = s.db.Query(`SELECT a.id,a.task_id,a.status,a.proposal_json,COALESCE(a.result_json,''),a.error_code,t.json FROM product_actions a JOIN agent_tasks t ON t.id=a.task_id
 WHERE a.workspace_id=? AND t.workspace_id=? AND a.kind='command' AND a.consumed=1 AND a.status IN ('SUCCEEDED','FAILED') AND json_extract(a.proposal_json,'$.skill.id') IS NOT NULL ORDER BY a.created_at,a.id`, project, project)
	if e != nil {
		return out, e
	}
	seenTasks := map[string]bool{}
	for rows.Next() {
		var id, taskID, status, proposal, raw, code, taskRaw string
		if e = rows.Scan(&id, &taskID, &status, &proposal, &raw, &code, &taskRaw); e != nil {
			break
		}
		var p ProductProposal
		var task AgentTask
		if json.Unmarshal([]byte(proposal), &p) != nil || p.Skill == nil || json.Unmarshal([]byte(taskRaw), &task) != nil || task.Agent == nil || p.WorkspaceID != project || p.Command == nil || p.Command.ID != p.Skill.Tool {
			continue
		}
		bound := false
		requests := 0
		for _, r := range task.Agent.Rounds {
			if r.Skill == nil && (r.Status == "COMPLETED" || r.Status == "FAILED" && (r.DurationMS > 0 || r.Usage.UsageAvailable)) {
				requests++
			}
			if r.ActionID == id && r.Skill != nil && r.Skill.ID == p.Skill.ID && r.Skill.Version == p.Skill.Version && r.Skill.Checksum == p.Skill.Checksum {
				bound = true
			}
		}
		if !bound {
			continue
		}
		key := evolutionKey(p.Skill.ID, p.Skill.Version)
		index, ok := indexes[key]
		if !ok {
			continue
		}
		var witness struct {
			ExitCode   *int  `json:"exitCode"`
			DurationMS int64 `json:"durationMs"`
			Cancelled  bool  `json:"cancelled"`
			TimedOut   bool  `json:"timedOut"`
		}
		if json.Unmarshal([]byte(raw), &witness) != nil && status == "SUCCEEDED" {
			continue
		}
		success := status == "SUCCEEDED" && witness.ExitCode != nil && *witness.ExitCode == 0 && !witness.Cancelled && !witness.TimedOut
		if status == "SUCCEEDED" && !success {
			continue
		}
		m := &out.Skills[index]
		m.Executions++
		m.TotalDurationMS += max(0, witness.DurationMS)
		if !seenTasks[key+":"+taskID] {
			m.ModelRequests += requests
			seenTasks[key+":"+taskID] = true
		}
		if success {
			m.Successes++
		} else {
			m.Failures++
			safeCode := "COMMAND_FAILED"
			if witness.TimedOut {
				safeCode = "COMMAND_TIMEOUT"
			}
			if witness.Cancelled {
				safeCode = "ACTION_CANCELLED"
			}
			out.Failures = append(out.Failures, evolutionFailure{taskID, p.Skill.ID, p.Skill.Version, safeCode, "The approved skill command did not pass. Inspect the current check and verify the next result before reuse."})
		}
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return out, e
	}
	for i := range out.Skills {
		m := &out.Skills[i]
		m.SelectionAdjustment = evolutionAdjustment(m.Successes, m.Failures)
	}
	// Keep all version metrics; bound user-visible failures without changing the sample counts.
	if len(out.Failures) > 100 {
		out.Failures = out.Failures[len(out.Failures)-100:]
	}
	e = s.db.QueryRow(`SELECT COUNT(*) FILTER(WHERE l.expires_at<>'' AND julianday(l.expires_at)<=julianday('now')),
 COUNT(*) FILTER(WHERE EXISTS(SELECT 1 FROM cognition_preference_reviews r WHERE r.memory_id=m.id AND r.status='pending')),
 COUNT(*) FILTER(WHERE COALESCE(a.archived,0)=1),
 COUNT(*) FILTER(WHERE COALESCE(a.archived,0)=0 AND (COALESCE(l.expires_at,'')='' OR julianday(l.expires_at)>julianday('now')) AND NOT EXISTS(SELECT 1 FROM cognition_preference_reviews r WHERE r.memory_id=m.id AND r.status='pending'))
 FROM memory_records m LEFT JOIN cognition_memory_state a ON a.memory_id=m.id LEFT JOIN cognition_memory_lifetime l ON l.memory_id=m.id WHERE m.workspace_id IS NULL OR m.workspace_id=?`, project).Scan(&out.Memory.Expired, &out.Memory.Conflicted, &out.Memory.Archived, &out.Memory.Active)
	return out, e
}
func (s *Service) proposeSkillImprovement(payload string) (cognitionCandidate, error) {
	var in struct {
		WorkspaceID  string `json:"workspaceId"`
		SkillID      string `json:"skillId"`
		TaskID       string `json:"taskId"`
		Instructions string `json:"instructions"`
	}
	if e := decode(payload, &in); e != nil {
		return cognitionCandidate{}, e
	}
	s.cognitionMu.Lock()
	defer s.cognitionMu.Unlock()
	insights, e := s.evolutionInsights(in.WorkspaceID)
	if e != nil {
		return cognitionCandidate{}, e
	}
	var evidence *evolutionFailure
	for i := range insights.Failures {
		f := &insights.Failures[i]
		if f.SkillID == in.SkillID && f.TaskID == in.TaskID {
			evidence = f
			break
		}
	}
	if evidence == nil {
		return cognitionCandidate{}, codedError("COGNITION_NO_EXECUTION_EVIDENCE")
	}
	task, e := loadAgentTask(s.db, in.TaskID)
	if e != nil {
		return cognitionCandidate{}, e
	}
	if task.WorkspaceID != in.WorkspaceID {
		return cognitionCandidate{}, codedError("NOT_FOUND")
	}
	expectedManifest := ""
	e = s.transaction(func(tx *sql.Tx) error {
		var raw string
		var version int
		if e := tx.QueryRow(`SELECT v.manifest,p.version FROM skill_packages p JOIN skill_versions v ON v.package_id=p.id AND v.version=p.version WHERE p.id=? AND (p.workspace_id IS NULL OR p.workspace_id=?)`, in.SkillID, in.WorkspaceID).Scan(&raw, &version); e != nil {
			return mvpNotFound(e)
		}
		if version != evidence.Version {
			return codedError("COGNITION_REVIEW_STALE")
		}
		var skill cognitionSkillInput
		if e := json.Unmarshal([]byte(raw), &skill); e != nil {
			return e
		}
		if in.Instructions == "" {
			skill.Instructions = strings.TrimSpace(skill.Instructions) + " Before reusing this workflow, inspect the previous failed check, diagnose its cause, and independently verify the current result."
		} else {
			skill.Instructions = in.Instructions
		}
		skill.SourceExperience = task.ID
		if e := normalizeCognitionSkill(&skill); e != nil {
			return e
		}
		expectedManifest = encode(skill)
		return persistCognitionCandidate(tx, task, skill)
	})
	if e != nil {
		return cognitionCandidate{}, e
	}
	candidates, e := s.cognitionCandidates(in.WorkspaceID)
	if e != nil {
		return cognitionCandidate{}, e
	}
	for _, c := range candidates {
		if c.TaskID == in.TaskID && c.Status == "pending" && c.BaseVersion == evidence.Version && encode(c.Manifest) == expectedManifest {
			return c, nil
		}
	}
	return cognitionCandidate{}, codedError("COGNITION_REVIEW_STALE")
}

// Every edge is pinned and rechecked. A changed, disabled, or cross-scope dependency invalidates the whole composition.
func validateCognitionDependencies(q querier, in cognitionSkillInput) error {
	visiting := map[string]bool{}
	nodes := 0
	var walk func(cognitionSkillInput, int) error
	walk = func(parent cognitionSkillInput, depth int) error {
		if depth > 8 || len(parent.Dependencies) > 8 {
			return codedError("COGNITION_REVIEW_STALE")
		}
		if len(parent.Dependencies) == 0 {
			return nil
		}
		union := []string{}
		supported := []cognitionSkillStep{}
		seen := map[string]bool{}
		for _, dep := range parent.Dependencies {
			nodes++
			if nodes > 32 || dep.ID == "" || dep.Version < 1 || !validAgentSHA(dep.Checksum) || visiting[dep.ID] || seen[dep.ID] {
				return codedError("COGNITION_REVIEW_STALE")
			}
			seen[dep.ID] = true
			var raw, checksum, source, project string
			e := q.QueryRow(`SELECT v.manifest,v.checksum,p.source,COALESCE(p.workspace_id,'') FROM skill_packages p JOIN skill_versions v ON v.package_id=p.id AND v.version=p.version
 WHERE p.id=? AND p.version=? AND p.enabled=1 AND NOT EXISTS(SELECT 1 FROM cognition_skill_state st WHERE st.package_id=p.id)`, dep.ID, dep.Version).Scan(&raw, &checksum, &source, &project)
			if e != nil {
				if e == sql.ErrNoRows {
					return codedError("COGNITION_REVIEW_STALE")
				}
				return e
			}
			if !strings.HasPrefix(source, "reviewed:") || checksum != dep.Checksum || cognitionChecksum(raw) != checksum || (parent.Scope == "user" && project != "") || (project != "" && project != parent.WorkspaceID) {
				return codedError("COGNITION_REVIEW_STALE")
			}
			var child cognitionSkillInput
			if json.Unmarshal([]byte(raw), &child) != nil || normalizeCognitionSkill(&child) != nil || child.WorkspaceID != project {
				return codedError("COGNITION_REVIEW_STALE")
			}
			visiting[dep.ID] = true
			if e := walk(child, depth+1); e != nil {
				return e
			}
			delete(visiting, dep.ID)
			union = append(union, child.AllowedTools...)
			supported = append(supported, child.Execution...)
		}
		for _, tool := range parent.AllowedTools {
			if !slices.Contains(union, tool) {
				return codedError("COGNITION_REVIEW_STALE")
			}
		}
		for _, step := range parent.Execution {
			if !slices.Contains(supported, step) {
				return codedError("COGNITION_REVIEW_STALE")
			}
		}
		return nil
	}
	return walk(in, 0)
}
func (s *Service) composeCognitionSkills(payload string) (cognitionSkill, error) {
	var in struct {
		Scope        string                     `json:"scope"`
		WorkspaceID  string                     `json:"workspaceId"`
		Slug         string                     `json:"slug"`
		Name         string                     `json:"name"`
		Description  string                     `json:"description"`
		Triggers     []string                   `json:"triggers"`
		Dependencies []cognitionSkillDependency `json:"dependencies"`
	}
	if e := decode(payload, &in); e != nil {
		return cognitionSkill{}, e
	}
	if len(in.Dependencies) == 0 || len(in.Dependencies) > 8 || !validCognitionScope(in.Scope, in.WorkspaceID, in.Slug) {
		return cognitionSkill{}, codedError("COGNITION_INVALID_SKILL")
	}
	if in.WorkspaceID != "" {
		if _, e := s.workspace(in.WorkspaceID); e != nil {
			return cognitionSkill{}, e
		}
	}
	s.cognitionMu.Lock()
	defer s.cognitionMu.Unlock()
	manifest := cognitionSkillInput{Scope: in.Scope, WorkspaceID: in.WorkspaceID, Slug: in.Slug, Name: in.Name, Description: in.Description, Triggers: in.Triggers, Dependencies: in.Dependencies, Instructions: "Use these reviewed workflows in dependency order, preserving every required approval.", Limitations: "Instructional composition. A registered Go test step can be scheduled locally after a verified Go edit; other workflow steps remain reference guidance."}
	e := s.transaction(func(tx *sql.Tx) error {
		if e := validateCognitionDependencies(tx, manifest); e != nil {
			return e
		}
		for _, dep := range manifest.Dependencies {
			var raw string
			if e := tx.QueryRow("SELECT manifest FROM skill_versions WHERE package_id=? AND version=?", dep.ID, dep.Version).Scan(&raw); e != nil {
				return e
			}
			var child cognitionSkillInput
			if e := json.Unmarshal([]byte(raw), &child); e != nil {
				return e
			}
			manifest.Instructions += "\n" + child.Name + ": " + child.Instructions
			manifest.Workflow = append(manifest.Workflow, child.Workflow...)
			manifest.AllowedTools = append(manifest.AllowedTools, child.AllowedTools...)
			for _, step := range child.Execution {
				if !slices.Contains(manifest.Execution, step) {
					manifest.Execution = append(manifest.Execution, step)
				}
			}
		}
		slices.Sort(manifest.AllowedTools)
		manifest.AllowedTools = slices.Compact(manifest.AllowedTools)
		if e := normalizeCognitionSkill(&manifest); e != nil {
			return e
		}
		return saveCognitionSkill(tx, manifest, "reviewed:composition")
	})
	if e != nil {
		return cognitionSkill{}, e
	}
	skills, e := s.cognitionSkills(in.WorkspaceID)
	for _, skill := range skills {
		if skill.Scope == in.Scope && skill.Slug == in.Slug {
			return skill, e
		}
	}
	if e == nil {
		e = codedError("NOT_FOUND")
	}
	return cognitionSkill{}, e
}
