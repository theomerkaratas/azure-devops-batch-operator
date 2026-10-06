package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const banner = `
    _      _____ _   _ ____  _____   ____  _______     _____  ____  ____  
   / \    |__  /| | | |  _ \| ____| |  _ \| ____\ \   / / _ \|  _ \/ ___| 
  / _ \     / / | | | | |_) |  _|   | | | |  _|  \ \ / / | | | |_) \___ \ 
 / ___ \   / /_ | |_| |  _ <| |___  | |_| | |___  \ V /| |_| |  __/ ___) |
/_/   \_\ /____| \___/|_| \_\_____| |____/|_____|  \_/  \___/|_|   |____/ 

 ____    _  _____ ____ _   _    ___  ____  _____ ____      _  _____ ___  ____  
| __ )  / \|_   _/ ___| | | |  / _ \|  _ \| ____|  _ \    / \|_   _/ _ \|  _ \ 
|  _ \ / _ \ | || |   | |_| | | | | | |_) |  _| | |_) |  / _ \ | || | | | |_) |
| |_) / ___ \| || |___|  _  | | |_| |  __/| |___|  _ <  / ___ \| || |_| |  _ < 
|____/_/   \_\_| \____|_| |_|  \___/|_|   |_____|_| \_\/_/   \_\_| \___/|_| \_\
`

const menuChrome = 18

type menuNode struct {
	label       string
	description string
	commandID   string
	children    []menuNode
}

var menuTree = []menuNode{
	{label: "Create", description: "Create pipelines, releases, or local files.", children: []menuNode{
		{label: "PowerShell pipeline", description: "Create a release pipeline from PowerShell scripts.", commandID: "create-powershell-pipeline"},
		{label: "Trigger release", description: "Create releases for matching pipelines.", commandID: "trigger-release"},
		{label: "Create files", description: "Create empty local files and folders.", commandID: "create-files"},
	}},
	{label: "Clone", description: "Copy an existing release pipeline.", commandID: "clone-pipeline"},
	{label: "Read", description: "Inspect and compare Azure DevOps resources.", children: []menuNode{
		{label: "List", description: "List pipelines, releases, agent pools, and configuration.", children: []menuNode{
			{label: "Release pipelines", description: "List pipeline folders and definitions.", commandID: "list-releases"},
			{label: "Agent pools", description: "List pools and their agents.", commandID: "list-pools"},
			{label: "Pool members", description: "List agents belonging to matching pools.", commandID: "list-pool-members"},
			{label: "Pipeline variables", description: "List pipeline and stage variables.", commandID: "list-pipeline-variables"},
			{label: "Pipeline steps", description: "List stages, tasks, and scripts.", commandID: "list-pipeline-steps"},
			{label: "Pipeline schedule", description: "List scheduled triggers.", commandID: "list-pipeline-schedule"},
			{label: "Pipeline agent job", description: "List pool, demands, and timeouts.", commandID: "list-pipeline-agent-job"},
			{label: "Release history", description: "List recent releases and stage statuses.", commandID: "list-release-history"},
			{label: "Release status", description: "List latest status across pipelines.", commandID: "list-release-status"},
		}},
	}},
	{label: "Compare", description: "Compare pipelines or whole folders.", children: []menuNode{
		{label: "Compare pipelines", description: "Compare variables, jobs, and tasks between two pipelines.", commandID: "compare-pipelines"},
		{label: "Compare folders", description: "Compare release counts, names, and content between two folders.", commandID: "compare-folders"},
	}},
	{label: "Update", description: "Modify pipeline configuration and organization.", children: []menuNode{
		{label: "Pipeline variables", description: "Set or remove variables.", commandID: "update-pipeline-variables"},
		{label: "Pipeline schedule", description: "Set or pause scheduled triggers.", commandID: "update-pipeline-schedule"},
		{label: "Pipeline agent job", description: "Update pool and timeout settings.", commandID: "update-pipeline-agent-job"},
		{label: "Pipeline demands", description: "Set, add, remove, or clear demands.", commandID: "update-pipeline-demands"},
		{label: "Rename or move pipelines", description: "Rename pipelines or move them between folders.", commandID: "rename-or-move-pipelines"},
	}},
	{label: "Delete", description: "Permanently delete matching release pipelines.", commandID: "delete-pipelines"},
	{label: "Cancel", description: "Cancel deployments and optionally abandon releases.", commandID: "cancel-releases"},
	{label: "Backup", description: "Back up or restore release pipeline definitions.", children: []menuNode{
		{label: "Back up pipelines", description: "Save matching pipelines' definitions to local JSON files.", commandID: "backup-pipelines"},
		{label: "Restore pipelines", description: "Recreate or overwrite pipelines from backup JSON files.", commandID: "restore-pipelines"},
	}},
}

func (m model) currentMenu() []menuNode {
	nodes := menuTree
	for _, i := range m.menuPath {
		if i < 0 || i >= len(nodes) {
			return menuTree
		}
		nodes = nodes[i].children
	}
	return nodes
}

func (m model) breadcrumbs() string {
	nodes := menuTree
	parts := []string{"Home"}
	for _, i := range m.menuPath {
		if i < 0 || i >= len(nodes) {
			break
		}
		parts = append(parts, nodes[i].label)
		nodes = nodes[i].children
	}
	return strings.Join(parts, " / ")
}

func (m model) visible() []int {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	nodes := m.currentMenu()
	idx := make([]int, 0, len(nodes))
	for i, node := range nodes {
		if q == "" || strings.Contains(strings.ToLower(node.label+" "+node.description), q) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m *model) moveMenu(delta int) {
	n := len(m.visible())
	if n > 0 {
		m.menuIdx = (m.menuIdx + delta + n) % n
	}
}

func findCommandSpec(id string) *commandSpec {
	for i := range commandSpecs {
		if commandSpecs[i].id == id {
			return &commandSpecs[i]
		}
	}
	return nil
}

func (m model) openSelected() (tea.Model, tea.Cmd) {
	vis := m.visible()
	if m.menuIdx >= len(vis) {
		return m, nil
	}
	selected := vis[m.menuIdx]
	node := m.currentMenu()[selected]
	if len(node.children) > 0 {
		m.menuPath = append(m.menuPath, selected)
		m.menuIdx = 0
		m.filtering = false
		m.filter.SetValue("")
		m.filter.Blur()
		return m, nil
	}

	spec := findCommandSpec(node.commandID)
	if spec == nil {
		return m, nil
	}
	m.activeSpec = spec
	m.fields = spec.newFields()
	for _, f := range m.fields {
		f.pick = pickModes[f.key]
	}
	m.inputs.apply(spec.id, m.fields)
	m.focusIdx = 0
	m.formErr = ""
	var cmds []tea.Cmd
	if len(m.fields) > 0 && m.fields[0].kind == fieldText {
		cmds = append(cmds, m.fields[0].input.Focus())
	}
	m.state = stateForm
	return m, tea.Batch(cmds...)
}

func (m model) backMenu() model {
	if len(m.menuPath) > 0 {
		m.menuPath = m.menuPath[:len(m.menuPath)-1]
	}
	m.menuIdx = 0
	m.filtering = false
	m.filter.SetValue("")
	m.filter.Blur()
	return m
}

func (m model) updateMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	key := keyMsg.String()

	if m.filtering {
		switch key {
		case "esc":
			m.filtering = false
			m.filter.SetValue("")
			m.filter.Blur()
			m.menuIdx = 0
			return m, nil
		case "up":
			m.moveMenu(-1)
			return m, nil
		case "down":
			m.moveMenu(1)
			return m, nil
		case "enter":
			return m.openSelected()
		}
		before := m.filter.Value()
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		if m.filter.Value() != before {
			m.menuIdx = 0
		}
		return m, cmd
	}

	switch key {
	case "q":
		return m, tea.Quit
	case "esc", "backspace", "left":
		if len(m.menuPath) > 0 {
			m = m.backMenu()
		}
	case "h":
		m.state = stateHistory
	case "/":
		m.filtering = true
		return m, m.filter.Focus()
	case "up", "k":
		m.moveMenu(-1)
	case "down", "j":
		m.moveMenu(1)
	case "enter", "right", "l":
		return m.openSelected()
	}
	return m, nil
}

func (m model) viewMenu() string {
	var b strings.Builder
	b.WriteString(bannerStyle.Render(banner))
	b.WriteString("\n")
	b.WriteString(descStyle.Render("Release pipeline toolkit  •  " + m.breadcrumbs()))
	b.WriteString("\n")
	if m.filtering {
		b.WriteString(m.filter.View())
	}
	b.WriteString("\n")

	nodes := m.currentMenu()
	vis := m.visible()
	maxLabel := 0
	for _, i := range vis {
		if n := len(nodes[i].label); n > maxLabel {
			maxLabel = n
		}
	}
	start, end := 0, len(vis)
	if rows := m.height - menuChrome; m.height > 0 && rows >= 3 && rows < len(vis) {
		start = m.menuIdx - rows/2
		if start < 0 {
			start = 0
		}
		if start+rows > len(vis) {
			start = len(vis) - rows
		}
		end = start + rows
	}
	if len(vis) == 0 {
		b.WriteString(descStyle.Render("  no actions match the filter") + "\n")
	}
	for pos := start; pos < end; pos++ {
		node := nodes[vis[pos]]
		// Pad the label to a fixed width first, then append the chevron, so the chevron
		// lands in the same column for every row regardless of label length. Every entry
		// gets a chevron, whether it opens a submenu or jumps straight to a command's form.
		line := truncate(fmt.Sprintf("%-*s ›  %s", maxLabel, node.label, node.description), m.width-4)
		if pos == m.menuIdx {
			b.WriteString(selCell.Render("▸ " + line))
		} else {
			b.WriteString(cellStyle.Render("  " + line))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	if m.filtering {
		b.WriteString(helpStyle.Render("type to filter • ↑/↓: move • enter: select • esc: clear filter"))
	} else {
		back := ""
		if len(m.menuPath) > 0 {
			back = " • esc/←: back"
		}
		b.WriteString(helpStyle.Render(fmt.Sprintf("↑/↓ or j/k: move • enter/→: select%s • /: filter • h: history (%d) • q: quit", back, len(m.history))))
	}
	return b.String()
}
