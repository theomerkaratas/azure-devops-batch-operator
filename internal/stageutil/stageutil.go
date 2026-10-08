// Package stageutil holds the raw release-definition stage (environment) manipulation shared by
// the stage-management commands. Stages are plain JSON maps; ranks are kept 1..n in array order
// and dependencies are the "environmentState" entries of each stage's conditions.
package stageutil

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Map is a raw JSON object.
type Map = map[string]interface{}

const releaseStarted = "ReleaseStarted"

// Clone deep-copies a JSON value.
func Clone(value interface{}) interface{} {
	data, _ := json.Marshal(value)
	var out interface{}
	_ = json.Unmarshal(data, &out)
	return out
}

// Name returns the stage name.
func Name(stage Map) string { n, _ := stage["name"].(string); return n }

// Stages returns the definition's stages sorted by rank (stable for equal/missing ranks).
func Stages(raw Map) []Map {
	items, _ := raw["environments"].([]interface{})
	out := make([]Map, 0, len(items))
	for _, item := range items {
		if m, _ := item.(Map); m != nil {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

// Index returns the position of the named stage (case-insensitive) or -1.
func Index(stages []Map, name string) int {
	for i, s := range stages {
		if strings.EqualFold(Name(s), name) {
			return i
		}
	}
	return -1
}

// Save renumbers ranks 1..n in slice order and writes the stages back to the definition.
func Save(raw Map, stages []Map) {
	items := make([]interface{}, len(stages))
	for i, s := range stages {
		s["rank"] = float64(i + 1)
		items[i] = s
	}
	raw["environments"] = items
}

func rank(stage Map) float64 { r, _ := stage["rank"].(float64); return r }

func isStageCondition(c Map) bool {
	switch t := c["conditionType"].(type) {
	case float64:
		return t == 2
	case string:
		return strings.EqualFold(t, "environmentState") || t == "2"
	}
	return false
}

// Dependencies returns the names of stages the given stage waits for.
func Dependencies(stage Map) []string {
	var out []string
	conds, _ := stage["conditions"].([]interface{})
	for _, c := range conds {
		if m, _ := c.(Map); m != nil && isStageCondition(m) {
			n, _ := m["name"].(string)
			out = append(out, n)
		}
	}
	return out
}

// SetDependencies replaces the stage's triggers with the named stages, or with the release start
// when none are given. Non-stage conditions (artifact triggers, etc.) are dropped with the rest.
func SetDependencies(stage Map, names []string) {
	if len(names) == 0 {
		stage["conditions"] = []interface{}{Map{"name": releaseStarted, "conditionType": "event", "value": ""}}
		return
	}
	conds := make([]interface{}, len(names))
	for i, n := range names {
		conds[i] = Map{"name": n, "conditionType": "environmentState", "value": "4"}
	}
	stage["conditions"] = conds
}

// PruneDependencies drops stage conditions that reference stages not in known (a set of lower-case
// names) and falls back to the release start when nothing is left.
func PruneDependencies(stage Map, known map[string]bool) {
	var keep []string
	for _, d := range Dependencies(stage) {
		if known[strings.ToLower(d)] {
			keep = append(keep, d)
		}
	}
	if len(keep) != len(Dependencies(stage)) || len(keep) == 0 {
		SetDependencies(stage, keep)
	}
}

// RenameDependency rewrites conditions that reference oldName.
func RenameDependency(stages []Map, oldName, newName string) {
	for _, s := range stages {
		conds, _ := s["conditions"].([]interface{})
		for _, c := range conds {
			if m, _ := c.(Map); m != nil && isStageCondition(m) {
				if n, _ := m["name"].(string); strings.EqualFold(n, oldName) {
					m["name"] = newName
				}
			}
		}
	}
}

// Validate checks that stage names are unique, every dependency exists and precedes its
// dependent in rank order (which also rules out cycles).
func Validate(stages []Map) error {
	pos := map[string]int{}
	for i, s := range stages {
		n := strings.ToLower(Name(s))
		if n == "" {
			return fmt.Errorf("stage %d has no name", i+1)
		}
		if _, dup := pos[n]; dup {
			return fmt.Errorf("duplicate stage name %q", Name(s))
		}
		pos[n] = i
	}
	for i, s := range stages {
		for _, d := range Dependencies(s) {
			j, ok := pos[strings.ToLower(d)]
			switch {
			case !ok:
				return fmt.Errorf("stage %q depends on missing stage %q", Name(s), d)
			case j >= i:
				return fmt.Errorf("stage %q must come after its dependency %q", Name(s), d)
			}
		}
	}
	return nil
}

// Dependents lists stages that wait for the named stage.
func Dependents(stages []Map, name string) []Map {
	var out []Map
	for _, s := range stages {
		for _, d := range Dependencies(s) {
			if strings.EqualFold(d, name) {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// NewStage returns a minimal stage with automated approvals and default retention.
func NewStage(name string, owner interface{}) Map {
	auto := func() Map {
		return Map{"approvals": []interface{}{Map{"rank": 1.0, "isAutomated": true, "isNotificationOn": false}}}
	}
	stage := Map{
		"name": name, "variables": Map{}, "variableGroups": []interface{}{}, "deployPhases": []interface{}{},
		"preDeployApprovals": auto(), "postDeployApprovals": auto(), "schedules": []interface{}{},
		"retentionPolicy": Map{"daysToKeep": 30.0, "releasesToKeep": 3.0, "retainBuild": true},
	}
	if owner != nil {
		stage["owner"] = Clone(owner)
	}
	return stage
}

// ResetIDs removes server-assigned IDs from a stage, its jobs and its approvals so the server
// assigns fresh ones.
func ResetIDs(stage Map) {
	delete(stage, "id")
	stripIDs(stage["deployPhases"])
	for _, key := range []string{"preDeployApprovals", "postDeployApprovals"} {
		block, _ := stage[key].(Map)
		stripIDs(block["approvals"])
	}
}

func stripIDs(value interface{}) {
	items, _ := value.([]interface{})
	for _, item := range items {
		if m, _ := item.(Map); m != nil {
			delete(m, "id")
		}
	}
}

// RebindIDs copies the IDs of dest's stage, same-named jobs and positionally matching approvals
// onto src so that replacing dest with src updates it in place.
func RebindIDs(src, dest Map) {
	ResetIDs(src)
	if id, ok := dest["id"]; ok {
		src["id"] = id
	}
	destJobs := map[string]Map{}
	for _, item := range asList(dest["deployPhases"]) {
		if m, _ := item.(Map); m != nil {
			destJobs[Name(m)] = m
		}
	}
	for _, item := range asList(src["deployPhases"]) {
		if m, _ := item.(Map); m != nil {
			if d := destJobs[Name(m)]; d != nil {
				if id, ok := d["id"]; ok {
					m["id"] = id
				}
			}
		}
	}
	for _, key := range []string{"preDeployApprovals", "postDeployApprovals"} {
		sb, _ := src[key].(Map)
		db, _ := dest[key].(Map)
		da := asList(db["approvals"])
		for i, item := range asList(sb["approvals"]) {
			a, _ := item.(Map)
			if d, _ := indexMap(da, i); a != nil && d != nil {
				if id, ok := d["id"]; ok {
					a["id"] = id
				}
			}
		}
	}
}

func asList(v interface{}) []interface{} { l, _ := v.([]interface{}); return l }

func indexMap(l []interface{}, i int) (Map, bool) {
	if i < 0 || i >= len(l) {
		return nil, false
	}
	m, ok := l[i].(Map)
	return m, ok
}

// Insert places stage at position index (0-based, clamped) in stages.
func Insert(stages []Map, stage Map, index int) []Map {
	if index < 0 || index > len(stages) {
		index = len(stages)
	}
	stages = append(stages, nil)
	copy(stages[index+1:], stages[index:])
	stages[index] = stage
	return stages
}

// Remove deletes the stage at index.
func Remove(stages []Map, index int) []Map {
	return append(stages[:index:index], stages[index+1:]...)
}

// Trigger kinds reported by TriggerOf.
const (
	TriggerRelease = "after-release"
	TriggerStages  = "after-stages"
	TriggerManual  = "manual"
	TriggerOther   = "other"
)

// TriggerOf classifies how a stage starts and returns the stages it waits for.
func TriggerOf(stage Map) (string, []string) {
	conds, _ := stage["conditions"].([]interface{})
	if deps := Dependencies(stage); len(deps) > 0 {
		return TriggerStages, deps
	}
	if len(conds) == 0 {
		return TriggerManual, nil
	}
	for _, c := range conds {
		m, _ := c.(Map)
		if n, _ := m["name"].(string); strings.EqualFold(n, releaseStarted) {
			return TriggerRelease, nil
		}
	}
	return TriggerOther, nil
}

// SetManual makes the stage start only when deployed by hand.
func SetManual(stage Map) { stage["conditions"] = []interface{}{} }

// SameSet reports whether a and b hold the same names, ignoring case and order.
func SameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, n := range a {
		seen[strings.ToLower(n)]++
	}
	for _, n := range b {
		seen[strings.ToLower(n)]--
	}
	for _, v := range seen {
		if v != 0 {
			return false
		}
	}
	return true
}
