package auditpipelinepermissions

import (
	"testing"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

const (
	view   = 1
	edit   = 2
	del    = 4
	appr   = 8
	create = 64
)

var bits = map[int]string{view: "view", edit: "edit", del: "delete", appr: "approve", create: "trigger"}

func ace(d string, allow, deny int) azuredevops.ACE {
	return azuredevops.ACE{Descriptor: d, Allow: allow, Deny: deny}
}

func acl(token string, inherit bool, aces ...azuredevops.ACE) azuredevops.ACL {
	m := map[string]azuredevops.ACE{}
	for _, a := range aces {
		m[a.Descriptor] = a
	}
	return azuredevops.ACL{Token: token, InheritPermissions: inherit, AcesDictionary: m}
}

func TestInheritanceDenyAndCut(t *testing.T) {
	pipes := []pipeline{{1, "A", `\F`}, {2, "B", `\F`}, {3, "C", `\G`}}
	acls := []azuredevops.ACL{
		acl("P", true, ace("users", view|edit|create, 0)),
		acl("P/F", true, ace("users", 0, edit)),       // deny edit on folder F
		acl("P/G", false, ace("admins", view|del, 0)), // G does not inherit
		acl("P/F/2", true, ace("users", appr, 0)),     // extra grant on B only
	}
	res := analyze("P", pipes, acls, bits)
	if res.perPipeline[1] == res.perPipeline[2] {
		t.Fatal("B should differ from A")
	}
	var a *permissionSet
	for _, g := range res.groups {
		for _, p := range g.pipelines {
			if p.id == 1 {
				a = g
			}
		}
	}
	if len(a.grants) != 1 || a.grants[0].caps["edit"] || !a.grants[0].caps["trigger"] {
		t.Fatalf("deny not applied: %+v", a.grants)
	}
	c := res.groups[res.indexOf(3)]
	if len(c.grants) != 1 || c.grants[0].descriptor != "admins" || c.grants[0].caps["trigger"] {
		t.Fatalf("inheritance cut ignored: %+v", c.grants)
	}
	if len(res.noInherit) != 1 {
		t.Fatalf("noInherit = %v", res.noInherit)
	}
}

func (a analysis) indexOf(id int) int {
	for i, g := range a.groups {
		for _, p := range g.pipelines {
			if p.id == id {
				return i
			}
		}
	}
	return -1
}

func TestReportFindings(t *testing.T) {
	pipes := []pipeline{{1, "A", `\F`}, {2, "B", `\F`}}
	acls := []azuredevops.ACL{
		acl("P", true, ace("valid", view|edit, 0)),
		acl("P/F/2", true, ace("other", view, 0)),
	}
	res := analyze("P", pipes, acls, bits)
	counts := report(res, func(d string) string {
		if d == "valid" {
			return "[Proj]\\Project Valid Users"
		}
		return d
	}, []string{"Valid Users"})
	if counts.broad != 2 || counts.inconsistent != 1 {
		t.Fatalf("%+v", counts)
	}
}

func TestChildAllowOverridesInheritedDeny(t *testing.T) {
	pipes := []pipeline{{1, "A", `\F`}, {2, "B", `\F`}}
	acls := []azuredevops.ACL{
		acl("P/F", true, ace("users", 0, edit)),           // folder denies edit
		acl("P/F/1", true, ace("users", edit, 0)),         // pipeline A explicitly allows it
		acl("P/F/2", true, ace("users", view|edit, edit)), // same-level allow+deny: deny wins
	}
	res := analyze("P", pipes, acls, bits)
	has := func(id int, cap string) bool {
		for _, g := range res.groups[res.indexOf(id)].grants {
			if g.caps[cap] {
				return true
			}
		}
		return false
	}
	if !has(1, "edit") {
		t.Fatal("child explicit allow must override parent deny")
	}
	if has(2, "edit") || !has(2, "view") {
		t.Fatal("same-level deny must win and view must remain")
	}
}
