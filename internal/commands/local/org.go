package local

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
)

var (
	// Styles for org display
	orgNameStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4"))

	orgIDStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#999999"))

	currentOrgStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FF00"))
)

// `ufctl org` command implementation
var OrgCmd = &cobra.Command{
	Use:   "org",
	Short: "Manage organization context",
	Long:  "Commands to manage organization context for accessing Underleaf services.",
}

// `ufctl org list` displays all organizations the user is a member of
var OrgListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all organizations",
	Long:  "Display all organizations you are a member of.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		// Get dependencies
		deps := utils.NewDependencyManager(ctx)
		if deps == nil {
			return fmt.Errorf("failed to create dependencies")
		}

		userClient := deps.CPlaneClient.Users

		// Get user profile with organizations
		logger.Info("Fetching user organizations")
		me, err := userClient.GetMe(ctx)
		if err != nil {
			return fmt.Errorf("failed to get user organizations: %w", err)
		}

		if len(me.Organizations) == 0 {
			printer.PrintInfo("No organizations found. Create an organization to get started.")
			return nil
		}

		// Get current org from config
		currentOrgID, _ := deps.ConfigClient.Get("local.organization_id")

		// Build a table for org list
		table := ui.NewTableBuilder().
			WithTitle("Your Organizations").
			WithHeaders("", "Name", "Role", "ID", "Status")

		for _, org := range me.Organizations {
			marker := ""
			if currentOrgID != nil && currentOrgID.(string) == org.ID {
				marker = "▶"
			}
			status := org.Status
			if status == "" {
				status = "-"
			}
			table.AddRow(marker, org.Name, org.Role, org.ID, status)
		}

		printer.PrintTable(table)
		return nil
	},
}

// `ufctl org current` displays the current organization context
var OrgCurrentCmd = &cobra.Command{
	Use:   "current",
	Short: "Show current organization",
	Long:  "Display the currently selected organization context.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		printer := ui.GetPrinter(ctx)

		// Get dependencies
		deps := utils.NewDependencyManager(ctx)
		if deps == nil {
			return fmt.Errorf("failed to create dependencies")
		}

		orgID, hasOrgID := deps.ConfigClient.Get("local.organization_id")
		orgName, hasOrgName := deps.ConfigClient.Get("local.organization_name")

		if !hasOrgID || orgID == nil || orgID.(string) == "" {
			printer.PrintInfo("No organization context set. Use 'ufctl org switch <org-id>' to select an organization.")
			return nil
		}

		table := ui.NewTableBuilder().
			WithTitle("Current Organization").
			WithHeaders("Property", "Value")

		if hasOrgName && orgName != nil && orgName.(string) != "" {
			table.AddRow("Name", orgName.(string))
		}
		table.AddRow("ID", orgID.(string))

		printer.PrintTable(table)
		return nil
	},
}

// `ufctl org switch` changes the current organization context
var OrgSwitchCmd = &cobra.Command{
	Use:   "switch <org-id>",
	Short: "Switch organization context",
	Long:  "Change the current organization context to the specified organization.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)
		targetOrgID := args[0]

		// Get dependencies
		deps := utils.NewDependencyManager(ctx)
		if deps == nil {
			return fmt.Errorf("failed to create dependencies")
		}

		userClient := deps.CPlaneClient.Users

		// Get user profile to validate org membership
		logger.Info("Validating organization membership", "org_id", targetOrgID)
		me, err := userClient.GetMe(ctx)
		if err != nil {
			return fmt.Errorf("failed to get user organizations: %w", err)
		}

		// Find the target organization
		var targetOrg *struct {
			ID   string
			Name string
		}
		for _, org := range me.Organizations {
			if org.ID == targetOrgID {
				targetOrg = &struct {
					ID   string
					Name string
				}{
					ID:   org.ID,
					Name: org.Name,
				}
				break
			}
		}

		if targetOrg == nil {
			return fmt.Errorf("organization %s not found. You are not a member of this organization", targetOrgID)
		}

		// Update config
		logger.Info("Switching organization context", "org_id", targetOrg.ID, "org_name", targetOrg.Name)
		if err := deps.ConfigClient.Set("local.organization_id", targetOrg.ID); err != nil {
			return fmt.Errorf("failed to save organization ID: %w", err)
		}
		if err := deps.ConfigClient.Set("local.organization_name", targetOrg.Name); err != nil {
			return fmt.Errorf("failed to save organization name: %w", err)
		}

		printer.PrintSuccess("Switched to organization: " + targetOrg.Name)
		printer.Print(fmt.Sprintf("  ID: %s", targetOrg.ID))

		return nil
	},
}

func init() {
	OrgCmd.AddCommand(OrgListCmd)
	OrgCmd.AddCommand(OrgCurrentCmd)
	OrgCmd.AddCommand(OrgSwitchCmd)
}
