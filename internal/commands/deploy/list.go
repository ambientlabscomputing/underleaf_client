package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/types"
	"github.com/ambientlabscomputing/underleaf_client/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

var ListCmd = &cobra.Command{
	Use:   "list [directory]",
	Short: "List available deployment specifications",
	Long: `List all deployment JSON files in the specified directory.

Examples:
  ufctl deploy list
  ufctl deploy list ./deployments`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)

		directory := "."
		if len(args) > 0 {
			directory = args[0]
		}

		files, err := filepath.Glob(filepath.Join(directory, "*.json"))
		if err != nil {
			deps.Printer.PrintError("Failed to list deployment files: " + err.Error())
			return err
		}

		if len(files) == 0 {
			deps.Printer.PrintInfo(fmt.Sprintf("No deployment files found in %s", directory))
			return nil
		}

		var deployments []deploymentInfo
		for _, file := range files {
			data, err := os.ReadFile(file)
			if err != nil {
				continue
			}

			var deployment types.AppDeployment
			if err := json.Unmarshal(data, &deployment); err != nil {
				continue
			}

			deployments = append(deployments, deploymentInfo{
				File:     filepath.Base(file),
				Path:     file,
				ID:       deployment.ID,
				Slug:     deployment.Slug,
				Name:     deployment.Name,
				Version:  deployment.Version,
				Services: len(deployment.Services),
				Networks: len(deployment.Networks),
				Volumes:  len(deployment.Volumes),
			})
		}

		if len(deployments) == 0 {
			deps.Printer.PrintInfo("No valid deployment files found")
			return nil
		}

		table := ui.NewTableBuilder().
			WithTitle("Deployments").
			WithHeaders("FILE", "ID", "SLUG", "NAME", "VERSION", "SERVICES", "NETWORKS", "VOLUMES")

		for _, d := range deployments {
			table.AddRow(
				d.File,
				d.ID,
				d.Slug,
				d.Name,
				fmt.Sprintf("%d", d.Version),
				fmt.Sprintf("%d", d.Services),
				fmt.Sprintf("%d", d.Networks),
				fmt.Sprintf("%d", d.Volumes),
			)
		}

		deps.Printer.Print("\n" + lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("86")).
			Render(fmt.Sprintf("Found %d deployment(s)", len(deployments))))
		deps.Printer.PrintTable(table)

		return nil
	},
}

type deploymentInfo struct {
	File     string
	Path     string
	ID       string
	Slug     string
	Name     string
	Version  int
	Services int
	Networks int
	Volumes  int
}
