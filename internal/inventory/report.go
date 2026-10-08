package inventory

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Formats accepted by Write.
var Formats = []string{"text", "json", "csv"}

// Write renders records in the given format.
func Write(w io.Writer, format string, recs []Record) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(recs)
	case "csv":
		return writeCSV(w, recs)
	case "text":
		writeText(w, recs)
		return nil
	}
	return fmt.Errorf("unknown format %q (use text, json or csv)", format)
}

var csvHeader = []string{
	"project", "folder", "pipeline", "id", "revision", "variables", "secret_variables", "variable_groups", "artifacts",
	"triggers", "schedules", "stage", "rank", "conditions", "stage_variables", "pre_approvals", "post_approvals",
	"retention_days", "retention_releases", "retain_build", "jobs", "pools", "demands", "tasks",
}

// writeCSV writes one row per pipeline stage (a pipeline without stages gets one row).
func writeCSV(w io.Writer, recs []Record) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	for _, r := range recs {
		secrets := 0
		for _, v := range r.Variables {
			if v.Secret {
				secrets++
			}
		}
		var groups, arts []string
		for _, g := range r.VariableGroups {
			groups = append(groups, fmt.Sprint(g))
		}
		for _, a := range r.Artifacts {
			arts = append(arts, a.Alias+" ("+a.Type+": "+a.Source+")")
		}
		base := []string{r.Project, r.Folder, r.Name, fmt.Sprint(r.ID), fmt.Sprint(r.Revision), fmt.Sprint(len(r.Variables)), fmt.Sprint(secrets),
			strings.Join(groups, ";"), strings.Join(arts, ";"), strings.Join(r.Triggers, ";"), strings.Join(r.Schedules, ";")}
		stages := r.Stages
		if len(stages) == 0 {
			stages = []Stage{{}}
		}
		for _, s := range stages {
			var jobs, pools, demands, tasks []string
			for _, j := range s.Jobs {
				jobs = append(jobs, j.Name)
				if j.Pool != "" {
					pools = append(pools, j.Pool)
				}
				demands = append(demands, j.Demands...)
				for _, t := range j.Tasks {
					tasks = append(tasks, t.Name+"@"+t.Version)
				}
			}
			days, rel, rb := "", "", ""
			if s.Retention != nil {
				days, rel = fmt.Sprint(s.Retention.Days), fmt.Sprint(s.Retention.Releases)
				if s.Retention.RetainBuild != nil {
					rb = fmt.Sprint(*s.Retention.RetainBuild)
				}
			}
			row := append(append([]string{}, base...), s.Name, fmt.Sprint(s.Rank), strings.Join(s.Conditions, ";"), fmt.Sprint(len(s.Variables)), s.PreApprovals, s.PostApprovals,
				days, rel, rb, strings.Join(jobs, ";"), strings.Join(pools, ";"), strings.Join(demands, ";"), strings.Join(tasks, ";"))
			if err := cw.Write(row); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeText(w io.Writer, recs []Record) {
	for _, r := range recs {
		secrets := 0
		for _, v := range r.Variables {
			if v.Secret {
				secrets++
			}
		}
		fmt.Fprintf(w, "%s (id=%d, rev=%d)\n", r.Label(), r.ID, r.Revision)
		fmt.Fprintf(w, "  variables: %d (%d secret) | variable groups: %v\n", len(r.Variables), secrets, r.VariableGroups)
		for _, a := range r.Artifacts {
			fmt.Fprintf(w, "  artifact %s: %s %s primary=%t\n", a.Alias, a.Type, a.Source, a.Primary)
		}
		if len(r.Triggers) > 0 {
			fmt.Fprintf(w, "  triggers: %s\n", strings.Join(r.Triggers, ", "))
		}
		for _, s := range r.Schedules {
			fmt.Fprintf(w, "  schedule: %s\n", s)
		}
		for _, s := range r.Stages {
			fmt.Fprintf(w, "  stage %d. %s | pre: %s | post: %s", s.Rank, s.Name, s.PreApprovals, s.PostApprovals)
			if len(s.Conditions) > 0 {
				fmt.Fprintf(w, " | conditions: %s", strings.Join(s.Conditions, "; "))
			}
			if s.Retention != nil {
				fmt.Fprintf(w, " | retention: %d days, %d releases", s.Retention.Days, s.Retention.Releases)
				if s.Retention.RetainBuild != nil {
					fmt.Fprintf(w, ", retain build=%t", *s.Retention.RetainBuild)
				}
			}
			fmt.Fprintf(w, " | variables: %d\n", len(s.Variables))
			for _, j := range s.Jobs {
				fmt.Fprintf(w, "    job %s [%s] pool=%s timeout=%dmin demands=[%s]\n", j.Name, j.Type, j.Pool, j.TimeoutMin, strings.Join(j.Demands, "; "))
				for i, t := range j.Tasks {
					off := ""
					if !t.Enabled {
						off = " (disabled)"
					}
					fmt.Fprintf(w, "      %02d %s@%s%s\n", i+1, t.Name, t.Version, off)
				}
			}
		}
		fmt.Fprintln(w)
	}
}
