# Bubble Tea UI - Quick Reference

## Import
```go
import "github.com/ambientlabscomputing/underleaf_client/internal/ui"
```

## Interactive Components

### Selection List
```go
choices := []string{"Option 1", "Option 2", "Option 3"}
selected, err := ui.RunSelection("Choose options:", choices)
```
**Keys:** ↑/↓ or k/j to move, space/enter to select, q to quit

### Text Input
```go
value, err := ui.PromptInput("Enter value:", "placeholder")
```
**Keys:** Type text, enter to submit, esc to cancel

### Confirmation
```go
confirmed, err := ui.Confirm("Are you sure?")
```
**Keys:** ←/→ to toggle, y/n to select, enter to confirm

### Spinner
```go
err := ui.ShowSpinner("Loading...", func() error {
    // Your work here
    return doSomething()
})
```
**Auto:** Shows spinner, then success/error message

## Display Components

### Table
```go
table := ui.NewTableBuilder().
    WithTitle("Results").
    WithHeaders("Column1", "Column2").
    AddRow("value1", "value2").
    AddRow("value3", "value4")
    
table.Print()  // or table.Render() for string
```

### Messages
```go
ui.PrintSuccess("Operation successful!")
ui.PrintError("Connection failed")
ui.PrintWarning("Low disk space")
ui.PrintInfo("Status updated")
```

### Message Boxes
```go
fmt.Println(ui.ErrorBox("Error", "Details..."))
fmt.Println(ui.WarningBox("Warning", "Details..."))
fmt.Println(ui.InfoBox("Info", "Details..."))
```

### Progress Bar
```go
// Simple text-based
progress := ui.SimpleProgressBar(current, total, width)
fmt.Println(progress)
```

### Lists
```go
items := []string{"Item 1", "Item 2", "Item 3"}
fmt.Println(ui.FormatList(items))
```

## Common Patterns

### Command with Confirmation
```go
func deleteCommand(id string) error {
    confirmed, err := ui.Confirm(fmt.Sprintf("Delete %s?", id))
    if err != nil || !confirmed {
        return err
    }
    
    return ui.ShowSpinner("Deleting...", func() error {
        return delete(id)
    })
}
```

### Display Status Table
```go
func showStatus(nodes []Node) {
    table := ui.NewTableBuilder().
        WithTitle("Node Status").
        WithHeaders("ID", "Status", "CPU", "Memory")
    
    for _, node := range nodes {
        table.AddRow(node.ID, node.Status, node.CPU, node.Memory)
    }
    
    table.Print()
}
```

### Multi-step with Progress
```go
steps := []string{"Step 1", "Step 2", "Step 3"}
for i, step := range steps {
    fmt.Println(ui.SimpleProgressBar(i, len(steps), 40))
    if err := doStep(step); err != nil {
        ui.PrintError(err.Error())
        return err
    }
}
ui.PrintSuccess("All steps complete!")
```

### Select from API Results
```go
nodes, err := fetchNodes()
if err != nil {
    ui.PrintError(fmt.Sprintf("Failed to fetch: %v", err))
    return err
}

names := make([]string, len(nodes))
for i, n := range nodes {
    names[i] = n.Name
}

selected, err := ui.RunSelection("Select nodes:", names)
```

## Color Codes

- **Green (10):** Success messages
- **Red (9):** Error messages  
- **Yellow (11):** Warning messages
- **Blue (12):** Info messages
- **Gray (240):** Muted/help text

## Examples

Try them out:
```bash
# Classic tutorial
go run examples/shopping_list.go

# Full demo
go run examples/ui_demo.go
```

## Tips

1. **Always handle errors** from interactive components
2. **Use spinners** for operations > 1 second
3. **Confirm destructive operations** with `Confirm()`
4. **Style consistently** using provided functions
5. **Test interactively** - UI feels different from code!
