package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/spf13/cobra"
)

var (
	port int
	mode string
)

var rootCmd = &cobra.Command{
	Use:   "underleaf_agent",
	Short: "Underleaf Agent Daemon",
	Long:  "The Underleaf agent daemon runs on edge servers to manage workloads and communicate with the control plane.",
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the agent HTTP server",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize logging context
		ctx := context.Background()

		// Determine launch mode
		launchMode := agent.ModeDaemon
		if mode == "dev" {
			launchMode = agent.ModeDev
		}

		// Create launcher
		launcher := agent.NewLauncher(agent.LauncherConfig{
			Mode: launchMode,
			Port: port,
		})

		// Create runtime
		runtime := agent.NewRuntime(launcher)

		// Run the agent
		fmt.Printf("Starting Underleaf Agent on port %d...\n", port)
		return runtime.Run(ctx)
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Underleaf Agent v0.0.1")
	},
}

func init() {
	serveCmd.Flags().IntVarP(&port, "port", "p", 8081, "Port to run the agent on")
	serveCmd.Flags().StringVarP(&mode, "mode", "m", "daemon", "Launch mode (dev or daemon)")

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
