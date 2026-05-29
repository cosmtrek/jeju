package contextmgr

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"jeju/internal/model"
)

func TestPrepareTruncatesOldToolResultsBeforeSummary(t *testing.T) {
	req := model.Request{
		Model: "test",
		Messages: []model.Message{
			{Role: "system", Content: "runtime"},
			{Role: "user", Content: "task"},
			{Role: "tool", ToolCallID: "old", Content: strings.Repeat("large output ", 900)},
			{Role: "user", Content: "recent question"},
			{Role: "assistant", Content: "recent answer"},
		},
	}
	stateMessages := req.Messages[1:]

	result, err := Prepare(req, stateMessages, "", Options{
		ContextWindow:    1400,
		Threshold:        0.8,
		RecentBlocks:     2,
		ToolResultTokens: 80,
	})
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	if !result.Report.Compressed {
		t.Fatal("expected compression")
	}
	if result.Report.TruncatedToolResult != 1 {
		t.Fatalf("expected one truncated tool result, got %d", result.Report.TruncatedToolResult)
	}
	if result.Summary != "" {
		t.Fatalf("did not expect summary when tool truncation is enough: %q", result.Summary)
	}
	if !strings.Contains(result.StateMessages[1].Content, "Jeju truncated tool result") {
		t.Fatalf("tool result was not truncated: %q", result.StateMessages[1].Content)
	}
}

func TestPrepareBudgetsAgainstOutputReserve(t *testing.T) {
	stateMessages := []model.Message{
		{Role: "user", Content: strings.Repeat("input ", 320)},
	}
	req := model.Request{
		Model:    "test",
		Messages: append([]model.Message{{Role: "system", Content: "runtime"}}, stateMessages...),
	}

	result, err := Prepare(req, stateMessages, "", Options{
		ContextWindow:   1000,
		MaxOutputTokens: 600,
		Threshold:       0.8,
	})
	if err == nil {
		t.Fatalf("expected overflow against effective input limit, got report %+v", result.Report)
	}
	if !IsOverflow(err) {
		t.Fatalf("expected overflow error, got %T %v", err, err)
	}
	if result.Report.EffectiveInputLimit != 400 || result.Report.ThresholdTokens != 320 {
		t.Fatalf("output reserve was not applied to budget: %+v", result.Report)
	}
}

func TestPrepareEmergencyTruncatesRecentToolResult(t *testing.T) {
	stateMessages := []model.Message{
		{Role: "user", Content: "read the large file"},
		{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "call_recent", Name: "read", Arguments: json.RawMessage(`{"path":"large.txt"}`)}}},
		{Role: "tool", ToolCallID: "call_recent", Content: strings.Repeat("recent large output ", 500)},
	}
	req := model.Request{
		Model:    "test",
		Messages: append([]model.Message{{Role: "system", Content: "runtime"}}, stateMessages...),
	}

	result, err := Prepare(req, stateMessages, "", Options{
		ContextWindow:    900,
		Threshold:        0.8,
		RecentBlocks:     4,
		ToolResultTokens: 60,
	})
	if err != nil {
		t.Fatalf("Prepare failed: %v report=%+v", err, result.Report)
	}
	if !containsStrategy(result.Report.Strategies, "recent_tool_result_truncate") {
		t.Fatalf("expected recent tool truncation strategy, got %+v", result.Report.Strategies)
	}
	if !strings.Contains(result.StateMessages[2].Content, "Jeju truncated tool result") {
		t.Fatalf("recent tool result was not truncated: %q", result.StateMessages[2].Content)
	}
}

func TestPrepareSummarizesOlderBlocksAndPreservesNativeToolBlock(t *testing.T) {
	oldArgs := json.RawMessage(`{"query":"old"}`)
	recentArgs := json.RawMessage(`{"query":"recent"}`)
	stateMessages := []model.Message{
		{Role: "user", Content: strings.Repeat("old discussion ", 200)},
		{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "old_call", Name: "search", Arguments: oldArgs}}},
		{Role: "tool", ToolCallID: "old_call", Content: strings.Repeat("old search result ", 200)},
		{Role: "user", Content: "recent request"},
		{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "recent_call", Name: "search", Arguments: recentArgs}}},
		{Role: "tool", ToolCallID: "recent_call", Content: "recent result"},
	}
	req := model.Request{
		Model:    "test",
		Messages: append([]model.Message{{Role: "system", Content: "runtime"}}, stateMessages...),
	}

	result, err := Prepare(req, stateMessages, "previous facts", Options{
		ContextWindow:    360,
		Threshold:        0.8,
		RecentBlocks:     2,
		ToolResultTokens: 40,
	})
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}
	if result.PendingSummary == nil {
		t.Fatalf("expected pending summary: %+v", result.Report)
	}
	result = CompleteSummary(result, result.PendingSummary.PreviousSummary+"\nupdated old discussion summary", Options{
		ContextWindow:    360,
		Threshold:        0.8,
		RecentBlocks:     2,
		ToolResultTokens: 40,
	})
	if !result.Report.SummaryChanged {
		t.Fatalf("expected summary compression: %+v", result.Report)
	}
	if !strings.Contains(result.Summary, "previous facts") || !strings.Contains(result.Summary, "old discussion") {
		t.Fatalf("summary did not preserve previous and old context: %q", result.Summary)
	}
	if len(result.StateMessages) != 3 {
		t.Fatalf("expected recent user plus assistant/tool block, got %+v", result.StateMessages)
	}
	if got := result.StateMessages[1]; got.Role != "assistant" || len(got.ToolCalls) != 1 || got.ToolCalls[0].ID != "recent_call" {
		t.Fatalf("recent assistant tool call was not preserved: %+v", got)
	}
	if got := result.StateMessages[2]; got.Role != "tool" || got.ToolCallID != "recent_call" {
		t.Fatalf("recent tool result was not preserved: %+v", got)
	}
}

func TestPreparePreservesRecentNativeToolBlocksWithoutUserBoundary(t *testing.T) {
	stateMessages := []model.Message{
		{Role: "user", Content: strings.Repeat("initial task ", 120)},
	}
	for i := 0; i < 5; i++ {
		callID := fmt.Sprintf("call_%d", i)
		stateMessages = append(stateMessages,
			model.Message{
				Role: "assistant",
				ToolCalls: []model.ToolCall{{
					ID:        callID,
					Name:      "search",
					Arguments: json.RawMessage(`{"query":"native"}`),
				}},
			},
			model.Message{Role: "tool", ToolCallID: callID, Content: strings.Repeat("native tool result ", 80)},
		)
	}
	req := model.Request{
		Model:    "test",
		Messages: append([]model.Message{{Role: "system", Content: "runtime"}}, stateMessages...),
	}

	result, err := Prepare(req, stateMessages, "previous facts", Options{
		ContextWindow:    700,
		Threshold:        0.8,
		RecentBlocks:     2,
		ToolResultTokens: 40,
	})
	if err != nil {
		t.Fatalf("Prepare failed: %v report=%+v", err, result.Report)
	}
	if result.PendingSummary == nil {
		t.Fatalf("expected pending summary: %+v", result.Report)
	}
	result = CompleteSummary(result, "updated native summary", Options{
		ContextWindow:    700,
		Threshold:        0.8,
		RecentBlocks:     2,
		ToolResultTokens: 40,
	})
	if len(result.StateMessages) != 4 {
		t.Fatalf("expected two recent native tool blocks, got %+v", result.StateMessages)
	}
	if result.StateMessages[0].Role != "assistant" || len(result.StateMessages[0].ToolCalls) != 1 {
		t.Fatalf("expected recent history to keep assistant tool call first, got %+v", result.StateMessages)
	}
	if result.StateMessages[1].Role != "tool" || result.StateMessages[1].ToolCallID != result.StateMessages[0].ToolCalls[0].ID {
		t.Fatalf("expected first recent tool result to match assistant call, got %+v", result.StateMessages[:2])
	}
	if result.StateMessages[2].Role != "assistant" || len(result.StateMessages[2].ToolCalls) != 1 {
		t.Fatalf("expected second recent assistant tool call, got %+v", result.StateMessages)
	}
	if result.StateMessages[3].Role != "tool" || result.StateMessages[3].ToolCallID != result.StateMessages[2].ToolCalls[0].ID {
		t.Fatalf("expected second recent tool result to match assistant call, got %+v", result.StateMessages[2:])
	}
	if !strings.HasPrefix(result.Request.Messages[len(result.Request.Messages)-len(result.StateMessages)-1].Content, "# Conversation Summary") {
		t.Fatalf("expected summary user message before assistant-led recent history: %+v", result.Request.Messages)
	}
}

func TestPrepareKeepsCompressedHistoryStartingWithUser(t *testing.T) {
	stateMessages := []model.Message{
		{Role: "user", Content: strings.Repeat("old discussion ", 120)},
		{Role: "assistant", Content: strings.Repeat("old answer ", 120)},
		{Role: "assistant", ToolCalls: []model.ToolCall{{ID: "call_mid", Name: "search", Arguments: json.RawMessage(`{"query":"mid"}`)}}},
		{Role: "tool", ToolCallID: "call_mid", Content: strings.Repeat("mid result ", 120)},
		{Role: "user", Content: "recent request"},
		{Role: "assistant", Content: "recent answer"},
	}
	req := model.Request{
		Model:    "test",
		Messages: append([]model.Message{{Role: "system", Content: "runtime"}}, stateMessages...),
	}

	result, err := Prepare(req, stateMessages, "", Options{
		ContextWindow:    360,
		Threshold:        0.8,
		RecentBlocks:     3,
		ToolResultTokens: 40,
	})
	if err != nil {
		t.Fatalf("Prepare failed: %v report=%+v", err, result.Report)
	}
	if result.PendingSummary != nil {
		result = CompleteSummary(result, "updated summary", Options{
			ContextWindow:    360,
			Threshold:        0.8,
			RecentBlocks:     3,
			ToolResultTokens: 40,
		})
	}
	if len(result.StateMessages) == 0 || result.StateMessages[0].Role != "user" {
		t.Fatalf("compressed history should start with a user message: %+v", result.StateMessages)
	}
	for _, message := range result.Request.Messages {
		if strings.HasPrefix(message.Content, "# Conversation Summary") && message.Role != "user" {
			t.Fatalf("summary should be represented as a user message, got %+v", message)
		}
	}
}

func TestPrepareSummarizerReceivesPreviousSummaryAndEvictedMessages(t *testing.T) {
	stateMessages := []model.Message{
		{Role: "user", Content: strings.Repeat("old user requirement ", 80)},
		{Role: "assistant", Content: strings.Repeat("old assistant decision ", 80)},
		{Role: "user", Content: strings.Repeat("newer request ", 40)},
		{Role: "assistant", Content: strings.Repeat("newer answer ", 40)},
		{Role: "user", Content: "recent request"},
		{Role: "assistant", Content: "recent answer"},
	}
	req := model.Request{
		Model:    "test",
		Messages: append([]model.Message{{Role: "system", Content: "runtime"}}, stateMessages...),
	}
	var got SummaryRequest
	result, err := Prepare(req, stateMessages, "previous summary", Options{
		ContextWindow:    180,
		Threshold:        0.8,
		RecentBlocks:     2,
		ToolResultTokens: 40,
	})
	if err != nil {
		t.Fatalf("Prepare failed: %v report=%+v", err, result.Report)
	}
	if result.PendingSummary == nil {
		t.Fatalf("expected pending summary: %+v", result.Report)
	}
	got = *result.PendingSummary
	result = CompleteSummary(result, got.PreviousSummary+"\nmodel-updated summary", Options{
		ContextWindow:    180,
		Threshold:        0.8,
		RecentBlocks:     2,
		ToolResultTokens: 40,
	})
	if got.PreviousSummary != "previous summary" {
		t.Fatalf("summarizer did not receive prior summary: %+v", got)
	}
	if len(got.Messages) == 0 {
		t.Fatal("expected evicted messages")
	}
	for _, message := range got.Messages {
		if strings.HasPrefix(message.Content, "# Conversation Summary") {
			t.Fatalf("previous summary should be passed separately, not as an evicted message: %+v", got.Messages)
		}
	}
	if !strings.Contains(result.Summary, "model-updated summary") {
		t.Fatalf("result summary did not use model output: %q", result.Summary)
	}
}

func TestUpdateCorrectionFactorUsesActualUsage(t *testing.T) {
	got := UpdateCorrectionFactor(1, 100, 150)
	if got <= 1 {
		t.Fatalf("expected correction factor to increase, got %v", got)
	}
	if unchanged := UpdateCorrectionFactor(got, 0, 150); unchanged != got {
		t.Fatalf("expected missing estimate to keep factor, got %v want %v", unchanged, got)
	}
}

func containsStrategy(strategies []string, want string) bool {
	for _, strategy := range strategies {
		if strategy == want {
			return true
		}
	}
	return false
}
