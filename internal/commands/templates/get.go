package templates

import (
	"context"
	"fmt"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var GetCmd = &cobra.Command{
	Use:   "get <template-id>",
	Short: "Get details of a specific template",
	Long: `Get detailed information about a command template.

Examples:
  ufctl templates get abc123
  ufctl templates get my-deploy-template`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		templateID := args[0]
		return getTemplate(deps, templateID)
	},
}

func getTemplate(deps *utils.DependencyManager, templateID string) error {
	var template Template
	err := deps.CPlaneClient.API().GET(context.Background(), "/templates/"+templateID, &template)
	if err != nil {
		deps.Printer.PrintError("Failed to get template: " + err.Error())
		return err
	}

	// Styles
	boldStyle := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))

	deps.Printer.Print("")
	deps.Printer.Print(headerStyle.Render("Template Details"))
	deps.Printer.Print("")

	// Basic info
	deps.Printer.Print(fmt.Sprintf("  %s %s", boldStyle.Render("ID:"), template.ID))
	deps.Printer.Print(fmt.Sprintf("  %s %s", boldStyle.Render("Name:"), template.Name))
	if template.Description != "" {
		deps.Printer.Print(fmt.Sprintf("  %s %s", boldStyle.Render("Description:"), template.Description))
	}
	deps.Printer.Print("")

	// Command
	deps.Printer.Print(fmt.Sprintf("  %s", boldStyle.Render("Command:")))
	deps.Printer.Print(fmt.Sprintf("    %s", template.CommandTemplate))
	deps.Printer.Print("")

	// Configuration
	if template.WorkDir != "" {
		deps.Printer.Print(fmt.Sprintf("  %s %s", boldStyle.Render("Working Dir:"), template.WorkDir))
	}
	if template.Timeout > 0 {
		deps.Printer.Print(fmt.Sprintf("  %s %ds", boldStyle.Render("Timeout:"), template.Timeout))
	}

	// Environment Variables
	if len(template.EnvVars) > 0 {
		deps.Printer.Print("")
		deps.Printer.Print(fmt.Sprintf("  %s", boldStyle.Render("Environment Variables:")))
		for k, v := range template.EnvVars {
			deps.Printer.Print(fmt.Sprintf("    %s=%s", k, v))
		}
	}

	// Variables (Input Parameters)
	if len(template.Inputs) > 0 {
		deps.Printer.Print("")
		deps.Printer.Print(fmt.Sprintf("  %s", boldStyle.Render("Input Variables:")))

		varTable := ui.NewTableBuilder().
			WithHeaders("Key", "Type", "Required", "Default", "Description")

		for _, v := range template.Inputs {
			required := "No"
			if v.Required {
				required = "Yes"
			}
			defaultVal := v.Default
			if defaultVal == "" {
				defaultVal = dimStyle.Render("-")
			}
			desc := v.Description
			if len(desc) > 40 {
				desc = desc[:37] + "..."
			}
			if desc == "" {
				desc = dimStyle.Render("-")
			}
			varTable.AddRow(v.Key, v.Type, required, defaultVal, desc)
		}
		deps.Printer.Print(varTable.Render())
	}

	// Target Servers
	if len(template.ServerIDs) > 0 {
		deps.Printer.Print("")
		deps.Printer.Print(fmt.Sprintf("  %s %s", boldStyle.Render("Target Servers:"), strings.Join(template.ServerIDs, ", ")))
	}

	// Tags
	if len(template.Tags) > 0 {
		deps.Printer.Print("")
		deps.Printer.Print(fmt.Sprintf("  %s", boldStyle.Render("Tags:")))
		for k, v := range template.Tags {
			deps.Printer.Print(fmt.Sprintf("    %s=%s", k, v))
		}
	}

	// Timestamps
	deps.Printer.Print("")
	deps.Printer.Print(fmt.Sprintf("  %s %s", dimStyle.Render("Created:"), template.CreatedAt))
	deps.Printer.Print(fmt.Sprintf("  %s %s", dimStyle.Render("Updated:"), template.UpdatedAt))
	deps.Printer.Print("")

	// Usage hint
	deps.Printer.PrintInfo("Use 'ufctl templates trigger " + template.ID + "' to execute this template")

	return nil
}

func init() {
	TemplatesCmd.AddCommand(GetCmd)
}
