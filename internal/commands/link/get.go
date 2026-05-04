package link

import (
	"encoding/json"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var GetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a Rhizo link",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		link, err := deps.CPlaneClient.Links.GetLink(ctx, args[0])
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to get link: %v", err))
			return err
		}
		if deps.Printer.Format() == ui.FormatJSON {
			jsonBytes, _ := json.MarshalIndent(link, "", "  ")
			deps.Printer.Print(string(jsonBytes))
			return nil
		}
		deps.Printer.Print(fmt.Sprintf("ID:         %s", link.ID))
		deps.Printer.Print(fmt.Sprintf("Kind:       %s", link.Kind))
		deps.Printer.Print(fmt.Sprintf("Visibility: %s", link.Visibility))
		deps.Printer.Print(fmt.Sprintf("State:      %s/%s", link.State, link.Status))
		if link.PublicURL != "" {
			deps.Printer.Print(fmt.Sprintf("URL:        %s", link.PublicURL))
		}
		return nil
	},
}
