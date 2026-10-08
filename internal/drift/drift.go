// Package drift compares many pipelines' settings against one baseline and groups the
// differences into recurring patterns.
package drift

import (
	"sort"
	"strings"
)

// Missing is the value reported for a setting a pipeline does not have.
const Missing = "(missing)"

// Diff is one setting that differs from the baseline.
type Diff struct {
	Key      string `json:"key"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

// Pattern is one distinct difference shared by several pipelines.
type Pattern struct {
	Diff
	Pipelines []string `json:"pipelines"`
}

// Cluster is a set of pipelines that drift in exactly the same way.
type Cluster struct {
	Diffs     []Diff   `json:"diffs"`
	Pipelines []string `json:"pipelines"`
}

// Result is the outcome of Analyze.
type Result struct {
	Compared   int               `json:"compared"`
	Conforming []string          `json:"conforming"`
	Patterns   []Pattern         `json:"patterns"`
	Clusters   []Cluster         `json:"clusters"`
	PerPipe    map[string][]Diff `json:"perPipeline"`
	NoNorm     []string          `json:"noConsensus,omitempty"`
}

// Consensus derives a baseline from the most common value of every setting. A setting becomes part
// of the baseline only if at least minAgreement (0-1] of the pipelines agree on it (a pipeline that
// lacks the setting counts as agreeing on "missing"). Settings without such agreement are returned
// in varied and are not judged.
func Consensus(all map[string]map[string]string, minAgreement float64) (baseline map[string]string, varied []string) {
	keys := map[string]bool{}
	for _, s := range all {
		for k := range s {
			keys[k] = true
		}
	}
	baseline = map[string]string{}
	n := float64(len(all))
	for k := range keys {
		counts := map[string]int{}
		for _, s := range all {
			v, ok := s[k]
			if !ok {
				v = Missing
			}
			counts[v]++
		}
		best, bestN := "", 0
		for v, c := range counts {
			if c > bestN || (c == bestN && v < best) {
				best, bestN = v, c
			}
		}
		if float64(bestN)/n < minAgreement {
			varied = append(varied, k)
			continue
		}
		if best != Missing {
			baseline[k] = best
		}
	}
	sort.Strings(varied)
	return baseline, varied
}

// Analyze compares every pipeline to baseline. Settings in skip are ignored.
func Analyze(baseline map[string]string, all map[string]map[string]string, skip []string) Result {
	skipped := map[string]bool{}
	for _, k := range skip {
		skipped[k] = true
	}
	res := Result{Compared: len(all), PerPipe: map[string][]Diff{}, NoNorm: skip}
	patterns := map[Diff][]string{}
	clusters := map[string]*Cluster{}
	for name, settings := range all {
		var diffs []Diff
		keys := map[string]bool{}
		for k := range baseline {
			keys[k] = true
		}
		for k := range settings {
			keys[k] = true
		}
		for k := range keys {
			if skipped[k] {
				continue
			}
			want, hasWant := baseline[k]
			got, hasGot := settings[k]
			if !hasWant {
				want = Missing
			}
			if !hasGot {
				got = Missing
			}
			if want != got {
				diffs = append(diffs, Diff{k, want, got})
			}
		}
		sort.Slice(diffs, func(i, j int) bool { return diffs[i].Key < diffs[j].Key })
		if len(diffs) == 0 {
			res.Conforming = append(res.Conforming, name)
			continue
		}
		res.PerPipe[name] = diffs
		var sig strings.Builder
		for _, d := range diffs {
			patterns[d] = append(patterns[d], name)
			sig.WriteString(d.Key + "\x00" + d.Expected + "\x00" + d.Actual + "\x01")
		}
		c := clusters[sig.String()]
		if c == nil {
			c = &Cluster{Diffs: diffs}
			clusters[sig.String()] = c
		}
		c.Pipelines = append(c.Pipelines, name)
	}
	sort.Strings(res.Conforming)
	for d, p := range patterns {
		sort.Strings(p)
		res.Patterns = append(res.Patterns, Pattern{d, p})
	}
	sort.Slice(res.Patterns, func(i, j int) bool {
		a, b := res.Patterns[i], res.Patterns[j]
		if len(a.Pipelines) != len(b.Pipelines) {
			return len(a.Pipelines) > len(b.Pipelines)
		}
		return a.Key < b.Key
	})
	for _, c := range clusters {
		sort.Strings(c.Pipelines)
		res.Clusters = append(res.Clusters, *c)
	}
	sort.Slice(res.Clusters, func(i, j int) bool {
		a, b := res.Clusters[i], res.Clusters[j]
		if len(a.Pipelines) != len(b.Pipelines) {
			return len(a.Pipelines) > len(b.Pipelines)
		}
		return a.Pipelines[0] < b.Pipelines[0]
	})
	return res
}
