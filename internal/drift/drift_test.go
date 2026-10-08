package drift

import "testing"

func fleet() map[string]map[string]string {
	return map[string]map[string]string{
		"a": {"pool": "P1", "retention": "30", "task": "x@1"},
		"b": {"pool": "P1", "retention": "30", "task": "x@1"},
		"c": {"pool": "P1", "retention": "30", "task": "x@1"},
		"d": {"pool": "P2", "retention": "30", "task": "x@1"},
		"e": {"pool": "P2", "retention": "30"},
		"f": {"pool": "P1", "retention": "90", "task": "x@1", "extra": "y"},
	}
}

func TestConsensusBaseline(t *testing.T) {
	base, varied := Consensus(fleet(), 0.5)
	if base["pool"] != "P1" || base["retention"] != "30" || base["task"] != "x@1" {
		t.Fatalf("baseline = %v", base)
	}
	if _, ok := base["extra"]; ok {
		t.Fatal("setting only one pipeline has must not enter the baseline")
	}
	if len(varied) != 0 {
		t.Fatalf("varied = %v", varied)
	}
	if _, varied := Consensus(fleet(), 0.9); len(varied) == 0 {
		t.Fatal("expected settings without 90% agreement")
	}
}

func TestAnalyzeGroupsPatterns(t *testing.T) {
	base, _ := Consensus(fleet(), 0.5)
	res := Analyze(base, fleet(), nil)
	if len(res.Conforming) != 3 || res.Compared != 6 {
		t.Fatalf("conforming = %v", res.Conforming)
	}
	top := res.Patterns[0]
	if top.Key != "pool" || top.Actual != "P2" || len(top.Pipelines) != 2 {
		t.Fatalf("top pattern = %+v", top)
	}
	var extra bool
	for _, p := range res.Patterns {
		if p.Key == "extra" && p.Expected == Missing && p.Actual == "y" {
			extra = true
		}
	}
	if !extra {
		t.Fatal("extra setting should be reported against a missing baseline")
	}
	if len(res.PerPipe["e"]) != 2 {
		t.Fatalf("e diffs = %v", res.PerPipe["e"])
	}
}

func TestClustersGroupIdenticalDrift(t *testing.T) {
	all := map[string]map[string]string{
		"a": {"k": "1"}, "b": {"k": "1"}, "c": {"k": "1"}, "d": {"k": "2"}, "e": {"k": "2"},
	}
	res := Analyze(map[string]string{"k": "1"}, all, nil)
	if len(res.Clusters) != 1 || len(res.Clusters[0].Pipelines) != 2 {
		t.Fatalf("clusters = %+v", res.Clusters)
	}
}
