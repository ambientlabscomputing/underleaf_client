package templates

import (
	"github.com/spf13/cobra"
)

var TemplatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Manage command templates",
	Long:  "Commands to create, manage, and trigger command templates with variable interpolation.",
}
