package evolve

import (
	"context"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cosmtrek/jeju/internal/evaluate"
)

func TestPerTaskScoresAveragesTrialsPerTask(t *testing.T) {
	scores, err := perTaskScores("evaluation.score", []TrialResult{
		{TaskID: "a", Weight: 1, Evaluation: evaluate.Result{Score: 0.2}},
		{TaskID: "a", Weight: 1, Evaluation: evaluate.Result{Score: 0.6}},
		{TaskID: "b", Weight: 1, Evaluation: evaluate.Result{Score: 1.0}},
	})
	if err != nil {
		t.Fatalf("perTaskScores failed: %v", err)
	}
	if got := scores["a"]; got != 0.4 {
		t.Fatalf("expected task a score 0.4, got %f", got)
	}
	if got := scores["b"]; got != 1.0 {
		t.Fatalf("expected task b score 1.0, got %f", got)
	}
}

func TestUpdatePoolWinsCountsInstanceFrontier(t *testing.T) {
	a := &poolEntry{cand: &candidate{ID: "a"}, taskScores: map[string]float64{"t1": 1.0, "t2": 0.0, "t3": 0.5}}
	b := &poolEntry{cand: &candidate{ID: "b"}, taskScores: map[string]float64{"t1": 0.0, "t2": 1.0, "t3": 0.5}}
	updatePoolWins([]*poolEntry{a, b}, "maximize")
	if a.wins != 2 || b.wins != 2 {
		t.Fatalf("expected shared frontier wins 2/2, got %d/%d", a.wins, b.wins)
	}
	c := &poolEntry{cand: &candidate{ID: "c"}, taskScores: map[string]float64{"t1": 0.0, "t2": 0.0, "t3": 0.0}}
	updatePoolWins([]*poolEntry{a, b, c}, "maximize")
	if c.wins != 0 {
		t.Fatalf("expected dominated candidate to win 0 tasks, got %d", c.wins)
	}
	updatePoolWins([]*poolEntry{a, b, c}, "minimize")
	if c.wins != 3 {
		t.Fatalf("expected minimize direction to flip frontier, got %d wins", c.wins)
	}
}

func TestSamplePoolParentIsDeterministicForSeed(t *testing.T) {
	pool := []*poolEntry{
		{cand: &candidate{ID: "a"}, wins: 3},
		{cand: &candidate{ID: "b"}, wins: 1},
	}
	first := samplePoolParent(pool, rand.New(rand.NewSource(7))).cand.ID
	second := samplePoolParent(pool, rand.New(rand.NewSource(7))).cand.ID
	if first != second {
		t.Fatalf("expected deterministic sampling, got %q then %q", first, second)
	}
	zero := []*poolEntry{{cand: &candidate{ID: "only"}}}
	if got := samplePoolParent(zero, rand.New(rand.NewSource(1))).cand.ID; got != "only" {
		t.Fatalf("expected fallback to uniform sampling, got %q", got)
	}
}

func TestPrunePoolKeepsFrontierMembersAndBest(t *testing.T) {
	a := &poolEntry{cand: &candidate{ID: "a"}, trainMetric: 0.9, taskScores: map[string]float64{"t1": 1.0, "t2": 0.0}}
	b := &poolEntry{cand: &candidate{ID: "b"}, trainMetric: 0.5, taskScores: map[string]float64{"t1": 0.0, "t2": 1.0}}
	dominated := &poolEntry{cand: &candidate{ID: "c"}, trainMetric: 0.1, taskScores: map[string]float64{"t1": 0.0, "t2": 0.0}}
	pruned := prunePool([]*poolEntry{a, b, dominated}, 8, "maximize", "b")
	if len(pruned) != 2 {
		t.Fatalf("expected dominated entry pruned, got %d entries", len(pruned))
	}
	pruned = prunePool([]*poolEntry{a, b}, 1, "maximize", "b")
	if len(pruned) != 1 || pruned[0].cand.ID != "b" {
		t.Fatalf("expected capped pool to retain best candidate, got %+v", pruned)
	}
}

func TestSampleTasksDeterministicSubset(t *testing.T) {
	tasks := []TaskCase{{ID: "t1"}, {ID: "t2"}, {ID: "t3"}, {ID: "t4"}, {ID: "t5"}}
	first := sampleTasks(tasks, 2, rand.New(rand.NewSource(3)))
	second := sampleTasks(tasks, 2, rand.New(rand.NewSource(3)))
	if len(first) != 2 || len(second) != 2 || first[0].ID != second[0].ID || first[1].ID != second[1].ID {
		t.Fatalf("expected deterministic mini-batch, got %+v vs %+v", first, second)
	}
	full := sampleTasks(tasks, 10, rand.New(rand.NewSource(3)))
	if len(full) != len(tasks) {
		t.Fatalf("expected full split when size exceeds tasks, got %d", len(full))
	}
}

func TestSubsetAggregateUsesTaskWeights(t *testing.T) {
	scores := map[string]float64{"t1": 1.0, "t2": 0.0}
	tasks := []TaskCase{{ID: "t1", Weight: 3}, {ID: "t2", Weight: 1}}
	if got := subsetAggregate(scores, tasks); got != 0.75 {
		t.Fatalf("expected weighted aggregate 0.75, got %f", got)
	}
}

func TestDefaultStrategyIsPareto(t *testing.T) {
	root := writeFixture(t)
	exp, err := LoadFile(filepath.Join(root, "experiments", "research-evolve.yaml"))
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if exp.Search.Strategy != strategyPareto {
		t.Fatalf("expected default strategy pareto, got %q", exp.Search.Strategy)
	}
}

func TestValidateRejectsUnknownStrategy(t *testing.T) {
	root := writeFixture(t)
	expPath := filepath.Join(root, "experiments", "research-evolve.yaml")
	data, err := os.ReadFile(expPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), "search:\n", "search:\n  strategy: beam\n", 1)
	writeFile(t, expPath, updated)
	_, err = LoadFile(expPath)
	if err == nil || !strings.Contains(err.Error(), "search.strategy must be") {
		t.Fatalf("expected unknown strategy rejection, got %v", err)
	}
}

func TestParetoStrategyFullPathWithTestSplit(t *testing.T) {
	root := writeFixture(t)
	expPath := filepath.Join(root, "experiments", "research-evolve.yaml")
	data, err := os.ReadFile(expPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), "search:\n  iterations: 1\n", "search:\n  strategy: pareto\n  iterations: -1\n", 1)
	writeFile(t, expPath, updated)
	result, err := Run(context.Background(), expPath, RunOptions{RunTest: true})
	if err != nil {
		t.Fatalf("Run pareto failed: %v", err)
	}
	if result.BestID != "baseline" {
		t.Fatalf("expected baseline best with no iterations, got %q", result.BestID)
	}
	report, err := os.ReadFile(result.ReportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(report), "- strategy: `pareto`") {
		t.Fatalf("report missing pareto strategy:\n%s", string(report))
	}
	if !strings.Contains(string(report), "### test") {
		t.Fatalf("report missing test metrics:\n%s", string(report))
	}
}

func TestBuildDigestIncludesReflectionAndRejectedBuffer(t *testing.T) {
	ctrl := &controller{
		exp: &Experiment{
			Objective: ObjectiveSpec{Metric: "evaluation.score", Direction: "maximize"},
			Target:    TargetSpec{Editable: []string{"instructions.system"}},
		},
		trainByID: map[string]TaskCase{
			"train-1": {ID: "train-1", Input: "what is jeju?"},
		},
		history: []historyItem{
			{Candidate: "candidate-001-01", Accepted: false, Reason: "minibatch gate", Proposal: &Proposal{Hypothesis: "be concise"}},
		},
	}
	cand := &candidate{
		ID:           "candidate-1",
		ManifestPath: filepath.Join(t.TempDir(), "missing.yaml"),
		Results: map[string]*SplitResult{
			"train": {
				Split: "train",
				Trials: []TrialResult{{
					TaskID: "train-1",
					Final:  "the island",
					Evaluation: evaluate.Result{
						Score: 0.25,
						Evaluators: []evaluate.EvaluatorResult{{
							Name:    "judge",
							Results: []evaluate.RuleResult{{Rule: "commandJudge", Message: "em=0 f1=0.25"}},
						}},
					},
				}},
			},
		},
	}
	digest := ctrl.buildDigest(1, cand)
	reflection, ok := digest["reflection"].([]map[string]any)
	if !ok || len(reflection) != 1 {
		t.Fatalf("expected one reflection entry, got %#v", digest["reflection"])
	}
	if reflection[0]["input"] != "what is jeju?" {
		t.Fatalf("reflection missing rendered task input: %#v", reflection[0])
	}
	feedback, ok := reflection[0]["feedback"].([]string)
	if !ok || len(feedback) != 1 || !strings.Contains(feedback[0], "em=0 f1=0.25") {
		t.Fatalf("reflection missing evaluator feedback: %#v", reflection[0])
	}
	rejected, ok := digest["rejected_proposals"].([]map[string]any)
	if !ok || len(rejected) != 1 || rejected[0]["hypothesis"] != "be concise" {
		t.Fatalf("expected rejected proposal buffer, got %#v", digest["rejected_proposals"])
	}
}
