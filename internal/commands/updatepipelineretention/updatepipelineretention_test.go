package updatepipelineretention

import "testing"

func raw() obj {
	return obj{"environments": []interface{}{
		obj{"name": "Dev", "retentionPolicy": obj{"daysToKeep": 30.0, "releasesToKeep": 3.0, "retainBuild": true}},
		obj{"name": "Prod"},
	}}
}

func TestApplyAllAndSelected(t *testing.T) {
	r := raw()
	f := false
	lines, err := apply(r, policy{days: 60, releases: 3, retainBuild: &f}, nil)
	if err != nil || len(lines) != 2 {
		t.Fatalf("%v %v", lines, err)
	}
	dev := r["environments"].([]interface{})[0].(obj)["retentionPolicy"].(obj)
	if dev["daysToKeep"] != 60.0 || dev["releasesToKeep"] != 3.0 || dev["retainBuild"] != false {
		t.Fatalf("%v", dev)
	}
	if lines, _ := apply(r, policy{days: 60, releases: 3, retainBuild: &f}, nil); lines != nil {
		t.Fatal("not idempotent")
	}
	if lines, err := apply(raw(), policy{days: 10}, []string{"prod"}); err != nil || len(lines) != 1 {
		t.Fatalf("%v %v", lines, err)
	}
	if _, err := apply(raw(), policy{days: 10}, []string{"Nope"}); err == nil {
		t.Fatal("missing stage must fail")
	}
}
