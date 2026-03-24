package deploy

import (
	"encoding/json"
	"fmt"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/compiler"
	"github.com/ambientlabscomputing/underleaf_client/internal/compiler/state"
	"github.com/ambientlabscomputing/underleaf_client/internal/types"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	diffStateDir    string
	diffShowConfigs bool
)

var DiffCmd = &cobra.Command{
	Use:   "diff [deployment-file]",
	Short: "Show what changes would be made to reach desired state",
	Long: `Performs three-way diff reconciliation showing what operations would be
performed without actually executing them.

Examples:
  ufctl deploy diff my-app.json
  ufctl deploy diff my-app.json --show-configs`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		deploymentFile := args[0]

		deployment, err := loadDeploymentSpec(deploymentFile)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to load deployment: %v", err))
			return err
		}

		deps.Printer.Print(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render(fmt.Sprintf("\n🔍 Analyzing deployment: %s (v%d)\n", deployment.Slug, deployment.Version)))

		c := compiler.NewCompiler()
		graph, err := c.Compile(deployment)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Compilation failed: %v", err))
			return err
		}

		observedStore, err := state.NewDockerObservedStateStore()
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to create Docker client: %v", err))
			return err
		}
		observed, err := observedStore.GetDeploymentResources(ctx, deployment.ID, deployment.Slug)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to query Docker state: %v", err))
			return err
		}

		lastAppliedStore := state.NewFileLastAppliedStore(diffStateDir)
		lastApplied, _ := lastAppliedStore.GetLatest(deployment.ID)

		reconciler := compiler.NewReconciler()
		diff, err := reconciler.Reconcile(graph, observed, lastApplied, types.ReconcileOptions{
			PruneUnknown: false,
			AdoptOrphan:  false,
		})
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Reconciliation failed: %v", err))
			return err
		}

		if len(diff.Operations) == 0 {
			deps.Printer.PrintSuccess("\n✅ No changes required - deployment matches desired state")
			return nil
		}

		table := ui.NewTableBuilder().
			WithTitle("Changes").
			WithHeaders("TYPE", "RESOURCE", "NAME", "REASON")

		for _, op := range diff.Operations {
			table.AddRow(
				colorizeOpType(string(op.Type)),
				string(op.ResourceType),
				truncate(op.ResourceName, 30),
				truncate(op.Description, 40),
			)
		}

		deps.Printer.Print("")
		deps.Printer.PrintTable(table)

		opCounts := make(map[types.OperationType]int)
		for _, op := range diff.Operations {
			opCounts[op.Type]++
		}

		deps.Printer.Print("\n" + lipgloss.NewStyle().
			Bold(true).
			Render("Summary:"))
		for opType, count := range opCounts {
			deps.Printer.Print(fmt.Sprintf("  %s: %d", opType, count))
		}

		if diffShowConfigs {
			deps.Printer.Print("\n" + lipgloss.NewStyle().
				Bold(true).
				Render("Detailed Configurations:"))
			for _, op := range diff.Operations {
				deps.Printer.Print(fmt.Sprintf("\n[%s] %s:", op.Type, op.ResourceName))
				if op.DesiredConfig != nil {
					data, _ := json.MarshalIndent(op.DesiredConfig, "  ", "  ")
					deps.Printer.Print("  Desired:")
					deps.Printer.Print("  " + string(data))
				}
				if op.ObservedConfig != nil {
					data, _ := json.MarshalIndent(op.ObservedConfig, "  ", "  ")
					deps.Printer.Print("  Observed:")
					deps.Printer.Print("  " + string(data))
				}
			}
		}

		return nil
	},
}

func init() {
	DiffCmd.Flags().StringVar(&diffStateDir, "state-dir", "./state", "Directory for last-applied state storage")
	DiffCmd.Flags().BoolVar(&diffShowConfigs, "show-configs", false, "Show detailed resource configurations")
}

func colorizeOpType(opType string) string {
	switch opType {
	case "create":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(opType)
	case "update":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(opType)
	case "delete":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(opType)
	case "noop":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(opType)
	default:
		return opType
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
