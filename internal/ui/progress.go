package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ProgressModel is a Bubble Tea model with a progress bar
type ProgressModel struct {
	progress progress.Model
	message  string
	percent  float64
	quitting bool
	done     bool
}

// NewProgressModel creates a new progress model
func NewProgressModel(message string) ProgressModel {
	prog := progress.New(
		progress.WithDefaultGradient(),
		progress.WithWidth(40),
	)

	return ProgressModel{
		progress: prog,
		message:  message,
		percent:  0.0,
	}
}

// Init initializes the progress bar
func (m ProgressModel) Init() tea.Cmd {
	return nil
}

// progressMsg is sent to update progress
type progressMsg float64

// Update handles messages
func (m ProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}

	case progressMsg:
		m.percent = float64(msg)
		if m.percent >= 1.0 {
			m.done = true
			m.quitting = true
			return m, tea.Quit
		}

		cmd := m.progress.SetPercent(m.percent)
		return m, cmd

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd
	}

	return m, nil
}

// View renders the progress bar
func (m ProgressModel) View() string {
	if m.quitting {
		if m.done {
			return SuccessStyle.Render("✓ " + m.message + " completed")
		}
		return ""
	}

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Bold(true)

	percentStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12"))

	return fmt.Sprintf(
		"%s\n\n%s %s",
		titleStyle.Render(m.message),
		m.progress.View(),
		percentStyle.Render(fmt.Sprintf("%.0f%%", m.percent*100)),
	)
}

// UpdateProgress sends a progress update
func UpdateProgress(p *tea.Program, percent float64) {
	if p != nil {
		p.Send(progressMsg(percent))
	}
}

// SimpleProgressBar creates a simple text-based progress bar
func SimpleProgressBar(current, total int, width int) string {
	if total == 0 {
		return ""
	}

	percent := float64(current) / float64(total)
	filled := int(percent * float64(width))

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)

	style := lipgloss.NewStyle().Foreground(lipgloss.Color("12"))

	return fmt.Sprintf("%s %d/%d (%.0f%%)",
		style.Render(bar),
		current,
		total,
		percent*100,
	)
}
