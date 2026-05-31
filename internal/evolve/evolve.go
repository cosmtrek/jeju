package evolve

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"jeju/internal/compiler"
	"jeju/internal/config"
	"jeju/internal/evaluate"
	"jeju/internal/runs"
	"jeju/internal/runtime"
	"jeju/internal/trajectory"

	"gopkg.in/yaml.v3"
)

type Experiment struct {
	APIVersion string         `yaml:"apiVersion"`
	Kind       string         `yaml:"kind"`
	Metadata   Metadata       `yaml:"metadata"`
	Target     TargetSpec     `yaml:"target"`
	Data       DataSpec       `yaml:"data"`
	Objective  ObjectiveSpec  `yaml:"objective"`
	Evolver    EvolverSpec    `yaml:"evolver"`
	Search     SearchSpec     `yaml:"search"`
	Output     OutputSpec     `yaml:"output"`
	Extensions map[string]any `yaml:"extensions,omitempty"`

	path    string
	baseDir string
}

type Metadata struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
}

type TargetSpec struct {
	Agent     string   `yaml:"agent"`
	Editable  []string `yaml:"editable"`
	Forbidden []string `yaml:"forbidden"`
}

type DataSpec struct {
	Format    string     `yaml:"format"`
	Train     string     `yaml:"train"`
	Selection string     `yaml:"selection"`
	Test      string     `yaml:"test,omitempty"`
	Render    RenderSpec `yaml:"render,omitempty"`
}

type RenderSpec struct {
	Template string `yaml:"template,omitempty"`
}

type ObjectiveSpec struct {
	Metric    string   `yaml:"metric"`
	Direction string   `yaml:"direction"`
	MinDelta  float64  `yaml:"minDelta"`
	Guards    []string `yaml:"guards,omitempty"`
	Guidance  []string `yaml:"guidance,omitempty"`
}

type EvolverSpec struct {
	Agent     string `yaml:"agent"`
	Proposals int    `yaml:"proposals"`
}

type SearchSpec struct {
	Iterations    int        `yaml:"iterations"`
	TrialsPerTask int        `yaml:"trialsPerTask"`
	Parallelism   int        `yaml:"parallelism"`
	Seed          int64      `yaml:"seed,omitempty"`
	Budget        BudgetSpec `yaml:"budget,omitempty"`
}

type BudgetSpec struct {
	MaxRuns        int `yaml:"maxRuns,omitempty"`
	MaxModelTokens int `yaml:"maxModelTokens,omitempty"`
}

type OutputSpec struct {
	Dir string `yaml:"dir"`
}

type TaskCase struct {
	ID       string         `json:"id" yaml:"id"`
	Input    any            `json:"input" yaml:"input"`
	Expected any            `json:"expected,omitempty" yaml:"expected,omitempty"`
	Eval     any            `json:"eval,omitempty" yaml:"eval,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty" yaml:"metadata,omitempty"`
	Weight   float64        `json:"weight,omitempty" yaml:"weight,omitempty"`
}

type Proposal struct {
	ID              string    `json:"id,omitempty"`
	ParentCandidate string    `json:"parent_candidate,omitempty"`
	Hypothesis      string    `json:"hypothesis"`
	Changes         []PatchOp `json:"changes"`
	Confidence      float64   `json:"confidence,omitempty"`
}

type PatchOp struct {
	Target  string `json:"target"`
	Find    string `json:"find"`
	Replace string `json:"replace"`
}

type RunOptions struct {
	Out           string
	MaxIterations int
	DryRun        bool
	BaselineOnly  bool
}

type Result struct {
	ExperimentID string
	OutputDir    string
	BestID       string
	ReportPath   string
}

type controller struct {
	exp           *Experiment
	opts          RunOptions
	outDir        string
	id            string
	events        *eventWriter
	train         []TaskCase
	selection     []TaskCase
	renderer      *template.Template
	history       []historyItem
	usageMu       sync.Mutex
	bundleBaseDir string
	runCount      int
	tokenCount    int
}

type candidate struct {
	ID           string                  `json:"id"`
	Dir          string                  `json:"dir"`
	ManifestPath string                  `json:"manifest_path"`
	Parent       string                  `json:"parent,omitempty"`
	Proposal     *Proposal               `json:"proposal,omitempty"`
	Results      map[string]*SplitResult `json:"results,omitempty"`
	Rejected     bool                    `json:"rejected,omitempty"`
	RejectReason string                  `json:"reject_reason,omitempty"`
}

type SplitResult struct {
	Split        string             `json:"split"`
	Metrics      map[string]float64 `json:"metrics"`
	Trials       []TrialResult      `json:"trials"`
	GuardPass    bool               `json:"guard_pass"`
	GuardReasons []string           `json:"guard_reasons,omitempty"`
}

type TrialResult struct {
	TaskID     string          `json:"task_id"`
	Trial      int             `json:"trial"`
	RunID      string          `json:"run_id,omitempty"`
	RunDir     string          `json:"run_dir,omitempty"`
	Status     string          `json:"status,omitempty"`
	Final      string          `json:"final,omitempty"`
	Evaluation evaluate.Result `json:"evaluation"`
	Stats      RunStats        `json:"stats"`
	Weight     float64         `json:"weight,omitempty"`
	Error      string          `json:"error,omitempty"`
}

type RunStats struct {
	Steps            int     `json:"steps"`
	ModelCalls       int     `json:"model_calls"`
	ToolCalls        int     `json:"tool_calls"`
	ModelErrors      int     `json:"model_errors"`
	ToolErrors       int     `json:"tool_errors"`
	PermissionDenied int     `json:"permission_denied"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	DurationSec      float64 `json:"duration_sec"`
}

type historyItem struct {
	Iteration int                `json:"iteration"`
	Candidate string             `json:"candidate"`
	Accepted  bool               `json:"accepted"`
	Reason    string             `json:"reason,omitempty"`
	Metrics   map[string]float64 `json:"metrics,omitempty"`
	Proposal  *Proposal          `json:"proposal,omitempty"`
}

func LoadFile(path string) (*Experiment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var exp Experiment
	if err := yaml.Unmarshal(data, &exp); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	exp.path = abs
	exp.baseDir = filepath.Dir(abs)
	exp.applyDefaults()
	if err := exp.validate(); err != nil {
		return nil, err
	}
	return &exp, nil
}

func Run(ctx context.Context, specPath string, opts RunOptions) (*Result, error) {
	exp, err := LoadFile(specPath)
	if err != nil {
		return nil, err
	}
	c := &controller{exp: exp, opts: opts}
	return c.run(ctx)
}

func (e *Experiment) applyDefaults() {
	if e.Data.Format == "" {
		e.Data.Format = "jeju.task.v1"
	}
	if e.Evolver.Proposals == 0 {
		e.Evolver.Proposals = 2
	}
	if e.Search.Iterations == 0 {
		e.Search.Iterations = 3
	}
	if e.Search.TrialsPerTask == 0 {
		e.Search.TrialsPerTask = 1
	}
	if e.Search.Parallelism == 0 {
		e.Search.Parallelism = 1
	}
	if e.Output.Dir == "" && e.Metadata.Name != "" {
		e.Output.Dir = filepath.Join(".jeju-dev", "evolve", e.Metadata.Name)
	}
}

func (e *Experiment) validate() error {
	if e.APIVersion != "jeju/v1alpha1" {
		return fmt.Errorf("unsupported apiVersion %q", e.APIVersion)
	}
	if e.Kind != "EvolutionExperiment" {
		return fmt.Errorf("unsupported kind %q", e.Kind)
	}
	if strings.TrimSpace(e.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if strings.TrimSpace(e.Target.Agent) == "" {
		return fmt.Errorf("target.agent is required")
	}
	if len(e.Target.Editable) == 0 {
		return fmt.Errorf("target.editable is required")
	}
	if e.Data.Format != "jeju.task.v1" {
		return fmt.Errorf("data.format %q is not supported", e.Data.Format)
	}
	if e.Data.Train == "" || e.Data.Selection == "" {
		return fmt.Errorf("data.train and data.selection are required")
	}
	if e.Objective.Metric == "" {
		return fmt.Errorf("objective.metric is required")
	}
	if e.Objective.Direction != "maximize" && e.Objective.Direction != "minimize" {
		return fmt.Errorf("objective.direction must be maximize or minimize")
	}
	if e.Evolver.Agent == "" {
		return fmt.Errorf("evolver.agent is required")
	}
	if e.Evolver.Proposals < 1 || e.Evolver.Proposals > 8 {
		return fmt.Errorf("evolver.proposals must be between 1 and 8")
	}
	for _, editable := range e.Target.Editable {
		for _, forbidden := range e.Target.Forbidden {
			if editable == forbidden {
				return fmt.Errorf("target.editable %q conflicts with target.forbidden", editable)
			}
		}
	}
	return nil
}

func (c *controller) run(ctx context.Context) (*Result, error) {
	outDir := c.opts.Out
	if outDir == "" {
		outDir = c.exp.Output.Dir
	}
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(c.exp.baseDir, outDir)
	}
	c.outDir = outDir
	c.id = time.Now().Format("20060102-150405") + "-" + sanitizeName(c.exp.Metadata.Name)
	runDir := filepath.Join(outDir, c.id)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return nil, err
	}
	c.outDir = runDir
	events, err := newEventWriter(filepath.Join(runDir, "events.jsonl"))
	if err != nil {
		return nil, err
	}
	defer events.Close()
	c.events = events
	if err := writeJSON(filepath.Join(runDir, "experiment.snapshot.json"), c.exp); err != nil {
		return nil, err
	}
	c.events.Write("experiment.started", map[string]any{"id": c.id})

	if err := c.loadData(); err != nil {
		return nil, err
	}
	if c.opts.DryRun {
		return c.dryRun(ctx)
	}

	baseline, err := c.createBaseline()
	if err != nil {
		return nil, err
	}
	trainBaseline, err := c.evaluateCandidate(ctx, baseline, "train", c.train, nil)
	if err != nil {
		return nil, err
	}
	baseline.Results["train"] = trainBaseline
	selectionBaseline, err := c.evaluateCandidate(ctx, baseline, "selection", c.selection, nil)
	if err != nil {
		return nil, err
	}
	baseline.Results["selection"] = selectionBaseline
	if err := writeJSON(filepath.Join(baseline.Dir, "results.json"), baseline); err != nil {
		return nil, err
	}
	best := baseline
	bestMetric := selectionBaseline.Metrics[c.exp.Objective.Metric]
	c.history = append(c.history, historyItem{Candidate: baseline.ID, Accepted: true, Reason: "baseline", Metrics: selectionBaseline.Metrics})
	if c.opts.BaselineOnly {
		if err := writeJSON(filepath.Join(c.outDir, "leaderboard.json"), []*candidate{baseline}); err != nil {
			return nil, err
		}
		bestDir := filepath.Join(c.outDir, "best")
		if err := os.RemoveAll(bestDir); err != nil {
			return nil, err
		}
		if err := copyBundle(baseline.Dir, bestDir); err != nil {
			return nil, err
		}
		if err := writeJSON(filepath.Join(bestDir, "results.json"), baseline); err != nil {
			return nil, err
		}
		report, err := c.writeReport(best, []*candidate{baseline})
		if err != nil {
			return nil, err
		}
		return &Result{ExperimentID: c.id, OutputDir: c.outDir, BestID: best.ID, ReportPath: report}, nil
	}

	allCandidates := []*candidate{baseline}
	iterations := c.exp.Search.Iterations
	if c.opts.MaxIterations > 0 {
		iterations = c.opts.MaxIterations
	}
	noImprove := 0
	for iter := 1; iter <= iterations; iter++ {
		runCount, tokenCount := c.usageSnapshot()
		if c.exp.Search.Budget.MaxRuns > 0 && runCount >= c.exp.Search.Budget.MaxRuns {
			c.events.Write("budget.exhausted", map[string]any{"max_runs": c.exp.Search.Budget.MaxRuns})
			break
		}
		if c.exp.Search.Budget.MaxModelTokens > 0 && tokenCount >= c.exp.Search.Budget.MaxModelTokens {
			c.events.Write("budget.exhausted", map[string]any{"max_model_tokens": c.exp.Search.Budget.MaxModelTokens})
			break
		}
		iterationParent := best
		proposals, err := c.propose(ctx, iter, best)
		if err != nil {
			return nil, err
		}
		improved := false
		for i, proposal := range proposals {
			cand, err := c.createCandidate(iter, i+1, iterationParent, proposal)
			if err != nil {
				c.history = append(c.history, historyItem{Iteration: iter, Candidate: proposal.ID, Accepted: false, Reason: err.Error(), Proposal: &proposal})
				continue
			}
			allCandidates = append(allCandidates, cand)
			trainResult, err := c.evaluateCandidate(ctx, cand, "train", c.train, trainBaseline.Metrics)
			if err != nil {
				return nil, err
			}
			cand.Results["train"] = trainResult
			if !trainResult.GuardPass {
				c.reject(cand, "train guards failed: "+strings.Join(trainResult.GuardReasons, "; "))
				continue
			}
			selectionResult, err := c.evaluateCandidate(ctx, cand, "selection", c.selection, selectionBaseline.Metrics)
			if err != nil {
				return nil, err
			}
			cand.Results["selection"] = selectionResult
			if !selectionResult.GuardPass {
				c.reject(cand, "selection guards failed: "+strings.Join(selectionResult.GuardReasons, "; "))
				continue
			}
			metric := selectionResult.Metrics[c.exp.Objective.Metric]
			if isImproved(metric, bestMetric, c.exp.Objective.Direction, c.exp.Objective.MinDelta) {
				best = cand
				bestMetric = metric
				improved = true
				c.history = append(c.history, historyItem{Iteration: iter, Candidate: cand.ID, Accepted: true, Metrics: selectionResult.Metrics, Proposal: cand.Proposal})
				c.events.Write("candidate.accepted", map[string]any{"candidate": cand.ID, "metric": metric})
			} else {
				c.reject(cand, "objective did not improve by minDelta")
			}
			if err := writeJSON(filepath.Join(cand.Dir, "results.json"), cand); err != nil {
				return nil, err
			}
		}
		if improved {
			noImprove = 0
		} else {
			noImprove++
		}
		if noImprove >= 3 {
			c.events.Write("search.stopped", map[string]any{"reason": "no improvement"})
			break
		}
	}
	bestDir := filepath.Join(c.outDir, "best")
	if err := os.RemoveAll(bestDir); err != nil {
		return nil, err
	}
	if err := copyBundle(best.Dir, bestDir); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(bestDir, "results.json"), best); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(c.outDir, "leaderboard.json"), allCandidates); err != nil {
		return nil, err
	}
	report, err := c.writeReport(best, allCandidates)
	if err != nil {
		return nil, err
	}
	c.events.Write("experiment.completed", map[string]any{"best": best.ID})
	return &Result{ExperimentID: c.id, OutputDir: c.outDir, BestID: best.ID, ReportPath: report}, nil
}

func (c *controller) dryRun(ctx context.Context) (*Result, error) {
	baseline, err := c.createBaseline()
	if err != nil {
		return nil, err
	}
	_, err = compiler.CompileWithOptions(baseline.ManifestPath, compiler.Options{
		RunStore:          runs.NewStore(filepath.Join(baseline.Dir, "dry-run-runs")),
		WorkspaceOverride: filepath.Join(baseline.Dir, "dry-run-workspace"),
	})
	if err != nil {
		return nil, err
	}
	c.events.Write("dry_run.completed", map[string]any{"candidate": baseline.ID})
	report, err := c.writeReport(baseline, []*candidate{baseline})
	if err != nil {
		return nil, err
	}
	_ = ctx
	return &Result{ExperimentID: c.id, OutputDir: c.outDir, BestID: baseline.ID, ReportPath: report}, nil
}

func (c *controller) reject(cand *candidate, reason string) {
	cand.Rejected = true
	cand.RejectReason = reason
	c.history = append(c.history, historyItem{Candidate: cand.ID, Accepted: false, Reason: reason, Metrics: candMetricSnapshot(cand), Proposal: cand.Proposal})
	c.events.Write("candidate.rejected", map[string]any{"candidate": cand.ID, "reason": reason})
	_ = writeJSON(filepath.Join(cand.Dir, "results.json"), cand)
}

func candMetricSnapshot(cand *candidate) map[string]float64 {
	if cand.Results == nil || cand.Results["selection"] == nil {
		return nil
	}
	return cand.Results["selection"].Metrics
}

func (c *controller) loadData() error {
	var err error
	c.train, err = loadTasks(c.resolvePath(c.exp.Data.Train))
	if err != nil {
		return fmt.Errorf("load train: %w", err)
	}
	c.selection, err = loadTasks(c.resolvePath(c.exp.Data.Selection))
	if err != nil {
		return fmt.Errorf("load selection: %w", err)
	}
	if c.exp.Data.Render.Template != "" {
		data, err := os.ReadFile(c.resolvePath(c.exp.Data.Render.Template))
		if err != nil {
			return err
		}
		c.renderer, err = template.New(filepath.Base(c.exp.Data.Render.Template)).Parse(string(data))
		if err != nil {
			return err
		}
	}
	return nil
}

func loadTasks(path string) ([]TaskCase, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var tasks []TaskCase
	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var task TaskCase
		if err := json.Unmarshal([]byte(line), &task); err != nil {
			return nil, err
		}
		if task.ID == "" {
			return nil, fmt.Errorf("task id is required")
		}
		if seen[task.ID] {
			return nil, fmt.Errorf("duplicate task id %q", task.ID)
		}
		seen[task.ID] = true
		if task.Weight == 0 {
			task.Weight = 1
		}
		tasks = append(tasks, task)
	}
	return tasks, scanner.Err()
}

func (c *controller) createBaseline() (*candidate, error) {
	dir := filepath.Join(c.outDir, "baseline")
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	manifest, err := c.createBundleFromSource(dir)
	if err != nil {
		return nil, err
	}
	return &candidate{ID: "baseline", Dir: dir, ManifestPath: manifest, Results: map[string]*SplitResult{}}, nil
}

func (c *controller) createCandidate(iteration, index int, parent *candidate, proposal Proposal) (*candidate, error) {
	id := fmt.Sprintf("candidate-%03d-%02d", iteration, index)
	dir := filepath.Join(c.outDir, "iterations", fmt.Sprintf("%03d", iteration), id)
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := copyBundle(parent.Dir, dir); err != nil {
		return nil, err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(dir)
		}
	}()
	manifestPath := filepath.Join(dir, relPath(c.bundleBase(), c.resolvePath(c.exp.Target.Agent)))
	if proposal.ID == "" {
		proposal.ID = fmt.Sprintf("proposal-%03d-%02d", iteration, index)
	}
	proposal.ParentCandidate = parent.ID
	if err := c.applyProposal(manifestPath, proposal); err != nil {
		return nil, err
	}
	if _, err := compiler.CompileWithOptions(manifestPath, compiler.Options{
		RunStore:          runs.NewStore(filepath.Join(dir, "compile-runs")),
		WorkspaceOverride: filepath.Join(dir, "compile-workspace"),
	}); err != nil {
		return nil, err
	}
	if err := writeJSON(filepath.Join(dir, "patch.json"), proposal); err != nil {
		return nil, err
	}
	cleanup = false
	return &candidate{ID: id, Dir: dir, ManifestPath: manifestPath, Parent: parent.ID, Proposal: &proposal, Results: map[string]*SplitResult{}}, nil
}

func (c *controller) createBundleFromSource(dst string) (string, error) {
	agentPath := c.resolvePath(c.exp.Target.Agent)
	manifest, _, err := config.LoadFile(agentPath)
	if err != nil {
		return "", err
	}
	paths := []string{agentPath, manifest.Instructions.System, manifest.Workspace.Path}
	paths = append(paths, manifest.Skills.Dirs...)
	for _, tool := range manifest.Tools {
		if tool.Command.Run != "" {
			paths = append(paths, tool.Command.Run)
		}
		for _, arg := range tool.Command.Args {
			if filepath.IsAbs(arg) {
				paths = append(paths, arg)
			}
		}
		if schemaPath, ok := tool.Input.Schema.(string); ok {
			paths = append(paths, schemaPath)
		}
	}
	for _, ev := range manifest.Evaluate.Evaluators {
		if ev.Prompt != "" {
			paths = append(paths, ev.Prompt)
		}
		if ev.Command.Run != "" {
			paths = append(paths, ev.Command.Run)
		}
		for _, arg := range ev.Command.Args {
			if filepath.IsAbs(arg) {
				paths = append(paths, arg)
			}
		}
	}
	paths = uniqueStrings(paths)
	c.bundleBaseDir = commonAncestor(paths)
	for _, src := range paths {
		if src == "" {
			continue
		}
		rel, ok := relUnder(c.bundleBase(), src)
		if !ok {
			return "", fmt.Errorf("referenced path %q is outside bundle root %q", src, c.bundleBase())
		}
		dstPath := filepath.Join(dst, rel)
		if err := copyPath(src, dstPath); err != nil {
			return "", err
		}
	}
	relAgent, _ := relUnder(c.bundleBase(), agentPath)
	return filepath.Join(dst, relAgent), nil
}

func (c *controller) applyProposal(manifestPath string, proposal Proposal) error {
	before, err := manifestLeafSnapshot(manifestPath)
	if err != nil {
		return err
	}
	for _, change := range proposal.Changes {
		if err := c.applyPatch(manifestPath, change); err != nil {
			return err
		}
	}
	after, err := manifestLeafSnapshot(manifestPath)
	if err != nil {
		return err
	}
	return c.validateManifestChanges(before, after)
}

func (c *controller) applyPatch(manifestPath string, change PatchOp) error {
	if change.Target == "" || change.Find == "" {
		return fmt.Errorf("patch target and find are required")
	}
	if !matchesAny(change.Target, c.exp.Target.Editable) {
		return fmt.Errorf("target %q is not editable", change.Target)
	}
	if matchesAny(change.Target, c.exp.Target.Forbidden) {
		return fmt.Errorf("target %q is forbidden", change.Target)
	}
	targetPath := manifestPath
	if change.Target == "instructions.system" {
		manifest, _, err := config.LoadFile(manifestPath)
		if err != nil {
			return err
		}
		targetPath = manifest.Instructions.System
	}
	if !isUnder(c.bundleRootFromManifest(manifestPath), targetPath) {
		return fmt.Errorf("patch target %q resolves outside candidate bundle", change.Target)
	}
	data, err := os.ReadFile(targetPath)
	if err != nil {
		return err
	}
	content := string(data)
	count := strings.Count(content, change.Find)
	if count != 1 {
		return fmt.Errorf("patch target %q find must match exactly once, got %d", change.Target, count)
	}
	updated := strings.Replace(content, change.Find, change.Replace, 1)
	return os.WriteFile(targetPath, []byte(updated), 0o644)
}

func manifestLeafSnapshot(manifestPath string) (map[string]string, error) {
	snapshot := map[string]string{}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("read manifest field snapshot: %w", err)
	}
	flattenLeafConfigValue("", normalizeConfigValue(raw), snapshot)
	return snapshot, nil
}

func (c *controller) validateManifestChanges(before, after map[string]string) error {
	seen := map[string]bool{}
	for path, beforeValue := range before {
		seen[path] = true
		afterValue, ok := after[path]
		if !ok || afterValue != beforeValue {
			if matchesPathOrDescendant(path, c.exp.Target.Forbidden) {
				return fmt.Errorf("forbidden field %q changed by patch", path)
			}
			if !matchesPathOrDescendant(path, c.exp.Target.Editable) {
				return fmt.Errorf("manifest field %q is not editable", path)
			}
		}
	}
	for path := range after {
		if !seen[path] {
			if matchesPathOrDescendant(path, c.exp.Target.Forbidden) {
				return fmt.Errorf("forbidden field %q changed by patch", path)
			}
			if !matchesPathOrDescendant(path, c.exp.Target.Editable) {
				return fmt.Errorf("manifest field %q is not editable", path)
			}
		}
	}
	return nil
}

func flattenLeafConfigValue(path string, value any, out map[string]string) {
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 && path != "" {
			out[path] = canonicalConfigValue(value)
			return
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childPath := key
			if path != "" {
				childPath = path + "." + key
			}
			flattenLeafConfigValue(childPath, typed[key], out)
		}
	case []any:
		if len(typed) == 0 && path != "" {
			out[path] = canonicalConfigValue(value)
			return
		}
		for i, item := range typed {
			flattenLeafConfigValue(fmt.Sprintf("%s[%d]", path, i), item, out)
		}
	default:
		if path != "" {
			out[path] = canonicalConfigValue(value)
		}
	}
}

func canonicalConfigValue(value any) string {
	data, _ := json.Marshal(normalizeConfigValue(value))
	return string(data)
}

func normalizeConfigValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[key] = normalizeConfigValue(item)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[fmt.Sprint(key)] = normalizeConfigValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = normalizeConfigValue(item)
		}
		return out
	default:
		return value
	}
}

func (c *controller) evaluateCandidate(ctx context.Context, cand *candidate, split string, tasks []TaskCase, baseline map[string]float64) (*SplitResult, error) {
	result := &SplitResult{Split: split, GuardPass: true}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("%s split has no tasks", split)
	}
	type trialJob struct {
		task  TaskCase
		trial int
	}
	var jobs []trialJob
	for _, task := range tasks {
		for trial := 1; trial <= c.exp.Search.TrialsPerTask; trial++ {
			jobs = append(jobs, trialJob{task: task, trial: trial})
		}
	}
	parallelism := c.exp.Search.Parallelism
	if parallelism < 1 {
		parallelism = 1
	}
	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for _, job := range jobs {
		job := job
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			trialResult, err := c.runTrial(ctx, cand, split, job.task, job.trial)
			mu.Lock()
			defer mu.Unlock()
			if err != nil && firstErr == nil {
				firstErr = err
			}
			result.Trials = append(result.Trials, trialResult)
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(result.Trials, func(i, j int) bool {
		if result.Trials[i].TaskID == result.Trials[j].TaskID {
			return result.Trials[i].Trial < result.Trials[j].Trial
		}
		return result.Trials[i].TaskID < result.Trials[j].TaskID
	})
	metrics, err := c.extractMetrics(result.Trials)
	if err != nil {
		return nil, err
	}
	result.Metrics = metrics
	if baseline != nil {
		pass, reasons, err := c.evaluateGuards(metrics, baseline)
		if err != nil {
			return nil, err
		}
		result.GuardPass = pass
		result.GuardReasons = reasons
	}
	return result, nil
}

func (c *controller) runTrial(ctx context.Context, cand *candidate, split string, task TaskCase, trial int) (TrialResult, error) {
	trialDir := filepath.Join(cand.Dir, "tasks", sanitizeName(task.ID), fmt.Sprintf("trial-%02d", trial))
	workspaceDir := filepath.Join(trialDir, "workspace")
	runStore := runs.NewStore(filepath.Join(trialDir, "runs"))
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return TrialResult{}, err
	}
	agent, err := compiler.CompileWithOptions(cand.ManifestPath, compiler.Options{
		RunStore:          runStore,
		WorkspaceOverride: workspaceDir,
	})
	if err != nil {
		return TrialResult{}, err
	}
	input, err := c.renderInput(task)
	if err != nil {
		return TrialResult{}, err
	}
	auto := ""
	rt := runtime.NewWithOptions(runtime.Options{AutoApprove: true, AutoUserInput: &auto})
	runResult, err := rt.Run(ctx, agent, input)
	tr := TrialResult{TaskID: task.ID, Trial: trial}
	if err != nil {
		tr.Error = err.Error()
		return tr, nil
	}
	runDir, err := runStore.LoadRun(runResult.RunID)
	if err != nil {
		return TrialResult{}, err
	}
	events, err := trajectory.ReadFile(filepath.Join(runDir.Path, runs.TrajectoryFile))
	if err != nil {
		return TrialResult{}, err
	}
	stats := statsFromEvents(events)
	if meta, err := runStore.ReadMetadata(runResult.RunID); err == nil {
		stats.DurationSec = durationSec(meta)
	}
	evalResult, err := c.effectiveEvaluation(ctx, agent, runStore, runResult, input, task, stats)
	if err != nil {
		return TrialResult{}, err
	}
	c.recordUsage(stats)
	tr.RunID = runResult.RunID
	tr.RunDir = runDir.Path
	tr.Status = string(runResult.Status)
	tr.Final = runResult.Final
	tr.Evaluation = evalResult
	tr.Stats = stats
	tr.Weight = task.Weight
	c.events.Write("trial.completed", map[string]any{"candidate": cand.ID, "split": split, "task": task.ID, "trial": trial, "run_id": runResult.RunID})
	return tr, nil
}

func (c *controller) recordUsage(stats RunStats) {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	c.runCount++
	c.tokenCount += stats.TotalTokens
}

func (c *controller) usageSnapshot() (int, int) {
	c.usageMu.Lock()
	defer c.usageMu.Unlock()
	return c.runCount, c.tokenCount
}

func (c *controller) effectiveEvaluation(ctx context.Context, agent *compiler.CompiledAgent, store *runs.Store, runResult *runtime.RunResult, input string, task TaskCase, stats RunStats) (evaluate.Result, error) {
	evaluators := []evaluate.EvaluatorResult{}
	if taskEval, ok := expectationEvaluator(task.Expected, runResult.Final); ok {
		evaluators = append(evaluators, taskEval)
	}
	if len(agent.Evaluators) > 0 {
		result, err := evaluate.Run(ctx, runResult.RunID, agent.Evaluators, evaluate.Context{
			RunID:            runResult.RunID,
			Input:            input,
			Status:           string(runResult.Status),
			Final:            runResult.Final,
			Expected:         task.Expected,
			Eval:             task.Eval,
			Metadata:         task.Metadata,
			Steps:            stats.Steps,
			ToolCalls:        stats.ToolCalls,
			ModelErrors:      stats.ModelErrors,
			ToolErrors:       stats.ToolErrors,
			PermissionDenied: stats.PermissionDenied,
			MaxSteps:         agent.Config.Runtime.Limits.MaxSteps,
			MaxToolCalls:     agent.Config.Runtime.Limits.MaxToolCalls,
		})
		if err != nil {
			return evaluate.Result{}, err
		}
		evaluators = append(evaluators, result.Evaluators...)
	}
	if len(evaluators) == 0 {
		passed := runResult.Status == runtime.StatusCompleted
		score := 0.0
		if passed {
			score = 1
		}
		evaluators = append(evaluators, evaluate.EvaluatorResult{
			Name:   "run_status",
			Type:   "rules",
			Passed: passed,
			Score:  score,
			Results: []evaluate.RuleResult{{
				Rule:    "runCompleted",
				Passed:  passed,
				Message: "default evolve evaluation when no task or manifest evaluator is configured",
			}},
		})
	}
	result := combineEvaluation(runResult.RunID, evaluators)
	data, _ := json.MarshalIndent(result, "", "  ")
	if err := store.WriteEvaluation(runResult.RunID, data); err != nil {
		return evaluate.Result{}, err
	}
	if meta, err := store.ReadMetadata(runResult.RunID); err == nil {
		meta.Evaluation = runs.EvaluationFile
		_ = store.WriteMetadata(runResult.RunID, meta)
	}
	return result, nil
}

func expectationEvaluator(expected any, final string) (evaluate.EvaluatorResult, bool) {
	if expected == nil {
		return evaluate.EvaluatorResult{}, false
	}
	values, ok := expected.(map[string]any)
	if !ok {
		return evaluate.EvaluatorResult{}, false
	}
	var results []evaluate.RuleResult
	for _, text := range stringList(values, "mustInclude", "must_include") {
		passed := strings.Contains(final, text)
		results = append(results, evaluate.RuleResult{Rule: "mustInclude", Passed: passed, Message: text})
	}
	for _, text := range stringList(values, "mustNotInclude", "must_not_include") {
		passed := !strings.Contains(final, text)
		results = append(results, evaluate.RuleResult{Rule: "mustNotInclude", Passed: passed, Message: text})
	}
	if len(results) == 0 {
		return evaluate.EvaluatorResult{}, false
	}
	passed := true
	score := 0.0
	for _, result := range results {
		if result.Passed {
			score++
		} else {
			passed = false
		}
	}
	score = score / float64(len(results))
	return evaluate.EvaluatorResult{Name: "task_expected", Type: "rules", Passed: passed, Score: score, Results: results}, true
}

func combineEvaluation(runID string, items []evaluate.EvaluatorResult) evaluate.Result {
	result := evaluate.Result{RunID: runID, Passed: true, Evaluators: items}
	total := 0.0
	for _, item := range items {
		total += item.Score
		if !item.Passed {
			result.Passed = false
		}
	}
	if len(items) > 0 {
		result.Score = total / float64(len(items))
	}
	return result
}

func (c *controller) renderInput(task TaskCase) (string, error) {
	if c.renderer != nil {
		var b strings.Builder
		if err := c.renderer.Execute(&b, task); err != nil {
			return "", err
		}
		return b.String(), nil
	}
	if text, ok := task.Input.(string); ok {
		return text, nil
	}
	data, err := json.MarshalIndent(task.Input, "", "  ")
	return string(data), err
}

func (c *controller) extractMetrics(trials []TrialResult) (map[string]float64, error) {
	sources := map[string]bool{c.exp.Objective.Metric: true}
	for _, guard := range c.exp.Objective.Guards {
		for _, source := range extractMetricSources(guard) {
			sources[source] = true
		}
	}
	metrics := map[string]float64{}
	for source := range sources {
		if strings.HasPrefix(source, "baseline.") {
			continue
		}
		value, err := metricValue(source, trials)
		if err != nil {
			return nil, err
		}
		metrics[source] = value
	}
	return metrics, nil
}

func (c *controller) evaluateGuards(metrics, baseline map[string]float64) (bool, []string, error) {
	if len(c.exp.Objective.Guards) == 0 {
		return true, nil, nil
	}
	vars := map[string]float64{}
	for k, v := range metrics {
		vars[k] = v
	}
	for k, v := range baseline {
		vars["baseline."+k] = v
	}
	var reasons []string
	for _, guard := range c.exp.Objective.Guards {
		pass, err := evalGuard(guard, vars)
		if err != nil {
			return false, nil, err
		}
		if !pass {
			reasons = append(reasons, guard)
		}
	}
	return len(reasons) == 0, reasons, nil
}

func (c *controller) propose(ctx context.Context, iteration int, best *candidate) ([]Proposal, error) {
	iterDir := filepath.Join(c.outDir, "iterations", fmt.Sprintf("%03d", iteration))
	if err := os.MkdirAll(iterDir, 0o755); err != nil {
		return nil, err
	}
	digest := c.buildDigest(iteration, best)
	digestData, _ := json.MarshalIndent(digest, "", "  ")
	if err := os.WriteFile(filepath.Join(iterDir, "feedback_digest.json"), digestData, 0o644); err != nil {
		return nil, err
	}
	evolverPath := c.resolvePath(c.exp.Evolver.Agent)
	agent, err := compiler.CompileWithOptions(evolverPath, compiler.Options{
		RunStore:          runs.NewStore(filepath.Join(iterDir, "evolver", "runs")),
		WorkspaceOverride: filepath.Join(iterDir, "evolver", "workspace"),
	})
	if err != nil {
		return nil, err
	}
	auto := ""
	result, err := runtime.NewWithOptions(runtime.Options{AutoApprove: true, AutoUserInput: &auto}).Run(ctx, agent, string(digestData))
	if err != nil {
		return nil, err
	}
	if runDir, err := agent.RunStore.LoadRun(result.RunID); err == nil {
		if events, err := trajectory.ReadFile(filepath.Join(runDir.Path, runs.TrajectoryFile)); err == nil {
			c.recordUsage(statsFromEvents(events))
		}
	}
	proposals, err := parseProposals(result.Final)
	if err != nil {
		return nil, err
	}
	if len(proposals) > c.exp.Evolver.Proposals {
		proposals = proposals[:c.exp.Evolver.Proposals]
	}
	for i := range proposals {
		if proposals[i].ID == "" {
			proposals[i].ID = fmt.Sprintf("proposal-%03d-%02d", iteration, i+1)
		}
	}
	if err := writeJSON(filepath.Join(iterDir, "proposals.json"), proposals); err != nil {
		return nil, err
	}
	c.events.Write("proposals.generated", map[string]any{"iteration": iteration, "count": len(proposals), "evolver_run_id": result.RunID})
	return proposals, nil
}

func (c *controller) buildDigest(iteration int, best *candidate) map[string]any {
	return map[string]any{
		"iteration": iteration,
		"objective": c.exp.Objective,
		"target": map[string]any{
			"editable":  c.exp.Target.Editable,
			"forbidden": c.exp.Target.Forbidden,
		},
		"best_candidate":   best.ID,
		"train_results":    best.Results["train"],
		"history":          c.history,
		"editable_content": c.editableContent(best.ManifestPath),
		"guidance":         c.exp.Objective.Guidance,
		"instructions":     "Return strict JSON with a proposals array or a single proposal. Each change must use target/find/replace and find must be copied exactly from editable_content. Selection task details are intentionally withheld; use train feedback to propose general improvements.",
	}
}

func (c *controller) editableContent(manifestPath string) map[string]string {
	content := map[string]string{}
	manifestData, err := os.ReadFile(manifestPath)
	if err == nil {
		content["manifest"] = string(manifestData)
	}
	manifest, _, err := config.LoadFile(manifestPath)
	if err == nil && manifest.Instructions.System != "" {
		if data, err := os.ReadFile(manifest.Instructions.System); err == nil {
			content["instructions.system"] = string(data)
		}
	}
	return content
}

func parseProposals(text string) ([]Proposal, error) {
	clean := strings.TrimSpace(stripMarkdownFence(text))
	var wrapper struct {
		Proposals []Proposal `json:"proposals"`
	}
	if err := json.Unmarshal([]byte(clean), &wrapper); err == nil && len(wrapper.Proposals) > 0 {
		return validateParsedProposals(wrapper.Proposals)
	}
	var proposal Proposal
	if err := json.Unmarshal([]byte(clean), &proposal); err == nil && len(proposal.Changes) > 0 {
		return validateParsedProposals([]Proposal{proposal})
	}
	var proposals []Proposal
	if err := json.Unmarshal([]byte(clean), &proposals); err == nil && len(proposals) > 0 {
		return validateParsedProposals(proposals)
	}
	start := strings.Index(clean, "{")
	end := strings.LastIndex(clean, "}")
	if start >= 0 && end > start {
		sub := clean[start : end+1]
		if sub != clean {
			return parseProposals(sub)
		}
	}
	return nil, fmt.Errorf("evolver output did not contain valid proposal JSON")
}

func validateParsedProposals(proposals []Proposal) ([]Proposal, error) {
	for i, proposal := range proposals {
		if len(proposal.Changes) == 0 {
			return nil, fmt.Errorf("proposal %d has no changes", i+1)
		}
		for j, change := range proposal.Changes {
			if change.Target == "" || change.Find == "" {
				return nil, fmt.Errorf("proposal %d change %d patch target and find are required", i+1, j+1)
			}
		}
	}
	return proposals, nil
}

func stripMarkdownFence(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	lines := strings.Split(text, "\n")
	if len(lines) >= 3 {
		return strings.Join(lines[1:len(lines)-1], "\n")
	}
	return text
}

func (c *controller) writeReport(best *candidate, candidates []*candidate) (string, error) {
	path := filepath.Join(c.outDir, "report.md")
	var b strings.Builder
	fmt.Fprintf(&b, "# Evolution Report: %s\n\n", c.exp.Metadata.Name)
	fmt.Fprintf(&b, "- experiment: `%s`\n- best: `%s`\n- objective: `%s` `%s`\n\n", c.id, best.ID, c.exp.Objective.Direction, c.exp.Objective.Metric)
	fmt.Fprintf(&b, "## Best Metrics\n\n")
	if best.Results != nil {
		for _, split := range []string{"train", "selection"} {
			if result := best.Results[split]; result != nil {
				fmt.Fprintf(&b, "### %s\n\n", split)
				writeMetricTable(&b, result.Metrics)
			}
		}
	}
	fmt.Fprintf(&b, "## Candidates\n\n")
	fmt.Fprintf(&b, "| candidate | parent | hypothesis | rejected | reason | %s |\n", c.exp.Objective.Metric)
	fmt.Fprintf(&b, "| --- | --- | --- | --- | --- | --- |\n")
	for _, cand := range candidates {
		metric := ""
		if cand.Results != nil && cand.Results["selection"] != nil {
			metric = fmt.Sprintf("%.4f", cand.Results["selection"].Metrics[c.exp.Objective.Metric])
		}
		hypothesis := ""
		if cand.Proposal != nil {
			hypothesis = cand.Proposal.Hypothesis
		}
		fmt.Fprintf(&b, "| `%s` | `%s` | %s | %v | %s | %s |\n", cand.ID, cand.Parent, escapeTable(hypothesis), cand.Rejected, escapeTable(cand.RejectReason), metric)
	}
	fmt.Fprintf(&b, "\n## Notes\n\n")
	if c.exp.Data.Test != "" {
		fmt.Fprintf(&b, "- `data.test` is configured but not executed in P0-P4; it is reserved for P5 final evaluation.\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func writeMetricTable(b *strings.Builder, metrics map[string]float64) {
	fmt.Fprintf(b, "| metric | value |\n| --- | --- |\n")
	keys := make([]string, 0, len(metrics))
	for key := range metrics {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(b, "| `%s` | %.4f |\n", key, metrics[key])
	}
	fmt.Fprintln(b)
}

func metricValue(source string, trials []TrialResult) (float64, error) {
	if len(trials) == 0 {
		return 0, fmt.Errorf("no trials for metric %s", source)
	}
	switch source {
	case "evaluation.score":
		return avgEval(trials, func(t TrialResult) float64 { return t.Evaluation.Score }), nil
	case "evaluation.passed_rate":
		return avgEval(trials, func(t TrialResult) float64 {
			if t.Evaluation.Passed {
				return 1
			}
			return 0
		}), nil
	case "run.steps":
		return avgStats(trials, func(s RunStats) float64 { return float64(s.Steps) }), nil
	case "run.toolCalls":
		return avgStats(trials, func(s RunStats) float64 { return float64(s.ToolCalls) }), nil
	case "run.modelCalls":
		return avgStats(trials, func(s RunStats) float64 { return float64(s.ModelCalls) }), nil
	case "run.modelErrors":
		return avgStats(trials, func(s RunStats) float64 { return float64(s.ModelErrors) }), nil
	case "run.toolErrors":
		return avgStats(trials, func(s RunStats) float64 { return float64(s.ToolErrors) }), nil
	case "run.permissionDenied":
		return avgStats(trials, func(s RunStats) float64 { return float64(s.PermissionDenied) }), nil
	case "run.tokens", "run.totalTokens":
		return avgStats(trials, func(s RunStats) float64 { return float64(s.TotalTokens) }), nil
	case "run.promptTokens":
		return avgStats(trials, func(s RunStats) float64 { return float64(s.PromptTokens) }), nil
	case "run.completionTokens":
		return avgStats(trials, func(s RunStats) float64 { return float64(s.CompletionTokens) }), nil
	case "run.durationSec":
		return avgStats(trials, func(s RunStats) float64 { return s.DurationSec }), nil
	}
	if match := evaluatorScoreRe.FindStringSubmatch(source); match != nil {
		return avgEvaluator(trials, match[1], "", false)
	}
	if match := evaluatorPassedRe.FindStringSubmatch(source); match != nil {
		return avgEvaluator(trials, match[1], "", true)
	}
	if match := evaluatorRulePassedRe.FindStringSubmatch(source); match != nil {
		return avgEvaluator(trials, match[1], match[2], true)
	}
	return 0, fmt.Errorf("unsupported metric source %q", source)
}

var (
	evaluatorScoreRe      = regexp.MustCompile(`^evaluation\.evaluators\["([^"]+)"\]\.score$`)
	evaluatorPassedRe     = regexp.MustCompile(`^evaluation\.evaluators\["([^"]+)"\]\.passed$`)
	evaluatorRulePassedRe = regexp.MustCompile(`^evaluation\.evaluators\["([^"]+)"\]\.rules\["([^"]+)"\]\.passed$`)
	metricSourceRe        = regexp.MustCompile(`(?:baseline\.)?(?:evaluation\.[A-Za-z0-9_\.\[\]"]+|run\.[A-Za-z0-9_]+)`)
)

func avgEval(trials []TrialResult, fn func(TrialResult) float64) float64 {
	total := 0.0
	weightTotal := 0.0
	for _, trial := range trials {
		weight := trialWeight(trial)
		total += fn(trial) * weight
		weightTotal += weight
	}
	return total / weightTotal
}

func avgStats(trials []TrialResult, fn func(RunStats) float64) float64 {
	total := 0.0
	weightTotal := 0.0
	for _, trial := range trials {
		weight := trialWeight(trial)
		total += fn(trial.Stats) * weight
		weightTotal += weight
	}
	return total / weightTotal
}

func avgEvaluator(trials []TrialResult, name, rule string, passed bool) (float64, error) {
	total := 0.0
	weightTotal := 0.0
	for _, trial := range trials {
		weight := trialWeight(trial)
		for _, ev := range trial.Evaluation.Evaluators {
			if ev.Name != name {
				continue
			}
			if rule == "" {
				if passed {
					if ev.Passed {
						total += weight
					}
				} else {
					total += ev.Score * weight
				}
				weightTotal += weight
				continue
			}
			for _, rr := range ev.Results {
				if rr.Rule == rule {
					if rr.Passed {
						total += weight
					}
					weightTotal += weight
				}
			}
		}
	}
	if weightTotal == 0 {
		return 0, fmt.Errorf("metric evaluator %q rule %q not found", name, rule)
	}
	return total / weightTotal, nil
}

func trialWeight(trial TrialResult) float64 {
	if trial.Weight > 0 {
		return trial.Weight
	}
	return 1
}

func extractMetricSources(expr string) []string {
	raw := metricSourceRe.FindAllString(expr, -1)
	var out []string
	seen := map[string]bool{}
	for _, item := range raw {
		item = strings.TrimPrefix(item, "baseline.")
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

func statsFromEvents(events []trajectory.Event) RunStats {
	var stats RunStats
	for _, event := range events {
		if event.Step > stats.Steps {
			stats.Steps = event.Step
		}
		switch event.Type {
		case trajectory.EventModelCompleted:
			stats.ModelCalls++
			stats.PromptTokens += intPayload(event.Payload, "tokens_in")
			stats.CompletionTokens += intPayload(event.Payload, "tokens_out")
			stats.TotalTokens += intPayload(event.Payload, "tokens_total")
		case trajectory.EventModelFailed:
			stats.ModelErrors++
		case trajectory.EventToolCompleted:
			stats.ToolCalls++
		case trajectory.EventToolFailed:
			stats.ToolErrors++
		case trajectory.EventPermissionDenied:
			stats.PermissionDenied++
		}
	}
	return stats
}

func durationSec(meta runs.Metadata) float64 {
	if meta.EndedAt == nil {
		return 0
	}
	return meta.EndedAt.Sub(meta.StartedAt).Seconds()
}

func intPayload(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		i, _ := value.Int64()
		return int(i)
	default:
		return 0
	}
}

func evalGuard(expr string, vars map[string]float64) (bool, error) {
	for _, op := range []string{">=", "<=", "==", "!=", ">", "<"} {
		if idx := strings.Index(expr, op); idx >= 0 {
			left, err := evalArithmetic(strings.TrimSpace(expr[:idx]), vars)
			if err != nil {
				return false, err
			}
			right, err := evalArithmetic(strings.TrimSpace(expr[idx+len(op):]), vars)
			if err != nil {
				return false, err
			}
			switch op {
			case ">=":
				return left >= right, nil
			case "<=":
				return left <= right, nil
			case ">":
				return left > right, nil
			case "<":
				return left < right, nil
			case "==":
				return left == right, nil
			case "!=":
				return left != right, nil
			}
		}
	}
	value, err := evalArithmetic(expr, vars)
	return value != 0, err
}

func evalArithmetic(expr string, vars map[string]float64) (float64, error) {
	parser := &exprParser{tokens: tokenizeExpr(expr), vars: vars}
	value, err := parser.parseExpr()
	if err != nil {
		return 0, err
	}
	if parser.pos != len(parser.tokens) {
		return 0, fmt.Errorf("unexpected token %q in %q", parser.tokens[parser.pos], expr)
	}
	return value, nil
}

type exprParser struct {
	tokens []string
	pos    int
	vars   map[string]float64
}

func (p *exprParser) parseExpr() (float64, error) {
	value, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for p.match("+") || p.match("-") {
		op := p.tokens[p.pos-1]
		right, err := p.parseTerm()
		if err != nil {
			return 0, err
		}
		if op == "+" {
			value += right
		} else {
			value -= right
		}
	}
	return value, nil
}

func (p *exprParser) parseTerm() (float64, error) {
	value, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for p.match("*") || p.match("/") {
		op := p.tokens[p.pos-1]
		right, err := p.parseFactor()
		if err != nil {
			return 0, err
		}
		if op == "*" {
			value *= right
		} else {
			value /= right
		}
	}
	return value, nil
}

func (p *exprParser) parseFactor() (float64, error) {
	if p.match("(") {
		value, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		if !p.match(")") {
			return 0, errors.New("missing closing parenthesis")
		}
		return value, nil
	}
	if p.match("-") {
		value, err := p.parseFactor()
		return -value, err
	}
	if p.pos >= len(p.tokens) {
		return 0, errors.New("unexpected end of expression")
	}
	token := p.tokens[p.pos]
	p.pos++
	if value, err := strconv.ParseFloat(token, 64); err == nil {
		return value, nil
	}
	value, ok := p.vars[token]
	if !ok {
		return 0, fmt.Errorf("unknown metric %q", token)
	}
	return value, nil
}

func (p *exprParser) match(token string) bool {
	if p.pos < len(p.tokens) && p.tokens[p.pos] == token {
		p.pos++
		return true
	}
	return false
}

func tokenizeExpr(expr string) []string {
	var tokens []string
	for i := 0; i < len(expr); {
		ch := expr[i]
		if ch == ' ' || ch == '\t' || ch == '\n' {
			i++
			continue
		}
		if strings.ContainsRune("()+-*/", rune(ch)) {
			tokens = append(tokens, string(ch))
			i++
			continue
		}
		start := i
		for i < len(expr) && !strings.ContainsRune(" \t\n()+-*/", rune(expr[i])) {
			i++
		}
		tokens = append(tokens, expr[start:i])
	}
	return tokens
}

func isImproved(candidate, best float64, direction string, minDelta float64) bool {
	if direction == "minimize" {
		return candidate <= best-minDelta
	}
	return candidate >= best+minDelta
}

func (c *controller) resolvePath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Clean(filepath.Join(c.exp.baseDir, path))
}

func (c *controller) bundleRootFromManifest(manifestPath string) string {
	relAgent := relPath(c.bundleBase(), c.resolvePath(c.exp.Target.Agent))
	root := strings.TrimSuffix(manifestPath, relAgent)
	return strings.TrimSuffix(root, string(filepath.Separator))
}

func (c *controller) bundleBase() string {
	if c.bundleBaseDir != "" {
		return c.bundleBaseDir
	}
	return c.exp.baseDir
}

func relPath(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return filepath.Base(path)
	}
	return rel
}

func relUnder(base, path string) (string, bool) {
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

func isUnder(base, path string) bool {
	rel, err := filepath.Rel(base, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func copyBundle(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		name := d.Name()
		if name == "tasks" || name == "compile-runs" || name == "compile-workspace" || name == "results.json" || name == "patch.json" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyPath(src, dst string) error {
	info, err := os.Stat(src)
	if os.IsNotExist(err) {
		return os.MkdirAll(dst, 0o755)
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst, info.Mode())
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

type eventWriter struct {
	mu   sync.Mutex
	file *os.File
}

func newEventWriter(path string) (*eventWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &eventWriter{file: file}, nil
}

func (w *eventWriter) Write(typ string, payload map[string]any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	data, _ := json.Marshal(map[string]any{
		"ts":      time.Now(),
		"type":    typ,
		"payload": payload,
	})
	_, _ = w.file.Write(append(data, '\n'))
}

func (w *eventWriter) Close() error {
	return w.file.Close()
}

func uniqueStrings(items []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func commonAncestor(paths []string) string {
	var common string
	for _, path := range paths {
		if path == "" {
			continue
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		info, err := os.Stat(abs)
		if err == nil && !info.IsDir() {
			abs = filepath.Dir(abs)
		}
		if err != nil && filepath.Ext(abs) != "" {
			abs = filepath.Dir(abs)
		}
		if common == "" {
			common = abs
			continue
		}
		for !isUnder(common, abs) {
			parent := filepath.Dir(common)
			if parent == common {
				return common
			}
			common = parent
		}
	}
	if common == "" {
		return "."
	}
	return common
}

func matchesAny(target string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchPattern(target, pattern) {
			return true
		}
	}
	return false
}

func matchesPathOrDescendant(target string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchPattern(target, pattern) || matchPatternPrefix(target, pattern) {
			return true
		}
	}
	return false
}

func matchPattern(target, pattern string) bool {
	ok, _ := regexp.MatchString("^"+patternRegexp(pattern)+"$", target)
	return ok
}

func matchPatternPrefix(target, pattern string) bool {
	ok, _ := regexp.MatchString("^"+patternRegexp(pattern)+`(?:\.|\[).+`, target)
	return ok
}

func patternRegexp(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); {
		if strings.HasPrefix(pattern[i:], "[]") {
			b.WriteString(`(?:\[\d+\]|\[\])`)
			i += 2
			continue
		}
		if pattern[i] == '*' {
			b.WriteString(`[^.]+`)
			i++
			continue
		}
		b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		i++
	}
	return b.String()
}

func stringList(values map[string]any, keys ...string) []string {
	for _, key := range keys {
		value, ok := values[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case []any:
			var out []string
			for _, item := range typed {
				if text, ok := item.(string); ok {
					out = append(out, text)
				}
			}
			return out
		case []string:
			return typed
		case string:
			return []string{typed}
		}
	}
	return nil
}

func sanitizeName(name string) string {
	name = strings.ToLower(name)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "item"
	}
	return out
}

func escapeTable(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "|", "\\|")
	return text
}
