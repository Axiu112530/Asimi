package app

import (
	"encoding/json"
	"testing"
)

func TestAgent02CapturedDeepSeekSingleEffectPlan(t *testing.T) {
	// Public generated fixture response captured from one authorized real request.
	const captured = `{"objective":"Fix the incorrect Add function in calc.go so it returns a+b, keeping the public API unchanged, then run the Go tests.","assumptions":["calc.go is a small editable file with the full source already provided","The test in calc_test.go expects Add(2,3)==5"],"requiredCapabilities":["workspace","command"],"steps":[{"type":"PROPOSE_FILE_CHANGE","path":"calc.go","baseSha256":"6f13dad15cf4ac4763fc3262f8e59b1de4c6e888c2371e9acc025ed1a4fd2458","proposedContent":"package calc\nfunc Add(a,b int) int{return a+b}\n","summary":"Change Add to perform addition (a+b) instead of subtraction, preserving the exported signature."}],"expectedResults":["calc.go contains Add(a,b int) int{return a+b}","Existing tests pass after patch"],"riskLevel":"write"}`
	plan, err := parseAgent02Plan(captured)
	if err != nil || len(plan.Steps) != 1 || plan.Steps[0].Type != "PROPOSE_FILE_CHANGE" || plan.Steps[0].ProposedContent == nil || *plan.Steps[0].ProposedContent != "package calc\nfunc Add(a,b int) int{return a+b}\n" {
		t.Fatalf("valid one-effect captured plan rejected: %v", err)
	}
	for name, mutate := range map[string]func(*agent02Plan){
		"second effect": func(p *agent02Plan) {
			p.Steps = append(p.Steps, agent02PlanStep{Type: "PROPOSE_COMMAND", CommandID: "go.test", Args: []string{"./..."}})
		},
		"unverified finalization after proposal": func(p *agent02Plan) {
			p.Steps = append(p.Steps, agent02PlanStep{Type: "FINALIZE", Summary: "already complete"})
		},
		"undeclared tool capability": func(p *agent02Plan) { p.RequiredCapabilities = append(p.RequiredCapabilities, "file.change") },
	} {
		t.Run(name, func(t *testing.T) {
			var p agent02Plan
			if err := json.Unmarshal([]byte(captured), &p); err != nil {
				t.Fatal(err)
			}
			mutate(&p)
			if _, err := parseAgent02Plan(encode(p)); errorCode(err) != "AGENT_PLAN_INVALID" {
				t.Fatal("unsafe mutated plan was admitted", err)
			}
		})
	}
}
