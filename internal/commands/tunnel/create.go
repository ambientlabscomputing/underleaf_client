package tunnel

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	agentclient "github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
)

var (
	createServerID string
	createHostname string
)

// CreateCmd creates a public tunnel to a local port or URL.
var CreateCmd = &cobra.Command{
	Use:   "create <target>",
	Short: "Create a public tunnel",
	Long: `Create an on-demand public HTTPS tunnel via the Hyphae gateway.

<target> can be a local port number (e.g. 8080) or a full URL
(e.g. http://192.168.1.50:3000).

Without --server, the tunnel runs locally in the foreground and closes when
you press Ctrl+C (like kubectl port-forward).

With --server <id>, the tunnel runs persistently on the remote server and
must be closed with 'ufctl tunnel close <id>'.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := args[0]
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		deps.Printer.Print(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render("\n🔗 Creating tunnel...\n"))

		resp, err := deps.CPlaneClient.Tunnels.CreateTunnel(ctx, controlplane.CreateTunnelRequest{
			Target:   target,
			Hostname: createHostname,
			ServerID: createServerID,
		})
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to create tunnel: %v", err))
			return err
		}

		// Remote tunnel — print details and return immediately.
		if createServerID != "" {
			deps.Printer.PrintSuccess("✅ Tunnel created (server is binding in background):")
			deps.Printer.Print(fmt.Sprintf("   ID:       %s", resp.ID))
			deps.Printer.Print(fmt.Sprintf("   Target:   %s", resp.Target))
			deps.Printer.Print(fmt.Sprintf("   Hostname: %s", resp.Hostname))
			deps.Printer.Print(fmt.Sprintf("   Status:   %s", resp.Status))
			if resp.PublicURL != "" {
				deps.Printer.Print(fmt.Sprintf("   URL:      %s", resp.PublicURL))
			}
			deps.Printer.Print(fmt.Sprintf("\nCheck status with: ufctl tunnel get %s", resp.ID))
			return nil
		}

		// Local tunnel — ask the local agent to bind it, then block until Ctrl+C.
		port := 2240
		if portVal, ok := deps.ConfigClient.Get("agent.port"); ok {
			if portInt, ok := portVal.(int); ok {
				port = portInt
			}
		}

		agentCli := agentclient.NewClient(port)

		targetType := "url"
		if _, parseErr := strconv.Atoi(target); parseErr == nil {
			targetType = "port"
		}

		deps.Printer.Print("Binding tunnel via local Underleaf agent...")
		bindResp, err := agentCli.BindTunnel(agentclient.TunnelBindHTTPRequest{
			TunnelID:         resp.ID,
			LeaseID:          resp.LeaseID,
			Hostname:         resp.Hostname,
			Target:           target,
			TargetType:       targetType,
			HyphaeTunnelAddr: resp.HyphaeTunnelAddr,
		})
		if err != nil {
			// Best-effort cleanup of the pending tunnel record.
			_ = deps.CPlaneClient.Tunnels.CloseTunnel(context.Background(), resp.ID)
			deps.Printer.PrintError(fmt.Sprintf("Failed to bind tunnel: %v", err))
			return err
		}

		deps.Printer.PrintSuccess("\n✅ Tunnel established!")
		deps.Printer.Print(fmt.Sprintf("   %s  →  %s", bindResp.PublicURL, target))
		deps.Printer.Print("\nPress Ctrl+C to close the tunnel.\n")

		// Block until SIGINT or SIGTERM.
		sigCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
		<-sigCtx.Done()

		deps.Printer.Print("\nClosing tunnel...")
		_ = agentCli.UnbindTunnel(resp.ID)
		_ = deps.CPlaneClient.Tunnels.CloseTunnel(context.Background(), resp.ID)
		deps.Printer.PrintSuccess("Tunnel closed.")
		return nil
	},
}

func init() {
	CreateCmd.Flags().StringVar(&createServerID, "server", "", "Run tunnel on a remote server (persistent)")
	CreateCmd.Flags().StringVar(&createHostname, "hostname", "", "Custom public hostname (auto-generated if omitted)")
}
