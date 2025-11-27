package servers

import (
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List registered edge servers",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		servers, err := deps.ServerSvc.ListServers(ctx)
		if err != nil {
			deps.Printer.PrintError("Failed to list edge servers: " + err.Error())
			return err
		}

		if len(servers) == 0 {
			deps.Printer.PrintInfo("No edge servers registered.")
			return nil
		}

		// Build table
		table := ui.NewTableBuilder().
			WithTitle("Edge Servers").
			WithHeaders("ID", "Name", "Platform", "Literal Commands")

		for _, server := range servers {
			platform := server.Config.Payload.Platform.OS + "/" + server.Config.Payload.Platform.Arch
			allowLiteral := "No"
			if server.Config.Payload.Commands.AllowLiteralCommands {
				allowLiteral = "Yes"
			}
			table.AddRow(server.ID, server.Name, platform, allowLiteral)
		}

		deps.Printer.Print(table.Render())
		return nil
	},
}

func init() {
	ServersCmd.AddCommand(ListCmd)
}
