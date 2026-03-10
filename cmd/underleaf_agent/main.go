package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ambientlabscomputing/underleaf_client/internal/agent"
	"github.com/ambientlabscomputing/underleaf_client/internal/devmode"
	"github.com/ambientlabscomputing/underleaf_client/internal/logging"
	"github.com/ambientlabscomputing/underleaf_client/pkg/version"
	"github.com/spf13/cobra"
)

var (
	port            int
	mode            string
	buildConfigPath string
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

		// Determine launch mode first
		launchMode := agent.ModeDaemon
		if mode == "dev" {
			launchMode = agent.ModeDev
		}

		// Initialize logging for the agent
		// Always use LoggerModeAgent for file-based logging since daemon mode redirects
		// stdout/stderr to a file anyway, and LoggerModeDev (tint) doesn't work well
		// when stdout is redirected
		logFile := filepath.Join(os.TempDir(), "underleaf-agent-structured.log")
		ctx, _ = logging.Init(ctx, logging.LoggerModeAgent, &logFile)

		// Create launcher
		var devConfig *devmode.DevConfig
		if buildConfigPath != "" {
			var err error
			devConfig, err = devmode.LoadBuildConfig(buildConfigPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to load build config: %v\n", err)
			} else {
				fmt.Printf("DEV MODE: loaded build config from %s\n", buildConfigPath)
			}
		}

		launcher := agent.NewLauncher(agent.LauncherConfig{
			Mode:      launchMode,
			Port:      port,
			DevConfig: devConfig,
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
		fmt.Printf("Underleaf Agent %s\n", version.Version)
	},
}

func init() {
	serveCmd.Flags().IntVarP(&port, "port", "p", 2240, "Port to run the agent on")
	serveCmd.Flags().StringVarP(&mode, "mode", "m", "daemon", "Launch mode (dev or daemon)")
	serveCmd.Flags().StringVar(&buildConfigPath, "build-config", "", "Path to build.yaml for dev mode UMC overrides")

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(versionCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
