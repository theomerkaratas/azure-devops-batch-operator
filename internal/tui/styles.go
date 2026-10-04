package tui

import "github.com/charmbracelet/lipgloss"

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	bannerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	descStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	errorStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	okStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))

	cellStyle = lipgloss.NewStyle()
	selCell   = cellStyle.Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("39"))

	labelStyle      = lipgloss.NewStyle().Width(22)
	focusLabelStyle = labelStyle.Bold(true).Foreground(lipgloss.Color("39"))

	runButtonStyle      = lipgloss.NewStyle().Padding(0, 2).Foreground(lipgloss.Color("255")).Background(lipgloss.Color("240"))
	runButtonFocusStyle = lipgloss.NewStyle().Padding(0, 2).Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("42"))
)
