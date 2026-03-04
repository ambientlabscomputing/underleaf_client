//go:build !dev

package local

import (
	"github.com/ambientlabscomputing/underleaf_client/internal/devmode"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
)

func init() {
	// No-op in production builds
}

// LoadDevConfig returns nil in production builds.
func LoadDevConfig(cmd interface{}, printer ui.Printer) (*devmode.DevConfig, error) {
	return nil, nil
}

// PrintDevModeInfo is a no-op in production builds.
func PrintDevModeInfo(cfg *devmode.DevConfig, printer *ui.Printer) {
	// No-op
}
