package runs

import "time"

const (
	TrajectoryFile = "trajectory.jsonl"
	ReportFile     = "report.html"
)

type Metadata struct {
	RunID     string     `json:"run_id"`
	Agent     string     `json:"agent"`
	Status    string     `json:"status"`
	Integrity string     `json:"trajectory_integrity,omitempty"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Input     string     `json:"input"`
}

type RunDir struct {
	RunID string
	Path  string
}
