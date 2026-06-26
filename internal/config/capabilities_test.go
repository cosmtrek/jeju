package config

import "testing"

func TestSupportedCapabilities(t *testing.T) {
	caps := SupportedCapabilities()
	requireContains(t, caps.ModelProviderTypes, "mock")
	requireContains(t, caps.ModelProviderTypes, "openaiCompatible")
	requireContains(t, caps.ModelPresets, "deepseek")
	requireContains(t, caps.ModelPresets, "mimo")
	requireNotContains(t, caps.ModelPresets, "")
	requireContains(t, caps.ToolUses, "builtin:search")
	requireContains(t, caps.ToolUses, "command")
	requireContains(t, caps.ToolUses, "agent")
	requireContains(t, caps.EvaluatorUses, "rules")
	requireContains(t, caps.EvaluatorUses, "llm")
	requireContains(t, caps.TrajectoryFormats, "jeju-jsonl")
}

func requireContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("expected %q in %v", want, values)
}

func requireNotContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			t.Fatalf("did not expect %q in %v", want, values)
		}
	}
}
