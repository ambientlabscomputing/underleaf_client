# Underleaf CLI UI Examples

This directory contains example programs demonstrating the Bubble Tea UI components.

## Running the examples

### UI Demo
A comprehensive demo of all UI components:

```bash
go run examples/ui_demo.go
```

This demo showcases:
- **Selection lists**: Interactive multi-select menus
- **Text input**: Prompt users for text input
- **Confirmations**: Yes/no dialogs
- **Tables**: Formatted data tables with styling
- **Error messages**: Styled error, warning, info, and success messages
- **Progress bars**: Visual progress indicators
- **Lists**: Formatted bullet-point lists

## Available UI Components

### Selection Model (`tea_model.go`)
Interactive list selection with multi-select support:
```go
selected, err := ui.RunSelection("Choose options:", choices)
```

### Input Model (`input.go`)
Text input prompts and yes/no confirmations:
```go
name, err := ui.PromptInput("Enter name:", "placeholder")
confirmed, err := ui.Confirm("Are you sure?")
```

### Tables (`table.go`)
Formatted tables with headers and styling:
```go
table := ui.NewTableBuilder().
    WithTitle("Title").
    WithHeaders("Col1", "Col2").
    AddRow("val1", "val2")
```

### Spinner (`spinner.go`)
Loading indicators for long-running operations:
```go
err := ui.ShowSpinner("Loading...", func() error {
    // Your work here
    return nil
})
```

### Progress Bar (`progress.go`)
Visual progress tracking:
```go
progress := ui.SimpleProgressBar(current, total, width)
```

### Error Formatting (`errors.go`)
Styled error, warning, and info messages:
```go
ui.PrintError("Error message")
ui.PrintWarning("Warning message")
ui.PrintSuccess("Success message")
ui.PrintInfo("Info message")
```

## Styling

All components use Lipgloss for consistent styling with:
- Color-coded messages (green=success, red=error, yellow=warning, blue=info)
- Bordered boxes for important messages
- Tables with proper alignment and borders
- Responsive layouts

## Integration

These UI components are designed to integrate seamlessly with the `ufctl` CLI commands. Import them in your command implementations:

```go
import "github.com/ambientlabscomputing/underleaf_client/internal/ui"
```
