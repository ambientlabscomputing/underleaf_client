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

var CreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new command template",
	Long: `Create a new command template with variable interpolation.

Templates can be created from a YAML/JSON file or using flags.

Examples:
  # Create from file
  ufctl templates create --file template.yaml

  # Create with flags
  ufctl templates create --name "deploy" --command "kubectl apply -f {{ .manifest }}" --var "manifest:string:true"

Variable format: key:type:required[:default]
  - type: string, int, or array
  - required: true or false
  - default: optional default value

Example variables:
  --var "environment:string:true"
  --var "replicas:int:false:3"
  --var "targets:array:true"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		filePath, _ := cmd.Flags().GetString("file")
		if filePath != "" {
			return createTemplateFromFile(deps, filePath)
		}

		// Create from flags
		name, _ := cmd.Flags().GetString("name")
		command, _ := cmd.Flags().GetString("command")
		description, _ := cmd.Flags().GetString("description")
		workDir, _ := cmd.Flags().GetString("workdir")
		timeout, _ := cmd.Flags().GetInt("timeout")
		varSpecs, _ := cmd.Flags().GetStringArray("var")
		serverIDs, _ := cmd.Flags().GetStringSlice("server-ids")
		tagSpecs, _ := cmd.Flags().GetStringSlice("tag")
		envSpecs, _ := cmd.Flags().GetStringSlice("env")

		if name == "" {
			deps.Printer.PrintError("--name is required")
			return fmt.Errorf("name is required")
		}
		if command == "" {
			deps.Printer.PrintError("--command is required")
			return fmt.Errorf("command is required")
		}

		// Parse variables
		variables := parseVariableSpecs(varSpecs)

		// Parse tags
		tags := parseKeyValueSpecs(tagSpecs)

		// Parse env vars
		envVars := parseKeyValueSpecs(envSpecs)

		req := CreateTemplateRequest{
			Name:        name,
			Description: description,
			Command:     command,
			WorkDir:     workDir,
			Timeout:     timeout,
			Variables:   variables,
			ServerIDs:   serverIDs,
			Tags:        tags,
			EnvVars:     envVars,
		}

		return createTemplate(deps, req)
	},
}

type CreateTemplateRequest struct {
	Name        string            `json:"name" yaml:"name"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Command     string            `json:"command" yaml:"command"`
	WorkDir     string            `json:"work_dir,omitempty" yaml:"work_dir,omitempty"`
	EnvVars     map[string]string `json:"env_vars,omitempty" yaml:"env_vars,omitempty"`
	Timeout     int               `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Variables   []InputVariable   `json:"variables,omitempty" yaml:"variables,omitempty"`
	ServerIDs   []string          `json:"server_ids,omitempty" yaml:"server_ids,omitempty"`
	Tags        map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`
}

func createTemplateFromFile(deps *utils.DependencyManager, filePath string) error {
	data, err := os.ReadFile(filePath)
	if err != nil {
		deps.Printer.PrintError("Failed to read file: " + err.Error())
		return err
	}

	var req CreateTemplateRequest

	// Try YAML first, then JSON
	if err := yaml.Unmarshal(data, &req); err != nil {
		if err := json.Unmarshal(data, &req); err != nil {
			deps.Printer.PrintError("Failed to parse file (tried YAML and JSON): " + err.Error())
			return err
		}
	}

	return createTemplate(deps, req)
}

func createTemplate(deps *utils.DependencyManager, req CreateTemplateRequest) error {
	var response Template
	err := deps.CPlaneClient.API().POST(context.Background(), "/templates", req, &response)
	if err != nil {
		deps.Printer.PrintError("Failed to create template: " + err.Error())
		return err
	}

	// Success output
	successStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	deps.Printer.Print("")
	deps.Printer.Print(successStyle.Render("✓ Template created successfully"))
	deps.Printer.Print("")
	deps.Printer.Print(fmt.Sprintf("  ID:   %s", response.ID))
	deps.Printer.Print(fmt.Sprintf("  Name: %s", response.Name))
	deps.Printer.Print("")
	deps.Printer.PrintInfo("Use 'ufctl templates trigger " + response.ID + "' to execute this template")

	return nil
}

// parseVariableSpecs parses variable specifications from flags
// Format: key:type:required[:default]
func parseVariableSpecs(specs []string) []InputVariable {
	var variables []InputVariable
	for _, spec := range specs {
		parts := strings.Split(spec, ":")
		if len(parts) < 3 {
			continue
		}
		v := InputVariable{
			Key:      parts[0],
			Type:     parts[1],
			Required: parts[2] == "true",
		}
		if len(parts) > 3 {
			v.Default = parts[3]
		}
		variables = append(variables, v)
	}
	return variables
}

// parseKeyValueSpecs parses key=value specifications
func parseKeyValueSpecs(specs []string) map[string]string {
	result := make(map[string]string)
	for _, spec := range specs {
		parts := strings.SplitN(spec, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

func init() {
	CreateCmd.Flags().StringP("file", "f", "", "Path to YAML/JSON template definition file")
	CreateCmd.Flags().StringP("name", "n", "", "Template name")
	CreateCmd.Flags().StringP("command", "c", "", "Command template with {{ .var }} placeholders")
	CreateCmd.Flags().StringP("description", "d", "", "Template description")
	CreateCmd.Flags().String("workdir", "", "Working directory for command execution")
	CreateCmd.Flags().Int("timeout", 0, "Command timeout in seconds")
	CreateCmd.Flags().StringArray("var", []string{}, "Variable definition (key:type:required[:default])")
	CreateCmd.Flags().StringSlice("server-ids", []string{}, "Target server IDs")
	CreateCmd.Flags().StringSlice("tag", []string{}, "Tag selector (key=value)")
	CreateCmd.Flags().StringSlice("env", []string{}, "Environment variable (KEY=VALUE)")
	TemplatesCmd.AddCommand(CreateCmd)
}
