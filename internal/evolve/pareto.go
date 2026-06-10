package evolve

import (
	"context"
	"fmt"
	"math/rand"
	"path/filepath"
	"sort"
	"strings"
)

// searchPareto is the GEPA-style search strategy: instead of greedy
// hill-climbing on a single lineage, it maintains a pool of candidates and an
// instance-wise Pareto frontier over train tasks. A candidate survives if it
// is the best known config on at least one train task; parents are sampled
// from the pool proportionally to how many tasks they win. A cheap train
// mini-batch gate filters clearly worse proposals before full evaluation.
// The final best is the pool candidate with the strongest selection metric.
func (c *controller) searchPareto(ctx context.Context, baseline *candidate, trainBaseline, selectionBaseline *SplitResult, iterations int, allCandidates []*candidate) (*candidate, []*candidate, error) {
	metric := c.exp.Objective.Metric
	direction := c.exp.Objective.Direction

	baseScores, err := perTaskScores(metric, trainBaseline.Trials)
	if err != nil {
		return nil, nil, err
	}
	pool := []*poolEntry{{
		cand:            baseline,
		taskScores:      baseScores,
		trainMetric:     trainBaseline.Metrics[metric],
		selectionMetric: selectionBaseline.Metrics[metric],
	}}
	best := baseline
	bestMetric := selectionBaseline.Metrics[metric]
	rng := rand.New(rand.NewSource(c.exp.Search.Seed))
	noNew := 0

	for iter := 1; iter <= iterations; iter++ {
		if c.budgetExhausted() {
			break
		}
		updatePoolWins(pool, direction)
		parent := samplePoolParent(pool, rng)
		c.poolDigest = poolSummary(pool, best.ID)
		c.events.Write("pareto.parent_selected", map[string]any{
			"iteration": iter,
			"parent":    parent.cand.ID,
			"wins":      parent.wins,
			"pool_size": len(pool),
		})
		proposals, err := c.propose(ctx, iter, parent.cand)
		c.poolDigest = nil
		if err != nil {
			return nil, nil, err
		}
		minibatch := sampleTasks(c.train, c.exp.Search.Minibatch, rng)
		added := false
		for i, proposal := range proposals {
			cand, err := c.createCandidate(iter, i+1, parent.cand, proposal)
			if err != nil {
				c.history = append(c.history, historyItem{Iteration: iter, Candidate: proposal.ID, Accepted: false, Reason: err.Error(), Proposal: &proposal})
				continue
			}
			allCandidates = append(allCandidates, cand)
			minibatchResult, err := c.evaluateCandidate(ctx, cand, "minibatch", minibatch, nil)
			if err != nil {
				return nil, nil, err
			}
			cand.Results["minibatch"] = minibatchResult
			parentMinibatch := subsetAggregate(parent.taskScores, minibatch)
			candMinibatch := minibatchResult.Metrics[metric]
			if isWorse(candMinibatch, parentMinibatch, direction) {
				c.reject(cand, fmt.Sprintf("minibatch gate: %.4f is worse than parent %.4f", candMinibatch, parentMinibatch))
				continue
			}
			trainResult, err := c.evaluateCandidate(ctx, cand, "train", c.train, trainBaseline.Metrics)
			if err != nil {
				return nil, nil, err
			}
			cand.Results["train"] = trainResult
			if !trainResult.GuardPass {
				c.reject(cand, "train guards failed: "+strings.Join(trainResult.GuardReasons, "; "))
				continue
			}
			selectionResult, err := c.evaluateCandidate(ctx, cand, "selection", c.selection, selectionBaseline.Metrics)
			if err != nil {
				return nil, nil, err
			}
			cand.Results["selection"] = selectionResult
			if !selectionResult.GuardPass {
				c.reject(cand, "selection guards failed: "+strings.Join(selectionResult.GuardReasons, "; "))
				continue
			}
			scores, err := perTaskScores(metric, trainResult.Trials)
			if err != nil {
				return nil, nil, err
			}
			entry := &poolEntry{
				cand:            cand,
				taskScores:      scores,
				trainMetric:     trainResult.Metrics[metric],
				selectionMetric: selectionResult.Metrics[metric],
			}
			pool = append(pool, entry)
			updatePoolWins(pool, direction)
			if entry.wins == 0 && !isBetter(entry.trainMetric, parent.trainMetric, direction) {
				pool = pool[:len(pool)-1]
				c.reject(cand, "pareto: no train task win and no train aggregate gain over parent")
				continue
			}
			added = true
			c.history = append(c.history, historyItem{
				Iteration: iter,
				Candidate: cand.ID,
				Accepted:  true,
				Reason:    fmt.Sprintf("pareto pool: wins=%d", entry.wins),
				Metrics:   selectionResult.Metrics,
				Proposal:  cand.Proposal,
			})
			c.events.Write("candidate.pool_added", map[string]any{
				"candidate": cand.ID,
				"wins":      entry.wins,
				"train":     entry.trainMetric,
				"selection": entry.selectionMetric,
			})
			if isImproved(entry.selectionMetric, bestMetric, direction, c.exp.Objective.MinDelta) {
				best = cand
				bestMetric = entry.selectionMetric
				c.events.Write("candidate.accepted", map[string]any{"candidate": cand.ID, "metric": entry.selectionMetric})
			}
			if err := writeJSON(filepath.Join(cand.Dir, "results.json"), cand); err != nil {
				return nil, nil, err
			}
		}
		pool = prunePool(pool, c.exp.Search.Pool, direction, best.ID)
		if added {
			noNew = 0
		} else {
			noNew++
		}
		if noNew >= 3 {
			c.events.Write("search.stopped", map[string]any{"reason": "no pool additions"})
			break
		}
	}
	return best, allCandidates, nil
}

type poolEntry struct {
	cand            *candidate
	taskScores      map[string]float64
	trainMetric     float64
	selectionMetric float64
	wins            int
}

const metricEpsilon = 1e-9

func isBetter(candidate, incumbent float64, direction string) bool {
	if direction == "minimize" {
		return candidate < incumbent-metricEpsilon
	}
	return candidate > incumbent+metricEpsilon
}

func isWorse(candidate, incumbent float64, direction string) bool {
	return isBetter(incumbent, candidate, direction)
}

// perTaskScores averages the objective metric per train task across trials.
func perTaskScores(metric string, trials []TrialResult) (map[string]float64, error) {
	byTask := map[string][]TrialResult{}
	for _, trial := range trials {
		byTask[trial.TaskID] = append(byTask[trial.TaskID], trial)
	}
	scores := make(map[string]float64, len(byTask))
	for taskID, taskTrials := range byTask {
		value, err := metricValue(metric, taskTrials)
		if err != nil {
			return nil, err
		}
		scores[taskID] = value
	}
	return scores, nil
}

// updatePoolWins recomputes, for every pool entry, the number of train tasks
// on which it matches the pool-wide best score (the instance-wise Pareto
// frontier membership count).
func updatePoolWins(pool []*poolEntry, direction string) {
	for _, entry := range pool {
		entry.wins = 0
	}
	taskIDs := map[string]bool{}
	for _, entry := range pool {
		for taskID := range entry.taskScores {
			taskIDs[taskID] = true
		}
	}
	for taskID := range taskIDs {
		first := true
		var bestScore float64
		for _, entry := range pool {
			score, ok := entry.taskScores[taskID]
			if !ok {
				continue
			}
			if first || isBetter(score, bestScore, direction) {
				bestScore = score
				first = false
			}
		}
		if first {
			continue
		}
		for _, entry := range pool {
			score, ok := entry.taskScores[taskID]
			if ok && !isBetter(bestScore, score, direction) {
				entry.wins++
			}
		}
	}
}

// samplePoolParent picks the next parent with probability proportional to its
// frontier wins, so lineages that are best somewhere keep getting explored.
func samplePoolParent(pool []*poolEntry, rng *rand.Rand) *poolEntry {
	total := 0
	for _, entry := range pool {
		total += entry.wins
	}
	if total == 0 {
		return pool[rng.Intn(len(pool))]
	}
	pick := rng.Intn(total)
	for _, entry := range pool {
		pick -= entry.wins
		if pick < 0 {
			return entry
		}
	}
	return pool[len(pool)-1]
}

// prunePool keeps frontier members (wins > 0) plus the current best, capped
// at maxSize by train aggregate.
func prunePool(pool []*poolEntry, maxSize int, direction string, bestID string) []*poolEntry {
	updatePoolWins(pool, direction)
	kept := make([]*poolEntry, 0, len(pool))
	for _, entry := range pool {
		if entry.wins > 0 || entry.cand.ID == bestID {
			kept = append(kept, entry)
		}
	}
	if len(kept) <= maxSize {
		return kept
	}
	sort.SliceStable(kept, func(i, j int) bool {
		return isBetter(kept[i].trainMetric, kept[j].trainMetric, direction)
	})
	trimmed := kept[:maxSize]
	hasBest := false
	for _, entry := range trimmed {
		if entry.cand.ID == bestID {
			hasBest = true
			break
		}
	}
	if !hasBest {
		for _, entry := range kept[maxSize:] {
			if entry.cand.ID == bestID {
				trimmed[len(trimmed)-1] = entry
				break
			}
		}
	}
	return trimmed
}

// sampleTasks draws a deterministic mini-batch of train tasks for the cascade
// gate. If size covers the whole split, the full split is used.
func sampleTasks(tasks []TaskCase, size int, rng *rand.Rand) []TaskCase {
	if size >= len(tasks) {
		return tasks
	}
	picked := make([]TaskCase, 0, size)
	for _, idx := range rng.Perm(len(tasks))[:size] {
		picked = append(picked, tasks[idx])
	}
	sort.Slice(picked, func(i, j int) bool { return picked[i].ID < picked[j].ID })
	return picked
}

// subsetAggregate computes the parent's weighted aggregate over the
// mini-batch tasks from its stored full-train per-task scores, so the gate
// comparison costs zero extra runs.
func subsetAggregate(taskScores map[string]float64, tasks []TaskCase) float64 {
	total := 0.0
	weightTotal := 0.0
	for _, task := range tasks {
		score, ok := taskScores[task.ID]
		if !ok {
			continue
		}
		weight := task.Weight
		if weight <= 0 {
			weight = 1
		}
		total += score * weight
		weightTotal += weight
	}
	if weightTotal == 0 {
		return 0
	}
	return total / weightTotal
}

func poolSummary(pool []*poolEntry, bestID string) []map[string]any {
	out := make([]map[string]any, 0, len(pool))
	for _, entry := range pool {
		out = append(out, map[string]any{
			"candidate":    entry.cand.ID,
			"train_metric": entry.trainMetric,
			"wins":         entry.wins,
			"is_best":      entry.cand.ID == bestID,
		})
	}
	return out
}
