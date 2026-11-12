package computer

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ValidateText restricts the semantic fixture editor to a short plain-text line.
func ValidateText(text string) bool {
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > 512 ||
		strings.TrimSpace(text) == "" {
		return false
	}
	for _, r := range text {
		if r != ' ' && !unicode.IsLetter(r) && !unicode.IsMark(r) && !unicode.IsNumber(r) &&
			!strings.ContainsRune(".,!?'-，。！？、", r) {
			return false
		}
	}
	lower := strings.ToLower(text)
	for _, word := range []string{"password", "passphrase", "credential", "api key", "token", "secret", "密码", "密钥"} {
		if strings.Contains(lower, word) {
			return false
		}
	}
	words := strings.FieldsFunc(lower, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) == 0 {
		return false
	}
	switch words[0] {
	case "cmd", "powershell", "pwsh", "bash", "sh", "wsl", "git", "reg", "net", "runas", "sudo", "shutdown", "curl", "wget", "del", "rm", "python", "node", "schtasks", "rundll32":
		return false
	}
	for _, word := range strings.Fields(text) {
		if len(word) < 24 {
			continue
		}
		ascii, digit := true, false
		for _, r := range word {
			ascii = ascii && r <= unicode.MaxASCII
			digit = digit || r >= '0' && r <= '9'
		}
		if ascii && digit {
			return false
		}
	}
	return true
}
