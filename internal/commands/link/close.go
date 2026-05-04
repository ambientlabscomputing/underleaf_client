package link

import (
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/spf13/cobra"
)

var CloseCmd = &cobra.Command{
	Use:     "close <id>",
	Aliases: []string{"delete", "rm"},
	Short:   "Close a Rhizo link",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		if err := deps.CPlaneClient.Links.CloseLink(ctx, args[0]); err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to close link: %v", err))
			return err
		}
		deps.Printer.PrintSuccess(fmt.Sprintf("Closed link %s", args[0]))
		return nil
	},
}
