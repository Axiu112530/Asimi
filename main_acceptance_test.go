package main

import "testing"

func TestPresetAcceptancePayloadMatchesCurrentForm(t *testing.T) {
	for _, item := range []struct {
		name, payload string
		want          bool
	}{
		{"legacy", `{"presetId":"deepseek","secret":"preset-native-acceptance-placeholder"}`, true},
		{"current model form", `{"presetId":"deepseek","modelId":"deepseek-flash","secret":"preset-native-acceptance-placeholder"}`, true},
		{"reordered JSON", `{"secret":"preset-native-acceptance-placeholder","modelId":"deepseek-flash","presetId":"deepseek"}`, true},
		{"different model", `{"presetId":"deepseek","modelId":"other","secret":"preset-native-acceptance-placeholder"}`, false},
		{"different provider", `{"presetId":"openai","secret":"preset-native-acceptance-placeholder"}`, false},
		{"real form secret", `{"presetId":"deepseek","secret":"not-the-test-placeholder"}`, false},
		{"unknown field", `{"presetId":"deepseek","secret":"preset-native-acceptance-placeholder","extra":true}`, false},
		{"trailing JSON", `{"presetId":"deepseek","secret":"preset-native-acceptance-placeholder"}{}`, false},
	} {
		t.Run(item.name, func(t *testing.T) {
			if got := isPresetAcceptanceRequest(item.payload); got != item.want {
				t.Fatalf("acceptance hook match = %v; want %v", got, item.want)
			}
		})
	}
}
