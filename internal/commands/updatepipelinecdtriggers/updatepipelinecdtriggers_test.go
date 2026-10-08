package updatepipelinecdtriggers

import "testing"

func raw() obj {
	return obj{"artifacts": []interface{}{obj{"type": "Build", "alias": "_A"}, obj{"type": "Build", "alias": "_B"}, obj{"type": "Git", "alias": "_G"}}, "triggers": []interface{}{}}
}

func TestEnableFiltersDisable(t *testing.T) {
	r := raw()
	lines, err := apply(r, "enable", nil, []string{"refs/heads/main"}, nil)
	if err != nil || len(lines) != 2 || len(r["triggers"].([]interface{})) != 2 {
		t.Fatalf("%v %v", lines, err)
	}
	if lines, _ := apply(r, "enable", nil, []string{"refs/heads/main"}, nil); lines != nil {
		t.Fatalf("not idempotent: %v", lines)
	}
	if lines, _ := apply(r, "set-filters", []string{"_A"}, []string{"refs/heads/release"}, []string{"rc"}); len(lines) != 1 {
		t.Fatalf("%v", lines)
	}
	if lines, _ := apply(r, "disable", []string{"_B"}, nil, nil); len(lines) != 1 || len(r["triggers"].([]interface{})) != 1 {
		t.Fatalf("%v", lines)
	}
	if _, err := apply(r, "enable", []string{"_G"}, nil, nil); err == nil {
		t.Fatal("non-build alias must fail")
	}
}
