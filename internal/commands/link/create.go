package link

import (
	"encoding/json"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/spf13/cobra"
)

var (
	createKind       string
	createVisibility string
	createServerID   string
	createHostname   string
	createDeployID   string
	createService    string
	createPort       int
	createTarget     string
	createTargetType string
	createSource     string
	createDest       string
	createPurpose    string
	createTTL        int
)

var CreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a Rhizo link",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		req := controlplane.CreateLinkRequest{
			Kind:       createKind,
			Visibility: createVisibility,
			ServerID:   createServerID,
			Hostname:   createHostname,
			Spec: controlplane.LinkSpec{
				DeploymentID:   createDeployID,
				ServiceName:    createService,
				TargetPort:     createPort,
				Target:         createTarget,
				TargetType:     createTargetType,
				SourceServerID: createSource,
				DestServerID:   createDest,
				Purpose:        createPurpose,
				TTLSeconds:     createTTL,
			},
		}

		resp, err := deps.CPlaneClient.Links.CreateLink(ctx, req)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to create link: %v", err))
			return err
		}
		if deps.Printer.Format() == ui.FormatJSON {
			jsonBytes, _ := json.MarshalIndent(resp, "", "  ")
			deps.Printer.Print(string(jsonBytes))
			return nil
		}
		deps.Printer.PrintSuccess(fmt.Sprintf("Created %s link %s", resp.Link.Kind, resp.Link.ID))
		if resp.Link.PublicURL != "" {
			deps.Printer.Print(resp.Link.PublicURL)
		}
		if resp.Grant != "" {
			deps.Printer.Print(fmt.Sprintf("Grant: %s", resp.Grant))
		}
		return nil
	},
}

func init() {
	CreateCmd.Flags().StringVar(&createKind, "kind", "", "Link kind: exposure, tunnel, channel")
	CreateCmd.Flags().StringVar(&createVisibility, "visibility", "", "Link visibility: public, token, peer")
	CreateCmd.Flags().StringVar(&createServerID, "server", "", "Server ID")
	CreateCmd.Flags().StringVar(&createHostname, "hostname", "", "Requested hostname")
	CreateCmd.Flags().StringVar(&createDeployID, "deployment", "", "Deployment ID")
	CreateCmd.Flags().StringVar(&createService, "service", "", "Deployment service name")
	CreateCmd.Flags().IntVar(&createPort, "port", 0, "Target service port")
	CreateCmd.Flags().StringVar(&createTarget, "target", "", "Tunnel target")
	CreateCmd.Flags().StringVar(&createTargetType, "target-type", "", "Target type: port or url")
	CreateCmd.Flags().StringVar(&createSource, "source-server", "", "Source server ID")
	CreateCmd.Flags().StringVar(&createDest, "dest-server", "", "Destination server ID")
	CreateCmd.Flags().StringVar(&createPurpose, "purpose", "", "Link purpose")
	CreateCmd.Flags().IntVar(&createTTL, "ttl", 0, "TTL in seconds")
	_ = CreateCmd.MarkFlagRequired("kind")
}
