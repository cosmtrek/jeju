package team

import (
	"html/template"
	"os"
	"path/filepath"
	"sort"
)

func writeReport(path string, summary Summary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	tasks := make([]TaskState, 0, len(summary.Tasks))
	for _, task := range summary.Tasks {
		tasks = append(tasks, task)
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].RoundCreated == tasks[j].RoundCreated {
			return tasks[i].ID < tasks[j].ID
		}
		return tasks[i].RoundCreated < tasks[j].RoundCreated
	})
	data := struct {
		Summary Summary
		Tasks   []TaskState
	}{
		Summary: summary,
		Tasks:   tasks,
	}
	return teamReportTemplate.Execute(file, data)
}

var teamReportTemplate = template.Must(template.New("team-report").Parse(`<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>Jeju Team Report - {{.Summary.Team}}</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 32px; color: #202124; }
    code, pre { background: #f6f8fa; border-radius: 4px; padding: 2px 4px; }
    table { border-collapse: collapse; width: 100%; margin-top: 16px; }
    th, td { border: 1px solid #d0d7de; padding: 8px; text-align: left; vertical-align: top; }
    th { background: #f6f8fa; }
    .muted { color: #59636e; }
    .final { white-space: pre-wrap; border: 1px solid #d0d7de; padding: 12px; border-radius: 6px; background: #fbfbfb; }
  </style>
</head>
<body>
  <h1>Jeju Team Report</h1>
  <p><strong>Team:</strong> {{.Summary.Team}}</p>
  <p><strong>Run:</strong> <code>{{.Summary.TeamRunID}}</code></p>
  <p><strong>Status:</strong> {{.Summary.Status}}</p>
  <p><strong>Goal:</strong> {{.Summary.Goal}}</p>
  <p class="muted">Rounds: {{.Summary.RoundCount}} / {{.Summary.MaxRounds}} | Child runs: {{.Summary.Stats.ChildRuns}} | Model calls: {{.Summary.Stats.ModelCalls}} | Tool calls: {{.Summary.Stats.ToolCalls}} | Tokens: {{.Summary.Stats.TotalTokens}}</p>

  <h2>Tasks</h2>
  <table>
    <thead>
      <tr><th>ID</th><th>Worker</th><th>Status</th><th>Round</th><th>Run ID</th><th>Verification</th><th>Objective</th></tr>
    </thead>
    <tbody>
    {{range .Tasks}}
      <tr>
        <td><code>{{.ID}}</code></td>
        <td>{{.Worker}}</td>
        <td>{{.Status}}</td>
        <td>{{.RoundCreated}}</td>
        <td><code>{{.RunID}}</code></td>
        <td>{{if .Verification.Passed}}passed{{else}}{{range .Verification.Reasons}}{{.}} {{end}}{{end}}</td>
        <td>{{.Objective}}</td>
      </tr>
    {{end}}
    </tbody>
  </table>

  <h2>Final</h2>
  <div class="final">{{.Summary.Final}}</div>
</body>
</html>
`))
