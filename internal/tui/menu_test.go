package tui

import "testing"

func TestMainMenuHasSixTopLevelCategories(t *testing.T) {
	want := []string{"Create", "Clone", "Read", "Update", "Delete", "Cancel"}
	if len(menuTree) != len(want) {
		t.Fatalf("main menu has %d entries, want %d", len(menuTree), len(want))
	}
	for i, label := range want {
		if menuTree[i].label != label {
			t.Errorf("main menu entry %d = %q, want %q", i, menuTree[i].label, label)
		}
	}
}

func TestReadMenuGroupsDetailCommandsUnderList(t *testing.T) {
	readMenu := menuTree[2].children
	want := []string{"List", "Compare pipelines"}
	if len(readMenu) != len(want) {
		t.Fatalf("read menu has %d entries, want %d", len(readMenu), len(want))
	}
	for i, label := range want {
		if readMenu[i].label != label {
			t.Errorf("read menu entry %d = %q, want %q", i, readMenu[i].label, label)
		}
	}
	if got := len(readMenu[0].children); got != 9 {
		t.Errorf("List submenu has %d entries, want 9", got)
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
