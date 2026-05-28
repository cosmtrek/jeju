package trajectory

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync/atomic"
	"time"

	"jeju/internal/runs"
)

type Recorder struct {
	Sinks           []Sink
	FailOnSinkError bool
	counter         uint64
}

func NewRecorder(runDir string) (*Recorder, error) {
	recorder := &Recorder{}
	recorder.Sinks = append(recorder.Sinks, NewConsoleSink())
	fileSink, err := NewFileSink(filepath.Join(runDir, runs.TrajectoryFile))
	if err != nil {
		return nil, err
	}
	recorder.Sinks = append(recorder.Sinks, fileSink)
	return recorder, nil
}

func (r *Recorder) Emit(ctx context.Context, typ EventType, runID string, step int, actor string, payload map[string]any) Event {
	id := atomic.AddUint64(&r.counter, 1)
	event := Event{
		ID:      fmt.Sprintf("evt_%06d", id),
		Type:    typ,
		RunID:   runID,
		Step:    step,
		TS:      time.Now(),
		Actor:   actor,
		Payload: payload,
	}
	for _, sink := range r.Sinks {
		if err := sink.Write(ctx, event); err != nil {
			if r.FailOnSinkError {
				panic(err)
			}
			log.Printf("trajectory sink error: %v", err)
		}
	}
	return event
}

func (r *Recorder) Close() error {
	var first error
	for _, sink := range r.Sinks {
		if err := sink.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
