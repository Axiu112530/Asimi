package app

import (
	"encoding/json"
	"strings"
	"time"

	"asimi/internal/ai"
	"asimi/internal/command"
	"asimi/internal/configuration"
	"asimi/internal/developer"
)

type agent02Preview struct {
	TaskID            string   `json:"taskId"`
	Provider          string   `json:"provider"`
	Endpoint          string   `json:"endpoint"`
	Model             string   `json:"model"`
	Files             []string `json:"files"`
	Content           string   `json:"content"`
	Hash              string   `json:"hash"`
	MaxRequestUSD     float64  `json:"maxRequestUsd"`
	RemainingUSD      float64  `json:"remainingUsd"`
	RemainingRequests int      `json:"remainingRequests"`
	profile           ai.Profile
	model             ai.Model
	language          string
	project           developer.AgentContext
	task              AgentTask
	workspace         Workspace
	limits            configuration.AgentLimits
}

type agent02Observation struct {
	Kind     string `json:"kind"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
	Summary  string `json:"summary,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

func (s *Service) previewAgent02(id string) (agent02Preview, error) {
	s.mvp.mu.Lock()
	t, err := loadAgentTask(s.db, id)
	s.mvp.mu.Unlock()
	if err != nil {
		return agent02Preview{}, err
	}
	if t.Agent == nil || t.Status != "NEEDS_REVIEW" {
		return agent02Preview{}, codedError("TASK_STATE")
	}
	config, err := configuration.Resolve(configuration.Preferences{Agent: t.Agent.Limits})
	if err != nil {
		return agent02Preview{}, err
	}
	limits := config.Agent
	if len(t.Agent.Rounds) >= limits.Requests || !t.Agent.Deadline.After(time.Now()) {
		return agent02Preview{}, codedError("AGENT_BUDGET_EXHAUSTED")
	}
	profile, model, language, err := s.agent02Provider(t.Agent.ProviderID, t.Agent.ModelID, t.Goal)
	if err != nil {
		return agent02Preview{}, err
	}
	workspace, err := s.workspace(t.WorkspaceID)
	if err != nil {
		return agent02Preview{}, err
	}
	if err = agent02CheckRoot(t, workspace.Root); err != nil {
		return agent02Preview{}, err
	}
	project, err := developer.BuildAgentContextWithExclusions(workspace.Root, t.Goal, t.Agent.Paths, t.Agent.Exclusions)
	if err != nil {
		return agent02Preview{}, err
	}
	if err = agent02CheckRoot(t, workspace.Root); err != nil {
		return agent02Preview{}, err
	}
	observations := s.agent02Observations(t, workspace.Root)
	commands := []string{}
	for _, spec := range command.Catalog() {
		if spec.AgentAllowed && !spec.RequiresDownload {
			commands = append(commands, spec.ID)
		}
	}
	content, err := json.Marshal(struct {
		Context          developer.AgentContext `json:"projectContext"`
		CommandTemplates []string               `json:"commandTemplates"`
		Observations     []agent02Observation   `json:"observations"`
		Round            int                    `json:"round"`
		RemainingRounds  int                    `json:"remainingRounds"`
		MaxSteps         int                    `json:"maxSteps"`
		FinalizeOnly     bool                   `json:"finalizeOnly"`
	}{project, commands, observations, len(t.Agent.Rounds) + 1, limits.Requests - len(t.Agent.Rounds), limits.StepsPerPlan, len(t.Agent.Rounds)+1 == limits.Requests})
	if err != nil {
		return agent02Preview{}, err
	}
	inputTokensUpperBound := len(content) + len(agent02Instruction) + 384
	if len(content) > limits.ContextBytes || model.Capabilities.ContextLimit != nil && int64(inputTokensUpperBound+limits.OutputTokens) > *model.Capabilities.ContextLimit {
		return agent02Preview{}, codedError("CONTEXT_LIMIT_EXCEEDED")
	}
	inputRate := model.Pricing.InputPerMillion
	if model.Pricing.CachedInputPerMillion != nil {
		inputRate = max(inputRate, *model.Pricing.CachedInputPerMillion)
	}
	reserve := (float64(inputTokensUpperBound)*inputRate + float64(limits.OutputTokens)*model.Pricing.OutputPerMillion) / 1_000_000
	remaining := limits.BudgetUSD - t.Agent.ReservedUSD
	if reserve > remaining {
		return agent02Preview{}, codedError("AGENT_BUDGET_EXHAUSTED")
	}
	p := agent02Preview{TaskID: t.ID, Provider: profile.Name, Endpoint: profile.BaseURL, Model: model.ID, Content: string(content), Files: []string{}, MaxRequestUSD: reserve, RemainingUSD: remaining, RemainingRequests: limits.Requests - len(t.Agent.Rounds), profile: profile, model: model, language: language, project: project, task: t, workspace: workspace, limits: limits}
	for _, file := range project.Files {
		p.Files = append(p.Files, file.Path)
	}
	// Consent binds the exact bytes, system instruction version, provider,
	// configured price and observed task/workspace version.
	p.Hash = hashControlText(encode([]any{agent02Policy, agent02Instruction, t.ID, t.UpdatedAt, t.Agent.BoundWorkspace, profile.ID, profile.Protocol, profile.BaseURL, profile.CredentialID, profile.HasCredential, model, language, limits, p.Content}))
	return p, nil
}

func agent02CheckRoot(t AgentTask, root string) error {
	identity, err := developer.RootIdentity(root)
	if err != nil {
		return err
	}
	if identity != t.Agent.BoundWorkspace {
		return codedError("STALE_WORKSPACE")
	}
	return nil
}

func (s *Service) agent02Observations(t AgentTask, root string) []agent02Observation {
	observations := []agent02Observation{}
	for _, step := range t.Steps[max(0, len(t.Steps)-12):] {
		entry := agent02Observation{Kind: step.Kind, Status: step.Status, Summary: boundedAgentText(step.Title, root, 450)}
		entry.Error = step.Error
		switch step.Kind {
		case "file":
			var witness struct {
				RelativePath string `json:"relativePath"`
				Verified     bool   `json:"verified"`
				AfterHash    string `json:"afterHash"`
			}
			if json.Unmarshal(step.Result, &witness) == nil {
				entry.Evidence = encode(witness)
				if witness.Verified && step.Status == "COMPLETED" {
					entry.Summary = "The approved working-copy change was already applied and independently verified."
				}
			}
		case "command":
			var result struct {
				ExitCode   int    `json:"exitCode"`
				Output     string `json:"output"`
				Stdout     string `json:"stdout"`
				Stderr     string `json:"stderr"`
				Cancelled  bool   `json:"cancelled"`
				TimedOut   bool   `json:"timedOut"`
				DurationMS int64  `json:"durationMs"`
				Code       string `json:"code"`
			}
			if json.Unmarshal(step.Result, &result) == nil {
				if result.Stdout != "" || result.Stderr != "" {
					result.Output = ""
				}
				result.Output = boundedAgentText(result.Output, root, 2000)
				result.Stdout = boundedAgentText(result.Stdout, root, 1000)
				result.Stderr = boundedAgentText(result.Stderr, root, 1000)
				entry.Evidence = encode(result)
			}
		case "agent.read", "agent.search", "agent.environment":
			entry.Evidence = boundedAgentText(string(step.Result), root, 1200)
		}
		observations = append(observations, entry)
	}
	// UIA and screen linkage exposes journal states, not pixels or application text.
	if evidence, err := s.computerTaskEvidence(t.ID); err == nil {
		for _, item := range evidence[max(0, len(evidence)-4):] {
			observations = append(observations, agent02Observation{Kind: item.Kind, Status: item.Status, Summary: item.ActionType})
		}
	}
	return observations
}

func boundedAgentText(value, root string, limit int) string {
	value = developer.RedactAgentWorkspaceText(root, value)
	if len(value) > limit {
		value = value[:limit] + " [truncated]"
	}
	return strings.ToValidUTF8(value, "�")
}
