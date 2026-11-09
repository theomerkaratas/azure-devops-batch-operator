// Command export-release-steps exports classic release pipelines to local files: the full definition
// JSON, the inline PowerShell and Command Line step scripts, or both.
package exportreleasesteps

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
	"github.com/omerkaratas/azure-devops-go-automations/internal/export"
)

const (
	modeJSON    = "json"
	modeScripts = "scripts"
	modeBoth    = "both"

	powershellTaskID  = "e213ff0f-5d5c-4791-802d-52ea3e7be1f1"
	commandLineTaskID = "d9bafed4-0b18-4f58-968d-86655b4d2ce9"
)

// scriptExtensions maps the task IDs of script steps to the file extension they are exported as.
var scriptExtensions = map[string]string{
	powershellTaskID:  ".ps1",
	commandLineTaskID: ".cmd",
}

const usageEpilog = `
Examples:
  export-release-steps 'Example.Project\Ara Yaygınlaştırma' --out ./out
  export-release-steps 'Example.Project\DRC' 'Example.Project\Kesintisiz' --mode both --out ./out --dry-run
Arguments:
  target     Required, one or more. A folder (covers every pipeline beneath it) or one pipeline's full path.
  --out      Required. Local folder to write into. Files are placed under Project/Folder/... like backup-pipelines.
  --mode     json: definition JSON as <id>_<name>.json.
             scripts: inline PowerShell and Command Line steps as <pipeline>/<step>.ps1 and .cmd (no JSON).
             both: JSON and scripts. Default: json.
  --filter   Optional. Only pipelines whose name contains this text (case-insensitive).
  --level    Optional. PAT level (default: read). Choices: read | read-write | manage
  --dry-run  Lists what would be written without writing any files.
Only inline PowerShell steps are exported as .ps1. File-based PowerShell steps are skipped and counted.
`

type options struct {
	targets []string
	out     string
	mode    string
	filter  string
	level   string
	dryRun  bool
}

// fileOutput is one file the export will write.
type fileOutput struct {
	path string
	data []byte
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("export-release-steps", flag.ExitOnError)
	out := fs.String("out", "", "Local folder to write into")
	mode := fs.String("mode", modeJSON, "Export format: json, scripts, both")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	level := fs.String("level", azuredevops.DefaultLevel("read"), "PAT authorization level to use: read, read-write, manage")
	dryRun := fs.Bool("dry-run", false, "Lists what would be written without writing any files")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Exports release pipeline definitions and inline step scripts to local files.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: export-release-steps <target>... --out <dir> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"out": true, "mode": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(2)
	}
	if strings.TrimSpace(*out) == "" {
		fmt.Fprintln(os.Stderr, "Error: --out is required")
		os.Exit(2)
	}
	if *mode != modeJSON && *mode != modeScripts && *mode != modeBoth {
		fmt.Fprintln(os.Stderr, "Error: --mode must be one of: json, scripts, both")
		os.Exit(2)
	}
	err := run(options{
		targets: fs.Args(),
		out:     *out,
		mode:    *mode,
		filter:  *filter,
		level:   *level,
		dryRun:  *dryRun,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// totals accumulates counts across every pipeline in every target.
type totals struct {
	pipelines, steps, ps1, cmd, skipped, failed int
}

func run(o options) error {
	cfg, err := azuredevops.LoadConfig(o.level)
	if err != nil {
		return err
	}
	dest, err := expandDir(o.out)
	if err != nil {
		return err
	}
	if o.dryRun {
		fmt.Println(">>> DRY-RUN MODE ACTIVE (no files will be written) <<<")
	}

	var t totals
	for _, target := range o.targets {
		fmt.Printf("\n== %s\n", target)
		project, defs, err := batchupdate.SelectDefinitions(cfg, target, o.filter)
		if err != nil {
			fmt.Printf("  ERROR %v\n", err)
			t.failed++
			continue
		}
		if len(defs) == 0 {
			fmt.Println("  no matching pipelines found")
			continue
		}
		sort.SliceStable(defs, func(i, j int) bool {
			if defs[i].Path != defs[j].Path {
				return defs[i].Path < defs[j].Path
			}
			return defs[i].Name < defs[j].Name
		})

		var targetTotals totals
		for _, d := range defs {
			res, err := exportPipeline(cfg, project, d, dest, o.mode)
			if err != nil {
				fmt.Printf("  ERROR %d %s: %v\n", d.ID, d.Name, err)
				targetTotals.failed++
				continue
			}
			targetTotals.pipelines++
			targetTotals.steps += res.steps
			targetTotals.ps1 += res.ps1
			targetTotals.cmd += res.cmd
			targetTotals.skipped += res.skipped

			fmt.Printf("  ok %s\\%s\n", d.Path, res.name)
			for _, f := range res.files {
				if o.dryRun {
					fmt.Printf("     would write %s\n", f.path)
					continue
				}
				if err := export.WriteFile(f.path, f.data); err != nil {
					fmt.Printf("     ERROR %v\n", err)
					targetTotals.failed++
					continue
				}
				fmt.Printf("     %s\n", f.path)
			}
			if res.skipped > 0 {
				fmt.Printf("     skipped %d file-based PowerShell step(s)\n", res.skipped)
			}
		}

		fmt.Printf("  processed: %d, total steps: %d, ps1: %d, cmd: %d, skipped: %d, errors: %d\n",
			targetTotals.pipelines, targetTotals.steps, targetTotals.ps1, targetTotals.cmd, targetTotals.skipped, targetTotals.failed)
		fmt.Printf("  output: %s\n", outputDir(dest, project, ""))

		t.pipelines += targetTotals.pipelines
		t.steps += targetTotals.steps
		t.ps1 += targetTotals.ps1
		t.cmd += targetTotals.cmd
		t.skipped += targetTotals.skipped
		t.failed += targetTotals.failed
	}

	if o.dryRun {
		fmt.Printf("\nDry-run complete. %d pipeline(s) would be exported.\n", t.pipelines)
		return nil
	}
	fmt.Printf("\n=== DONE ===\nPipelines: %d | Steps: %d | ps1: %d | cmd: %d | Skipped: %d | Failed: %d\n",
		t.pipelines, t.steps, t.ps1, t.cmd, t.skipped, t.failed)
	if t.failed > 0 {
		return fmt.Errorf("%d error(s) during export", t.failed)
	}
	return nil
}

// pipelineResult describes what one pipeline exported.
type pipelineResult struct {
	name    string
	files   []fileOutput
	steps   int
	ps1     int
	cmd     int
	skipped int
}

// exportPipeline fetches one definition and builds the files for it. Nothing is written here, so a
// pipeline that fails part-way leaves no partial output behind.
func exportPipeline(cfg azuredevops.Config, project string, d azuredevops.ReleaseDefinition, dest, mode string) (pipelineResult, error) {
	raw, err := cfg.GetDefinitionDetailRaw(project, d.ID)
	if err != nil {
		return pipelineResult{}, err
	}
	name := asString(raw["name"])
	if name == "" {
		name = d.Name
	}
	res := pipelineResult{name: name, steps: countSteps(raw)}
	dir := outputDir(dest, project, d.Path)

	if mode == modeJSON || mode == modeBoth {
		data, err := marshalJSON(raw)
		if err != nil {
			return pipelineResult{}, err
		}
		file := filepath.Join(dir, fmt.Sprintf("%d_%s.json", d.ID, export.SafeName(d.Name)))
		res.files = append(res.files, fileOutput{path: file, data: data})
	}

	if mode == modeScripts || mode == modeBoth {
		scripts, skipped, err := collectScripts(raw)
		if err != nil {
			return pipelineResult{}, err
		}
		res.skipped = skipped
		scriptDir := filepath.Join(dir, export.SafeName(name))
		for _, s := range scripts {
			res.files = append(res.files, fileOutput{path: filepath.Join(scriptDir, s.name+s.ext), data: s.data})
			if s.ext == ".ps1" {
				res.ps1++
			} else {
				res.cmd++
			}
		}
	}
	return res, nil
}

// scriptFile is one exported step script, named without its extension.
type scriptFile struct {
	name string
	ext  string
	data []byte
}

// collectScripts returns the inline PowerShell and Command Line steps of a definition, in stage
// and job order, and the number of file-based PowerShell steps that were skipped.
func collectScripts(raw map[string]interface{}) ([]scriptFile, int, error) {
	var scripts []scriptFile
	skipped := 0
	used := map[string]bool{}
	for _, task := range allTasks(raw) {
		ext, ok := scriptExtensions[asString(task["taskId"])]
		if !ok {
			continue
		}
		inputs := asMap(task["inputs"])
		if ext == ".ps1" && asString(inputs["targetType"]) != "inline" {
			skipped++
			continue
		}
		taskName := asString(task["name"])
		stem := export.SafeName(taskName)
		if stem == "" {
			stem = "step"
		}
		data, err := export.EncodeScript(asString(inputs["script"]), ext)
		if err != nil {
			return nil, skipped, fmt.Errorf("step %q: %w", taskName, err)
		}
		scripts = append(scripts, scriptFile{name: export.UniqueName(stem, used), ext: ext, data: data})
	}
	return scripts, skipped, nil
}

// allTasks returns every workflow task of a definition, across all stages and jobs.
func allTasks(raw map[string]interface{}) []map[string]interface{} {
	var tasks []map[string]interface{}
	for _, env := range asMaps(raw["environments"]) {
		for _, phase := range asMaps(env["deployPhases"]) {
			tasks = append(tasks, asMaps(phase["workflowTasks"])...)
		}
	}
	return tasks
}

func countSteps(raw map[string]interface{}) int {
	return len(allTasks(raw))
}

// outputDir mirrors a pipeline's project and folder path under dest, so exports from different
// folders never collide. Pass an empty folder for the project directory itself.
func outputDir(dest, project, folder string) string {
	parts := []string{dest, export.SafeName(project)}
	for _, s := range strings.Split(folder, `\`) {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, export.SafeName(s))
		}
	}
	return filepath.Join(parts...)
}

// marshalJSON writes indented JSON without escaping <, >, and & (so script text stays readable)
// and without a trailing newline.
func marshalJSON(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func expandDir(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[1:])
	}
	return filepath.Abs(path)
}

func asMap(v interface{}) map[string]interface{} {
	m, _ := v.(map[string]interface{})
	return m
}

func asMaps(v interface{}) []map[string]interface{} {
	list, _ := v.([]interface{})
	out := make([]map[string]interface{}, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}
