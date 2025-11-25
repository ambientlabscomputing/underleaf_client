# Bubble Tea UI Implementation

This directory contains a complete Bubble Tea-based terminal UI implementation for the Underleaf CLI.

## Overview

The UI package (`internal/ui/`) provides reusable, interactive terminal UI components built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and [Lipgloss](https://github.com/charmbracelet/lipgloss).

## Components

### 1. **Selection Lists** (`tea_model.go`)
Interactive multi-select lists with keyboard navigation.

```go
selected, err := ui.RunSelection("Choose servers:", serverList)
```

Features:
- ↑/↓ or k/j to navigate
- Space/Enter to select/deselect
- Returns selected items
- Styled with cursor and checkboxes

### 2. **Text Input** (`input.go`)
Prompt users for text input with validation support.

```go
name, err := ui.PromptInput("Enter server name:", "server-001")
```

### 3. **Confirmation Dialogs** (`input.go`)
Yes/No confirmation prompts.

```go
confirmed, err := ui.Confirm("Delete this server?")
```

Features:
- Arrow keys or y/n to select
- Visual highlighting
- Returns boolean result

### 4. **Tables** (`table.go`)
Formatted data tables with borders and styling.

```go
table := ui.NewTableBuilder().
    WithTitle("Cluster Status").
    WithHeaders("Server", "Status", "CPU", "Memory").
    AddRow("server-001", "Running", "45%", "2.1GB").
    AddRow("server-002", "Running", "23%", "1.8GB")

table.Print()
```

Features:
- Automatic column alignment
- Styled headers and borders
- Optional titles
- Helper functions for key-value pairs

### 5. **Spinner** (`spinner.go`)
Loading indicators for long-running operations.

```go
err := ui.ShowSpinner("Connecting to server...", func() error {
    // Your work here
    return doWork()
})
```

Features:
- Animated spinner
- Success/error messages on completion
- Non-blocking operation

### 6. **Progress Bars** (`progress.go`)
Visual progress tracking for multi-step operations.

```go
// Simple text-based progress bar
fmt.Println(ui.SimpleProgressBar(7, 10, 40))

// Full interactive progress bar
prog := ui.NewProgressModel("Deploying...")
// Update via messages
```

### 7. **Error Formatting** (`errors.go`)
Styled messages and boxed errors.

```go
ui.PrintError("Connection failed")
ui.PrintWarning("Low memory")
ui.PrintSuccess("Deployment complete")
ui.PrintInfo("New server registered")

// Boxed messages
fmt.Println(ui.ErrorBox("Error", "Details here"))
fmt.Println(ui.WarningBox("Warning", "Details here"))
fmt.Println(ui.InfoBox("Info", "Details here"))
```

### 8. **Printer** (`printer.go`)
Output formatting with multiple format support.

```go
printer := ui.NewPrinter(ui.FormatTable)
printer.Print(data)
```

Supported formats:
- Table
- JSON
- YAML
- Wide (detailed)

## Examples

### Shopping List Tutorial
The classic Bubble Tea tutorial example:

```bash
go run examples/shopping_list.go
```

This is the original tutorial shopping list demonstrating:
- Basic model structure
- Init/Update/View pattern
- Keyboard input handling
- State management

### Comprehensive UI Demo
All UI components in action:

```bash
go run examples/ui_demo.go
```

Demonstrates:
- Selection lists
- Text input
- Confirmations
- Tables
- Error formatting
- Progress bars
- Lists

## Usage in CLI Commands

Import the UI package in your command implementations:

```go
package commands

import (
    "github.com/ambientlabscomputing/underleaf_client/internal/ui"
    "github.com/spf13/cobra"
)

var deleteCmd = &cobra.Command{
    Use:   "delete [server-id]",
    Short: "Delete a server",
    RunE: func(cmd *cobra.Command, args []string) error {
        // Confirm before deletion
        confirmed, err := ui.Confirm("Are you sure you want to delete this server?")
        if err != nil {
            return err
        }
        
        if !confirmed {
            ui.PrintWarning("Operation cancelled")
            return nil
        }
        
        // Show spinner while deleting
        err = ui.ShowSpinner("Deleting server...", func() error {
            return deleteServer(args[0])
        })
        
        if err != nil {
            ui.PrintError(fmt.Sprintf("Failed to delete server: %v", err))
            return err
        }
        
        ui.PrintSuccess("Server deleted successfully")
        return nil
    },
}
```

## Styling

All components use consistent Lipgloss styling:

- **Colors**:
  - Success: Green (#10)
  - Error: Red (#9)
  - Warning: Yellow (#11)
  - Info: Blue (#12)
  - Muted: Gray (#240)

- **Borders**: Rounded borders for boxes, normal borders for tables
- **Typography**: Bold for headers, regular for content
- **Spacing**: Consistent padding and margins

## The Elm Architecture

Bubble Tea is based on The Elm Architecture with three core methods:

1. **Init**: Returns initial commands
2. **Update**: Handles messages and updates state
3. **View**: Renders the UI

This functional approach makes terminal UIs composable and testable.

## Best Practices

1. **Use the high-level functions** for simple cases:
   - `ui.RunSelection()` for lists
   - `ui.PromptInput()` for text input
   - `ui.Confirm()` for yes/no
   - `ui.ShowSpinner()` for loading

2. **Create custom models** for complex interactions:
   - Extend the base models
   - Add your own state
   - Implement custom update logic

3. **Style consistently**:
   - Use the provided style constants
   - Maintain color conventions
   - Keep layouts consistent

4. **Handle errors gracefully**:
   - Use `ui.PrintError()` for user-facing errors
   - Show context with `ui.ErrorBox()`
   - Provide helpful messages

## Dependencies

- `github.com/charmbracelet/bubbletea` - The TUI framework
- `github.com/charmbracelet/lipgloss` - Terminal styling
- `github.com/charmbracelet/bubbles` - Pre-built components (spinner, progress, textinput)

## Testing

UI components can be tested by:
1. Creating models with test data
2. Sending test messages via Update()
3. Verifying View() output
4. Testing state transitions

Example:
```go
func TestSelectionModel(t *testing.T) {
    m := ui.NewSelectionModel("Test", []string{"A", "B", "C"})
    
    // Test navigation
    m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
    if m.Cursor != 1 {
        t.Error("Cursor should move down")
    }
}
```

## Learn More

- [Bubble Tea Documentation](https://github.com/charmbracelet/bubbletea)
- [Lipgloss Documentation](https://github.com/charmbracelet/lipgloss)
- [Bubbles Components](https://github.com/charmbracelet/bubbles)
- [The Elm Architecture](https://guide.elm-lang.org/architecture/)
