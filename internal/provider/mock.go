// Package provider defines neutral model contracts. The offline Mock has no OS
// or network authority: it can only propose one typed file-create request.
package provider

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"unicode"

	"asimi/internal/domain"
)

type Draft struct {
	FileName string  `json:"fileName"`
	Content  *string `json:"content"`
}

type Request struct {
	Input         string
	Messages      []domain.Message
	AppLanguage   string
	AgentLanguage string
	Draft         *Draft
}

type Result struct {
	Text     string
	Intent   string
	Draft    *Draft
	Language string
}

type Provider interface {
	Generate(context.Context, Request) (Result, error)
	Summarize(context.Context, SummaryRequest) (Result, error)
}
type Mock struct{}

// SummaryRequest carries only the independently checked tool outcome. It does
// not grant a provider permission to execute or decide task success.
type SummaryRequest struct {
	Language     string
	Verification domain.Verification
	ErrorCode    string
}

func (Mock) Summarize(ctx context.Context, req SummaryRequest) (Result, error) {
	if e := ctx.Err(); e != nil {
		return Result{}, e
	}
	var en, zh string
	if req.ErrorCode == "" && req.Verification.Passed && req.Verification.Created && req.Verification.ContentMatched {
		en = "The file was created and independently verified. Its content, byte count and SHA-256 match the approved operation."
		zh = "文件已创建并通过独立验证。内容、字节数及 SHA-256 均与审批操作一致。"
	} else if req.Verification.Created {
		en = "A file was committed, but the task could not be verified (" + req.ErrorCode + "). The file was preserved. Review the verification result."
		zh = "文件已提交，但任务未能通过验证（" + req.ErrorCode + "）。文件已保留，请查看验证结果。"
	} else {
		en = "The file was not created (" + req.ErrorCode + ")."
		zh = "文件未创建（" + req.ErrorCode + "）。"
	}
	return Result{Intent: "SUMMARY", Text: Text(req.Language, en, zh), Language: req.Language}, nil
}

var quoted = regexp.MustCompile("\"([^\"]*)\"|'([^']*)'|“([^”]*)”|‘([^’]*)’|「([^」]*)」")
var nameLabel = regexp.MustCompile(`(?i)^(?:file\s*name|filename|name|文件名|名称)\s*(?:is|为|是|[:：=])?\s*$`)
var contentLabel = regexp.MustCompile(`(?i)^(?:contents?|内容)\s*(?:is|为|是|[:：=])?\s*$`)
var languageSuffix = regexp.MustCompile(`(?i)^(?:(?:please\s+)?(?:reply|respond)\s+in\s+(?:english|chinese)(?:\s+please)?|请?用(?:中文|英文|英语|简体中文)(?:回复|回答)?)$`)

func quoteValue(match []string) string {
	for _, value := range match[1:] {
		if value != "" {
			return value
		}
	}
	return ""
}

// A pending draft gives no authority to reinterpret an unrelated command as
// file content. Continuations have a deliberately small, field-aware grammar.
func continuation(input string, d Draft) (Draft, bool) {
	spans := quoted.FindAllStringIndex(input, -1)
	values := quoted.FindAllStringSubmatch(input, -1)
	if len(spans) == 0 || len(spans) > 2 {
		return d, false
	}
	previous := 0
	seenName, seenContent := false, false
	for i, span := range spans {
		label := strings.Trim(strings.TrimSpace(input[previous:span[0]]), " ,，;；:：")
		value := quoteValue(values[i])
		switch {
		case nameLabel.MatchString(label):
			if seenName {
				return d, false
			}
			d.FileName = value
			seenName = true
		case contentLabel.MatchString(label):
			if seenContent {
				return d, false
			}
			v := value
			d.Content = &v
			seenContent = true
		case label == "" && len(spans) == 1:
			if d.FileName == "" {
				d.FileName = value
			} else if d.Content == nil {
				v := value
				d.Content = &v
			} else {
				return d, false
			}
		default:
			return d, false
		}
		previous = span[1]
	}
	tail := strings.Trim(strings.TrimSpace(input[previous:]), " ,，;；.。!！")
	return d, tail == "" || languageSuffix.MatchString(tail)
}

func language(req Request) string {
	lower := strings.ToLower(quoted.ReplaceAllString(req.Input, ""))
	if strings.Contains(lower, "reply in english") || strings.Contains(lower, "respond in english") || strings.Contains(lower, "in english please") || strings.Contains(lower, "用英语") || strings.Contains(lower, "用英文") {
		return "en-US"
	}
	if strings.Contains(lower, "reply in chinese") || strings.Contains(lower, "respond in chinese") || strings.Contains(lower, "用中文") || strings.Contains(lower, "用简体中文") {
		return "zh-CN"
	}
	if req.AgentLanguage == "en-US" || req.AgentLanguage == "zh-CN" {
		return req.AgentLanguage
	}
	if req.AgentLanguage == "auto" {
		for _, r := range req.Input {
			if unicode.Is(unicode.Han, r) {
				return "zh-CN"
			}
		}
		return "en-US"
	}
	if req.AppLanguage == "zh-CN" {
		return "zh-CN"
	}
	return "en-US"
}

func Text(lang, en, zh string) string {
	if lang == "zh-CN" {
		return zh
	}
	return en
}

func (Mock) Generate(ctx context.Context, req Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	lang := language(req)
	answer := func(intent, en, zh string, d *Draft) (Result, error) {
		return Result{Text: Text(lang, en, zh), Intent: intent, Draft: d, Language: lang}, nil
	}
	lower := strings.ToLower(quoted.ReplaceAllString(req.Input, ""))
	// JSON is a guided, typed request, never an executable command or approval.
	var obj struct {
		FileName *string `json:"fileName"`
		Content  *string `json:"content"`
	}
	isJSON := json.Unmarshal([]byte(req.Input), &obj) == nil && (obj.FileName != nil || obj.Content != nil)
	isCreate := isJSON || strings.Contains(lower, "create") || strings.Contains(lower, "创建") || strings.Contains(lower, "新建") || strings.Contains(lower, "生成文件")
	if !isJSON && (strings.Contains(lower, "capabilit") || strings.Contains(lower, "what can you") || strings.Contains(lower, "available demo") || strings.Contains(lower, "有哪些") || strings.Contains(lower, "能做什么") || strings.Contains(lower, "能力")) {
		return answer("CAPABILITIES", "The offline Mock can propose one explicitly named file with exact content, explain the plan, and clarify missing details. Every file requires an exact approval and independent verification. Desktop control and external models are unavailable.", "离线 Mock 可以提议创建明确指定名称和内容的单个文件、解释计划并询问缺失信息。每个文件都需要准确审批和独立验证。桌面控制及外部模型不可用。", nil)
	}
	if !isCreate && req.Draft == nil {
		if strings.Contains(lower, "plan") || strings.Contains(lower, "计划") {
			return answer("EXPLAIN", "I validate the file name and size, prepare an exact path and SHA-256 approval, wait for your decision, create without overwriting, and independently read back the bytes and hash. No file is created by explaining this plan.", "我会验证文件名和大小，准备绑定准确路径与 SHA-256 的审批，等待你的决定，不覆盖已有文件地创建，然后独立重新读取并核对字节和哈希。解释计划不会创建文件。", nil)
		}
		if strings.Contains(lower, "capabilit") || strings.Contains(lower, "can you") || strings.Contains(lower, "支持") || strings.Contains(lower, "能做") || strings.Contains(lower, "能力") || strings.Contains(lower, "help") {
			return answer("CAPABILITIES", "I am the offline Mock Provider. I can propose one file with an explicit name and content in the isolated workspace, explain its plan, or ask for missing details. File creation requires your exact approval. I cannot control the desktop, run commands, browse files, or call external models.", "我是离线 Mock Provider。我可以在隔离工作目录中提议创建一个明确指定名称和内容的文件，解释计划，或询问缺失信息。创建文件需要你的准确审批。我不能控制桌面、执行命令、浏览文件或调用外部模型。", nil)
		}
		return answer("UNSUPPORTED", "This Phase 1 Mock supports only approved file creation, capability questions, and plan explanations. Try: Create file \"notes.txt\" with content \"Hello\", or send {\"fileName\":\"notes.txt\",\"content\":\"Hello\"}.", "Phase 1 Mock 仅支持经审批的文件创建、能力查询和计划解释。可以输入：创建文件“notes.txt”，内容为“你好”，或发送 {\"fileName\":\"notes.txt\",\"content\":\"你好\"}。", nil)
	}
	// An explanation/question mentioning creation is read-only; it must not turn
	// quoted examples into a tool proposal.
	if !isJSON && (strings.Contains(lower, "explain") || strings.Contains(lower, "解释") || strings.Contains(lower, "plan") || strings.Contains(lower, "计划") || strings.Contains(lower, "how would") || strings.Contains(lower, "如何")) {
		return answer("EXPLAIN", "The plan is: validate the exact file request, ask you to approve the path and content hash, create without replacement, then independently verify content, byte count and SHA-256. I will not execute an explanation.", "计划是：验证明确的文件请求，请你审批路径和内容哈希，不覆盖地创建文件，然后独立核对内容、字节数及 SHA-256。计划说明本身不会执行操作。", nil)
	}
	d := Draft{}
	if req.Draft != nil && (!isCreate || isJSON) {
		d = *req.Draft
	}
	if isJSON {
		if obj.FileName != nil {
			d.FileName = *obj.FileName
		}
		if obj.Content != nil {
			v := *obj.Content
			d.Content = &v
		}
	} else if req.Draft != nil && !isCreate {
		var ok bool
		d, ok = continuation(req.Input, d)
		if !ok {
			return answer("UNSUPPORTED", "This request does not continue the file details. The offline Mock cannot run commands or perform desktop actions. To continue, send a quoted value, a filename/content-labeled value, or the guided JSON form.", "此请求不是文件信息补充。离线 Mock 不能执行命令或操作桌面。若要继续，请发送带引号的值、明确标注文件名或内容的值，或引导式 JSON。", nil)
		}
	} else {
		matches := quoted.FindAllStringSubmatch(req.Input, -1)
		if len(matches) > 2 {
			return answer("UNSUPPORTED", "Only one file per request is supported. Provide exactly one quoted filename and one quoted content value, or use the guided JSON form.", "每个请求仅支持一个文件。请提供一个带引号的文件名和一个带引号的内容，或使用引导式 JSON。", nil)
		}
		values := []string{}
		for _, m := range matches {
			values = append(values, quoteValue(m))
		}
		if len(values) >= 2 {
			d.FileName = values[0]
			v := values[1]
			d.Content = &v
		} else if len(values) == 1 {
			if strings.Contains(lower, "content") || strings.Contains(lower, "内容") {
				v := values[0]
				d.Content = &v
			} else if d.FileName == "" {
				d.FileName = values[0]
			} else if d.Content == nil {
				v := values[0]
				d.Content = &v
			}
		} else if req.Draft != nil {
			// Continuation values still must be explicit: bare text cannot silently
			// become a filename or file contents.
		}
	}
	if strings.TrimSpace(d.FileName) == "" || d.Content == nil {
		return answer("CLARIFY", "Please provide the missing file name and exact content in quotes, or use JSON: {\"fileName\":\"notes.txt\",\"content\":\"Hello\"}. Empty content must be explicit as \"\". Nothing has been created.", "请用引号补充缺失的文件名和准确内容，或使用 JSON：{\"fileName\":\"notes.txt\",\"content\":\"你好\"}。空内容必须明确写为 \"\"。尚未创建任何文件。", &d)
	}
	return answer("CREATE", "I prepared a three-step file creation plan. Review the exact path, content, byte count and SHA-256 in the approval preview. I will create the file only after approval, then independently verify the result.", "我已准备三步文件创建计划。请在审批预览中核对准确路径、内容、字节数及 SHA-256。只有审批后才会创建文件，随后独立验证结果。", &d)
}

func Profiles() []domain.ProviderProfile {
	m := domain.Model{ProviderID: "mock", ID: "mock-local-v1", Text: "SUPPORTED", Vision: "UNSUPPORTED", ToolCalling: "SUPPORTED", Streaming: "UNSUPPORTED", ComputerUse: "UNSUPPORTED", StructuredOutput: "SUPPORTED", ContextLength: 0, PricingMetadata: map[string]any{"cost": 0, "currency": "USD", "source": "offline-mock"}}
	profiles := []domain.ProviderProfile{{ID: "mock", Name: "Mock Provider", Enabled: true, Status: "ENABLED", Models: []domain.Model{m}}}
	for _, p := range [][2]string{{"openai", "OpenAI"}, {"anthropic", "Anthropic"}, {"gemini", "Google Gemini"}, {"qwen", "Alibaba Qwen"}, {"deepseek", "DeepSeek"}, {"zhipu", "Zhipu"}, {"volcengine", "Volcengine"}, {"custom", "Custom OpenAI Compatible"}} {
		profiles = append(profiles, domain.ProviderProfile{ID: p[0], Name: p[1], Status: "NOT_CONNECTED", Models: []domain.Model{{ProviderID: p[0], ID: "not-configured", Text: "UNKNOWN", Vision: "UNKNOWN", ToolCalling: "UNKNOWN", Streaming: "UNKNOWN", ComputerUse: "UNKNOWN", StructuredOutput: "UNKNOWN", PricingMetadata: map[string]any{}}}})
	}
	return profiles
}
