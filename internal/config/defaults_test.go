package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestApplyDefaultsKeepsOnlyEnvKeyForNamedProviders(t *testing.T) {
	for _, tc := range []struct {
		preset  string
		model   string
		envKey  string
		baseURL string
	}{
		{preset: "deepseek", model: "deepseek-v4-flash", envKey: "DEEPSEEK_API_KEY", baseURL: "https://api.deepseek.com"},
		{preset: "mimo", model: "mimo-v2.5-pro", envKey: "MIMO_API_KEY", baseURL: "https://api.xiaomimimo.com/v1"},
	} {
		t.Run(tc.preset, func(t *testing.T) {
			manifest := &AgentManifest{
				Models: ModelsConfig{
					Providers: map[string]ModelConfig{
						"primary": {
							Preset: tc.preset,
							Model:  tc.model,
							EnvKey: tc.envKey,
						},
					},
				},
			}

			ApplyDefaults(manifest)
			provider := manifest.Models.Providers["primary"]
			if provider.EnvKey != tc.envKey {
				t.Fatalf("unexpected envKey %q", provider.EnvKey)
			}
			if provider.BaseURL != tc.baseURL {
				t.Fatalf("unexpected baseUrl %q", provider.BaseURL)
			}
			if provider.Thinking.Type != "disabled" {
				t.Fatalf("expected thinking disabled for %s preset, got %q", tc.preset, provider.Thinking.Type)
			}
			if provider.ContextWindow != 128000 {
				t.Fatalf("expected default context window for %s preset, got %d", tc.preset, provider.ContextWindow)
			}
			if manifest.Runtime.CompressionThreshold != 0.8 {
				t.Fatalf("expected default compression threshold, got %v", manifest.Runtime.CompressionThreshold)
			}
			snapshot, err := yaml.Marshal(manifest)
			if err != nil {
				t.Fatalf("Marshal failed: %v", err)
			}
			if strings.Contains(string(snapshot), "apiKeyEnv") {
				t.Fatalf("snapshot should not include apiKeyEnv:\n%s", snapshot)
			}
		})
	}
}
