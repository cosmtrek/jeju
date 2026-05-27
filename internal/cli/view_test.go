package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jeju/internal/runs"
	"jeju/internal/trajectory"
)

func TestParseViewArgs(t *testing.T) {
	opts, err := parseViewArgs([]string{"run-1", "--out", "out.html"})
	if err != nil {
		t.Fatalf("parseViewArgs failed: %v", err)
	}
	if opts.runID != "run-1" || opts.out != "out.html" {
		t.Fatalf("unexpected opts: %#v", opts)
	}
}

func TestBuildRunReportAndWriteHTML(t *testing.T) {
	tmp := t.TempDir()
	store := runs.NewStore(filepath.Join(tmp, "runs"))
	runDir, err := store.CreateRun("agent", "write notes")
	if err != nil {
		t.Fatalf("CreateRun failed: %v", err)
	}
	endedAt := time.Now().Add(time.Second)
	if err := store.WriteMetadata(runDir.RunID, runs.Metadata{
		RunID:          runDir.RunID,
		Agent:          "agent",
		Status:         "completed",
		StartedAt:      time.Now(),
		EndedAt:        &endedAt,
		Input:          "write notes",
		ConfigSnapshot: runs.ConfigSnapshotFile,
		Trajectory:     runs.TrajectoryFile,
		Final:          runs.FinalFile,
		Evaluation:     runs.EvaluationFile,
	}); err != nil {
		t.Fatalf("WriteMetadata failed: %v", err)
	}
	if err := store.WriteConfigSnapshot(runDir.RunID, []byte("name: agent\n")); err != nil {
		t.Fatalf("WriteConfigSnapshot failed: %v", err)
	}
	if err := store.WriteFinal(runDir.RunID, "done"); err != nil {
		t.Fatalf("WriteFinal failed: %v", err)
	}
	if err := store.WriteEvaluation(runDir.RunID, []byte(`{"run_id":"x","passed":true,"score":1,"evaluators":[{"name":"rules","type":"rule","passed":true,"score":1}]}`)); err != nil {
		t.Fatalf("WriteEvaluation failed: %v", err)
	}
	if _, err := store.WriteArtifact(runDir.RunID, "step001_model_output.txt", []byte("output")); err != nil {
		t.Fatalf("WriteArtifact failed: %v", err)
	}
	if err := writeTrajectory(filepath.Join(runDir.Path, runs.TrajectoryFile), []trajectory.Event{
		{ID: "evt_000001", Type: trajectory.EventRunStarted, RunID: runDir.RunID, TS: time.Now(), Actor: "runtime", Payload: map[string]any{"agent": "agent"}},
		{ID: "evt_000002", Type: trajectory.EventModelCompleted, RunID: runDir.RunID, Step: 1, TS: time.Now(), Actor: "model:mock"},
	}); err != nil {
		t.Fatalf("writeTrajectory failed: %v", err)
	}

	report, err := buildRunReport(store, runDir)
	if err != nil {
		t.Fatalf("buildRunReport failed: %v", err)
	}
	if report.Summary.ModelCompleted != 1 || len(report.Artifacts) != 1 || !report.EvaluationExists {
		t.Fatalf("unexpected report: %#v", report)
	}

	out := filepath.Join(tmp, "report.html")
	if err := writeRunReportHTML(out, report); err != nil {
		t.Fatalf("writeRunReportHTML failed: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read report failed: %v", err)
	}
	html := string(data)
	for _, want := range []string{"Jeju Run", "write notes", "Final Output", "step001_model_output.txt"} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected html to contain %q", want)
		}
	}
}

func writeTrajectory(path string, events []trajectory.Event) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := file.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	return nil
}
