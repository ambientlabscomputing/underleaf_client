package local

import (
	"net/http"

	"github.com/ambientlabscomputing/underleaf_client/internal/config_manager"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/server"
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
		printer := ui.GetPrinter(ctx)
		configClient := config_manager.NewConfigClient(config_manager.ConfigClientTypeCLI)
		h := http.DefaultClient
		cPlane := controlplane.NewCPlaneClient(&configClient, h)
		service := server.NewServerService(cPlane, configClient)

		// Prompt for server name
		name, err := ui.PromptInput("Enter a name for this edge server:", "my-edge-server")
		if err != nil {
			printer.PrintError("Failed to get input: " + err.Error())
			return err
		}

		if name == "" {
			printer.PrintWarning("Registration cancelled - no name provided")
			return nil
		}

		if err := service.RegisterServer(ctx, name); err != nil {
			printer.PrintError("Failed to register edge server: " + err.Error())
			return err
		}
		printer.PrintSuccess("Edge server '" + name + "' registered successfully!")

		return nil
	},
}
