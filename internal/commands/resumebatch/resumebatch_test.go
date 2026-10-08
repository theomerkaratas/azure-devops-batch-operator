package resumebatch

import (
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/manifest"
)

func TestDecide(t *testing.T) {
	m := &manifest.Manifest{Comment: "c"}
	e := manifest.Entry{OriginalRevision: 5}
	cases := []struct {
		name string
		cur  map[string]interface{}
		want decision
	}{
		{"untouched", map[string]interface{}{"revision": 5.0}, apply},
		{"applied before crash", map[string]interface{}{"revision": 6.0, "comment": "c"}, alreadyDone},
		{"someone else edited", map[string]interface{}{"revision": 6.0, "comment": "other"}, conflict},
		{"several edits", map[string]interface{}{"revision": 9.0, "comment": "c"}, conflict},
	}
	for _, c := range cases {
		if got, _ := decide(m, e, c.cur); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}
