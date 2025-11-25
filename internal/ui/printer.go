package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

// OutputFormat represents the output format for printing
type OutputFormat string
type PrinterKey struct{}

const (
	FormatTable OutputFormat = "table"
	FormatJSON  OutputFormat = "json"
	FormatYAML  OutputFormat = "yaml"
	FormatWide  OutputFormat = "wide"
)

// Printer handles output formatting and printing
type Printer struct {
	writer io.Writer
	format OutputFormat
}

// NewPrinter creates a new Printer with the specified format
func NewPrinter(format OutputFormat) *Printer {
	return &Printer{
		writer: os.Stdout,
		format: format,
	}
}

func NewPrinterToContext(ctx context.Context, format OutputFormat) (context.Context, *Printer) {
	printer := NewPrinter(format)
	ctx = context.WithValue(ctx, PrinterKey{}, printer)
	return ctx, printer
}

func GetPrinter(ctx context.Context) *Printer {
	if printer, ok := ctx.Value(PrinterKey{}).(*Printer); ok {
		return printer
	}
	return NewPrinter(FormatTable)
}

// WithWriter sets a custom writer for the printer
func (p *Printer) WithWriter(w io.Writer) *Printer {
	p.writer = w
	return p
}

// Print outputs the data in the configured format
func (p *Printer) Print(data interface{}) error {
	// If it's a string, just print it directly
	if str, ok := data.(string); ok {
		fmt.Fprintln(p.writer, str)
		return nil
	}

	switch p.format {
	case FormatJSON:
		return p.printJSON(data)
	case FormatYAML:
		return p.printYAML(data)
	case FormatTable:
		return p.printTable(data)
	case FormatWide:
		return p.printWide(data)
	default:
		fmt.Fprintf(p.writer, "%v\n", data)
		return nil
	}
}

func (p *Printer) printJSON(data interface{}) error {
	encoder := json.NewEncoder(p.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

func (p *Printer) printYAML(data interface{}) error {
	encoder := yaml.NewEncoder(p.writer)
	encoder.SetIndent(2)
	defer encoder.Close()
	return encoder.Encode(data)
}

func (p *Printer) printTable(data interface{}) error {
	// For simple strings, just print them
	if str, ok := data.(string); ok {
		fmt.Fprintln(p.writer, str)
		return nil
	}
	// For complex data, try to format as JSON by default
	return p.printJSON(data)
}

func (p *Printer) printWide(data interface{}) error {
	// For simple strings, just print them
	if str, ok := data.(string); ok {
		fmt.Fprintln(p.writer, str)
		return nil
	}
	// For complex data, format as JSON
	return p.printJSON(data)
}

// Styles for output formatting
var (
	SuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	ErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	WarningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	InfoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
)

// PrintSuccess prints a success message using the printer's writer
func (p *Printer) PrintSuccess(message string) {
	fmt.Fprintln(p.writer, SuccessStyle.Render("✓ "+message))
}

// PrintError prints an error message using the printer's writer
func (p *Printer) PrintError(message string) {
	fmt.Fprintln(p.writer, ErrorStyle.Render("✗ "+message))
}

// PrintWarning prints a warning message using the printer's writer
func (p *Printer) PrintWarning(message string) {
	fmt.Fprintln(p.writer, WarningStyle.Render("⚠ "+message))
}

// PrintInfo prints an info message using the printer's writer
func (p *Printer) PrintInfo(message string) {
	fmt.Fprintln(p.writer, InfoStyle.Render("ℹ "+message))
}

// Global convenience functions that use stdout directly
// Use these when you don't have a printer instance

func PrintSuccess(message string) {
	fmt.Println(SuccessStyle.Render("✓ " + message))
}

func PrintError(message string) {
	fmt.Println(ErrorStyle.Render("✗ " + message))
}

func PrintWarning(message string) {
	fmt.Println(WarningStyle.Render("⚠ " + message))
}

func PrintInfo(message string) {
	fmt.Println(InfoStyle.Render("ℹ " + message))
}
