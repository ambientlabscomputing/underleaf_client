package deploy

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/compiler"
	"github.com/ambientlabscomputing/underleaf_client/internal/types"
	"github.com/spf13/cobra"
)

var (
	compileOutput string
)

var CompileCmd = &cobra.Command{
	Use:   "compile [deployment-file]",
	Short: "Compile a deployment specification into a dependency graph",
	Long: `Compiles an application deployment JSON file into a full dependency graph
with topologically sorted creation and deletion orders.

Examples:
  ufctl deploy compile my-app.json
  ufctl deploy compile my-app.json -o compiled.json`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		deploymentFile := args[0]

		deps.Printer.PrintInfo(fmt.Sprintf("📦 Compiling deployment from %s...\n", deploymentFile))

		deployment, err := loadDeploymentSpec(deploymentFile)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to load deployment: %v", err))
			return err
		}

		deps.Printer.PrintSuccess(fmt.Sprintf("✅ Loaded deployment: %s (v%d)", deployment.Slug, deployment.Version))
		deps.Printer.Print(fmt.Sprintf("   ID: %s", deployment.ID))
		deps.Printer.Print(fmt.Sprintf("   Services: %d, Networks: %d, Volumes: %d, Capabilities: %d\n",
			len(deployment.Services), len(deployment.Networks), len(deployment.Volumes), len(deployment.CapabilityRequirements)))

		// Validate secret references against the control plane.
		if warns := compiler.ValidateSecretRefs(ctx, deployment, newSecretsLister(deps.CPlaneClient.Secrets)); len(warns) > 0 {
			for _, w := range warns {
				deps.Printer.PrintInfo(fmt.Sprintf("⚠ secret ref '${secret:%s}': %s", w.SecretRef, w.Reason))
			}
		}

		// Show capability requirements if present
		if len(deployment.CapabilityRequirements) > 0 {
			deps.Printer.PrintSuccess(fmt.Sprintf("📋 Capability requirements: %d", len(deployment.CapabilityRequirements)))
			for _, req := range deployment.CapabilityRequirements {
				label := req.CapabilityID
				if req.Alias != "" {
					label = req.Alias + " (" + req.CapabilityID + ")"
				}
				version := "latest"
				if req.VersionRange != "" {
					version = req.VersionRange
				}
				deps.Printer.Print(fmt.Sprintf("   - %s @ %s", label, version))
			}
			deps.Printer.Print("")
		}

		// Compile container resources if present
		var graph *types.CompiledGraph
		if len(deployment.Services) > 0 || len(deployment.Networks) > 0 || len(deployment.Volumes) > 0 {
			c := compiler.NewCompiler()
			var compileErr error
			graph, compileErr = c.Compile(deployment)
			if compileErr != nil {
				deps.Printer.PrintError(fmt.Sprintf("Compilation failed: %v", compileErr))
				return compileErr
			}
			deps.Printer.PrintSuccess(fmt.Sprintf("✅ Graph compiled: %d resources, %d edges",
				len(graph.Nodes), len(graph.Edges)))
			deps.Printer.Print(fmt.Sprintf("   Creation order: %s", formatResourceTypes(graph.CreationOrder)))
			deps.Printer.Print(fmt.Sprintf("   Deletion order: %s\n", formatResourceTypes(graph.DeletionOrder)))
		} else {
			deps.Printer.PrintInfo("   No container resources (capability-only recipe)")
		}

		// Build output
		output := map[string]interface{}{
			"deployment": deployment,
		}

		if len(deployment.CapabilityRequirements) > 0 {
			output["capability_requirements"] = deployment.CapabilityRequirements
		}

		if graph != nil {
			// Convert graph nodes map to slice for JSON serialization
			nodes := make([]types.ResourceNode, 0, len(graph.Nodes))
			for _, node := range graph.Nodes {
				nodes = append(nodes, node)
			}

			// Convert edges map to serializable format
			edgesSlice := make([]map[string]interface{}, 0, len(graph.Edges))
			for from, toList := range graph.Edges {
				edgesSlice = append(edgesSlice, map[string]interface{}{
					"from": from,
					"to":   toList,
				})
			}

			output["compiled_service"] = map[string]interface{}{
				"nodes":          nodes,
				"edges":          edgesSlice,
				"creation_order": graph.CreationOrder,
				"deletion_order": graph.DeletionOrder,
			}
		}

		if compileOutput != "" {
			data, err := json.MarshalIndent(output, "", "  ")
			if err != nil {
				deps.Printer.PrintError(fmt.Sprintf("Failed to marshal output: %v", err))
				return err
			}

			if err := os.WriteFile(compileOutput, data, 0644); err != nil {
				deps.Printer.PrintError(fmt.Sprintf("Failed to write output file: %v", err))
				return err
			}

			deps.Printer.PrintSuccess(fmt.Sprintf("✅ Compiled graph written to %s", compileOutput))
		} else {
			data, _ := json.MarshalIndent(output, "", "  ")
			deps.Printer.Print("\n" + string(data))
		}

		return nil
	},
}

func init() {
	CompileCmd.Flags().StringVarP(&compileOutput, "output", "o", "", "Output file for compiled graph (JSON)")
}

func loadDeploymentSpec(path string) (*types.AppDeployment, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var deployment types.AppDeployment
	if err := json.Unmarshal(data, &deployment); err != nil {
		return nil, err
	}

	return &deployment, nil
}

func formatResourceTypes(ids []types.ResourceID) string {
	typeCount := make(map[types.ResourceType]int)
	for _, id := range ids {
		typeCount[id.Type]++
	}

	result := ""
	for t, count := range typeCount {
		if result != "" {
			result += ", "
		}
		result += fmt.Sprintf("%d %s", count, t)
	}
	return result
}
