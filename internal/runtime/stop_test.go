package runtime

import (
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/config"
)

func TestShouldStopAllowsSinglePendingFinalSchemaRetry(t *testing.T) {
	state := NewRunState("run", "agent", "task")
	state.Step = 2
	state.ConsecutiveErrors = 1
	state.FinalValidationRetryPending = true
	limits := config.RuntimeLimits{MaxSteps: 2, MaxConsecutiveErrors: 1}

	if err := shouldStop(state, limits); err != nil {
		t.Fatalf("pending schema retry should bypass step/error limits once, got %v", err)
	}

	state.FinalValidationRetryPending = false
	err := shouldStop(state, limits)
	if err == nil || !strings.Contains(err.Error(), "max steps exceeded") {
		t.Fatalf("expected max steps error after pending retry clears, got %v", err)
	}
}

func TestShouldStopAllowsPendingFinalSchemaRetryAfterValidationError(t *testing.T) {
	state := NewRunState("run", "agent", "task")
	state.ConsecutiveErrors = 1
	state.FinalValidationRetryPending = true
	limits := config.RuntimeLimits{MaxConsecutiveErrors: 1}

	if err := shouldStop(state, limits); err != nil {
		t.Fatalf("pending schema retry should bypass consecutive error limit once, got %v", err)
	}

	state.FinalValidationRetryPending = false
	err := shouldStop(state, limits)
	if err == nil || !strings.Contains(err.Error(), "max consecutive errors exceeded") {
		t.Fatalf("expected consecutive errors after pending retry clears, got %v", err)
	}
}

func TestShouldStopAllowsPendingFinalSchemaRetryAfterToolBudget(t *testing.T) {
	state := NewRunState("run", "agent", "task")
	state.ToolCalls = 1
	state.ToolBudgetFinalTried = true
	state.FinalValidationRetryPending = true
	limits := config.RuntimeLimits{MaxToolCalls: 1}

	if err := shouldStop(state, limits); err != nil {
		t.Fatalf("pending schema retry should bypass exhausted tool budget once, got %v", err)
	}

	state.FinalValidationRetryPending = false
	err := shouldStop(state, limits)
	if err == nil || !strings.Contains(err.Error(), "max tool calls exceeded") {
		t.Fatalf("expected tool budget error after pending retry clears, got %v", err)
	}
}
