package servers

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/spf13/cobra"
)

var ServiceLogsCmd = &cobra.Command{
	Use:   "service-logs <server-id>",
	Short: "View container logs from a service running on a server",
	Long: `Collect and display Docker container logs from services running on a managed server.

The command dispatches a log collection request to the agent via Mycelium Spine.
The agent retrieves the container logs, zips them, and uploads them back.
This command polls until the collection is complete and then prints the logs.

Examples:
  # Collect all service logs for a deployment
  ufctl servers service-logs <server-id> --deployment <deployment-id>

  # Collect logs for a single service
  ufctl servers service-logs <server-id> --deployment <deployment-id> --service web

  # Show last 200 lines per container
  ufctl servers service-logs <server-id> --deployment <deployment-id> -n 200

  # Save the zip archive instead of printing
  ufctl servers service-logs <server-id> --deployment <deployment-id> --output logs.zip`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		deps := utils.NewDependencyManager(ctx)
		serverID := args[0]

		deploymentID, _ := cmd.Flags().GetString("deployment")
		serviceName, _ := cmd.Flags().GetString("service")
		lines, _ := cmd.Flags().GetInt("lines")
		outputFile, _ := cmd.Flags().GetString("output")

		if deploymentID == "" {
			deps.Printer.PrintError("--deployment flag is required")
			return fmt.Errorf("--deployment flag is required")
		}

		return collectAndDisplayServiceLogs(ctx, deps, serverID, deploymentID, serviceName, lines, outputFile)
	},
}

func collectAndDisplayServiceLogs(
	ctx context.Context,
	deps *utils.DependencyManager,
	serverID, deploymentID, serviceName string,
	lines int,
	outputFile string,
) error {
	api := deps.CPlaneClient.API()

	// 1. Trigger log collection
	deps.Printer.Print(fmt.Sprintf("Requesting service logs from server %s...", serverID))

	collectPath := fmt.Sprintf("/servers/servers/%s/service-logs/collect", serverID)
	collectReq := map[string]interface{}{
		"deployment_id": deploymentID,
		"service_name":  serviceName,
		"lines":         lines,
	}
	var collectResp map[string]interface{}
	if err := api.POST(ctx, collectPath, collectReq, &collectResp); err != nil {
		deps.Printer.PrintError("Failed to request service log collection: " + err.Error())
		return err
	}

	jobID, _ := collectResp["job_id"].(string)
	if jobID == "" {
		return fmt.Errorf("no job_id in response from collect endpoint")
	}
	deps.Printer.Print(fmt.Sprintf("Log collection started (job: %s). Waiting for agent...", jobID))

	// 2. Poll job until completed or failed
	if err := pollJobUntilDone(ctx, deps, jobID); err != nil {
		return err
	}

	// 3. Download the zip
	deps.Printer.Print("Downloading log archive...")
	downloadPath := fmt.Sprintf("/servers/servers/%s/service-logs/download/%s", serverID, jobID)
	zipData, err := api.GETBytes(ctx, downloadPath)
	if err != nil {
		deps.Printer.PrintError("Failed to download log archive: " + err.Error())
		return err
	}

	// 4. If --output requested, write to file
	if outputFile != "" {
		if err := writeFile(outputFile, zipData); err != nil {
			deps.Printer.PrintError("Failed to write output file: " + err.Error())
			return err
		}
		deps.Printer.Print(fmt.Sprintf("Log archive saved to: %s", outputFile))
		return nil
	}

	// 5. Otherwise, extract and print each container's log
	return printZipLogs(deps, zipData)
}

// pollJobUntilDone polls the jobs API every 2 seconds until the job completes or fails.
func pollJobUntilDone(ctx context.Context, deps *utils.DependencyManager, jobID string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			var job map[string]interface{}
			if err := deps.CPlaneClient.API().GET(ctx, "/jobs/"+jobID, &job); err != nil {
				return fmt.Errorf("failed to poll job status: %w", err)
			}
			status, _ := job["status"].(string)
			switch status {
			case "completed":
				return nil
			case "failed":
				return fmt.Errorf("log collection job %s failed", jobID)
			default:
				deps.Printer.Print(fmt.Sprintf("  Job status: %s...", status))
			}
		}
	}
}

// printZipLogs extracts and prints each .log file from the zip archive.
func printZipLogs(deps *utils.DependencyManager, zipData []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("failed to read log archive: %w", err)
	}

	if len(zr.File) == 0 {
		deps.Printer.PrintWarning("Log archive is empty — no containers were found for the deployment.")
		return nil
	}

	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to open %s in archive: %v", f.Name, err))
			continue
		}

		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to read %s: %v", f.Name, err))
			continue
		}

		separator := strings.Repeat("=", 70)
		deps.Printer.Print(separator)
		deps.Printer.Print(fmt.Sprintf("  Container: %s", f.Name))
		deps.Printer.Print(separator)
		deps.Printer.Print(string(content))
		deps.Printer.Print("")
	}

	return nil
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}

func init() {
	ServiceLogsCmd.Flags().StringP("deployment", "d", "", "Deployment ID to collect logs for (required)")
	ServiceLogsCmd.Flags().StringP("service", "s", "", "Limit to a specific service name (default: all services)")
	ServiceLogsCmd.Flags().IntP("lines", "n", 500, "Number of log lines to collect per container")
	ServiceLogsCmd.Flags().StringP("output", "o", "", "Save the zip archive to this file instead of printing")
	ServersCmd.AddCommand(ServiceLogsCmd)
}
