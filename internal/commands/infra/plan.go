package infra

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// NewPlanCmd creates the plan command
func NewPlanCmd() *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:   "plan <manifest-file>",
		Short: "Show execution plan for infrastructure changes",
		Long:  `Computes and displays what changes would be made without applying them.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			manifestPath := args[0]

			// Get dependencies
			deps := utils.NewDependencyManager(ctx)
			if deps == nil {
				return fmt.Errorf("failed to create dependencies")
			}

			// Read manifest file
			manifestBytes, err := os.ReadFile(manifestPath)
			if err != nil {
				deps.Printer.PrintError("Failed to read manifest: " + err.Error())
				return fmt.Errorf("failed to read manifest: %w", err)
			}

			// Parse manifest
			var manifest map[string]interface{}
			if err := yaml.Unmarshal(manifestBytes, &manifest); err != nil {
				deps.Printer.PrintError("Failed to parse manifest YAML: " + err.Error())
				return fmt.Errorf("failed to parse manifest: %w", err)
			}

			// Call plan endpoint
			var plan struct {
				ManifestName string `json:"manifest_name"`
				Operations   []struct {
					ResourceType string `json:"resource_type"`
					ResourceName string `json:"resource_name"`
					ResourceID   string `json:"resource_id"`
					Action       string `json:"action"`
					Details      []struct {
						Field    string      `json:"field"`
						OldValue interface{} `json:"old_value"`
						NewValue interface{} `json:"new_value"`
					} `json:"details"`
				} `json:"operations"`
				CreateCount   int `json:"create_count"`
				UpdateCount   int `json:"update_count"`
				DeleteCount   int `json:"delete_count"`
				NoChangeCount int `json:"no_change_count"`
			}

			err = deps.CPlaneClient.API().POST("/infra/plan", manifest, &plan)
			if err != nil {
				deps.Printer.PrintError("Failed to compute plan: " + err.Error())
				return fmt.Errorf("failed to compute plan: %w", err)
			}

			// Display plan
			if outputFormat == "json" {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(plan)
			}

			// Human-readable output
			deps.Printer.Print(fmt.Sprintf("Infrastructure Plan for: %s\n", plan.ManifestName))
			deps.Printer.Print(fmt.Sprintf("Summary: %d create, %d update, %d delete, %d no-change\n",
				plan.CreateCount, plan.UpdateCount, plan.DeleteCount, plan.NoChangeCount))

			if len(plan.Operations) == 0 {
				deps.Printer.PrintInfo("No changes required.")
				return nil
			}

			for _, op := range plan.Operations {
				var symbol string
				switch op.Action {
				case "create":
					symbol = "+"
				case "update":
					symbol = "~"
				case "delete":
					symbol = "-"
				case "no-change":
					continue // Skip no-change in output
				}

				deps.Printer.Print(fmt.Sprintf("%s %s: %s", symbol, op.ResourceType, op.ResourceName))
				for _, detail := range op.Details {
					deps.Printer.Print(fmt.Sprintf("    %s: %v -> %v", detail.Field, detail.OldValue, detail.NewValue))
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "text", "Output format: text or json")

	return cmd
}
