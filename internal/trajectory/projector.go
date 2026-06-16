package trajectory

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type RunRecord struct {
	RunID           string
	TrajectoryID    string
	SessionID       string
	Agent           string
	Package         PackageProvenance
	Input           string
	Status          string
	Integrity       string
	IntegrityIssues []string
	StartedAt       time.Time
	EndedAt         *time.Time
	DurationMS      int64
	FinalRef        string
	ConfigRef       string
	Evaluation      map[string]any
	EvaluationRef   string
	Stats           RunStats
	Artifacts       map[string]Artifact
	Spans           map[string]SpanRecord
	Steps           []ProjectedStep
	Events          []Event
}

type RunStats struct {
	Steps                int
	ModelCalls           int
	ToolCalls            int
	ModelErrors          int
	ToolErrors           int
	PermissionDenied     int
	PromptTokens         int
	PromptCacheHitTokens int
	CompletionTokens     int
	TotalTokens          int
}

type PackageProvenance struct {
	ID            string
	Version       string
	Digest        string
	Source        string
	StorePath     string
	AgentManifest string
	Resolved      map[string]any
}

const (
	IntegrityComplete = "complete"
	IntegrityPartial  = "partial"
	IntegrityCorrupt  = "corrupt"
)

type Artifact struct {
	ID        string
	Role      string
	MediaType string
	Encoding  string
	Text      string
	Value     any
	Bytes     int64
	SHA256    string
	Chunked   bool
	Data      []byte
}

func (a Artifact) Content() string {
	if a.Text != "" {
		return a.Text
	}
	if a.Value != nil {
		data, err := json.MarshalIndent(a.Value, "", "  ")
		if err == nil {
			return string(data)
		}
	}
	if len(a.Data) > 0 {
		return string(a.Data)
	}
	return ""
}

type SpanRecord struct {
	ID               string
	ParentID         string
	StepID           int
	Kind             string
	Name             string
	Actor            string
	Status           string
	Operation        string
	InputRef         string
	OutputRef        string
	Output           map[string]any
	Error            string
	ReasoningRef     string
	ReasoningPreview string
	StartedAt        time.Time
	EndedAt          *time.Time
	Metrics          map[string]any
	Attrs            map[string]any
	ToolCallID       string
	SummaryRef       string
	ReportRef        string
	Strategies       []any
}

type ProjectedStep struct {
	ID           int
	SpanID       string
	Status       string
	Thought      string
	Kind         string
	ModelSpans   []SpanRecord
	ToolSpans    []SpanRecord
	ContextSpans []SpanRecord
	Actions      []map[string]any
	Permissions  []map[string]any
	Messages     []map[string]any
	Artifacts    []Artifact
	Events       []Event
}

func Project(events []Event) RunRecord {
	record := RunRecord{
		Integrity: IntegrityComplete,
		Artifacts: map[string]Artifact{},
		Spans:     map[string]SpanRecord{},
		Events:    events,
	}
	chunks := map[string][][]byte{}
	chunkedArtifacts := map[string]bool{}
	finalizedArtifacts := map[string]bool{}
	startedSpans := map[string]bool{}
	endedSpans := map[string]bool{}
	steps := map[int]*ProjectedStep{}
	headerSeen := false
	summarySeen := false
	var expectedSeq uint64
	for _, event := range events {
		if event.Seq > 0 {
			if expectedSeq == 0 {
				expectedSeq = 1
			}
			if event.Seq != expectedSeq {
				addIntegrityIssue(&record, "corrupt:seq_gap")
				expectedSeq = event.Seq
			}
			expectedSeq++
		}
		if record.RunID == "" {
			record.RunID = event.RunID
			record.TrajectoryID = event.TrajectoryID
			record.SessionID = event.SessionID
		}
		step := ensureStep(steps, event.StepID)
		if step != nil {
			step.Events = append(step.Events, event)
		}
		switch event.Type {
		case EventTrajectoryHeader:
			headerSeen = true
			record.RunID = event.RunID
			record.TrajectoryID = event.TrajectoryID
			record.SessionID = event.SessionID
			agentPayload := mapPayload(event.Payload, "agent")
			record.Agent = stringPayload(agentPayload, "name")
			record.Package = packageProvenanceFromPayload(mapPayload(agentPayload, "package"))
			record.Input = stringPayload(event.Payload, "input")
			record.StartedAt = event.TS
		case EventSpanStarted:
			span := spanFromEvent(event)
			if span.ID == "" {
				addIntegrityIssue(&record, "partial:span_started_missing_id")
			} else {
				startedSpans[span.ID] = true
			}
			span.StartedAt = event.TS
			record.Spans[span.ID] = span
			if span.Kind == string(SpanStep) && event.StepID > 0 && step != nil {
				step.SpanID = span.ID
			}
		case EventSpanEnded:
			span := record.Spans[event.SpanID]
			if span.ID == "" {
				span = spanFromEvent(event)
			}
			if span.ID == "" {
				addIntegrityIssue(&record, "partial:span_ended_missing_id")
			} else {
				endedSpans[span.ID] = true
				if !startedSpans[span.ID] {
					addIntegrityIssue(&record, "partial:span_end_without_start:"+span.ID)
				}
			}
			ended := event.TS
			span.EndedAt = &ended
			span.Status = stringPayload(event.Payload, "status")
			span.OutputRef = nestedString(event.Payload, "output", "content_ref")
			span.Output = mapPayload(event.Payload, "output")
			span.Error = nestedString(event.Payload, "error", "message")
			span.Metrics = mapPayload(event.Payload, "metrics")
			span.ToolCallID = stringPayload(event.Payload, "tool_call_id")
			record.Spans[span.ID] = span
			applySpanStats(&record.Stats, span)
			if span.Kind == string(SpanStep) && step != nil {
				step.Status = span.Status
			}
		case EventActionCreated:
			if step != nil {
				step.Actions = append(step.Actions, event.Payload)
				if thought := stringPayload(event.Payload, "thought"); thought != "" {
					step.Thought = thought
				}
				if kind := stringPayload(event.Payload, "kind"); kind != "" {
					step.Kind = kind
				}
			}
			if stringPayload(event.Payload, "kind") == "final" {
				if ref := nestedString(event.Payload, "final", "content_ref"); ref != "" {
					record.FinalRef = ref
				}
			}
		case EventMessageCreated:
			if step != nil {
				step.Messages = append(step.Messages, event.Payload)
			}
		case EventPermissionDecided:
			if step != nil {
				step.Permissions = append(step.Permissions, event.Payload)
			}
			if stringPayload(event.Payload, "decision") == "denied" {
				record.Stats.PermissionDenied++
			}
		case EventArtifactCreated:
			artifact := artifactFromPayload(event.Payload)
			if artifact.ID == "" {
				addIntegrityIssue(&record, "partial:artifact_missing_id")
			}
			record.Artifacts[artifact.ID] = artifact
			if artifact.Chunked {
				chunkedArtifacts[artifact.ID] = true
			}
			switch artifact.Role {
			case "config_snapshot":
				record.ConfigRef = artifact.ID
			case "final":
				record.FinalRef = artifact.ID
			case "evaluation":
				record.EvaluationRef = artifact.ID
			case "package_provenance":
				if record.Package.ID == "" {
					record.Package = packageProvenanceFromContent(artifact.Content())
				}
			}
			if step != nil {
				step.Artifacts = append(step.Artifacts, artifact)
			}
		case EventArtifactChunk:
			id := stringPayload(event.Payload, "artifact_id")
			if id == "" {
				addIntegrityIssue(&record, "partial:artifact_chunk_missing_id")
			} else if !chunkedArtifacts[id] {
				addIntegrityIssue(&record, "partial:artifact_chunk_without_created:"+id)
			}
			data := stringPayload(event.Payload, "data")
			if stringPayload(event.Payload, "encoding") == "base64" {
				decoded, err := base64.StdEncoding.DecodeString(data)
				if err == nil {
					chunks[id] = append(chunks[id], decoded)
				} else {
					addIntegrityIssue(&record, "corrupt:artifact_chunk_base64:"+id)
				}
			} else {
				chunks[id] = append(chunks[id], []byte(data))
			}
		case EventArtifactFinalized:
			id := stringPayload(event.Payload, "artifact_id")
			if id == "" {
				addIntegrityIssue(&record, "partial:artifact_finalized_missing_id")
			}
			finalizedArtifacts[id] = true
			artifact := record.Artifacts[id]
			for _, chunk := range chunks[id] {
				artifact.Data = append(artifact.Data, chunk...)
			}
			artifact.Bytes = int64Payload(event.Payload, "bytes")
			artifact.SHA256 = stringPayload(event.Payload, "sha256")
			record.Artifacts[id] = artifact
		case EventRunSummary:
			summarySeen = true
			if status := stringPayload(event.Payload, "status"); status != "" {
				record.Status = status
			}
			if ref := nestedString(event.Payload, "final", "content_ref"); ref != "" {
				record.FinalRef = ref
			}
			if _, ok := event.Payload["duration_ms"]; ok {
				record.DurationMS = int64Payload(event.Payload, "duration_ms")
			}
			if ended := stringPayload(event.Payload, "ended_at"); ended != "" {
				if parsed, err := time.Parse(time.RFC3339Nano, ended); err == nil {
					record.EndedAt = &parsed
				}
			} else if record.EndedAt == nil {
				ended := event.TS
				record.EndedAt = &ended
			}
			if evaluation := mapPayload(event.Payload, "evaluation"); len(evaluation) > 0 {
				record.Evaluation = evaluation
			}
		}
	}
	if len(events) == 0 {
		addIntegrityIssue(&record, "partial:empty")
	}
	if !headerSeen {
		addIntegrityIssue(&record, "partial:missing_header")
	}
	if !summarySeen {
		addIntegrityIssue(&record, "partial:missing_run_summary")
	}
	for id := range startedSpans {
		if !endedSpans[id] {
			addIntegrityIssue(&record, "partial:open_span:"+id)
		}
	}
	for id := range chunkedArtifacts {
		if !finalizedArtifacts[id] {
			addIntegrityIssue(&record, "partial:unfinalized_artifact:"+id)
		}
	}
	for _, span := range record.Spans {
		if span.StepID <= 0 {
			continue
		}
		step := ensureStep(steps, span.StepID)
		switch span.Kind {
		case string(SpanLLM):
			step.ModelSpans = append(step.ModelSpans, span)
		case string(SpanTool), string(SpanShell):
			step.ToolSpans = append(step.ToolSpans, span)
		case string(SpanContext):
			step.ContextSpans = append(step.ContextSpans, span)
		}
	}
	for _, step := range steps {
		if step.ID <= 0 {
			continue
		}
		sort.Slice(step.ModelSpans, func(i, j int) bool { return step.ModelSpans[i].StartedAt.Before(step.ModelSpans[j].StartedAt) })
		sort.Slice(step.ToolSpans, func(i, j int) bool { return step.ToolSpans[i].StartedAt.Before(step.ToolSpans[j].StartedAt) })
		sort.Slice(step.ContextSpans, func(i, j int) bool { return step.ContextSpans[i].StartedAt.Before(step.ContextSpans[j].StartedAt) })
		record.Steps = append(record.Steps, *step)
	}
	sort.Slice(record.Steps, func(i, j int) bool { return record.Steps[i].ID < record.Steps[j].ID })
	if record.Stats.Steps == 0 {
		record.Stats.Steps = len(record.Steps)
	}
	if record.Status == "" {
		if record.FinalRef != "" {
			record.Status = "completed"
		} else {
			record.Status = "interrupted"
		}
	}
	return record
}

func ensureStep(steps map[int]*ProjectedStep, id int) *ProjectedStep {
	if id <= 0 {
		return nil
	}
	step := steps[id]
	if step == nil {
		step = &ProjectedStep{ID: id, Status: "running"}
		steps[id] = step
	}
	return step
}

func spanFromEvent(event Event) SpanRecord {
	return SpanRecord{
		ID:               event.SpanID,
		ParentID:         event.ParentSpanID,
		StepID:           event.StepID,
		Kind:             stringPayload(event.Payload, "kind"),
		Name:             stringPayload(event.Payload, "name"),
		Operation:        stringPayload(event.Payload, "operation"),
		Actor:            event.Actor,
		InputRef:         nestedString(event.Payload, "input", "content_ref"),
		OutputRef:        nestedString(event.Payload, "output", "content_ref"),
		Output:           mapPayload(event.Payload, "output"),
		ReasoningRef:     nestedString(event.Payload, "reasoning", "content_ref"),
		ReasoningPreview: nestedString(event.Payload, "reasoning", "preview"),
		StartedAt:        event.TS,
		Attrs:            mapPayload(event.Payload, "attrs"),
		Metrics:          mapPayload(event.Payload, "metrics"),
		ToolCallID:       stringPayload(event.Payload, "tool_call_id"),
		SummaryRef:       stringPayload(event.Payload, "summary_ref"),
		ReportRef:        nestedString(event.Payload, "attrs", "report_ref"),
		Strategies:       slicePayload(mapPayload(event.Payload, "attrs"), "strategies"),
	}
}

func artifactFromPayload(payload map[string]any) Artifact {
	return Artifact{
		ID:        stringPayload(payload, "artifact_id"),
		Role:      stringPayload(payload, "role"),
		MediaType: stringPayload(payload, "media_type"),
		Encoding:  stringPayload(payload, "encoding"),
		Text:      stringPayload(payload, "text"),
		Value:     payload["value"],
		Chunked:   boolPayload(payload, "chunked"),
	}
}

func packageProvenanceFromPayload(payload map[string]any) PackageProvenance {
	if len(payload) == 0 {
		return PackageProvenance{}
	}
	return PackageProvenance{
		ID:            stringPayload(payload, "id"),
		Version:       stringPayload(payload, "version"),
		Digest:        stringPayload(payload, "digest"),
		Source:        stringPayload(payload, "source"),
		StorePath:     stringPayload(payload, "store_path"),
		AgentManifest: stringPayload(payload, "agent_manifest"),
		Resolved:      mapPayload(payload, "resolved"),
	}
}

func packageProvenanceFromContent(content string) PackageProvenance {
	if content == "" {
		return PackageProvenance{}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return PackageProvenance{}
	}
	return packageProvenanceFromPayload(payload)
}

func applySpanStats(stats *RunStats, span SpanRecord) {
	switch span.Kind {
	case string(SpanLLM):
		if span.Status == string(SpanStatusError) {
			stats.ModelErrors++
		} else {
			stats.ModelCalls++
			stats.PromptTokens += intPayload(span.Metrics, "prompt_tokens")
			stats.PromptCacheHitTokens += intPayload(span.Metrics, "prompt_cache_hit_tokens")
			stats.CompletionTokens += intPayload(span.Metrics, "completion_tokens")
			stats.TotalTokens += intPayload(span.Metrics, "total_tokens")
		}
	case string(SpanTool):
		if span.Status == string(SpanStatusError) {
			stats.ToolErrors++
		} else {
			stats.ToolCalls++
		}
	}
}

func addIntegrityIssue(record *RunRecord, issue string) {
	if issue == "" {
		return
	}
	for _, existing := range record.IntegrityIssues {
		if existing == issue {
			return
		}
	}
	record.IntegrityIssues = append(record.IntegrityIssues, issue)
	if strings.HasPrefix(issue, "corrupt:") {
		record.Integrity = IntegrityCorrupt
		return
	}
	if record.Integrity != IntegrityCorrupt {
		record.Integrity = IntegrityPartial
	}
}

func stringPayload(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	if value, ok := payload[key].(string); ok {
		return value
	}
	return ""
}

func nestedString(payload map[string]any, key, nested string) string {
	return stringPayload(mapPayload(payload, key), nested)
}

func mapPayload(payload map[string]any, key string) map[string]any {
	if payload == nil {
		return nil
	}
	if value, ok := payload[key].(map[string]any); ok {
		return value
	}
	return nil
}

func boolPayload(payload map[string]any, key string) bool {
	if payload == nil {
		return false
	}
	if value, ok := payload[key].(bool); ok {
		return value
	}
	return false
}

func slicePayload(payload map[string]any, key string) []any {
	if payload == nil {
		return nil
	}
	if values, ok := payload[key].([]any); ok {
		return values
	}
	return nil
}

func intPayload(payload map[string]any, key string) int {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	default:
		return 0
	}
}

func int64Payload(payload map[string]any, key string) int64 {
	if payload == nil {
		return 0
	}
	switch value := payload[key].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	case json.Number:
		n, _ := value.Int64()
		return n
	default:
		return 0
	}
}

func (r RunRecord) ArtifactContent(ref string) string {
	if ref == "" {
		return ""
	}
	if artifact, ok := r.Artifacts[ref]; ok {
		return artifact.Content()
	}
	return ""
}

func (r RunRecord) MustArtifact(ref string) (Artifact, error) {
	artifact, ok := r.Artifacts[ref]
	if !ok {
		return Artifact{}, fmt.Errorf("artifact %q not found", ref)
	}
	return artifact, nil
}
