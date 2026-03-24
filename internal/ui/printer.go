package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"

	"github.com/charmbracelet/lipgloss"
)

// OutputFormat represents the output format for printing
type OutputFormat string

// PrinterKey is the context key for the Printer
type PrinterKey struct{}

const (
	FormatHuman OutputFormat = "human"
	FormatShell OutputFormat = "shell"
	FormatJSON  OutputFormat = "json"

	// Deprecated: use FormatHuman
	FormatTable OutputFormat = "human"
	// Deprecated: use FormatHuman
	FormatWide OutputFormat = "human"
	// Deprecated: use FormatHuman
	FormatYAML OutputFormat = "human"
)

// ansiRe matches ANSI escape sequences
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI removes ANSI escape codes from a string
func StripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// IsInteractive returns true if stdout is connected to a terminal
func IsInteractive() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// DetectFormat returns FormatHuman if running interactively, FormatShell otherwise.
// Respects the NO_COLOR environment variable.
func DetectFormat() OutputFormat {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return FormatShell
	}
	if IsInteractive() {
		return FormatHuman
	}
	return FormatShell
}

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

// NewPrinterToContext creates a Printer and stores it in the context
func NewPrinterToContext(ctx context.Context, format OutputFormat) (context.Context, *Printer) {
	printer := NewPrinter(format)
	ctx = context.WithValue(ctx, PrinterKey{}, printer)
	return ctx, printer
}

// GetPrinter retrieves the Printer from context, or creates a default one
func GetPrinter(ctx context.Context) *Printer {
	if printer, ok := ctx.Value(PrinterKey{}).(*Printer); ok {
		return printer
	}
	return NewPrinter(FormatHuman)
}

// Format returns the current output format
func (p *Printer) Format() OutputFormat {
	return p.format
}

// SetFormat updates the output format
func (p *Printer) SetFormat(f OutputFormat) {
	p.format = f
}

// WithWriter sets a custom writer for the printer
func (p *Printer) WithWriter(w io.Writer) *Printer {
	p.writer = w
	return p
}

// Print outputs data in the configured format.
// Strings: printed as-is (human), ANSI-stripped (shell), or {"msg":"..."} (json).
// Non-string data: JSON-encoded in all modes.
func (p *Printer) Print(data interface{}) error {
	if str, ok := data.(string); ok {
		switch p.format {
		case FormatJSON:
			if str == "" {
				return nil
			}
			return p.printJSON(map[string]string{"msg": StripANSI(str)})
		case FormatShell:
			fmt.Fprintln(p.writer, StripANSI(str))
			return nil
		default:
			fmt.Fprintln(p.writer, str)
			return nil
		}
	}

	return p.printJSON(data)
}

// PrintTable outputs a TableBuilder in the appropriate format
func (p *Printer) PrintTable(tb *TableBuilder) error {
	_, err := fmt.Fprintln(p.writer, tb.RenderForFormat(p.format))
	return err
}

func (p *Printer) printJSON(data interface{}) error {
	encoder := json.NewEncoder(p.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

// Styles for output formatting (used in human mode)
var (
	SuccessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	ErrorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	WarningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	InfoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
)

// PrintSuccess prints a success message
func (p *Printer) PrintSuccess(message string) {
	switch p.format {
	case FormatJSON:
		p.printJSON(map[string]string{"level": "success", "msg": message})
	case FormatShell:
		fmt.Fprintln(p.writer, "OK: "+message)
	default:
		fmt.Fprintln(p.writer, SuccessStyle.Render("✓ "+message))
	}
}

// PrintError prints an error message
func (p *Printer) PrintError(message string) {
	switch p.format {
	case FormatJSON:
		p.printJSON(map[string]string{"level": "error", "msg": message})
	case FormatShell:
		fmt.Fprintln(p.writer, "ERROR: "+message)
	default:
		fmt.Fprintln(p.writer, ErrorStyle.Render("✗ "+message))
	}
}

// PrintWarning prints a warning message
func (p *Printer) PrintWarning(message string) {
	switch p.format {
	case FormatJSON:
		p.printJSON(map[string]string{"level": "warning", "msg": message})
	case FormatShell:
		fmt.Fprintln(p.writer, "WARN: "+message)
	default:
		fmt.Fprintln(p.writer, WarningStyle.Render("⚠ "+message))
	}
}

// PrintInfo prints an info message
func (p *Printer) PrintInfo(message string) {
	switch p.format {
	case FormatJSON:
		p.printJSON(map[string]string{"level": "info", "msg": message})
	case FormatShell:
		fmt.Fprintln(p.writer, "INFO: "+message)
	default:
		fmt.Fprintln(p.writer, InfoStyle.Render("ℹ "+message))
	}
}

// Global convenience functions that auto-detect format.
// Prefer using a Printer instance from context instead.

func PrintSuccess(message string) {
	NewPrinter(DetectFormat()).PrintSuccess(message)
}

func PrintError(message string) {
	NewPrinter(DetectFormat()).PrintError(message)
}

func PrintWarning(message string) {
	NewPrinter(DetectFormat()).PrintWarning(message)
}

func PrintInfo(message string) {
	NewPrinter(DetectFormat()).PrintInfo(message)
}
