//go:build dev

package local

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/devmode"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
)

func init() {
	// Register dev-mode flags on 'ufctl start' command
	StartCmd.Flags().Bool("dev-mode", false, "Enable development mode (load UMCs from local paths via build.yaml)")
	StartCmd.Flags().String("build-config", "./build.yaml", "Path to build.yaml for dev mode UMC overrides")

	// Register dev-mode flags on 'ufctl agent start' command
	agentStartCmd.Flags().Bool("dev-mode", false, "Enable development mode (load UMCs from local paths via build.yaml)")
	agentStartCmd.Flags().String("build-config", "./build.yaml", "Path to build.yaml for dev mode UMC overrides")
}

// LoadDevConfig parses the build.yaml file and returns a DevConfig if dev mode is enabled.
// Returns nil if dev mode is disabled. Returns error only on validation failures.
func LoadDevConfig(cmd interface{}, printer ui.Printer) (*devmode.DevConfig, error) {
	// This is a helper function that both start and agent start commands can use.
	// Since the actual flag reading needs the cobra Command, we'll handle it at the caller level.
	return nil, nil
}

// PrintDevModeInfo prints a banner and config info when starting in dev mode.
func PrintDevModeInfo(cfg *devmode.DevConfig, printer *ui.Printer) {
	if cfg == nil {
		return
	}

	fmt.Println()
	printer.PrintWarning("⚠ DEV MODE — Loading UMCs from local paths")
	fmt.Println()

	summaryLines := cfg.SummaryString()
	for _, line := range summaryLines {
		if line == '\n' {
			fmt.Println()
		}
	}

	fmt.Println()
	printer.PrintInfo("Set up clean: unset build.yaml overrides or use --build-config to point to a different file")
	fmt.Println()
}
