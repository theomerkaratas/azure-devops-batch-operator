package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/omerkaratas/azure-devops-go-automations/internal/azuredevops"
)

type appState int

const (
	stateMenu appState = iota
	stateForm
	stateOutput
	stateHistory
	statePicker
)

// historyEntry is one finished command run, kept so its output can be replayed.
type historyEntry struct {
	spec   *commandSpec
	args   []string
	output string
	err    error
	at     time.Time
}

type model struct {
	state    appState
	menuIdx  int
	menuPath []int
	width    int
	height   int

	filter    textinput.Model
	filtering bool

	activeSpec *commandSpec
	fields     []*field
	focusIdx   int
	formErr    string

	lastSpec      *commandSpec
	lastErr       error
	lastOutput    string
	outputRunning bool
	viewport      viewport.Model
	notice        string

	history     []historyEntry // newest first
	histIdx     int
	fromHistory bool

	// helpCache maps command id to its captured --help text.
	helpCache map[string]string
	inputs    savedInputs

	pick  *picker
	cache *pickCache
}

func initialModel() model {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "filter commands"
	return model{
		state:     stateMenu,
		helpCache: map[string]string{},
		inputs:    loadInputs(),
		filter:    ti,
		cache:     &pickCache{defs: map[string][]azuredevops.ReleaseDefinition{}},
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if wsMsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width, m.height = wsMsg.Width, wsMsg.Height
		if m.state == stateOutput {
			m.resizeViewport()
		}
		return m, nil
	}

	if hm, ok := msg.(helpMsg); ok {
		m.helpCache[hm.id] = hm.text
		return m, nil
	}

	switch msg.(type) {
	case projectsMsg, defsMsg:
		return m.handlePickData(msg), nil
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.state {
	case stateMenu:
		return m.updateMenu(msg)
	case stateForm:
		return m.updateForm(msg)
	case stateOutput:
		return m.updateOutput(msg)
	case stateHistory:
		return m.updateHistory(msg)
	case statePicker:
		return m.updatePicker(msg)
	}
	return m, nil
}

func (m model) View() string {
	switch m.state {
	case stateForm:
		return m.viewForm()
	case stateOutput:
		return m.viewOutput()
	case stateHistory:
		return m.viewHistory()
	case statePicker:
		return m.viewPicker()
	default:
		return m.viewMenu()
	}
}
