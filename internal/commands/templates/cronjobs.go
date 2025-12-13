package templates

import (
	"github.com/spf13/cobra"
)

var CronjobsCmd = &cobra.Command{
	Use:   "cronjobs",
	Short: "Manage cron jobs for templates",
	Long:  "Commands to create, manage, and monitor scheduled template executions.",
}

func init() {
	TemplatesCmd.AddCommand(CronjobsCmd)
}
