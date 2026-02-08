package deploy

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var CapabilitiesCmd = &cobra.Command{
	Use:   "capabilities [deployment-file]",
	Short: "Show capability requirements for a deployment",
	Long: `Shows the capability requirements declared in a deployment recipe.
For each requirement, displays the capability ID, version range, alias,
configuration overrides, and resolution constraints.

Examples:
  ufctl deploy capabilities my-app.json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		deploymentFile := args[0]

		deployment, err := loadDeploymentSpec(deploymentFile)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to load deployment: %v", err))
			return err
		}

		deps.Printer.Print(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render(fmt.Sprintf("\n📋 Capability Requirements for %s (v%d)\n", deployment.Name, deployment.Version)))

		if len(deployment.CapabilityRequirements) == 0 {
			deps.Printer.PrintInfo("   No capability requirements — this is a pure container deployment")
			return nil
		}

		deps.Printer.Print(fmt.Sprintf("   Total: %d capability requirement(s)\n", len(deployment.CapabilityRequirements)))

		for i, req := range deployment.CapabilityRequirements {
			label := req.CapabilityID
			if req.Alias != "" {
				label = fmt.Sprintf("%s (%s)", req.Alias, req.CapabilityID)
			}

			deps.Printer.Print(lipgloss.NewStyle().Bold(true).Render(fmt.Sprintf("  [%d] %s", i+1, label)))

			version := "latest"
			if req.VersionRange != "" {
				version = req.VersionRange
			}
			deps.Printer.Print(fmt.Sprintf("      Version: %s", version))

			if req.Constraints != nil {
				if req.Constraints.TrustTier != "" {
					deps.Printer.Print(fmt.Sprintf("      Trust Tier: %s", req.Constraints.TrustTier))
				}
				if req.Constraints.Platform != "" {
					deps.Printer.Print(fmt.Sprintf("      Platform: %s", req.Constraints.Platform))
				}
				if req.Constraints.Architecture != "" {
					deps.Printer.Print(fmt.Sprintf("      Architecture: %s", req.Constraints.Architecture))
				}
			}

			if len(req.Config) > 0 {
				deps.Printer.Print("      Config overrides:")
				for k, v := range req.Config {
					deps.Printer.Print(fmt.Sprintf("        %s = %s", k, v))
				}
			}
			deps.Printer.Print("")
		}

		// Also show container summary alongside
		if len(deployment.Services) > 0 {
			deps.Printer.Print(lipgloss.NewStyle().Bold(true).Render("\n📦 Container Services (also included):"))
			for _, svc := range deployment.Services {
				deps.Printer.Print(fmt.Sprintf("   - %s (%s)", svc.Name, svc.Image))
			}
		}

		// Write JSON if output flag specified
		if capOutput != "" {
			data, err := json.MarshalIndent(deployment.CapabilityRequirements, "", "  ")
			if err != nil {
				deps.Printer.PrintError(fmt.Sprintf("Failed to marshal: %v", err))
				return err
			}
			if err := os.WriteFile(capOutput, data, 0644); err != nil {
				deps.Printer.PrintError(fmt.Sprintf("Failed to write: %v", err))
				return err
			}
			deps.Printer.PrintSuccess(fmt.Sprintf("✅ Written to %s", capOutput))
		}

		return nil
	},
}

var capOutput string

func init() {
	CapabilitiesCmd.Flags().StringVarP(&capOutput, "output", "o", "", "Output file for capability requirements (JSON)")
}
