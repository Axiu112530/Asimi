package developer

import (
	"path/filepath"
	"regexp"
	"strings"
)

const (
	agentRedactedValue    = "[REDACTED]"
	agentSecretName       = `\b[\w.-]*(?:api[_-]?key|access[_-]?key|account[_-]?key|auth[_-]?token|access[_-]?token|refresh[_-]?token|session[_-]?token|private[_-]?key|signing[_-]?key|password|passwd|passphrase|secret|connection[_-]?string|database[_-]?(?:url|dsn)|authorization|token)[\w.-]*["']?`
	agentSecretAssignment = agentSecretName + `(?:[ \t]+[\w.\[\]*]+)?[ \t]*[:=][ \t]*`
)

var (
	agentPrivateKey       = regexp.MustCompile(`(?s)-----BEGIN [^-\r\n]*PRIVATE KEY-----.*?(?:-----END [^-\r\n]*PRIVATE KEY-----|$)`)
	agentMultilineSecrets = []*regexp.Regexp{
		regexp.MustCompile(`(?is)(` + agentSecretAssignment + `)""".*?(?:"""|$)`),
		regexp.MustCompile(`(?is)(` + agentSecretAssignment + `)'''.*?(?:'''|$)`),
		regexp.MustCompile(`(?is)(` + agentSecretAssignment + `)` + "`" + `.*?(?:` + "`" + `|$)`),
		regexp.MustCompile(`(?i)(` + agentSecretAssignment + `)[|>][-+]?[ \t]*\r?\n(?:[ \t]+[^\r\n]*(?:\r?\n|$))+`),
	}
	agentSecretLine       = regexp.MustCompile(`(?i)(` + agentSecretAssignment + `)(?:\r?\n[ \t]*)?[^\r\n]*`)
	agentInlineCredential = regexp.MustCompile(`(?i)\b(bearer|token|password|passwd)[ \t]+["']?[^\s"';,]+`)
	agentCredentialURL    = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^\s/"'@]+:[^\s/"'@]*@`)
	agentKnownToken       = regexp.MustCompile(`\b(?:sk-(?:proj-)?[A-Za-z0-9_-]{12,}|gh[pousr]_[A-Za-z0-9_]{16,}|github_pat_[A-Za-z0-9_]{16,}|AKIA[A-Z0-9]{16}|ASIA[A-Z0-9]{16}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,})\b`)
)

// RedactAgentText removes common credential formats before text is clipped,
// persisted in an agent review, or sent to a provider.
func RedactAgentText(text string) string {
	text = agentPrivateKey.ReplaceAllString(text, agentRedactedValue)
	for _, pattern := range agentMultilineSecrets {
		text = pattern.ReplaceAllString(text, "${1}"+agentRedactedValue)
	}
	text = agentSecretLine.ReplaceAllString(text, "${1}"+agentRedactedValue)
	text = agentInlineCredential.ReplaceAllString(text, "${1} "+agentRedactedValue)
	text = agentCredentialURL.ReplaceAllString(text, "${1}"+agentRedactedValue+"@")
	return agentKnownToken.ReplaceAllString(text, agentRedactedValue)
}

// RedactAgentWorkspaceText also removes the local workspace path, including
// case variants and paths in escaped output.
func RedactAgentWorkspaceText(root, text string) string {
	return RedactAgentText(rootRedactor(root)(text))
}

func rootRedactor(root string) func(string) string {
	if root == "" {
		return func(text string) string { return text }
	}
	escaped := regexp.QuoteMeta(strings.ReplaceAll(root, `\`, `\\`))
	native := regexp.QuoteMeta(root)
	slash := regexp.QuoteMeta(filepath.ToSlash(root))
	pattern := regexp.MustCompile("(?i)(?:" + escaped + "|" + native + "|" + slash + ")")
	return func(text string) string {
		return pattern.ReplaceAllString(text, "[WORKSPACE]")
	}
}
