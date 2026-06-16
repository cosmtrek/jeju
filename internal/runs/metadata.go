package runs

import "time"

const (
	TrajectoryFile = "trajectory.jsonl"
	ReportFile     = "report.html"
)

type Metadata struct {
	RunID                string     `json:"run_id"`
	Agent                string     `json:"agent"`
	PackageID            string     `json:"package_id,omitempty"`
	PackageVersion       string     `json:"package_version,omitempty"`
	PackageDigest        string     `json:"package_digest,omitempty"`
	PackageSource        string     `json:"package_source,omitempty"`
	PackageStorePath     string     `json:"package_store_path,omitempty"`
	PackageAgentManifest string     `json:"package_agent_manifest,omitempty"`
	Status               string     `json:"status"`
	Integrity            string     `json:"trajectory_integrity,omitempty"`
	StartedAt            time.Time  `json:"started_at"`
	EndedAt              *time.Time `json:"ended_at,omitempty"`
	Input                string     `json:"input"`
}

type RunDir struct {
	RunID string
	Path  string
}
