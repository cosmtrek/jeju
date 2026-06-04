package trajectory

import (
	"context"
	"encoding/json"
	"os"
	"time"
)

type FileSink struct {
	file *os.File
}

func NewFileSink(path string) (*FileSink, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &FileSink{file: file}, nil
}

func (s *FileSink) Write(ctx context.Context, event Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := s.file.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (s *FileSink) Close() error {
	return s.file.Close()
}

func AppendEvent(path string, event Event) error {
	events, err := ReadFile(path)
	if err != nil {
		return err
	}
	next := uint64(len(events) + 1)
	if event.SchemaVersion == "" {
		event.SchemaVersion = SchemaVersion
	}
	event.Seq = next
	event.EventID = eventID(next)
	if event.TS.IsZero() {
		event.TS = time.Now()
	}
	if len(events) > 0 {
		last := events[len(events)-1]
		if event.TrajectoryID == "" {
			event.TrajectoryID = last.TrajectoryID
		}
		if event.SessionID == "" {
			event.SessionID = last.SessionID
		}
		if event.RunID == "" {
			event.RunID = last.RunID
		}
	}
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = file.Write(append(data, '\n'))
	return err
}

func eventID(seq uint64) string {
	return "evt_" + zeroPad(seq, 6)
}

func zeroPad(n uint64, width int) string {
	s := ""
	for {
		s = string(byte('0'+n%10)) + s
		n /= 10
		if n == 0 {
			break
		}
	}
	for len(s) < width {
		s = "0" + s
	}
	return s
}
