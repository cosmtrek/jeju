package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/sandbox"
	"github.com/cosmtrek/jeju/internal/tools"
)

func TestEditRejectsAmbiguousOldText(t *testing.T) {
	box, err := sandbox.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	if err := box.WriteFile(context.Background(), "notes.txt", []byte("same\nsame\n")); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	edit := NewEdit(tools.Spec{}, box)
	_, err = edit.Run(context.Background(), json.RawMessage(`{"path":"notes.txt","oldText":"same","newText":"next"}`))
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous oldText error, got %v", err)
	}
}
