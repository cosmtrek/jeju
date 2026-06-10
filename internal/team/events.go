package team

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

type eventWriter struct {
	mu   sync.Mutex
	file *os.File
	enc  *json.Encoder
	seq  int
}

func newEventWriter(path string) (*eventWriter, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &eventWriter{file: file, enc: json.NewEncoder(file)}, nil
}

func (w *eventWriter) Close() error {
	if w == nil || w.file == nil {
		return nil
	}
	return w.file.Close()
}

func (w *eventWriter) Write(eventType string, payload map[string]any) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seq++
	_ = w.enc.Encode(map[string]any{
		"seq":     w.seq,
		"type":    eventType,
		"ts":      time.Now().Format(time.RFC3339Nano),
		"payload": payload,
	})
}
