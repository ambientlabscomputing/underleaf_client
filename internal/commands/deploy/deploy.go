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
	deployRef      string
	deployServerID string
	deployToken    string
)

var DeployCmd = &cobra.Command{
	Use:   "deploy [<gh:owner/repo[@ref]> | <local:./path>]",
	Short: "Deploy an application",
	Long: `Deploy an application from a GitHub repository or a local directory.

Source types:
  gh:owner/repo[@ref]   Deploy directly from a GitHub repository (optionally at a
                        specific branch, tag, or commit SHA).
  local:./path          Deploy from a local directory by uploading the build context
                        to your server and applying the .underleaf/deploy.yaml manifest.

Examples:
  ufctl deploy gh:ambientlabscomputing/hello-world
  ufctl deploy gh:myorg/my-app@v1.2.0 --server <server-id>
  ufctl deploy gh:myorg/private-app --token $GITHUB_TOKEN
  ufctl deploy local:.
  ufctl deploy local:./my-app --server <server-id>`,
	Args:              cobra.MaximumNArgs(1),
	PersistentPreRunE: nil,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return cmd.Help()
		}

		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		source := args[0]

		var targeting *controlplane.SourceTargeting
		if deployServerID != "" {
			targeting = &controlplane.SourceTargeting{
				Mode:      "server_ids",
				ServerIDs: []string{deployServerID},
			}
		}

		// Prefer the local server for replica selection so that the machine
		// running `ufctl deploy` is the primary target.
		localServerID := ""
		if sid, ok := deps.ConfigClient.Get("server.id"); ok && sid != nil {
			localServerID, _ = sid.(string)
		}
		if localServerID != "" {
			if targeting == nil {
				targeting = &controlplane.SourceTargeting{Mode: "all"}
			}
			targeting.PreferServerID = localServerID
		}

		switch {
		case strings.HasPrefix(source, "gh:"):
			return runGitHubDeploy(cmd, deps, source, targeting)
		case strings.HasPrefix(source, "local:"):
			return runLocalDeploy(cmd, deps, source, targeting)
		default:
			deps.Printer.PrintError(fmt.Sprintf("Unknown source prefix in %q — expected 'gh:' or 'local:'", source))
			return fmt.Errorf("unknown source prefix: %s", source)
		}
	},
}

func init() {
	DeployCmd.Flags().StringVar(&deployRef, "ref", "", "Git ref to deploy (branch, tag, or commit SHA; gh: only)")
	DeployCmd.Flags().StringVar(&deployServerID, "server", "", "Target a specific server by ID")
	DeployCmd.Flags().StringVar(&deployToken, "token", "", "GitHub Personal Access Token for private repos (gh: only)")

	DeployCmd.AddCommand(ListCmd)
	DeployCmd.AddCommand(CompileCmd)
	DeployCmd.AddCommand(PlanCmd)
	DeployCmd.AddCommand(DiffCmd)
	DeployCmd.AddCommand(CapabilitiesCmd)
	DeployCmd.AddCommand(PsCmd)
	DeployCmd.AddCommand(RmCmd)
}

// runGitHubDeploy handles gh: sources — resolves via UCRS and creates a tracked deployment.
func runGitHubDeploy(cmd *cobra.Command, deps *utils.DependencyManager, source string, targeting *controlplane.SourceTargeting) error {
	ctx := cmd.Context()

	// Resolve GitHub token: flag > env var > config
	token := deployToken
	if token == "" {
		token = os.Getenv("UNDERLEAF_GITHUB_TOKEN")
	}
	if token == "" {
		if t, ok := deps.ConfigClient.Get("auth.github_token"); ok && t != nil {
			token = fmt.Sprintf("%v", t)
		}
	}

	req := controlplane.DeployFromSourceRequest{
		Source:      source,
		Ref:         deployRef,
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

	printDeployResult(deps, resp)
	return nil
}

// printDeployResult prints the shared success output for both gh: and local: deploys.
func printDeployResult(deps *utils.DependencyManager, resp *controlplane.DeployFromSourceResponse) {
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
}
