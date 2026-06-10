package trajectory

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
)

func TestRecorderConcurrentEmitKeepsSequentialJSONL(t *testing.T) {
	runDir := t.TempDir()
	recorder, err := NewRecorderWithOptions(runDir, RecorderOptions{Console: false})
	if err != nil {
		t.Fatalf("NewRecorderWithOptions() error = %v", err)
	}

	const goroutines = 8
	const perGoroutine = 25
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				recorder.Emit(context.Background(), EventMessageCreated, "run", 0, "test", map[string]any{"role": "user"})
			}
		}()
	}
	wg.Wait()
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	events, err := ReadFile(filepath.Join(runDir, "trajectory.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	want := goroutines * perGoroutine
	if len(events) != want {
		t.Fatalf("events = %d, want %d", len(events), want)
	}
	for i, event := range events {
		wantSeq := uint64(i + 1)
		if event.Seq != wantSeq {
			t.Fatalf("event %d seq = %d, want %d", i, event.Seq, wantSeq)
		}
	}
}
