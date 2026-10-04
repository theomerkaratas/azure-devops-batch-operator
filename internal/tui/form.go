package tui

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// execDoneMsg is delivered once the spawned subprocess exits.
type execDoneMsg struct {
	spec   *commandSpec
	args   []string
	output string
	err    error
}

// helpMsg carries a command's captured --help output.
type helpMsg struct {
	id   string
	text string
}

func loadHelp(spec *commandSpec) tea.Cmd {
	return func() tea.Msg {
		out, _ := exec.Command(selfExe(), spec.id, "--help").CombinedOutput()
		return helpMsg{id: spec.id, text: string(out)}
	}
}

// setFocus blurs every field, focuses the one at idx (if it's a text field) and updates
// m.focusIdx. idx == len(m.fields) represents the "Run" button.
func (m *model) setFocus(idx int) tea.Cmd {
	for _, f := range m.fields {
		if f.kind == fieldText {
			f.input.Blur()
		}
	}
	m.focusIdx = idx
	if idx < len(m.fields) && m.fields[idx].kind == fieldText {
		return m.fields[idx].input.Focus()
	}
	return nil
}

func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	n := len(m.fields) + 1 // + Run button
	switch keyMsg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.state = stateMenu
		m.fields = nil
		m.activeSpec = nil
		m.formErr = ""
		return m, nil
	case "tab", "down":
		cmd := m.setFocus((m.focusIdx + 1) % n)
		return m, cmd
	case "shift+tab", "up":
		cmd := m.setFocus((m.focusIdx - 1 + n) % n)
		return m, cmd
	case "left", "right":
		if m.focusIdx < len(m.fields) {
			f := m.fields[m.focusIdx]
			if f.kind == fieldChoice {
				delta := 1
				if keyMsg.String() == "left" {
					delta = -1
				}
				f.choiceIdx = (f.choiceIdx + delta + len(f.choices)) % len(f.choices)
				return m, nil
			}
		}
	case "enter":
		if m.focusIdx < len(m.fields) && m.fields[m.focusIdx].pick != pickNone {
			return m.openPicker()
		}
		if m.focusIdx == len(m.fields) {
			return m.runSelected()
		}
		cmd := m.setFocus((m.focusIdx + 1) % n)
		return m, cmd
	}

	// Any other key (typed characters, backspace, left/right cursor movement, etc.) goes to
	// the focused text field, if there is one.
	if m.focusIdx < len(m.fields) && m.fields[m.focusIdx].kind == fieldText {
		var cmd tea.Cmd
		m.fields[m.focusIdx].input, cmd = m.fields[m.focusIdx].input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// runSelected validates the form, builds the subprocess' arguments and hands execution off to
// tea.ExecProcess, which suspends the TUI and lets the subprocess use the real terminal
// directly (so its own prompts, like update-pipeline-demands' confirmation, work as-is).
func (m model) runSelected() (tea.Model, tea.Cmd) {
	values := make(map[string]string, len(m.fields))
	for _, f := range m.fields {
		values[f.key] = f.value()
	}

	for _, f := range m.fields {
		if f.required && values[f.key] == "" {
			m.formErr = fmt.Sprintf("%s is required", f.label)
			return m, nil
		}
	}

	args, err := m.activeSpec.buildArgs(values)
	if err != nil {
		m.formErr = err.Error()
		return m, nil
	}
	m.formErr = ""
	m.inputs.remember(m.activeSpec.id, values)

	spec := m.activeSpec
	m.lastSpec = spec
	m.lastErr = nil
	m.lastOutput = ""
	m.fromHistory = false
	m.notice = ""
	m.outputRunning = true
	m.state = stateOutput
	m.viewport = viewport.New(0, 0)
	m.viewport.SetContent("")
	m.resizeViewport()

	c := exec.Command(selfExe(), append([]string{spec.id}, args...)...)
	return m, func() tea.Msg {
		out, err := c.CombinedOutput()
		return execDoneMsg{spec: spec, args: args, output: string(out), err: err}
	}
}

func (m model) viewForm() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.activeSpec.id))
	b.WriteString("\n")
	b.WriteString(descStyle.Render(m.activeSpec.description))
	b.WriteString("\n\n")

	for i, f := range m.fields {
		focused := i == m.focusIdx
		prefix := "  "
		lbl := labelStyle
		if focused {
			prefix = "> "
			lbl = focusLabelStyle
		}

		var value string
		if f.kind == fieldText {
			value = f.input.View()
			if focused && f.pick != pickNone {
				value += " " + helpStyle.Render("[enter: browse]")
			}
		} else {
			value = "‹ " + f.value() + " ›"
		}
		b.WriteString(prefix + lbl.Render(f.label) + " " + value + "\n")
	}

	b.WriteString("\n")
	runStyle := runButtonStyle
	if m.focusIdx == len(m.fields) {
		runStyle = runButtonFocusStyle
	}
	b.WriteString(runStyle.Render("[ Run ]"))
	b.WriteString("\n")

	if m.formErr != "" {
		b.WriteString("\n" + errorStyle.Render("Error: "+m.formErr) + "\n")
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("tab/↓/↑: move • ←/→: change choice • enter: browse path/next/run • esc: back"))

	b.WriteString("\n\n")
	if text, ok := m.helpCache[m.activeSpec.id]; ok {
		b.WriteString(descStyle.Render(strings.TrimRight(text, "\n")))
	} else {
		b.WriteString(descStyle.Render("Loading help..."))
	}
	return b.String()
}
