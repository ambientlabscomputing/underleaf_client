package local

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	editConfig       bool
	editorWithEditor string
)

// ConfigCmd represents the config command
var ConfigCmd = &cobra.Command{
	Use:   "config",
	Short: "View or edit the configuration file",
	Long:  "View the current configuration in a paged format, or edit it with a text editor.",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		// Get config file path
		configInfo := deps.ConfigClient.ConfigClientInfo()
		configPath, ok := configInfo["path"].(string)
		if !ok || configPath == "" || configPath == "in-memory (no config file)" {
			deps.Printer.PrintError("No config file found")
			return fmt.Errorf("no config file found")
		}

		// If edit flag is set, open in editor
		if editConfig {
			editor := editorWithEditor
			if editor == "" {
				editor = "vim" // default to vim
			}

			// Validate editor choice
			validEditors := map[string]bool{"vim": true, "vi": true, "nano": true}
			if !validEditors[editor] {
				deps.Printer.PrintError(fmt.Sprintf("Invalid editor: %s. Must be one of: vim, vi, nano", editor))
				return fmt.Errorf("invalid editor: %s", editor)
			}

			deps.Printer.PrintInfo(fmt.Sprintf("Opening %s with %s...", configPath, editor))

			editorCmd := exec.Command(editor, configPath)
			editorCmd.Stdin = os.Stdin
			editorCmd.Stdout = os.Stdout
			editorCmd.Stderr = os.Stderr

			if err := editorCmd.Run(); err != nil {
				deps.Printer.PrintError(fmt.Sprintf("Failed to open editor: %v", err))
				return err
			}

			deps.Printer.PrintSuccess("Config file edited successfully")
			return nil
		}

		// Otherwise, display config in paged format with syntax highlighting
		configData, err := os.ReadFile(configPath)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to read config file: %v", err))
			return err
		}

		// Parse and pretty print the YAML with color
		var data interface{}
		if err := yaml.Unmarshal(configData, &data); err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to parse config: %v", err))
			return err
		}

		// Format YAML with proper indentation
		prettyYAML, err := yaml.Marshal(data)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to format config: %v", err))
			return err
		}

		// Try different pagers in order of preference
		if _, err := exec.LookPath("bat"); err == nil {
			// bat is available - use it for syntax highlighting
			pagerCmd := exec.Command("bat", "--language", "yaml", "--style", "plain", "--paging", "always", configPath)
			pagerCmd.Stdout = os.Stdout
			pagerCmd.Stderr = os.Stderr
			pagerCmd.Stdin = os.Stdin
			return pagerCmd.Run()
		} else if _, err := exec.LookPath("less"); err == nil {
			// Use less with stdin pipe
			pagerCmd := exec.Command("less", "-R")
			pagerCmd.Stdout = os.Stdout
			pagerCmd.Stderr = os.Stderr

			stdin, err := pagerCmd.StdinPipe()
			if err != nil {
				// Fallback to direct print
				deps.Printer.PrintInfo("Configuration at: " + configPath)
				fmt.Println()
				fmt.Println(string(prettyYAML))
				return nil
			}

			if err := pagerCmd.Start(); err != nil {
				// Fallback to direct print
				deps.Printer.PrintInfo("Configuration at: " + configPath)
				fmt.Println()
				fmt.Println(string(prettyYAML))
				return nil
			}

			stdin.Write(prettyYAML)
			stdin.Close()

			return pagerCmd.Wait()
		} else {
			// No pager available, print directly
			deps.Printer.PrintInfo("Configuration at: " + configPath)
			fmt.Println()
			fmt.Println(string(prettyYAML))
			return nil
		}
	},
}

func init() {
	ConfigCmd.Flags().BoolVarP(&editConfig, "edit", "e", false, "Edit the config file")
	ConfigCmd.Flags().StringVar(&editorWithEditor, "with-editor", "vim", "Text editor to use (vim, vi, nano)")
}
