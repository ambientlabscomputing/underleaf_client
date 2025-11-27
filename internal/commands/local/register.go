package local

import (
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

// RegisterCmd represents the register command
// `ufctl local register` prompts the user for a name and registers a new edge server
var RegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a new Underleaf edge server",
	Long:  "Registers a new Underleaf edge server with the control plane.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		// Prompt for server name
		name, err := ui.PromptInput("Enter a name for this edge server:", "my-edge-server")
		if err != nil {
			deps.Printer.PrintError("Failed to get input: " + err.Error())
			return err
		}

		if name == "" {
			deps.Printer.PrintWarning("Registration cancelled - no name provided")
			return nil
		}

		if err := deps.ServerSvc.RegisterServer(ctx, name); err != nil {
			deps.Printer.PrintError("Failed to register edge server: " + err.Error())
			return err
		}
		deps.Printer.PrintSuccess("Edge server '" + name + "' registered successfully!")

		return nil
	},
}
