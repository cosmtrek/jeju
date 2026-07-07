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

	"github.com/cosmtrek/jeju/internal/cli"
	"github.com/cosmtrek/jeju/internal/compiler"
	"github.com/cosmtrek/jeju/internal/config"
	"github.com/cosmtrek/jeju/internal/runs"
	teamrunner "github.com/cosmtrek/jeju/internal/team"
	"github.com/cosmtrek/jeju/internal/trajectory"
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
		if err := cli.Execute(ctx, []string{"view"}); err != nil {
			t.Fatalf("view command failed: %v", err)
		}
		if err := cli.Execute(ctx, []string{"inspect", runID}); err != nil {
			t.Fatalf("inspect command failed: %v", err)
		}

		runDir := filepath.Join(workdir, "runs", runID)
		requireFile(t, filepath.Join(runDir, runs.TrajectoryFile))
		requireFile(t, filepath.Join(workdir, "workspace", "agent", "notes.md"))

		events, err := trajectory.ReadFile(filepath.Join(runDir, runs.TrajectoryFile))
		if err != nil {
			t.Fatalf("read trajectory failed: %v", err)
		}
		requireEventTypes(t, events,
			trajectory.EventTrajectoryHeader,
			trajectory.EventSpanStarted,
			trajectory.EventSpanEnded,
			trajectory.EventActionCreated,
			trajectory.EventPermissionDecided,
			trajectory.EventArtifactCreated,
			trajectory.EventRunSummary,
		)

		record := trajectory.Project(events)
		if record.Evaluation == nil && record.EvaluationRef == "" {
			t.Fatalf("expected evaluation in trajectory")
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
		instructions, err := os.ReadFile(filepath.Join(workdir, "prompts", "deep-research.md"))
		if err != nil {
			t.Fatalf("read deep research instructions failed: %v", err)
		}
		if strings.Contains(string(instructions), "Workflow:") || strings.Contains(string(instructions), "reports/deep-research.md") {
			t.Fatalf("deep research system instructions should delegate workflow to skill:\n%s", instructions)
		}
		prompt := agent.SystemPrompt()
		if !strings.Contains(prompt, `"name":"search_api"`) {
			t.Fatalf("system prompt does not include search_api tool:\n%s", prompt)
		}
		if !strings.Contains(prompt, "# Active Skill Instructions") ||
			!strings.Contains(prompt, "## Skill: deep-research") ||
			!strings.Contains(prompt, "reports/deep-research.md") {
			t.Fatalf("system prompt does not include loaded deep research skill workflow:\n%s", prompt)
		}
		messages := agent.PromptMessages(true)
		if len(messages) < 4 {
			t.Fatalf("expected layered prompt messages, got %+v", messages)
		}
		if messages[0].Role != "system" || !strings.Contains(messages[0].Content, "# Agent Instructions") ||
			strings.Contains(messages[0].Content, "Jeju, a config-defined agent runtime") {
			t.Fatalf("first native prompt layer should be agent instructions without Jeju runtime protocol: %+v", messages[0])
		}
		last := messages[len(messages)-1]
		if last.Role != "user" || !strings.Contains(last.Content, "# Active Skill Instructions") ||
			!strings.Contains(last.Content, "reports/deep-research.md") {
			t.Fatalf("active skill instructions should be the final contextual user layer before task input: %+v", last)
		}
	})

	t.Run("agent team deep research fixture runs", func(t *testing.T) {
		tmp := t.TempDir()
		workdir := filepath.Join(tmp, "agent-team-deep-research")
		copyDir(t, fixturePath(t, "agent-team-deep-research"), workdir)

		restoreCWD := chdir(t, workdir)
		defer restoreCWD()

		ctx := context.Background()
		goal := "Research agent team mechanisms and recommend the smallest Jeju implementation. Write final report to reports/agent-team-mechanism.md."
		if err := cli.Execute(ctx, []string{"team", "run", "--output", "final", "teams/agent-team-research.team.yaml", goal}); err != nil {
			t.Fatalf("team run failed: %v", err)
		}

		teamRoot := filepath.Join(workdir, ".jeju-dev", "team", "agent-team-deep-research")
		entries, err := os.ReadDir(teamRoot)
		if err != nil {
			t.Fatalf("read team output root failed: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 team run dir, got %d", len(entries))
		}
		runDir := filepath.Join(teamRoot, entries[0].Name())
		requireFile(t, filepath.Join(runDir, runs.TrajectoryFile))
		requireFile(t, filepath.Join(runDir, "report.html"))
		requireFile(t, filepath.Join(workdir, "workspace", "research", "reports", "agent-team-mechanism.md"))
		if _, err := os.Stat(filepath.Join(runDir, "team.events.jsonl")); !os.IsNotExist(err) {
			t.Fatalf("team.events.jsonl should not be written, stat err=%v", err)
		}
		if _, err := os.Stat(filepath.Join(runDir, "team.summary.json")); !os.IsNotExist(err) {
			t.Fatalf("team.summary.json should not be written, stat err=%v", err)
		}
		events, err := trajectory.ReadFile(filepath.Join(runDir, runs.TrajectoryFile))
		if err != nil {
			t.Fatalf("read team trajectory failed: %v", err)
		}
		record := trajectory.Project(events)
		if record.Integrity != trajectory.IntegrityComplete {
			t.Fatalf("team trajectory integrity = %q issues=%v", record.Integrity, record.IntegrityIssues)
		}
		summary, ok := teamrunner.ProjectSummary(record)
		if !ok {
			t.Fatal("expected projected team summary")
		}
		if summary.Status != "completed" {
			t.Fatalf("team status = %q, want completed", summary.Status)
		}
		if summary.RoundCount < 2 || summary.RoundCount > summary.MaxRounds {
			t.Fatalf("round_count = %d, max = %d", summary.RoundCount, summary.MaxRounds)
		}
		if len(summary.Tasks) < 3 {
			t.Fatalf("expected at least 3 tasks, got %d", len(summary.Tasks))
		}
		workers := map[string]bool{}
		for _, worker := range summary.DeclaredWorkers {
			workers[worker] = true
		}
		for _, want := range []string{"framework_researcher", "jeju_architect", "verifier", "writer"} {
			if !workers[want] {
				t.Fatalf("declared worker %s missing from summary", want)
			}
		}
		verifierTask, ok := summary.Tasks["final-readiness-check"]
		if !ok {
			t.Fatal("verifier task missing")
		}
		if verifierTask.RoundCreated <= 1 {
			t.Fatalf("verifier task round = %d, want after round 1", verifierTask.RoundCreated)
		}
		if _, ok := summary.Tasks["final-report"]; !ok {
			t.Fatal("final report writer task missing")
		}
		for id, task := range summary.Tasks {
			if !workers[task.Worker] {
				t.Fatalf("task %s used undeclared worker %s", id, task.Worker)
			}
			if task.Status != "verified" {
				t.Fatalf("task %s status = %q, want verified", id, task.Status)
			}
			if task.RunID == "" || !task.Verification.Passed {
				t.Fatalf("task %s missing run id or verification pass: %+v", id, task)
			}
		}
		leadRuns := 0
		workerRuns := 0
		for _, run := range summary.ChildRuns {
			if run.Role == "lead" {
				leadRuns++
			}
			if run.Role == "worker" {
				workerRuns++
			}
		}
		if leadRuns == 0 || workerRuns < 3 {
			t.Fatalf("unexpected child run roles: lead=%d worker=%d", leadRuns, workerRuns)
		}
	})

	t.Run("long horizon fixture compiles", func(t *testing.T) {
		tmp := t.TempDir()
		workdir := filepath.Join(tmp, "long-horizon")
		copyDir(t, fixturePath(t, "long-horizon"), workdir)

		restoreCWD := chdir(t, workdir)
		defer restoreCWD()

		manifest, _, err := config.LoadFile("agents/long-horizon.yaml")
		if err != nil {
			t.Fatalf("LoadFile failed: %v", err)
		}
		if err := config.Validate(manifest); err != nil {
			t.Fatalf("Validate failed: %v", err)
		}
		if manifest.Runtime.CompressionThreshold != 0.4 {
			t.Fatalf("expected compression threshold 0.4, got %v", manifest.Runtime.CompressionThreshold)
		}
		provider := manifest.Models.Providers["primary"]
		if provider.ContextWindow != 16000 || provider.MaxOutputTokens != 1024 {
			t.Fatalf("unexpected compression budget provider config: %+v", provider)
		}

		agent, err := compiler.Compile("agents/long-horizon.yaml")
		if err != nil {
			t.Fatalf("Compile failed: %v", err)
		}
		if _, ok := agent.Tools.Get("chapter_probe"); !ok {
			t.Fatal("chapter_probe tool missing")
		}
		if _, ok := agent.Tools.Get("write"); !ok {
			t.Fatal("write tool missing")
		}
		if _, ok := agent.Skills.Get("long-horizon-audit"); !ok {
			t.Fatal("long-horizon-audit skill missing")
		}
		prompt := agent.SystemPrompt()
		if !strings.Contains(prompt, "chapter_probe") ||
			!strings.Contains(prompt, "reports/long-horizon-summary.md") {
			t.Fatalf("system prompt does not include long horizon workflow:\n%s", prompt)
		}

		tool, ok := agent.Tools.Get("chapter_probe")
		if !ok {
			t.Fatal("chapter_probe tool missing")
		}
		result, err := tool.Run(context.Background(), json.RawMessage(`{"chapter":3}`))
		if err != nil {
			t.Fatalf("chapter_probe failed: %v", err)
		}
		if !strings.Contains(result.Output, "CHK-03-411") || !strings.Contains(result.Output, "chapter=3") {
			t.Fatalf("unexpected chapter_probe output: %s", result.Output)
		}
	})
}

func TestCodeReviewTeamExampleCompiles(t *testing.T) {
	root := repoRoot(t)
	teamPath := filepath.Join(root, "examples", "code-review-team", "teams", "code-review.team.yaml")
	manifest, _, err := teamrunner.LoadFile(teamPath)
	if err != nil {
		t.Fatalf("LoadFile(%s) failed: %v", teamPath, err)
	}
	if manifest.Metadata.Name != "code-review-team" {
		t.Fatalf("team name = %q, want code-review-team", manifest.Metadata.Name)
	}
	if !manifest.Verification.RequireVerifier {
		t.Fatal("code review team example should require verifier")
	}
	if _, ok := manifest.Workers[teamrunner.VerifierWorkerName]; !ok {
		t.Fatalf("code review team example missing %q worker", teamrunner.VerifierWorkerName)
	}

	agentPaths := []string{manifest.Lead.Agent}
	for _, worker := range manifest.Workers {
		agentPaths = append(agentPaths, worker.Agent)
	}
	for _, path := range agentPaths {
		if _, err := compiler.Compile(path); err != nil {
			t.Fatalf("Compile(%s) failed: %v", path, err)
		}
	}
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

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(file))
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
