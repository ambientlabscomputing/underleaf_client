package local

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
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

	orgRoleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00BFFF"))
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
			fmt.Println("No organizations found. Create an organization to get started.")
			return nil
		}

		// Get current org from config
		currentOrgID, _ := deps.ConfigClient.Get("local.organization_id")

		fmt.Println()
		fmt.Println(titleStyle.Render("📋 Your Organizations"))
		fmt.Println()

		for _, org := range me.Organizations {
			prefix := "  "
			if currentOrgID != nil && currentOrgID.(string) == org.ID {
				prefix = currentOrgStyle.Render("▶ ")
			}

			orgLine := fmt.Sprintf("%s%s",
				orgNameStyle.Render(org.Name),
				orgRoleStyle.Render(fmt.Sprintf(" (%s)", org.Role)),
			)
			fmt.Println(prefix + orgLine)
			fmt.Println("  " + orgIDStyle.Render("  ID: "+org.ID))
			if org.Status != "" {
				fmt.Println("  " + orgIDStyle.Render("  Status: "+org.Status))
			}
			fmt.Println()
		}

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

		// Get dependencies
		deps := utils.NewDependencyManager(ctx)
		if deps == nil {
			return fmt.Errorf("failed to create dependencies")
		}

		orgID, hasOrgID := deps.ConfigClient.Get("local.organization_id")
		orgName, hasOrgName := deps.ConfigClient.Get("local.organization_name")

		if !hasOrgID || orgID == nil || orgID.(string) == "" {
			fmt.Println("No organization context set. Use 'ufctl org switch <org-id>' to select an organization.")
			return nil
		}

		fmt.Println()
		fmt.Println(titleStyle.Render("🏢 Current Organization"))
		fmt.Println()
		if hasOrgName && orgName != nil && orgName.(string) != "" {
			fmt.Println(currentOrgStyle.Render("  " + orgName.(string)))
		}
		fmt.Println(orgIDStyle.Render("  ID: " + orgID.(string)))
		fmt.Println()

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

		fmt.Println()
		fmt.Println(successStyle.Render("✓ Switched to organization: " + targetOrg.Name))
		fmt.Println(orgIDStyle.Render("  ID: " + targetOrg.ID))
		fmt.Println()

		return nil
	},
}

func init() {
	OrgCmd.AddCommand(OrgListCmd)
	OrgCmd.AddCommand(OrgCurrentCmd)
	OrgCmd.AddCommand(OrgSwitchCmd)
}
