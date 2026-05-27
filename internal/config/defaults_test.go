package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestApplyDefaultsKeepsOnlyEnvKeyForNamedProviders(t *testing.T) {
	for _, tc := range []struct {
		provider string
		model    string
		envKey   string
		baseURL  string
	}{
		{provider: "deepseek", model: "deepseek-v4-flash", envKey: "DEEPSEEK_API_KEY", baseURL: "https://api.deepseek.com"},
		{provider: "mimo", model: "mimo-v2.5-pro", envKey: "MIMO_API_KEY", baseURL: "https://api.xiaomimimo.com/v1"},
	} {
		t.Run(tc.provider, func(t *testing.T) {
			manifest := &AgentManifest{
				Models: ModelsConfig{
					Default: "primary",
					Providers: map[string]ModelConfig{
						"primary": {
							Provider: tc.provider,
							Model:    tc.model,
							EnvKey:   tc.envKey,
						},
					},
				},
			}

			ApplyDefaults(manifest)
			provider := manifest.Models.Providers["primary"]
			if provider.EnvKey != tc.envKey {
				t.Fatalf("unexpected env_key %q", provider.EnvKey)
			}
			if provider.BaseURL != tc.baseURL {
				t.Fatalf("unexpected base_url %q", provider.BaseURL)
			}
			snapshot, err := yaml.Marshal(manifest)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			if strings.Contains(string(snapshot), "api_key_env") {
				t.Fatalf("snapshot should not include api_key_env:\n%s", snapshot)
			}
		})
	}
}
