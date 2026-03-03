package expose

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	revokeForce bool
)

var RevokeCmd = &cobra.Command{
	Use:   "revoke <exposure-id>",
	Short: "Revoke a public exposure",
	Long: `Revoke (delete) a public exposure and tear down its tunnel.

This will:
1. Revoke the Hyphae lease
2. Close the mTLS tunnel
3. Remove the exposure record

Examples:
  ufctl expose revoke exp-12345678
  ufctl expose revoke exp-12345678 --force`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		exposureID := args[0]

		deps.Printer.Print(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("9")).
			Render("\n🔥 Revoking exposure...\n"))

		// Confirmation
		if !revokeForce {
			deps.Printer.Print(fmt.Sprintf("⚠️  Are you sure you want to revoke exposure %s?", exposureID))
			deps.Printer.Print("   This will close the public tunnel and remove the exposure record.")
			deps.Printer.Print("   Type 'yes' to confirm: ")

			reader := bufio.NewReader(os.Stdin)
			response, err := reader.ReadString('\n')
			if err != nil {
				deps.Printer.PrintError("Error reading input")
				return err
			}

			response = strings.TrimSpace(strings.ToLower(response))
			if response != "yes" {
				deps.Printer.PrintInfo("❌ Revoke cancelled")
				return nil
			}
		}

		// Call API
		if err := deps.CPlaneClient.API().RevokeExposure(ctx, exposureID); err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to revoke exposure: %v", err))
			return err
		}

		deps.Printer.PrintSuccess(fmt.Sprintf("✅ Exposure %s revoked successfully!", exposureID))
		deps.Printer.Print("   Tunnel will close shortly")

		return nil
	},
}

func init() {
	RevokeCmd.Flags().BoolVar(&revokeForce, "force", false, "Skip confirmation prompt")
}
