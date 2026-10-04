// Package tui is the interactive terminal UI: pick an operation, fill in its arguments (with a
// path picker for Azure DevOps paths) and see the output.
package tui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// Run starts the TUI and blocks until it exits.
func Run() error {
	_, err := tea.NewProgram(initialModel(), tea.WithAltScreen()).Run()
	return err
}

// selfExe is the path of the running binary; commands run as subprocesses of it (`<self> <command> ...`).
func selfExe() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return os.Args[0]
}
