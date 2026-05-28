package builtin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"jeju/internal/sandbox"
	"jeju/internal/tools"
)

func TestSearchRejectsEmptyQuery(t *testing.T) {
	box, err := sandbox.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	if err := box.WriteFile(context.Background(), "notes.txt", []byte("hello\n")); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	search := NewSearch(tools.Spec{}, box)
	_, err = search.Run(context.Background(), json.RawMessage(`{"query":" "}`))
	if err == nil || !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("expected empty query error, got %v", err)
	}
}
