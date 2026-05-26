package tests

import (
	"path/filepath"
	"testing"

	"jeju/internal/compiler"
	"jeju/internal/config"
)

func TestDeepSeekFixtureConfigCompiles(t *testing.T) {
	tmp := t.TempDir()
	workdir := filepath.Join(tmp, "deepseek-agent")
	copyDir(t, fixturePath(t, "deepseek-agent"), workdir)

	restoreCWD := chdir(t, workdir)
	defer restoreCWD()

	manifest, _, err := config.LoadFile("agents/deepseek.agent.yaml")
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if err := config.Validate(manifest); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	provider := manifest.Models.Providers["primary"]
	if provider.Provider != "deepseek" {
		t.Fatalf("expected deepseek provider, got %q", provider.Provider)
	}
	if provider.Model != "deepseek-v4-flash" {
		t.Fatalf("expected deepseek-v4-flash, got %q", provider.Model)
	}
	if provider.BaseURL != "https://api.deepseek.com" {
		t.Fatalf("unexpected base_url %q", provider.BaseURL)
	}
	if provider.EnvKey != "DEEPSEEK_API_KEY" || provider.APIKeyEnv != "DEEPSEEK_API_KEY" {
		t.Fatalf("env_key/api_key_env not normalized: env_key=%q api_key_env=%q", provider.EnvKey, provider.APIKeyEnv)
	}

	agent, err := compiler.Compile("agents/deepseek.agent.yaml")
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	_, compiledProvider, ok := agent.Models.Get("primary")
	if !ok {
		t.Fatal("compiled primary provider missing")
	}
	if !compiledProvider.JSONMode {
		t.Fatal("deepseek provider should enable JSON mode")
	}
}
