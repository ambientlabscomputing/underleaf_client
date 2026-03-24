package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SpinnerModel is a Bubble Tea model with a spinner
type SpinnerModel struct {
	spinner  spinner.Model
	message  string
	quitting bool
	done     bool
	err      error
}

// NewSpinnerModel creates a new spinner model
func NewSpinnerModel(message string) SpinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))

	return SpinnerModel{
		spinner: s,
		message: message,
	}
}

// Init initializes the spinner
func (m SpinnerModel) Init() tea.Cmd {
	return m.spinner.Tick
}

// Update handles messages
func (m SpinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case doneMsg:
		m.done = true
		m.quitting = true
		m.err = msg.err
		return m, tea.Quit
	}

	return m, nil
}

// View renders the spinner
func (m SpinnerModel) View() string {
	if m.quitting {
		if m.err != nil {
			return ErrorStyle.Render("✗ " + m.message + " failed: " + m.err.Error())
		}
		if m.done {
			return SuccessStyle.Render("✓ " + m.message + " completed")
		}
		return ""
	}

	return fmt.Sprintf("%s %s", m.spinner.View(), m.message)
}

// doneMsg is sent when a long-running operation completes
type doneMsg struct {
	err error
}

// SendDone sends a done message
func SendDone(err error) tea.Msg {
	return doneMsg{err: err}
}

// ShowSpinner displays a spinner while running a function.
// In non-interactive mode, it runs the function directly without animation.
func ShowSpinner(message string, fn func() error) error {
	if !IsInteractive() {
		return fn()
	}

	m := NewSpinnerModel(message)
	p := tea.NewProgram(m)

	// Run the function in a goroutine
	go func() {
		time.Sleep(100 * time.Millisecond) // Give UI time to start
		err := fn()
		p.Send(doneMsg{err: err})
	}()

	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	if sm, ok := finalModel.(SpinnerModel); ok {
		return sm.err
	}

	return nil
}
