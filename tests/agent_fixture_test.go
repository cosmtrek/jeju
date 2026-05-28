package tests

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"jeju/internal/cli"
	"jeju/internal/compiler"
	"jeju/internal/config"
	"jeju/internal/evaluate"
	"jeju/internal/runs"
	"jeju/internal/trajectory"
)

func TestAgentFixtures(t *testing.T) {
	t.Run("mock full run", func(t *testing.T) {
		tmp := t.TempDir()
		workdir := filepath.Join(tmp, "agent")
		copyDir(t, fixturePath(t, "agent"), workdir)

		restoreCWD := chdir(t, workdir)
		defer restoreCWD()

		ctx := context.Background()
		if err := cli.Execute(ctx, []string{"validate", "agents/agent.yaml"}); err != nil {
			t.Fatalf("validate failed: %v", err)
		}
		withStdin(t, "y\n", func() {
			if err := cli.Execute(ctx, []string{"run", "agents/agent.yaml", "write a brief AgentOps note and save it to notes.md"}); err != nil {
				t.Fatalf("run failed: %v", err)
			}
		})

		store := runs.NewStore("./runs")
		runList, err := store.ListRuns()
		if err != nil {
			t.Fatalf("list runs failed: %v", err)
		}
		if len(runList) != 1 {
			t.Fatalf("expected 1 run, got %d", len(runList))
		}
		runID := runList[0].RunID
		if err := cli.Execute(ctx, []string{"runs"}); err != nil {
			t.Fatalf("runs command failed: %v", err)
		}
		if err := cli.Execute(ctx, []string{"inspect", runID}); err != nil {
			t.Fatalf("inspect command failed: %v", err)
		}

		runDir := filepath.Join(workdir, "runs", runID)
		requireFile(t, filepath.Join(runDir, runs.MetadataFile))
		requireFile(t, filepath.Join(runDir, runs.ConfigSnapshotFile))
		requireFile(t, filepath.Join(runDir, runs.TrajectoryFile))
		requireFile(t, filepath.Join(runDir, runs.FinalFile))
		requireFile(t, filepath.Join(runDir, runs.EvaluationFile))
		requireFile(t, filepath.Join(workdir, "workspace", "agent", "notes.md"))

		events, err := trajectory.ReadFile(filepath.Join(runDir, runs.TrajectoryFile))
		if err != nil {
			t.Fatalf("read trajectory failed: %v", err)
		}
		requireEventTypes(t, events,
			trajectory.EventRunStarted,
			trajectory.EventSkillDisclosed,
			trajectory.EventSkillLoaded,
			trajectory.EventStepStarted,
			trajectory.EventModelStarted,
			trajectory.EventModelCompleted,
			trajectory.EventActionParsed,
			trajectory.EventToolRequested,
			trajectory.EventPermissionChecked,
			trajectory.EventPermissionApproved,
			trajectory.EventToolStarted,
			trajectory.EventToolCompleted,
			trajectory.EventEvaluationStarted,
			trajectory.EventEvaluationCompleted,
			trajectory.EventRunCompleted,
		)

		var result evaluate.Result
		data, err := os.ReadFile(filepath.Join(runDir, runs.EvaluationFile))
		if err != nil {
			t.Fatalf("read evaluation failed: %v", err)
		}
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("unmarshal evaluation failed: %v", err)
		}
		if !result.Passed || result.Score != 1 {
			t.Fatalf("expected passing evaluation score=1, got passed=%v score=%v", result.Passed, result.Score)
		}
	})

	for _, tc := range []struct {
		name    string
		model   string
		baseURL string
		envKey  string
	}{
		{name: "deepseek", model: "deepseek-v4-flash", baseURL: "https://api.deepseek.com", envKey: "DEEPSEEK_API_KEY"},
		{name: "mimo", model: "mimo-v2.5-pro", baseURL: "https://api.xiaomimimo.com/v1", envKey: "MIMO_API_KEY"},
	} {
		t.Run(tc.name+" provider config compiles", func(t *testing.T) {
			workdir := copyAgentFixtureWithProvider(t, tc.name, tc.model, tc.envKey)
			restoreCWD := chdir(t, workdir)
			defer restoreCWD()

			manifest, _, err := config.LoadFile("agents/agent.yaml")
			if err != nil {
				t.Fatalf("LoadFile failed: %v", err)
			}
			if err := config.Validate(manifest); err != nil {
				t.Fatalf("Validate failed: %v", err)
			}
			provider := manifest.Models.Providers["primary"]
			if provider.Preset != tc.name {
				t.Fatalf("expected %s preset, got %q", tc.name, provider.Preset)
			}
			if provider.Model != tc.model {
				t.Fatalf("expected %s, got %q", tc.model, provider.Model)
			}
			if provider.BaseURL != tc.baseURL {
				t.Fatalf("unexpected baseUrl %q", provider.BaseURL)
			}
			if provider.EnvKey != tc.envKey {
				t.Fatalf("expected envKey %s, got %q", tc.envKey, provider.EnvKey)
			}

			agent, err := compiler.Compile("agents/agent.yaml")
			if err != nil {
				t.Fatalf("Compile failed: %v", err)
			}
			_, compiledProvider, ok := agent.Models.Get("primary")
			if !ok {
				t.Fatal("compiled primary provider missing")
			}
			if !compiledProvider.JSONMode {
				t.Fatalf("%s provider should enable JSON mode", tc.name)
			}
			if !compiledProvider.ToolCalling {
				t.Fatalf("%s provider should enable native tool calling", tc.name)
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
		})
	}

	t.Run("deep research fixture compiles", func(t *testing.T) {
		tmp := t.TempDir()
		workdir := filepath.Join(tmp, "deep-research")
		copyDir(t, fixturePath(t, "deep-research"), workdir)

		restoreCWD := chdir(t, workdir)
		defer restoreCWD()

		manifest, _, err := config.LoadFile("agents/deep-research.yaml")
		if err != nil {
			t.Fatalf("LoadFile failed: %v", err)
		}
		if err := config.Validate(manifest); err != nil {
			t.Fatalf("Validate failed: %v", err)
		}
		provider := manifest.Models.Providers["primary"]
		if provider.Preset != "mimo" || provider.Model != "mimo-v2.5-pro" || provider.EnvKey != "MIMO_API_KEY" {
			t.Fatalf("unexpected provider config: %+v", provider)
		}

		agent, err := compiler.Compile("agents/deep-research.yaml")
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}
		if _, _, ok := agent.Models.Get("primary"); !ok {
			t.Fatal("compiled primary provider missing")
		}
		if _, ok := agent.Tools.Get("search_api"); !ok {
			t.Fatal("search_api tool missing")
		}
		if _, ok := agent.Tools.Get("write"); !ok {
			t.Fatal("write tool missing")
		}
		if _, ok := agent.Skills.Get("deep-research"); !ok {
			t.Fatal("deep-research skill missing")
		}
		prompt := agent.SystemPrompt()
		if !strings.Contains(prompt, `"name":"search_api"`) || !strings.Contains(prompt, "reports/deep-research.md") {
			t.Fatalf("system prompt does not include deep research tool/report instructions:\n%s", prompt)
		}
	})
}

func copyAgentFixtureWithProvider(t *testing.T, provider, model, envKey string) string {
	t.Helper()
	workdir := filepath.Join(t.TempDir(), provider+"-agent")
	copyDir(t, fixturePath(t, "agent"), workdir)

	manifestPath := filepath.Join(workdir, "agents", "agent.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest failed: %v", err)
	}
	manifest := string(data)
	manifest = strings.ReplaceAll(manifest, "type: mock", "type: openaiCompatible\n      preset: "+provider)
	manifest = strings.ReplaceAll(manifest, "model: mock-react", "model: "+model+"\n      envKey: "+envKey)
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest failed: %v", err)
	}
	return workdir
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "fixtures", name)
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	if err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	}); err != nil {
		t.Fatalf("copy fixture failed: %v", err)
	}
}

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd failed: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	return func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("restore cwd failed: %v", err)
		}
	}
}

func withStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	old := os.Stdin
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe failed: %v", err)
	}
	if _, err := writer.WriteString(input); err != nil {
		t.Fatalf("write stdin pipe failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdin writer failed: %v", err)
	}
	os.Stdin = reader
	defer func() {
		os.Stdin = old
		_ = reader.Close()
	}()
	fn()
}

func requireFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("expected file %s: %v", path, err)
	}
	if info.IsDir() {
		t.Fatalf("expected %s to be a file", path)
	}
}

func requireEventTypes(t *testing.T, events []trajectory.Event, types ...trajectory.EventType) {
	t.Helper()
	seen := map[trajectory.EventType]bool{}
	for _, event := range events {
		seen[event.Type] = true
	}
	for _, typ := range types {
		if !seen[typ] {
			t.Fatalf("missing trajectory event %q", typ)
		}
	}
}
