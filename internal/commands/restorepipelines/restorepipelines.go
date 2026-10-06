// Command restore-pipelines recreates or overwrites release pipelines from JSON files produced
// by backup-pipelines.
package restorepipelines

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  restore-pipelines ./backups/Example.Project/TEST/CONFIG --dry-run
  restore-pipelines ./backups/Example.Project --filter WCF -y
  restore-pipelines ./backups/Example.Project/TEST/CONFIG/App.Svc.json -y
Arguments:
  source     Required. A single backup JSON file (from backup-pipelines), or a folder to restore
             every *.json backup found under it.
  --filter   Optional. Only backup files whose filename contains this text (case-insensitive).
  --level    Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run  Lists what would be created or overwritten without changing anything.
  -y, --yes  Skips the confirmation prompt.
Notes:
  A backup whose project/folder/name still matches an existing pipeline is OVERWRITTEN with the
  backed-up definition. One with no match is CREATED fresh (its folder is created if missing).
`

// backupFile mirrors the format backup-pipelines writes.
type backupFile struct {
	Project    string                 `json:"project"`
	Definition map[string]interface{} `json:"definition"`
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("restore-pipelines", flag.ExitOnError)
	filter := fs.String("filter", "", "Only backup files whose filename contains this text")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level to use: read-write, manage (default: config default_token or read-write)")
	dryRun := fs.Bool("dry-run", false, "Lists what would be created or overwritten without changing anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Restores release pipeline definitions from backup-pipelines JSON files.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: restore-pipelines <source> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *filter, *level, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// plannedRestore is one backup file resolved against the organization's current state.
type plannedRestore struct {
	file     string
	project  string
	path     string
	name     string
	raw      map[string]interface{}
	existing *azuredevops.ReleaseDefinition
}

func run(source, filter, level string, dryRun, autoYes bool) error {
	files, err := collectBackupFiles(source, filter)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Printf("No backup files found under '%s'.\n", source)
		return nil
	}
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}

	var plans []plannedRestore
	for _, f := range files {
		bf, err := loadBackup(f)
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		path, _ := bf.Definition["path"].(string)
		name, _ := bf.Definition["name"].(string)
		if path == "" {
			path = `\`
		}
		p := plannedRestore{file: f, project: bf.Project, path: path, name: name, raw: bf.Definition}
		if existing, err := cfg.FindDefinition(bf.Project, path, name); err == nil {
			e := existing
			p.existing = &e
		}
		plans = append(plans, p)
	}

	fmt.Printf("Source: %s\nBackup files found: %d\nLevel: %s\n", source, len(plans), level)
	if dryRun {
		fmt.Println(">>> DRY-RUN MODE ACTIVE (nothing will be changed) <<<")
	}

	fmt.Println("\n=== PLANNED RESTORE ===")
	for _, p := range plans {
		action := "CREATE"
		if p.existing != nil {
			action = "OVERWRITE"
		}
		fmt.Printf("[WILL %s] %s\n", action, pipelineDisplay(p.project, p.path, p.name))
	}

	if dryRun {
		fmt.Println("\nDry-run complete. Nothing was changed.")
		return nil
	}
	if !autoYes && !batchupdate.Confirm(fmt.Sprintf("\n%d pipeline(s) will be restored. Continue? (y/N): ", len(plans))) {
		fmt.Println("Cancelled.")
		return nil
	}

	fmt.Println("\nRestoring...")
	ok, failed := 0, 0
	for _, p := range plans {
		if err := restoreOne(cfg, p); err != nil {
			fmt.Printf("  x %s\\%s ERROR: %v\n", p.path, p.name, err)
			failed++
			continue
		}
		ok++
	}
	fmt.Printf("\n=== DONE ===\nSucceeded: %d | Failed: %d\n", ok, failed)
	if failed > 0 {
		return fmt.Errorf("%d restore(s) failed", failed)
	}
	return nil
}

// pipelineDisplay joins a project, folder path (as returned by Azure DevOps, which already
// carries its own leading '\' or is exactly '\' for the root) and pipeline name into one
// readable path without doubling up the separator.
func pipelineDisplay(project, path, name string) string {
	if path == `\` {
		return project + `\` + name
	}
	return project + path + `\` + name
}

func restoreOne(cfg azuredevops.Config, p plannedRestore) error {
	raw := p.raw
	if p.existing != nil {
		current, err := cfg.GetDefinitionDetailRaw(p.project, p.existing.ID)
		if err != nil {
			return err
		}
		raw["id"] = current["id"]
		raw["revision"] = current["revision"]
		if _, err := cfg.UpdateDefinitionRaw(p.project, raw, "Restored from backup "+p.file); err != nil {
			return err
		}
		fmt.Printf("  ok %s\\%s overwritten\n", p.path, p.name)
		return nil
	}

	// Drop server-assigned identity so the API creates a fresh definition.
	for _, k := range []string{"id", "revision", "createdBy", "createdOn", "modifiedBy", "modifiedOn", "url", "_links", "lastRelease"} {
		delete(raw, k)
	}
	if envs, ok := raw["environments"].([]interface{}); ok {
		for _, e := range envs {
			if env, ok := e.(map[string]interface{}); ok {
				env["id"] = 0
			}
		}
	}
	if p.path != `\` {
		if err := cfg.CreateFolder(p.project, p.path); err != nil {
			return fmt.Errorf("create folder %s: %w", p.path, err)
		}
	}
	if _, err := cfg.CreateDefinitionRaw(p.project, raw, "Restored from backup "+p.file); err != nil {
		return err
	}
	fmt.Printf("  ok %s\\%s created\n", p.path, p.name)
	return nil
}

func collectBackupFiles(source, filter string) ([]string, error) {
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	var files []string
	if !info.IsDir() {
		files = append(files, source)
	} else {
		err = filepath.WalkDir(source, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
				return nil
			}
			files = append(files, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	if filter != "" {
		var out []string
		for _, f := range files {
			if strings.Contains(strings.ToLower(filepath.Base(f)), strings.ToLower(filter)) {
				out = append(out, f)
			}
		}
		files = out
	}
	sort.Strings(files)
	return files, nil
}

func loadBackup(path string) (backupFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return backupFile{}, err
	}
	var bf backupFile
	if err := json.Unmarshal(data, &bf); err != nil {
		return backupFile{}, err
	}
	if bf.Project == "" {
		return backupFile{}, fmt.Errorf(`missing "project" field`)
	}
	if bf.Definition == nil {
		return backupFile{}, fmt.Errorf(`missing "definition" field`)
	}
	return bf, nil
}
