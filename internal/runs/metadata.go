package runs

import "time"

const (
	MetadataFile       = "metadata.json"
	ConfigSnapshotFile = "config.snapshot.yaml"
	TrajectoryFile     = "trajectory.jsonl"
	FinalFile          = "final.md"
	EvaluationFile     = "evaluation.json"
	ReportFile         = "report.html"
	ArtifactsDir       = "artifacts"
)

type Metadata struct {
	RunID          string     `json:"run_id"`
	Agent          string     `json:"agent"`
	Status         string     `json:"status"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	Input          string     `json:"input"`
	ConfigSnapshot string     `json:"config_snapshot"`
	Trajectory     string     `json:"trajectory"`
	Final          string     `json:"final"`
	Evaluation     string     `json:"evaluation,omitempty"`
}

type RunDir struct {
	RunID        string
	Path         string
	ArtifactsDir string
}
