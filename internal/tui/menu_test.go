package tui

import "testing"

func TestMainMenuHasFourCRUDCategories(t *testing.T) {
	want := []string{"Create", "Read", "Update", "Delete"}
	if len(menuTree) != len(want) {
		t.Fatalf("main menu has %d entries, want %d", len(menuTree), len(want))
	}
	for i, label := range want {
		if menuTree[i].label != label {
			t.Errorf("main menu entry %d = %q, want %q", i, menuTree[i].label, label)
		}
	}
}

func TestMenuTreeContainsEveryCommandExactlyOnce(t *testing.T) {
	seen := map[string]int{}
	var walk func([]menuNode)
	walk = func(nodes []menuNode) {
		for _, node := range nodes {
			if node.commandID != "" {
				seen[node.commandID]++
			}
			walk(node.children)
		}
	}
	walk(menuTree)

	for _, spec := range commandSpecs {
		if seen[spec.id] != 1 {
			t.Errorf("command %q appears %d times in menu tree, want once", spec.id, seen[spec.id])
		}
		delete(seen, spec.id)
	}
	for id, count := range seen {
		t.Errorf("unknown command %q appears %d times in menu tree", id, count)
	}
}
