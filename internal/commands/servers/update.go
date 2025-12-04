package servers

import (
	"fmt"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/types"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var UpdateCmd = &cobra.Command{
	Use:   "update [server-id]",
	Short: "Update server metadata",
	Long: `Update server metadata such as location, IP address, or hostname.

Examples:
  ufctl servers update server-abc123 --location us-west-2
  ufctl servers update server-abc123 --ip 192.168.1.100 --hostname web01.example.com`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		serverID := args[0]

		// Get update flags
		location, _ := cmd.Flags().GetString("location")
		ipAddress, _ := cmd.Flags().GetString("ip")
		hostname, _ := cmd.Flags().GetString("hostname")

		// Check if at least one flag is provided
		if location == "" && ipAddress == "" && hostname == "" {
			deps.Printer.PrintError("At least one of --location, --ip, or --hostname must be provided")
			return fmt.Errorf("no update fields provided")
		}

		updates := types.UpdateServerRequest{
			Location:  location,
			IPAddress: ipAddress,
			Hostname:  hostname,
		}

		server, err := deps.ServerSvc.UpdateServer(ctx, serverID, updates)
		if err != nil {
			deps.Printer.PrintError("Failed to update server: " + err.Error())
			return err
		}

		deps.Printer.PrintSuccess(fmt.Sprintf("Server %s updated successfully", serverID))

		// Show updated values
		boldStyle := lipgloss.NewStyle().Bold(true)
		labelStyle := lipgloss.NewStyle().Width(20)

		deps.Printer.Print("")
		deps.Printer.Print(boldStyle.Render("Updated Server Details"))
		deps.Printer.Print(strings.Repeat("─", 40))

		if server.Location != "" {
			deps.Printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Location:"), server.Location))
		}
		if server.IPAddress != "" {
			deps.Printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("IP Address:"), server.IPAddress))
		}
		if server.Hostname != "" {
			deps.Printer.Print(fmt.Sprintf("%s%s", labelStyle.Render("Hostname:"), server.Hostname))
		}

		deps.Printer.Print("")
		return nil
	},
}

func init() {
	UpdateCmd.Flags().StringP("location", "l", "", "Server location (e.g., us-east-1)")
	UpdateCmd.Flags().String("ip", "", "Server IP address")
	UpdateCmd.Flags().String("hostname", "", "Server hostname")

	ServersCmd.AddCommand(UpdateCmd)
}
