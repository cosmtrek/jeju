package evaluate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCommandEvaluatorRunsExecutableWithArgsAndDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fixture.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile fixture failed: %v", err)
	}
	script := filepath.Join(dir, "judge.sh")
	if err := os.WriteFile(script, []byte(`#!/bin/sh
test "$(cat fixture.txt)" = "ok" || exit 3
test "$1" = "expected" || exit 4
cat >/dev/null
printf '{"score":1,"passed":true,"reason":"ok"}'
`), 0o755); err != nil {
		t.Fatalf("WriteFile script failed: %v", err)
	}

	evaluator := NewCommandEvaluator("judge", script, []string{"expected"}, 5)
	result, err := evaluator.Evaluate(context.Background(), Context{RunID: "run", Status: "completed"})
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if !result.Passed || result.Score != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
