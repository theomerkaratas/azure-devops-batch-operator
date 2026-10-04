package createfiles

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunCreatesNestedFile(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "target")
	if err := run(target, []string{"nested/file.txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "nested", "file.txt")); err != nil {
		t.Fatalf("created file: %v", err)
	}
}

func TestRunRejectsPathOutsideTarget(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := run(target, []string{"../outside.txt"}); err == nil {
		t.Fatal("run() accepted a filename outside the target folder")
	}
	if _, err := os.Stat(filepath.Join(root, "outside.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside file exists or stat failed unexpectedly: %v", err)
	}
}

func TestRunRejectsAbsoluteFilename(t *testing.T) {
	t.Parallel()

	target := filepath.Join(t.TempDir(), "target")
	if err := run(target, []string{filepath.Join(string(filepath.Separator), "tmp", "outside.txt")}); err == nil {
		t.Fatal("run() accepted an absolute filename")
	}
}
