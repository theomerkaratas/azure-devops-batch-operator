// Command update-pipeline-schedule bulk-sets or removes the scheduled triggers of Azure DevOps
// release pipelines under a given path.
package updatepipelineschedule

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  # Pause all schedules under a folder (removes the schedule triggers):
  update-pipeline-schedule 'Example.Project\TEST\CONFIG' --action remove --dry-run

  # Move every schedule to 02:30 on weekdays:
  update-pipeline-schedule 'Example.Project\TEST\CONFIG' --action set --time 02:30 --days weekdays

Arguments:
  target      Required. A folder (covers all pipelines under it) or the full path of one pipeline.

  --action    Required. set | remove
                set    -> Updates existing schedules (or adds one if a pipeline has none).
                remove -> Removes all schedule triggers. Azure DevOps has no "disabled" flag for
                          release schedules, so removal is the only way to pause them; use
                          show-pipeline-schedule beforehand to record what you removed.

  --time      With set: HH:MM start time (24h, in the schedule's time zone).
  --days      With set: comma-separated days (mon,tue,wed,thu,fri,sat,sun) or 'all' / 'weekdays'.
                Default: keep the existing days (new schedules: all).
  --timezone  With set: time zone id, e.g. 'Turkey Standard Time'.
                Default: keep the existing zone (new schedules: UTC).

  --filter    Optional. Only pipelines whose name contains this text (case-insensitive).
  --level     Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run   Lists the changes without saving anything.
  -y, --yes   Skips the confirmation prompt.
`

var dayBits = map[string]int{"mon": 1, "tue": 2, "wed": 4, "thu": 8, "fri": 16, "sat": 32, "sun": 64}

func parseDays(s string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return 0, nil
	case "all":
		return 127, nil
	case "weekdays":
		return 31, nil
	}
	mask := 0
	for _, d := range strings.Split(s, ",") {
		key := strings.ToLower(strings.TrimSpace(d))
		if len(key) > 3 {
			key = key[:3]
		}
		bit, ok := dayBits[key]
		if !ok {
			return 0, fmt.Errorf("unknown day %q", d)
		}
		mask |= bit
	}
	return mask, nil
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("update-pipeline-schedule", flag.ExitOnError)
	action := fs.String("action", "", "set or remove")
	timeStr := fs.String("time", "", "With set: HH:MM start time")
	days := fs.String("days", "", "With set: comma-separated days, 'all' or 'weekdays'")
	tz := fs.String("timezone", "", "With set: time zone id")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level to use: read, read-write, manage (default: config default_token or read-write)")
	dryRun := fs.Bool("dry-run", false, "Lists the changes without saving anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")

	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Bulk-sets or removes the scheduled triggers of release pipelines under a path.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: update-pipeline-schedule <target> --action set|remove [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"action": true, "time": true, "days": true, "timezone": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if *action != "set" && *action != "remove" {
		fmt.Fprintln(os.Stderr, "Error: --action must be one of: set, remove")
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}

	var hours, minutes int
	var dayMask int
	if *action == "set" {
		if _, err := fmt.Sscanf(*timeStr, "%d:%d", &hours, &minutes); err != nil || hours < 0 || hours > 23 || minutes < 0 || minutes > 59 {
			fmt.Fprintln(os.Stderr, "Error: --time HH:MM is required with --action set.")
			os.Exit(2)
		}
		var err error
		if dayMask, err = parseDays(*days); err != nil {
			fmt.Fprintln(os.Stderr, "Error:", err)
			os.Exit(2)
		}
	}

	opts := batchupdate.Options{Target: fs.Arg(0), Filter: *filter, Level: *level, DryRun: *dryRun, AutoYes: *yes}
	mutate := func(_ azuredevops.Config, _ string, raw map[string]interface{}) []string {
		if *action == "remove" {
			return removeSchedules(raw)
		}
		return setSchedules(raw, hours, minutes, dayMask, *tz)
	}
	if err := batchupdate.Run(opts, "Automated schedule update", mutate); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func isScheduleTrigger(t map[string]interface{}) bool {
	tt, _ := t["triggerType"].(string)
	return strings.Contains(strings.ToLower(tt), "schedule")
}

// scheduleMaps returns the schedule objects of a trigger, which may use either "schedules" or "schedule".
func scheduleMaps(t map[string]interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	if list, ok := t["schedules"].([]interface{}); ok {
		for _, s := range list {
			if m, ok := s.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
	}
	if m, ok := t["schedule"].(map[string]interface{}); ok {
		out = append(out, m)
	}
	return out
}

func describe(s map[string]interface{}) string {
	return fmt.Sprintf("%02d:%02d days=%v tz=%v", batchupdate.Int(s, "startHours"), batchupdate.Int(s, "startMinutes"), s["daysToRelease"], s["timeZoneId"])
}

func removeSchedules(raw map[string]interface{}) []string {
	triggers, _ := raw["triggers"].([]interface{})
	var kept []interface{}
	var lines []string
	for _, t := range triggers {
		tm, ok := t.(map[string]interface{})
		if !ok || !isScheduleTrigger(tm) {
			kept = append(kept, t)
			continue
		}
		for _, s := range scheduleMaps(tm) {
			lines = append(lines, "- remove schedule "+describe(s))
		}
		if len(scheduleMaps(tm)) == 0 {
			lines = append(lines, "- remove empty schedule trigger")
		}
	}
	if len(lines) > 0 {
		if kept == nil {
			kept = []interface{}{}
		}
		raw["triggers"] = kept
	}
	return lines
}

func setSchedules(raw map[string]interface{}, hours, minutes, dayMask int, tz string) []string {
	triggers, _ := raw["triggers"].([]interface{})
	var lines []string
	found := false

	for _, t := range triggers {
		tm, ok := t.(map[string]interface{})
		if !ok || !isScheduleTrigger(tm) {
			continue
		}
		for _, s := range scheduleMaps(tm) {
			found = true
			before := describe(s)
			s["startHours"], s["startMinutes"] = hours, minutes
			if dayMask != 0 {
				if _, isString := s["daysToRelease"].(string); isString {
					s["daysToRelease"] = fmt.Sprint(dayMask)
				} else {
					s["daysToRelease"] = dayMask
				}
			}
			if tz != "" {
				s["timeZoneId"] = tz
			}
			if after := describe(s); after != before {
				lines = append(lines, fmt.Sprintf("~ schedule %s -> %s", before, after))
			}
		}
	}

	if !found {
		if dayMask == 0 {
			dayMask = 127
		}
		zone := tz
		if zone == "" {
			zone = "UTC"
		}
		s := map[string]interface{}{
			"startHours": hours, "startMinutes": minutes,
			"daysToRelease": fmt.Sprint(dayMask), "timeZoneId": zone,
		}
		raw["triggers"] = append(triggers, map[string]interface{}{"triggerType": "schedule", "schedule": s})
		lines = append(lines, "+ add schedule "+describe(s))
	}
	return lines
}
