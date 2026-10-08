package manifest

import "testing"

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv(dirEnv, t.TempDir())
	m, err := New("update-pipeline-retention", `P\TEST`, "", "P", "c")
	if err != nil {
		t.Fatal(err)
	}
	m.Entries = []Entry{
		{DefinitionID: 1, Name: "a", Status: StatusSucceeded, OriginalRevision: 3, NewRevision: 4, Original: map[string]interface{}{"revision": 3.0}},
		{DefinitionID: 2, Name: "b", Status: StatusFailed, Error: "boom"},
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(m.ID[:12])
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != m.ID || len(got.Entries) != 2 || got.Entries[1].Error != "boom" || Revision(got.Entries[0].Original) != 3 {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if c := got.Counts(); c[StatusSucceeded] != 1 || c[StatusFailed] != 1 {
		t.Fatalf("counts = %v", c)
	}
	if _, err := Load("nope"); err == nil {
		t.Fatal("expected error for unknown id")
	}
}

func TestCloneIsDeep(t *testing.T) {
	src := map[string]interface{}{"a": map[string]interface{}{"b": 1.0}}
	cp, _ := Clone(src)
	cp["a"].(map[string]interface{})["b"] = 2.0
	if src["a"].(map[string]interface{})["b"] != 1.0 {
		t.Fatal("clone shares state with source")
	}
}
