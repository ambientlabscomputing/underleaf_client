package infra

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const exampleManifest = `version: "1"
name: production-edge

deployments:
  - name: Example Web Service
    slug: example-web
    targeting:
      mode: tags
      tags:
        environment: production
    services:
      - name: nginx
        image: nginx:1.25
        ports:
          - "80:80"
          - "443:443"
        environment:
          UPSTREAM: "localhost:3000"
        networks:
          - frontend
        volumes:
          - config:/etc/nginx
    networks:
      - name: frontend
        driver: bridge
    volumes:
      - name: config
        path: /data/nginx

clusters:
  - name: edge-site-alpha
    servers:
      - server-1
      - server-2
      - server-3

templates:
  - name: health-check
    command_template: "curl -sf http://localhost:{{ .port }}/health"
    inputs:
      - key: port
        type: string
        required: false
        default: "8080"

cron_jobs:
  - name: scheduled-health-check
    template_name: health-check
    schedule:
      minute: "*/5"
      hour: "*"
      day_of_month: "*"
      month: "*"
      day_of_week: "*"
    all_servers: true
    enabled: true
`

// NewInitCmd creates the init command
func NewInitCmd() *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new infrastructure manifest",
		Long:  `Creates an example infra.yaml file in the current directory.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Determine output path
			if outputPath == "" {
				outputPath = "infra.yaml"
			}

			// Check if file already exists
			if _, err := os.Stat(outputPath); err == nil {
				return fmt.Errorf("file %s already exists", outputPath)
			}

			// Create parent directory if needed
			dir := filepath.Dir(outputPath)
			if dir != "." {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return fmt.Errorf("failed to create directory: %w", err)
				}
			}

			// Write manifest
			if err := os.WriteFile(outputPath, []byte(exampleManifest), 0644); err != nil {
				return fmt.Errorf("failed to write manifest: %w", err)
			}

			fmt.Printf("✓ Created example manifest at %s\n", outputPath)
			fmt.Println("\nNext steps:")
			fmt.Println("  1. Edit the manifest to match your infrastructure")
			fmt.Println("  2. Validate: ufctl infra validate infra.yaml")
			fmt.Println("  3. Preview: ufctl infra plan infra.yaml")
			fmt.Println("  4. Apply: ufctl infra apply infra.yaml")
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output path for manifest (default: infra.yaml)")

	return cmd
}
