package team

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/config"
	"github.com/cosmtrek/jeju/internal/runs"
	"github.com/cosmtrek/jeju/internal/trajectory"
)

func TestRunAgentTeamWithMockLeadWorkerRounds(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	writeAgent := func(name string, tools string) string {
		t.Helper()
		agentDir := filepath.Join(root, "agents")
		promptDir := filepath.Join(root, "prompts")
		if err := os.MkdirAll(agentDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(promptDir, 0o755); err != nil {
			t.Fatal(err)
		}
		promptPath := filepath.Join(promptDir, name+".md")
		if err := os.WriteFile(promptPath, []byte("You are "+name+" for an AgentTeam fixture.\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		agentPath := filepath.Join(agentDir, name+".agent.yaml")
		content := `apiVersion: jeju/v1alpha1
kind: Agent

metadata:
  name: ` + name + `

models:
  providers:
    primary:
      type: mock
      model: mock

instructions:
  system: ../prompts/` + name + `.md

runtime:
  model: primary
  loop:
    type: react
  limits:
    maxSteps: 4
    maxDurationSec: 60
    maxToolCalls: 4
    maxConsecutiveErrors: 2

workspace:
  path: ../workspace

tools:
` + tools + `

permissions:
  access: full
  approval: never

evaluate:
  enabled: true
  evaluators:
    - name: basic
      uses: rules
      rules: [finalAnswerExists, runCompleted]
`
		if err := os.WriteFile(agentPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return agentPath
	}

	lead := writeAgent("research-lead", "  - read\n")
	framework := writeAgent("framework-researcher", "  []\n")
	architect := writeAgent("jeju-architect", "  []\n")
	verifier := writeAgent("verifier", "  []\n")
	writer := writeAgent("writer", "  - write\n")

	teamDir := filepath.Join(root, "teams")
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	teamPath := filepath.Join(teamDir, "agent-team-research.team.yaml")
	outDir := filepath.Join(root, ".jeju-dev", "team", "agent-team-deep-research")
	teamYAML := `apiVersion: jeju/v1alpha1
kind: AgentTeam

metadata:
  name: agent-team-deep-research

lead:
  agent: ` + lead + `

workers:
  framework_researcher:
    agent: ` + framework + `
    maxTasks: 2
  jeju_architect:
    agent: ` + architect + `
    maxTasks: 2
  verifier:
    agent: ` + verifier + `
    maxTasks: 2
  writer:
    agent: ` + writer + `
    maxTasks: 1

runtime:
  topology: lead_worker
  maxRounds: 4
  maxTasks: 7
  maxParallel: 2
  maxRetriesPerTask: 1

verification:
  requireStructuredTaskOutput: true
  requiredTaskFields:
    - summary
    - findings
    - evidence
    - gaps
    - residual_risk

output:
  dir: ` + outDir + `
`
	if err := os.WriteFile(teamPath, []byte(teamYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Run(context.Background(), teamPath, "Research agent team mechanisms and recommend the smallest Jeju implementation.", Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != StatusCompleted {
		t.Fatalf("status = %q, want %q", result.Status, StatusCompleted)
	}
	if result.Summary.RoundCount < 3 {
		t.Fatalf("round_count = %d, want at least 3", result.Summary.RoundCount)
	}
	if len(result.Summary.Tasks) != 4 {
		t.Fatalf("task count = %d, want 4", len(result.Summary.Tasks))
	}
	for _, worker := range []string{"framework_researcher", "jeju_architect", "verifier", "writer"} {
		found := false
		for _, task := range result.Summary.Tasks {
			if task.Worker == worker && task.Status == TaskVerified {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing verified task for worker %s", worker)
		}
	}
	verifierTask := result.Summary.Tasks["final-readiness-check"]
	if verifierTask.RoundCreated <= 1 {
		t.Fatalf("verifier task round = %d, want after round 1", verifierTask.RoundCreated)
	}
	finalTask := result.Summary.Tasks["final-report"]
	if finalTask.Worker != "writer" || finalTask.Status != TaskVerified {
		t.Fatalf("final-report task = %+v, want verified writer task", finalTask)
	}
	if !strings.Contains(result.Final, "# Agent Team Mechanism Recommendation") {
		t.Fatalf("final = %q, want writer task report", result.Final)
	}
	if result.Summary.Stats.ChildRuns < 8 {
		t.Fatalf("child_runs = %d, want at least 6", result.Summary.Stats.ChildRuns)
	}
	if _, err := os.Stat(filepath.Join(result.OutputDir, runs.TrajectoryFile)); err != nil {
		t.Fatalf("trajectory.jsonl missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(result.OutputDir, "team.events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("team.events.jsonl should not be written, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(result.OutputDir, "team.summary.json")); !os.IsNotExist(err) {
		t.Fatalf("team.summary.json should not be written, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(result.OutputDir, "team.snapshot.yaml")); !os.IsNotExist(err) {
		t.Fatalf("team.snapshot.yaml should not be written, stat err=%v", err)
	}
	events, err := trajectory.ReadFile(filepath.Join(result.OutputDir, runs.TrajectoryFile))
	if err != nil {
		t.Fatalf("read trajectory failed: %v", err)
	}
	record := trajectory.Project(events)
	if record.Integrity != trajectory.IntegrityComplete {
		t.Fatalf("trajectory integrity = %q issues=%v", record.Integrity, record.IntegrityIssues)
	}
	requireTeamEventTypes(t, events, trajectory.EventTrajectoryHeader, trajectory.EventSpanStarted, trajectory.EventSpanEnded, trajectory.EventActionCreated, trajectory.EventArtifactCreated, trajectory.EventRunSummary)
	summary, ok := ProjectSummary(record)
	if !ok {
		t.Fatal("expected team summary projection")
	}
	if summary.TeamRunID != result.TeamRunID {
		t.Fatalf("summary team_run_id = %q, want %q", summary.TeamRunID, result.TeamRunID)
	}
	if _, err := os.Stat(result.Report); err != nil {
		t.Fatalf("report missing: %v", err)
	}
	leadPlanning := findChildRun(t, result.Summary.ChildRuns, "lead-round-001")
	planningSnapshot := readConfigSnapshot(t, resolveRunDir(result.OutputDir, leadPlanning.RunDir))
	if !strings.Contains(planningSnapshot, "name: read") {
		t.Fatalf("planning lead should retain read tool, snapshot:\n%s", planningSnapshot)
	}
	if strings.Contains(planningSnapshot, "name: write") {
		t.Fatalf("planning lead should not expose write tool, snapshot:\n%s", planningSnapshot)
	}
	leadCounts := []int{}
	for _, child := range result.Summary.ChildRuns {
		if child.Role != "lead" {
			continue
		}
		leadCounts = append(leadCounts, readContextMessageCount(t, resolveRunDir(result.OutputDir, child.RunDir)))
	}
	if len(leadCounts) < 3 {
		t.Fatalf("lead child runs = %d, want at least 3", len(leadCounts))
	}
	for i := 1; i < len(leadCounts); i++ {
		if leadCounts[i] <= leadCounts[i-1] {
			t.Fatalf("lead message counts should grow across rounds, got %v", leadCounts)
		}
	}
	writerRun := findChildRun(t, result.Summary.ChildRuns, "task-final-report")
	writerSnapshot := readConfigSnapshot(t, resolveRunDir(result.OutputDir, writerRun.RunDir))
	if !strings.Contains(writerSnapshot, "name: write") {
		t.Fatalf("writer worker should retain write tool, snapshot:\n%s", writerSnapshot)
	}
}

func TestParseTeamDecisionAcceptsFlexibleModelShapes(t *testing.T) {
	decision, err := parseTeamDecision(`{
		"decision": "continue",
		"tasks": {
			"research-task": {
				"worker": "framework_researcher",
				"objective": "Map framework patterns.",
				"output_contract": "json"
			}
		},
		"finish": false
	}`)
	if err != nil {
		t.Fatalf("parseTeamDecision() error = %v", err)
	}
	if decision.Decision != "continue" {
		t.Fatalf("decision = %q", decision.Decision)
	}
	if len(decision.Tasks) != 1 {
		t.Fatalf("task count = %d, want 1", len(decision.Tasks))
	}
	task := decision.Tasks[0]
	if task.ID != "research-task" {
		t.Fatalf("task id = %q", task.ID)
	}
	if task.OutputContract.Format != "json" {
		t.Fatalf("output contract format = %q, want json", task.OutputContract.Format)
	}
	if decision.Finish == nil {
		t.Fatal("finish pointer should be present for flexible bool input")
	}
	if decision.Finish.Content != "" {
		t.Fatalf("finish content = %q, want empty", decision.Finish.Content)
	}
}

func TestAddTasksRejectsInvalidTaskSpecAndKeepsValidTasks(t *testing.T) {
	c := &controller{
		manifest: &AgentTeamManifest{
			Metadata: config.Metadata{Name: "invalid-task-team"},
			Workers: map[string]Worker{
				"worker": {MaxTasks: 1},
			},
			Runtime: RuntimeConfig{MaxTasks: 1},
		},
	}
	c.initSummary()

	added := c.addTasks([]TaskSpec{
		{
			ID:     "bad-worker",
			Worker: "worker",
		},
		{
			ID:        "good-task",
			Worker:    "worker",
			Objective: "This task should still be accepted.",
		},
	}, 1)

	if added != 1 {
		t.Fatalf("added = %d, want 1", added)
	}
	rejected := c.summary.Tasks["bad-worker"]
	if rejected.Status != TaskRejected {
		t.Fatalf("bad-worker status = %q, want %q", rejected.Status, TaskRejected)
	}
	if rejected.Verification.Passed || !strings.Contains(rejected.Error, "objective is required") {
		t.Fatalf("bad-worker verification/error = %+v / %q", rejected.Verification, rejected.Error)
	}
	accepted := c.summary.Tasks["good-task"]
	if accepted.Status != TaskPlanned {
		t.Fatalf("good-task status = %q, want %q", accepted.Status, TaskPlanned)
	}
	if !c.hasUnsuccessfulTasks() {
		t.Fatal("invalid task should make the team partial instead of invisible")
	}
}

func TestAddTasksDefaultsContextRefsToDependsOnAndNormalizesRefs(t *testing.T) {
	c := &controller{
		manifest: &AgentTeamManifest{
			Metadata: config.Metadata{Name: "context-team"},
			Workers: map[string]Worker{
				"worker": {MaxTasks: 3},
			},
			Runtime: RuntimeConfig{MaxTasks: 3},
		},
	}
	c.initSummary()
	c.summary.Tasks["source"] = TaskState{ID: "source", Worker: "worker", Status: TaskVerified}

	added := c.addTasks([]TaskSpec{
		{
			ID:        "default-context",
			Worker:    "worker",
			Objective: "Use source output.",
			DependsOn: []string{"task:source"},
		},
		{
			ID:             "explicit-empty-context",
			Worker:         "worker",
			Objective:      "Wait for source but do not inject it.",
			DependsOn:      []string{"source"},
			ContextRefs:    []string{},
			contextRefsSet: true,
		},
	}, 2)
	if added != 2 {
		t.Fatalf("added = %d, want 2", added)
	}
	if got := c.summary.Tasks["default-context"].DependsOn; len(got) != 1 || got[0] != "source" {
		t.Fatalf("default-context depends_on = %#v, want [source]", got)
	}
	if got := c.summary.Tasks["default-context"].ContextRefs; len(got) != 1 || got[0] != "source" {
		t.Fatalf("default-context context_refs = %#v, want [source]", got)
	}
	if got := c.summary.Tasks["explicit-empty-context"].ContextRefs; len(got) != 0 {
		t.Fatalf("explicit-empty-context context_refs = %#v, want []", got)
	}
}

func TestReadyTasksBlocksRejectedDependencies(t *testing.T) {
	c := &controller{
		manifest: &AgentTeamManifest{
			Metadata: config.Metadata{Name: "dependency-team"},
			Workers: map[string]Worker{
				"worker": {},
			},
			Runtime: RuntimeConfig{MaxTasks: 3},
		},
	}
	c.initSummary()
	c.summary.Tasks["failed"] = TaskState{
		ID:       "failed",
		Worker:   "worker",
		Status:   TaskRejected,
		Attempts: 1,
		Error:    "worker failed",
	}
	c.summary.Tasks["dependent"] = TaskState{
		ID:        "dependent",
		Worker:    "worker",
		Objective: "Use failed task output.",
		DependsOn: []string{"failed"},
		Status:    TaskPlanned,
	}

	ready := c.readyTasks()
	if len(ready) != 0 {
		t.Fatalf("ready task count = %d, want 0", len(ready))
	}
	dependent := c.summary.Tasks["dependent"]
	if dependent.Status != TaskBlocked {
		t.Fatalf("dependent status = %q, want %q", dependent.Status, TaskBlocked)
	}
	if !strings.Contains(dependent.Error, `dependency "failed" is rejected`) {
		t.Fatalf("dependent error = %q", dependent.Error)
	}
}

func TestFinishBlockedWhenTasksRemainUnresolved(t *testing.T) {
	c := &controller{
		manifest: &AgentTeamManifest{
			Metadata: config.Metadata{Name: "pending-team"},
			Workers: map[string]Worker{
				"worker": {},
			},
			Runtime: RuntimeConfig{MaxTasks: 1},
		},
	}
	c.initSummary()
	c.summary.Tasks["pending"] = TaskState{
		ID:     "pending",
		Worker: "worker",
		Status: TaskPlanned,
	}

	reason := c.finishBlockedReason()
	if !strings.Contains(reason, "unresolved tasks remain") {
		t.Fatalf("finishBlockedReason() = %q, want unresolved task reason", reason)
	}
}

func TestResolveFinishValidatesTaskID(t *testing.T) {
	c := &controller{manifest: &AgentTeamManifest{}}
	c.initSummary()
	c.summary.Tasks["completed"] = TaskState{ID: "completed", Worker: "writer", Status: TaskCompleted, Final: "draft"}
	c.summary.Tasks["empty"] = TaskState{ID: "empty", Worker: "writer", Status: TaskVerified}
	c.summary.Tasks["final"] = TaskState{ID: "final", Worker: "writer", Status: TaskVerified, Final: "done"}

	if _, reason := c.resolveFinish(&Finish{Content: "manual", TaskID: "final"}); !strings.Contains(reason, "mutually exclusive") {
		t.Fatalf("resolveFinish content+task_id reason = %q, want mutual exclusion", reason)
	}
	if _, reason := c.resolveFinish(&Finish{TaskID: "completed"}); !strings.Contains(reason, "want verified") {
		t.Fatalf("resolveFinish completed reason = %q, want verified status check", reason)
	}
	if _, reason := c.resolveFinish(&Finish{TaskID: "empty"}); !strings.Contains(reason, "empty final output") {
		t.Fatalf("resolveFinish empty reason = %q, want empty final check", reason)
	}
	final, reason := c.resolveFinish(&Finish{TaskID: "task:final"})
	if reason != "" || final != "done" {
		t.Fatalf("resolveFinish task:final = final %q reason %q, want done/no reason", final, reason)
	}
}

func TestLeadTurnInputUsesCompactState(t *testing.T) {
	c := &controller{
		manifest: &AgentTeamManifest{
			Metadata: config.Metadata{Name: "compact-team"},
			Workers: map[string]Worker{
				"worker": {Description: "Worker"},
			},
			Runtime: RuntimeConfig{MaxRounds: 3, MaxTasks: 2},
		},
		goal: "Review changes.",
	}
	c.initSummary()
	largeFinal := strings.Repeat("x", leadStateTaskFinalLimit+200)
	c.summary.ChildRuns = []ChildRunSummary{
		{Label: "task-large", RunID: "child-run"},
	}
	c.summary.Tasks["large"] = TaskState{
		ID:     "large",
		Worker: "worker",
		Status: TaskVerified,
		Final:  largeFinal,
	}

	input := c.leadTurnInput(2)
	if strings.Contains(input, largeFinal) {
		t.Fatal("lead decision input should not include the full task final")
	}
	if !strings.Contains(input, "Jeju truncated team state") {
		t.Fatalf("lead decision input should mention truncation:\n%s", input)
	}
	if strings.Contains(input, "child-run") {
		t.Fatalf("lead decision input should omit child run list:\n%s", input)
	}
}

func TestRunTaskClearsPreviousRunOutputOnChildError(t *testing.T) {
	root := t.TempDir()
	recorder, err := trajectory.NewRecorderWithOptions(root, trajectory.RecorderOptions{Console: false})
	if err != nil {
		t.Fatalf("NewRecorderWithOptions() error = %v", err)
	}
	defer recorder.Close()
	c := &controller{
		manifest: &AgentTeamManifest{
			Metadata: config.Metadata{Name: "retry-team"},
			Workers: map[string]Worker{
				"worker": {Agent: filepath.Join(root, "missing.agent.yaml")},
			},
			Runtime: RuntimeConfig{MaxRetriesPerTask: 0},
		},
		id:       "test-run",
		runDir:   root,
		recorder: recorder,
	}
	task := TaskState{
		ID:       "retry",
		Worker:   "worker",
		Status:   TaskRetryScheduled,
		Attempts: 1,
		RunID:    "previous-run",
		RunDir:   filepath.Join(root, "previous-run"),
		Final:    "stale final",
	}

	result := c.runTask(context.Background(), task)
	if result.Status != TaskRejected {
		t.Fatalf("status = %q, want %q", result.Status, TaskRejected)
	}
	if result.RunID != "" || result.RunDir != "" || result.Final != "" {
		t.Fatalf("stale run output was not cleared: %+v", result)
	}
}

func TestRunFailsAfterAbortDecision(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	lead := writeMockAgent(t, root, "research-lead", "  - write\n")
	worker := writeMockAgent(t, root, "worker", "  []\n")

	teamDir := filepath.Join(root, "teams")
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	teamPath := filepath.Join(teamDir, "blocked.team.yaml")
	outDir := filepath.Join(root, ".jeju-dev", "team", "blocked-team")
	teamYAML := `apiVersion: jeju/v1alpha1
kind: AgentTeam
metadata:
  name: blocked-team
lead:
  agent: ` + lead + `
workers:
  worker:
    agent: ` + worker + `
runtime:
  topology: lead_worker
  maxRounds: 3
  maxTasks: 4
  maxParallel: 1
verification:
  requireStructuredTaskOutput: true
  requiredTaskFields:
    - summary
output:
  dir: ` + outDir + `
`
	if err := os.WriteFile(teamPath, []byte(teamYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Run(context.Background(), teamPath, "Use blocked decision.", Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != StatusFailed {
		t.Fatalf("status = %q, want %q", result.Status, StatusFailed)
	}
	if result.Final != "Team aborted by lead without a final explanation." {
		t.Fatalf("final = %q", result.Final)
	}
}

func TestRunSkipsFinishWhenRequiredVerifierMissing(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	lead := writeMockAgent(t, root, "research-lead", "  - write\n")
	framework := writeMockAgent(t, root, "framework-researcher", "  []\n")
	architect := writeMockAgent(t, root, "jeju-architect", "  []\n")
	verifier := writeMockAgent(t, root, "verifier", "  []\n")

	teamDir := filepath.Join(root, "teams")
	if err := os.MkdirAll(teamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	teamPath := filepath.Join(teamDir, "agent-team-research.team.yaml")
	outDir := filepath.Join(root, ".jeju-dev", "team", "agent-team-deep-research")
	teamYAML := `apiVersion: jeju/v1alpha1
kind: AgentTeam
metadata:
  name: agent-team-deep-research
lead:
  agent: ` + lead + `
workers:
  framework_researcher:
    agent: ` + framework + `
  jeju_architect:
    agent: ` + architect + `
  verifier:
    agent: ` + verifier + `
runtime:
  topology: lead_worker
  maxRounds: 1
  maxTasks: 6
  maxParallel: 2
  maxRetriesPerTask: 0
verification:
  requireStructuredTaskOutput: true
  requireVerifier: true
  requiredTaskFields:
    - summary
    - findings
    - evidence
    - gaps
    - residual_risk
output:
  dir: ` + outDir + `
`
	if err := os.WriteFile(teamPath, []byte(teamYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Run(context.Background(), teamPath, "Research agent team mechanisms and recommend the smallest Jeju implementation.", Options{})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != StatusPartialCompleted {
		t.Fatalf("status = %q, want %q", result.Status, StatusPartialCompleted)
	}
	if !strings.Contains(result.Final, "no verifier worker task is verified") {
		t.Fatalf("final = %q, want verifier gate message", result.Final)
	}
}

func writeMockAgent(t *testing.T, root, name, tools string) string {
	t.Helper()
	agentDir := filepath.Join(root, "agents")
	promptDir := filepath.Join(root, "prompts")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(promptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	promptPath := filepath.Join(promptDir, name+".md")
	if err := os.WriteFile(promptPath, []byte("You are "+name+" for an AgentTeam fixture.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(agentDir, name+".agent.yaml")
	content := `apiVersion: jeju/v1alpha1
kind: Agent
metadata:
  name: ` + name + `
models:
  providers:
    primary:
      type: mock
      model: mock
instructions:
  system: ../prompts/` + name + `.md
runtime:
  model: primary
  loop:
    type: react
  limits:
    maxSteps: 4
    maxDurationSec: 60
    maxToolCalls: 4
    maxConsecutiveErrors: 2
workspace:
  path: ../workspace
tools:
` + tools + `
permissions:
  access: full
  approval: never
evaluate:
  enabled: true
  evaluators:
    - name: basic
      uses: rules
      rules: [finalAnswerExists, runCompleted]
`
	if err := os.WriteFile(agentPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return agentPath
}

func findChildRun(t *testing.T, children []ChildRunSummary, label string) ChildRunSummary {
	t.Helper()
	for _, child := range children {
		if child.Label == label {
			return child
		}
	}
	t.Fatalf("missing child run %s", label)
	return ChildRunSummary{}
}

func resolveRunDir(parent, child string) string {
	if filepath.IsAbs(child) {
		return child
	}
	return filepath.Join(parent, child)
}

func readConfigSnapshot(t *testing.T, runDir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(runDir, runs.TrajectoryFile))
	if err != nil {
		t.Fatalf("read trajectory failed: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event struct {
			Type    string `json:"type"`
			Payload struct {
				Role string `json:"role"`
				Text string `json:"text"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("trajectory event invalid: %v", err)
		}
		if event.Type == "artifact.created" && event.Payload.Role == "config_snapshot" {
			return event.Payload.Text
		}
	}
	t.Fatalf("missing config snapshot in %s", runDir)
	return ""
}

func readContextMessageCount(t *testing.T, runDir string) int {
	t.Helper()
	events, err := trajectory.ReadFile(filepath.Join(runDir, runs.TrajectoryFile))
	if err != nil {
		t.Fatalf("read child trajectory failed: %v", err)
	}
	for _, event := range events {
		if event.Type != trajectory.EventArtifactCreated {
			continue
		}
		role, _ := event.Payload["role"].(string)
		if role != "context_report" {
			continue
		}
		value, _ := event.Payload["value"].(map[string]any)
		count, _ := value["message_count_before"].(float64)
		return int(count)
	}
	t.Fatalf("missing context report in %s", runDir)
	return 0
}

func requireTeamEventTypes(t *testing.T, events []trajectory.Event, types ...trajectory.EventType) {
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
