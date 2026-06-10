package team

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var markdownRenderer = goldmark.New(goldmark.WithExtensions(extension.GFM))

type teamReportData struct {
	Summary          Summary
	Tasks            []TaskState
	ChildRuns        []ChildRunSummary
	FinalHTML        template.HTML
	Duration         string
	StatusClass      string
	TokensTotalLabel string
	TokensInLabel    string
	TokensOutLabel   string
	StartedLabel     string
	EndedLabel       string
}

func WriteReport(path string, summary Summary) error {
	return writeReport(path, summary)
}

func writeReport(path string, summary Summary) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return teamReportTemplate.Execute(file, buildTeamReportData(summary))
}

func buildTeamReportData(summary Summary) teamReportData {
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
	// Child runs keep recorded order: the lead-round/worker interleaving is the team timeline.
	children := append([]ChildRunSummary(nil), summary.ChildRuns...)
	started, _ := time.Parse(time.RFC3339Nano, summary.StartedAt)
	ended, _ := time.Parse(time.RFC3339Nano, summary.EndedAt)
	duration := ""
	if !started.IsZero() && !ended.IsZero() {
		duration = ended.Sub(started).Round(time.Millisecond).String()
	}
	return teamReportData{
		Summary:          summary,
		Tasks:            tasks,
		ChildRuns:        children,
		FinalHTML:        renderReportMarkdown(summary.Final),
		Duration:         duration,
		StatusClass:      statusClass(summary.Status),
		TokensTotalLabel: formatReportCount(summary.Stats.TotalTokens),
		TokensInLabel:    formatReportCount(summary.Stats.PromptTokens),
		TokensOutLabel:   formatReportCount(summary.Stats.CompletionTokens),
		StartedLabel:     formatClock(started),
		EndedLabel:       formatClock(ended),
	}
}

func renderReportMarkdown(src string) template.HTML {
	if src == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := markdownRenderer.Convert([]byte(src), &buf); err != nil {
		return template.HTML(template.HTMLEscapeString(src))
	}
	return template.HTML(buf.String())
}

func statusClass(status string) string {
	switch status {
	case StatusCompleted:
		return "ok"
	case StatusFailed:
		return "bad"
	default:
		return "warn"
	}
}

func formatReportCount(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func formatClock(ts time.Time) string {
	if ts.IsZero() {
		return "running"
	}
	return ts.Format("15:04:05")
}

func verificationLabel(task TaskState) string {
	if task.Verification.Passed {
		return "passed"
	}
	if len(task.Verification.Reasons) == 0 {
		return ""
	}
	return strings.Join(task.Verification.Reasons, "; ")
}

// statusTone maps task/child-run statuses onto the report color scheme.
func statusTone(status string) string {
	switch status {
	case TaskVerified, TaskCompleted:
		return "ok"
	case TaskRejected, TaskBlocked, StatusFailed:
		return "bad"
	default:
		return "warn"
	}
}

// taskFinalHTML renders a task final output: structured JSON contracts get a
// pretty-printed code block, anything else is rendered as markdown.
func taskFinalHTML(final string) template.HTML {
	final = strings.TrimSpace(final)
	if final == "" {
		return ""
	}
	if pretty, ok := prettyJSON(final); ok {
		return template.HTML(`<pre class="codeblock">` + template.HTMLEscapeString(pretty) + `</pre>`)
	}
	return renderReportMarkdown(final)
}

func prettyJSON(src string) (string, bool) {
	if !strings.HasPrefix(src, "{") && !strings.HasPrefix(src, "[") {
		return "", false
	}
	var value any
	if err := json.Unmarshal([]byte(src), &value); err != nil {
		return "", false
	}
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", false
	}
	return string(out), true
}

func formatDurationMS(ms int64) string {
	if ms <= 0 {
		return ""
	}
	d := time.Duration(ms) * time.Millisecond
	if d >= time.Second {
		d = d.Round(100 * time.Millisecond)
	}
	return d.String()
}

var teamReportTemplate = template.Must(template.New("team-report").Funcs(template.FuncMap{
	"verificationLabel": verificationLabel,
	"statusTone":        statusTone,
	"taskFinalHTML":     taskFinalHTML,
	"durationMS":        formatDurationMS,
	"count":             formatReportCount,
}).Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Jeju Team {{.Summary.Team}}</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #ffffff;
      --ink: #172033;
      --muted: #6b7280;
      --faint: #9aa2af;
      --line: #e5e8ee;
      --soft: #f5f6f8;
      --ok: #15803d;
      --bad: #b42318;
      --warn: #a15c07;
      --accent: #2563eb;
      --code: #101828;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--ink);
      font: 14px/1.55 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    .wrap {
      max-width: 1280px;
      margin: 0 auto;
      padding-left: 40px;
      padding-right: 40px;
    }
    header {
      padding: 28px 0;
      border-bottom: 1px solid var(--line);
    }
    .header-inner {
      display: grid;
      grid-template-columns: minmax(0, 1fr) 320px;
      gap: 28px 64px;
      align-items: start;
    }
    .header-id {
      display: flex;
      gap: 12px;
      align-items: baseline;
      flex-wrap: wrap;
    }
    h1 {
      margin: 0;
      font-size: 22px;
      font-weight: 650;
      letter-spacing: 0;
    }
    .run-id {
      margin-top: 5px;
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 12.5px;
      color: var(--muted);
    }
    .badge {
      display: inline-flex;
      align-items: center;
      height: 22px;
      padding: 0 8px;
      border-radius: 999px;
      font-size: 12px;
      font-weight: 650;
    }
    .badge.ok { color: var(--ok); background: #ecfdf3; }
    .badge.bad { color: var(--bad); background: #fef2f2; }
    .badge.warn { color: var(--warn); background: #fff7ed; }
    .kv {
      margin: 20px 0 0;
      display: flex;
      flex-direction: column;
      gap: 9px;
    }
    .kv-row {
      display: flex;
      gap: 16px;
      align-items: baseline;
    }
    .kv dt {
      flex: 0 0 64px;
      font-size: 12px;
      color: var(--muted);
      letter-spacing: 0;
    }
    .kv dd {
      margin: 0;
      flex: 1;
      min-width: 0;
      font-size: 13px;
      color: var(--ink);
      overflow-wrap: anywhere;
    }
    .mono {
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 12.5px;
    }
    .header-result {
      display: flex;
      flex-direction: column;
    }
    .metric-group {
      border-top: 1px solid var(--line);
      padding: 9px 0;
    }
    .metric-group:first-child { border-top: 0; padding-top: 0; }
    .metric {
      display: flex;
      justify-content: space-between;
      align-items: baseline;
      gap: 16px;
    }
    .metric-label {
      font-size: 12px;
      color: var(--muted);
      letter-spacing: 0;
    }
    .metric-val {
      font: 700 22px/1 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      letter-spacing: 0;
      font-variant-numeric: tabular-nums;
      color: var(--ink);
    }
    .metric-sub {
      margin-top: 6px;
      font-size: 12px;
      color: var(--muted);
      font-variant-numeric: tabular-nums;
      text-align: right;
    }
    main { padding: 28px 0 44px; }
    .layout {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(360px, 45%);
      gap: 64px;
      align-items: start;
    }
    section { margin-bottom: 34px; }
    h2 {
      margin: 0 0 12px;
      font-size: 12px;
      font-weight: 700;
      color: var(--muted);
      text-transform: uppercase;
      letter-spacing: 0;
    }
    .task {
      padding: 14px 16px;
      border-radius: 6px;
      background: var(--soft);
      overflow-wrap: anywhere;
      white-space: pre-wrap;
    }
    .final-md {
      color: var(--ink);
      overflow-wrap: anywhere;
    }
    .final-md > :first-child { margin-top: 0; }
    .final-md > :last-child { margin-bottom: 0; }
    table {
      width: 100%;
      border-collapse: collapse;
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
      font-size: 11px;
      font-weight: 650;
      text-transform: uppercase;
      letter-spacing: 0;
    }
    tr:last-child td { border-bottom: 0; }
    a {
      color: var(--accent);
      text-decoration: none;
      border-bottom: 1px dashed currentColor;
    }
    .subtle { color: var(--muted); }
    .status-ok { color: var(--ok); font-weight: 650; }
    .status-bad { color: var(--bad); font-weight: 650; }
    .status-warn { color: var(--warn); font-weight: 650; }
    .cards {
      display: flex;
      flex-direction: column;
      gap: 8px;
    }
    details.card {
      border: 1px solid var(--line);
      border-radius: 6px;
      background: var(--bg);
    }
    details.card > summary {
      list-style: none;
      cursor: pointer;
      display: flex;
      align-items: baseline;
      gap: 10px;
      padding: 9px 12px;
      user-select: none;
    }
    details.card > summary::-webkit-details-marker { display: none; }
    details.card > summary::before {
      content: "";
      flex: 0 0 auto;
      align-self: center;
      width: 0;
      height: 0;
      border-left: 5px solid var(--faint);
      border-top: 4px solid transparent;
      border-bottom: 4px solid transparent;
      transition: transform 0.12s ease;
    }
    details.card[open] > summary::before { transform: rotate(90deg); }
    details.card[open] > summary { border-bottom: 1px solid var(--line); }
    .card-title {
      font-weight: 650;
      overflow-wrap: anywhere;
    }
    .card-meta {
      font-size: 12px;
      color: var(--muted);
      white-space: nowrap;
    }
    .card-meta.grow { flex: 1; }
    .card-tail {
      margin-left: auto;
      font-size: 12px;
      color: var(--muted);
      font-variant-numeric: tabular-nums;
      white-space: nowrap;
    }
    .card-body {
      padding: 12px;
      display: flex;
      flex-direction: column;
      gap: 12px;
    }
    .field-label {
      margin-bottom: 4px;
      font-size: 11px;
      font-weight: 650;
      color: var(--muted);
      text-transform: uppercase;
      letter-spacing: 0;
    }
    .field-text {
      white-space: pre-wrap;
      overflow-wrap: anywhere;
    }
    .chip {
      display: inline-flex;
      align-items: center;
      margin: 0 6px 4px 0;
      padding: 1px 8px;
      border: 1px solid var(--line);
      border-radius: 999px;
      background: var(--soft);
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 11.5px;
      color: var(--ink);
    }
    a.chip { border-bottom: 1px solid var(--line); color: var(--accent); }
    .codeblock {
      margin: 0;
      padding: 10px 12px;
      border: 1px solid var(--line);
      border-radius: 6px;
      background: var(--soft);
      font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
      font-size: 12px;
      line-height: 1.5;
      white-space: pre-wrap;
      overflow-wrap: anywhere;
      max-height: 420px;
      overflow: auto;
    }
    .final-md.in-card { font-size: 13px; }
    .stat-grid {
      display: grid;
      grid-template-columns: repeat(3, minmax(0, 1fr));
      gap: 10px 16px;
    }
    .stat-label {
      font-size: 11px;
      color: var(--muted);
    }
    .stat-val {
      font-weight: 650;
      font-variant-numeric: tabular-nums;
    }
    @media (max-width: 900px) {
      .wrap { padding-left: 20px; padding-right: 20px; }
      .header-inner, .layout { grid-template-columns: 1fr; gap: 28px; }
      .metric-sub { text-align: left; }
    }
  </style>
</head>
<body>
  <header>
    <div class="wrap header-inner">
      <div>
        <div class="header-id">
          <h1>{{.Summary.Team}}</h1>
          <span class="badge {{.StatusClass}}">{{.Summary.Status}}</span>
        </div>
        <div class="run-id">{{.Summary.TeamRunID}}</div>
        <dl class="kv">
          <div class="kv-row"><dt>kind</dt><dd>agent team</dd></div>
          <div class="kv-row"><dt>workers</dt><dd>{{range $i, $w := .Summary.DeclaredWorkers}}{{if $i}}<span class="subtle"> &middot; </span>{{end}}<span class="mono">{{$w}}</span>{{end}}</dd></div>
          <div class="kv-row"><dt>trajectory</dt><dd>projected team run</dd></div>
        </dl>
      </div>
      <div class="header-result">
        <div class="metric-group">
          <div class="metric"><span class="metric-label">rounds</span><span class="metric-val">{{.Summary.RoundCount}}/{{.Summary.MaxRounds}}</span></div>
        </div>
        <div class="metric-group">
          <div class="metric"><span class="metric-label">tasks</span><span class="metric-val">{{len .Tasks}}</span></div>
          <div class="metric-sub">{{.Summary.Stats.ChildRuns}} child runs</div>
        </div>
        <div class="metric-group">
          <div class="metric"><span class="metric-label">tokens</span><span class="metric-val">{{.TokensTotalLabel}}</span></div>
          <div class="metric-sub">in {{.TokensInLabel}} &middot; out {{.TokensOutLabel}}</div>
        </div>
        <div class="metric-group">
          <div class="metric"><span class="metric-label">duration</span><span class="metric-val">{{if .Duration}}{{.Duration}}{{else}}running{{end}}</span></div>
          <div class="metric-sub">{{.StartedLabel}} &rarr; {{.EndedLabel}}</div>
        </div>
      </div>
    </div>
  </header>
  <main class="wrap">
    <div class="layout">
      <div>
        <section>
          <h2>Task</h2>
          <div class="task">{{.Summary.Goal}}</div>
        </section>
        <section>
          <h2>Final Output</h2>
          {{if .FinalHTML}}<div class="final-md">{{.FinalHTML}}</div>{{else}}<div class="subtle">No final artifact found.</div>{{end}}
        </section>
      </div>
      <div>
        <section>
          <h2>Tasks</h2>
          {{if .Tasks}}
          <div class="cards">
            {{range .Tasks}}
            <details class="card" id="task-{{.ID}}">
              <summary>
                <span class="card-title mono">{{.ID}}</span>
                <span class="card-meta grow">{{.Worker}}</span>
                <span class="card-meta status-{{statusTone .Status}}">{{.Status}}</span>
                <span class="card-tail">r{{.RoundCreated}}{{if gt .Attempts 1}} &middot; {{.Attempts}} attempts{{end}}</span>
              </summary>
              <div class="card-body">
                <div>
                  <div class="field-label">Objective</div>
                  <div class="field-text">{{.Objective}}</div>
                </div>
                {{if .DependsOn}}
                <div>
                  <div class="field-label">Depends On</div>
                  <div>{{range .DependsOn}}<a class="chip" href="#task-{{.}}">{{.}}</a>{{end}}</div>
                </div>
                {{end}}
                {{if .OutputContract.RequiredFields}}
                <div>
                  <div class="field-label">Output Contract{{with .OutputContract.Format}} &middot; {{.}}{{end}}</div>
                  <div>{{range .OutputContract.RequiredFields}}<span class="chip">{{.}}</span>{{end}}</div>
                </div>
                {{end}}
                {{if verificationLabel .}}
                <div>
                  <div class="field-label">Verification</div>
                  <div class="field-text {{if .Verification.Passed}}status-ok{{else}}status-bad{{end}}">{{verificationLabel .}}</div>
                </div>
                {{end}}
                {{if .Error}}
                <div>
                  <div class="field-label">Error</div>
                  <div class="field-text status-bad">{{.Error}}</div>
                </div>
                {{end}}
                {{if taskFinalHTML .Final}}
                <div>
                  <div class="field-label">Output</div>
                  <div class="final-md in-card">{{taskFinalHTML .Final}}</div>
                </div>
                {{end}}
                <div>
                  <div class="field-label">Run</div>
                  <div>{{if .RunDir}}<a class="mono" href="{{.RunDir}}/trajectory.jsonl">{{.RunID}}</a>{{else}}<span class="subtle">none</span>{{end}}</div>
                </div>
              </div>
            </details>
            {{end}}
          </div>
          {{else}}<div class="subtle">No tasks recorded.</div>{{end}}
        </section>
        <section>
          <h2>Child Runs</h2>
          {{if .ChildRuns}}
          <div class="cards">
            {{range .ChildRuns}}
            <details class="card">
              <summary>
                <span class="card-title mono">{{.Label}}</span>
                <span class="card-meta grow">{{.Role}}</span>
                <span class="card-meta status-{{statusTone .Status}}">{{.Status}}</span>
                <span class="card-tail">{{count .Stats.TotalTokens}} tok{{with durationMS .Stats.DurationMS}} &middot; {{.}}{{end}}</span>
              </summary>
              <div class="card-body">
                <div class="stat-grid">
                  <div><div class="stat-label">agent</div><div class="stat-val mono">{{.Agent}}</div></div>
                  <div><div class="stat-label">model calls</div><div class="stat-val">{{.Stats.ModelCalls}}</div></div>
                  <div><div class="stat-label">tool calls</div><div class="stat-val">{{.Stats.ToolCalls}}</div></div>
                  <div><div class="stat-label">tokens in</div><div class="stat-val">{{count .Stats.PromptTokens}}</div></div>
                  <div><div class="stat-label">cache hit</div><div class="stat-val">{{count .Stats.PromptCacheHitTokens}}</div></div>
                  <div><div class="stat-label">tokens out</div><div class="stat-val">{{count .Stats.CompletionTokens}}</div></div>
                  {{if .Stats.ModelErrors}}<div><div class="stat-label">model errors</div><div class="stat-val status-bad">{{.Stats.ModelErrors}}</div></div>{{end}}
                  {{if .Stats.ToolErrors}}<div><div class="stat-label">tool errors</div><div class="stat-val status-bad">{{.Stats.ToolErrors}}</div></div>{{end}}
                  {{if .Stats.PermissionDenied}}<div><div class="stat-label">denied</div><div class="stat-val status-bad">{{.Stats.PermissionDenied}}</div></div>{{end}}
                </div>
                {{if .TaskID}}
                <div>
                  <div class="field-label">Task</div>
                  <div><a class="chip" href="#task-{{.TaskID}}">{{.TaskID}}</a></div>
                </div>
                {{end}}
                <div>
                  <div class="field-label">Run</div>
                  <div>{{if .RunDir}}<a class="mono" href="{{.RunDir}}/trajectory.jsonl">{{.RunID}}</a>{{else}}<span class="mono">{{.RunID}}</span>{{end}}</div>
                </div>
              </div>
            </details>
            {{end}}
          </div>
          {{else}}<div class="subtle">No child runs recorded.</div>{{end}}
        </section>
      </div>
    </div>
  </main>
  <script>
    (function () {
      function openTarget() {
        var el = location.hash && document.getElementById(location.hash.slice(1));
        if (el && el.tagName === "DETAILS") el.open = true;
      }
      window.addEventListener("hashchange", openTarget);
      openTarget();
    })();
  </script>
</body>
</html>
`))
