// Command backup-pipelines writes the full definition of every matched release pipeline to a
// local JSON file, so it can be restored later with restore-pipelines.
package backuppipelines

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  backup-pipelines 'Example.Project\TEST\CONFIG' --out ./backups
  backup-pipelines 'Example.Project' --out ./backups --filter WCF --dry-run
Arguments:
  target     Required. A folder (covers all pipelines under it) or the full path of one pipeline.
  --out      Required. Local folder to write one JSON file per pipeline into, mirroring the
             project/folder structure so files don't collide.
  --filter   Optional. Only pipelines whose name contains this text (case-insensitive).
  --level    Optional. PAT level (default: read-write). Choices: read | read-write | manage
  --dry-run  Lists what would be backed up without writing any files.
Each backup file holds the pipeline's full definition JSON plus its project name, so
restore-pipelines can recreate or restore it later.
`

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("backup-pipelines", flag.ExitOnError)
	out := fs.String("out", "", "Local folder to write one JSON file per pipeline into")
	filter := fs.String("filter", "", "Only pipelines whose name contains this text")
	level := fs.String("level", azuredevops.DefaultLevel("read-write"), "PAT authorization level to use: read, read-write, manage (default: config default_token or read-write)")
	dryRun := fs.Bool("dry-run", false, "Lists what would be backed up without writing any files")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Backs up release pipeline definitions under a path to local JSON files.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: backup-pipelines <target> --out <dir> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	valueFlags := map[string]bool{"out": true, "filter": true, "level": true}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], valueFlags)); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}
	if strings.TrimSpace(*out) == "" {
		fmt.Fprintln(os.Stderr, "Error: --out is required")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), *filter, *out, *level, *dryRun); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// backupFile is the on-disk format written for each pipeline. Project is stored alongside the
// raw definition because Azure DevOps' definition JSON doesn't carry its own project name.
type backupFile struct {
	Project    string                 `json:"project"`
	BackedUpAt time.Time              `json:"backedUpAt"`
	Definition map[string]interface{} `json:"definition"`
}

func run(target, filter, outDir, level string, dryRun bool) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	project, defs, err := batchupdate.SelectDefinitions(cfg, target, filter)
	if err != nil {
		return err
	}
	if len(defs) == 0 {
		fmt.Printf("No matching release pipelines found under '%s'.\n", target)
		return nil
	}

	fmt.Printf("Target: %s\nPipelines found: %d\nOutput: %s\n", target, len(defs), outDir)
	if dryRun {
		fmt.Println(">>> DRY-RUN MODE ACTIVE (no files will be written) <<<")
	}
	fmt.Println()

	ok, failed := 0, 0
	for _, d := range defs {
		folder := d.Path
		if folder == "" {
			folder = `\`
		}
		destPath := backupFilePath(outDir, project, folder, d.Name)
		if dryRun {
			fmt.Printf("  would back up %s\\%s -> %s\n", folder, d.Name, destPath)
			continue
		}
		raw, err := cfg.GetDefinitionDetailRaw(project, d.ID)
		if err != nil {
			fmt.Printf("  x %s\\%s ERROR: %v\n", folder, d.Name, err)
			failed++
			continue
		}
		if err := writeBackup(destPath, project, raw); err != nil {
			fmt.Printf("  x %s\\%s ERROR: %v\n", folder, d.Name, err)
			failed++
			continue
		}
		fmt.Printf("  ok %s\\%s -> %s\n", folder, d.Name, destPath)
		ok++
	}

	if dryRun {
		fmt.Printf("\nDry-run complete. %d pipeline(s) would be backed up.\n", len(defs))
		return nil
	}
	fmt.Printf("\n=== DONE ===\nSucceeded: %d | Failed: %d\n", ok, failed)
	if failed > 0 {
		return fmt.Errorf("%d backup(s) failed", failed)
	}
	return nil
}

// backupFilePath mirrors the pipeline's project/folder structure under outDir, so backing up
// many pipelines never collides two files onto the same path.
func backupFilePath(outDir, project, folder, name string) string {
	segments := []string{outDir, sanitize(project)}
	for _, s := range strings.Split(folder, `\`) {
		if s = strings.TrimSpace(s); s != "" {
			segments = append(segments, sanitize(s))
		}
	}
	segments = append(segments, sanitize(name)+".json")
	return filepath.Join(segments...)
}

// sanitize replaces characters that are illegal in file/folder names on common filesystems.
func sanitize(s string) string {
	replacer := strings.NewReplacer(`\`, "_", "/", "_", ":", "_", "*", "_", "?", "_", `"`, "_", "<", "_", ">", "_", "|", "_")
	return replacer.Replace(s)
}

func writeBackup(path, project string, raw map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	bf := backupFile{Project: project, BackedUpAt: time.Now().UTC(), Definition: raw}
	data, err := json.MarshalIndent(bf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
