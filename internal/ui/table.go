package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// TableBuilder helps build and render tables
type TableBuilder struct {
	headers []string
	rows    [][]string
	title   string
}

// NewTableBuilder creates a new table builder
func NewTableBuilder() *TableBuilder {
	return &TableBuilder{
		headers: []string{},
		rows:    [][]string{},
	}
}

// WithTitle sets the table title
func (tb *TableBuilder) WithTitle(title string) *TableBuilder {
	tb.title = title
	return tb
}

// WithHeaders sets the table headers
func (tb *TableBuilder) WithHeaders(headers ...string) *TableBuilder {
	tb.headers = headers
	return tb
}

// AddRow adds a row to the table
func (tb *TableBuilder) AddRow(cells ...string) *TableBuilder {
	tb.rows = append(tb.rows, cells)
	return tb
}

// Render renders the table as a string
func (tb *TableBuilder) Render() string {
	if len(tb.headers) == 0 && len(tb.rows) == 0 {
		return "No data to display"
	}

	// Create styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("12")).
		Align(lipgloss.Left)

	cellStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Align(lipgloss.Left)

	borderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	// Create table
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(borderStyle).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == 0 {
				return headerStyle
			}
			return cellStyle
		})

	// Add headers
	if len(tb.headers) > 0 {
		t.Headers(tb.headers...)
	}

	// Add rows
	for _, row := range tb.rows {
		t.Row(row...)
	}

	output := t.String()

	// Add title if present
	if tb.title != "" {
		titleStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("14")).
			MarginBottom(1)
		output = titleStyle.Render(tb.title) + "\n" + output
	}

	return output
}

// Print prints the table to stdout
func (tb *TableBuilder) Print() {
	fmt.Println(tb.Render())
}

// SimpleTable creates and prints a simple table
func SimpleTable(headers []string, rows [][]string) string {
	tb := NewTableBuilder().WithHeaders(headers...)
	for _, row := range rows {
		tb.AddRow(row...)
	}
	return tb.Render()
}

// KeyValueTable creates a simple key-value table
func KeyValueTable(data map[string]string) string {
	tb := NewTableBuilder().WithHeaders("Key", "Value")
	for k, v := range data {
		tb.AddRow(k, v)
	}
	return tb.Render()
}

// FormatList formats a list with bullet points
func FormatList(items []string) string {
	var b strings.Builder
	bullet := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render("•")

	for _, item := range items {
		b.WriteString(fmt.Sprintf("%s %s\n", bullet, item))
	}

	return b.String()
}
