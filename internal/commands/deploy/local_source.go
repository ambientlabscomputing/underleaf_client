package deploy

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ambientlabscomputing/underleaf_client/internal/commands/utils"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// runLocalDeploy handles the full local: deploy flow:
//  1. Parse the source path from "local:./path"
//  2. Read and parse .underleaf/deploy.yaml
//  3. Upload build context if any service has a build: section
//  4. Call DeployFromSource on the control plane
func runLocalDeploy(cmd *cobra.Command, deps *utils.DependencyManager, source string, targeting *controlplane.SourceTargeting) error {
	ctx := cmd.Context()

	// Resolve project directory from "local:./path"
	rawPath := strings.TrimPrefix(source, "local:")
	if rawPath == "" {
		rawPath = "."
	}
	projectDir, err := filepath.Abs(rawPath)
	if err != nil {
		deps.Printer.PrintError(fmt.Sprintf("Failed to resolve path %q: %v", rawPath, err))
		return err
	}

	if info, err := os.Stat(projectDir); err != nil || !info.IsDir() {
		deps.Printer.PrintError(fmt.Sprintf("Project directory not found: %s", projectDir))
		return fmt.Errorf("directory not found: %s", projectDir)
	}

	// Read .underleaf/deploy.yaml
	manifestPath := filepath.Join(projectDir, ".underleaf", "deploy.yaml")
	manifestData, err := os.ReadFile(manifestPath)
	if err != nil {
		deps.Printer.PrintError(fmt.Sprintf("Failed to read %s: %v", manifestPath, err))
		return err
	}

	var manifest controlplane.InlineManifest
	if err := yaml.Unmarshal(manifestData, &manifest); err != nil {
		deps.Printer.PrintError(fmt.Sprintf("Failed to parse deploy.yaml: %v", err))
		return err
	}

	if manifest.Name == "" {
		deps.Printer.PrintError("deploy.yaml must specify a 'name' field")
		return fmt.Errorf("deploy.yaml missing 'name'")
	}

	deps.Printer.Print(lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("86")).
		Render(fmt.Sprintf("\n🚀 Deploying %s from %s\n", manifest.Name, projectDir)))

	// Resolve deploy ref: if user didn't supply --ref, auto-detect from git
	ref := deployRef
	if ref == "" {
		ref = resolveLocalRef(projectDir)
	}

	// Upload build context if any service requires a build
	var archiveRef string
	if hasBuildServices(&manifest) {
		deps.Printer.PrintInfo("Packaging build context...")

		tarPath, err := createBuildArchive(projectDir)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to create build archive: %v", err))
			return err
		}
		defer os.Remove(tarPath)

		archiveRef, err = deps.CPlaneClient.Deployments.UploadBuildContext(ctx, tarPath)
		if err != nil {
			deps.Printer.PrintError(fmt.Sprintf("Failed to upload build context: %v", err))
			return err
		}
		deps.Printer.PrintSuccess("Build context uploaded.")
	}

	deps.Printer.PrintInfo(fmt.Sprintf("Deploy ref: %s", ref))

	req := controlplane.DeployFromSourceRequest{
		Source:     source,
		Ref:        ref,
		Targeting:  targeting,
		Manifest:   &manifest,
		ArchiveRef: archiveRef,
	}

	resp, err := deps.CPlaneClient.Deployments.DeployFromSource(ctx, req)
	if err != nil {
		deps.Printer.PrintError(fmt.Sprintf("Deploy failed: %v", err))
		return err
	}

	printDeployResult(deps, resp)
	return nil
}

// resolveLocalRef determines the deploy ref for a local source.
// If the directory is a git repo with a clean worktree, returns "<branch>-<short-sha>".
// Otherwise returns "local-<random>".
func resolveLocalRef(projectDir string) string {
	// Check if this is a git repository
	gitCheck := exec.Command("git", "-C", projectDir, "rev-parse", "--is-inside-work-tree")
	if err := gitCheck.Run(); err != nil {
		return localRandomRef()
	}

	// Check if the worktree is dirty
	statusCmd := exec.Command("git", "-C", projectDir, "status", "--porcelain")
	statusOut, err := statusCmd.Output()
	if err != nil || len(strings.TrimSpace(string(statusOut))) > 0 {
		return localRandomRef()
	}

	// Get branch name
	branchCmd := exec.Command("git", "-C", projectDir, "rev-parse", "--abbrev-ref", "HEAD")
	branchOut, err := branchCmd.Output()
	if err != nil {
		return localRandomRef()
	}
	branch := strings.TrimSpace(string(branchOut))

	// Get short commit SHA
	shaCmd := exec.Command("git", "-C", projectDir, "rev-parse", "--short", "HEAD")
	shaOut, err := shaCmd.Output()
	if err != nil {
		return localRandomRef()
	}
	sha := strings.TrimSpace(string(shaOut))

	return branch + "-" + sha
}

func localRandomRef() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "local-" + hex.EncodeToString(b)
}

func hasBuildServices(m *controlplane.InlineManifest) bool {
	for _, svc := range m.Services {
		if svc.Build != nil {
			return true
		}
	}
	return false
}
