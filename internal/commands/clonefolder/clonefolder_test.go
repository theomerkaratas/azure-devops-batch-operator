package clonefolder

import "testing"

func TestParseAndRemapFolderPath(t *testing.T) {
	project, root, err := parseFolderPath(`Example.Project/DEV/CONFIG`)
	if err != nil {
		t.Fatal(err)
	}
	if project != "Example.Project" || root != `\DEV\CONFIG` {
		t.Fatalf("parseFolderPath() = %q, %q", project, root)
	}
	got := remapPath(`\DEV\CONFIG\Apps\API`, root, `\TEST\CONFIG`)
	if got != `\TEST\CONFIG\Apps\API` {
		t.Fatalf("remapPath() = %q", got)
	}
}

func TestIsAtOrBelowUsesFolderBoundary(t *testing.T) {
	if !isAtOrBelow(`\DEV\CONFIG\Child`, `\dev\config`) {
		t.Fatal("expected child folder to match case-insensitively")
	}
	if isAtOrBelow(`\DEV\CONFIGURATION`, `\DEV\CONFIG`) {
		t.Fatal("folder prefix without a boundary must not match")
	}
}

func TestUniqueSortedPathsParentsFirst(t *testing.T) {
	got := uniqueSortedPaths([]string{`\Copy\Child\Deep`, `\Copy`, `\copy\child`, `\Copy\Child`})
	want := []string{`\Copy`, `\copy\child`, `\Copy\Child\Deep`}
	if len(got) != len(want) {
		t.Fatalf("paths = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("path %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPrepareCloneRemovesServerIdentity(t *testing.T) {
	env := map[string]interface{}{"id": float64(7), "name": "Stage"}
	raw := map[string]interface{}{
		"id": float64(42), "revision": float64(3), "name": "Pipeline", "comment": "old",
		"environments": []interface{}{env},
	}
	prepareClone(raw)
	if _, ok := raw["id"]; ok {
		t.Fatal("definition id was not removed")
	}
	if _, ok := raw["comment"]; ok {
		t.Fatal("old comment was not removed")
	}
	if env["id"] != 0 {
		t.Fatalf("environment id = %#v, want 0", env["id"])
	}
}
