package cli

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/cosmtrek/jeju/internal/runs"
	"github.com/cosmtrek/jeju/internal/trajectory"
)

func runInspect(runID, runsDir string) error {
	store := runs.NewStore(resolveRunsDir(runsDir))
	runDir, err := store.LoadRun(runID)
	if err != nil {
		return err
	}
	events, err := trajectory.ReadFile(filepath.Join(runDir.Path, runs.TrajectoryFile))
	if err != nil {
		return err
	}
	record := trajectory.Project(events)

	summary := summarizeInspect(events)
	duration := time.Duration(0)
	if record.EndedAt != nil {
		duration = record.EndedAt.Sub(record.StartedAt)
	}

	fmt.Printf("Run %s\n", record.RunID)
	fmt.Printf("  agent: %s\n", record.Agent)
	fmt.Printf("  status: %s\n", record.Status)
	fmt.Printf("  integrity: %s\n", record.Integrity)
	fmt.Printf("  started: %s\n", record.StartedAt.Format("2006-01-02 15:04:05"))
	if duration > 0 {
		fmt.Printf("  duration: %s\n", duration.Round(time.Millisecond))
	}
	fmt.Printf("  task: %s\n", record.Input)

	fmt.Println("\nSummary")
	fmt.Printf("  steps: %d\n", summary.Steps)
	fmt.Printf("  model_calls: %d completed=%d failed=%d\n", summary.ModelStarted, summary.ModelCompleted, summary.ModelFailed)
	fmt.Printf("  tool_calls: %d completed=%d failed=%d\n", summary.ToolStarted, summary.ToolCompleted, summary.ToolFailed)
	fmt.Printf("  permissions: checked=%d approved=%d denied=%d\n", summary.PermissionChecked, summary.PermissionApproved, summary.PermissionDenied)
	fmt.Printf("  skills: disclosed=%d loaded=%d\n", summary.SkillDisclosed, summary.SkillLoaded)
	fmt.Printf("  artifacts: %d\n", summary.Artifacts)
	if len(record.IntegrityIssues) > 0 {
		fmt.Println("\nTrajectory Issues")
		for _, issue := range record.IntegrityIssues {
			fmt.Printf("  - %s\n", issue)
		}
	}

	if evalSummary := evaluationSummaryFromRecord(record); evalSummary.Exists {
		fmt.Println("\nEvaluation")
		fmt.Printf("  passed: %v\n", evalSummary.Passed)
		fmt.Printf("  score: %.3g\n", evalSummary.Score)
		fmt.Printf("  evaluators: %d\n", evalSummary.Evaluators)
	}

	fmt.Println("\nFiles")
	fmt.Printf("  trajectory: %s\n", filepath.Join(runDir.Path, runs.TrajectoryFile))
	fmt.Printf("  report: %s\n", filepath.Join(runDir.Path, runs.ReportFile))
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
		case trajectory.EventSpanStarted:
			switch stringPayload(event.Payload, "kind") {
			case string(trajectory.SpanStep):
				summary.Steps++
			case string(trajectory.SpanLLM):
				summary.ModelStarted++
			case string(trajectory.SpanTool):
				summary.ToolStarted++
			}
		case trajectory.EventSpanEnded:
			switch stringPayload(event.Payload, "kind") {
			case string(trajectory.SpanLLM):
				if stringPayload(event.Payload, "status") == string(trajectory.SpanStatusError) {
					summary.ModelFailed++
				} else {
					summary.ModelCompleted++
				}
			case string(trajectory.SpanTool):
				if stringPayload(event.Payload, "status") == string(trajectory.SpanStatusError) {
					summary.ToolFailed++
				} else {
					summary.ToolCompleted++
				}
			case string(trajectory.SpanSkill):
				if output, ok := event.Payload["output"].(map[string]any); ok && stringPayload(output, "name") != "" {
					summary.SkillLoaded++
				} else {
					summary.SkillDisclosed++
				}
			}
		case trajectory.EventPermissionDecided:
			summary.PermissionChecked++
			if stringPayload(event.Payload, "decision") == "denied" {
				summary.PermissionDenied++
			} else {
				summary.PermissionApproved++
			}
		case trajectory.EventArtifactCreated:
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

func evaluationSummaryFromRecord(record trajectory.RunRecord) evaluationSummary {
	result, exists := evaluationFromRecord(record)
	if !exists || result == nil {
		return evaluationSummary{}
	}
	return evaluationSummary{
		Exists:     true,
		Passed:     result.Passed,
		Score:      result.Score,
		Evaluators: len(result.Evaluators),
	}
}
