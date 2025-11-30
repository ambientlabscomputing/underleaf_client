package servers

import (
	"fmt"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/server"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var DescribeCmd = &cobra.Command{
	Use:   "describe [server-id]",
	Short: "Show detailed information about a server",
	Long: `Display detailed information about a specific server.

Examples:
  ufctl servers describe server-abc123
  ufctl servers describe my-server-name`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		serverID := args[0]

		// Get server details from control plane
		srv, err := deps.ServerSvc.GetServer(ctx, serverID)
		if err != nil {
			deps.Printer.PrintError("Failed to get server: " + err.Error())
			return err
		}

		// Display server details
		showServerDetails(deps.Printer, srv)

		return nil
	},
}

func showServerDetails(printer ui.Printer, srv server.Server) {
	boldStyle := lipgloss.NewStyle().Bold(true)
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	labelStyle := lipgloss.NewStyle().Width(20)

	printer.Print("")
	printer.Print(boldStyle.Render(fmt.Sprintf("Server: %s", srv.Name)))
	printer.Print(strings.Repeat("─", 50))
	printer.Print("")

	// Basic Info
	printer.Print(headerStyle.Render("Basic Information"))
	printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("ID:"), srv.ID))
	printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Name:"), srv.Name))

	// Platform
	platform := srv.Config.Payload.Platform
	printer.Print(fmt.Sprintf("%s%s/%s", labelStyle.Render("Platform:"), platform.OS, platform.Arch))

	// Tags
	if len(srv.Tags) > 0 {
		printer.Print("")
		printer.Print(headerStyle.Render("Tags"))
		for k, v := range srv.Tags {
			printer.Print(fmt.Sprintf("%s%s", labelStyle.Render(k+":"), v))
		}
	}

	// Command Settings
	printer.Print("")
	printer.Print(headerStyle.Render("Command Settings"))
	commands := srv.Config.Payload.Commands
	printer.Print(fmt.Sprintf("%s%v", labelStyle.Render("Allow Literal:"), commands.AllowLiteralCommands))

	// Config version
	printer.Print("")
	printer.Print(fmt.Sprintf("%s%d", labelStyle.Render("Config Version:"), srv.Config.Version))

	printer.Print("")
}

func init() {
	ServersCmd.AddCommand(DescribeCmd)
}
