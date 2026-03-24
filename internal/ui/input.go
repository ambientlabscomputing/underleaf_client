package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ErrNotInteractive is returned when an interactive prompt is required but stdout is not a TTY
var ErrNotInteractive = fmt.Errorf("interactive terminal required; provide values via flags instead")

// InputModel is a Bubble Tea model for text input
type InputModel struct {
	textInput textinput.Model
	prompt    string
	value     string
	quitting  bool
	submitted bool
}

// NewInputModel creates a new input model
func NewInputModel(prompt, placeholder string) InputModel {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 50

	return InputModel{
		textInput: ti,
		prompt:    prompt,
	}
}

// Init initializes the input
func (m InputModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages
func (m InputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit

		case "enter":
			m.value = m.textInput.Value()
			m.submitted = true
			m.quitting = true
			return m, tea.Quit
		}
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

// View renders the input
func (m InputModel) View() string {
	if m.quitting {
		return ""
	}

	promptStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Bold(true).
		MarginBottom(1)

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		MarginTop(1)

	return promptStyle.Render(m.prompt) + "\n" +
		m.textInput.View() + "\n" +
		helpStyle.Render("Press enter to submit, esc to cancel")
}

// GetValue returns the submitted value
func (m InputModel) GetValue() string {
	return m.value
}

// PromptInput shows an input prompt and returns the user's input.
// Returns ErrNotInteractive if stdout is not a TTY.
func PromptInput(prompt, placeholder string) (string, error) {
	if !IsInteractive() {
		return "", ErrNotInteractive
	}
	m := NewInputModel(prompt, placeholder)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	if im, ok := finalModel.(InputModel); ok {
		if im.submitted {
			return im.GetValue(), nil
		}
	}

	return "", nil
}

// PromptSecret shows a masked input prompt (for passwords/secrets) and returns the value.
// Returns ErrNotInteractive if stdout is not a TTY.
func PromptSecret(prompt string) (string, error) {
	if !IsInteractive() {
		return "", ErrNotInteractive
	}
	ti := textinput.New()
	ti.Placeholder = "••••••••"
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.Focus()
	ti.CharLimit = 4096
	ti.Width = 50

	m := InputModel{
		textInput: ti,
		prompt:    prompt,
	}

	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	if im, ok := finalModel.(InputModel); ok {
		if im.submitted {
			return im.GetValue(), nil
		}
	}

	return "", nil
}

// ConfirmModel is a Bubble Tea model for yes/no confirmation
type ConfirmModel struct {
	prompt   string
	selected bool
	quitting bool
	answered bool
}

// NewConfirmModel creates a new confirmation model
func NewConfirmModel(prompt string) ConfirmModel {
	return ConfirmModel{
		prompt:   prompt,
		selected: true, // Default to "yes"
	}
}

// Init initializes the confirmation
func (m ConfirmModel) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (m ConfirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.quitting = true
			return m, tea.Quit

		case "left", "h", "right", "l":
			m.selected = !m.selected

		case "y":
			m.selected = true
			m.answered = true
			m.quitting = true
			return m, tea.Quit

		case "n":
			m.selected = false
			m.answered = true
			m.quitting = true
			return m, tea.Quit

		case "enter":
			m.answered = true
			m.quitting = true
			return m, tea.Quit
		}
	}

	return m, nil
}

// View renders the confirmation
func (m ConfirmModel) View() string {
	if m.quitting {
		return ""
	}

	promptStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Bold(true).
		MarginBottom(1)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10")).
		Bold(true)

	unselectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		MarginTop(1)

	yes := "Yes"
	no := "No"

	if m.selected {
		yes = selectedStyle.Render("> " + yes)
		no = unselectedStyle.Render("  " + no)
	} else {
		yes = unselectedStyle.Render("  " + yes)
		no = selectedStyle.Render("> " + no)
	}

	return promptStyle.Render(m.prompt) + "\n\n" +
		yes + "  " + no + "\n" +
		helpStyle.Render("Use arrow keys or y/n to select, enter to confirm")
}

// GetAnswer returns whether the user confirmed
func (m ConfirmModel) GetAnswer() bool {
	return m.answered && m.selected
}

// Confirm shows a yes/no prompt and returns the user's answer.
// Returns ErrNotInteractive if stdout is not a TTY.
func Confirm(prompt string) (bool, error) {
	if !IsInteractive() {
		return false, ErrNotInteractive
	}
	m := NewConfirmModel(prompt)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return false, err
	}

	if cm, ok := finalModel.(ConfirmModel); ok {
		return cm.GetAnswer(), nil
	}

	return false, nil
}
