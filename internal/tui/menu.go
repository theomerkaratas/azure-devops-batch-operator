package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// banner is the "AZURE DEVOPS BATCH OPERATOR" ASCII art title shown at the top of the menu screen.
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

// menuChrome is the number of rows used by everything on the menu screen except the command list.
const menuChrome = 17

// visible returns the indexes into commandSpecs that match the filter text.
func (m model) visible() []int {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	var idx []int
	for i, s := range commandSpecs {
		if q == "" || strings.Contains(strings.ToLower(s.id+" "+s.description), q) {
			idx = append(idx, i)
		}
	}
	return idx
}

func (m *model) moveMenu(delta int) {
	n := len(m.visible())
	if n == 0 {
		return
	}
	m.menuIdx = (m.menuIdx + delta + n) % n
}

// openSelected opens the form for the highlighted command, pre-filled with its last inputs.
func (m model) openSelected() (tea.Model, tea.Cmd) {
	vis := m.visible()
	if m.menuIdx >= len(vis) {
		return m, nil
	}

	spec := &commandSpecs[vis[m.menuIdx]]
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
	if _, cached := m.helpCache[spec.id]; !cached {
		cmds = append(cmds, loadHelp(spec))
	}
	return m, tea.Batch(cmds...)
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
	case "/":
		m.filtering = true
		return m, m.filter.Focus()
	case "h":
		m.state = stateHistory
	case "up", "k":
		m.moveMenu(-1)
	case "down", "j":
		m.moveMenu(1)
	case "enter":
		return m.openSelected()
	}
	return m, nil
}

func (m model) viewMenu() string {
	var b strings.Builder
	b.WriteString(bannerStyle.Render(banner))
	b.WriteString("\n")
	b.WriteString(descStyle.Render("Release pipeline toolkit"))
	b.WriteString("\n")
	if m.filtering {
		b.WriteString(m.filter.View())
	}
	b.WriteString("\n")

	vis := m.visible()
	maxID := 0
	for _, i := range vis {
		if n := len(commandSpecs[i].id); n > maxID {
			maxID = n
		}
	}

	// Show a window of the list that keeps the selection visible on short terminals.
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
		b.WriteString(descStyle.Render("  no commands match the filter"))
		b.WriteString("\n")
	}
	for pos := start; pos < end; pos++ {
		spec := commandSpecs[vis[pos]]
		line := truncate(fmt.Sprintf("%-*s  %s", maxID, spec.id, spec.description), m.width-4)
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
		b.WriteString(helpStyle.Render(fmt.Sprintf("↑/↓ or j/k: move • enter: select • /: filter • h: history (%d) • q/ctrl+c: quit", len(m.history))))
	}
	return b.String()
}
