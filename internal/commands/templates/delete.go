package templates

import (
	"context"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var DeleteCmd = &cobra.Command{
	Use:   "delete <template-id>",
	Short: "Delete a command template",
	Long: `Delete a command template.

Examples:
  ufctl templates delete abc123
  ufctl templates delete my-deploy-template`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		templateID := args[0]
		force, _ := cmd.Flags().GetBool("force")

		return deleteTemplate(deps, templateID, force)
	},
}

func deleteTemplate(deps *utils.DependencyManager, templateID string, force bool) error {
	// If not force, confirm
	if !force {
		deps.Printer.Print("")
		deps.Printer.PrintWarning(fmt.Sprintf("This will permanently delete template '%s'", templateID))
		deps.Printer.Print("")
		deps.Printer.Print("Use --force to skip confirmation")
		return fmt.Errorf("deletion cancelled - use --force to confirm")
	}

	var response map[string]interface{}
	err := deps.CPlaneClient.API().DELETE(context.Background(), "/templates/"+templateID, &response)
	if err != nil {
		deps.Printer.PrintError("Failed to delete template: " + err.Error())
		return err
	}

	successStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	deps.Printer.Print("")
	deps.Printer.Print(successStyle.Render("✓ Template deleted successfully"))
	deps.Printer.Print("")

	return nil
}

func init() {
	DeleteCmd.Flags().Bool("force", false, "Skip confirmation prompt")
	TemplatesCmd.AddCommand(DeleteCmd)
}
