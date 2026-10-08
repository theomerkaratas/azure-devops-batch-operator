// Command report-pipeline-inventory writes an inventory of every release pipeline under a folder:
// stages, tasks, variables, pools, demands, schedules, artifacts, approvals and retention.
package reportpipelineinventory

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
	"github.com/omerkaratas/azure-devops-go-automations/internal/inventory"
)

const usageEpilog = `
Examples:
  report-pipeline-inventory 'Example.Project\TEST'
  report-pipeline-inventory 'Example.Project' --format csv --out inventory.csv
  report-pipeline-inventory 'Example.Project\TEST' --format json --out inventory.json
Arguments:
  target          Required. A folder (covers all pipelines under it) or one pipeline path.
  --format        text (default), json or csv. CSV has one row per pipeline stage.
  --out           Optional. Write the report to this file instead of stdout.
  --filter        Optional. Only pipelines whose name contains this text (case-insensitive).
  --include-values  Include non-secret variable values in JSON output (names only by default).
  --level         Optional. PAT level (default: read). Choices: read | read-write | manage
Secret variable values are never available and never included.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("report-pipeline-inventory", flag.ExitOnError)
	format := fs.String("format", "text", "Output format: text, json or csv")
	out := fs.String("out", "", "Write the report to this file instead of stdout")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	values := fs.Bool("include-values", false, "Include non-secret variable values (JSON)")
	level := fs.String("level", azuredevops.DefaultLevel("read"), fmt.Sprintf("PAT authorization level to use: %s (default: config default_token or read)", strings.Join(azuredevops.Levels(), ", ")))
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Generates an inventory report of the release pipelines under a folder.")
		fmt.Fprintln(os.Stderr, "\nUsage: report-pipeline-inventory <target> [flags]")
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], map[string]bool{"format": true, "out": true, "filter": true, "level": true})); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *filter, *level, *format, *out, *values); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(target, filter, level, format, out string, values bool) error {
	valid := false
	for _, f := range inventory.Formats {
		valid = valid || f == format
	}
	if !valid {
		return fmt.Errorf("--format must be one of: %s", strings.Join(inventory.Formats, ", "))
	}
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	project, defs, err := batchupdate.SelectDefinitions(cfg, target, filter)
	if err != nil {
		return err
	}
	if len(defs) == 0 {
		return fmt.Errorf("no matching release pipelines found under `%s`", target)
	}
	fmt.Fprintf(os.Stderr, "Reading %d pipeline definition(s)...\n", len(defs))
	recs, err := inventory.Collect(cfg, project, defs, inventory.Options{IncludeValues: values})
	if err != nil {
		return err
	}
	var w io.Writer = os.Stdout
	if out != "" {
		f, err := os.Create(out)
		if err != nil {
			return err
		}
		defer f.Close()
		w = f
	}
	if err := inventory.Write(w, format, recs); err != nil {
		return err
	}
	if out != "" {
		fmt.Fprintf(os.Stderr, "Wrote %d pipeline(s) to %s\n", len(recs), out)
	}
	return nil
}
