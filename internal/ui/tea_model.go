package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SelectionModel is a Bubble Tea model for selecting items from a list
type SelectionModel struct {
	Title    string
	Choices  []string
	Cursor   int
	Selected map[int]struct{}
	Quitting bool
}

// NewSelectionModel creates a new selection model
func NewSelectionModel(title string, choices []string) SelectionModel {
	return SelectionModel{
		Title:    title,
		Choices:  choices,
		Selected: make(map[int]struct{}),
	}
}

// Init initializes the model
func (m SelectionModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the model
func (m SelectionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.Quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.Cursor > 0 {
				m.Cursor--
			}

		case "down", "j":
			if m.Cursor < len(m.Choices)-1 {
				m.Cursor++
			}

		case "enter", " ":
			_, ok := m.Selected[m.Cursor]
			if ok {
				delete(m.Selected, m.Cursor)
			} else {
				m.Selected[m.Cursor] = struct{}{}
			}
		}
	}

	return m, nil
}

// View renders the UI
func (m SelectionModel) View() string {
	if m.Quitting {
		return ""
	}

	// Styles
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		MarginBottom(1)

	cursorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Bold(true)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("10"))

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		MarginTop(1)

	// Build the view
	s := titleStyle.Render(m.Title) + "\n\n"

	// Iterate over choices
	for i, choice := range m.Choices {
		cursor := "  "
		if m.Cursor == i {
			cursor = cursorStyle.Render("> ")
		}

		checked := "[ ]"
		if _, ok := m.Selected[i]; ok {
			checked = selectedStyle.Render("[x]")
		}

		s += fmt.Sprintf("%s%s %s\n", cursor, checked, choice)
	}

	// Add help text
	s += helpStyle.Render("\nPress ↑/↓ or k/j to move, space/enter to select, q to quit")

	return s
}

// GetSelectedIndices returns the indices of selected items
func (m SelectionModel) GetSelectedIndices() []int {
	indices := make([]int, 0, len(m.Selected))
	for i := range m.Selected {
		indices = append(indices, i)
	}
	return indices
}

// GetSelectedItems returns the selected items
func (m SelectionModel) GetSelectedItems() []string {
	items := make([]string, 0, len(m.Selected))
	for i := range m.Selected {
		if i < len(m.Choices) {
			items = append(items, m.Choices[i])
		}
	}
	return items
}

// RunSelection runs a selection UI and returns the selected items
func RunSelection(title string, choices []string) ([]string, error) {
	m := NewSelectionModel(title, choices)
	p := tea.NewProgram(m)

	finalModel, err := p.Run()
	if err != nil {
		return nil, err
	}

	if finalModel, ok := finalModel.(SelectionModel); ok {
		return finalModel.GetSelectedItems(), nil
	}

	return nil, fmt.Errorf("unexpected model type")
}
