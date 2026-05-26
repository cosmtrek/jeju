package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"jeju/internal/evaluate"
	"jeju/internal/runs"
	"jeju/internal/trajectory"
)

func runInspect(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: jeju inspect <run_id>")
	}
	store := runs.NewStore(filepath.Clean("./runs"))
	runDir, err := store.LoadRun(args[0])
	if err != nil {
		return err
	}
	meta, err := store.ReadMetadata(args[0])
	if err != nil {
		return err
	}
	events, err := trajectory.ReadFile(filepath.Join(runDir.Path, "trajectory.jsonl"))
	if err != nil {
		return err
	}

	summary := summarizeInspect(events)
	evalSummary := readEvaluationSummary(filepath.Join(runDir.Path, meta.Evaluation))
	duration := time.Duration(0)
	if meta.EndedAt != nil {
		duration = meta.EndedAt.Sub(meta.StartedAt)
	}

	fmt.Printf("Run %s\n", meta.RunID)
	fmt.Printf("  agent: %s\n", meta.Agent)
	fmt.Printf("  status: %s\n", meta.Status)
	fmt.Printf("  started: %s\n", meta.StartedAt.Format("2006-01-02 15:04:05"))
	if duration > 0 {
		fmt.Printf("  duration: %s\n", duration.Round(time.Millisecond))
	}
	fmt.Printf("  task: %s\n", meta.Input)

	fmt.Println("\nSummary")
	fmt.Printf("  steps: %d\n", summary.Steps)
	fmt.Printf("  model_calls: %d completed=%d failed=%d\n", summary.ModelStarted, summary.ModelCompleted, summary.ModelFailed)
	fmt.Printf("  tool_calls: %d completed=%d failed=%d\n", summary.ToolStarted, summary.ToolCompleted, summary.ToolFailed)
	fmt.Printf("  permissions: checked=%d approved=%d denied=%d\n", summary.PermissionChecked, summary.PermissionApproved, summary.PermissionDenied)
	fmt.Printf("  skills: disclosed=%d loaded=%d\n", summary.SkillDisclosed, summary.SkillLoaded)
	fmt.Printf("  artifacts: %d\n", summary.Artifacts)

	if meta.Evaluation != "" {
		fmt.Println("\nEvaluation")
		if evalSummary.Exists {
			fmt.Printf("  passed: %v\n", evalSummary.Passed)
			fmt.Printf("  score: %.3g\n", evalSummary.Score)
			fmt.Printf("  evaluators: %d\n", evalSummary.Evaluators)
		} else {
			fmt.Printf("  status: missing\n")
		}
		fmt.Printf("  file: %s\n", filepath.Join(runDir.Path, meta.Evaluation))
	}

	fmt.Println("\nFiles")
	fmt.Printf("  final: %s\n", filepath.Join(runDir.Path, meta.Final))
	fmt.Printf("  trajectory: %s\n", filepath.Join(runDir.Path, meta.Trajectory))
	fmt.Printf("  metadata: %s\n", filepath.Join(runDir.Path, "metadata.json"))
	fmt.Printf("  config_snapshot: %s\n", filepath.Join(runDir.Path, meta.ConfigSnapshot))
	fmt.Printf("  artifacts: %s\n", filepath.Join(runDir.Path, "artifacts"))
	return nil
}

type inspectSummary struct {
	Steps              int
	ModelStarted       int
	ModelCompleted     int
	ModelFailed        int
	ToolStarted        int
	ToolCompleted      int
	ToolFailed         int
	PermissionChecked  int
	PermissionApproved int
	PermissionDenied   int
	SkillDisclosed     int
	SkillLoaded        int
	Artifacts          int
}

func summarizeInspect(events []trajectory.Event) inspectSummary {
	var summary inspectSummary
	for _, event := range events {
		switch event.Type {
		case "step.started":
			summary.Steps++
		case "model.started":
			summary.ModelStarted++
		case "model.completed":
			summary.ModelCompleted++
		case "model.failed":
			summary.ModelFailed++
		case "tool.started":
			summary.ToolStarted++
		case "tool.completed":
			summary.ToolCompleted++
		case "tool.failed":
			summary.ToolFailed++
		case "permission.checked":
			summary.PermissionChecked++
		case "permission.approved":
			summary.PermissionApproved++
		case "permission.denied":
			summary.PermissionDenied++
		case "skill.disclosed":
			summary.SkillDisclosed++
		case "skill.loaded":
			summary.SkillLoaded++
		case "artifact.created":
			summary.Artifacts++
		}
	}
	return summary
}

type evaluationSummary struct {
	Exists     bool
	Passed     bool
	Score      float64
	Evaluators int
}

func readEvaluationSummary(path string) evaluationSummary {
	if path == "" {
		return evaluationSummary{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return evaluationSummary{}
	}
	var result evaluate.Result
	if err := json.Unmarshal(data, &result); err != nil {
		return evaluationSummary{}
	}
	return evaluationSummary{
		Exists:     true,
		Passed:     result.Passed,
		Score:      result.Score,
		Evaluators: len(result.Evaluators),
	}
}
