package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/compiler"
	"github.com/cosmtrek/jeju/internal/model"
)

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
