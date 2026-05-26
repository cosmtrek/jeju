package sandbox

import (
	"context"
	"testing"
)

func TestLocalSandboxRestrictsFilePaths(t *testing.T) {
	box, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal returned error: %v", err)
	}
	if err := box.WriteFile(context.Background(), "notes/out.txt", []byte("ok")); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	data, err := box.ReadFile(context.Background(), "notes/out.txt")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(data) != "ok" {
		t.Fatalf("unexpected file content %q", string(data))
	}
	if err := box.WriteFile(context.Background(), "../escape.txt", []byte("bad")); err == nil {
		t.Fatal("expected path traversal write to fail")
	}
	if _, err := box.ReadFile(context.Background(), "/tmp/escape.txt"); err == nil {
		t.Fatal("expected absolute read to fail")
	}
}
