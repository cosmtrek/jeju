package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/sandbox"
	"github.com/cosmtrek/jeju/internal/tools"
)

func TestFileReadSupportsOffsetLimit(t *testing.T) {
	box, err := sandbox.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	if err := box.WriteFile(context.Background(), "notes.txt", []byte("one\ntwo\nthree\nfour\n")); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	read := NewFileRead(tools.Spec{}, box)
	result, err := read.Run(context.Background(), json.RawMessage(`{"path":"notes.txt","offset":2,"limit":2}`))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var out struct {
		Content    string `json:"content"`
		Offset     int    `json:"offset"`
		Limit      int    `json:"limit"`
		TotalLines int    `json:"totalLines"`
		NextOffset int    `json:"nextOffset"`
		Truncated  bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Content != "two\nthree\n" {
		t.Fatalf("unexpected content %q", out.Content)
	}
	if out.Offset != 2 || out.Limit != 2 || out.TotalLines != 4 || out.NextOffset != 4 || !out.Truncated {
		t.Fatalf("unexpected metadata: %+v", out)
	}
}

func TestFileReadDefaultsToFirstTwoHundredLines(t *testing.T) {
	box, err := sandbox.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	var builder strings.Builder
	for i := 1; i <= 205; i++ {
		fmt.Fprintf(&builder, "line %03d\n", i)
	}
	if err := box.WriteFile(context.Background(), "notes.txt", []byte(builder.String())); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	read := NewFileRead(tools.Spec{}, box)
	result, err := read.Run(context.Background(), json.RawMessage(`{"path":"notes.txt"}`))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var out struct {
		Content    string `json:"content"`
		Offset     int    `json:"offset"`
		Limit      int    `json:"limit"`
		TotalLines int    `json:"totalLines"`
		NextOffset int    `json:"nextOffset"`
		Truncated  bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Offset != 1 || out.Limit != 200 || out.TotalLines != 205 || out.NextOffset != 201 || !out.Truncated {
		t.Fatalf("unexpected output: %+v", out)
	}
	if !strings.Contains(out.Content, "line 200\n") || strings.Contains(out.Content, "line 201\n") {
		t.Fatalf("unexpected content page tail: %q", out.Content[len(out.Content)-32:])
	}
}

func TestFileReadRejectsInvalidPage(t *testing.T) {
	box, err := sandbox.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	if err := box.WriteFile(context.Background(), "notes.txt", []byte("hello\n")); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	read := NewFileRead(tools.Spec{}, box)
	_, err = read.Run(context.Background(), json.RawMessage(`{"path":"notes.txt","offset":-1}`))
	if err == nil || !strings.Contains(err.Error(), "offset must be a positive 1-based line number") {
		t.Fatalf("expected offset error, got %v", err)
	}
	_, err = read.Run(context.Background(), json.RawMessage(`{"path":"notes.txt","offset":0}`))
	if err == nil || !strings.Contains(err.Error(), "offset must be a positive 1-based line number") {
		t.Fatalf("expected offset error, got %v", err)
	}
	_, err = read.Run(context.Background(), json.RawMessage(`{"path":"notes.txt","limit":-1}`))
	if err == nil || !strings.Contains(err.Error(), "limit must be a positive line count") {
		t.Fatalf("expected limit error, got %v", err)
	}
	_, err = read.Run(context.Background(), json.RawMessage(`{"path":"notes.txt","limit":0}`))
	if err == nil || !strings.Contains(err.Error(), "limit must be a positive line count") {
		t.Fatalf("expected limit error, got %v", err)
	}
}

func TestFileReadOffsetPastEndDoesNotAdvertiseNextPage(t *testing.T) {
	box, err := sandbox.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal failed: %v", err)
	}
	if err := box.WriteFile(context.Background(), "notes.txt", []byte("one\ntwo\n")); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	read := NewFileRead(tools.Spec{}, box)
	result, err := read.Run(context.Background(), json.RawMessage(`{"path":"notes.txt","offset":99}`))
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	var out struct {
		Content    string `json:"content"`
		TotalLines int    `json:"totalLines"`
		NextOffset int    `json:"nextOffset"`
		Truncated  bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(result.Output), &out); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	if out.Content != "" || out.TotalLines != 2 || out.NextOffset != 0 || out.Truncated {
		t.Fatalf("unexpected output: %+v", out)
	}
}
