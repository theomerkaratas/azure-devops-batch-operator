package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

// pickMode says what a path field accepts from the picker; pickNone means a plain text field.
type pickMode int

const (
	pickNone        pickMode = iota
	pickAny                  // project, folder or pipeline: Project[\Folder[\Pipeline]]
	pickPipeline             // a pipeline only: Project\Folder\Pipeline
	pickNewPipeline          // a folder to put a new pipeline in; result ends with '\' for the name
	pickFolder               // a folder without the project name: \Folder\Sub (or \ for the root)
)

type itemKind int

const (
	kindProject itemKind = iota
	kindFolder
	kindPipeline
)

type pickItem struct {
	name string
	kind itemKind
}

// pickCache keeps fetched projects and definitions so reopening the picker is instant.
type pickCache struct {
	projects []string
	defs     map[string][]azuredevops.ReleaseDefinition
}

type picker struct {
	fieldIdx int
	mode     pickMode
	level    string
	cache    *pickCache

	project string // empty while listing projects
	folder  string // current folder inside the project, `\` for its root
	loading bool
	err     string
	idx     int
}

type projectsMsg struct {
	names []string
	err   error
}

type defsMsg struct {
	project string
	defs    []azuredevops.ReleaseDefinition
	err     error
}

func fetchProjects(level string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := azuredevops.LoadConfig(level)
		if err != nil {
			return projectsMsg{err: err}
		}
		names, err := cfg.ListProjects()
		return projectsMsg{names: names, err: err}
	}
}

func fetchDefs(level, project string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := azuredevops.LoadConfig(level)
		if err != nil {
			return defsMsg{project: project, err: err}
		}
		defs, err := cfg.ListReleaseDefinitions(project)
		return defsMsg{project: project, defs: defs, err: err}
	}
}

func lessFold(a, b string) bool { return strings.ToLower(a) < strings.ToLower(b) }

func joinFolder(folder, name string) string {
	if folder == `\` {
		return `\` + name
	}
	return folder + `\` + name
}

func parentFolder(folder string) (parent, leaf string) {
	i := strings.LastIndex(folder, `\`)
	leaf = folder[i+1:]
	if i <= 0 {
		return `\`, leaf
	}
	return folder[:i], leaf
}

// items lists what is shown at the current level: projects, or sub-folders then pipelines.
func (p *picker) items() []pickItem {
	var out []pickItem
	if p.project == "" {
		names := append([]string(nil), p.cache.projects...)
		sort.Slice(names, func(i, j int) bool { return lessFold(names[i], names[j]) })
		for _, n := range names {
			out = append(out, pickItem{n, kindProject})
		}
		return out
	}

	prefix := p.folder
	if prefix != `\` {
		prefix += `\`
	}
	seen := map[string]bool{}
	var folders, pipes []string
	for _, d := range p.cache.defs[p.project] {
		path := d.Path
		if path == "" {
			path = `\`
		}
		switch {
		case path == p.folder:
			pipes = append(pipes, d.Name)
		case strings.HasPrefix(path, prefix):
			seg := strings.SplitN(strings.TrimPrefix(path, prefix), `\`, 2)[0]
			if !seen[seg] {
				seen[seg] = true
				folders = append(folders, seg)
			}
		}
	}
	sort.Slice(folders, func(i, j int) bool { return lessFold(folders[i], folders[j]) })
	sort.Slice(pipes, func(i, j int) bool { return lessFold(pipes[i], pipes[j]) })
	for _, f := range folders {
		out = append(out, pickItem{f, kindFolder})
	}
	if p.mode == pickAny || p.mode == pickPipeline {
		for _, n := range pipes {
			out = append(out, pickItem{n, kindPipeline})
		}
	}
	return out
}

// selectValue returns the field value for choosing a project/folder (container) or a pipeline.
func (p *picker) selectValue(project, folder, name string, kind itemKind) (string, bool) {
	full := project
	if folder != `\` {
		full += folder
	}
	if kind == kindPipeline {
		return full + `\` + name, p.mode == pickAny || p.mode == pickPipeline
	}
	switch p.mode {
	case pickAny:
		return full, true
	case pickNewPipeline:
		return full + `\`, true
	case pickFolder:
		return folder, true
	}
	return "", false
}

func (p *picker) selectItem(it pickItem) (string, bool) {
	switch it.kind {
	case kindProject:
		return p.selectValue(it.name, `\`, "", kindFolder)
	case kindFolder:
		return p.selectValue(p.project, joinFolder(p.folder, it.name), "", kindFolder)
	default:
		return p.selectValue(p.project, p.folder, it.name, kindPipeline)
	}
}

func (p *picker) move(delta, n int) {
	if n > 0 {
		p.idx = (p.idx + delta + n) % n
	}
}

// descend enters a project or folder, fetching its definitions when not cached yet.
func (p *picker) descend(it pickItem) tea.Cmd {
	p.err = ""
	p.idx = 0
	if it.kind == kindProject {
		p.project, p.folder = it.name, `\`
		if _, ok := p.cache.defs[it.name]; !ok {
			p.loading = true
			return fetchDefs(p.level, it.name)
		}
		return nil
	}
	p.folder = joinFolder(p.folder, it.name)
	return nil
}

// ascend goes up one level and re-selects the entry it came from.
func (p *picker) ascend() {
	if p.project == "" {
		return
	}
	var leave string
	if p.folder == `\` {
		leave = p.project
		p.project = ""
	} else {
		p.folder, leave = parentFolder(p.folder)
	}
	p.err = ""
	p.idx = 0
	for i, it := range p.items() {
		if it.name == leave {
			p.idx = i
		}
	}
}

func (m model) pickerLevel() string {
	for _, f := range m.fields {
		if f.key == "level" {
			return f.value()
		}
	}
	return "read"
}

func (m model) openPicker() (tea.Model, tea.Cmd) {
	f := m.fields[m.focusIdx]
	p := &picker{fieldIdx: m.focusIdx, mode: f.pick, level: m.pickerLevel(), cache: m.cache}
	m.pick = p
	m.state = statePicker
	if m.cache.projects == nil {
		p.loading = true
		return m, fetchProjects(p.level)
	}
	return m, nil
}

// handlePickData applies fetched projects/definitions to the open picker.
func (m model) handlePickData(msg tea.Msg) model {
	switch msg := msg.(type) {
	case projectsMsg:
		if msg.err != nil {
			if m.pick != nil {
				m.pick.loading, m.pick.err = false, msg.err.Error()
			}
			return m
		}
		m.cache.projects = msg.names
		if m.pick != nil {
			m.pick.loading = false
		}
	case defsMsg:
		if msg.err != nil {
			if m.pick != nil && m.pick.project == msg.project {
				m.pick.project, m.pick.loading, m.pick.err = "", false, msg.err.Error()
			}
			return m
		}
		m.cache.defs[msg.project] = msg.defs
		if m.pick != nil && m.pick.project == msg.project {
			m.pick.loading = false
		}
	}
	return m
}

func (m model) closePicker(value string, chosen bool) (tea.Model, tea.Cmd) {
	idx := m.pick.fieldIdx
	m.pick = nil
	m.state = stateForm
	if chosen {
		m.fields[idx].input.SetValue(value)
	}
	return m, m.setFocus(idx)
}

func (m model) updatePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok || m.pick == nil {
		return m, nil
	}
	p := m.pick
	key := keyMsg.String()

	if key == "esc" || key == "q" {
		return m.closePicker("", false)
	}
	if p.loading {
		if key == "left" || key == "h" {
			p.ascend()
			p.loading = false
		}
		return m, nil
	}

	items := p.items()
	var sel *pickItem
	if p.idx >= 0 && p.idx < len(items) {
		sel = &items[p.idx]
	}

	switch key {
	case "up", "k":
		p.move(-1, len(items))
	case "down", "j":
		p.move(1, len(items))
	case "left", "h", "backspace":
		p.ascend()
	case "right", "l":
		if sel != nil && sel.kind != kindPipeline {
			return m, p.descend(*sel)
		}
	case "enter":
		if sel == nil {
			return m, nil
		}
		if v, ok := p.selectItem(*sel); ok {
			return m.closePicker(v, true)
		}
		if sel.kind != kindPipeline {
			return m, p.descend(*sel)
		}
	case " ":
		if p.project != "" {
			if v, ok := p.selectValue(p.project, p.folder, "", kindFolder); ok {
				return m.closePicker(v, true)
			}
		}
	}
	return m, nil
}

func (m model) viewPicker() string {
	p := m.pick
	var b strings.Builder
	b.WriteString(titleStyle.Render("Select " + m.fields[p.fieldIdx].label))
	b.WriteString("\n")

	where := "Projects"
	if p.project != "" {
		where = p.project
		if p.folder != `\` {
			where += p.folder
		}
	}
	b.WriteString(descStyle.Render(where))
	b.WriteString("\n\n")

	switch {
	case p.loading:
		b.WriteString(descStyle.Render("Loading..."))
		b.WriteString("\n")
	case p.err != "":
		b.WriteString(errorStyle.Render("Error: " + truncate(p.err, m.width*3)))
		b.WriteString("\n")
	}

	items := p.items()
	if !p.loading && len(items) == 0 && p.err == "" {
		b.WriteString(descStyle.Render("  (empty)"))
		b.WriteString("\n")
	}

	start, end := 0, len(items)
	if rows := m.height - 9; m.height > 0 && rows >= 3 && rows < len(items) {
		start = p.idx - rows/2
		if start < 0 {
			start = 0
		}
		if start+rows > len(items) {
			start = len(items) - rows
		}
		end = start + rows
	}
	for i := start; i < end && !p.loading; i++ {
		name := items[i].name
		if items[i].kind != kindPipeline {
			name += "/"
		}
		line := truncate(name, m.width-4)
		if i == p.idx {
			b.WriteString(selCell.Render("▸ " + line))
		} else {
			b.WriteString(cellStyle.Render("  " + line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	hint := "↑/↓: move • →: open • ←: back • enter: choose • esc: cancel"
	if p.project != "" {
		hint = "↑/↓: move • →: open • ←: back • enter: choose • space: choose this folder • esc: cancel"
	}
	b.WriteString(helpStyle.Render(hint))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(fmt.Sprintf("accepts: %s", pickModeLabel(p.mode))))
	return b.String()
}

func pickModeLabel(m pickMode) string {
	switch m {
	case pickPipeline:
		return "a pipeline (folders open with enter or →)"
	case pickNewPipeline:
		return "a folder for the new pipeline; type its name after the path"
	case pickFolder:
		return "a folder (without the project name)"
	default:
		return "a project, folder or pipeline"
	}
}
