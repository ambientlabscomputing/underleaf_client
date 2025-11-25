package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// ErrorFormatter formats errors for terminal output
type ErrorFormatter struct {
	style lipgloss.Style
}

// NewErrorFormatter creates a new error formatter
func NewErrorFormatter() *ErrorFormatter {
	return &ErrorFormatter{
		style: lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true).
			Padding(0, 1),
	}
}

// Format formats an error message
func (ef *ErrorFormatter) Format(err error) string {
	if err == nil {
		return ""
	}
	return ef.style.Render(fmt.Sprintf("Error: %s", err.Error()))
}

// FormatWithContext formats an error with additional context
func (ef *ErrorFormatter) FormatWithContext(err error, context string) string {
	if err == nil {
		return ""
	}

	contextStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Italic(true)

	return ef.style.Render(fmt.Sprintf("Error: %s", err.Error())) +
		"\n" + contextStyle.Render(context)
}

// FormatError is a convenience function to format an error
func FormatError(err error) string {
	return NewErrorFormatter().Format(err)
}

// FormatErrorWithContext is a convenience function to format an error with context
func FormatErrorWithContext(err error, context string) string {
	return NewErrorFormatter().FormatWithContext(err, context)
}

// ErrorBox creates a boxed error message
func ErrorBox(title string, message string) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("9")).
		Padding(1, 2).
		Width(60)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("9")).
		Bold(true)

	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15"))

	content := titleStyle.Render(title) + "\n\n" + messageStyle.Render(message)
	return boxStyle.Render(content)
}

// WarningBox creates a boxed warning message
func WarningBox(title string, message string) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("11")).
		Padding(1, 2).
		Width(60)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("11")).
		Bold(true)

	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15"))

	content := titleStyle.Render(title) + "\n\n" + messageStyle.Render(message)
	return boxStyle.Render(content)
}

// InfoBox creates a boxed info message
func InfoBox(title string, message string) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("12")).
		Padding(1, 2).
		Width(60)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Bold(true)

	messageStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15"))

	content := titleStyle.Render(title) + "\n\n" + messageStyle.Render(message)
	return boxStyle.Render(content)
}
