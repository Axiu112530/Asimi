package app

import "asimi/internal/ai"

type presetOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func providerPresets() []presetPreset {
	return []presetPreset{
		presetDeepSeek(),
		// China-region API keys for Moonshot, BigModel, DashScope and Tencent TokenHub.
		chatPreset("kimi", "Kimi", "https://api.moonshot.cn/v1", presetModel("kimi-k3", true)),
		chatPreset("glm", "GLM", "https://open.bigmodel.cn/api/paas/v4",
			presetModel("glm-4.6", false), presetModel("glm-4.6v", true)),
		chatPreset("qwen", "Qwen", "https://dashscope.aliyuncs.com/compatible-mode/v1",
			presetModel("qwen-plus", false), presetModel("qwen3-vl-plus", true)),
		chatPreset("hy", "Tencent Hy", "https://tokenhub.tencentmaas.com/v1", presetModel("hy4", true)),
		chatPreset("openai", "OpenAI", "https://api.openai.com/v1", presetModel("gpt-4.1-mini", true)),
		chatPreset("claude", "Claude", "https://api.anthropic.com/v1", presetModel("claude-haiku-4-5", true)),
		chatPreset("gemini", "Gemini", "https://generativelanguage.googleapis.com/v1beta/openai", presetModel("gemini-2.5-flash-lite", true)),
	}
}

func providerPreset(id string) (presetPreset, bool) {
	if id == "" {
		id = presetDeepSeekID
	}
	for _, preset := range providerPresets() {
		if preset.ID == id {
			return preset, true
		}
	}
	return presetPreset{}, false
}

func chatPreset(id, name, baseURL string, models ...ai.Model) presetPreset {
	return presetPreset{ID: id, Profile: ai.Profile{
		Name: name, BaseURL: baseURL, Protocol: ai.ChatCompletions, Enabled: true, Models: models,
	}}
}

func presetModel(id string, vision bool) ai.Model {
	caps := ai.Capabilities{
		Text: ai.Supported, Vision: ai.Unsupported, Streaming: ai.Supported,
		ToolCalling: ai.Unsupported, ComputerUse: ai.Unsupported,
		StructuredOutput: ai.Unknown, Temperature: ai.Unknown,
		ImageDetails: []string{"auto"},
	}
	if vision {
		caps.Vision = ai.Supported
	}
	return ai.Model{ID: id, DisplayName: id, Capabilities: caps}
}
