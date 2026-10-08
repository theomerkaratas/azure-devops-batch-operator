// Package inventory turns raw release definitions into a normalized, report-friendly record
// and renders collections of them as text, JSON or CSV.
package inventory

import (
	"fmt"
	"sort"
	"strings"

	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
	"github.com/omerkaratas/azure-devops-go-automations/internal/batchupdate"
)

type obj = map[string]interface{}

// Variable is a pipeline or stage variable. Value is only filled when requested and never for secrets.
type Variable struct {
	Name          string `json:"name"`
	Secret        bool   `json:"secret"`
	AllowOverride bool   `json:"allowOverride"`
	Value         string `json:"value,omitempty"`
}

// Task is one step of a job.
type Task struct {
	Name    string `json:"name"`
	TaskID  string `json:"taskId"`
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
}

// Job is an agent job within a stage.
type Job struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Pool       string   `json:"pool"`
	Demands    []string `json:"demands"`
	TimeoutMin int      `json:"timeoutMinutes"`
	Tasks      []Task   `json:"tasks"`
}

// Retention is a stage's release retention policy.
type Retention struct {
	Days        int   `json:"daysToKeep"`
	Releases    int   `json:"releasesToKeep"`
	RetainBuild *bool `json:"retainBuild,omitempty"`
}

// Stage is one stage (environment).
type Stage struct {
	Name          string     `json:"name"`
	Rank          int        `json:"rank"`
	Conditions    []string   `json:"conditions"`
	PreApprovals  string     `json:"preApprovals"`
	PostApprovals string     `json:"postApprovals"`
	Retention     *Retention `json:"retention,omitempty"`
	Variables     []Variable `json:"variables"`
	Jobs          []Job      `json:"jobs"`
}

// Artifact is a linked artifact source.
type Artifact struct {
	Alias   string `json:"alias"`
	Type    string `json:"type"`
	Source  string `json:"source"`
	Primary bool   `json:"primary"`
}

// Record is the inventory of one release pipeline.
type Record struct {
	Project        string     `json:"project"`
	Folder         string     `json:"folder"`
	Name           string     `json:"name"`
	ID             int        `json:"id"`
	Revision       int        `json:"revision"`
	Variables      []Variable `json:"variables"`
	VariableGroups []int      `json:"variableGroups"`
	Artifacts      []Artifact `json:"artifacts"`
	Triggers       []string   `json:"triggers"`
	Schedules      []string   `json:"schedules"`
	Stages         []Stage    `json:"stages"`
}

// Label is the display path of the pipeline.
func (r Record) Label() string {
	if r.Folder == `\` || r.Folder == "" {
		return r.Project + `\` + r.Name
	}
	return r.Project + r.Folder + `\` + r.Name
}

// Options controls what Build includes.
type Options struct {
	IncludeValues bool // include non-secret variable values
}

// Build normalizes a raw definition. Pool names are resolved through cfg when it can reach them.
func Build(cfg azuredevops.Config, project string, raw obj, o Options) Record {
	r := Record{Project: project, Folder: str(raw["path"]), Name: str(raw["name"]), ID: num(raw["id"]), Revision: num(raw["revision"])}
	r.Variables = variables(raw["variables"], o)
	for _, g := range list(raw["variableGroups"]) {
		if f, ok := g.(float64); ok {
			r.VariableGroups = append(r.VariableGroups, int(f))
		}
	}
	sort.Ints(r.VariableGroups)
	for _, a := range list(raw["artifacts"]) {
		am, _ := a.(obj)
		ref, _ := am["definitionReference"].(obj)
		src := ""
		for _, k := range []string{"definition", "repository", "artifactSourceDefinitionUrl"} {
			if m, ok := ref[k].(obj); ok && str(m["name"]) != "" {
				src = str(m["name"])
				break
			}
		}
		prim, _ := am["isPrimary"].(bool)
		r.Artifacts = append(r.Artifacts, Artifact{Alias: str(am["alias"]), Type: str(am["type"]), Source: src, Primary: prim})
	}
	sort.Slice(r.Artifacts, func(i, j int) bool { return r.Artifacts[i].Alias < r.Artifacts[j].Alias })
	for _, t := range list(raw["triggers"]) {
		tm, _ := t.(obj)
		tt := str(tm["triggerType"])
		if strings.EqualFold(tt, "schedule") {
			for _, s := range list(tm["schedules"]) {
				sm, _ := s.(obj)
				r.Schedules = append(r.Schedules, fmt.Sprintf("%02d:%02d %s days=%v", num(sm["startHours"]), num(sm["startMinutes"]), str(sm["timeZoneId"]), sm["daysToRelease"]))
			}
			continue
		}
		r.Triggers = append(r.Triggers, tt)
	}
	sort.Strings(r.Triggers)
	sort.Strings(r.Schedules)
	for _, e := range list(raw["environments"]) {
		em, _ := e.(obj)
		st := Stage{Name: str(em["name"]), Rank: num(em["rank"]), Variables: variables(em["variables"], o)}
		for _, c := range list(em["conditions"]) {
			cm, _ := c.(obj)
			st.Conditions = append(st.Conditions, str(cm["name"])+":"+str(cm["conditionType"])+":"+str(cm["value"]))
		}
		sort.Strings(st.Conditions)
		st.PreApprovals = approvals(em["preDeployApprovals"])
		st.PostApprovals = approvals(em["postDeployApprovals"])
		if rp, ok := em["retentionPolicy"].(obj); ok {
			ret := &Retention{Days: num(rp["daysToKeep"]), Releases: num(rp["releasesToKeep"])}
			if b, ok := rp["retainBuild"].(bool); ok {
				ret.RetainBuild = &b
			}
			st.Retention = ret
		}
		for _, p := range list(em["deployPhases"]) {
			pm, _ := p.(obj)
			job := Job{Name: str(pm["name"]), Type: str(pm["phaseType"])}
			if di, ok := pm["deploymentInput"].(obj); ok {
				if q := num(di["queueId"]); q != 0 {
					job.Pool = cfg.ResolvePoolName(project, q)
				}
				for _, d := range list(di["demands"]) {
					job.Demands = append(job.Demands, str(d))
				}
				sort.Strings(job.Demands)
				job.TimeoutMin = num(di["timeoutInMinutes"])
			}
			for _, t := range list(pm["workflowTasks"]) {
				tm, _ := t.(obj)
				en, ok := tm["enabled"].(bool)
				job.Tasks = append(job.Tasks, Task{Name: str(tm["name"]), TaskID: str(tm["taskId"]), Version: str(tm["version"]), Enabled: en || !ok})
			}
			st.Jobs = append(st.Jobs, job)
		}
		r.Stages = append(r.Stages, st)
	}
	sort.SliceStable(r.Stages, func(i, j int) bool { return r.Stages[i].Rank < r.Stages[j].Rank })
	return r
}

func variables(v interface{}, o Options) []Variable {
	m, _ := v.(obj)
	var out []Variable
	for name, item := range m {
		im, _ := item.(obj)
		sec, _ := im["isSecret"].(bool)
		ov, _ := im["allowOverride"].(bool)
		vr := Variable{Name: name, Secret: sec, AllowOverride: ov}
		if o.IncludeValues && !sec {
			vr.Value = str(im["value"])
		}
		out = append(out, vr)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func approvals(v interface{}) string {
	m, _ := v.(obj)
	var manual []string
	for _, a := range list(m["approvals"]) {
		am, _ := a.(obj)
		if auto, _ := am["isAutomated"].(bool); auto {
			continue
		}
		who := "unassigned"
		if ap, ok := am["approver"].(obj); ok {
			who = firstNonEmpty(str(ap["displayName"]), str(ap["uniqueName"]), who)
		}
		manual = append(manual, who)
	}
	if len(manual) == 0 {
		return "automatic"
	}
	sort.Strings(manual)
	return "manual: " + strings.Join(manual, ", ")
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func list(v interface{}) []interface{} { l, _ := v.([]interface{}); return l }

func str(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return fmt.Sprint(int(t))
	}
	return fmt.Sprint(v)
}

func num(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		var n int
		fmt.Sscanf(t, "%d", &n)
		return n
	}
	return 0
}

// Flatten reduces a record to comparable settings (key -> value). Identity (id, revision,
// artifact source names) is left out so equal configurations compare equal.
func (r Record) Flatten() map[string]string {
	out := map[string]string{}
	vars := func(prefix string, vs []Variable) {
		for _, v := range vs {
			val := "plain"
			if v.Secret {
				val = "secret"
			} else if v.Value != "" {
				val = "plain=" + v.Value
			}
			if v.AllowOverride {
				val += ", overridable"
			}
			out[prefix+"variable "+v.Name] = val
		}
	}
	vars("", r.Variables)
	for _, g := range r.VariableGroups {
		out[fmt.Sprintf("variable group %d", g)] = "linked"
	}
	for _, a := range r.Artifacts {
		out["artifact "+a.Alias] = fmt.Sprintf("type=%s primary=%t", a.Type, a.Primary)
	}
	for _, t := range r.Triggers {
		out["trigger "+t] = "yes"
	}
	for _, s := range r.Schedules {
		out["schedule "+s] = "yes"
	}
	for _, s := range r.Stages {
		p := "stage " + s.Name + ": "
		out[p+"exists"] = "yes"
		out[p+"rank"] = fmt.Sprint(s.Rank)
		if len(s.Conditions) > 0 {
			out[p+"conditions"] = strings.Join(s.Conditions, "; ")
		}
		out[p+"pre-approvals"] = s.PreApprovals
		out[p+"post-approvals"] = s.PostApprovals
		if s.Retention != nil {
			out[p+"retention days"] = fmt.Sprint(s.Retention.Days)
			out[p+"retention releases"] = fmt.Sprint(s.Retention.Releases)
			if s.Retention.RetainBuild != nil {
				out[p+"retention retain build"] = fmt.Sprint(*s.Retention.RetainBuild)
			}
		}
		vars(p, s.Variables)
		seenJobs := map[string]int{}
		for _, j := range s.Jobs {
			// Jobs may share a name; later ones get an occurrence suffix so none overwrites another.
			seenJobs[j.Name]++
			jobKey := j.Name
			if n := seenJobs[j.Name]; n > 1 {
				jobKey = fmt.Sprintf("%s #%d", j.Name, n)
			}
			jp := p + "job " + jobKey + ": "
			out[jp+"type"] = j.Type
			out[jp+"pool"] = j.Pool
			out[jp+"demands"] = strings.Join(j.Demands, "; ")
			out[jp+"timeout (min)"] = fmt.Sprint(j.TimeoutMin)
			for i, t := range j.Tasks {
				out[fmt.Sprintf("%stask %02d", jp, i+1)] = fmt.Sprintf("%s@%s enabled=%t", t.Name, t.Version, t.Enabled)
			}
		}
	}
	return out
}

// Collect reads the definitions and builds a record for each, sorted by label.
func Collect(cfg azuredevops.Config, project string, defs []azuredevops.ReleaseDefinition, o Options) ([]Record, error) {
	details, err := batchupdate.FetchDetails(cfg, project, defs)
	if err != nil {
		return nil, err
	}
	recs := make([]Record, 0, len(defs))
	for _, d := range defs {
		recs = append(recs, Build(cfg, project, details[d.ID], o))
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].Label() < recs[j].Label() })
	return recs, nil
}
