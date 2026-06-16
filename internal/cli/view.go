package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"gopkg.in/yaml.v3"

	"github.com/cosmtrek/jeju/internal/config"
	"github.com/cosmtrek/jeju/internal/evaluate"
	"github.com/cosmtrek/jeju/internal/runs"
	teamrunner "github.com/cosmtrek/jeju/internal/team"
	"github.com/cosmtrek/jeju/internal/trajectory"
)

var markdownRenderer = goldmark.New(goldmark.WithExtensions(extension.GFM))

var openReportFile = defaultOpenReportFile

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

func runView(runID, out, runsDir string) error {
	loaded, err := loadRunFromCandidateStores(runID, runsDir)
	if err != nil {
		return err
	}

	if out == "" {
		out = defaultRunReportPath(loaded.RunDir)
	}
	generated, err := ensureRunReport(loaded.Store, loaded.RunDir, out)
	if err != nil {
		return err
	}
	if generated {
		fmt.Printf("wrote %s\n", out)
	}
	if err := openReportFile(out); err != nil {
		return err
	}
	fmt.Printf("opened %s\n", out)
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

func ensureRunReport(store *runs.Store, runDir *runs.RunDir, out string) (bool, error) {
	stale, err := reportNeedsRender(runDir, out)
	if err != nil {
		return false, err
	}
	if !stale {
		return false, nil
	}
	if err := writeRunReport(store, runDir, out); err != nil {
		return false, err
	}
	return true, nil
}

func reportNeedsRender(runDir *runs.RunDir, out string) (bool, error) {
	trajectoryInfo, err := os.Stat(filepath.Join(runDir.Path, runs.TrajectoryFile))
	if err != nil {
		return false, err
	}
	reportInfo, err := os.Stat(out)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return reportInfo.ModTime().Before(trajectoryInfo.ModTime()), nil
}

func writeRunReport(store *runs.Store, runDir *runs.RunDir, out string) error {
	events, err := trajectory.ReadFile(filepath.Join(runDir.Path, runs.TrajectoryFile))
	if err != nil {
		return err
	}
	if summary, ok := teamrunner.ProjectSummary(trajectory.Project(events)); ok {
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return teamrunner.WriteReport(out, summary)
	}
	report, err := buildRunReport(store, runDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return writeRunReportHTML(out, report)
}

func defaultOpenReportFile(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", abs)
	case "windows":
		cmd = exec.Command("cmd", "/C", "start", "", abs)
	default:
		cmd = exec.Command("xdg-open", abs)
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("open report %q: %w", abs, err)
	}
	return nil
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
	Metrics          runMetrics
	Artifacts        []artifactView
	Steps            []stepView
	Blocks           []stepBlock
	Events           []eventView
	MetadataJSON     string
	IntegrityIssues  []string
}

type artifactView struct {
	Path        string
	Size        int64
	Content     string
	ContentType string
	ContentNote string
}

// runMetrics aggregates the headline indicators surfaced in the report header:
// the model that produced the run, its key parameters, and the cumulative
// step/tool/token counts derived from the trajectory.
type runMetrics struct {
	// model identity and parameters
	Provider string        // friendly provider name (preset, then type, then event)
	ModelID  string        // model id, e.g. deepseek-v4-flash
	Params   []metricParam // temperature, thinking, context window, max output
	Skills   []string      // skills loaded at startup (skill.loaded events)

	// counts
	Steps        int
	ToolCalls    int
	ToolDist     []toolStat // per-tool counts, sorted high → low (capped)
	ToolDistMore int        // tools beyond the displayed cap
	TokensIn     int
	TokensOut    int
	TokensTotal  int

	TokensInLabel    string
	TokensOutLabel   string
	TokensTotalLabel string
}

type metricParam struct {
	Label string
	Value string
}

type toolStat struct {
	Name    string
	Count   int
	Percent int // share of the most-used tool's count, 0-100, for bar width
}

// buildRunMetrics walks the trajectory once to tally steps, tool calls (by
// name), and token usage, then layers in model identity/parameters from the
// config snapshot (falling back to the provider/model recorded on model events).
func buildRunMetrics(events []trajectory.Event, snapshot string) runMetrics {
	record := trajectory.Project(events)
	m := runMetrics{}
	toolCounts := map[string]int{}
	var evProvider, evModel string
	m.Steps = record.Stats.Steps
	m.ToolCalls = record.Stats.ToolCalls
	m.TokensIn = record.Stats.PromptTokens
	m.TokensOut = record.Stats.CompletionTokens
	for _, span := range record.Spans {
		if span.Kind == string(trajectory.SpanTool) {
			name := strings.TrimPrefix(span.Actor, "tool:")
			if name == "" {
				name = "(unknown)"
			}
			toolCounts[name]++
		}
		if span.Kind == string(trajectory.SpanLLM) {
			if evProvider == "" {
				evProvider = stringPayload(span.Attrs, "provider")
			}
			if evModel == "" {
				evModel = stringPayload(span.Attrs, "model")
			}
		}
		if span.Kind == string(trajectory.SpanSkill) && span.Operation == "load" {
			if out := span.Output; len(out) > 0 {
				if name := stringPayload(out, "name"); name != "" {
					m.Skills = append(m.Skills, name)
				}
			}
		}
	}
	m.TokensTotal = m.TokensIn + m.TokensOut
	m.TokensInLabel = formatCount(m.TokensIn)
	m.TokensOutLabel = formatCount(m.TokensOut)
	m.TokensTotalLabel = formatCount(m.TokensTotal)

	for name, count := range toolCounts {
		m.ToolDist = append(m.ToolDist, toolStat{Name: name, Count: count})
	}
	sort.Slice(m.ToolDist, func(i, j int) bool {
		if m.ToolDist[i].Count != m.ToolDist[j].Count {
			return m.ToolDist[i].Count > m.ToolDist[j].Count
		}
		return m.ToolDist[i].Name < m.ToolDist[j].Name
	})
	maxCount := 0
	for _, ts := range m.ToolDist {
		if ts.Count > maxCount {
			maxCount = ts.Count
		}
	}
	for i := range m.ToolDist {
		if maxCount > 0 {
			m.ToolDist[i].Percent = m.ToolDist[i].Count * 100 / maxCount
		}
	}
	const maxToolRows = 6
	if len(m.ToolDist) > maxToolRows {
		m.ToolDistMore = len(m.ToolDist) - maxToolRows
		m.ToolDist = m.ToolDist[:maxToolRows]
	}

	if cfg, ok := activeModelConfig(snapshot); ok {
		switch {
		case cfg.Preset != "":
			m.Provider = cfg.Preset
		case cfg.Type != "":
			m.Provider = cfg.Type
		}
		m.ModelID = cfg.Model
		m.Params = modelParams(cfg)
	}
	if m.Provider == "" {
		m.Provider = evProvider
	}
	if m.ModelID == "" {
		m.ModelID = evModel
	}
	return m
}

// activeModelConfig resolves the model provider the run actually used from the
// config snapshot. It honours runtime.model, then falls back to "primary", then
// to any provider, so reports render even for loosely-specified manifests.
func activeModelConfig(snapshot string) (config.ModelConfig, bool) {
	if snapshot == "" {
		return config.ModelConfig{}, false
	}
	var manifest config.AgentManifest
	if err := yaml.Unmarshal([]byte(snapshot), &manifest); err != nil {
		return config.ModelConfig{}, false
	}
	providers := manifest.Models.Providers
	if len(providers) == 0 {
		return config.ModelConfig{}, false
	}
	if key := manifest.Runtime.Model; key != "" {
		if cfg, ok := providers[key]; ok {
			return cfg, true
		}
	}
	if cfg, ok := providers["primary"]; ok {
		return cfg, true
	}
	for _, cfg := range providers {
		return cfg, true
	}
	return config.ModelConfig{}, false
}

func modelParams(cfg config.ModelConfig) []metricParam {
	var params []metricParam
	if cfg.Temperature != nil {
		params = append(params, metricParam{Label: "temp", Value: strconv.FormatFloat(*cfg.Temperature, 'g', -1, 64)})
	}
	if label := thinkingLabel(cfg.Thinking); label != "" {
		params = append(params, metricParam{Label: "thinking", Value: label})
	}
	if cfg.ContextWindow > 0 {
		params = append(params, metricParam{Label: "ctx", Value: formatTokensShort(cfg.ContextWindow)})
	}
	if cfg.MaxOutputTokens > 0 {
		params = append(params, metricParam{Label: "max out", Value: formatTokensShort(cfg.MaxOutputTokens)})
	}
	return params
}

func thinkingLabel(t config.ThinkingConfig) string {
	switch t.Type {
	case "", "disabled":
		return "off"
	case "enabled":
		if t.Effort != "" {
			return t.Effort
		}
		return "on"
	default:
		if t.Effort != "" {
			return t.Type + " · " + t.Effort
		}
		return t.Type
	}
}

// formatCount renders an integer with thousands separators (e.g. 27023 → "27,023").
func formatCount(n int) string {
	negative := n < 0
	if negative {
		n = -n
	}
	digits := strconv.Itoa(n)
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if negative {
		return "-" + b.String()
	}
	return b.String()
}

// formatTokensShort renders token-window sizes compactly (e.g. 128000 → "128k").
func formatTokensShort(n int) string {
	if n >= 1000 {
		return strconv.Itoa(n/1000) + "k"
	}
	return strconv.Itoa(n)
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
	events, err := trajectory.ReadFile(filepath.Join(runDir.Path, runs.TrajectoryFile))
	if err != nil {
		return runReport{}, err
	}
	record := trajectory.Project(events)
	meta := runs.Metadata{
		RunID:     record.RunID,
		Agent:     record.Agent,
		Status:    record.Status,
		Integrity: record.Integrity,
		StartedAt: record.StartedAt,
		EndedAt:   record.EndedAt,
		Input:     record.Input,
	}
	meta.PackageID = record.Package.ID
	meta.PackageVersion = record.Package.Version
	meta.PackageDigest = record.Package.Digest
	meta.PackageSource = record.Package.Source
	meta.PackageStorePath = record.Package.StorePath
	meta.PackageAgentManifest = record.Package.AgentManifest
	final := record.ArtifactContent(record.FinalRef)
	configSnapshot := record.ArtifactContent(record.ConfigRef)
	evaluation, evaluationExists := evaluationFromRecord(record)
	artifacts := listTrajectoryArtifacts(record)
	workspaceRoot := findWorkspaceRoot(runDir.Path, meta.Agent, workspacePathFromConfig(configSnapshot))
	steps := buildStepViews(record, workspaceRoot)
	blocks := groupSteps(steps)
	metadataJSON, err := marshalIndented(meta)
	if err != nil {
		return runReport{}, err
	}

	duration := ""
	if meta.EndedAt != nil {
		duration = meta.EndedAt.Sub(meta.StartedAt).Round(time.Millisecond).String()
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
		Metrics:          buildRunMetrics(events, configSnapshot),
		Artifacts:        artifacts,
		Steps:            steps,
		Blocks:           blocks,
		Events:           mapEventViews(events),
		MetadataJSON:     metadataJSON,
		IntegrityIssues:  record.IntegrityIssues,
	}, nil
}

func evaluationFromRecord(record trajectory.RunRecord) (*evaluate.Result, bool) {
	for _, span := range record.Spans {
		if span.Kind != string(trajectory.SpanEvaluator) || span.Status != string(trajectory.SpanStatusOK) {
			continue
		}
		if span.OutputRef != "" {
			if result := parseEvaluationContent(record.ArtifactContent(span.OutputRef)); result != nil {
				return result, true
			}
		}
	}
	if record.EvaluationRef != "" {
		if result := parseEvaluationContent(record.ArtifactContent(record.EvaluationRef)); result != nil {
			return result, true
		}
	}
	return nil, false
}

func parseEvaluationContent(content string) *evaluate.Result {
	if content == "" {
		return nil
	}
	var result evaluate.Result
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil
	}
	return &result
}

func listTrajectoryArtifacts(record trajectory.RunRecord) []artifactView {
	artifacts := make([]artifactView, 0, len(record.Artifacts))
	for _, artifact := range record.Artifacts {
		content := artifact.Content()
		view := artifactView{
			Path:        artifact.ID,
			Size:        int64(len(content)),
			Content:     content,
			ContentType: strings.TrimPrefix(strings.ToLower(filepath.Ext(artifact.MediaType)), "."),
		}
		if artifact.MediaType == "application/json" {
			view.ContentType = "json"
		}
		if artifact.MediaType == "text/markdown" {
			view.ContentType = "md"
		}
		artifacts = append(artifacts, view)
	}
	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].Path < artifacts[j].Path
	})
	return artifacts
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

func contentTypeForArtifact(artifact trajectory.Artifact) string {
	switch artifact.MediaType {
	case "application/json":
		return "json"
	case "text/markdown":
		return "md"
	case "text/plain":
		return "txt"
	default:
		return strings.TrimPrefix(strings.ToLower(filepath.Ext(artifact.MediaType)), ".")
	}
}

func mapEventViews(events []trajectory.Event) []eventView {
	out := make([]eventView, 0, len(events))
	for _, event := range events {
		payload := ""
		if len(event.Payload) > 0 {
			payload, _ = marshalIndented(event.Payload)
		}
		out = append(out, eventView{
			ID:          event.EventID,
			Type:        string(event.Type),
			Actor:       event.Actor,
			Step:        event.StepID,
			Timestamp:   event.TS.Format("2006-01-02 15:04:05.000"),
			PayloadJSON: payload,
		})
	}
	return out
}

func buildStepViews(record trajectory.RunRecord, workspaceRoot string) []stepView {
	steps := make([]stepView, 0, len(record.Steps))
	for _, projected := range record.Steps {
		step := stepView{
			Number: projected.ID,
			Status: projected.Status,
			Kind:   projected.Kind,
			Events: mapEventViews(projected.Events),
		}
		if step.Status == "" {
			step.Status = "running"
		}
		if step.Kind == "" {
			step.Kind = "other"
		}
		for _, action := range projected.Actions {
			if thought := stringPayload(action, "thought"); thought != "" {
				step.Thought = thought
			}
			if stringPayload(action, "kind") == "final" {
				step.Kind = "final"
				step.Status = "completed"
			}
		}
		if len(projected.ModelSpans) > 0 {
			modelSpan := projected.ModelSpans[len(projected.ModelSpans)-1]
			step.InputRef = modelSpan.InputRef
			step.OutputRef = modelSpan.OutputRef
			step.ReasoningRef = modelSpan.ReasoningRef
			step.ReasoningPreview = modelSpan.ReasoningPreview
			if modelSpan.ReasoningRef != "" {
				artifact := record.Artifacts[modelSpan.ReasoningRef]
				step.ReasoningContent = artifact.Content()
				step.ReasoningContentType = contentTypeForArtifact(artifact)
			}
			if modelSpan.Status == string(trajectory.SpanStatusError) {
				step.Status = "failed"
				step.Error = modelSpan.Error
			}
		}
		toolCallIndex := map[string]int{}
		for _, action := range projected.Actions {
			if stringPayload(action, "kind") != "tool_call" {
				continue
			}
			callID := stringPayload(action, "tool_call_id")
			tc := toolCallView{
				Status: "running",
				Tool:   stringPayload(action, "function_name"),
			}
			if input, ok := action["arguments"].(map[string]any); ok {
				if cmd := stringPayload(input, "command"); cmd != "" {
					tc.Command = cmd
				}
				if path := stringPayload(input, "path"); path != "" && tc.Tool == "write" {
					tc.FilePath = path
				}
				if pretty, err := marshalIndented(input); err == nil {
					tc.Input = pretty
				}
			}
			step.ToolCalls = append(step.ToolCalls, tc)
			toolCallIndex[callID] = len(step.ToolCalls) - 1
		}
		for _, span := range projected.ToolSpans {
			index, ok := toolCallIndex[span.ToolCallID]
			if !ok {
				step.ToolCalls = append(step.ToolCalls, toolCallView{Tool: strings.TrimPrefix(span.Actor, "tool:")})
				index = len(step.ToolCalls) - 1
				if span.ToolCallID != "" {
					toolCallIndex[span.ToolCallID] = index
				}
			}
			tc := &step.ToolCalls[index]
			if span.Status == string(trajectory.SpanStatusError) {
				tc.Status = "failed"
				tc.Error = span.Error
			} else {
				tc.Status = "completed"
			}
			tc.OutputRef = span.OutputRef
			if span.OutputRef != "" {
				artifact := record.Artifacts[span.OutputRef]
				applyToolOutput(tc, artifactView{Path: artifact.ID, Content: artifact.Content(), ContentType: contentTypeForArtifact(artifact)})
			}
			switch tc.Tool {
			case "shell":
				if tc.Status == "failed" {
					tc.Kind = "shell_failed"
				} else {
					tc.Kind = "shell_ok"
				}
			case "write":
				tc.Kind = "write"
				if tc.FilePath != "" {
					tc.FileContent, tc.FileContentType, tc.FileContentNote, _ = readWorkspaceFile(workspaceRoot, tc.FilePath)
				}
			default:
				tc.Kind = "other"
			}
		}
		if len(step.ToolCalls) > 0 && step.Kind == "tool_call" {
			step.Kind = "tool"
		}
		for _, span := range projected.ContextSpans {
			applyContextSpan(&step, span)
		}
		for _, artifact := range projected.Artifacts {
			step.DebugArtifacts = append(step.DebugArtifacts, artifactRefView{
				Path:        artifact.ID,
				Type:        artifact.Role,
				Content:     artifact.Content(),
				ContentType: contentTypeForArtifact(artifact),
			})
		}
		finalizeStep(&step)
		steps = append(steps, step)
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

func applyContextSpan(step *stepView, span trajectory.SpanRecord) {
	c := ensureCompression(step)
	if boolPayload(span.Attrs, "compression_required") || span.Operation == "compaction" || span.Name == "context compression" {
		c.Triggered = true
	}
	if n := intPayload(span.Metrics, "estimated_tokens"); n > 0 {
		c.EstimatedTokens = n
	}
	if n := intPayload(span.Metrics, "threshold_tokens"); n > 0 {
		c.ThresholdTokens = n
	}
	if n := intPayload(span.Metrics, "context_window"); n > 0 {
		c.ContextWindow = n
	}
	if n := intPayload(span.Metrics, "effective_input_limit"); n > 0 {
		c.EffectiveInputLimit = n
	}
	if before, ok := span.Attrs["before"].(map[string]any); ok {
		c.BeforeTokens = intPayload(before, "tokens")
	}
	if after, ok := span.Attrs["after"].(map[string]any); ok {
		c.AfterTokens = intPayload(after, "tokens")
	}
	if c.BeforeTokens == 0 {
		c.BeforeTokens = intPayload(span.Metrics, "before_tokens")
	}
	if c.AfterTokens == 0 {
		c.AfterTokens = intPayload(span.Metrics, "after_tokens")
	}
	if n := intPayload(span.Metrics, "preserved_blocks"); n > 0 {
		c.PreservedBlocks = n
	}
	if n := intPayload(span.Metrics, "truncated_tool_results"); n > 0 {
		c.TruncatedToolResults = n
	}
	if span.SummaryRef != "" {
		c.SummaryRef = span.SummaryRef
	}
	if span.ReportRef != "" {
		c.ReportRef = span.ReportRef
	}
	if len(span.Strategies) > 0 {
		c.Strategies = stringSliceAny(span.Strategies)
	}
	if span.Status == string(trajectory.SpanStatusError) {
		c.Failed = true
		c.Error = span.Error
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
	return stringSliceAny(raw)
}

func stringSliceAny(raw []any) []string {
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
      grid-template-columns: minmax(0, 1fr) 320px;
      gap: 28px 64px;
      align-items: start;
    }

    /* ----- left: identity + setup ----- */
    .header-setup { min-width: 0; }
    .header-id {
      display: flex;
      gap: 12px;
      align-items: baseline;
      flex-wrap: wrap;
    }
    .agent-name {
      margin: 0;
      font-size: 22px;
      font-weight: 650;
      letter-spacing: -.015em;
    }
    .run-id {
      margin-top: 5px;
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 12.5px;
      color: var(--muted);
    }
    .kv {
      margin: 20px 0 0;
      display: flex;
      flex-direction: column;
      gap: 9px;
    }
    .kv-row {
      display: flex;
      gap: 16px;
      align-items: baseline;
    }
    .kv dt {
      flex: 0 0 52px;
      font-size: 12px;
      color: var(--muted);
      letter-spacing: .02em;
    }
    .kv dd {
      margin: 0;
      flex: 1;
      min-width: 0;
      font-size: 13px;
      color: var(--ink);
      overflow-wrap: anywhere;
    }
    .kv dd.mono {
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 12.5px;
      font-weight: 600;
    }
    .kv .sep { color: var(--faint); margin: 0 6px; }
    .kv .param-k { color: var(--muted); }
    .kv .param-v { color: var(--ink); font-weight: 600; font-variant-numeric: tabular-nums; }
    .kv .skill {
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 12px;
    }
    .model-slash { color: var(--faint); margin: 0 1px; }

    /* ----- right: result ledger (big numbers + drill-down) ----- */
    .header-result {
      display: flex;
      flex-direction: column;
    }
    .metric-group {
      border-top: 1px solid var(--line);
      padding: 9px 0;
    }
    .metric-group:first-child { border-top: 0; padding-top: 0; }
    .metric {
      display: flex;
      justify-content: space-between;
      align-items: baseline;
      gap: 16px;
    }
    .metric-label {
      font-size: 12px;
      color: var(--muted);
      letter-spacing: .03em;
    }
    .metric-val {
      font: 700 22px/1 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      letter-spacing: -.02em;
      font-variant-numeric: tabular-nums;
      color: var(--ink);
    }
    .metric-val.bad { color: var(--bad); }
    .metric-sub {
      margin-top: 6px;
      font-size: 12px;
      color: var(--muted);
      font-variant-numeric: tabular-nums;
      text-align: right;
    }
    .metric-sub .sep { color: var(--faint); margin: 0 5px; }

    /* tool-call distribution list (drill-down under "tool calls") */
    .tool-dist {
      margin-top: 9px;
      display: flex;
      flex-direction: column;
      gap: 7px;
    }
    .tool-row {
      display: grid;
      grid-template-columns: minmax(48px, max-content) 1fr auto;
      align-items: center;
      gap: 10px;
    }
    .tool-name {
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 12px;
      color: var(--ink);
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }
    .tool-bar {
      height: 5px;
      background: var(--soft);
      border-radius: 999px;
      overflow: hidden;
    }
    .tool-bar-fill {
      display: block;
      height: 100%;
      background: var(--accent);
      border-radius: 999px;
    }
    .tool-count {
      font-size: 12px;
      font-variant-numeric: tabular-nums;
      color: var(--muted);
      min-width: 1.5em;
      text-align: right;
    }
    .tool-more { font-size: 11.5px; color: var(--faint); padding-left: 2px; }

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
    .issue-list {
      margin: 10px 0 0;
      padding-left: 18px;
      color: var(--muted);
    }
    .issue-list li { margin: 3px 0; }
    .issue-list code {
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 12px;
      color: var(--ink);
      background: var(--soft);
      border-radius: 4px;
      padding: 2px 5px;
      overflow-wrap: anywhere;
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
      .header-inner { grid-template-columns: 1fr; gap: 18px; }
      .header-stats { gap: 28px; }
      .stat { text-align: left; }
      .header-tools { grid-template-columns: 1fr; gap: 10px; }
    }
  </style>
</head>
<body>
  <header>
    <div class="wrap header-inner">
      <div class="header-setup">
        <div class="header-id">
          <h1 class="agent-name">{{if .Metadata.Agent}}{{.Metadata.Agent}}{{else}}{{.Metadata.RunID}}{{end}}</h1>
          {{if eq .Metadata.Status "completed"}}<span class="badge ok">{{.Metadata.Status}}</span>{{else}}<span class="badge warn">{{.Metadata.Status}}</span>{{end}}
        </div>
        <div class="run-id">{{.Metadata.RunID}}</div>
        <dl class="kv">
          {{if .Metadata.PackageID}}<div class="kv-row">
            <dt>package</dt>
            <dd class="mono">{{.Metadata.PackageID}}{{if .Metadata.PackageVersion}}@{{.Metadata.PackageVersion}}{{end}}</dd>
          </div>{{end}}
          {{if .Metadata.PackageDigest}}<div class="kv-row">
            <dt>package digest</dt>
            <dd class="mono">{{.Metadata.PackageDigest}}</dd>
          </div>{{end}}
          {{if .Metadata.PackageSource}}<div class="kv-row">
            <dt>package source</dt>
            <dd class="mono">{{.Metadata.PackageSource}}</dd>
          </div>{{else if .Metadata.PackageStorePath}}<div class="kv-row">
            <dt>package store</dt>
            <dd class="mono">{{.Metadata.PackageStorePath}}</dd>
          </div>{{end}}
          {{if .Metrics.ModelID}}<div class="kv-row">
            <dt>model</dt>
            <dd class="mono">{{if .Metrics.Provider}}{{.Metrics.Provider}}<span class="model-slash">/</span>{{end}}{{.Metrics.ModelID}}</dd>
          </div>{{end}}
          {{if .Metrics.Params}}<div class="kv-row">
            <dt>params</dt>
            <dd>{{range $i, $p := .Metrics.Params}}{{if $i}}<span class="sep">·</span>{{end}}<span class="param-k">{{$p.Label}}</span> <span class="param-v">{{$p.Value}}</span>{{end}}</dd>
          </div>{{end}}
          {{if .Metrics.Skills}}<div class="kv-row">
            <dt>skills</dt>
            <dd>{{range $i, $s := .Metrics.Skills}}{{if $i}}<span class="sep">·</span>{{end}}<span class="skill">{{$s}}</span>{{end}}</dd>
          </div>{{end}}
          {{if .Metadata.Integrity}}<div class="kv-row">
            <dt>trajectory</dt>
            <dd>{{.Metadata.Integrity}}{{if .IntegrityIssues}} <span class="subtle">({{len .IntegrityIssues}} issue{{if ne (len .IntegrityIssues) 1}}s{{end}})</span>{{end}}</dd>
          </div>{{end}}
        </dl>
      </div>

      <div class="header-result">
        <div class="metric-group">
          <div class="metric"><span class="metric-label">steps</span><span class="metric-val">{{.Metrics.Steps}}</span></div>
        </div>
        <div class="metric-group">
          <div class="metric"><span class="metric-label">tool calls</span><span class="metric-val">{{.Metrics.ToolCalls}}</span></div>
          {{if .Metrics.ToolDist}}<div class="tool-dist">
            {{range .Metrics.ToolDist}}<div class="tool-row">
              <span class="tool-name">{{.Name}}</span>
              <span class="tool-bar"><span class="tool-bar-fill" style="width:{{.Percent}}%"></span></span>
              <span class="tool-count">{{.Count}}</span>
            </div>{{end}}
            {{if .Metrics.ToolDistMore}}<div class="tool-more">+{{.Metrics.ToolDistMore}} more</div>{{end}}
          </div>{{end}}
        </div>
        <div class="metric-group">
          <div class="metric"><span class="metric-label">tokens</span><span class="metric-val">{{.Metrics.TokensTotalLabel}}</span></div>
          <div class="metric-sub">in {{.Metrics.TokensInLabel}} <span class="sep">·</span> out {{.Metrics.TokensOutLabel}}</div>
        </div>
        <div class="metric-group">
          <div class="metric"><span class="metric-label">duration</span><span class="metric-val">{{if .Duration}}{{.Duration}}{{else}}running{{end}}</span></div>
          <div class="metric-sub">{{.Metadata.StartedAt.Format "15:04:05"}} → {{if .Metadata.EndedAt}}{{.Metadata.EndedAt.Format "15:04:05"}}{{else}}running{{end}}</div>
        </div>
        {{if .Summary.ToolFailed}}<div class="metric-group">
          <div class="metric"><span class="metric-label">tool failures</span><span class="metric-val bad">{{.Summary.ToolFailed}}</span></div>
        </div>{{end}}
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

        {{if .IntegrityIssues}}
        <section>
          <h2>Trajectory Integrity</h2>
          <div class="subtle">This report was projected from a {{.Metadata.Integrity}} trajectory.</div>
          <ul class="issue-list">
            {{range .IntegrityIssues}}<li><code>{{.}}</code></li>{{end}}
          </ul>
        </section>
        {{end}}

        <section>
          <h2>Final Output</h2>
          {{if .FinalHTML}}<div class="final-md">{{.FinalHTML}}</div>{{else}}<div class="subtle">No final artifact found.</div>{{end}}
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
            <div class="subtle">Not evaluated for this run.</div>
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
