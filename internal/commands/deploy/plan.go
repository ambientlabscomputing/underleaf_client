package deploy

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/compiler"
	"github.com/ambientlabscomputing/underleaf_client/internal/compiler/state"
	"github.com/ambientlabscomputing/underleaf_client/internal/types"
	"github.com/spf13/cobra"
)

var (
	planOutput       string
	planStateDir     string
	planPruneUnknown bool
	planAdoptOrphans bool
)

var PlanCmd = &cobra.Command{
	Use:   "plan [deployment-file]",
	Short: "Generate an execution plan for a deployment",
	Long: `Compiles a deployment, queries Docker state, performs three-way reconciliation,
and generates a complete execution plan.

Examples:
  ufctl deploy plan my-app.json
  ufctl deploy plan my-app.json -o plan.json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		deploymentFile := args[0]

		deps.Printer.PrintInfo(fmt.Sprintf("📋 Generating execution plan for %s...\n", deploymentFile))

		deployment, err := loadDeploymentSpec(deploymentFile)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to load deployment: %v", err))
			return err
		}
		deps.Printer.PrintSuccess(fmt.Sprintf("✅ Loaded: %s (v%d)", deployment.Slug, deployment.Version))

		c := compiler.NewCompiler()
		graph, err := c.Compile(deployment)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Compilation failed: %v", err))
			return err
		}
		deps.Printer.PrintSuccess(fmt.Sprintf("✅ Compiled: %d resources", len(graph.Nodes)))

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

		lastAppliedStore := state.NewFileLastAppliedStore(planStateDir)
		lastApplied, err := lastAppliedStore.GetLatest(deployment.ID)
		if err != nil {
			deps.Printer.PrintInfo("   No previous state found (new deployment)")
			lastApplied = nil
		} else {
			deps.Printer.PrintSuccess(fmt.Sprintf("✅ Last Applied: v%d", lastApplied.Version))
		}

		reconciler := compiler.NewReconciler()
		diff, err := reconciler.Reconcile(graph, observed, lastApplied, types.ReconcileOptions{
			PruneUnknown: planPruneUnknown,
			AdoptOrphan:  planAdoptOrphans,
		})
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Reconciliation failed: %v", err))
			return err
		}

		opCounts := make(map[types.OperationType]int)
		for _, op := range diff.Operations {
			opCounts[op.Type]++
		}
		deps.Printer.PrintSuccess(fmt.Sprintf("✅ Reconciled: %d operations", len(diff.Operations)))
		for opType, count := range opCounts {
			deps.Printer.Print(fmt.Sprintf("   - %s: %d", opType, count))
		}

		planner := compiler.NewPlanner()
		plan, err := planner.GeneratePlan(graph, diff, types.PlanOptions{
			DryRun:       false,
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

		deps.Printer.Print("📊 Execution Plan:")
		for i, op := range plan.Operations {
			deps.Printer.Print(fmt.Sprintf("  [%d] %s - %s", i+1, op.Type, op.Description))
		}

		output := map[string]interface{}{
			"deployment":     deployment,
			"compiled_graph": graph,
			"observed_state": observed,
			"last_applied":   lastApplied,
			"diff_results":   diff,
			"execution_plan": plan,
		}

		if planOutput != "" {
			data, err := json.MarshalIndent(output, "", "  ")
			if err != nil {
				deps.Printer.PrintError(fmt.Sprintf("Failed to marshal output: %v", err))
				return err
			}

			if err := os.WriteFile(planOutput, data, 0644); err != nil {
				deps.Printer.PrintError(fmt.Sprintf("Failed to write output file: %v", err))
				return err
			}

			deps.Printer.PrintSuccess(fmt.Sprintf("\n✅ Plan written to %s", planOutput))
		} else {
			data, _ := json.MarshalIndent(output, "", "  ")
			deps.Printer.Print("\n" + string(data))
		}

		return nil
	},
}

func init() {
	PlanCmd.Flags().StringVarP(&planOutput, "output", "o", "", "Output file for execution plan (JSON)")
	PlanCmd.Flags().StringVar(&planStateDir, "state-dir", "./state", "Directory for last-applied state storage")
	PlanCmd.Flags().BoolVar(&planPruneUnknown, "prune-unknown", false, "Remove unknown resources with matching slug")
	PlanCmd.Flags().BoolVar(&planAdoptOrphans, "adopt-orphans", false, "Adopt orphaned resources if config matches")
}
