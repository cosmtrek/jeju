package tests

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
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
	if provider.EnvKey != "DEEPSEEK_API_KEY" {
		t.Fatalf("expected env_key, got %q", provider.EnvKey)
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
	prompt := agent.SystemPrompt()
	if !strings.Contains(prompt, `"name":"keyword_count"`) || !strings.Contains(prompt, `"input_schema"`) {
		t.Fatalf("system prompt does not include custom tool schema:\n%s", prompt)
	}

	tool, ok := agent.Tools.Get("keyword_count")
	if !ok {
		t.Fatal("custom keyword_count tool missing")
	}
	result, err := tool.Run(context.Background(), json.RawMessage(`{"text":"Jeju uses tools. Jeju records tools.","keyword":"Jeju"}`))
	if err != nil {
		t.Fatalf("keyword_count tool failed: %v", err)
	}
	var output struct {
		Keyword string `json:"keyword"`
		Count   int    `json:"count"`
	}
	if err := json.Unmarshal([]byte(result.Output), &output); err != nil {
		t.Fatalf("unmarshal keyword_count output failed: %v", err)
	}
	if output.Keyword != "Jeju" || output.Count != 2 {
		t.Fatalf("unexpected keyword_count output: %+v", output)
	}
}
