package cli

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"jeju/internal/evaluate"
	"jeju/internal/runs"
	"jeju/internal/trajectory"
)

func runView(args []string) error {
	opts, err := parseViewArgs(args)
	if err != nil {
		return err
	}

	store := runs.NewStore(filepath.Clean("./runs"))
	runDir, err := store.LoadRun(opts.runID)
	if err != nil {
		return err
	}
	report, err := buildRunReport(store, runDir)
	if err != nil {
		return err
	}

	out := opts.out
	if out == "" {
		out = filepath.Join(runDir.Path, "report.html")
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if err := writeRunReportHTML(out, report); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", out)
	return nil
}

type viewOptions struct {
	runID string
	out   string
}

func parseViewArgs(args []string) (viewOptions, error) {
	var opts viewOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help":
			return opts, fmt.Errorf("usage: jeju view <run_id> [--out <html>]")
		case "--out":
			if i+1 >= len(args) {
				return opts, fmt.Errorf("--out requires a path")
			}
			i++
			opts.out = args[i]
		default:
			if strings.HasPrefix(arg, "-") {
				return opts, fmt.Errorf("unknown view flag %q", arg)
			}
			if opts.runID != "" {
				return opts, fmt.Errorf("usage: jeju view <run_id> [--out <html>]")
			}
			opts.runID = arg
		}
	}
	if opts.runID == "" {
		return opts, fmt.Errorf("usage: jeju view <run_id> [--out <html>]")
	}
	return opts, nil
}

type runReport struct {
	GeneratedAt      time.Time
	RunDir           string
	Metadata         runs.Metadata
	Duration         string
	Summary          inspectSummary
	Final            string
	ConfigSnapshot   string
	Evaluation       *evaluate.Result
	EvaluationExists bool
	Artifacts        []artifactView
	Events           []eventView
	MetadataJSON     string
}

type artifactView struct {
	Path string
	Size int64
}

type eventView struct {
	ID          string
	Type        string
	Actor       string
	Step        int
	Timestamp   string
	PayloadJSON string
}

func buildRunReport(store *runs.Store, runDir *runs.RunDir) (runReport, error) {
	meta, err := store.ReadMetadata(runDir.RunID)
	if err != nil {
		return runReport{}, err
	}
	events, err := trajectory.ReadFile(filepath.Join(runDir.Path, meta.Trajectory))
	if err != nil {
		return runReport{}, err
	}
	final, err := readOptionalText(filepath.Join(runDir.Path, meta.Final))
	if err != nil {
		return runReport{}, err
	}
	configSnapshot, err := readOptionalText(filepath.Join(runDir.Path, meta.ConfigSnapshot))
	if err != nil {
		return runReport{}, err
	}
	evaluationPath := ""
	if meta.Evaluation != "" {
		evaluationPath = filepath.Join(runDir.Path, meta.Evaluation)
	}
	evaluation, evaluationExists, err := readEvaluation(evaluationPath)
	if err != nil {
		return runReport{}, err
	}
	artifacts, err := listArtifacts(filepath.Join(runDir.Path, "artifacts"))
	if err != nil {
		return runReport{}, err
	}
	metadataJSON, err := marshalIndented(meta)
	if err != nil {
		return runReport{}, err
	}

	duration := ""
	if meta.EndedAt != nil {
		duration = meta.EndedAt.Sub(meta.StartedAt).Round(time.Millisecond).String()
	}
	return runReport{
		GeneratedAt:      time.Now(),
		RunDir:           runDir.Path,
		Metadata:         meta,
		Duration:         duration,
		Summary:          summarizeInspect(events),
		Final:            final,
		ConfigSnapshot:   configSnapshot,
		Evaluation:       evaluation,
		EvaluationExists: evaluationExists,
		Artifacts:        artifacts,
		Events:           mapEventViews(events),
		MetadataJSON:     metadataJSON,
	}, nil
}

func readOptionalText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func readEvaluation(path string) (*evaluate.Result, bool, error) {
	if path == "" {
		return nil, false, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var result evaluate.Result
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, false, err
	}
	return &result, true, nil
}

func listArtifacts(root string) ([]artifactView, error) {
	var artifacts []artifactView
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filepath.Dir(root), path)
		if err != nil {
			return err
		}
		artifacts = append(artifacts, artifactView{
			Path: filepath.ToSlash(rel),
			Size: info.Size(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(artifacts, func(i, j int) bool {
		return artifacts[i].Path < artifacts[j].Path
	})
	return artifacts, nil
}

func mapEventViews(events []trajectory.Event) []eventView {
	out := make([]eventView, 0, len(events))
	for _, event := range events {
		payload := ""
		if len(event.Payload) > 0 {
			payload, _ = marshalIndented(event.Payload)
		}
		out = append(out, eventView{
			ID:          event.ID,
			Type:        event.Type,
			Actor:       event.Actor,
			Step:        event.Step,
			Timestamp:   event.TS.Format("2006-01-02 15:04:05.000"),
			PayloadJSON: payload,
		})
	}
	return out
}

func marshalIndented(value any) (string, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func writeRunReportHTML(path string, report runReport) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return runReportTemplate.Execute(file, report)
}

var runReportTemplate = template.Must(template.New("run-report").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jeju Run {{.Metadata.RunID}}</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f7f9;
      --panel: #ffffff;
      --ink: #1d2430;
      --muted: #667085;
      --line: #d8dee8;
      --accent: #2563eb;
      --ok: #15803d;
      --bad: #b42318;
      --code: #101828;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--ink);
      font: 14px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    header {
      border-bottom: 1px solid var(--line);
      background: var(--panel);
      padding: 24px 32px;
    }
    main {
      max-width: 1180px;
      margin: 0 auto;
      padding: 24px;
    }
    h1, h2, h3 { margin: 0; line-height: 1.2; }
    h1 { font-size: 24px; }
    h2 { font-size: 18px; margin-bottom: 14px; }
    h3 { font-size: 14px; margin-bottom: 8px; }
    .subtle { color: var(--muted); }
    .grid {
      display: grid;
      gap: 16px;
    }
    .grid.two { grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); }
    .grid.four { grid-template-columns: repeat(4, minmax(0, 1fr)); }
    .panel {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 18px;
      margin-bottom: 16px;
    }
    .metric {
      background: #fbfcfe;
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 14px;
      min-width: 0;
    }
    .metric .label {
      color: var(--muted);
      font-size: 12px;
      text-transform: uppercase;
    }
    .metric .value {
      font-size: 22px;
      font-weight: 650;
      margin-top: 4px;
      overflow-wrap: anywhere;
    }
    dl {
      display: grid;
      grid-template-columns: 150px minmax(0, 1fr);
      gap: 8px 14px;
      margin: 0;
    }
    dt { color: var(--muted); }
    dd { margin: 0; overflow-wrap: anywhere; }
    pre {
      margin: 0;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
      background: var(--code);
      color: #f8fafc;
      border-radius: 8px;
      padding: 14px;
      overflow-x: auto;
      font: 12px/1.45 ui-monospace, SFMono-Regular, Menlo, monospace;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      table-layout: fixed;
    }
    th, td {
      border-bottom: 1px solid var(--line);
      padding: 8px 10px;
      text-align: left;
      vertical-align: top;
      overflow-wrap: anywhere;
    }
    th {
      color: var(--muted);
      font-size: 12px;
      font-weight: 600;
      text-transform: uppercase;
    }
    .badge {
      display: inline-block;
      border: 1px solid var(--line);
      border-radius: 999px;
      padding: 2px 8px;
      background: #fff;
      font-size: 12px;
    }
    .badge.ok { color: var(--ok); border-color: #bbf7d0; background: #f0fdf4; }
    .badge.bad { color: var(--bad); border-color: #fecaca; background: #fef2f2; }
    details {
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 10px 12px;
      background: #fff;
      margin: 8px 0;
    }
    summary { cursor: pointer; font-weight: 600; }
    .event-meta {
      display: grid;
      grid-template-columns: 90px 1fr 120px 160px;
      gap: 8px;
      margin: 8px 0;
      color: var(--muted);
      font-size: 12px;
    }
    @media (max-width: 760px) {
      header { padding: 20px; }
      main { padding: 16px; }
      .grid.two, .grid.four { grid-template-columns: 1fr; }
      dl { grid-template-columns: 1fr; }
      .event-meta { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>
  <header>
    <h1>Jeju Run {{.Metadata.RunID}}</h1>
    <div class="subtle">Generated {{.GeneratedAt.Format "2006-01-02 15:04:05"}} from {{.RunDir}}</div>
  </header>
  <main>
    <section class="panel">
      <h2>Overview</h2>
      <div class="grid four">
        <div class="metric"><div class="label">Status</div><div class="value">{{.Metadata.Status}}</div></div>
        <div class="metric"><div class="label">Agent</div><div class="value">{{.Metadata.Agent}}</div></div>
        <div class="metric"><div class="label">Steps</div><div class="value">{{.Summary.Steps}}</div></div>
        <div class="metric"><div class="label">Duration</div><div class="value">{{if .Duration}}{{.Duration}}{{else}}running{{end}}</div></div>
      </div>
    </section>

    <section class="panel">
      <h2>Run Details</h2>
      <dl>
        <dt>Task</dt><dd>{{.Metadata.Input}}</dd>
        <dt>Started</dt><dd>{{.Metadata.StartedAt.Format "2006-01-02 15:04:05"}}</dd>
        <dt>Ended</dt><dd>{{if .Metadata.EndedAt}}{{.Metadata.EndedAt.Format "2006-01-02 15:04:05"}}{{else}}-{{end}}</dd>
        <dt>Config Snapshot</dt><dd>{{.Metadata.ConfigSnapshot}}</dd>
        <dt>Trajectory</dt><dd>{{.Metadata.Trajectory}}</dd>
        <dt>Final</dt><dd>{{.Metadata.Final}}</dd>
        {{if .Metadata.Evaluation}}<dt>Evaluation</dt><dd>{{.Metadata.Evaluation}}</dd>{{end}}
      </dl>
    </section>

    <section class="panel">
      <h2>Summary</h2>
      <table>
        <thead><tr><th>Area</th><th>Started</th><th>Completed</th><th>Failed</th><th>Other</th></tr></thead>
        <tbody>
          <tr><td>Model calls</td><td>{{.Summary.ModelStarted}}</td><td>{{.Summary.ModelCompleted}}</td><td>{{.Summary.ModelFailed}}</td><td>-</td></tr>
          <tr><td>Tool calls</td><td>{{.Summary.ToolStarted}}</td><td>{{.Summary.ToolCompleted}}</td><td>{{.Summary.ToolFailed}}</td><td>-</td></tr>
          <tr><td>Permissions</td><td>{{.Summary.PermissionChecked}}</td><td>{{.Summary.PermissionApproved}}</td><td>{{.Summary.PermissionDenied}}</td><td>-</td></tr>
          <tr><td>Skills</td><td>{{.Summary.SkillDisclosed}}</td><td>{{.Summary.SkillLoaded}}</td><td>-</td><td>artifacts {{.Summary.Artifacts}}</td></tr>
        </tbody>
      </table>
    </section>

    <div class="grid two">
      <section class="panel">
        <h2>Final Output</h2>
        {{if .Final}}<pre>{{.Final}}</pre>{{else}}<div class="subtle">No final.md content found.</div>{{end}}
      </section>

      <section class="panel">
        <h2>Evaluation</h2>
        {{if .EvaluationExists}}
          <dl>
            <dt>Passed</dt><dd>{{if .Evaluation.Passed}}<span class="badge ok">true</span>{{else}}<span class="badge bad">false</span>{{end}}</dd>
            <dt>Score</dt><dd>{{.Evaluation.Score}}</dd>
            <dt>Evaluators</dt><dd>{{len .Evaluation.Evaluators}}</dd>
          </dl>
          {{range .Evaluation.Evaluators}}
            <details>
              <summary>{{.Name}} <span class="badge">{{.Type}}</span></summary>
              <dl>
                <dt>Passed</dt><dd>{{.Passed}}</dd>
                <dt>Score</dt><dd>{{.Score}}</dd>
              </dl>
              {{if .Results}}
                <table>
                  <thead><tr><th>Rule</th><th>Passed</th><th>Message</th></tr></thead>
                  <tbody>{{range .Results}}<tr><td>{{.Rule}}</td><td>{{.Passed}}</td><td>{{.Message}}</td></tr>{{end}}</tbody>
                </table>
              {{end}}
            </details>
          {{end}}
        {{else}}
          <div class="subtle">No evaluation.json content found.</div>
        {{end}}
      </section>
    </div>

    <section class="panel">
      <h2>Artifacts</h2>
      {{if .Artifacts}}
        <table>
          <thead><tr><th>Path</th><th style="width: 140px;">Size</th></tr></thead>
          <tbody>{{range .Artifacts}}<tr><td>{{.Path}}</td><td>{{.Size}} bytes</td></tr>{{end}}</tbody>
        </table>
      {{else}}
        <div class="subtle">No artifacts found.</div>
      {{end}}
    </section>

    <section class="panel">
      <h2>Trajectory</h2>
      {{range .Events}}
        <details>
          <summary>{{.Type}} <span class="badge">step {{.Step}}</span></summary>
          <div class="event-meta">
            <div>{{.ID}}</div>
            <div>{{.Actor}}</div>
            <div>step {{.Step}}</div>
            <div>{{.Timestamp}}</div>
          </div>
          {{if .PayloadJSON}}<pre>{{.PayloadJSON}}</pre>{{else}}<div class="subtle">No payload.</div>{{end}}
        </details>
      {{end}}
    </section>

    <section class="panel">
      <h2>Raw Metadata</h2>
      <pre>{{.MetadataJSON}}</pre>
    </section>

    <section class="panel">
      <h2>Config Snapshot</h2>
      {{if .ConfigSnapshot}}<pre>{{.ConfigSnapshot}}</pre>{{else}}<div class="subtle">No config snapshot found.</div>{{end}}
    </section>
  </main>
</body>
</html>
`))
