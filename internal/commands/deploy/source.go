package deploy

import (
	"fmt"
	"os"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	sourceRef         string
	sourceServerID    string
	sourceGitHubToken string
)

// SourceCmd deploys an application directly from a GitHub repository.
var SourceCmd = &cobra.Command{
	Use:   "source <gh:owner/repo[@ref]>",
	Short: "Deploy an app directly from a GitHub repository",
	Long: `Resolves .underleaf/deploy.yaml from a GitHub repository, builds or pulls
the container image on your server, and starts the application.

The source argument must be a "gh:" reference. An optional "[@ref]" suffix
overrides the branch, tag, or commit SHA to deploy from.

Examples:
  ufctl deploy source gh:ambientlabscomputing/hello-world
  ufctl deploy source gh:myorg/my-app@v1.2.0
  ufctl deploy source gh:myorg/my-app --ref main --server <server-id>
  ufctl deploy source gh:myorg/private-app --token $GITHUB_TOKEN`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		source := args[0]
		if !strings.HasPrefix(source, "gh:") {
			deps.Printer.PrintError("source must start with 'gh:' — e.g. gh:owner/repo")
			return fmt.Errorf("invalid source: %s", source)
		}

		// Resolve GitHub token: flag > env var > config
		token := sourceGitHubToken
		if token == "" {
			token = os.Getenv("UNDERLEAF_GITHUB_TOKEN")
		}
		if token == "" {
			if t, ok := deps.ConfigClient.Get("auth.github_token"); ok && t != nil {
				token = fmt.Sprintf("%v", t)
			}
		}

		// Build targeting based on flags
		var targeting *controlplane.SourceTargeting
		if sourceServerID != "" {
			targeting = &controlplane.SourceTargeting{
				Mode:      "server_ids",
				ServerIDs: []string{sourceServerID},
			}
		}

		req := controlplane.DeployFromSourceRequest{
			Source:      source,
			Ref:         sourceRef,
			Targeting:   targeting,
			GitHubToken: token,
		}

		deps.Printer.Print(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render(fmt.Sprintf("\n🚀 Deploying from %s\n", source)))

		resp, err := deps.CPlaneClient.Deployments.DeployFromSource(ctx, req)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Deploy failed: %v", err))
			return err
		}

		deps.Printer.PrintSuccess(fmt.Sprintf("✅ Deployment created: %s", resp.Slug))
		deps.Printer.Print(fmt.Sprintf("   Deployment ID : %s", resp.DeploymentID))
		deps.Printer.Print(fmt.Sprintf("   Job ID        : %s", resp.JobID))
		deps.Printer.Print(fmt.Sprintf("   Source        : %s", resp.Source))
		deps.Printer.Print(fmt.Sprintf("   Started at    : %s", resp.Timestamp))

		if len(resp.Exposures) > 0 {
			deps.Printer.Print("")
			deps.Printer.Print(lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("86")).
				Render("🌐 Public URLs:"))
			for _, exp := range resp.Exposures {
				deps.Printer.Print(fmt.Sprintf("   %s → %s", exp.ServiceName, exp.PublicURL))
			}
		}

		deps.Printer.Print("")
		deps.Printer.PrintInfo("Use 'ufctl jobs get " + resp.JobID + "' to track progress.")

		return nil
	},
}

func init() {
	SourceCmd.Flags().StringVar(&sourceRef, "ref", "", "Git ref to deploy (branch, tag, or commit SHA)")
	SourceCmd.Flags().StringVar(&sourceServerID, "server", "", "Target a specific server by ID")
	SourceCmd.Flags().StringVar(&sourceGitHubToken, "token", "", "GitHub Personal Access Token for private repos")
}
