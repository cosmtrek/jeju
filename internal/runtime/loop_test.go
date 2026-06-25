package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/compiler"
	"github.com/cosmtrek/jeju/internal/model"
)

func TestRunStateRecordUsageAccumulatesTokenStats(t *testing.T) {
	state := NewRunState("run", "agent", "task")

	state.RecordUsage(model.Usage{InputTokens: 10, CacheHitTokens: 4, OutputTokens: 3, TotalTokens: 13})
	state.RecordUsage(model.Usage{InputTokens: 7, CacheHitTokens: 2, OutputTokens: 5, TotalTokens: 12})

	if state.PromptTokens != 17 ||
		state.PromptCacheHitTokens != 6 ||
		state.CompletionTokens != 8 ||
		state.TotalTokens != 25 {
		t.Fatalf("unexpected token stats: prompt=%d cache=%d completion=%d total=%d",
			state.PromptTokens,
			state.PromptCacheHitTokens,
			state.CompletionTokens,
			state.TotalTokens,
		)
	}
}

func TestRunStatsPayloadIncludesTokenUsage(t *testing.T) {
	state := NewRunState("run", "agent", "task")
	state.Step = 2
	state.ModelCalls = 3
	state.ToolCalls = 1
	state.PermissionDenied = 1
	state.ModelErrors = 1
	state.ToolErrors = 1
	state.RecordUsage(model.Usage{InputTokens: 10, CacheHitTokens: 4, OutputTokens: 3, TotalTokens: 13})
	state.RecordUsage(model.Usage{InputTokens: 7, CacheHitTokens: 2, OutputTokens: 5, TotalTokens: 12})

	stats := runStatsPayload(state)
	for key, want := range map[string]int{
		"steps":                   2,
		"model_calls":             3,
		"tool_calls":              1,
		"permission_denied":       1,
		"model_errors":            1,
		"tool_errors":             1,
		"prompt_tokens":           17,
		"prompt_cache_hit_tokens": 6,
		"completion_tokens":       8,
		"total_tokens":            25,
	} {
		if got, ok := stats[key].(int); !ok || got != want {
			t.Fatalf("stats[%q] = %v (%T), want %d", key, stats[key], stats[key], want)
		}
	}
}

func TestRunStepClearsFinalSchemaRetryPendingWhenModelMissing(t *testing.T) {
	agent := &compiler.CompiledAgent{Models: model.NewRegistry()}
	state := NewRunState("run", "agent", "task")
	state.FinalValidationRetryPending = true

	err := New().runStep(context.Background(), agent, nil, state, "missing")
	if err == nil || !strings.Contains(err.Error(), `model "missing" is not compiled`) {
		t.Fatalf("runStep error = %v, want missing model error", err)
	}
	if state.FinalValidationRetryPending {
		t.Fatal("expected pending final schema retry flag to clear on missing model")
	}
}
