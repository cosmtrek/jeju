package trajectory

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"
)

type Recorder struct {
	Sinks           []Sink
	FailOnSinkError bool
	mu              sync.Mutex
	counter         uint64
	trajectoryID    string
	sessionID       string
}

type RecorderOptions struct {
	Console bool
}

func NewRecorder(runDir string) (*Recorder, error) {
	return NewRecorderWithOptions(runDir, RecorderOptions{Console: true})
}

func NewRecorderWithOptions(runDir string, opts RecorderOptions) (*Recorder, error) {
	runID := filepath.Base(runDir)
	recorder := &Recorder{
		trajectoryID: "trj_" + runID,
		sessionID:    runID,
	}
	if opts.Console {
		recorder.Sinks = append(recorder.Sinks, NewConsoleSink())
	}
	fileSink, err := NewFileSink(filepath.Join(runDir, "trajectory.jsonl"))
	if err != nil {
		return nil, err
	}
	recorder.Sinks = append(recorder.Sinks, fileSink)
	return recorder, nil
}

func (r *Recorder) Emit(ctx context.Context, typ EventType, runID string, step int, actor string, payload map[string]any) Event {
	return r.emit(ctx, typ, runID, step, "", "", actor, payload)
}

func (r *Recorder) emit(ctx context.Context, typ EventType, runID string, step int, spanID, parentSpanID, actor string, payload map[string]any) Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counter++
	id := r.counter
	if payload == nil {
		payload = map[string]any{}
	}
	eventID := fmt.Sprintf("evt_%06d", id)
	event := Event{
		SchemaVersion: SchemaVersion,
		Seq:           id,
		EventID:       eventID,
		Type:          typ,
		RunID:         runID,
		StepID:        step,
		SpanID:        spanID,
		ParentSpanID:  parentSpanID,
		TS:            time.Now(),
		TrajectoryID:  r.trajectoryID,
		SessionID:     r.sessionID,
		Actor:         actor,
		Payload:       payload,
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

func (r *Recorder) EmitSpanStarted(ctx context.Context, runID string, step int, spanID, parentSpanID, actor string, kind SpanKind, name string, payload map[string]any) Event {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["kind"] = string(kind)
	if name != "" {
		payload["name"] = name
	}
	return r.emit(ctx, EventSpanStarted, runID, step, spanID, parentSpanID, actor, payload)
}

func (r *Recorder) EmitSpanEnded(ctx context.Context, runID string, step int, spanID, parentSpanID, actor string, kind SpanKind, status SpanStatus, payload map[string]any) Event {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["kind"] = string(kind)
	payload["status"] = string(status)
	return r.emit(ctx, EventSpanEnded, runID, step, spanID, parentSpanID, actor, payload)
}

func (r *Recorder) EmitWithSpan(ctx context.Context, typ EventType, runID string, step int, spanID, parentSpanID, actor string, payload map[string]any) Event {
	return r.emit(ctx, typ, runID, step, spanID, parentSpanID, actor, payload)
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
