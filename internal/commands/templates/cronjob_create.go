package templates

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var CronjobCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new cron job",
	Long: `Create a new cron job to schedule template execution.

Cron jobs can be created from a YAML/JSON file or using flags.

Schedule format uses standard cron syntax:
  minute hour day-of-month month day-of-week

Examples:
  # Create from file
  ufctl templates cronjobs create --file cronjob.yaml

  # Create with flags - run every day at 3am
  ufctl templates cronjobs create --name "nightly-deploy" --template abc123 --schedule "0 3 * * *"

  # Create with preset inputs
  ufctl templates cronjobs create --name "prod-backup" --template xyz789 --schedule "0 0 * * *" --input env=production`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		filePath, _ := cmd.Flags().GetString("file")
		if filePath != "" {
			return createCronJobFromFile(deps, filePath)
		}

		// Create from flags
		name, _ := cmd.Flags().GetString("name")
		templateID, _ := cmd.Flags().GetString("template")
		scheduleStr, _ := cmd.Flags().GetString("schedule")
		inputSpecs, _ := cmd.Flags().GetStringArray("input")
		enabled, _ := cmd.Flags().GetBool("enabled")

		if name == "" {
			deps.Printer.PrintError("--name is required")
			return fmt.Errorf("name is required")
		}
		if templateID == "" {
			deps.Printer.PrintError("--template is required")
			return fmt.Errorf("template is required")
		}
		if scheduleStr == "" {
			deps.Printer.PrintError("--schedule is required")
			return fmt.Errorf("schedule is required")
		}

		// Parse schedule
		schedule, err := parseScheduleString(scheduleStr)
		if err != nil {
			deps.Printer.PrintError("Invalid schedule: " + err.Error())
			return err
		}

		// Parse inputs
		inputs := parseKeyValueSpecs(inputSpecs)

		req := CreateCronJobRequest{
			Name:       name,
			TemplateID: templateID,
			SetInputs:  inputs,
			Schedule:   schedule,
			Enabled:    enabled,
		}

		return createCronJob(deps, req)
	},
}

type CreateCronJobRequest struct {
	Name       string            `json:"name" yaml:"name"`
	TemplateID string            `json:"template_id" yaml:"template_id"`
	SetInputs  map[string]string `json:"set_inputs,omitempty" yaml:"set_inputs,omitempty"`
	Schedule   CronSchedule      `json:"schedule" yaml:"schedule"`
	Enabled    bool              `json:"enabled" yaml:"enabled"`
}

func createCronJobFromFile(deps *utils.DependencyManager, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		deps.Printer.PrintError("Failed to read file: " + err.Error())
		return err
	}

	var req CreateCronJobRequest

	// Try YAML first, then JSON
	if err := yaml.Unmarshal(data, &req); err != nil {
		if err := json.Unmarshal(data, &req); err != nil {
			deps.Printer.PrintError("Failed to parse file (tried YAML and JSON): " + err.Error())
			return err
		}
	}

	return createCronJob(deps, req)
}

func createCronJob(deps *utils.DependencyManager, req CreateCronJobRequest) error {
	var response CronJob
	err := deps.CPlaneClient.API().POST(context.Background(), "/cron-jobs", req, &response)
	if err != nil {
		deps.Printer.PrintError("Failed to create cron job: " + err.Error())
		return err
	}

	// Success output
	successStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	deps.Printer.Print("")
	deps.Printer.Print(successStyle.Render("✓ Cron job created successfully"))
	deps.Printer.Print("")
	deps.Printer.Print(fmt.Sprintf("  ID:       %s", response.ID))
	deps.Printer.Print(fmt.Sprintf("  Name:     %s", response.Name))
	deps.Printer.Print(fmt.Sprintf("  Template: %s", response.TemplateID))
	deps.Printer.Print(fmt.Sprintf("  Schedule: %s", formatSchedule(response.Schedule)))
	deps.Printer.Print(fmt.Sprintf("  Enabled:  %v", response.Enabled))
	if response.NextRun != "" {
		deps.Printer.Print(fmt.Sprintf("  Next Run: %s", response.NextRun))
	}
	deps.Printer.Print("")

	return nil
}

// parseScheduleString parses a cron schedule string into CronSchedule
// Format: minute hour day-of-month month day-of-week
func parseScheduleString(s string) (CronSchedule, error) {
	parts := strings.Fields(s)
	if len(parts) != 5 {
		return CronSchedule{}, fmt.Errorf("expected 5 fields (minute hour day-of-month month day-of-week), got %d", len(parts))
	}

	return CronSchedule{
		Minute:     parts[0],
		Hour:       parts[1],
		DayOfMonth: parts[2],
		Month:      parts[3],
		DayOfWeek:  parts[4],
	}, nil
}

func init() {
	CronjobCreateCmd.Flags().StringP("file", "f", "", "Path to YAML/JSON cron job definition file")
	CronjobCreateCmd.Flags().StringP("name", "n", "", "Cron job name")
	CronjobCreateCmd.Flags().StringP("template", "t", "", "Template ID to execute")
	CronjobCreateCmd.Flags().StringP("schedule", "s", "", "Cron schedule (minute hour day-of-month month day-of-week)")
	CronjobCreateCmd.Flags().StringArrayP("input", "i", []string{}, "Preset input value (key=value)")
	CronjobCreateCmd.Flags().Bool("enabled", true, "Enable the cron job immediately")
	CronjobsCmd.AddCommand(CronjobCreateCmd)
}
