package app

import (
	"strings"
	"testing"
)

func TestAgent02PlanRejectsUnknownFieldsAndMultipleEffects(t *testing.T) {
	base := `{"objective":"Fix arithmetic","assumptions":[],"requiredCapabilities":["workspace"],"expectedResults":["tests pass"],"riskLevel":"write","steps":STEPS}`
	valid := `{"type":"PROPOSE_FILE_CHANGE","path":"calc.go","baseSha256":"` + strings.Repeat("a", 64) + `","proposedContent":"package calc\n","summary":"correct calculation"}`
	for _, steps := range []string{
		`[{"type":"RUN_SHELL","commandId":"powershell","args":["anything"]}]`,
		`[` + valid + `,` + valid + `]`,
		`[{"type":"FINALIZE","summary":"done","approved":true}]`,
		`[{"type":"PROPOSE_COMMAND","commandId":"go.test","args":["./..."]},{"type":"FINALIZE","summary":"fake success"}]`,
	} {
		if _, err := parseAgent02Plan(strings.Replace(base, "STEPS", steps, 1)); err == nil {
			t.Fatalf("accepted %s", steps)
		}
	}
	if _, err := parseAgent02Plan(strings.Replace(base, "STEPS", `[`+valid+`]`, 1)); err != nil {
		t.Fatal(err)
	}
}
