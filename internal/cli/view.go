package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"gopkg.in/yaml.v3"

	"github.com/cosmtrek/jeju/internal/evaluate"
	"github.com/cosmtrek/jeju/internal/runs"
	"github.com/cosmtrek/jeju/internal/trajectory"
)

var markdownRenderer = goldmark.New(goldmark.WithExtensions(extension.GFM))

func renderMarkdown(src string) template.HTML {
	if src == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(src), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(src))
	}
	return template.HTML(buf.String())
}

func runView(runID, out string) error {
	store := runs.NewStore(filepath.Clean("./runs"))
	runDir, err := store.LoadRun(runID)
	if err != nil {
		return err
	}

	if out == "" {
		out = defaultRunReportPath(runDir)
	}
	if err := writeRunReport(store, runDir, out); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", out)
	return nil
}

func writeDefaultRunReport(store *runs.Store, runID string) (string, error) {
	runDir, err := store.LoadRun(runID)
	if err != nil {
		return "", err
	}
	out := defaultRunReportPath(runDir)
	if err := writeRunReport(store, runDir, out); err != nil {
		return "", err
	}
	abs, err := filepath.Abs(out)
	if err != nil {
		return out, nil
	}
	return abs, nil
}

func defaultRunReportPath(runDir *runs.RunDir) string {
	return filepath.Join(runDir.Path, runs.ReportFile)
}

func writeRunReport(store *runs.Store, runDir *runs.RunDir, out string) error {
	report, err := buildRunReport(store, runDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return writeRunReportHTML(out, report)
}

type runReport struct {
	GeneratedAt      time.Time
	RunDir           string
	Metadata         runs.Metadata
	Duration         string
	Summary          inspectSummary
	Final            string
	FinalHTML        template.HTML
	ConfigSnapshot   string
	Evaluation       *evaluate.Result
	EvaluationExists bool
	EvalScoreLabel   string
	EvalScorePercent string
	Artifacts        []artifactView
	Steps            []stepView
	Blocks           []stepBlock
	Events           []eventView
	MetadataJSON     string
}

type artifactView struct {
	Path        string
	Size        int64
	Content     string
	ContentType string
	ContentNote string
}

type stepView struct {
	Number    int
	Kind      string // tool | parse_failed | final | other
	Title     string
	Status    string // completed | failed | running
	TypeLabel string // "TOOL" | "MODEL"
	TypeClass string // "tool" | "model"
	Thought   string
	Error     string // parse_failed error

	// Tool calls executed within this step. A single model turn may emit more
	// than one tool call, so each is tracked and rendered independently.
	ToolCalls []toolCallView

	// model thinking
	ReasoningRef         string
	ReasoningPreview     string
	ReasoningContent     string
	ReasoningContentType string
	ReasoningContentNote string

	// context compression applied before this step's model request
	Compression *compressionView

	// debug-only
	InputRef       string
	OutputRef      string
	DebugArtifacts []artifactRefView
	Events         []eventView
}

// compressionView summarizes the context-management activity recorded for a step
// (token estimate, threshold, and any truncation/summary/degradation applied).
type compressionView struct {
	Triggered            bool
	EstimatedTokens      int
	ThresholdTokens      int
	ContextWindow        int
	EffectiveInputLimit  int
	BeforeTokens         int
	AfterTokens          int
	Strategies           []string
	StrategiesLabel      string
	PreservedBlocks      int
	TruncatedToolResults int
	Summarized           bool
	SummaryFailed        bool
	SummaryTokensIn      int
	SummaryTokensOut     int
	SummaryRef           string
	ReportRef            string
	Failed               bool
	Error                string
}

type toolCallView struct {
	Kind   string // write | shell_ok | shell_failed | other
	Title  string
	Status string // completed | failed | running
	Tool   string
	Error  string

	// shell tool
	Command         string
	ShellExitCode   string
	ShellDurationMS string
	ShellStdout     string
	ShellStderr     string

	// write tool
	FilePath        string
	FileBytes       int64
	FileBytesLabel  string
	FileLineCount   int
	FileContent     string
	FileContentType string
	FileContentNote string

	// generic tool fallback
	Input      string
	ToolOutput string
	OutputRef  string
}

type artifactRefView struct {
	Path        string
	Type        string
	Content     string
	ContentType string
	ContentNote string
}

type stepBlock struct {
	IsGroup    bool
	Step       stepView
	Steps      []stepView
	GroupTitle string
	GroupError string
	GroupCount int
}

type eventView struct {
	ID          string
	Type        string
	Actor       string
	Step        int
	Timestamp   string
	PayloadJSON string
}

func buildRunReport(store *runs.Store, runDir *runs.RunDir) (runReport, error) {
	meta, err := store.ReadMetadata(runDir.RunID)
	if err != nil {
		return runReport{}, err
	}
	events, err := trajectory.ReadFile(filepath.Join(runDir.Path, meta.Trajectory))
	if err != nil {
		return runReport{}, err
	}
	final, err := readOptionalText(filepath.Join(runDir.Path, meta.Final))
	if err != nil {
		return runReport{}, err
	}
	configSnapshot, err := readOptionalText(filepath.Join(runDir.Path, meta.ConfigSnapshot))
	if err != nil {
		return runReport{}, err
	}
	evaluationPath := ""
	if meta.Evaluation != "" {
		evaluationPath = filepath.Join(runDir.Path, meta.Evaluation)
	}
	evaluation, evaluationExists, err := readEvaluation(evaluationPath)
	if err != nil {
		return runReport{}, err
	}
	artifacts, err := listArtifacts(filepath.Join(runDir.Path, runs.ArtifactsDir))
	if err != nil {
		return runReport{}, err
	}
	artifactByPath := mapArtifacts(artifacts)
	workspaceRoot := findWorkspaceRoot(runDir.Path, meta.Agent, workspacePathFromConfig(configSnapshot))
	steps := buildStepViews(events, artifactByPath, workspaceRoot)
	blocks := groupSteps(steps)
	metadataJSON, err := marshalIndented(meta)
	if err != nil {
		return runReport{}, err
	}

	duration := ""
	if meta.EndedAt != nil {
		duration = meta.EndedAt.Sub(meta.StartedAt).Round(time.Millisecond).String()
	}

	evalScoreLabel := ""
	evalScorePercent := ""
	if evaluationExists && evaluation != nil {
		if evaluation.Passed {
			evalScoreLabel = "eval passed"
		} else {
			evalScoreLabel = fmt.Sprintf("eval %.0f%%", evaluation.Score*100)
		}
		evalScorePercent = fmt.Sprintf("%.0f%%", evaluation.Score*100)
	}

	return runReport{
		GeneratedAt:      time.Now(),
		RunDir:           runDir.Path,
		Metadata:         meta,
		Duration:         duration,
		Summary:          summarizeInspect(events),
		Final:            final,
		FinalHTML:        renderMarkdown(final),
		ConfigSnapshot:   configSnapshot,
		Evaluation:       evaluation,
		EvaluationExists: evaluationExists,
		EvalScoreLabel:   evalScoreLabel,
		EvalScorePercent: evalScorePercent,
		Artifacts:        artifacts,
		Steps:            steps,
		Blocks:           blocks,
		Events:           mapEventViews(events),
		MetadataJSON:     metadataJSON,
	}, nil
}

func readOptionalText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func readEvaluation(path string) (*evaluate.Result, bool, error) {
	if path == "" {
		return nil, false, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var result evaluate.Result
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, false, err
	}
	return &result, true, nil
}

func listArtifacts(root string) ([]artifactView, error) {
	var artifacts []artifactView
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(root), path)
		if err != nil {
			return err
		}
		artifact := artifactView{
			Path: filepath.ToSlash(rel),
			Size: info.Size(),
		}
		artifact.Content, artifact.ContentType, artifact.ContentNote = readArtifactPreview(path, info.Size())
		artifacts = append(artifacts, artifact)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].Path < artifacts[j].Path
	})
	return artifacts, nil
}

func readArtifactPreview(path string, size int64) (string, string, string) {
	const maxEmbed = 200 * 1024
	if size > maxEmbed {
		return "", "", "Too large to embed in report."
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err.Error()
	}
	if !isLikelyText(data) {
		return "", "", "Binary content is not embedded."
	}
	content := string(data)
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext == "json" {
		var raw any
		if err := json.Unmarshal(data, &raw); err == nil {
			if pretty, err := marshalIndented(raw); err == nil {
				content = pretty
			}
		}
	}
	return content, ext, ""
}

// workspacePathFromConfig extracts workspace.path from a config snapshot. The
// directory name often differs from the agent name (e.g. agent "deep-research"
// using workspace "research"), so the snapshot is the authoritative source.
func workspacePathFromConfig(snapshot string) string {
	if snapshot == "" {
		return ""
	}
	var cfg struct {
		Workspace struct {
			Path string `yaml:"path"`
		} `yaml:"workspace"`
	}
	if err := yaml.Unmarshal([]byte(snapshot), &cfg); err != nil {
		return ""
	}
	return cfg.Workspace.Path
}

// findWorkspaceRoot returns the absolute path to the workspace directory used by
// the run, or "" if it cannot be located. It prefers the path recorded in the
// config snapshot, then falls back to a sibling of the runs directory keyed by
// the workspace directory name (so relocated runs still resolve) or the agent.
func findWorkspaceRoot(runDirPath, agent, configWorkspacePath string) string {
	if configWorkspacePath != "" {
		if info, err := os.Stat(configWorkspacePath); err == nil && info.IsDir() {
			return configWorkspacePath
		}
	}
	runsDir := filepath.Dir(runDirPath)
	root := filepath.Dir(runsDir)
	var names []string
	if configWorkspacePath != "" {
		names = append(names, filepath.Base(configWorkspacePath))
	}
	if agent != "" {
		names = append(names, agent)
	}
	for _, name := range names {
		ws := filepath.Join(root, "workspace", name)
		if info, err := os.Stat(ws); err == nil && info.IsDir() {
			return ws
		}
	}
	return ""
}

func readWorkspaceFile(workspaceRoot, relPath string) (content, contentType, note string, size int64) {
	if workspaceRoot == "" || relPath == "" {
		return "", "", "", 0
	}
	path := filepath.Join(workspaceRoot, relPath)
	info, err := os.Stat(path)
	if err != nil {
		return "", "", "", 0
	}
	if info.IsDir() {
		return "", "", "", 0
	}
	size = info.Size()
	const maxEmbed = 200 * 1024
	if size > maxEmbed {
		return "", "", "Too large to embed in report.", size
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err.Error(), size
	}
	if !isLikelyText(data) {
		return "", "", "Binary content is not embedded.", size
	}
	return string(data), strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."), "", size
}

func isLikelyText(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return false
		}
	}
	return true
}

func mapArtifacts(artifacts []artifactView) map[string]artifactView {
	out := make(map[string]artifactView, len(artifacts))
	for _, artifact := range artifacts {
		out[artifact.Path] = artifact
	}
	return out
}

func mapEventViews(events []trajectory.Event) []eventView {
	out := make([]eventView, 0, len(events))
	for _, event := range events {
		payload := ""
		if len(event.Payload) > 0 {
			payload, _ = marshalIndented(event.Payload)
		}
		out = append(out, eventView{
			ID:          event.ID,
			Type:        string(event.Type),
			Actor:       event.Actor,
			Step:        event.Step,
			Timestamp:   event.TS.Format("2006-01-02 15:04:05.000"),
			PayloadJSON: payload,
		})
	}
	return out
}

func buildStepViews(events []trajectory.Event, artifacts map[string]artifactView, workspaceRoot string) []stepView {
	stepsByNumber := map[int]*stepView{}
	order := []int{}
	for _, event := range events {
		if event.Step <= 0 {
			continue
		}
		step, ok := stepsByNumber[event.Step]
		if !ok {
			step = &stepView{Number: event.Step, Status: "running"}
			stepsByNumber[event.Step] = step
			order = append(order, event.Step)
		}
		step.Events = append(step.Events, mapEventViews([]trajectory.Event{event})...)
		applyEventToStep(step, event, artifacts, workspaceRoot)
	}
	sort.Ints(order)
	steps := make([]stepView, 0, len(order))
	for _, number := range order {
		step := stepsByNumber[number]
		finalizeStep(step)
		steps = append(steps, *step)
	}
	return steps
}

// currentToolCall returns the tool call most recently started within the step,
// allocating one if the trajectory emits a completion/failure without a prior
// request (defensive — the runtime always emits tool.requested first).
func currentToolCall(step *stepView) *toolCallView {
	if len(step.ToolCalls) == 0 {
		step.ToolCalls = append(step.ToolCalls, toolCallView{Status: "running"})
	}
	return &step.ToolCalls[len(step.ToolCalls)-1]
}

func applyEventToStep(step *stepView, event trajectory.Event, artifacts map[string]artifactView, workspaceRoot string) {
	switch event.Type {
	case trajectory.EventActionParsed:
		if thought := stringPayload(event.Payload, "thought"); thought != "" {
			step.Thought = thought
		}
		if stringPayload(event.Payload, "type") == "final" {
			step.Kind = "final"
			step.Status = "completed"
		}
	case trajectory.EventActionParseFailed:
		step.Kind = "parse_failed"
		step.Status = "failed"
		step.Error = stringPayload(event.Payload, "error")
	case trajectory.EventModelStarted:
		step.InputRef = stringPayload(event.Payload, "input_ref")
	case trajectory.EventModelCompleted:
		step.OutputRef = stringPayload(event.Payload, "output_ref")
		if ref := stringPayload(event.Payload, "reasoning_ref"); ref != "" {
			step.ReasoningRef = ref
			step.ReasoningPreview = stringPayload(event.Payload, "reasoning_preview")
			artifact := artifacts[ref]
			step.ReasoningContent = artifact.Content
			step.ReasoningContentType = artifact.ContentType
			step.ReasoningContentNote = artifact.ContentNote
		}
	case trajectory.EventToolRequested:
		tc := toolCallView{Status: "running"}
		if tool := stringPayload(event.Payload, "tool"); tool != "" {
			tc.Tool = tool
		}
		if input, ok := event.Payload["input"].(map[string]any); ok {
			if cmd := stringPayload(input, "command"); cmd != "" {
				tc.Command = cmd
			}
			if path := stringPayload(input, "path"); path != "" && tc.Tool == "write" {
				tc.FilePath = path
			}
			if len(input) > 0 {
				if pretty, err := marshalIndented(input); err == nil {
					tc.Input = pretty
				}
			}
		}
		step.ToolCalls = append(step.ToolCalls, tc)
	case trajectory.EventToolCompleted:
		tc := currentToolCall(step)
		tc.Status = "completed"
		if tool := stringPayload(event.Payload, "tool"); tool != "" {
			tc.Tool = tool
		}
		if ref := stringPayload(event.Payload, "output_ref"); ref != "" {
			tc.OutputRef = ref
			applyToolOutput(tc, artifacts[ref])
		}
		switch tc.Tool {
		case "shell":
			tc.Kind = "shell_ok"
		case "write":
			tc.Kind = "write"
			if tc.FilePath != "" {
				tc.FileContent, tc.FileContentType, tc.FileContentNote, _ = readWorkspaceFile(workspaceRoot, tc.FilePath)
			}
		}
	case trajectory.EventToolFailed:
		tc := currentToolCall(step)
		tc.Status = "failed"
		tc.Error = stringPayload(event.Payload, "error")
		if tool := stringPayload(event.Payload, "tool"); tool != "" {
			tc.Tool = tool
		}
		if tc.Tool == "shell" {
			tc.Kind = "shell_failed"
		} else {
			tc.Kind = "other"
		}
	case trajectory.EventStepCompleted:
		status := stringPayload(event.Payload, "status")
		if status != "" && step.Status != "failed" && step.Status != "completed" {
			step.Status = status
		}
	case trajectory.EventArtifactCreated:
		path := stringPayload(event.Payload, "path")
		if path == "" {
			return
		}
		artifact := artifacts[path]
		ref := artifactRefView{
			Path:        path,
			Type:        stringPayload(event.Payload, "type"),
			Content:     artifact.Content,
			ContentType: artifact.ContentType,
			ContentNote: artifact.ContentNote,
		}
		// All artifact references go into debug; the main view uses dedicated fields
		// (ShellStdout, FileContent, etc.) to surface the meaningful bits.
		step.DebugArtifacts = append(step.DebugArtifacts, ref)
	case trajectory.EventContextEstimated:
		c := ensureCompression(step)
		c.EstimatedTokens = intPayload(event.Payload, "estimated_tokens")
		c.ThresholdTokens = intPayload(event.Payload, "threshold_tokens")
		c.ContextWindow = intPayload(event.Payload, "context_window")
		c.EffectiveInputLimit = intPayload(event.Payload, "effective_input_limit")
		if boolPayload(event.Payload, "compression_required") {
			c.Triggered = true
		}
	case trajectory.EventContextCompressionStarted:
		c := ensureCompression(step)
		c.Triggered = true
		if c.BeforeTokens == 0 {
			c.BeforeTokens = intPayload(event.Payload, "before_tokens")
		}
		if c.ThresholdTokens == 0 {
			c.ThresholdTokens = intPayload(event.Payload, "threshold_tokens")
		}
	case trajectory.EventContextCompressionCompleted:
		c := ensureCompression(step)
		c.Triggered = true
		c.BeforeTokens = intPayload(event.Payload, "before_tokens")
		c.AfterTokens = intPayload(event.Payload, "after_tokens")
		c.PreservedBlocks = intPayload(event.Payload, "preserved_blocks")
		c.TruncatedToolResults = intPayload(event.Payload, "truncated_tool_results")
		c.Strategies = stringSlicePayload(event.Payload, "strategies")
		if ref := stringPayload(event.Payload, "summary_ref"); ref != "" {
			c.SummaryRef = ref
		}
		if ref := stringPayload(event.Payload, "report_ref"); ref != "" {
			c.ReportRef = ref
		}
	case trajectory.EventContextSummaryStarted:
		c := ensureCompression(step)
		c.Triggered = true
		c.Summarized = true
	case trajectory.EventContextSummaryCompleted:
		c := ensureCompression(step)
		c.Summarized = true
		c.SummaryTokensIn = intPayload(event.Payload, "tokens_in")
		c.SummaryTokensOut = intPayload(event.Payload, "tokens_out")
	case trajectory.EventContextSummaryFailed:
		c := ensureCompression(step)
		c.SummaryFailed = true
		if msg := stringPayload(event.Payload, "error"); msg != "" {
			c.Error = msg
		}
	case trajectory.EventContextCompressionFailed:
		c := ensureCompression(step)
		c.Triggered = true
		c.Failed = true
		c.Error = stringPayload(event.Payload, "error")
		if c.BeforeTokens == 0 {
			c.BeforeTokens = intPayload(event.Payload, "before_tokens")
		}
		if c.AfterTokens == 0 {
			c.AfterTokens = intPayload(event.Payload, "after_tokens")
		}
	}
}

func ensureCompression(step *stepView) *compressionView {
	if step.Compression == nil {
		step.Compression = &compressionView{}
	}
	return step.Compression
}

func applyToolOutput(tc *toolCallView, artifact artifactView) {
	if artifact.Content == "" {
		return
	}
	// Tool output JSON is shaped {"output":"<inner JSON>","artifacts":[...]} where
	// inner JSON is the tool's actual return payload. Unwrap and parse it.
	var wrapper struct {
		Output string `json:"output"`
	}
	inner := artifact.Content
	if err := json.Unmarshal([]byte(artifact.Content), &wrapper); err == nil && wrapper.Output != "" {
		inner = wrapper.Output
	}
	tc.ToolOutput = inner
	switch tc.Tool {
	case "shell":
		var shell struct {
			DurationMS int    `json:"duration_ms"`
			ExitCode   int    `json:"exit_code"`
			Stdout     string `json:"stdout"`
			Stderr     string `json:"stderr"`
		}
		if err := json.Unmarshal([]byte(inner), &shell); err == nil {
			tc.ShellExitCode = fmt.Sprintf("%d", shell.ExitCode)
			tc.ShellDurationMS = fmt.Sprintf("%d", shell.DurationMS)
			tc.ShellStdout = shell.Stdout
			tc.ShellStderr = shell.Stderr
		}
	case "write":
		var fw struct {
			Bytes int64  `json:"bytes"`
			Path  string `json:"path"`
		}
		if err := json.Unmarshal([]byte(inner), &fw); err == nil {
			tc.FileBytes = fw.Bytes
			tc.FileBytesLabel = formatBytes(fw.Bytes)
			if fw.Path != "" {
				tc.FilePath = fw.Path
			}
		}
	}
}

func formatBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	}
}

func finalizeStep(step *stepView) {
	for i := range step.ToolCalls {
		finalizeToolCall(&step.ToolCalls[i])
	}
	if step.Kind == "" {
		if len(step.ToolCalls) > 0 {
			step.Kind = "tool"
		} else {
			step.Kind = "other"
		}
	}
	if step.Kind == "tool" {
		step.Status = aggregateToolStatus(step.ToolCalls)
	}
	if step.Status == "" {
		step.Status = "running"
	}
	if step.Compression != nil {
		step.Compression.StrategiesLabel = strings.Join(step.Compression.Strategies, ", ")
	}
	step.Title = stepTitle(step)
	step.TypeLabel, step.TypeClass = stepType(step)
}

func finalizeToolCall(tc *toolCallView) {
	if tc.Kind == "" {
		tc.Kind = "other"
	}
	if tc.Status == "" {
		tc.Status = "running"
	}
	tc.Title = toolCallTitle(tc)
	if tc.FileContent != "" {
		tc.FileLineCount = strings.Count(tc.FileContent, "\n")
		if !strings.HasSuffix(tc.FileContent, "\n") {
			tc.FileLineCount++
		}
	}
}

func aggregateToolStatus(calls []toolCallView) string {
	status := "completed"
	for _, tc := range calls {
		if tc.Status == "failed" {
			return "failed"
		}
		if tc.Status != "completed" {
			status = "running"
		}
	}
	return status
}

func stepType(step *stepView) (label, class string) {
	switch step.Kind {
	case "tool", "other":
		return "TOOL", "tool"
	case "final", "parse_failed":
		return "MODEL", "model"
	default:
		return "", ""
	}
}

func stepTitle(step *stepView) string {
	switch step.Kind {
	case "parse_failed":
		return "model returned invalid JSON"
	case "final":
		return "Final answer"
	case "tool":
		if len(step.ToolCalls) == 1 {
			return step.ToolCalls[0].Title
		}
		return multiToolTitle(step.ToolCalls)
	default:
		return fmt.Sprintf("Step %d", step.Number)
	}
}

func multiToolTitle(calls []toolCallView) string {
	name := ""
	for _, tc := range calls {
		if tc.Tool == "" {
			name = ""
			break
		}
		if name == "" {
			name = tc.Tool
		} else if name != tc.Tool {
			name = ""
			break
		}
	}
	if name != "" {
		return fmt.Sprintf("%s × %d", name, len(calls))
	}
	return fmt.Sprintf("%d tool calls", len(calls))
}

func toolCallTitle(tc *toolCallView) string {
	switch tc.Kind {
	case "write":
		if tc.FilePath != "" && tc.FileBytesLabel != "" {
			return fmt.Sprintf("wrote %s · %s", tc.FilePath, tc.FileBytesLabel)
		}
		if tc.FilePath != "" {
			return fmt.Sprintf("wrote %s", tc.FilePath)
		}
		return "write"
	case "shell_ok", "shell_failed":
		if tc.Command != "" {
			return fmt.Sprintf("$ %s", truncate(tc.Command, 120))
		}
		return "shell"
	default:
		if tc.Tool != "" {
			return tc.Tool
		}
		return "tool"
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func groupSteps(steps []stepView) []stepBlock {
	blocks := make([]stepBlock, 0, len(steps))
	i := 0
	for i < len(steps) {
		if steps[i].Kind == "parse_failed" {
			j := i + 1
			for j < len(steps) && steps[j].Kind == "parse_failed" && steps[j].Error == steps[i].Error {
				j++
			}
			if j-i >= 2 {
				group := make([]stepView, j-i)
				copy(group, steps[i:j])
				blocks = append(blocks, stepBlock{
					IsGroup:    true,
					Steps:      group,
					GroupTitle: fmt.Sprintf("Steps %d–%d · model returned invalid JSON × %d", steps[i].Number, steps[j-1].Number, j-i),
					GroupError: steps[i].Error,
					GroupCount: j - i,
				})
				i = j
				continue
			}
		}
		blocks = append(blocks, stepBlock{Step: steps[i]})
		i++
	}
	return blocks
}

func stringPayload(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

// intPayload reads a numeric payload field. Numbers decoded from JSONL arrive as
// float64, so handle the common numeric kinds.
func intPayload(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case int64:
		return int(value)
	case json.Number:
		if n, err := value.Int64(); err == nil {
			return int(n)
		}
	}
	return 0
}

func boolPayload(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	value, _ := payload[key].(bool)
	return value
}

// stringSlicePayload reads a string-array payload field (e.g. compression strategies).
func stringSlicePayload(payload map[string]any, key string) []string {
	if payload == nil {
		return nil
	}
	raw, ok := payload[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func marshalIndented(value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func writeRunReportHTML(path string, report runReport) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return runReportTemplate.Execute(file, report)
}

var runReportTemplate = template.Must(template.New("run-report").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jeju Run {{.Metadata.RunID}}</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #ffffff;
      --ink: #172033;
      --muted: #6b7280;
      --faint: #9aa2af;
      --line: #e5e8ee;
      --soft: #f5f6f8;
      --ok: #15803d;
      --bad: #b42318;
      --warn: #a15c07;
      --accent: #2563eb;
      --code: #101828;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--ink);
      font: 14px/1.55 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }

    /* ---------- Shared layout wrap ---------- */
    .wrap {
      max-width: 1280px;
      margin: 0 auto;
      padding-left: 40px;
      padding-right: 40px;
    }

    /* ---------- Header ---------- */
    header {
      padding: 28px 0 28px;
      border-bottom: 1px solid var(--line);
    }
    .header-inner {
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 12px 32px;
      align-items: baseline;
    }
    .header-id {
      display: flex;
      gap: 14px;
      align-items: baseline;
      flex-wrap: wrap;
    }
    .header-id h1 {
      margin: 0;
      font-size: 19px;
      font-weight: 650;
      letter-spacing: -.01em;
    }
    .run-id { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
    .header-score-wrap {
      text-align: right;
      grid-row: 1 / span 2;
      grid-column: 2;
      align-self: center;
    }
    .header-score-label {
      font-size: 11px;
      color: var(--muted);
      letter-spacing: .04em;
      margin-bottom: 2px;
    }
    .header-score {
      font: 700 28px/1 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      letter-spacing: -.02em;
      font-variant-numeric: tabular-nums;
    }
    .header-score.ok { color: var(--ok); }
    .header-score.bad { color: var(--bad); }
    .header-meta {
      color: var(--muted);
      font-size: 12.5px;
      font-variant-numeric: tabular-nums;
      grid-column: 1;
    }
    .header-meta .sep { color: var(--faint); margin: 0 8px; }

    /* ---------- Main layout ---------- */
    main { padding: 40px 0 56px; }
    .layout {
      display: grid;
      grid-template-columns: 4fr 6fr;
      gap: 56px;
    }
    .col { min-width: 0; }
    .col > section { margin-bottom: 40px; }
    .col > section:last-child { margin-bottom: 0; }

    /* ---------- Section heads ---------- */
    h2 {
      margin: 0 0 14px;
      font-size: 11px;
      font-weight: 650;
      text-transform: uppercase;
      letter-spacing: .08em;
      color: var(--muted);
    }
    h3 {
      margin: 0 0 6px;
      font-size: 11px;
      color: var(--faint);
      text-transform: uppercase;
      letter-spacing: .06em;
      font-weight: 600;
    }
    .subtle { color: var(--muted); }
    .faint { color: var(--faint); }

    /* ---------- Content blocks ---------- */
    .task {
      font-size: 15px;
      line-height: 1.55;
      overflow-wrap: anywhere;
      white-space: pre-wrap;
    }
    .final-md { font-size: 14px; line-height: 1.65; color: var(--ink); }
    .final-md > *:first-child { margin-top: 0; }
    .final-md > *:last-child { margin-bottom: 0; }
    .final-md h1, .final-md h2, .final-md h3, .final-md h4 {
      margin: 22px 0 8px;
      font-weight: 650;
      letter-spacing: -.005em;
      color: var(--ink);
      text-transform: none;
    }
    .final-md h1 { font-size: 18px; }
    .final-md h2 { font-size: 16px; }
    .final-md h3 { font-size: 14.5px; }
    .final-md h4 { font-size: 13.5px; color: var(--muted); }
    .final-md p { margin: 0 0 10px; }
    .final-md ul, .final-md ol { margin: 0 0 10px; padding-left: 22px; }
    .final-md li { margin: 2px 0; }
    .final-md li > p { margin-bottom: 4px; }
    .final-md strong { font-weight: 650; }
    .final-md em { font-style: italic; }
    .final-md code {
      font: 12.5px ui-monospace, SFMono-Regular, Menlo, monospace;
      background: var(--soft);
      padding: 1px 5px;
      border-radius: 4px;
    }
    .final-md pre {
      background: var(--soft);
      color: var(--ink);
      padding: 12px 14px;
      border-radius: 6px;
      margin: 8px 0 12px;
    }
    .final-md pre code { background: transparent; padding: 0; border-radius: 0; }
    .final-md blockquote {
      margin: 8px 0;
      padding: 4px 12px;
      border-left: 3px solid var(--line);
      color: var(--muted);
    }
    .final-md table { margin: 8px 0 14px; }
    .final-md a { color: var(--accent); text-decoration: none; border-bottom: 1px solid currentColor; }
    .final-md hr { border: 0; border-top: 1px solid var(--line); margin: 18px 0; }
    .eval-line {
      font-size: 14px;
      display: flex;
      gap: 14px;
      align-items: baseline;
      flex-wrap: wrap;
    }
    .eval-line .score { font-size: 22px; font-weight: 650; letter-spacing: -.01em; }
    .eval-line .score.ok { color: var(--ok); }
    .eval-line .score.bad { color: var(--bad); }
    .eval-line .meta { color: var(--muted); font-size: 13px; }

    /* ---------- Badges ---------- */
    .badge {
      display: inline-block;
      border-radius: 999px;
      padding: 2px 9px;
      background: var(--soft);
      font-size: 11.5px;
      line-height: 1.6;
      color: var(--ink);
    }
    .badge.ok { color: var(--ok); background: #ecfdf3; }
    .badge.bad { color: var(--bad); background: #fef2f2; }
    .badge.warn { color: var(--warn); background: #fff7ed; }
    .badge.soft { color: var(--muted); background: var(--soft); }

    /* ---------- Code blocks ---------- */
    pre {
      margin: 0;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
      background: var(--code);
      color: #f8fafc;
      border-radius: 6px;
      padding: 11px 14px;
      overflow-x: auto;
      font: 12.5px/1.55 ui-monospace, SFMono-Regular, Menlo, monospace;
    }
    pre.light {
      background: var(--soft);
      color: var(--ink);
    }
    pre.cmd { background: var(--soft); color: var(--ink); padding: 8px 12px; }

    /* ---------- Timeline ---------- */
    .timeline { display: flex; flex-direction: column; }
    .step {
      padding: 18px 0;
      border-top: 1px solid var(--line);
      min-width: 0;
    }
    .step:first-child { border-top: 0; padding-top: 0; }
    .step-head {
      display: flex;
      gap: 12px;
      align-items: baseline;
      margin-bottom: 8px;
    }
    .step-num {
      color: var(--faint);
      font: 600 12px/1 ui-monospace, SFMono-Regular, Menlo, monospace;
      min-width: 36px;
      text-align: left;
    }
    .step-type {
      display: inline-block;
      font: 600 10.5px/1.6 ui-monospace, SFMono-Regular, Menlo, monospace;
      letter-spacing: .08em;
      padding: 1px 7px;
      border-radius: 4px;
      min-width: 60px;
      text-align: center;
      flex-shrink: 0;
    }
    .step-type.tool { color: #3730a3; background: #eef2ff; }
    .step-type.model { color: #6b21a8; background: #faf5ff; }
    .step-icon {
      display: inline-block;
      font: 600 13px/1 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      min-width: 14px;
      text-align: center;
      color: var(--faint);
      flex-shrink: 0;
    }
    .step-icon.ok { color: var(--ok); }
    .step-icon.bad { color: var(--bad); }
    .step-icon.warn { color: var(--warn); }
    .compression {
      margin: 0 0 10px;
      padding: 8px 10px;
      border: 1px solid #e6dcfb;
      background: #faf7ff;
      border-radius: 8px;
      font-size: 12.5px;
    }
    .compression.failed { border-color: #fbcfcf; background: #fef5f5; }
    .compression-head { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
    .compression-icon { color: #7c3aed; font-weight: 600; }
    .compression-label { font-weight: 600; color: #5b21b6; }
    .compression.failed .compression-label { color: var(--bad); }
    .compression-tokens { color: var(--muted); font-variant-numeric: tabular-nums; margin-left: auto; }
    .compression-body { margin-top: 6px; display: flex; flex-direction: column; gap: 4px; }
    .compression-meta { display: flex; gap: 6px; flex-wrap: wrap; }
    .chip {
      display: inline-block;
      padding: 1px 8px;
      border-radius: 999px;
      background: #f1ecfb;
      color: #5b21b6;
      font-size: 11.5px;
      font-variant-numeric: tabular-nums;
    }
    .chip.subtle { background: var(--soft); color: var(--muted); }
    .step-title {
      flex: 1;
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-weight: 600;
      font-size: 14px;
    }
    .step-title code, .step-title .cmd-inline {
      background: var(--soft);
      padding: 1px 6px;
      border-radius: 4px;
      font: 12.5px/1.4 ui-monospace, SFMono-Regular, Menlo, monospace;
    }
    .step-meta { color: var(--muted); font-size: 12px; white-space: nowrap; font-variant-numeric: tabular-nums; flex-shrink: 0; }
    .step-body { display: grid; gap: 10px; padding-left: 48px; }

    /* ---------- Multiple tool calls within one step ---------- */
    .toolcall {
      min-width: 0;
      display: grid;
      gap: 8px;
      padding: 10px 0 0;
      border-top: 1px dashed var(--line);
    }
    .toolcall:first-child { border-top: 0; padding-top: 0; }
    .toolcall-head {
      display: flex;
      gap: 10px;
      align-items: baseline;
      min-width: 0;
    }
    .toolcall-title {
      flex: 1;
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-weight: 600;
      font-size: 13px;
    }
    .toolcall-title code, .toolcall-title .cmd-inline {
      background: var(--soft);
      padding: 1px 6px;
      border-radius: 4px;
      font: 12px/1.4 ui-monospace, SFMono-Regular, Menlo, monospace;
    }
    .toolcall-body { display: grid; gap: 8px; min-width: 0; }
    .thought {
      color: var(--muted);
      font-style: italic;
      padding: 2px 0;
    }
    .thought::before { content: "“ "; opacity: .6; font-style: normal; }
    .thought::after { content: " ”"; opacity: .6; font-style: normal; }
    /* ---------- Model thinking (collapsible reasoning) ---------- */
    details.thinking {
      min-width: 0;
      border-radius: 6px;
      background: #faf5ff;
      border: 1px solid #f0e6fb;
    }
    details.thinking > summary {
      list-style: none;
      cursor: pointer;
      display: flex;
      gap: 8px;
      align-items: baseline;
      padding: 7px 10px;
      min-width: 0;
    }
    details.thinking > summary::-webkit-details-marker { display: none; }
    details.thinking > summary::before {
      content: "▸";
      color: #a855f7;
      flex-shrink: 0;
      transition: transform .15s;
    }
    details.thinking[open] > summary::before { transform: rotate(90deg); }
    details.thinking[open] > summary { border-bottom: 1px solid #f0e6fb; }
    .thinking-label {
      flex-shrink: 0;
      color: #6b21a8;
      font: 650 11px/1.6 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      text-transform: uppercase;
      letter-spacing: .07em;
    }
    .thinking-preview {
      flex: 1;
      min-width: 0;
      color: var(--muted);
      font-size: 12.5px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .thinking-body {
      display: grid;
      gap: 8px;
      min-width: 0;
      padding: 10px;
    }
    .thinking-text {
      min-width: 0;
      color: var(--ink);
      font-size: 13px;
      line-height: 1.6;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
    }
    .thinking-body .ref { font-size: 12px; overflow-wrap: anywhere; }
    .err-line {
      color: var(--bad);
      font-size: 13px;
      padding: 6px 10px;
      background: #fef2f2;
      border-radius: 6px;
    }
    .err-line::before { content: "⚠ "; }

    /* Group of consecutive parse failures */
    .group {
      padding: 14px 0;
      border-top: 1px solid var(--line);
    }
    .group:first-child { border-top: 0; padding-top: 0; }
    .group > summary {
      list-style: none;
      cursor: pointer;
      display: flex;
      gap: 12px;
      align-items: baseline;
    }
    .group > summary::-webkit-details-marker { display: none; }
    .group > summary:hover .group-title { color: var(--accent); }
    .group-title {
      font-weight: 600;
      font-size: 14px;
      flex: 1;
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .group-error { color: var(--muted); font-size: 12.5px; margin-top: 6px; margin-left: 28px; }
    .group .timeline { margin: 12px 0 4px 28px; padding-left: 14px; border-left: 1px solid var(--line); }

    /* ---------- File block (collapsible source preview) ---------- */
    details.file-block { padding: 0; background: transparent; }
    details.file-block > summary {
      list-style: none;
      cursor: pointer;
      display: inline-flex;
      gap: 8px;
      align-items: baseline;
      padding: 6px 10px;
      border-radius: 6px;
      background: var(--soft);
      font: 12.5px ui-monospace, SFMono-Regular, Menlo, monospace;
      color: var(--ink);
    }
    details.file-block > summary::-webkit-details-marker { display: none; }
    details.file-block > summary::before {
      content: "▸";
      color: var(--faint);
      transition: transform .15s;
    }
    details.file-block[open] > summary::before { transform: rotate(90deg); }
    details.file-block > summary .file-meta { color: var(--muted); }
    details.file-block > pre { margin-top: 6px; }

    /* ---------- Collapsible details / tables ---------- */
    details.collapsible {
      background: var(--soft);
      border-radius: 6px;
      padding: 8px 10px;
    }
    details.collapsible > summary { cursor: pointer; font-size: 13px; color: var(--muted); }
    table { width: 100%; border-collapse: collapse; }
    th, td { border-bottom: 1px solid var(--line); padding: 8px 10px; text-align: left; vertical-align: top; overflow-wrap: anywhere; }
    th { color: var(--muted); font-size: 11px; font-weight: 600; text-transform: uppercase; letter-spacing: .04em; }
    tr:last-child td { border-bottom: 0; }

    /* ---------- Artifact viewer (native dialog) ---------- */
    .artifact-link {
      color: var(--accent);
      text-decoration: none;
      border-bottom: 1px dashed currentColor;
      cursor: pointer;
    }
    .artifact-link:hover { color: #1d4ed8; }
    dialog.artifact-viewer {
      border: 0;
      border-radius: 10px;
      padding: 0;
      max-width: 920px;
      width: min(920px, 92vw);
      max-height: 82vh;
      background: #fff;
      box-shadow: 0 24px 48px rgba(15, 23, 42, .28);
    }
    dialog.artifact-viewer::backdrop {
      background: rgba(15, 23, 42, .42);
    }
    dialog.artifact-viewer .sheet { padding: 20px 24px 22px; }
    dialog.artifact-viewer .dlg-head {
      display: flex;
      align-items: baseline;
      gap: 12px;
      margin-bottom: 6px;
    }
    dialog.artifact-viewer .dlg-title {
      flex: 1;
      font: 13.5px/1.4 ui-monospace, SFMono-Regular, Menlo, monospace;
      word-break: break-all;
    }
    dialog.artifact-viewer .dlg-close {
      background: transparent;
      border: 0;
      color: var(--muted);
      font-size: 22px;
      line-height: 1;
      cursor: pointer;
      padding: 0 4px;
    }
    dialog.artifact-viewer .dlg-meta { color: var(--muted); font-size: 12px; margin-bottom: 12px; }
    dialog.artifact-viewer pre { max-height: 60vh; overflow: auto; }

    /* ---------- Responsive ---------- */
    @media (max-width: 900px) {
      header { padding: 20px 20px 16px; }
      main { padding: 24px 20px 32px; }
      .layout { grid-template-columns: 1fr; gap: 36px; }
      .step-body { padding-left: 0; }
    }
  </style>
</head>
<body>
  <header>
    <div class="wrap header-inner">
      <div class="header-id">
        <h1><span class="run-id">{{.Metadata.RunID}}</span></h1>
        {{if eq .Metadata.Status "completed"}}<span class="badge ok">{{.Metadata.Status}}</span>{{else}}<span class="badge warn">{{.Metadata.Status}}</span>{{end}}
      </div>
      {{if .EvalScorePercent}}<div class="header-score-wrap">
        <div class="header-score-label">评估分</div>
        <div class="header-score {{if .Evaluation.Passed}}ok{{else}}bad{{end}}">{{.EvalScorePercent}}</div>
      </div>{{end}}
      <div class="header-meta">
        {{if .Metadata.Agent}}{{.Metadata.Agent}}<span class="sep">·</span>{{end}}
        {{.Summary.Steps}} steps<span class="sep">·</span>
        {{if .Duration}}{{.Duration}}{{else}}running{{end}}
        {{if .Summary.ToolFailed}}<span class="sep">·</span>{{.Summary.ToolFailed}} tool failure{{if ne .Summary.ToolFailed 1}}s{{end}}{{end}}
        <span class="sep">·</span>{{.Metadata.StartedAt.Format "15:04:05"}} → {{if .Metadata.EndedAt}}{{.Metadata.EndedAt.Format "15:04:05"}}{{else}}running{{end}}
      </div>
    </div>
  </header>

  <main class="wrap">
    <div class="layout">
      <div class="col col-left">
        <section>
          <h2>Task</h2>
          <div class="task">{{.Metadata.Input}}</div>
        </section>

        <section>
          <h2>Final Output</h2>
          {{if .FinalHTML}}<div class="final-md">{{.FinalHTML}}</div>{{else}}<div class="subtle">No final.md content found.</div>{{end}}
        </section>

        <section>
          <h2>Evaluation</h2>
          {{if .EvaluationExists}}
            <div class="eval-line">
              {{if .Evaluation.Passed}}
                <span class="score ok">{{printf "%.2f" .Evaluation.Score}}</span>
                <span class="badge ok">passed</span>
              {{else}}
                <span class="score bad">{{printf "%.2f" .Evaluation.Score}}</span>
                <span class="badge bad">failed</span>
              {{end}}
              <span class="meta">{{len .Evaluation.Evaluators}} evaluator{{if ne (len .Evaluation.Evaluators) 1}}s{{end}}</span>
            </div>
            {{range .Evaluation.Evaluators}}
              <details class="collapsible" style="margin-top:14px">
                <summary>{{.Name}} <span class="badge soft">{{.Type}}</span></summary>
                {{if .Results}}
                  <table>
                    <thead><tr><th>Rule</th><th>Passed</th><th>Message</th></tr></thead>
                    <tbody>{{range .Results}}<tr><td>{{.Rule}}</td><td>{{if .Passed}}<span class="badge ok">true</span>{{else}}<span class="badge bad">false</span>{{end}}</td><td>{{.Message}}</td></tr>{{end}}</tbody>
                  </table>
                {{end}}
              </details>
            {{end}}
          {{else}}
            <div class="subtle">No evaluation.json content found.</div>
          {{end}}
        </section>
      </div>

      <div class="col col-right">
        <section>
          <h2>Process</h2>
          {{if .Blocks}}
            <div class="timeline">
              {{range .Blocks}}
                {{if .IsGroup}}
                  <details class="group">
                    <summary>
                      <span class="step-num">×{{.GroupCount}}</span>
                      <span class="step-type model">MODEL</span>
                      <span class="step-icon warn" aria-label="retry">↺</span>
                      <span class="group-title">{{.GroupTitle}}</span>
                    </summary>
                    <div class="group-error">{{.GroupError}}</div>
                    <div class="timeline">
                      {{range .Steps}}{{template "step" .}}{{end}}
                    </div>
                  </details>
                {{else}}
                  {{template "step" .Step}}
                {{end}}
              {{end}}
            </div>
          {{else}}
            <div class="subtle">No steps recorded.</div>
          {{end}}
        </section>
      </div>
    </div>

  </main>

  {{range .Artifacts}}
    <dialog class="artifact-viewer" id="a-{{.Path}}" aria-label="{{.Path}}">
      <div class="sheet">
        <div class="dlg-head">
          <span class="dlg-title">{{.Path}}</span>
          <button type="button" class="dlg-close" aria-label="Close">×</button>
        </div>
        <div class="dlg-meta">{{.Size}} bytes{{if .ContentType}} · {{.ContentType}}{{end}}</div>
        {{if .Content}}<pre class="light">{{.Content}}</pre>{{else}}<div class="subtle">{{if .ContentNote}}{{.ContentNote}}{{else}}No embedded content.{{end}}</div>{{end}}
      </div>
    </dialog>
  {{end}}

  <script>
    (function () {
      document.addEventListener('click', function (e) {
        var link = e.target.closest('.artifact-link');
        if (link) {
          var id = link.getAttribute('data-target');
          if (!id) return;
          var dlg = document.getElementById(id);
          if (dlg && typeof dlg.showModal === 'function') {
            e.preventDefault();
            dlg.showModal();
          }
          return;
        }
        if (e.target.matches('.dlg-close')) {
          var d = e.target.closest('dialog');
          if (d) d.close();
          return;
        }
        if (e.target.tagName === 'DIALOG') {
          e.target.close();
        }
      });
      document.addEventListener('keydown', function (e) {
        if (e.key === 'Escape') {
          document.querySelectorAll('dialog[open]').forEach(function (d) { d.close(); });
        }
      });
    })();
  </script>
</body>
</html>

{{define "step"}}
<div class="step {{if eq .Status "completed"}}ok{{else if eq .Status "failed"}}failed{{end}}">
  <div class="step-head">
    <span class="step-num">#{{.Number}}</span>
    {{if .TypeLabel}}<span class="step-type {{.TypeClass}}">{{.TypeLabel}}</span>{{end}}
    {{if eq .Status "failed"}}<span class="step-icon bad" aria-label="failed">✗</span>{{else if eq .Kind "final"}}<span class="step-icon" aria-label="final">→</span>{{else if eq .Status "completed"}}<span class="step-icon ok" aria-label="ok">✓</span>{{else}}<span class="step-icon" aria-label="{{.Status}}">·</span>{{end}}
    <span class="step-title">{{.Title}}</span>
    <span class="step-meta">
      {{if eq (len .ToolCalls) 1}}{{with index .ToolCalls 0}}{{if eq .Kind "shell_ok"}}{{if .ShellExitCode}}exit {{.ShellExitCode}}{{end}}{{if .ShellDurationMS}} · {{.ShellDurationMS}} ms{{end}}{{end}}{{end}}{{end}}
    </span>
  </div>
  <div class="step-body">
    {{if and .Compression .Compression.Triggered}}{{template "compression" .Compression}}{{end}}
    {{if .Thought}}<div class="thought">{{.Thought}}</div>{{end}}
    {{if .ReasoningRef}}
      <details class="thinking">
        <summary>
          <span class="thinking-label">Thinking</span>
          {{if .ReasoningPreview}}<span class="thinking-preview">{{.ReasoningPreview}}</span>{{end}}
        </summary>
        <div class="thinking-body">
          {{if .ReasoningContent}}<div class="thinking-text">{{.ReasoningContent}}</div>{{else}}<div class="subtle ref">{{if .ReasoningContentNote}}{{.ReasoningContentNote}}{{else}}No embedded reasoning.{{end}}</div>{{end}}
          <div class="subtle ref">Full reasoning: <a class="artifact-link" href="#" data-target="a-{{.ReasoningRef}}">{{.ReasoningRef}}</a></div>
        </div>
      </details>
    {{end}}

    {{if eq .Kind "parse_failed"}}
      {{if .Error}}<div class="err-line">{{.Error}}</div>{{end}}
      {{if .OutputRef}}<div class="subtle">Raw model output: <a class="artifact-link" href="#" data-target="a-{{.OutputRef}}">{{.OutputRef}}</a></div>{{end}}
    {{end}}

    {{if eq .Kind "tool"}}
      {{if eq (len .ToolCalls) 1}}
        {{with index .ToolCalls 0}}{{template "toolcallbody" .}}{{end}}
      {{else}}
        {{range .ToolCalls}}{{template "toolcallrow" .}}{{end}}
      {{end}}
    {{end}}
  </div>
</div>
{{end}}

{{define "compression"}}
<div class="compression{{if .Failed}} failed{{end}}">
  <div class="compression-head">
    <span class="compression-icon" aria-hidden="true">⊟</span>
    <span class="compression-label">Context compressed</span>
    {{if .Failed}}<span class="badge bad">overflow</span>{{end}}
    {{if .SummaryFailed}}<span class="badge warn">summary failed</span>{{end}}
    {{if .BeforeTokens}}<span class="compression-tokens">{{.BeforeTokens}} → {{.AfterTokens}} tok{{if .ThresholdTokens}} · threshold {{.ThresholdTokens}}{{end}}</span>{{end}}
  </div>
  <div class="compression-body">
    {{if .StrategiesLabel}}<div><span class="subtle">strategy:</span> {{.StrategiesLabel}}</div>{{end}}
    <div class="compression-meta">
      {{if .PreservedBlocks}}<span class="chip">{{.PreservedBlocks}} recent block{{if ne .PreservedBlocks 1}}s{{end}} kept</span>{{end}}
      {{if .TruncatedToolResults}}<span class="chip">{{.TruncatedToolResults}} tool result{{if ne .TruncatedToolResults 1}}s{{end}} truncated</span>{{end}}
      {{if .Summarized}}<span class="chip">summary {{.SummaryTokensIn}}→{{.SummaryTokensOut}} tok</span>{{end}}
      {{if .ContextWindow}}<span class="chip subtle">window {{.ContextWindow}}{{if .EffectiveInputLimit}} · input ≤ {{.EffectiveInputLimit}}{{end}}</span>{{end}}
    </div>
    {{if .Error}}<div class="err-line">{{.Error}}</div>{{end}}
    {{if or .SummaryRef .ReportRef}}
      <div class="subtle ref">
        {{if .SummaryRef}}summary: <a class="artifact-link" href="#" data-target="a-{{.SummaryRef}}">{{.SummaryRef}}</a>{{end}}
        {{if and .SummaryRef .ReportRef}} · {{end}}
        {{if .ReportRef}}report: <a class="artifact-link" href="#" data-target="a-{{.ReportRef}}">{{.ReportRef}}</a>{{end}}
      </div>
    {{end}}
  </div>
</div>
{{end}}

{{define "toolcallrow"}}
<div class="toolcall {{if eq .Status "failed"}}failed{{end}}">
  <div class="toolcall-head">
    {{if eq .Status "failed"}}<span class="step-icon bad" aria-label="failed">✗</span>{{else if eq .Status "completed"}}<span class="step-icon ok" aria-label="ok">✓</span>{{else}}<span class="step-icon" aria-label="{{.Status}}">·</span>{{end}}
    <span class="toolcall-title">{{.Title}}</span>
    <span class="step-meta">{{if eq .Kind "shell_ok"}}{{if .ShellExitCode}}exit {{.ShellExitCode}}{{end}}{{if .ShellDurationMS}} · {{.ShellDurationMS}} ms{{end}}{{end}}</span>
  </div>
  <div class="toolcall-body">{{template "toolcallbody" .}}</div>
</div>
{{end}}

{{define "toolcallbody"}}
  {{if eq .Kind "write"}}
    {{if .FileContent}}
      <details class="file-block">
        <summary>{{if .FilePath}}{{.FilePath}}{{else}}file{{end}}<span class="file-meta">{{if .FileLineCount}} · {{.FileLineCount}} lines{{end}}{{if .FileBytesLabel}} · {{.FileBytesLabel}}{{end}}</span></summary>
        <pre class="light">{{.FileContent}}</pre>
      </details>
    {{else if .FilePath}}
      <div>File: <code>{{.FilePath}}</code>{{if .FileBytesLabel}} <span class="subtle">· {{.FileBytesLabel}}</span>{{end}}{{if .FileContentNote}} <span class="subtle">— {{.FileContentNote}}</span>{{end}}</div>
    {{end}}
  {{end}}

  {{if eq .Kind "shell_ok"}}
    {{if .Command}}<pre class="cmd">$ {{.Command}}</pre>{{end}}
    {{if .ShellStdout}}<div><h3>stdout</h3><pre>{{.ShellStdout}}</pre></div>{{end}}
    {{if .ShellStderr}}<div><h3>stderr</h3><pre>{{.ShellStderr}}</pre></div>{{end}}
  {{end}}

  {{if eq .Kind "shell_failed"}}
    {{if .Command}}<pre class="cmd">$ {{.Command}}</pre>{{end}}
    {{if .Error}}<div class="err-line">{{.Error}}</div>{{end}}
  {{end}}

  {{if eq .Kind "other"}}
    {{if .Tool}}<div class="subtle">tool: {{.Tool}}</div>{{end}}
    {{if .Input}}<details class="collapsible" open><summary>Input</summary><pre class="light">{{.Input}}</pre></details>{{end}}
    {{if .Error}}<div class="err-line">{{.Error}}</div>{{end}}
    {{if .ToolOutput}}<details class="collapsible"><summary>Tool output</summary><pre class="light">{{.ToolOutput}}</pre></details>{{end}}
  {{end}}
{{end}}
`))
