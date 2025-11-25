package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "underleaf",
	Short: "Underleaf CLI is a tool to interact with the Underleaf platform",
	Long:  `Underleaf CLI provides commands to manage nodes, configurations, and other aspects of the Underleaf platform.`,
}

func Execute() error {
	return rootCmd.Execute()
}
