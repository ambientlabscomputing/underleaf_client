package cluster

import (
	"fmt"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/internal/mdns"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var (
	discoverTimeout time.Duration
	discoverJSON    bool
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover Underleaf nodes on the local network via mDNS",
	Long: `Discover Underleaf nodes on the local network using mDNS service discovery.

This command scans the local network for Underleaf nodes broadcasting their
presence via mDNS. Discovered nodes are potential cluster candidates but are
considered UNTRUSTED until explicitly added to a cluster via the trust ceremony.

Discovery does NOT imply cluster membership. Nodes visible on the LAN must be
manually added to clusters with explicit verification.

Examples:
  # Discover nodes with default 5 second timeout
  ufctl cluster discover

  # Discover with longer timeout
  ufctl cluster discover --timeout 10s

  # Output as JSON
  ufctl cluster discover --json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		logger := logging.GetLogger(ctx)
		printer := ui.GetPrinter(ctx)

		printer.Print("🔍 Discovering Underleaf nodes on local network...")
		printer.Print(fmt.Sprintf("   Scanning for %v...\n", discoverTimeout))

		// Create mDNS client
		client := mdns.NewClient(logger)

		// Discover nodes with timeout
		services, err := client.DiscoverAll(ctx, discoverTimeout)
		if err != nil {
			return fmt.Errorf("discovery failed: %w", err)
		}

		if len(services) == 0 {
			printer.PrintWarning("No Underleaf nodes discovered on local network")
			printer.Print("\n💡 Make sure:")
			printer.Print("   • Target nodes are running (underleaf_agent serve)")
			printer.Print("   • mDNS is enabled in their config (local.mdns.enabled=true)")
			printer.Print("   • Nodes are on the same local network")
			printer.Print("   • No firewall blocking UDP port 5353")
			return nil
		}

		logger.Info("discovered nodes", "count", len(services))

		if discoverJSON {
			// Output as JSON (Print() handles JSON format automatically)
			return printer.Print(services)
		}

		// Display as table
		printer.PrintSuccess(fmt.Sprintf("✓ Discovered %d node(s)\n", len(services)))

		// Group by cluster vs standalone
		clustered := make([]*mdns.ServiceInfo, 0)
		standalone := make([]*mdns.ServiceInfo, 0)
		leaderNodes := make(map[string]*mdns.ServiceInfo) // clusterID -> leader service

		for _, svc := range services {
			// Check if this is a leader announcement (api.underleaf.local)
			if svc.Hostname == "api.underleaf.local" {
				if svc.ClusterID != "" {
					leaderNodes[svc.ClusterID] = svc
				}
				continue
			}

			// Node-specific announcement
			if svc.ClusterID != "" {
				clustered = append(clustered, svc)
			} else {
				standalone = append(standalone, svc)
			}
		}

		// Print clustered nodes
		if len(clustered) > 0 {
			printer.Print("\n📦 Clustered Nodes:")
			printer.Print("   (Part of existing clusters)")
			printer.Print("")

			for _, svc := range clustered {
				status := "Member"
				if leader, exists := leaderNodes[svc.ClusterID]; exists && leader.IPAddr == svc.IPAddr && leader.Port == svc.Port {
					status = "🔷 Leader"
				}
				printer.Print(fmt.Sprintf("   %-30s %-20s %-20s %-10s %s",
					svc.Hostname,
					fmt.Sprintf("%s:%d", svc.IPAddr, svc.Port),
					svc.ClusterID,
					status,
					svc.Version))
			}
		}

		// Print standalone nodes
		if len(standalone) > 0 {
			printer.Print("\n📍 Standalone Nodes:")
			printer.Print("   (Available for cluster formation)")
			printer.Print("")

			for _, svc := range standalone {
				age := time.Since(svc.DiscoveredAt)
				ageStr := fmt.Sprintf("%ds ago", int(age.Seconds()))
				printer.Print(fmt.Sprintf("   %-30s %-20s %-15s %s",
					svc.Hostname,
					fmt.Sprintf("%s:%d", svc.IPAddr, svc.Port),
					svc.Version,
					ageStr))
			}
		}

		// Print next steps
		printer.Print("\n💡 Next Steps:")
		printer.Print("   • Create a new cluster:    ufctl cluster create --name my-cluster")
		printer.Print("   • Join an existing cluster: ufctl cluster join <node-id> --code XXXX-XXXX")
		printer.Print("   • View cluster status:     ufctl cluster status")

		printer.Print("\n⚠️  Trust Reminder:")
		printer.Print("   Discovery does NOT imply trust or cluster membership.")
		printer.Print("   Nodes must be explicitly added via trust ceremony.")

		return nil
	},
}

func init() {
	discoverCmd.Flags().DurationVarP(&discoverTimeout, "timeout", "t", 5*time.Second, "Discovery timeout")
	discoverCmd.Flags().BoolVar(&discoverJSON, "json", false, "Output as JSON")
	ClusterCmd.AddCommand(discoverCmd)
}
