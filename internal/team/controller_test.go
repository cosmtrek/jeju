package team

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cosmtrek/jeju/internal/config"
	"github.com/cosmtrek/jeju/internal/runs"
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
	synthesisLead := writeAgent("research-synthesis", "  - write\n")
	framework := writeAgent("framework-researcher", "  []\n")
	architect := writeAgent("jeju-architect", "  []\n")
	verifier := writeAgent("verifier", "  []\n")

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
  synthesisAgent: ` + synthesisLead + `

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

runtime:
  topology: lead_worker
  maxRounds: 3
  maxTasks: 6
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
	if len(result.Summary.Tasks) != 3 {
		t.Fatalf("task count = %d, want 3", len(result.Summary.Tasks))
	}
	for _, worker := range []string{"framework_researcher", "jeju_architect", "verifier"} {
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
	verifierTask := result.Summary.Tasks["synthesis-readiness-check"]
	if verifierTask.RoundCreated <= 1 {
		t.Fatalf("verifier task round = %d, want after round 1", verifierTask.RoundCreated)
	}
	if result.Summary.Stats.ChildRuns < 6 {
		t.Fatalf("child_runs = %d, want at least 6", result.Summary.Stats.ChildRuns)
	}
	if _, err := os.Stat(filepath.Join(result.OutputDir, "team.events.jsonl")); err != nil {
		t.Fatalf("team.events.jsonl missing: %v", err)
	}
	summaryData, err := os.ReadFile(filepath.Join(result.OutputDir, "team.summary.json"))
	if err != nil {
		t.Fatalf("team.summary.json missing: %v", err)
	}
	var summary Summary
	if err := json.Unmarshal(summaryData, &summary); err != nil {
		t.Fatalf("summary JSON invalid: %v", err)
	}
	if summary.TeamRunID != result.TeamRunID {
		t.Fatalf("summary team_run_id = %q, want %q", summary.TeamRunID, result.TeamRunID)
	}
	if _, err := os.Stat(result.Report); err != nil {
		t.Fatalf("report missing: %v", err)
	}
	leadPlanning := findChildRun(t, result.Summary.ChildRuns, "lead-round-001")
	planningSnapshot := readConfigSnapshot(t, leadPlanning.RunDir)
	if !strings.Contains(planningSnapshot, "name: read") {
		t.Fatalf("planning lead should retain read tool, snapshot:\n%s", planningSnapshot)
	}
	if strings.Contains(planningSnapshot, "name: write") {
		t.Fatalf("planning lead should not expose write tool, snapshot:\n%s", planningSnapshot)
	}
	leadSynthesis := findChildRun(t, result.Summary.ChildRuns, "lead-synthesis")
	synthesisSnapshot := readConfigSnapshot(t, leadSynthesis.RunDir)
	if !strings.Contains(synthesisSnapshot, "name: write") {
		t.Fatalf("synthesis lead should retain write tool, snapshot:\n%s", synthesisSnapshot)
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

func TestSynthesisBlockedWhenTasksRemainUnresolved(t *testing.T) {
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

	reason := c.synthesisBlockedReason()
	if !strings.Contains(reason, "unresolved tasks remain") {
		t.Fatalf("synthesisBlockedReason() = %q, want unresolved task reason", reason)
	}
}

func TestLeadDecisionInputUsesCompactState(t *testing.T) {
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

	input := c.leadDecisionInput(2)
	if strings.Contains(input, largeFinal) {
		t.Fatal("lead decision input should not include the full task final")
	}
	if !strings.Contains(input, "Jeju truncated team state") {
		t.Fatalf("lead decision input should mention truncation:\n%s", input)
	}
	if strings.Contains(input, "child-run") {
		t.Fatalf("lead decision input should omit child run list:\n%s", input)
	}
	if !strings.Contains(c.synthesisInput(), largeFinal) {
		t.Fatal("synthesis input should retain full final state")
	}
}

func TestCreateTeamRunDirAvoidsCollisions(t *testing.T) {
	root := t.TempDir()
	startedAt := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)

	firstID, firstDir, err := createTeamRunDir(root, startedAt, "review-team")
	if err != nil {
		t.Fatalf("create first run dir failed: %v", err)
	}
	secondID, secondDir, err := createTeamRunDir(root, startedAt, "review-team")
	if err != nil {
		t.Fatalf("create second run dir failed: %v", err)
	}
	if secondID != firstID+"-01" {
		t.Fatalf("second run id = %q, want %q", secondID, firstID+"-01")
	}
	if firstDir == secondDir {
		t.Fatalf("run dirs should differ: %q", firstDir)
	}
}

func TestRunTaskClearsPreviousRunOutputOnChildError(t *testing.T) {
	root := t.TempDir()
	c := &controller{
		manifest: &AgentTeamManifest{
			Metadata: config.Metadata{Name: "retry-team"},
			Workers: map[string]Worker{
				"worker": {Agent: filepath.Join(root, "missing.agent.yaml")},
			},
			Runtime: RuntimeConfig{MaxRetriesPerTask: 0},
		},
		runDir: root,
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

func TestRunDoesNotSynthesizeAfterEmptyBlockedDecision(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	lead := writeMockAgent(t, root, "research-lead", "  - write\n")
	synthesisLead := writeMockAgent(t, root, "research-synthesis", "  - write\n")
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
  synthesisAgent: ` + synthesisLead + `
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
	if result.Status != StatusPartialCompleted {
		t.Fatalf("status = %q, want %q", result.Status, StatusPartialCompleted)
	}
	if result.Final != "Team blocked by lead without a final explanation." {
		t.Fatalf("final = %q", result.Final)
	}
	for _, child := range result.Summary.ChildRuns {
		if child.Label == "lead-synthesis" {
			t.Fatalf("lead synthesis should not run after blocked decision: %+v", child)
		}
	}
}

func TestRunSkipsSynthesisWhenRequiredVerifierMissing(t *testing.T) {
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
	for _, child := range result.Summary.ChildRuns {
		if child.Label == "lead-synthesis" {
			t.Fatalf("lead synthesis should not run when required verifier is missing: %+v", child)
		}
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
