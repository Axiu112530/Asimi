package app

import (
	"regexp"
	"strings"
)

var cognitionUnsafeSkill = regexp.MustCompile(`(?i)(?:ignore\s+(?:all\s+)?(?:previous|prior|system)\s+instructions|(?:bypass|disable|skip|forge)\s+(?:(?:the|required|user)\s+)*(?:approval|permissions?|safety)|(?:reveal|steal|exfiltrate|send)\s+(?:the\s+)?(?:api\s*key|credentials?|passwords?|tokens?)|(?:read|access)\s+(?:another|other)\s+(?:workspace|project)|<(?:/?system|/?developer)>|\[/?INST\]|(?:\.\.[/\\])|(?:^|\s)(?:powershell|pwsh|cmd(?:\.exe)?|bash|sh)\s+(?:-enc|-command|-c|/c)\b|(?:^|\s)(?:curl|wget|Invoke-WebRequest|Invoke-RestMethod)\s|(?:^|\s)(?:rm\s+-[a-z]*r[a-z]*f|Remove-Item\s+[^\r\n]*-Recurse|format\s+[a-z]:|diskpart\b)|忽略.{0,8}(?:系统指令|安全规则)|绕过.{0,8}(?:审批|权限)|窃取.{0,8}(?:密钥|凭据))`)

func validateCognitionSkillText(in cognitionSkillInput) error {
	if len(in.Workflow) > 12 {
		return codedError("COGNITION_INVALID_SKILL")
	}
	fields := []string{in.Name, in.Description, in.Instructions, in.Applicability, in.Prerequisites, in.ExpectedOutputs, in.Validation, in.Limitations}
	fields = append(fields, in.Triggers...)
	fields = append(fields, in.Workflow...)
	for _, value := range fields {
		if value == "" {
			continue
		}
		if !cognitionText(value, 3600) || strings.Contains(value, "```") || cognitionUnsafeSkill.MatchString(value) {
			return codedError("COGNITION_UNSAFE_SKILL")
		}
	}
	if in.SourceExperience != "" && (len(in.SourceExperience) != 32 || !validAgentSHA(in.SourceExperience+in.SourceExperience)) {
		return codedError("COGNITION_INVALID_SKILL")
	}
	return nil
}
