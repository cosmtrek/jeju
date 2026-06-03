package evolve

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/evaluate"
)

func TestBaselineOnlyRunsTrainAndSelection(t *testing.T) {
	root := writeFixture(t)
	result, err := Run(context.Background(), filepath.Join(root, "experiments", "research-evolve.yaml"), RunOptions{BaselineOnly: true})
	if err != nil {
		t.Fatalf("Run baseline-only failed: %v", err)
	}
	if result.BestID != "baseline" {
		t.Fatalf("expected baseline best, got %q", result.BestID)
	}
	report, err := os.ReadFile(result.ReportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	text := string(report)
	if !strings.Contains(text, "### train") || !strings.Contains(text, "### selection") {
		t.Fatalf("report missing train/selection metrics:\n%s", text)
	}
	if !strings.Contains(text, "evaluation.score") {
		t.Fatalf("report missing objective metric:\n%s", text)
	}
	if !strings.Contains(text, "`data.test` is configured but was not run") {
		t.Fatalf("report missing test opt-in note:\n%s", text)
	}
}

func TestRunTestRunsConfiguredTestSplit(t *testing.T) {
	root := writeFixture(t)
	result, err := Run(context.Background(), filepath.Join(root, "experiments", "research-evolve.yaml"), RunOptions{BaselineOnly: true, RunTest: true})
	if err != nil {
		t.Fatalf("Run baseline-only --test failed: %v", err)
	}
	report, err := os.ReadFile(result.ReportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	text := string(report)
	if !strings.Contains(text, "### test") {
		t.Fatalf("report missing test metrics:\n%s", text)
	}
	if !strings.Contains(text, "Test metrics do not affect candidate acceptance") {
		t.Fatalf("report missing final holdout note:\n%s", text)
	}
	data, err := os.ReadFile(filepath.Join(result.OutputDir, "best", "results.json"))
	if err != nil {
		t.Fatalf("read best results: %v", err)
	}
	if !strings.Contains(string(data), `"split": "test"`) {
		t.Fatalf("best results missing test split:\n%s", string(data))
	}
}

func TestRunTestRunsAfterFullSelectionPath(t *testing.T) {
	root := writeFixture(t)
	manifestPath := filepath.Join(root, "experiments", "research-evolve.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	text := strings.Replace(string(data), "  iterations: 1\n", "  iterations: -1\n", 1)
	if err := os.WriteFile(manifestPath, []byte(text), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	result, err := Run(context.Background(), manifestPath, RunOptions{RunTest: true})
	if err != nil {
		t.Fatalf("Run full --test failed: %v", err)
	}
	report, err := os.ReadFile(result.ReportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(report), "### test") {
		t.Fatalf("report missing test metrics:\n%s", string(report))
	}
	data, err = os.ReadFile(filepath.Join(result.OutputDir, "best", "results.json"))
	if err != nil {
		t.Fatalf("read best results: %v", err)
	}
	if !strings.Contains(string(data), `"split": "test"`) {
		t.Fatalf("best results missing test split:\n%s", string(data))
	}
}

func TestRunTestRequiresConfiguredTestSplit(t *testing.T) {
	root := writeFixture(t)
	manifestPath := filepath.Join(root, "experiments", "research-evolve.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	text := strings.Replace(string(data), "  test: ../datasets/test.jsonl\n", "", 1)
	if err := os.WriteFile(manifestPath, []byte(text), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	_, err = Run(context.Background(), manifestPath, RunOptions{BaselineOnly: true, RunTest: true})
	if err == nil {
		t.Fatal("expected --test without data.test to fail")
	}
	if !strings.Contains(err.Error(), "--test requires data.test") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateCandidateAppliesExactReplacement(t *testing.T) {
	root := writeFixture(t)
	exp, err := LoadFile(filepath.Join(root, "experiments", "research-evolve.yaml"))
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	ctrl := &controller{
		exp:    exp,
		outDir: filepath.Join(root, ".jeju-dev", "evolve", "test"),
		id:     "test",
	}
	if err := os.MkdirAll(ctrl.outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base, err := ctrl.createBaseline()
	if err != nil {
		t.Fatalf("createBaseline failed: %v", err)
	}
	proposal := Proposal{
		ID:         "p1",
		Hypothesis: "increase max steps",
		Changes: []PatchOp{{
			Target:  "runtime.limits.maxSteps",
			Find:    "maxSteps: 20",
			Replace: "maxSteps: 24",
		}},
	}
	cand, err := ctrl.createCandidate(1, 1, base, proposal)
	if err != nil {
		t.Fatalf("createCandidate failed: %v", err)
	}
	data, err := os.ReadFile(cand.ManifestPath)
	if err != nil {
		t.Fatalf("read candidate manifest: %v", err)
	}
	if !strings.Contains(string(data), "maxSteps: 24") {
		t.Fatalf("candidate manifest was not patched:\n%s", string(data))
	}
}

func TestCreateCandidateAppliesEditableFilePatch(t *testing.T) {
	root := writeFixture(t)
	exp, err := LoadFile(filepath.Join(root, "experiments", "research-evolve.yaml"))
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	exp.Target.Editable = append(exp.Target.Editable, "file:../skills/research/SKILL.md")
	ctrl := &controller{
		exp:    exp,
		outDir: filepath.Join(root, ".jeju-dev", "evolve", "test"),
		id:     "test",
	}
	if err := os.MkdirAll(ctrl.outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base, err := ctrl.createBaseline()
	if err != nil {
		t.Fatalf("createBaseline failed: %v", err)
	}
	proposal := Proposal{
		ID:         "p1",
		Hypothesis: "improve reusable skill guidance",
		Changes: []PatchOp{{
			Target:  "file:../skills/research/SKILL.md",
			Find:    "Always answer with Jeju Mock Result.\n",
			Replace: "Always answer with Jeju Mock Result and cite the active skill.\n",
		}},
	}
	cand, err := ctrl.createCandidate(1, 1, base, proposal)
	if err != nil {
		t.Fatalf("createCandidate failed: %v", err)
	}
	skillPath := filepath.Join(filepath.Dir(cand.ManifestPath), "..", "skills", "research", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read candidate skill: %v", err)
	}
	if !strings.Contains(string(data), "cite the active skill") {
		t.Fatalf("candidate skill was not patched:\n%s", string(data))
	}
	sourceData, err := os.ReadFile(filepath.Join(root, "skills", "research", "SKILL.md"))
	if err != nil {
		t.Fatalf("read source skill: %v", err)
	}
	if strings.Contains(string(sourceData), "cite the active skill") {
		t.Fatalf("source skill was modified:\n%s", string(sourceData))
	}
}

func TestCreateCandidateAppliesPatchUnderEditableDir(t *testing.T) {
	root := writeFixture(t)
	exp, err := LoadFile(filepath.Join(root, "experiments", "research-evolve.yaml"))
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	exp.Target.Editable = append(exp.Target.Editable, "dir:../skills/research")
	ctrl := &controller{
		exp:    exp,
		outDir: filepath.Join(root, ".jeju-dev", "evolve", "test"),
		id:     "test",
	}
	if err := os.MkdirAll(ctrl.outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base, err := ctrl.createBaseline()
	if err != nil {
		t.Fatalf("createBaseline failed: %v", err)
	}
	digestContent := ctrl.editableContent(base.ManifestPath)
	if !strings.Contains(digestContent["file:../skills/research/SKILL.md"], "Always answer with Jeju Mock Result") {
		t.Fatalf("digest missing editable dir file content: %#v", digestContent)
	}
	proposal := Proposal{
		ID:         "p1",
		Hypothesis: "improve a skill under an editable dir",
		Changes: []PatchOp{{
			Target:  "file:../skills/research/SKILL.md",
			Find:    "Always answer with Jeju Mock Result.\n",
			Replace: "Always answer with Jeju Mock Result and cite the active skill.\n",
		}},
	}
	cand, err := ctrl.createCandidate(1, 1, base, proposal)
	if err != nil {
		t.Fatalf("createCandidate failed: %v", err)
	}
	skillPath := filepath.Join(filepath.Dir(cand.ManifestPath), "..", "skills", "research", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read candidate skill: %v", err)
	}
	if !strings.Contains(string(data), "cite the active skill") {
		t.Fatalf("candidate skill was not patched:\n%s", string(data))
	}
}

func TestCreateCandidateUsesSkillAliasForActivationAndFiles(t *testing.T) {
	root := writeFixture(t)
	exp, err := LoadFile(filepath.Join(root, "experiments", "research-evolve.yaml"))
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	exp.Target.Editable = []string{"skill:research"}
	ctrl := &controller{
		exp:    exp,
		outDir: filepath.Join(root, ".jeju-dev", "evolve", "test"),
		id:     "test",
	}
	if err := os.MkdirAll(ctrl.outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base, err := ctrl.createBaseline()
	if err != nil {
		t.Fatalf("createBaseline failed: %v", err)
	}
	digestContent := ctrl.editableContent(base.ManifestPath)
	if !strings.Contains(digestContent["file:../skills/research/SKILL.md"], "Always answer with Jeju Mock Result") {
		t.Fatalf("digest missing skill alias content: %#v", digestContent)
	}
	proposal := Proposal{
		ID:         "p1",
		Hypothesis: "improve the research skill",
		Changes: []PatchOp{{
			Target:  "file:../skills/research/SKILL.md",
			Find:    "Always answer with Jeju Mock Result.\n",
			Replace: "Always answer with Jeju Mock Result and cite the active skill.\n",
		}},
	}
	cand, err := ctrl.createCandidate(1, 1, base, proposal)
	if err != nil {
		t.Fatalf("createCandidate failed: %v", err)
	}
	skillPath := filepath.Join(filepath.Dir(cand.ManifestPath), "..", "skills", "research", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read candidate skill: %v", err)
	}
	if !strings.Contains(string(data), "cite the active skill") {
		t.Fatalf("candidate skill was not patched:\n%s", string(data))
	}
}

func TestCreateCandidateWritesFileUnderEditableDir(t *testing.T) {
	root := writeFixture(t)
	exp, err := LoadFile(filepath.Join(root, "experiments", "research-evolve.yaml"))
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	exp.Target.Editable = append(exp.Target.Editable, "dir:../skills/research")
	ctrl := &controller{
		exp:    exp,
		outDir: filepath.Join(root, ".jeju-dev", "evolve", "test"),
		id:     "test",
	}
	if err := os.MkdirAll(ctrl.outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base, err := ctrl.createBaseline()
	if err != nil {
		t.Fatalf("createBaseline failed: %v", err)
	}
	proposal := Proposal{
		ID:         "p1",
		Hypothesis: "add a note under the editable skill directory",
		Changes: []PatchOp{{
			Target:  "file:../skills/research/NOTES.md",
			Op:      "write",
			Content: "Use the research skill for all mock result tasks.\n",
		}},
	}
	cand, err := ctrl.createCandidate(1, 1, base, proposal)
	if err != nil {
		t.Fatalf("createCandidate failed: %v", err)
	}
	notePath := filepath.Join(filepath.Dir(cand.ManifestPath), "..", "skills", "research", "NOTES.md")
	data, err := os.ReadFile(notePath)
	if err != nil {
		t.Fatalf("read candidate note: %v", err)
	}
	if !strings.Contains(string(data), "mock result tasks") {
		t.Fatalf("candidate note was not written:\n%s", string(data))
	}
}

func TestCreateCandidateUsesToolAliasForDescriptionOnly(t *testing.T) {
	root := writeFixture(t)
	addLookupToolFixture(t, root)
	exp, err := LoadFile(filepath.Join(root, "experiments", "research-evolve.yaml"))
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	exp.Target.Editable = []string{"tool:lookup"}
	ctrl := &controller{
		exp:    exp,
		outDir: filepath.Join(root, ".jeju-dev", "evolve", "test"),
		id:     "test",
	}
	if err := os.MkdirAll(ctrl.outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base, err := ctrl.createBaseline()
	if err != nil {
		t.Fatalf("createBaseline failed: %v", err)
	}
	digestContent := ctrl.editableContent(base.ManifestPath)
	if !strings.Contains(digestContent["manifest"], "description: Lookup things.") {
		t.Fatalf("digest missing tool manifest: %#v", digestContent)
	}
	if _, ok := digestContent["file:../schemas/lookup.schema.json"]; ok {
		t.Fatalf("tool alias should not expose schema file by default: %#v", digestContent)
	}
	proposal := Proposal{
		ID:         "p1",
		Hypothesis: "improve lookup tool disclosure",
		Changes: []PatchOp{{
			Target:  "tools[0].description",
			Find:    "description: Lookup things.",
			Replace: "description: Search workspace notes by query before answering evidence-backed questions.",
		}},
	}
	cand, err := ctrl.createCandidate(1, 1, base, proposal)
	if err != nil {
		t.Fatalf("createCandidate failed: %v", err)
	}
	data, err := os.ReadFile(cand.ManifestPath)
	if err != nil {
		t.Fatalf("read candidate manifest: %v", err)
	}
	if !strings.Contains(string(data), "Search workspace notes") {
		t.Fatalf("candidate tool description was not patched:\n%s", string(data))
	}
}

func TestCreateCandidateRequiresExplicitFileForToolSchema(t *testing.T) {
	root := writeFixture(t)
	addLookupToolFixture(t, root)
	exp, err := LoadFile(filepath.Join(root, "experiments", "research-evolve.yaml"))
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	exp.Target.Editable = []string{"tool:lookup"}
	ctrl := &controller{
		exp:    exp,
		outDir: filepath.Join(root, ".jeju-dev", "evolve", "test"),
		id:     "test",
	}
	if err := os.MkdirAll(ctrl.outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base, err := ctrl.createBaseline()
	if err != nil {
		t.Fatalf("createBaseline failed: %v", err)
	}
	proposal := Proposal{
		ID:         "p1",
		Hypothesis: "attempt to edit schema without explicit file permission",
		Changes: []PatchOp{{
			Target:  "file:../schemas/lookup.schema.json",
			Op:      "write",
			Content: "{}\n",
		}},
	}
	_, err = ctrl.createCandidate(1, 1, base, proposal)
	if err == nil {
		t.Fatalf("expected schema write to be rejected")
	}
	if !strings.Contains(err.Error(), `target "file:../schemas/lookup.schema.json" is not editable`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateCandidateRejectsFilePatchOutsideBundle(t *testing.T) {
	root := writeFixture(t)
	exp, err := LoadFile(filepath.Join(root, "experiments", "research-evolve.yaml"))
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	exp.Target.Editable = append(exp.Target.Editable, "file:../../outside.txt")
	ctrl := &controller{
		exp:    exp,
		outDir: filepath.Join(root, ".jeju-dev", "evolve", "test"),
		id:     "test",
	}
	if err := os.MkdirAll(ctrl.outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base, err := ctrl.createBaseline()
	if err != nil {
		t.Fatalf("createBaseline failed: %v", err)
	}
	proposal := Proposal{
		ID:         "p1",
		Hypothesis: "attempt to edit outside the bundle",
		Changes: []PatchOp{{
			Target:  "file:../../outside.txt",
			Find:    "x",
			Replace: "y",
		}},
	}
	_, err = ctrl.createCandidate(1, 1, base, proposal)
	if err == nil {
		t.Fatalf("expected outside file patch to be rejected")
	}
	if !strings.Contains(err.Error(), "resolves outside candidate bundle") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateCandidateRejectsForbiddenDirPatch(t *testing.T) {
	root := writeFixture(t)
	exp, err := LoadFile(filepath.Join(root, "experiments", "research-evolve.yaml"))
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	exp.Target.Editable = append(exp.Target.Editable, "dir:../skills")
	exp.Target.Forbidden = append(exp.Target.Forbidden, "dir:../skills/research")
	ctrl := &controller{
		exp:    exp,
		outDir: filepath.Join(root, ".jeju-dev", "evolve", "test"),
		id:     "test",
	}
	if err := os.MkdirAll(ctrl.outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base, err := ctrl.createBaseline()
	if err != nil {
		t.Fatalf("createBaseline failed: %v", err)
	}
	proposal := Proposal{
		ID:         "p1",
		Hypothesis: "attempt to edit forbidden dir",
		Changes: []PatchOp{{
			Target:  "file:../skills/research/SKILL.md",
			Find:    "Always answer with Jeju Mock Result.\n",
			Replace: "Always answer with Jeju Mock Result and cite the active skill.\n",
		}},
	}
	_, err = ctrl.createCandidate(1, 1, base, proposal)
	if err == nil {
		t.Fatalf("expected forbidden dir patch to be rejected")
	}
	if !strings.Contains(err.Error(), `target "file:../skills/research/SKILL.md" is forbidden`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFileRejectsExpandedAliasConflict(t *testing.T) {
	root := writeFixture(t)
	expPath := filepath.Join(root, "experiments", "research-evolve.yaml")
	data, err := os.ReadFile(expPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), `  editable:
    - instructions.system
    - runtime.limits.maxSteps
data:
`, `  editable:
    - harness:prompt
    - runtime.limits.maxSteps
  forbidden:
    - instructions.system
data:
`, 1)
	writeFile(t, expPath, updated)

	_, err = LoadFile(expPath)
	if err == nil {
		t.Fatalf("expected expanded alias conflict to be rejected")
	}
	if !strings.Contains(err.Error(), `target.editable "instructions.system" conflicts with target.forbidden "instructions.system"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadFileRejectsExpandedDirFileConflict(t *testing.T) {
	root := writeFixture(t)
	expPath := filepath.Join(root, "experiments", "research-evolve.yaml")
	data, err := os.ReadFile(expPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), `  editable:
    - instructions.system
    - runtime.limits.maxSteps
data:
`, `  editable:
    - dir:../skills
  forbidden:
    - file:../skills/research/SKILL.md
data:
`, 1)
	writeFile(t, expPath, updated)

	_, err = LoadFile(expPath)
	if err == nil {
		t.Fatalf("expected expanded file/dir conflict to be rejected")
	}
	if !strings.Contains(err.Error(), `target.editable "dir:../skills" conflicts with target.forbidden "file:../skills/research/SKILL.md"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateCandidateRejectsForbiddenTextReplacement(t *testing.T) {
	root := writeFixture(t)
	exp, err := LoadFile(filepath.Join(root, "experiments", "research-evolve.yaml"))
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	ctrl := &controller{
		exp:    exp,
		outDir: filepath.Join(root, ".jeju-dev", "evolve", "test"),
		id:     "test",
	}
	if err := os.MkdirAll(ctrl.outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base, err := ctrl.createBaseline()
	if err != nil {
		t.Fatalf("createBaseline failed: %v", err)
	}
	proposal := Proposal{
		ID:         "p1",
		Hypothesis: "attempt forbidden access escalation through an editable target",
		Changes: []PatchOp{{
			Target:  "runtime.limits.maxSteps",
			Find:    "access: readOnly",
			Replace: "access: full",
		}},
	}
	_, err = ctrl.createCandidate(1, 1, base, proposal)
	if err == nil {
		t.Fatalf("expected forbidden patch to be rejected")
	}
	if !strings.Contains(err.Error(), `forbidden field "permissions.access" changed`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateCandidateRejectsUneditableTextReplacement(t *testing.T) {
	root := writeFixture(t)
	exp, err := LoadFile(filepath.Join(root, "experiments", "research-evolve.yaml"))
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	ctrl := &controller{
		exp:    exp,
		outDir: filepath.Join(root, ".jeju-dev", "evolve", "test"),
		id:     "test",
	}
	if err := os.MkdirAll(ctrl.outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base, err := ctrl.createBaseline()
	if err != nil {
		t.Fatalf("createBaseline failed: %v", err)
	}
	proposal := Proposal{
		ID:         "p1",
		Hypothesis: "attempt uneditable approval change through an editable target",
		Changes: []PatchOp{{
			Target:  "runtime.limits.maxSteps",
			Find:    "approval: onRequest",
			Replace: "approval: never",
		}},
	}
	_, err = ctrl.createCandidate(1, 1, base, proposal)
	if err == nil {
		t.Fatalf("expected uneditable patch to be rejected")
	}
	if !strings.Contains(err.Error(), `forbidden field "permissions.approval" changed`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseProposalsRejectsEmptyOrInvalidChanges(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{
			name:    "wrapper null changes",
			input:   `{"proposals":[{"hypothesis":"noop","changes":null}]}`,
			wantErr: "proposal 1 has no changes",
		},
		{
			name:    "wrapper empty changes",
			input:   `{"proposals":[{"hypothesis":"noop","changes":[]}]}`,
			wantErr: "proposal 1 has no changes",
		},
		{
			name:    "array empty changes",
			input:   `[{"hypothesis":"noop","changes":[]}]`,
			wantErr: "proposal 1 has no changes",
		},
		{
			name:    "invalid patch",
			input:   `{"proposals":[{"hypothesis":"noop","changes":[{"target":"","find":"old","replace":"new"}]}]}`,
			wantErr: "proposal 1 change 1 patch target is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proposals, err := parseProposals(tt.input)
			if err == nil {
				t.Fatalf("expected parseProposals to fail, got proposals: %+v", proposals)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestEvaluateCandidateParallelUsageCount(t *testing.T) {
	root := writeFixture(t)
	exp, err := LoadFile(filepath.Join(root, "experiments", "research-evolve.yaml"))
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	exp.Search.Parallelism = 4
	exp.Search.TrialsPerTask = 4
	ctrl := &controller{
		exp:    exp,
		outDir: filepath.Join(root, ".jeju-dev", "evolve", "test"),
		id:     "test",
	}
	if err := os.MkdirAll(ctrl.outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	events, err := newEventWriter(filepath.Join(ctrl.outDir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close()
	ctrl.events = events
	if err := ctrl.loadData(); err != nil {
		t.Fatalf("loadData failed: %v", err)
	}
	base, err := ctrl.createBaseline()
	if err != nil {
		t.Fatalf("createBaseline failed: %v", err)
	}
	result, err := ctrl.evaluateCandidate(context.Background(), base, "train", ctrl.train, nil)
	if err != nil {
		t.Fatalf("evaluateCandidate failed: %v", err)
	}
	if len(result.Trials) != 4 {
		t.Fatalf("expected 4 trials, got %d", len(result.Trials))
	}
	runCount, _ := ctrl.usageSnapshot()
	if runCount != 4 {
		t.Fatalf("expected runCount 4, got %d", runCount)
	}
}

func TestMetricValueUsesTaskWeights(t *testing.T) {
	got, err := metricValue("evaluation.score", []TrialResult{
		{Weight: 1, Evaluation: evaluate.Result{Score: 0}},
		{Weight: 3, Evaluation: evaluate.Result{Score: 1}},
	})
	if err != nil {
		t.Fatalf("metricValue failed: %v", err)
	}
	if got != 0.75 {
		t.Fatalf("expected weighted average 0.75, got %f", got)
	}
}

func TestBuildDigestWithholdsSelectionDetails(t *testing.T) {
	ctrl := &controller{
		exp: &Experiment{
			Objective: ObjectiveSpec{Metric: "evaluation.score", Direction: "maximize"},
			Target:    TargetSpec{Editable: []string{"instructions.system"}},
		},
	}
	cand := &candidate{
		ID:           "candidate-1",
		ManifestPath: filepath.Join(t.TempDir(), "missing.yaml"),
		Results: map[string]*SplitResult{
			"train": {
				Split: "train",
				Trials: []TrialResult{{
					TaskID: "train-visible",
					Final:  "train final",
				}},
			},
			"selection": {
				Split: "selection",
				Trials: []TrialResult{{
					TaskID: "selection-hidden",
					Final:  "selection final",
				}},
			},
		},
	}
	digest := ctrl.buildDigest(1, cand)
	data, err := json.Marshal(digest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "selection-hidden") || strings.Contains(text, "selection final") || strings.Contains(text, "best_results") {
		t.Fatalf("digest leaked selection details: %s", text)
	}
	if !strings.Contains(text, "train-visible") {
		t.Fatalf("digest missing train feedback: %s", text)
	}
}

func writeFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mkdirs := []string{"agents", "prompts", "workspace", "datasets", "experiments", "skills/research"}
	for _, dir := range mkdirs {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(root, "prompts", "research.md"), "You are a concise research agent.\n")
	writeFile(t, filepath.Join(root, "skills", "research", "SKILL.md"), `---
name: research
description: Research fixture skill.
---

Always answer with Jeju Mock Result.
`)
	writeFile(t, filepath.Join(root, "agents", "research.agent.yaml"), `apiVersion: jeju/v1alpha1
kind: Agent
metadata:
  name: research
models:
  providers:
    primary:
      type: mock
      model: mock
instructions:
  system: ../prompts/research.md
runtime:
  model: primary
  limits:
    maxSteps: 20
workspace:
  path: ../workspace
skills:
  dirs:
    - ../skills
  active:
    - research
permissions:
  access: readOnly
  approval: onRequest
evaluate:
  enabled: true
  evaluators:
    - name: basic
      uses: rules
      rules:
        - finalAnswerExists
        - runCompleted
`)
	writeFile(t, filepath.Join(root, "agents", "evolver.agent.yaml"), `apiVersion: jeju/v1alpha1
kind: Agent
metadata:
  name: evolver
models:
  providers:
    primary:
      type: mock
      model: mock
instructions:
  system: ../prompts/research.md
runtime:
  model: primary
workspace:
  path: ../workspace
permissions:
  access: readOnly
`)
	task := `{"id":"task-1","input":{"question":"Summarize Jeju"},"expected":{"mustInclude":["Jeju Mock Result"]}}` + "\n"
	writeFile(t, filepath.Join(root, "datasets", "train.jsonl"), task)
	writeFile(t, filepath.Join(root, "datasets", "selection.jsonl"), task)
	writeFile(t, filepath.Join(root, "datasets", "test.jsonl"), task)
	writeFile(t, filepath.Join(root, "experiments", "research-evolve.yaml"), `apiVersion: jeju/v1alpha1
kind: EvolutionExperiment
metadata:
  name: research-evolve
target:
  agent: ../agents/research.agent.yaml
  editable:
    - instructions.system
    - runtime.limits.maxSteps
data:
  train: ../datasets/train.jsonl
  selection: ../datasets/selection.jsonl
  test: ../datasets/test.jsonl
objective:
  metric: evaluation.score
  minDelta: 0.01
  guards:
    - "evaluation.passed_rate >= baseline.evaluation.passed_rate"
evolver:
  agent: ../agents/evolver.agent.yaml
  proposals: 1
search:
  iterations: 1
  parallelism: 1
output:
  dir: ../.jeju-dev/evolve/research
`)
	return root
}

func addLookupToolFixture(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "schemas"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "workspace", "lookup.sh"), "#!/bin/sh\nprintf lookup\n")
	writeFile(t, filepath.Join(root, "schemas", "lookup.schema.json"), `{
  "type": "object",
  "properties": {
    "query": {"type": "string"}
  },
  "required": ["query"]
}
`)
	manifestPath := filepath.Join(root, "agents", "research.agent.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	insert := `tools:
  - name: lookup
    uses: command
    description: Lookup things.
    capabilities: [workspaceRead]
    command:
      run: ../workspace/lookup.sh
    input:
      schema: ../schemas/lookup.schema.json
`
	text := strings.Replace(string(data), "permissions:\n", insert+"permissions:\n", 1)
	if err := os.WriteFile(manifestPath, []byte(text), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
