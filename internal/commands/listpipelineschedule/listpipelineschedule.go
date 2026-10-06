// Command list-pipeline-schedule lists the scheduled triggers for an Azure DevOps release pipeline.
package listpipelineschedule

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Example:
  list-pipeline-schedule 'Example.Project\PREP\CONFIG\PREPAPP\Example.API' --level read
Arguments:
  pipeline_path  Required. Full pipeline path in 'Project\Folder\SubFolder\PipelineName' format.
                 E.g.: Example.Project\PREP\CONFIG\PREPAPP\Example.API
  --level        Optional. The PAT authorization level to use (default: read).
                 Choices: read | read-write | manage
                   read        -> Read-only viewing (sufficient and recommended for this command).
                   read-write  -> For commands that update pipeline definitions.
                   manage      -> For advanced definition/queue management operations.
                 Each level uses its own PAT environment variable (see README).
`

// dayBits maps the Azure DevOps "daysToRelease" bitmask to day names, in bit order.
var dayBits = []struct {
	bit  int
	name string
}{
	{1, "Monday"},
	{2, "Tuesday"},
	{4, "Wednesday"},
	{8, "Thursday"},
	{16, "Friday"},
	{32, "Saturday"},
	{64, "Sunday"},
}

func decodeDays(value azuredevops.IntOrString) string {
	var names []string
	for _, d := range dayBits {
		if int(value)&d.bit != 0 {
			names = append(names, d.name)
		}
	}
	if len(names) == 0 {
		return "None"
	}
	return strings.Join(names, ", ")
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("list-pipeline-schedule", flag.ExitOnError)
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Lists the scheduled triggers for an Azure DevOps release pipeline.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: list-pipeline-schedule <pipeline_path> [--level read|read-write|manage]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], map[string]bool{"level": true})); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	pipelinePath := fs.Arg(0)
	if err := run(pipelinePath, *level); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
func run(pipelinePath, level string) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	definition, err := cfg.ResolveDefinition(pipelinePath)
	if err != nil {
		return err
	}
	fmt.Printf("Pipeline: %s  (id=%d)\n", definition.Name, definition.ID)
	var scheduleTriggers []azuredevops.Trigger
	for _, t := range definition.Triggers {
		if strings.Contains(strings.ToLower(t.TriggerType), "schedule") {
			scheduleTriggers = append(scheduleTriggers, t)
		}
	}
	if len(scheduleTriggers) == 0 {
		fmt.Println("\nNo scheduled triggers found.")
		return nil
	}
	for _, trigger := range scheduleTriggers {
		fmt.Printf("\nTrigger type: %s\n", trigger.TriggerType)
		schedules := trigger.Schedules
		if len(schedules) == 0 && trigger.Schedule != nil {
			schedules = []azuredevops.Schedule{*trigger.Schedule}
		}
		if len(schedules) == 0 {
			raw, _ := json.MarshalIndent(trigger, "", "  ")
			fmt.Println(string(raw))
			continue
		}
		for _, sch := range schedules {
			onlyWithChanges := "not specified"
			if sch.ScheduleOnlyWithChanges != nil {
				onlyWithChanges = fmt.Sprintf("%v", *sch.ScheduleOnlyWithChanges)
			}
			fmt.Printf(
				"  - Days: %s | Time: %02d:%02d | Time zone: %s | Only if changes: %s\n",
				decodeDays(sch.DaysToRelease), sch.StartHours, sch.StartMinutes, sch.TimeZoneID, onlyWithChanges,
			)
		}
	}
	return nil
}
