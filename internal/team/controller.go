package team

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cosmtrek/jeju/internal/compiler"
	"github.com/cosmtrek/jeju/internal/runs"
	"github.com/cosmtrek/jeju/internal/runtime"
	"github.com/cosmtrek/jeju/internal/trajectory"
)

const (
	StatusCompleted        = "completed"
	StatusPartialCompleted = "partial_completed"
	StatusFailed           = "failed"

	VerifierWorkerName = "verifier"

	TaskPlanned        = "planned"
	TaskReady          = "ready"
	TaskRunning        = "running"
	TaskCompleted      = "completed"
	TaskVerified       = "verified"
	TaskRejected       = "rejected"
	TaskRetryScheduled = "retry_scheduled"
	TaskBlocked        = "blocked"

	leadStateTaskFinalLimit = 4000
	leadStateTaskErrorLimit = 1000
)

type Options struct {
	WorkspaceOverride string
	OutputDir         string
}

type Result struct {
	TeamRunID string
	OutputDir string
	Report    string
	Status    string
	Final     string
	Summary   Summary
}

type Summary struct {
	TeamRunID       string               `json:"team_run_id"`
	Team            string               `json:"team"`
	Goal            string               `json:"goal"`
	Status          string               `json:"status"`
	StartedAt       string               `json:"started_at"`
	EndedAt         string               `json:"ended_at"`
	RoundCount      int                  `json:"round_count"`
	MaxRounds       int                  `json:"max_rounds"`
	MaxTasks        int                  `json:"max_tasks"`
	DeclaredWorkers []string             `json:"declared_workers"`
	Tasks           map[string]TaskState `json:"tasks"`
	ChildRuns       []ChildRunSummary    `json:"child_runs"`
	Stats           Stats                `json:"stats"`
	Final           string               `json:"final"`
	FinalReportPath string               `json:"final_report_path,omitempty"`
}

type TaskState struct {
	ID             string             `json:"id"`
	Worker         string             `json:"worker"`
	Objective      string             `json:"objective"`
	ContextRefs    []string           `json:"context_refs,omitempty"`
	DependsOn      []string           `json:"depends_on,omitempty"`
	OutputContract OutputContract     `json:"output_contract,omitempty"`
	Status         string             `json:"status"`
	RoundCreated   int                `json:"round_created"`
	Attempts       int                `json:"attempts"`
	RunID          string             `json:"run_id,omitempty"`
	RunDir         string             `json:"run_dir,omitempty"`
	Final          string             `json:"final,omitempty"`
	Verification   VerificationResult `json:"verification"`
	Error          string             `json:"error,omitempty"`
}

type VerificationResult struct {
	Passed  bool     `json:"passed"`
	Reasons []string `json:"reasons,omitempty"`
}

type ChildRunSummary struct {
	Label  string `json:"label"`
	Agent  string `json:"agent"`
	Role   string `json:"role"`
	TaskID string `json:"task_id,omitempty"`
	RunID  string `json:"run_id"`
	RunDir string `json:"run_dir"`
	Status string `json:"status"`
	Stats  Stats  `json:"stats"`
}

type Stats struct {
	ChildRuns            int   `json:"child_runs"`
	ModelCalls           int   `json:"model_calls"`
	ToolCalls            int   `json:"tool_calls"`
	ModelErrors          int   `json:"model_errors"`
	ToolErrors           int   `json:"tool_errors"`
	PermissionDenied     int   `json:"permission_denied"`
	PromptTokens         int   `json:"prompt_tokens"`
	PromptCacheHitTokens int   `json:"prompt_cache_hit_tokens"`
	CompletionTokens     int   `json:"completion_tokens"`
	TotalTokens          int   `json:"total_tokens"`
	DurationMS           int64 `json:"duration_ms"`
}

type TeamDecision struct {
	Decision     string     `json:"decision"`
	RoundSummary string     `json:"round_summary,omitempty"`
	Tasks        []TaskSpec `json:"tasks,omitempty"`
	Finish       *Finish    `json:"finish,omitempty"`
}

func (d *TeamDecision) UnmarshalJSON(data []byte) error {
	var raw struct {
		Decision     string          `json:"decision"`
		RoundSummary string          `json:"round_summary,omitempty"`
		Tasks        json.RawMessage `json:"tasks,omitempty"`
		Finish       *Finish         `json:"finish,omitempty"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	d.Decision = raw.Decision
	d.RoundSummary = raw.RoundSummary
	d.Finish = raw.Finish
	if len(raw.Tasks) == 0 || strings.TrimSpace(string(raw.Tasks)) == "null" {
		return nil
	}
	var list []TaskSpec
	if err := json.Unmarshal(raw.Tasks, &list); err == nil {
		d.Tasks = list
		return nil
	}
	var byID map[string]TaskSpec
	if err := json.Unmarshal(raw.Tasks, &byID); err != nil {
		return err
	}
	keys := make([]string, 0, len(byID))
	for key := range byID {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		task := byID[key]
		if task.ID == "" {
			task.ID = key
		}
		d.Tasks = append(d.Tasks, task)
	}
	return nil
}

type Finish struct {
	Content string `json:"content,omitempty"`
}

func (f *Finish) UnmarshalJSON(data []byte) error {
	switch strings.TrimSpace(string(data)) {
	case "", "null", "false":
		return nil
	case "true":
		return nil
	}
	var object struct {
		Content string `json:"content,omitempty"`
	}
	if err := json.Unmarshal(data, &object); err == nil {
		f.Content = object.Content
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	f.Content = text
	return nil
}

type TaskSpec struct {
	ID             string         `json:"id"`
	Worker         string         `json:"worker"`
	Objective      string         `json:"objective"`
	ContextRefs    []string       `json:"context_refs,omitempty"`
	DependsOn      []string       `json:"depends_on,omitempty"`
	OutputContract OutputContract `json:"output_contract,omitempty"`
}

type OutputContract struct {
	Format         string   `json:"format,omitempty"`
	RequiredFields []string `json:"required_fields,omitempty"`
}

func (c *OutputContract) UnmarshalJSON(data []byte) error {
	if strings.TrimSpace(string(data)) == "null" {
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		c.Format = strings.TrimSpace(text)
		return nil
	}
	var object struct {
		Format         string   `json:"format,omitempty"`
		RequiredFields []string `json:"required_fields,omitempty"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	c.Format = object.Format
	c.RequiredFields = object.RequiredFields
	return nil
}

type childRunResult struct {
	Label  string
	Role   string
	TaskID string
	RunID  string
	RunDir string
	Agent  string
	Status string
	Final  string
	Stats  Stats
}

type controller struct {
	manifest *AgentTeamManifest
	snapshot []byte
	goal     string
	opts     Options

	id        string
	runDir    string
	events    *eventWriter
	startedAt time.Time
	summary   Summary

	taskMu sync.Mutex
}

func Run(ctx context.Context, specPath string, goal string, opts Options) (*Result, error) {
	manifest, snapshot, err := LoadFile(specPath)
	if err != nil {
		return nil, err
	}
	c := &controller{manifest: manifest, snapshot: snapshot, goal: goal, opts: opts}
	return c.run(ctx)
}

func (c *controller) run(ctx context.Context) (*Result, error) {
	c.startedAt = time.Now()
	outputBase := c.manifest.Output.Dir
	if c.opts.OutputDir != "" {
		outputBase = c.opts.OutputDir
	}
	runID, runDir, err := createTeamRunDir(outputBase, c.startedAt, c.manifest.Metadata.Name)
	if err != nil {
		return nil, err
	}
	c.id = runID
	c.runDir = runDir
	if err := os.WriteFile(filepath.Join(c.runDir, "team.snapshot.yaml"), c.snapshot, 0o644); err != nil {
		return nil, err
	}
	events, err := newEventWriter(filepath.Join(c.runDir, "team.events.jsonl"))
	if err != nil {
		return nil, err
	}
	defer events.Close()
	c.events = events
	c.initSummary()
	c.events.Write("team.started", map[string]any{"team_run_id": c.id, "team": c.manifest.Metadata.Name, "goal": c.goal})

	if err := c.validateAgents(); err != nil {
		c.summary.Status = StatusFailed
		c.summary.Final = err.Error()
		return c.finalize()
	}

	emptyRounds := 0
	for round := 1; round <= c.manifest.Runtime.MaxRounds; round++ {
		c.summary.RoundCount = round
		c.events.Write("round.started", map[string]any{"round": round})
		decision, err := c.runLeadDecision(ctx, round)
		if err != nil {
			c.summary.Status = StatusFailed
			c.summary.Final = err.Error()
			return c.finalize()
		}
		c.events.Write("lead.decision", map[string]any{"round": round, "decision": decision})
		if decision.Decision == "blocked" {
			c.summary.Status = StatusPartialCompleted
			c.summary.Final = strings.TrimSpace(decision.RoundSummary)
			if decision.Finish != nil && strings.TrimSpace(decision.Finish.Content) != "" {
				c.summary.Final = strings.TrimSpace(decision.Finish.Content)
			}
			if c.summary.Final == "" {
				c.summary.Final = "Team blocked by lead without a final explanation."
			}
			break
		}
		if decision.Decision == "synthesize" {
			if c.manifest.Verification.RequireVerifier && !c.hasVerifiedWorker(VerifierWorkerName) {
				c.events.Write("lead.synthesis_rejected", map[string]any{"round": round, "reason": "verification.requireVerifier is true and no verifier task is verified"})
			} else if reason := c.pendingTaskReason(); reason != "" {
				c.events.Write("lead.synthesis_rejected", map[string]any{"round": round, "reason": reason})
			} else {
				break
			}
		}
		added := c.addTasks(decision.Tasks, round)
		blocked := c.blockTasksWithFailedDependencies()
		ready := c.readyTasks()
		if len(ready) == 0 && added == 0 && blocked == 0 {
			emptyRounds++
		} else {
			emptyRounds = 0
		}
		if len(ready) > 0 {
			if err := c.dispatchTasks(ctx, ready); err != nil {
				c.summary.Status = StatusFailed
				c.summary.Final = err.Error()
				return c.finalize()
			}
		}
		c.blockTasksWithFailedDependencies()
		c.events.Write("round.completed", map[string]any{"round": round, "added_tasks": added, "dispatched_tasks": len(ready)})
		if emptyRounds > c.manifest.Runtime.MaxConsecutiveEmptyRounds {
			break
		}
	}

	if c.summary.Final == "" {
		c.blockTasksWithFailedDependencies()
		if reason := c.synthesisBlockedReason(); reason != "" {
			c.summary.Status = StatusPartialCompleted
			c.summary.Final = reason
			c.events.Write("team.synthesis_skipped", map[string]any{"reason": reason})
		} else {
			final, err := c.runLeadSynthesis(ctx)
			if err != nil {
				c.summary.Status = StatusPartialCompleted
				c.summary.Final = fmt.Sprintf("Team synthesis failed: %s", err.Error())
			} else {
				c.summary.Final = final
			}
		}
	}
	if c.summary.Status == "" {
		if c.hasUnsuccessfulTasks() {
			c.summary.Status = StatusPartialCompleted
		} else {
			c.summary.Status = StatusCompleted
		}
	}
	return c.finalize()
}

func createTeamRunDir(baseDir string, startedAt time.Time, teamName string) (string, string, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", "", err
	}
	baseID := startedAt.Format("20060102-150405") + "-" + sanitizeName(teamName)
	for suffix := 0; ; suffix++ {
		runID := baseID
		if suffix > 0 {
			runID = fmt.Sprintf("%s-%02d", baseID, suffix)
		}
		runDir := filepath.Join(baseDir, runID)
		if err := os.Mkdir(runDir, 0o755); err == nil {
			return runID, runDir, nil
		} else if !os.IsExist(err) {
			return "", "", err
		}
	}
}

func (c *controller) initSummary() {
	c.summary = Summary{
		TeamRunID:       c.id,
		Team:            c.manifest.Metadata.Name,
		Goal:            c.goal,
		StartedAt:       c.startedAt.Format(time.RFC3339Nano),
		MaxRounds:       c.manifest.Runtime.MaxRounds,
		MaxTasks:        c.manifest.Runtime.MaxTasks,
		DeclaredWorkers: c.workerNames(),
		Tasks:           map[string]TaskState{},
	}
}

func (c *controller) validateAgents() error {
	if _, err := c.compileAgent(c.manifest.Lead.Agent, "compile-lead"); err != nil {
		return fmt.Errorf("compile lead agent: %w", err)
	}
	if c.manifest.Lead.SynthesisAgent != "" {
		if _, err := c.compileAgent(c.manifest.Lead.SynthesisAgent, "compile-lead-synthesis"); err != nil {
			return fmt.Errorf("compile lead synthesis agent: %w", err)
		}
	}
	for name, worker := range c.manifest.Workers {
		if _, err := c.compileAgent(worker.Agent, "compile-worker-"+name); err != nil {
			return fmt.Errorf("compile worker %s: %w", name, err)
		}
	}
	return nil
}

func (c *controller) compileAgent(agentPath string, label string) (*compiler.CompiledAgent, error) {
	opts := compiler.Options{
		RunStore: runs.NewStore(filepath.Join(c.runDir, "child-runs", sanitizeName(label))),
	}
	if c.opts.WorkspaceOverride != "" {
		opts.WorkspaceOverride = c.opts.WorkspaceOverride
	}
	return compiler.CompileWithOptions(agentPath, opts)
}

func (c *controller) runLeadDecision(ctx context.Context, round int) (TeamDecision, error) {
	label := fmt.Sprintf("lead-round-%03d", round)
	input := c.leadDecisionInput(round)
	child, err := c.runChild(ctx, c.manifest.Lead.Agent, label, "lead", "", input)
	if err != nil {
		return TeamDecision{}, err
	}
	c.recordChildRun(child)
	decision, err := parseTeamDecision(child.Final)
	if err != nil {
		return TeamDecision{}, fmt.Errorf("parse lead decision from %s: %w", child.RunID, err)
	}
	if err := c.validateDecision(decision); err != nil {
		return TeamDecision{}, err
	}
	return decision, nil
}

func (c *controller) runLeadSynthesis(ctx context.Context) (string, error) {
	child, err := c.runChild(ctx, c.leadSynthesisAgent(), "lead-synthesis", "lead", "", c.synthesisInput())
	if err != nil {
		return "", err
	}
	c.recordChildRun(child)
	return strings.TrimSpace(child.Final), nil
}

func (c *controller) leadSynthesisAgent() string {
	if c.manifest.Lead.SynthesisAgent != "" {
		return c.manifest.Lead.SynthesisAgent
	}
	return c.manifest.Lead.Agent
}

func (c *controller) runChild(ctx context.Context, agentPath, label, role, taskID, input string) (childRunResult, error) {
	agent, err := c.compileAgent(agentPath, label)
	if err != nil {
		return childRunResult{}, err
	}
	return c.runCompiledChild(ctx, agent, label, role, taskID, input)
}

func (c *controller) runCompiledChild(ctx context.Context, agent *compiler.CompiledAgent, label, role, taskID, input string) (childRunResult, error) {
	autoUserInput := "Use only the team goal, assigned task, task context refs, and available tools. Do not ask the user during an AgentTeam child run; return the required structured output or mark missing information in gaps/residual_risk."
	rt := runtime.NewWithOptions(runtime.Options{
		AutoUserInput:             &autoUserInput,
		SuppressConsoleTrajectory: true,
	})
	result, err := rt.Run(ctx, agent, input)
	if err != nil {
		return childRunResult{}, err
	}
	record, err := agent.RunStore.ReadRunRecord(result.RunID)
	if err != nil {
		return childRunResult{}, err
	}
	runDir := filepath.Join(agent.RunStore.BasePath, result.RunID)
	return childRunResult{
		Label:  label,
		Role:   role,
		TaskID: taskID,
		RunID:  result.RunID,
		RunDir: runDir,
		Agent:  record.Agent,
		Status: resultStatus(result.Status),
		Final:  result.Final,
		Stats:  statsFromRecord(record),
	}, nil
}

func (c *controller) synthesisBlockedReason() string {
	if c.manifest.Verification.RequireVerifier && !c.hasVerifiedWorker(VerifierWorkerName) {
		return "Team synthesis skipped: verification.requireVerifier is true and no verifier worker task is verified."
	}
	if reason := c.pendingTaskReason(); reason != "" {
		return "Team synthesis skipped: " + reason
	}
	return ""
}

func (c *controller) recordChildRun(child childRunResult) {
	c.summary.ChildRuns = append(c.summary.ChildRuns, ChildRunSummary{
		Label:  child.Label,
		Agent:  child.Agent,
		Role:   child.Role,
		TaskID: child.TaskID,
		RunID:  child.RunID,
		RunDir: child.RunDir,
		Status: child.Status,
		Stats:  child.Stats,
	})
	c.summary.Stats.add(child.Stats)
	c.summary.Stats.ChildRuns++
}

func (c *controller) validateDecision(decision TeamDecision) error {
	switch decision.Decision {
	case "continue", "synthesize", "blocked":
	default:
		return fmt.Errorf("lead decision %q is not supported", decision.Decision)
	}
	if decision.Decision != "continue" && len(decision.Tasks) > 0 {
		return fmt.Errorf("lead decision %q must not include new tasks", decision.Decision)
	}
	return nil
}

func (c *controller) addTasks(specs []TaskSpec, round int) int {
	added := 0
	for index, spec := range specs {
		if validTeamNameRe.MatchString(spec.ID) {
			if _, exists := c.summary.Tasks[spec.ID]; exists {
				c.events.Write("task.duplicate_skipped", map[string]any{"round": round, "task_id": spec.ID})
				continue
			}
		}
		if err := c.validateTaskSpec(spec); err != nil {
			c.recordRejectedTaskSpec(spec, round, index+1, err)
			continue
		}
		if c.taskLimitCount()+1 > c.manifest.Runtime.MaxTasks {
			c.events.Write("task.skipped", map[string]any{"round": round, "task_id": spec.ID, "reason": fmt.Sprintf("runtime.maxTasks exceeded: %d", c.manifest.Runtime.MaxTasks)})
			continue
		}
		task := TaskState{
			ID:             spec.ID,
			Worker:         spec.Worker,
			Objective:      spec.Objective,
			ContextRefs:    spec.ContextRefs,
			DependsOn:      spec.DependsOn,
			OutputContract: spec.OutputContract,
			Status:         TaskPlanned,
			RoundCreated:   round,
		}
		c.summary.Tasks[task.ID] = task
		c.events.Write("task.created", map[string]any{"round": round, "task": task})
		added++
	}
	return added
}

func (c *controller) recordRejectedTaskSpec(spec TaskSpec, round int, index int, cause error) {
	taskID := c.rejectedTaskID(spec, round, index)
	task := TaskState{
		ID:             taskID,
		Worker:         spec.Worker,
		Objective:      spec.Objective,
		ContextRefs:    spec.ContextRefs,
		DependsOn:      spec.DependsOn,
		OutputContract: spec.OutputContract,
		Status:         TaskRejected,
		RoundCreated:   round,
		Error:          cause.Error(),
		Verification: VerificationResult{
			Passed:  false,
			Reasons: []string{cause.Error()},
		},
	}
	c.summary.Tasks[task.ID] = task
	c.events.Write("task.rejected", map[string]any{"round": round, "task": task, "reason": cause.Error(), "source": "lead_task_spec"})
}

func (c *controller) rejectedTaskID(spec TaskSpec, round int, index int) string {
	if validTeamNameRe.MatchString(spec.ID) {
		if _, exists := c.summary.Tasks[spec.ID]; !exists {
			return spec.ID
		}
	}
	base := fmt.Sprintf("invalid-task-r%d-%d", round, index)
	if _, exists := c.summary.Tasks[base]; !exists {
		return base
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s-%d", base, suffix)
		if _, exists := c.summary.Tasks[candidate]; !exists {
			return candidate
		}
	}
}

func (c *controller) validateTaskSpec(spec TaskSpec) error {
	if strings.TrimSpace(spec.ID) == "" {
		return fmt.Errorf("task id is required")
	}
	if !validTeamNameRe.MatchString(spec.ID) {
		return fmt.Errorf("task id %q must match %s", spec.ID, validTeamNameRe.String())
	}
	if _, ok := c.manifest.Workers[spec.Worker]; !ok {
		return fmt.Errorf("task %q references undeclared worker %q", spec.ID, spec.Worker)
	}
	if strings.TrimSpace(spec.Objective) == "" {
		return fmt.Errorf("task %q objective is required", spec.ID)
	}
	for _, dep := range spec.DependsOn {
		if _, ok := c.summary.Tasks[dep]; !ok {
			return fmt.Errorf("task %q depends on unknown task %q", spec.ID, dep)
		}
	}
	for _, ref := range spec.ContextRefs {
		if strings.HasPrefix(ref, "task:") {
			taskID := strings.TrimPrefix(ref, "task:")
			if _, ok := c.summary.Tasks[taskID]; !ok {
				return fmt.Errorf("task %q references unknown context task %q", spec.ID, taskID)
			}
		}
	}
	if c.workerTaskCount(spec.Worker)+1 > c.workerMaxTasks(spec.Worker) {
		return fmt.Errorf("worker %q maxTasks exceeded", spec.Worker)
	}
	return nil
}

func (c *controller) readyTasks() []*TaskState {
	c.blockTasksWithFailedDependencies()
	ids := make([]string, 0, len(c.summary.Tasks))
	for id := range c.summary.Tasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	ready := []*TaskState{}
	for _, id := range ids {
		task := c.summary.Tasks[id]
		if task.Status != TaskPlanned && task.Status != TaskReady && task.Status != TaskRetryScheduled {
			continue
		}
		if c.dependenciesVerified(task) {
			task.Status = TaskReady
			c.summary.Tasks[id] = task
			copy := task
			ready = append(ready, &copy)
		}
	}
	return ready
}

func (c *controller) blockTasksWithFailedDependencies() int {
	blocked := 0
	for {
		changed := false
		ids := make([]string, 0, len(c.summary.Tasks))
		for id := range c.summary.Tasks {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			task := c.summary.Tasks[id]
			if task.Status != TaskPlanned && task.Status != TaskReady && task.Status != TaskRetryScheduled {
				continue
			}
			dep, status, ok := c.failedDependency(task)
			if !ok {
				continue
			}
			reason := fmt.Sprintf("dependency %q is %s", dep, status)
			task.Status = TaskBlocked
			task.Error = reason
			task.Verification = VerificationResult{Passed: false, Reasons: []string{reason}}
			c.summary.Tasks[id] = task
			c.events.Write("task.blocked", map[string]any{"task_id": id, "dependency": dep, "dependency_status": status, "reason": reason})
			blocked++
			changed = true
		}
		if !changed {
			return blocked
		}
	}
}

func (c *controller) failedDependency(task TaskState) (string, string, bool) {
	for _, dep := range task.DependsOn {
		depTask, ok := c.summary.Tasks[dep]
		if !ok {
			return dep, "missing", true
		}
		if depTask.Status == TaskRejected || depTask.Status == TaskBlocked {
			return dep, depTask.Status, true
		}
	}
	return "", "", false
}

func (c *controller) dependenciesVerified(task TaskState) bool {
	for _, dep := range task.DependsOn {
		depTask, ok := c.summary.Tasks[dep]
		if !ok || depTask.Status != TaskVerified {
			return false
		}
	}
	return true
}

func (c *controller) dispatchTasks(ctx context.Context, tasks []*TaskState) error {
	for start := 0; start < len(tasks); start += c.manifest.Runtime.MaxParallel {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := start + c.manifest.Runtime.MaxParallel
		if end > len(tasks) {
			end = len(tasks)
		}
		batch := tasks[start:end]
		var wg sync.WaitGroup
		results := make(chan TaskState, len(batch))
		for _, task := range batch {
			task := *task
			wg.Add(1)
			go func() {
				defer wg.Done()
				results <- c.runTask(ctx, task)
			}()
		}
		wg.Wait()
		close(results)
		for result := range results {
			c.summary.Tasks[result.ID] = result
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (c *controller) runTask(ctx context.Context, task TaskState) TaskState {
	task.Attempts++
	task.Status = TaskRunning
	c.events.Write("task.started", map[string]any{"task_id": task.ID, "worker": task.Worker, "attempt": task.Attempts})
	worker := c.manifest.Workers[task.Worker]
	child, err := c.runChild(ctx, worker.Agent, "task-"+task.ID, "worker", task.ID, c.workerInput(task))
	if err != nil {
		task.RunID = ""
		task.RunDir = ""
		task.Final = ""
		task.Status = TaskRejected
		task.Error = err.Error()
		task.Verification = VerificationResult{Passed: false, Reasons: []string{err.Error()}}
		if cancelErr := cancellationErr(ctx, err); cancelErr != nil {
			task.Status = TaskBlocked
			task.Error = cancelErr.Error()
			task.Verification = VerificationResult{Passed: false, Reasons: []string{task.Error}}
			c.events.Write("task.blocked", map[string]any{"task_id": task.ID, "reason": task.Error})
			return task
		}
		c.events.Write("task.rejected", map[string]any{"task_id": task.ID, "reason": err.Error()})
		return task
	}
	c.taskMu.Lock()
	c.recordChildRun(child)
	c.taskMu.Unlock()
	task.RunID = child.RunID
	task.RunDir = child.RunDir
	task.Final = strings.TrimSpace(child.Final)
	task.Status = TaskCompleted
	c.events.Write("task.completed", map[string]any{"task_id": task.ID, "run_id": task.RunID, "status": child.Status})
	task.Verification = c.verifyTask(task, child)
	if task.Verification.Passed {
		task.Status = TaskVerified
		c.events.Write("task.verified", map[string]any{"task_id": task.ID})
		return task
	}
	if task.Attempts <= c.manifest.Runtime.MaxRetriesPerTask {
		task.Status = TaskRetryScheduled
		c.events.Write("task.rejected", map[string]any{"task_id": task.ID, "retry": true, "reasons": task.Verification.Reasons})
		return task
	}
	task.Status = TaskRejected
	c.events.Write("task.rejected", map[string]any{"task_id": task.ID, "retry": false, "reasons": task.Verification.Reasons})
	return task
}

func cancellationErr(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func (c *controller) verifyTask(task TaskState, child childRunResult) VerificationResult {
	reasons := []string{}
	if child.Status != string(runtime.StatusCompleted) {
		reasons = append(reasons, "child run did not complete")
	}
	if strings.TrimSpace(task.Final) == "" {
		reasons = append(reasons, "task final answer is empty")
	}
	if c.manifest.Verification.RequireStructuredTaskOutput {
		fields := task.OutputContract.RequiredFields
		if len(fields) == 0 {
			fields = c.manifest.Verification.RequiredTaskFields
		}
		var output map[string]any
		if err := json.Unmarshal([]byte(extractJSONObject(task.Final)), &output); err != nil {
			reasons = append(reasons, "task final answer is not valid JSON")
		} else {
			for _, field := range fields {
				value, ok := output[field]
				if !ok || isEmptyJSONValue(value) {
					reasons = append(reasons, "missing required field "+field)
				}
			}
		}
	}
	return VerificationResult{Passed: len(reasons) == 0, Reasons: reasons}
}

func (c *controller) finalize() (*Result, error) {
	now := time.Now()
	c.summary.EndedAt = now.Format(time.RFC3339Nano)
	if c.summary.Status == "" {
		c.summary.Status = StatusCompleted
	}
	summaryPath := filepath.Join(c.runDir, "team.summary.json")
	data, err := json.MarshalIndent(c.summary, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(summaryPath, data, 0o644); err != nil {
		return nil, err
	}
	reportPath := filepath.Join(c.runDir, "report.html")
	if err := writeReport(reportPath, c.summary); err != nil {
		return nil, err
	}
	c.events.Write("team.completed", map[string]any{"status": c.summary.Status, "summary": "team.summary.json", "report": "report.html"})
	return &Result{
		TeamRunID: c.id,
		OutputDir: c.runDir,
		Report:    reportPath,
		Status:    c.summary.Status,
		Final:     c.summary.Final,
		Summary:   c.summary,
	}, nil
}

func (c *controller) leadDecisionInput(round int) string {
	var b strings.Builder
	b.WriteString("# Jeju Team Lead Decision\n\n")
	b.WriteString(fmt.Sprintf("round: %d\n", round))
	b.WriteString(fmt.Sprintf("max_rounds: %d\n", c.manifest.Runtime.MaxRounds))
	b.WriteString(fmt.Sprintf("max_tasks: %d\n", c.manifest.Runtime.MaxTasks))
	b.WriteString(fmt.Sprintf("require_verifier: %t\n", c.manifest.Verification.RequireVerifier))
	if c.manifest.Verification.RequireVerifier && !c.hasVerifiedWorker(VerifierWorkerName) {
		b.WriteString("verifier_status: missing_verified_verifier_task\n")
		b.WriteString("synthesis_rule: do not return decision=synthesize until a verifier worker task is verified\n")
	}
	b.WriteString("\n# Team Goal\n")
	b.WriteString(c.goal)
	b.WriteString("\n\n# Worker Catalog\n")
	for _, name := range c.workerNames() {
		worker := c.manifest.Workers[name]
		b.WriteString(fmt.Sprintf("- %s: %s (maxTasks=%d)\n", name, worker.Description, c.workerMaxTasks(name)))
	}
	b.WriteString("\n# Current Team State\n")
	b.WriteString(c.stateJSONForLead())
	b.WriteString("\n\n# Required Output\n")
	b.WriteString("Return only a JSON object with fields: decision, round_summary, tasks, finish.\n")
	b.WriteString("Use decision=continue to create tasks, synthesize to stop dispatching, or blocked when the team cannot proceed.\n")
	b.WriteString("Every task must choose one declared worker and include id, worker, objective, context_refs, depends_on, and output_contract.\n")
	return b.String()
}

func (c *controller) workerInput(task TaskState) string {
	var b strings.Builder
	b.WriteString("# Jeju Team Worker Task\n\n")
	b.WriteString(fmt.Sprintf("task_id: %s\nworker: %s\n\n", task.ID, task.Worker))
	b.WriteString("# Team Goal\n")
	b.WriteString(c.goal)
	b.WriteString("\n\n# Assigned Task\n")
	b.WriteString(task.Objective)
	b.WriteString("\n\n# Output Contract\n")
	fields := task.OutputContract.RequiredFields
	if len(fields) == 0 {
		fields = c.manifest.Verification.RequiredTaskFields
	}
	if len(fields) > 0 {
		b.WriteString("Return only JSON with required fields: ")
		b.WriteString(strings.Join(fields, ", "))
		b.WriteString(".\n")
	}
	if len(task.ContextRefs) > 0 {
		b.WriteString("\n# Context Refs\n")
		for _, ref := range task.ContextRefs {
			b.WriteString("- ")
			b.WriteString(ref)
			if text := c.contextRefText(ref); text != "" {
				b.WriteString("\n")
				b.WriteString(indent(text, "  "))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\n# Boundaries\nUse your configured Jeju tools and permissions. Do not inspect unrelated areas unless needed for this assigned task.\n")
	return b.String()
}

func (c *controller) synthesisInput() string {
	var b strings.Builder
	b.WriteString("# Jeju Team Synthesis\n\n")
	b.WriteString("# Team Goal\n")
	b.WriteString(c.goal)
	b.WriteString("\n\n# Final Team State\n")
	b.WriteString(c.stateJSON())
	b.WriteString("\n\nProduce the final answer from verified worker outputs. Identify unresolved gaps and residual risk.\n")
	return b.String()
}

func (c *controller) stateJSON() string {
	data, err := json.MarshalIndent(c.summary, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (c *controller) stateJSONForLead() string {
	summary := c.summary
	summary.ChildRuns = nil
	summary.Final = truncateTeamStateText(summary.Final, leadStateTaskFinalLimit)
	tasks := make(map[string]TaskState, len(c.summary.Tasks))
	for id, task := range c.summary.Tasks {
		task.Final = truncateTeamStateText(task.Final, leadStateTaskFinalLimit)
		task.Error = truncateTeamStateText(task.Error, leadStateTaskErrorLimit)
		tasks[id] = task
	}
	summary.Tasks = tasks
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}

func truncateTeamStateText(text string, maxChars int) string {
	if maxChars <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text
	}
	return string(runes[:maxChars]) + fmt.Sprintf("\n[Jeju truncated team state: omitted approximately %d characters]", len(runes)-maxChars)
}

func (c *controller) contextRefText(ref string) string {
	if !strings.HasPrefix(ref, "task:") {
		return ""
	}
	taskID := strings.TrimPrefix(ref, "task:")
	task, ok := c.summary.Tasks[taskID]
	if !ok {
		return ""
	}
	return task.Final
}

func (c *controller) workerNames() []string {
	names := make([]string, 0, len(c.manifest.Workers))
	for name := range c.manifest.Workers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *controller) workerMaxTasks(name string) int {
	worker := c.manifest.Workers[name]
	if worker.MaxTasks > 0 {
		return worker.MaxTasks
	}
	return c.manifest.Runtime.MaxTasks
}

func (c *controller) workerTaskCount(name string) int {
	count := 0
	for _, task := range c.summary.Tasks {
		if task.Worker == name && taskCountsAgainstLimit(task) {
			count++
		}
	}
	return count
}

func (c *controller) taskLimitCount() int {
	count := 0
	for _, task := range c.summary.Tasks {
		if taskCountsAgainstLimit(task) {
			count++
		}
	}
	return count
}

func taskCountsAgainstLimit(task TaskState) bool {
	return !(task.Status == TaskRejected && task.Attempts == 0 && task.RunID == "")
}

func (c *controller) hasUnsuccessfulTasks() bool {
	for _, task := range c.summary.Tasks {
		switch task.Status {
		case TaskRejected, TaskBlocked, TaskRetryScheduled, TaskPlanned, TaskReady, TaskRunning:
			return true
		}
	}
	return false
}

func (c *controller) pendingTaskReason() string {
	counts := map[string]int{}
	for _, task := range c.summary.Tasks {
		switch task.Status {
		case TaskPlanned, TaskReady, TaskRunning, TaskRetryScheduled:
			counts[task.Status]++
		}
	}
	if len(counts) == 0 {
		return ""
	}
	statuses := []string{TaskPlanned, TaskReady, TaskRunning, TaskRetryScheduled}
	parts := []string{}
	for _, status := range statuses {
		if count := counts[status]; count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s task(s)", count, status))
		}
	}
	return "unresolved tasks remain: " + strings.Join(parts, ", ")
}

func (c *controller) hasVerifiedWorker(worker string) bool {
	for _, task := range c.summary.Tasks {
		if task.Worker == worker && task.Status == TaskVerified {
			return true
		}
	}
	return false
}

func parseTeamDecision(text string) (TeamDecision, error) {
	var decision TeamDecision
	candidate := extractJSONObject(text)
	if err := json.Unmarshal([]byte(candidate), &decision); err != nil {
		return decision, err
	}
	if decision.Decision == "" {
		return decision, fmt.Errorf("decision is required")
	}
	return decision, nil
}

func extractJSONObject(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		lines := strings.Split(text, "\n")
		if len(lines) >= 3 {
			text = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end >= start {
		return text[start : end+1]
	}
	return text
}

func isEmptyJSONValue(value any) bool {
	if value == nil {
		return true
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) == ""
	default:
		return false
	}
}

func statsFromRecord(record trajectory.RunRecord) Stats {
	return Stats{
		ModelCalls:           record.Stats.ModelCalls,
		ToolCalls:            record.Stats.ToolCalls,
		ModelErrors:          record.Stats.ModelErrors,
		ToolErrors:           record.Stats.ToolErrors,
		PermissionDenied:     record.Stats.PermissionDenied,
		PromptTokens:         record.Stats.PromptTokens,
		PromptCacheHitTokens: record.Stats.PromptCacheHitTokens,
		CompletionTokens:     record.Stats.CompletionTokens,
		TotalTokens:          record.Stats.TotalTokens,
		DurationMS:           record.DurationMS,
	}
}

func (s *Stats) add(other Stats) {
	s.ModelCalls += other.ModelCalls
	s.ToolCalls += other.ToolCalls
	s.ModelErrors += other.ModelErrors
	s.ToolErrors += other.ToolErrors
	s.PermissionDenied += other.PermissionDenied
	s.PromptTokens += other.PromptTokens
	s.PromptCacheHitTokens += other.PromptCacheHitTokens
	s.CompletionTokens += other.CompletionTokens
	s.TotalTokens += other.TotalTokens
	s.DurationMS += other.DurationMS
}

func resultStatus(status runtime.RunStatus) string {
	return string(status)
}

func sanitizeName(name string) string {
	name = strings.Trim(regexpNonName.ReplaceAllString(name, "-"), "-")
	if name == "" {
		return "team"
	}
	return strings.ToLower(name)
}

var regexpNonName = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func indent(text, prefix string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
