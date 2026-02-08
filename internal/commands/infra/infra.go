package infra

import (
	"github.com/spf13/cobra"
)

// NewInfraCmd creates the infra command
func NewInfraCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "infra",
		Short: "Manage infrastructure declaratively",
		Long:  `Declarative infrastructure management for Underleaf deployments and clusters.`,
	}

	// Add subcommands
	cmd.AddCommand(NewInitCmd())
	cmd.AddCommand(NewValidateCmd())
	cmd.AddCommand(NewPlanCmd())
	cmd.AddCommand(NewApplyCmd())

	return cmd
}
