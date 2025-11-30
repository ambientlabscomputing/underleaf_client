package jobs

import (
	"github.com/spf13/cobra"
)

var JobsCmd = &cobra.Command{
	Use:   "jobs",
	Short: "Manage and view command execution jobs",
	Long:  "Commands to view and manage command execution jobs",
}
