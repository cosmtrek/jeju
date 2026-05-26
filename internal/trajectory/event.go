package trajectory

import "time"

type Event struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	RunID   string         `json:"run_id"`
	Step    int            `json:"step,omitempty"`
	TS      time.Time      `json:"ts"`
	Actor   string         `json:"actor"`
	Payload map[string]any `json:"payload,omitempty"`
}
