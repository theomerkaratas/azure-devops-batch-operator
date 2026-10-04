// Command create-files creates empty file(s) in a target folder, creating the folder if needed.
package createfiles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const usage = `Creates empty file(s) in a target folder; the folder is created if it doesn't exist.
Usage: create-files <target_path> <filename> [filename...]
Example:
  create-files targetpath file1.txt file2.py sub/folder/file3.txt
Arguments:
  target_path  Required. The target folder files are created in. Created automatically if missing.
  filenames    Required, one or more space-separated filenames. May include subfolders
               (e.g. sub/folder/file.txt); subfolders are created as needed.
               Existing files are not overwritten, they are skipped.
`

// Main runs the command using os.Args.
func Main() {
	if len(os.Args) < 3 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
func run(targetPath string, filenames []string) error {
	targetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("resolve target path: %w", err)
	}
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return err
	}
	for _, filename := range filenames {
		if filename == "" || filepath.IsAbs(filename) {
			return fmt.Errorf("invalid filename %q: must be a relative file path", filename)
		}
		filePath := filepath.Join(targetPath, filename)
		rel, err := filepath.Rel(targetPath, filePath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("invalid filename %q: path escapes target folder", filename)
		}
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
			return err
		}
		if _, err := os.Stat(filePath); err == nil {
			fmt.Printf("Skipped (already exists): %s\n", filePath)
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		f, err := os.OpenFile(filePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			return fmt.Errorf("close %s: %w", filePath, err)
		}
		fmt.Printf("Created: %s\n", filePath)
	}
	return nil
}
