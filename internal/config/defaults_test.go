package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestApplyDefaultsKeepsOnlyEnvKeyForDeepSeek(t *testing.T) {
	manifest := &AgentManifest{
		Models: ModelsConfig{
			Default: "primary",
			Providers: map[string]ModelConfig{
				"primary": {
					Provider: "deepseek",
					Model:    "deepseek-v4-flash",
					EnvKey:   "DEEPSEEK_API_KEY",
				},
			},
		},
	}

	ApplyDefaults(manifest)
	provider := manifest.Models.Providers["primary"]
	if provider.EnvKey != "DEEPSEEK_API_KEY" {
		t.Fatalf("unexpected env_key %q", provider.EnvKey)
	}
	snapshot, err := yaml.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if strings.Contains(string(snapshot), "api_key_env") {
		t.Fatalf("snapshot should not include api_key_env:\n%s", snapshot)
	}
}
