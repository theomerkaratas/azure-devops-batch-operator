// Command clone-folder copies a classic release folder tree and all definitions beneath it.
package clonefolder

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
	"github.com/omerkaratas/azure-devops-go-automations/internal/cliutil"
)

const usageEpilog = `
Examples:
  clone-folder 'Example.Project\DEV\CONFIG' 'Example.Project\TEST\CONFIG' --dry-run
  clone-folder 'Example.Project\DEV\CONFIG' 'Example.Project\TEST\CONFIG' --yes
Arguments:
  source       Required. Existing folder path, including the project name.
  destination  Required. New folder path in the same project. It must not already exist.
  --level      Optional. PAT level (default: read-write). Choices: read-write | manage
  --dry-run    Lists folders and release pipelines that would be created.
  -y, --yes    Skips the confirmation prompt.
Note: secret variable values are not returned by Azure DevOps, so cloned secret variables
are empty and must be set again (see update-pipeline-variables --secret).
`

type cloneItem struct {
	source azuredevops.ReleaseDefinition
	path   string
	raw    map[string]interface{}
}

// Main runs the command using os.Args.
func Main() {
	fs := flag.NewFlagSet("clone-folder", flag.ExitOnError)
	defaultLevel := azuredevops.DefaultLevel("read-write")
	if !batchupdate.ValidWriteLevel(defaultLevel) {
		defaultLevel = "read-write"
	}
	level := fs.String("level", defaultLevel, "PAT authorization level to use: read-write or manage")
	dryRun := fs.Bool("dry-run", false, "Lists what would be created without creating anything")
	yes := fs.Bool("yes", false, "Skips the confirmation prompt")
	fs.BoolVar(yes, "y", false, "Shorthand for --yes")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Copies a release folder, its subfolders, and all release pipelines beneath it.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: clone-folder <source-folder> <destination-folder> [flags]")
		fmt.Fprintln(os.Stderr)
		fs.PrintDefaults()
		fmt.Fprint(os.Stderr, usageEpilog)
	}
	if err := fs.Parse(cliutil.ReorderArgs(os.Args[1:], map[string]bool{"level": true})); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 2 {
		fs.Usage()
		os.Exit(2)
	}
	if !batchupdate.ValidWriteLevel(*level) {
		fmt.Fprintln(os.Stderr, "Error: --level must be one of: read-write, manage")
		os.Exit(2)
	}
	if err := run(fs.Arg(0), fs.Arg(1), *level, *dryRun, *yes); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(source, destination, level string, dryRun, autoYes bool) error {
	cfg, err := azuredevops.LoadConfig(level)
	if err != nil {
		return err
	}
	srcProject, srcRoot, err := parseFolderPath(source)
	if err != nil {
		return fmt.Errorf("source: %w", err)
	}
	dstProject, dstRoot, err := parseFolderPath(destination)
	if err != nil {
		return fmt.Errorf("destination: %w", err)
	}
	if !strings.EqualFold(srcProject, dstProject) {
		return fmt.Errorf("destination must be in the same project (%s)", srcProject)
	}
	if strings.EqualFold(srcRoot, dstRoot) {
		return fmt.Errorf("source and destination folders must be different")
	}

	folders, err := cfg.ListReleaseFolders(srcProject)
	if err != nil {
		return err
	}
	definitions, err := cfg.ListReleaseDefinitions(srcProject)
	if err != nil {
		return err
	}
	for _, folder := range folders {
		if strings.EqualFold(normalizeFolder(folder.Path), dstRoot) {
			return fmt.Errorf("destination folder already exists: %s", destination)
		}
	}

	var destinationFolders []string
	sourceExists := false
	for _, folder := range folders {
		path := normalizeFolder(folder.Path)
		if isAtOrBelow(path, srcRoot) {
			sourceExists = true
			destinationFolders = append(destinationFolders, remapPath(path, srcRoot, dstRoot))
		}
	}
	var selected []azuredevops.ReleaseDefinition
	for _, definition := range definitions {
		path := normalizeFolder(definition.Path)
		if isAtOrBelow(path, srcRoot) {
			sourceExists = true
			selected = append(selected, definition)
		}
	}
	if !sourceExists {
		return fmt.Errorf("source folder not found: %s", source)
	}
	destinationFolders = append(destinationFolders, dstRoot)
	destinationFolders = uniqueSortedPaths(destinationFolders)
	sort.Slice(selected, func(i, j int) bool {
		left, right := selected[i].Path+`\`+selected[i].Name, selected[j].Path+`\`+selected[j].Name
		return strings.ToLower(left) < strings.ToLower(right)
	})

	items := make([]cloneItem, 0, len(selected))
	for _, definition := range selected {
		raw, err := cfg.GetDefinitionDetailRaw(srcProject, definition.ID)
		if err != nil {
			return fmt.Errorf("read %s\\%s: %w", definition.Path, definition.Name, err)
		}
		prepareClone(raw)
		dstPath := remapPath(normalizeFolder(definition.Path), srcRoot, dstRoot)
		raw["path"] = dstPath
		items = append(items, cloneItem{source: definition, path: dstPath, raw: raw})
	}

	fmt.Printf("Source folder     : %s%s\n", srcProject, srcRoot)
	fmt.Printf("Destination folder: %s%s\n", dstProject, dstRoot)
	fmt.Printf("Folders to create : %d\nRelease pipelines : %d\n", len(destinationFolders), len(items))
	for _, item := range items {
		fmt.Printf("  - %s\\%s -> %s\\%s\n", normalizeFolder(item.source.Path), item.source.Name, item.path, item.source.Name)
	}
	if dryRun {
		fmt.Println("\nDry-run complete. Nothing was created.")
		return nil
	}
	if !autoYes && !batchupdate.Confirm(fmt.Sprintf("\nCreate %d folder(s) and clone %d pipeline(s)? (y/N): ", len(destinationFolders), len(items))) {
		fmt.Println("Cancelled.")
		return nil
	}
	for _, path := range destinationFolders {
		if err := cfg.CreateFolder(dstProject, path); err != nil {
			return fmt.Errorf("create folder %s: %w", path, err)
		}
	}
	created := 0
	for _, item := range items {
		if _, err := cfg.CreateDefinitionRaw(dstProject, item.raw, "Cloned from "+source); err != nil {
			return fmt.Errorf("clone %s\\%s: %w", item.source.Path, item.source.Name, err)
		}
		created++
		fmt.Printf("  ok %s\\%s\n", item.path, item.source.Name)
	}
	fmt.Printf("\nCreated %d folder(s) and cloned %d release pipeline(s).\n", len(destinationFolders), created)
	return nil
}

func parseFolderPath(value string) (string, string, error) {
	normalized := strings.Trim(strings.ReplaceAll(strings.TrimSpace(value), "/", `\`), `\`)
	parts := strings.FieldsFunc(normalized, func(r rune) bool { return r == '\\' })
	if len(parts) < 2 {
		return "", "", fmt.Errorf("folder path must look like 'Project\\Folder'")
	}
	return parts[0], `\` + strings.Join(parts[1:], `\`), nil
}

func normalizeFolder(path string) string {
	parts := strings.FieldsFunc(strings.ReplaceAll(path, "/", `\`), func(r rune) bool { return r == '\\' })
	if len(parts) == 0 {
		return `\`
	}
	return `\` + strings.Join(parts, `\`)
}

func isAtOrBelow(path, root string) bool {
	return strings.EqualFold(path, root) || (len(path) > len(root) && strings.EqualFold(path[:len(root)], root) && path[len(root)] == '\\')
}

func remapPath(path, sourceRoot, destinationRoot string) string {
	if strings.EqualFold(path, sourceRoot) {
		return destinationRoot
	}
	return destinationRoot + path[len(sourceRoot):]
}

func uniqueSortedPaths(paths []string) []string {
	seen := map[string]string{}
	for _, path := range paths {
		key := strings.ToLower(path)
		if _, ok := seen[key]; !ok {
			seen[key] = path
		}
	}
	out := make([]string, 0, len(seen))
	for _, path := range seen {
		out = append(out, path)
	}
	sort.Slice(out, func(i, j int) bool {
		depthI, depthJ := strings.Count(out[i], `\`), strings.Count(out[j], `\`)
		if depthI != depthJ {
			return depthI < depthJ
		}
		return strings.ToLower(out[i]) < strings.ToLower(out[j])
	})
	return out
}

func prepareClone(raw map[string]interface{}) {
	for _, key := range []string{"id", "revision", "createdBy", "createdOn", "modifiedBy", "modifiedOn", "url", "_links", "lastRelease", "comment"} {
		delete(raw, key)
	}
	if envs, ok := raw["environments"].([]interface{}); ok {
		for _, value := range envs {
			if env, ok := value.(map[string]interface{}); ok {
				env["id"] = 0
			}
		}
	}
}
