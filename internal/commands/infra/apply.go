package infra

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// NewApplyCmd creates the apply command
func NewApplyCmd() *cobra.Command {
	var autoApprove bool

	cmd := &cobra.Command{
		Use:   "apply <manifest-file>",
		Short: "Apply infrastructure changes",
		Long:  `Applies the infrastructure manifest, creating/updating/deleting resources as needed.`,
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

			// Validate YAML syntax
			var manifest map[string]interface{}
			if err := yaml.Unmarshal(manifestBytes, &manifest); err != nil {
				deps.Printer.PrintError("Failed to parse manifest YAML: " + err.Error())
				return fmt.Errorf("failed to parse manifest: %w", err)
			}

			// First, get the plan
			var plan struct {
				ManifestName  string `json:"manifest_name"`
				CreateCount   int    `json:"create_count"`
				UpdateCount   int    `json:"update_count"`
				DeleteCount   int    `json:"delete_count"`
				NoChangeCount int    `json:"no_change_count"`
			}

			err = deps.CPlaneClient.API().POSTRaw(context.Background(), "/infra/plan", manifestBytes, &plan)
			if err != nil {
				deps.Printer.PrintError("Failed to compute plan: " + err.Error())
				return fmt.Errorf("failed to compute plan: %w", err)
			}

			// Display summary
			deps.Printer.Print(fmt.Sprintf("Infrastructure Plan for: %s\n", plan.ManifestName))
			deps.Printer.Print(fmt.Sprintf("Summary: %d create, %d update, %d delete, %d no-change\n",
				plan.CreateCount, plan.UpdateCount, plan.DeleteCount, plan.NoChangeCount))

			if plan.CreateCount == 0 && plan.UpdateCount == 0 && plan.DeleteCount == 0 {
				deps.Printer.PrintInfo("No changes required.")
				return nil
			}

			// Confirm unless auto-approve
			if !autoApprove {
				deps.Printer.Print("Apply these changes? (yes/no): ")
				reader := bufio.NewReader(os.Stdin)
				response, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("failed to read response: %w", err)
				}
				response = strings.ToLower(strings.TrimSpace(response))
				if response != "yes" && response != "y" {
					deps.Printer.PrintInfo("Apply cancelled.")
					return nil
				}
			}

			// Apply changes
			deps.Printer.Print("\nApplying changes...")

			var result struct {
				ManifestName string `json:"manifest_name"`
				Success      bool   `json:"success"`
				Operations   []struct {
					ResourceType string `json:"resource_type"`
					ResourceName string `json:"resource_name"`
					Action       string `json:"action"`
					Success      bool   `json:"success"`
					Error        string `json:"error"`
				} `json:"operations"`
				SuccessCount int `json:"success_count"`
				FailureCount int `json:"failure_count"`
			}

			err = deps.CPlaneClient.API().POSTRaw(context.Background(), "/infra/apply", manifestBytes, &result)
			if err != nil {
				deps.Printer.PrintError("Failed to apply changes: " + err.Error())
				return fmt.Errorf("failed to apply changes: %w", err)
			}
			for _, op := range result.Operations {
				var symbol string
				if op.Success {
					symbol = "✓"
				} else {
					symbol = "✗"
				}

				msg := fmt.Sprintf("%s %s %s: %s", symbol, op.Action, op.ResourceType, op.ResourceName)
				if !op.Success {
					msg += fmt.Sprintf(" - %s", op.Error)
				}
				deps.Printer.Print(msg)
			}

			if !result.Success {
				return fmt.Errorf("some operations failed")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip confirmation prompt")

	return cmd
}
