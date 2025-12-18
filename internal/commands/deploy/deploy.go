package deploy

import "github.com/spf13/cobra"

var DeployCmd = &cobra.Command{
	Use:   "deploy",
	Short: "Manage application deployments",
	Long:  "Commands to compile, plan, and manage application deployments.",
}

func init() {
	DeployCmd.AddCommand(ListCmd)
	DeployCmd.AddCommand(CompileCmd)
	DeployCmd.AddCommand(PlanCmd)
	DeployCmd.AddCommand(DiffCmd)
	DeployCmd.AddCommand(ApplyCmd)
}
