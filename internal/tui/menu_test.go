package tui

import "testing"

func TestMainMenuHasEightTopLevelCategories(t *testing.T) {
	want := []string{"Create", "Clone", "Read", "Compare", "Update", "Delete", "Cancel", "Backup"}
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
	want := []string{"List", "Broken artifact references", "Deprecated tasks", "Permissions audit", "Policy audit", "Validate release batch", "Inventory report"}
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

func TestCompareMenuGroupsBothCompareCommands(t *testing.T) {
	compareMenu := menuTree[3].children
	want := []string{"Compare pipelines", "Compare folders", "Detect drift"}
	if len(compareMenu) != len(want) {
		t.Fatalf("compare menu has %d entries, want %d", len(compareMenu), len(want))
	}
	for i, label := range want {
		if compareMenu[i].label != label {
			t.Errorf("compare menu entry %d = %q, want %q", i, compareMenu[i].label, label)
		}
	}
}

func TestCloneMenuGroupsPipelineAndFolderCloning(t *testing.T) {
	cloneMenu := menuTree[1].children
	want := []string{"Pipeline", "Folder"}
	if len(cloneMenu) != len(want) {
		t.Fatalf("Clone submenu has %d entries, want %d", len(cloneMenu), len(want))
	}
	for i, label := range want {
		if cloneMenu[i].label != label {
			t.Errorf("Clone submenu entry %d = %q, want %q", i, cloneMenu[i].label, label)
		}
	}
}

func TestDeleteMenuGroupsPipelineAndStepDeletion(t *testing.T) {
	deleteMenu := menuTree[5].children
	want := []string{"Release pipelines", "Pipeline steps", "Old releases"}
	if len(deleteMenu) != len(want) {
		t.Fatalf("Delete submenu has %d entries, want %d", len(deleteMenu), len(want))
	}
	for i, label := range want {
		if deleteMenu[i].label != label {
			t.Errorf("Delete submenu entry %d = %q, want %q", i, deleteMenu[i].label, label)
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
