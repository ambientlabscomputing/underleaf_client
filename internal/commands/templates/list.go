package templates

import (
	"context"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List command templates",
	Long: `List all command templates.

Examples:
  ufctl templates list
  ufctl templates list --limit 20
  ufctl templates list --name deploy`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		limit, _ := cmd.Flags().GetInt("limit")
		name, _ := cmd.Flags().GetString("name")

		return listTemplates(deps, limit, name)
	},
}

type Template struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	CommandTemplate string            `json:"command_template"`
	WorkDir         string            `json:"work_dir,omitempty"`
	EnvVars         map[string]string `json:"env_vars,omitempty"`
	Timeout         int               `json:"timeout,omitempty"`
	Inputs          []InputVariable   `json:"inputs"`
	ServerIDs       []string          `json:"server_ids,omitempty"`
	Tags            map[string]string `json:"tags,omitempty"`
	CreatedAt       string            `json:"created_at"`
	UpdatedAt       string            `json:"updated_at"`
}

type InputVariable struct {
	Key         string `json:"key"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type"` // string, int, array
	Required    bool   `json:"required"`
	Default     string `json:"default,omitempty"`
}

type QueryTemplatesResponse struct {
	Results    []Template `json:"results"`
	TotalCount int        `json:"total_count"`
}

func listTemplates(deps *utils.DependencyManager, limit int, nameFilter string) error {
	// Build query params
	path := fmt.Sprintf("/templates?limit=%d", limit)
	if nameFilter != "" {
		path += fmt.Sprintf("&name=%s", nameFilter)
	}

	var response QueryTemplatesResponse
	err := deps.CPlaneClient.API().GET(context.Background(), path, &response)
	if err != nil {
		deps.Printer.PrintError("Failed to list templates: " + err.Error())
		return err
	}

	if len(response.Results) == 0 {
		deps.Printer.PrintInfo("No templates found")
		return nil
	}

	// Build table
	table := ui.NewTableBuilder().
		WithTitle(fmt.Sprintf("Templates (showing %d of %d)", len(response.Results), response.TotalCount)).
		WithHeaders("ID", "Name", "Command", "Variables", "Created")

	for _, tmpl := range response.Results {
		displayID := tmpl.ID

		// Truncate command for display
		command := tmpl.CommandTemplate
		if len(command) > 40 {
			command = command[:37] + "..."
		}

		// Count variables
		varCount := fmt.Sprintf("%d vars", len(tmpl.Inputs))

		// Format created time
		displayTime := tmpl.CreatedAt
		if len(displayTime) > 10 {
			displayTime = displayTime[:10] // YYYY-MM-DD
		}

		table.AddRow(displayID, tmpl.Name, command, varCount, displayTime)
	}

	deps.Printer.PrintTable(table)
	deps.Printer.Print("")
	deps.Printer.PrintInfo("Use 'ufctl templates get <id>' to view template details")
	deps.Printer.PrintInfo("Use 'ufctl templates trigger <id>' to execute a template")

	return nil
}

func init() {
	ListCmd.Flags().IntP("limit", "l", 20, "Number of templates to show")
	ListCmd.Flags().StringP("name", "n", "", "Filter by name (partial match)")
	TemplatesCmd.AddCommand(ListCmd)
}
