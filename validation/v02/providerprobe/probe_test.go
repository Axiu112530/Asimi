package providerprobe

import "testing"

func TestRunRequiresProcessCredential(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")

	report, err := Run(t.Context())
	if err == nil || report.Status != "PENDING" || report.ErrorCode != "CREDENTIAL_MISSING" ||
		report.Passed || report.ExternalCalls != 0 || len(report.Results) != 0 {
		t.Fatalf("missing credential should leave acceptance pending without dispatch: %#v, %v", report, err)
	}
}
