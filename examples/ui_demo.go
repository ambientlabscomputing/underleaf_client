package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
)

func main() {
	// Example 1: Simple selection list
	fmt.Println("=== Example 1: Selection List ===\n")

	choices := []string{
		"List all nodes",
		"Describe a node",
		"Execute command on node",
		"View node logs",
		"Check node status",
	}

	selected, err := ui.RunSelection("What would you like to do?", choices)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if len(selected) > 0 {
		fmt.Printf("\nYou selected:\n")
		for _, item := range selected {
			fmt.Printf("  - %s\n", item)
		}
	} else {
		fmt.Println("\nNo items selected")
	}

	fmt.Println("\n" + strings.Repeat("=", 50) + "\n")

	// Example 2: Text input
	fmt.Println("=== Example 2: Text Input ===\n")

	name, err := ui.PromptInput("Enter your name:", "John Doe")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if name != "" {
		ui.PrintSuccess(fmt.Sprintf("Hello, %s!", name))
	}

	fmt.Println("\n" + strings.Repeat("=", 50) + "\n")

	// Example 3: Confirmation
	fmt.Println("=== Example 3: Confirmation ===\n")

	confirmed, err := ui.Confirm("Do you want to continue?")
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	if confirmed {
		ui.PrintSuccess("You chose to continue!")
	} else {
		ui.PrintWarning("Operation cancelled")
	}

	fmt.Println("\n" + strings.Repeat("=", 50) + "\n")

	// Example 4: Table
	fmt.Println("=== Example 4: Table ===\n")

	table := ui.NewTableBuilder().
		WithTitle("Node Status").
		WithHeaders("Node ID", "Status", "CPU", "Memory").
		AddRow("node-001", "Running", "45%", "2.1GB").
		AddRow("node-002", "Running", "23%", "1.8GB").
		AddRow("node-003", "Stopped", "0%", "0GB").
		AddRow("node-004", "Running", "67%", "3.2GB")

	fmt.Println(table.Render())

	fmt.Println("\n" + strings.Repeat("=", 50) + "\n")

	// Example 5: Error formatting
	fmt.Println("=== Example 5: Error Messages ===\n")

	ui.PrintError("Failed to connect to node")
	ui.PrintWarning("Node is running low on memory")
	ui.PrintInfo("New node added to cluster")
	ui.PrintSuccess("Command executed successfully")

	fmt.Println()
	fmt.Println(ui.ErrorBox("Connection Error", "Unable to establish connection to the control plane.\nPlease check your network settings."))

	fmt.Println("\n" + strings.Repeat("=", 50) + "\n")

	// Example 6: Progress bar
	fmt.Println("=== Example 6: Progress Bar ===\n")

	fmt.Println(ui.SimpleProgressBar(7, 10, 40))
	fmt.Println(ui.SimpleProgressBar(3, 10, 40))
	fmt.Println(ui.SimpleProgressBar(10, 10, 40))

	fmt.Println("\n" + strings.Repeat("=", 50) + "\n")

	// Example 7: List formatting
	fmt.Println("=== Example 7: List ===\n")

	items := []string{
		"Initialize cluster configuration",
		"Register node with control plane",
		"Start agent services",
		"Begin health monitoring",
	}

	fmt.Println(ui.FormatList(items))
}
