package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// outputChrome is the number of terminal rows used by the title, status, notice and help lines.
const outputChrome = 6

func (m *model) resizeViewport() {
	m.viewport.Width = m.width
	h := m.height - outputChrome
	if h < 1 {
		h = 1
	}
	m.viewport.Height = h
	m.rewrap()
}

// rewrap re-renders the stored output at the current viewport width.
func (m *model) rewrap() {
	m.viewport.SetContent(lipgloss.NewStyle().Width(m.viewport.Width).Render(strings.TrimRight(m.lastOutput, "\n")))
}
func (m *model) setOutput(text string) {
	m.lastOutput = text
	m.rewrap()
	m.viewport.GotoTop()
}
func saveOutput(id, text string) (string, error) {
	const dir = "output"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%s.txt", id, time.Now().Format("20060102-150405")))
	return path, os.WriteFile(path, []byte(text), 0o644)
}
func (m model) backToMenu() model {
	m.state = stateMenu
	m.fields = nil
	m.activeSpec = nil
	m.formErr = ""
	return m
}
func (m model) updateOutput(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case execDoneMsg:
		m.outputRunning = false
		m.lastErr = msg.err
		m.setOutput(msg.output)
		entry := historyEntry{spec: msg.spec, args: msg.args, output: msg.output, err: msg.err, at: time.Now()}
		m.history = append([]historyEntry{entry}, m.history...)
		m.histIdx = 0
		return m, nil
	case tea.KeyMsg:
		m.notice = ""
		if !m.outputRunning {
			switch msg.String() {
			case "s":
				if path, err := saveOutput(m.lastSpec.id, m.lastOutput); err != nil {
					m.notice = "Save failed: " + err.Error()
				} else {
					m.notice = "Saved to " + path
				}
				return m, nil
			case "c":
				if err := clipboard.WriteAll(m.lastOutput); err != nil {
					m.notice = "Copy failed: " + err.Error()
				} else {
					m.notice = "Copied to clipboard"
				}
				return m, nil
			case "esc", "q", "enter", "b":
				if m.fromHistory {
					m.state = stateHistory
				} else if k := msg.String(); k == "enter" || k == "b" {
					m.state = stateForm
				} else {
					return m.backToMenu(), nil
				}
				return m, nil
			case "m":
				return m.backToMenu(), nil
			}
		}
	}
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}
func (m model) viewOutput() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.lastSpec.id))
	b.WriteString("\n")
	switch {
	case m.outputRunning:
		b.WriteString(descStyle.Render("Running..."))
	case m.lastErr != nil:
		b.WriteString(errorStyle.Render("Failed: " + m.lastErr.Error()))
	default:
		b.WriteString(okStyle.Render("Done"))
	}
	b.WriteString("\n\n")
	b.WriteString(m.viewport.View())
	b.WriteString("\n")
	if m.notice != "" {
		b.WriteString(okStyle.Render(m.notice))
	}
	b.WriteString("\n")
	help := "↑/↓/pgup/pgdn: scroll • s: save to file • c: copy • enter/b: back to form • esc/q/m: main menu"
	if m.fromHistory {
		help = "↑/↓/pgup/pgdn: scroll • s: save to file • c: copy • esc/q/enter: back to history • m: main menu"
	}
	b.WriteString(helpStyle.Render(help))
	return b.String()
}
func (m model) updateHistory(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	n := len(m.history)
	switch keyMsg.String() {
	case "esc", "q", "m":
		m.state = stateMenu
	case "up", "k":
		if n > 0 {
			m.histIdx = (m.histIdx - 1 + n) % n
		}
	case "down", "j":
		if n > 0 {
			m.histIdx = (m.histIdx + 1) % n
		}
	case "enter":
		if m.histIdx < n {
			e := m.history[m.histIdx]
			m.lastSpec, m.lastErr = e.spec, e.err
			m.outputRunning = false
			m.fromHistory = true
			m.notice = ""
			m.state = stateOutput
			m.lastOutput = e.output
			m.resizeViewport()
			m.viewport.GotoTop()
		}
	}
	return m, nil
}
func (m model) viewHistory() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Run history"))
	b.WriteString("\n")
	b.WriteString(descStyle.Render("Outputs of commands run in this session"))
	b.WriteString("\n\n")
	if len(m.history) == 0 {
		b.WriteString(descStyle.Render("No runs yet."))
		b.WriteString("\n")
	}
	for i, e := range m.history {
		mark := "ok  "
		if e.err != nil {
			mark = "FAIL"
		}
		line := truncate(fmt.Sprintf("%s  %s  %s %s", e.at.Format("15:04:05"), mark, e.spec.id, strings.Join(e.args, " ")), m.width-4)
		if i == m.histIdx {
			b.WriteString(selCell.Render("▸ " + line))
		} else {
			b.WriteString(cellStyle.Render("  " + line))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓: move • enter: view output • esc/q: main menu"))
	return b.String()
}

// truncate cuts s to max runes with an ellipsis; max <= 0 means no limit.
func truncate(s string, max int) string {
	r := []rune(s)
	if max <= 0 || len(r) <= max {
		return s
	}
	if max < 2 {
		return string(r[:max])
	}
	return string(r[:max-1]) + "…"
}
