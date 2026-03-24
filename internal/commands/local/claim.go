package local

import (
	"context"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	servertypes "github.com/ambientlabscomputing/underleaf_client/internal/types/server"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
)

// claimExistingServer downloads an existing server's identity into the local config.
//
// If server is nil, the user is prompted for a name or ID to look up. The 5-minute
// staleness check is enforced by FindExistingServer — live servers cannot be claimed.
//
// If server is non-nil (already looked up by the caller, e.g. from collision detection),
// the lookup step is skipped and the user is shown the details before confirming.
func claimExistingServer(ctx context.Context, deps *utils.DependencyManager, server *servertypes.Server) error {
	if server == nil {
		// Prompt for the name or UUID to claim
		nameOrID, err := ui.PromptInput("Enter the name or ID of the server to claim:", "")
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}
		if nameOrID == "" {
			return fmt.Errorf("no server name or ID provided, claim cancelled")
		}

		found, err := deps.ServerSvc.FindExistingServer(ctx, nameOrID)
		if err != nil {
			return fmt.Errorf("could not find claimable server: %w", err)
		}
		server = found
	}

	// Show what is about to be claimed
	deps.Printer.Print("")
	deps.Printer.PrintInfo("Server to claim:")
	deps.Printer.PrintInfo("  ID:   " + server.ID)
	deps.Printer.PrintInfo("  Name: " + server.Name)
	if server.LastCheckIn != nil && server.LastCheckIn.Valid {
		deps.Printer.PrintInfo("  Last check-in: " + server.LastCheckIn.Time.Format("2006-01-02 15:04:05"))
	} else {
		deps.Printer.PrintInfo("  Last check-in: Never")
	}
	deps.Printer.Print("")

	confirmed, err := ui.Confirm(fmt.Sprintf("Claim server '%s' and download its identity to this machine?", server.Name))
	if err != nil {
		return fmt.Errorf("confirmation failed: %w", err)
	}
	if !confirmed {
		return fmt.Errorf("claim cancelled")
	}

	// Write server_id, server_name (and location if set) to local config
	if err := deps.ServerSvc.DownloadServerConfig(ctx, server); err != nil {
		return fmt.Errorf("failed to download server config: %w", err)
	}

	// Pull the policy snapshot (best-effort — agent will retry on start)
	if err := downloadAndSaveConfigSnapshot(ctx, &deps.Printer, server.ID); err != nil {
		deps.Printer.PrintWarning("Failed to download config snapshot (will retry when agent starts): " + err.Error())
	}

	deps.Printer.PrintSuccess("✔ Identity claimed: " + server.Name)
	return nil
}
