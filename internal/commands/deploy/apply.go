package deploy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/compiler"
	"github.com/ambientlabscomputing/underleaf_client/internal/compiler/state"
	"github.com/ambientlabscomputing/underleaf_client/internal/runner"
	"github.com/ambientlabscomputing/underleaf_client/internal/types"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var (
	applyAutoApprove bool
	applyDryRun      bool
	applyStateDir    string
	applyReportDir   string
	applyOutput      string
)

var ApplyCmd = &cobra.Command{
	Use:   "apply [deployment-file]",
	Short: "Apply a deployment to Docker",
	Long: `Compiles a deployment, performs three-way reconciliation, generates an execution plan,
and applies it to Docker. This will create, update, or delete resources as needed.

Examples:
  ufctl deploy apply my-app.json
  ufctl deploy apply my-app.json --auto-approve
  ufctl deploy apply my-app.json --dry-run
  ufctl deploy apply my-app.json --state-dir ./state --report-dir ./reports`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		deploymentFile := args[0]

		deps.Printer.Print(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render(fmt.Sprintf("\n🚀 Applying deployment from %s\n", deploymentFile)))

		// Step 1: Load deployment
		deployment, err := loadDeploymentSpec(deploymentFile)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to load deployment: %v", err))
			return err
		}
		deps.Printer.PrintSuccess(fmt.Sprintf("✅ Loaded: %s (v%d)", deployment.Slug, deployment.Version))

		// Step 2: Compile
		c := compiler.NewCompiler()
		graph, err := c.Compile(deployment)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Compilation failed: %v", err))
			return err
		}
		deps.Printer.PrintSuccess(fmt.Sprintf("✅ Compiled: %d resources, %d edges", len(graph.Nodes), len(graph.Edges)))

		// Step 3: Query Docker state
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
		deps.Printer.PrintSuccess(fmt.Sprintf("✅ Observed: %d networks, %d volumes, %d containers",
			len(observed.Networks), len(observed.Volumes), len(observed.Containers)))

		// Step 4: Load last applied state
		lastAppliedStore := state.NewFileLastAppliedStore(applyStateDir)
		lastApplied, err := lastAppliedStore.GetLatest(deployment.ID)
		if err != nil {
			deps.Printer.PrintInfo("   No previous state found (new deployment)")
			lastApplied = nil
		} else {
			deps.Printer.PrintSuccess(fmt.Sprintf("✅ Last Applied: v%d", lastApplied.Version))
		}

		// Step 5: Reconcile
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

		// Show operation summary
		opCounts := make(map[types.OperationType]int)
		for _, op := range diff.Operations {
			opCounts[op.Type]++
		}
		deps.Printer.PrintSuccess(fmt.Sprintf("✅ Reconciled: %d operations", len(diff.Operations)))
		for opType, count := range opCounts {
			deps.Printer.Print(fmt.Sprintf("   - %s: %d", opType, count))
		}

		// Step 6: Generate plan
		planner := compiler.NewPlanner()
		plan, err := planner.GeneratePlan(graph, diff, types.PlanOptions{
			DryRun:       applyDryRun,
			DeploymentID: deployment.ID,
			ToVersion:    deployment.Version + 1,
			FromVersion:  0,
			Slug:         deployment.Slug,
		})
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Plan generation failed: %v", err))
			return err
		}
		deps.Printer.PrintSuccess(fmt.Sprintf("✅ Plan generated: %d operations, ~%d seconds\n",
			len(plan.Operations), plan.EstimatedSeconds))

		// Show execution plan
		deps.Printer.Print(lipgloss.NewStyle().Bold(true).Render("📋 Execution Plan:"))
		for i, op := range plan.Operations {
			icon := getOperationIcon(op.Type)
			deps.Printer.Print(fmt.Sprintf("  [%d] %s %s - %s", i+1, icon, op.Type, op.Description))
		}
		deps.Printer.Print("")

		// Step 7: Confirmation (unless auto-approve or dry-run)
		if !applyAutoApprove && !applyDryRun {
			if !confirmApply(deps) {
				deps.Printer.PrintInfo("❌ Apply cancelled by user")
				return nil
			}
		}

		if applyDryRun {
			deps.Printer.PrintInfo("\n🔍 Dry-run mode - skipping execution")
			deps.Printer.PrintSuccess("✅ Plan validated successfully")
			return nil
		}

		// Step 8: Execute plan
		deps.Printer.Print(lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("11")).
			Render("\n⚡ Executing deployment...\n"))

		r, err := runner.NewRunner(applyReportDir)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to create runner: %v", err))
			return err
		}

		result, err := r.Execute(ctx, plan)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Execution failed: %v", err))
			return err
		}

		// Step 9: Handle result
		if result.Success {
			deps.Printer.Print(lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("10")).
				Render(fmt.Sprintf("\n✅ Deployment successful! (v%d)\n", result.Version)))

			// Step 10: Save last-applied state
			snapshot := createSnapshot(deployment, graph, result)
			if err := lastAppliedStore.Save(snapshot); err != nil {
				deps.Printer.PrintError(fmt.Sprintf("⚠️  Warning: Failed to save state: %v", err))
			} else {
				deps.Printer.PrintSuccess(fmt.Sprintf("✅ State saved: %s/v%d.json", applyStateDir, snapshot.Version))
			}

			// Show summary
			deps.Printer.Print(fmt.Sprintf("\n📊 Summary:"))
			deps.Printer.Print(fmt.Sprintf("   Duration: %v", result.CompletedAt.Sub(result.StartedAt)))
			deps.Printer.Print(fmt.Sprintf("   Operations: %d/%d successful", len(result.Results), len(plan.Operations)))
		} else {
			deps.Printer.Print(lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("9")).
				Render(fmt.Sprintf("\n❌ Deployment failed: %s\n", result.Error)))

			deps.Printer.PrintError(fmt.Sprintf("⚠️  Partial state detected - some resources may have been created"))
			deps.Printer.PrintError(fmt.Sprintf("   Check report: %s", applyReportDir))

			// Show failed operation
			for i, opResult := range result.Results {
				if !opResult.Success {
					deps.Printer.PrintError(fmt.Sprintf("\n   Failed at operation %d:", i+1))
					deps.Printer.PrintError(fmt.Sprintf("   - Type: %s", opResult.Type))
					deps.Printer.PrintError(fmt.Sprintf("   - Resource: %s", opResult.ResourceName))
					deps.Printer.PrintError(fmt.Sprintf("   - Error: %s", opResult.Error))
					break
				}
			}

			return fmt.Errorf("deployment failed: %s", result.Error)
		}

		// Write result to file if requested
		if applyOutput != "" {
			data, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				deps.Printer.PrintError(fmt.Sprintf("Failed to marshal result: %v", err))
			} else if err := os.WriteFile(applyOutput, data, 0644); err != nil {
				deps.Printer.PrintError(fmt.Sprintf("Failed to write result file: %v", err))
			} else {
				deps.Printer.PrintSuccess(fmt.Sprintf("✅ Result written to %s", applyOutput))
			}
		}

		return nil
	},
}

func init() {
	ApplyCmd.Flags().BoolVar(&applyAutoApprove, "auto-approve", false, "Skip confirmation prompt")
	ApplyCmd.Flags().BoolVar(&applyDryRun, "dry-run", false, "Plan only, do not execute")
	ApplyCmd.Flags().StringVar(&applyStateDir, "state-dir", "./state", "Directory for last-applied state storage")
	ApplyCmd.Flags().StringVar(&applyReportDir, "report-dir", "./reports", "Directory for execution reports")
	ApplyCmd.Flags().StringVarP(&applyOutput, "output", "o", "", "Output file for execution result (JSON)")
}

// confirmApply prompts the user for confirmation
func confirmApply(deps *utils.DependencyManager) bool {
	deps.Printer.Print(lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("11")).
		Render("\n⚠️  Do you want to apply these changes?"))
	deps.Printer.Print("   Type 'yes' to confirm: ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "yes"
}

// getOperationIcon returns an emoji icon for each operation type
func getOperationIcon(opType types.OperationType) string {
	switch opType {
	case "create":
		return "➕"
	case "update":
		return "🔄"
	case "delete":
		return "🗑️"
	default:
		return "▪️"
	}
}

// createSnapshot converts a compiled graph to a last-applied snapshot
func createSnapshot(deployment *types.AppDeployment, graph *types.CompiledGraph, result *types.ExecutionResult) *types.LastAppliedSnapshot {
	resources := make(map[types.ResourceID]interface{})

	// Extract configs from graph nodes
	for id, node := range graph.Nodes {
		resources[id] = node.Config
	}

	return &types.LastAppliedSnapshot{
		DeploymentID: deployment.ID,
		Version:      result.Version,
		Resources:    resources,
		AppliedAt:    result.CompletedAt,
	}
}
