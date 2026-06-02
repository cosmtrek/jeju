package runtime

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/compiler"
	"github.com/cosmtrek/jeju/internal/config"
	"github.com/cosmtrek/jeju/internal/contextmgr"
	"github.com/cosmtrek/jeju/internal/memory"
	"github.com/cosmtrek/jeju/internal/model"
	"github.com/cosmtrek/jeju/internal/policy"
	"github.com/cosmtrek/jeju/internal/runs"
	"github.com/cosmtrek/jeju/internal/sandbox"
	"github.com/cosmtrek/jeju/internal/skills"
	"github.com/cosmtrek/jeju/internal/tools"
	"github.com/cosmtrek/jeju/internal/trajectory"
)

type failIfCalledClient struct {
	calls int
}

func (c *failIfCalledClient) Generate(ctx context.Context, req model.Request) (model.Response, error) {
	c.calls++
	return model.Response{}, fmt.Errorf("model should not be called")
}

type summaryOnlyClient struct {
	calls    int
	fail     bool
	requests []model.Request
}

func (c *summaryOnlyClient) Generate(ctx context.Context, req model.Request) (model.Response, error) {
	c.calls++
	c.requests = append(c.requests, req)
	if c.fail {
		return model.Response{}, fmt.Errorf("summary failed")
	}
	return model.Response{
		Text:     `{"summary":"model generated context summary"}`,
		Model:    req.Model,
		Provider: "mock",
		Usage:    model.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
	}, nil
}

func TestPrepareModelRequestRecordsContextCompression(t *testing.T) {
	tmp := t.TempDir()
	store := runs.NewStore(filepath.Join(tmp, "runs"))
	runDir, err := store.CreateRun("context", "current task")
	if err != nil {
		t.Fatalf("Create run failed: %v", err)
	}
	recorder, err := trajectory.NewRecorder(runDir.Path)
	if err != nil {
		t.Fatalf("NewRecorder failed: %v", err)
	}
	box, err := sandbox.NewLocal(filepath.Join(tmp, "workspace"))
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	agent := &compiler.CompiledAgent{
		Name:         "context",
		Instructions: "Keep context concise.",
		Config: config.AgentManifest{
			Metadata: config.Metadata{Name: "context"},
			Runtime: config.RuntimeConfig{
				CompressionThreshold: 0.8,
			},
		},
		Tools:    tools.NewRegistry(),
		Skills:   skills.NewRegistry(),
		Sandbox:  box,
		RunStore: store,
	}
	state := NewRunState(runDir.RunID, "context", "current task")
	state.Messages = []model.Message{
		{Role: "user", Content: "old setup"},
		{Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "current task"},
		{Role: "tool", ToolCallID: "old_call", Content: longText("old result ", 600)},
		{Role: "assistant", Content: "old follow-up"},
		{Role: "user", Content: "recent request"},
		{Role: "assistant", Content: "recent answer"},
	}

	client := &summaryOnlyClient{}
	req, err := New().prepareModelRequest(context.Background(), agent, recorder, state, client, model.ProviderConfig{
		Name:          "primary",
		Provider:      "mock",
		Model:         "mock",
		ContextWindow: 2700,
	})
	if err != nil {
		t.Fatalf("prepareModelRequest failed: %v", err)
	}
	if len(req.Messages) == 0 {
		t.Fatal("expected request messages")
	}
	if state.LastTokenEstimate == 0 {
		t.Fatal("expected state token estimate to be recorded")
	}
	events, err := trajectory.ReadFile(filepath.Join(runDir.Path, runs.TrajectoryFile))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	requireRuntimeEventTypes(t, events,
		trajectory.EventContextEstimated,
		trajectory.EventContextCompressionStarted,
		trajectory.EventContextCompressionCompleted,
	)
	if client.calls > 0 {
		requireRuntimeEventTypes(t, events,
			trajectory.EventContextSummaryStarted,
			trajectory.EventContextSummaryCompleted,
		)
		requireEventOrder(t, events,
			trajectory.EventContextEstimated,
			trajectory.EventContextCompressionStarted,
			trajectory.EventContextSummaryStarted,
			trajectory.EventContextSummaryCompleted,
			trajectory.EventContextCompressionCompleted,
		)
	}
}

func TestPrepareModelRequestDegradesWhenSummaryFails(t *testing.T) {
	tmp := t.TempDir()
	store := runs.NewStore(filepath.Join(tmp, "runs"))
	runDir, err := store.CreateRun("context", "current task")
	if err != nil {
		t.Fatalf("Create run failed: %v", err)
	}
	recorder, err := trajectory.NewRecorder(runDir.Path)
	if err != nil {
		t.Fatalf("NewRecorder failed: %v", err)
	}
	box, err := sandbox.NewLocal(filepath.Join(tmp, "workspace"))
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	agent := &compiler.CompiledAgent{
		Name:         "context",
		Instructions: "Keep context concise.",
		Config: config.AgentManifest{
			Metadata: config.Metadata{Name: "context"},
			Runtime: config.RuntimeConfig{
				CompressionThreshold: 0.8,
			},
		},
		Tools:    tools.NewRegistry(),
		Skills:   skills.NewRegistry(),
		Sandbox:  box,
		RunStore: store,
	}
	state := NewRunState(runDir.RunID, "context", "current task")
	state.Messages = []model.Message{
		{Role: "user", Content: longText("old user ", 260)},
		{Role: "assistant", Content: longText("old assistant ", 260)},
		{Role: "user", Content: "middle request"},
		{Role: "assistant", Content: "middle answer"},
		{Role: "user", Content: "recent request"},
		{Role: "assistant", Content: "recent answer"},
	}
	client := &summaryOnlyClient{fail: true}

	req, err := New().prepareModelRequest(context.Background(), agent, recorder, state, client, model.ProviderConfig{
		Name:          "primary",
		Provider:      "mock",
		Model:         "mock",
		ContextWindow: 2600,
	})
	if err != nil {
		t.Fatalf("prepareModelRequest should degrade instead of failing: %v", err)
	}
	if len(req.Messages) == 0 || state.Status == StatusFailed {
		t.Fatalf("unexpected failed state after summary degradation: status=%s req=%+v", state.Status, req)
	}
	if !containsText(req.Messages, "recent request") {
		t.Fatalf("degraded request should keep recent messages: %+v", req.Messages)
	}
	events, err := trajectory.ReadFile(filepath.Join(runDir.Path, runs.TrajectoryFile))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	requireRuntimeEventTypes(t, events,
		trajectory.EventContextSummaryFailed,
		trajectory.EventContextCompressionCompleted,
	)
	if !eventHasStrategy(events, "drop_evicted") {
		t.Fatalf("expected drop_evicted strategy after summary failure: %+v", events)
	}
}

func TestSummaryRequestTruncatesLargeMessagesBeforeModelCall(t *testing.T) {
	client := &summaryOnlyClient{}
	tmp := t.TempDir()
	store := runs.NewStore(filepath.Join(tmp, "runs"))
	runDir, err := store.CreateRun("context", "current task")
	if err != nil {
		t.Fatalf("Create run failed: %v", err)
	}
	recorder, err := trajectory.NewRecorder(runDir.Path)
	if err != nil {
		t.Fatalf("NewRecorder failed: %v", err)
	}
	agent := &compiler.CompiledAgent{Name: "context", RunStore: store}
	state := NewRunState(runDir.RunID, "context", "current task")
	_, err = New().summarizeContext(context.Background(), agent, recorder, state, client, model.ProviderConfig{
		Name:            "primary",
		Provider:        "mock",
		Model:           "mock",
		ContextWindow:   12000,
		MaxOutputTokens: 512,
	}, contextmgr.SummaryRequest{
		PreviousSummary: "previous",
		Messages: []model.Message{
			{Role: "user", Content: longText("huge pasted document ", 2000)},
		},
	})
	if err != nil {
		t.Fatalf("summarizeContext failed: %v", err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("expected one summary request, got %d", len(client.requests))
	}
	if got := client.requests[0].Messages[1].Content; !strings.Contains(got, "Jeju truncated summary input") {
		t.Fatalf("summary request did not truncate large message, length=%d", len(got))
	}
}

func TestPrepareModelRequestDegradesWhenSummaryInputExceedsBudget(t *testing.T) {
	tmp := t.TempDir()
	store := runs.NewStore(filepath.Join(tmp, "runs"))
	runDir, err := store.CreateRun("context", "current task")
	if err != nil {
		t.Fatalf("Create run failed: %v", err)
	}
	recorder, err := trajectory.NewRecorder(runDir.Path)
	if err != nil {
		t.Fatalf("NewRecorder failed: %v", err)
	}
	box, err := sandbox.NewLocal(filepath.Join(tmp, "workspace"))
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	agent := &compiler.CompiledAgent{
		Name:         "context",
		Instructions: "Keep context concise.",
		Config: config.AgentManifest{
			Metadata: config.Metadata{Name: "context"},
			Runtime: config.RuntimeConfig{
				CompressionThreshold: 0.8,
			},
		},
		Tools:    tools.NewRegistry(),
		Skills:   skills.NewRegistry(),
		Sandbox:  box,
		RunStore: store,
	}
	state := NewRunState(runDir.RunID, "context", "current task")
	for i := 0; i < 30; i++ {
		state.Messages = append(state.Messages,
			model.Message{Role: "user", Content: fmt.Sprintf("old request %d %s", i, longText("large pasted text ", 120))},
			model.Message{Role: "assistant", Content: fmt.Sprintf("old answer %d %s", i, longText("large answer text ", 120))},
		)
	}
	state.Messages = append(state.Messages,
		model.Message{Role: "user", Content: "recent request"},
		model.Message{Role: "assistant", Content: "recent answer"},
	)
	client := &summaryOnlyClient{}

	req, err := New().prepareModelRequest(context.Background(), agent, recorder, state, client, model.ProviderConfig{
		Name:            "primary",
		Provider:        "mock",
		Model:           "mock",
		ContextWindow:   2400,
		MaxOutputTokens: 512,
	})
	if err != nil {
		t.Fatalf("prepareModelRequest should degrade when summary input is too large: %v", err)
	}
	if client.calls != 0 {
		t.Fatalf("summary model should not be called after summary preflight overflow, got %d calls", client.calls)
	}
	if !containsText(req.Messages, "recent request") {
		t.Fatalf("degraded request should keep recent messages: %+v", req.Messages)
	}
	events, err := trajectory.ReadFile(filepath.Join(runDir.Path, runs.TrajectoryFile))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	requireRuntimeEventTypes(t, events,
		trajectory.EventContextSummaryStarted,
		trajectory.EventContextSummaryFailed,
		trajectory.EventContextCompressionCompleted,
	)
	if !eventHasStrategy(events, "drop_evicted") {
		t.Fatalf("expected drop_evicted strategy after summary preflight failure: %+v", events)
	}
}

func TestRuntimeContextOverflowFailsWithoutModelCall(t *testing.T) {
	tmp := t.TempDir()
	box, err := sandbox.NewLocal(filepath.Join(tmp, "workspace"))
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	client := &failIfCalledClient{}
	models := model.NewRegistry()
	models.Add("primary", model.ProviderConfig{
		Name:            "primary",
		Provider:        "mock",
		Model:           "mock",
		ContextWindow:   200,
		MaxOutputTokens: 180,
	}, client)
	agent := &compiler.CompiledAgent{
		Name:         "context-overflow",
		Instructions: "Keep context concise.",
		Config: config.AgentManifest{
			Metadata: config.Metadata{Name: "context-overflow"},
			Runtime: config.RuntimeConfig{
				Model:                "primary",
				CompressionThreshold: 0.8,
				Limits: config.RuntimeLimits{
					MaxSteps:             4,
					MaxDurationSec:       30,
					MaxConsecutiveErrors: 3,
				},
			},
			Permissions: config.PermissionsConfig{Access: "workspace", Approval: "never"},
		},
		ConfigSnapshot: []byte("apiVersion: jeju/v1alpha1\nkind: Agent\n"),
		Models:         models,
		Tools:          tools.NewRegistry(),
		Skills:         skills.NewRegistry(),
		Memory:         memory.Noop{},
		Sandbox:        box,
		Policy:         policy.NewGate(config.PermissionsConfig{Access: "workspace", Approval: "never"}),
		RunStore:       runs.NewStore(filepath.Join(tmp, "runs")),
	}

	result, err := New().Run(context.Background(), agent, longText("large input ", 100))
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if result.Status != StatusFailed {
		t.Fatalf("expected failed run, got %+v", result)
	}
	if client.calls != 0 {
		t.Fatalf("model should not be called after context overflow, got %d calls", client.calls)
	}
	events, err := trajectory.ReadFile(filepath.Join(tmp, "runs", result.RunID, runs.TrajectoryFile))
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !hasRuntimeEventType(events, trajectory.EventContextCompressionFailed) {
		t.Fatalf("expected context compression failure event, got %+v", events)
	}
	if hasRuntimeEventType(events, trajectory.EventContextCompressionCompleted) {
		t.Fatalf("did not expect completed compression event on overflow: %+v", events)
	}
	if hasRuntimeEventType(events, trajectory.EventModelStarted) {
		t.Fatalf("did not expect model.started after context overflow: %+v", events)
	}
}

func requireRuntimeEventTypes(t *testing.T, events []trajectory.Event, types ...trajectory.EventType) {
	t.Helper()
	seen := map[trajectory.EventType]bool{}
	for _, event := range events {
		seen[event.Type] = true
	}
	for _, typ := range types {
		if !seen[typ] {
			t.Fatalf("missing event type %q in %+v", typ, events)
		}
	}
}

func hasRuntimeEventType(events []trajectory.Event, typ trajectory.EventType) bool {
	for _, event := range events {
		if event.Type == typ {
			return true
		}
	}
	return false
}

func requireEventOrder(t *testing.T, events []trajectory.Event, order ...trajectory.EventType) {
	t.Helper()
	next := 0
	for _, event := range events {
		if next < len(order) && event.Type == order[next] {
			next++
		}
	}
	if next != len(order) {
		t.Fatalf("events did not contain order %+v: %+v", order, events)
	}
}

func eventHasStrategy(events []trajectory.Event, strategy string) bool {
	for _, event := range events {
		values, ok := event.Payload["strategies"].([]any)
		if !ok {
			continue
		}
		for _, value := range values {
			if value == strategy {
				return true
			}
		}
	}
	return false
}

func containsText(messages []model.Message, text string) bool {
	for _, message := range messages {
		if strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}

func longText(unit string, count int) string {
	out := ""
	for i := 0; i < count; i++ {
		out += unit
	}
	return out
}
