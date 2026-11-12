// Package configuration resolves preference data. It grants no tool permissions.
package configuration

import (
	"math"

	"deskpilot/internal/ai"
)

type Source string

const (
	Default     Source = "DEFAULT"
	Detected    Source = "DETECTED"
	Preset      Source = "PRESET"
	UserDefined Source = "USER_DEFINED"
)

type AgentLimits struct {
	Requests       int     `json:"requests"`
	StepsPerPlan   int     `json:"stepsPerPlan"`
	ContextBytes   int     `json:"contextBytes"`
	OutputTokens   int     `json:"outputTokens"`
	TimeoutSeconds int     `json:"timeoutSeconds"`
	RunSeconds     int     `json:"runSeconds"`
	BudgetUSD      float64 `json:"budgetUsd"`
}

func DefaultAgent() AgentLimits {
	return AgentLimits{Requests: 5, StepsPerPlan: 6, ContextBytes: 32000, OutputTokens: 1536, TimeoutSeconds: 60, RunSeconds: 900, BudgetUSD: 0.10}
}

type MediaLimits struct {
	Attachments int `json:"attachments"`
	ImageMiB    int `json:"imageMiB"`
}

const (
	MaxAttachments = 4
	MaxImageMiB    = 20
)

func DefaultMedia() MediaLimits {
	return MediaLimits{Attachments: MaxAttachments, ImageMiB: MaxImageMiB}
}

type Preferences struct {
	ai.Selection
	Agent *AgentLimits `json:"agentOverrides,omitempty"`
	Media *MediaLimits `json:"mediaOverrides,omitempty"`
}

type Resolved struct {
	Selection    ai.Selection      `json:"selection"`
	Agent        AgentLimits       `json:"agent"`
	AgentMaximum AgentLimits       `json:"agentMaximum"`
	Media        MediaLimits       `json:"media"`
	MediaMaximum MediaLimits       `json:"mediaMaximum"`
	Sources      map[string]Source `json:"sources"`
}

type Error string

func (e Error) Error() string { return string(e) }
func (e Error) Code() string  { return string(e) }

func Resolve(saved Preferences) (Resolved, error) {
	value := Resolved{Selection: saved.Selection, Sources: map[string]Source{}}
	valid := true
	resolveInt := func(key string, field *int, defaultValue, min, max int) {
		if *field == 0 {
			*field = defaultValue
			value.Sources[key] = Default
			return
		}
		if *field < min || *field > max {
			valid = false
		}
		value.Sources[key] = UserDefined
	}
	resolveInt("chat.maxOutputTokens", &value.Selection.MaxOutputTokens, 1024, 1, 131072)
	resolveInt("chat.maxContextBytes", &value.Selection.MaxContextBytes, 65536, 1024, 1048576)
	value.Sources["provider.selection"] = Default
	if value.Selection.DefaultProviderID != "" || value.Selection.DefaultModelID != "" {
		value.Sources["provider.selection"] = UserDefined
	}
	if threshold := value.Selection.WarningThreshold; threshold != nil && (*threshold < 0 || math.IsInf(*threshold, 0) || math.IsNaN(*threshold)) {
		valid = false
	}
	if saved.Agent != nil {
		value.Agent = *saved.Agent
	}
	limits := &value.Agent
	caps := DefaultAgent()
	value.AgentMaximum = caps
	resolveInt("agent.requests", &limits.Requests, caps.Requests, 1, caps.Requests)
	resolveInt("agent.stepsPerPlan", &limits.StepsPerPlan, caps.StepsPerPlan, 1, caps.StepsPerPlan)
	resolveInt("agent.contextBytes", &limits.ContextBytes, caps.ContextBytes, 1024, caps.ContextBytes)
	resolveInt("agent.outputTokens", &limits.OutputTokens, caps.OutputTokens, 128, caps.OutputTokens)
	resolveInt("agent.timeoutSeconds", &limits.TimeoutSeconds, caps.TimeoutSeconds, 5, caps.TimeoutSeconds)
	resolveInt("agent.runSeconds", &limits.RunSeconds, caps.RunSeconds, 60, caps.RunSeconds)
	if limits.BudgetUSD == 0 {
		limits.BudgetUSD = caps.BudgetUSD
		value.Sources["agent.budgetUsd"] = Default
	} else {
		if !(limits.BudgetUSD > 0 && limits.BudgetUSD <= caps.BudgetUSD) {
			valid = false
		}
		value.Sources["agent.budgetUsd"] = UserDefined
	}
	value.Sources["model.capabilities"] = value.Sources["provider.selection"]
	value.MediaMaximum = DefaultMedia()
	if saved.Media != nil {
		value.Media = *saved.Media
	}
	resolveInt("media.attachments", &value.Media.Attachments, value.MediaMaximum.Attachments, 1, value.MediaMaximum.Attachments)
	resolveInt("media.imageMiB", &value.Media.ImageMiB, value.MediaMaximum.ImageMiB, 1, value.MediaMaximum.ImageMiB)
	value.Sources["workspace.requirements"] = Detected
	value.Sources["runtime.discovery"] = Detected
	value.Sources["command.templates"] = Default
	value.Sources["ui.language"] = Default
	value.Sources["ui.theme"] = Default
	if !valid {
		return Resolved{}, Error("CONFIGURATION_INVALID")
	}
	return value, nil
}
