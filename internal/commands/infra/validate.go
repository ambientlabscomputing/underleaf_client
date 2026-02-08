package infra

import (
	"fmt"
	"os"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// NewValidateCmd creates the validate command
func NewValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <manifest-file>",
		Short: "Validate an infrastructure manifest",
		Long:  `Validates the syntax and structure of an infrastructure manifest via the server API.`,
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

			// Parse manifest locally first
			var manifest map[string]interface{}
			if err := yaml.Unmarshal(manifestBytes, &manifest); err != nil {
				deps.Printer.PrintError("Failed to parse manifest YAML: " + err.Error())
				return fmt.Errorf("failed to parse manifest: %w", err)
			}

			// Call validate endpoint
			var result struct {
				Valid  bool     `json:"valid"`
				Errors []string `json:"errors"`
			}

			err = deps.CPlaneClient.API().POST("/infra/validate", manifest, &result)
			if err != nil {
				deps.Printer.PrintError("Failed to validate manifest: " + err.Error())
				return fmt.Errorf("failed to validate: %w", err)
			}

			if result.Valid {
				deps.Printer.PrintSuccess("✓ Manifest is valid")
				return nil
			} else {
				deps.Printer.PrintError("✗ Manifest validation errors:")
				for _, e := range result.Errors {
					deps.Printer.Print(fmt.Sprintf("  - %s", e))
				}
				return fmt.Errorf("validation failed")
			}
		},
	}

	return cmd
}
