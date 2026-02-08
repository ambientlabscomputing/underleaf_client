package recipe

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/capability"
	"github.com/ambientlabscomputing/underleaf_client/internal/types"
)

// ProgressReporter is called to report stage progress during reconciliation
type ProgressReporter func(stage, message string, data map[string]interface{})

// Reconciler orchestrates the two execution paths of a recipe:
//   - Capability requirements → resolved via capability.Manager
//   - Container specs (services/networks/volumes) → compiled and executed via the Docker pipeline
type Reconciler struct {
	capManager    *capability.Manager
	manifestPath  string // Path to recipe_providers.json
}

// NewReconciler creates a new recipe reconciler
func NewReconciler(capManager *capability.Manager, dataDir string) *Reconciler {
	return &Reconciler{
		capManager:   capManager,
		manifestPath: filepath.Join(dataDir, "recipe_providers.json"),
	}
}

// ReconcileCapabilities handles the capability requirement phase of a deployment.
// For each CapabilityRequirement, it calls EnsureCapability on the capability manager.
// It also handles removal of capabilities that were previously installed but are no longer needed.
func (r *Reconciler) ReconcileCapabilities(
	ctx context.Context,
	deployment *types.AppDeployment,
	lastApplied *types.LastAppliedSnapshot,
	report ProgressReporter,
) ([]CapabilityResult, error) {
	if len(deployment.CapabilityRequirements) == 0 && (lastApplied == nil || len(lastApplied.InstalledCapabilities) == 0) {
		return nil, nil // Nothing to do
	}

	results := make([]CapabilityResult, 0, len(deployment.CapabilityRequirements))

	// Phase 1: Resolve capabilities — report what providers will be used
	if len(deployment.CapabilityRequirements) > 0 {
		report("resolving_capabilities", fmt.Sprintf("Resolving %d capability requirements", len(deployment.CapabilityRequirements)), map[string]interface{}{
			"count": len(deployment.CapabilityRequirements),
		})
	}

	// Phase 2: Install and start providers for each capability requirement
	for _, req := range deployment.CapabilityRequirements {
		start := time.Now()

		capReq := capability.CapabilityRequest{
			CapabilityID: req.CapabilityID,
			VersionRange: req.VersionRange,
		}
		if req.Constraints != nil {
			capReq.Constraints = capability.ResolveConstraints{
				TrustTier:    req.Constraints.TrustTier,
				Platform:     req.Constraints.Platform,
				Architecture: req.Constraints.Architecture,
			}
		}

		report("installing_providers", fmt.Sprintf("Ensuring capability: %s", req.CapabilityID), map[string]interface{}{
			"capability_id": req.CapabilityID,
			"version_range": req.VersionRange,
		})

		endpoint, err := r.capManager.EnsureCapability(ctx, capReq)
		duration := time.Since(start)

		if err != nil {
			slog.Error("failed to ensure capability",
				"capability_id", req.CapabilityID,
				"error", err,
			)
			results = append(results, CapabilityResult{
				CapabilityID: req.CapabilityID,
				Alias:        req.Alias,
				Success:      false,
				Error:        err.Error(),
				Duration:     duration,
			})
			continue
		}

		slog.Info("capability ensured",
			"capability_id", req.CapabilityID,
			"provider_id", endpoint.Provider.ProviderID,
			"version", endpoint.Provider.Version,
			"endpoint", endpoint.Endpoint,
			"state", endpoint.State,
		)

		results = append(results, CapabilityResult{
			CapabilityID: req.CapabilityID,
			Alias:        req.Alias,
			ProviderID:   endpoint.Provider.ProviderID,
			Version:      endpoint.Provider.Version,
			Endpoint:     endpoint.Endpoint,
			State:        endpoint.State,
			Success:      true,
			Duration:     duration,
		})
	}

	// Phase 3: Handle removed capabilities — uninstall providers no longer needed
	if lastApplied != nil && len(lastApplied.InstalledCapabilities) > 0 {
		desiredSet := make(map[string]bool, len(deployment.CapabilityRequirements))
		for _, req := range deployment.CapabilityRequirements {
			desiredSet[req.CapabilityID] = true
		}

		for _, prev := range lastApplied.InstalledCapabilities {
			if desiredSet[prev.CapabilityID] {
				continue // Still desired
			}

			slog.Info("capability removed from recipe, checking if provider can be uninstalled",
				"capability_id", prev.CapabilityID,
				"provider_id", prev.ProviderID,
			)

			// Only uninstall if no other deployment is using this provider
			if r.isProviderUsedByOtherDeployment(deployment.ID, prev.ProviderID, prev.Version) {
				slog.Info("provider still used by another deployment, skipping uninstall",
					"provider_id", prev.ProviderID,
				)
				continue
			}

			report("removing_providers", fmt.Sprintf("Removing provider for removed capability: %s", prev.CapabilityID), map[string]interface{}{
				"capability_id": prev.CapabilityID,
				"provider_id":   prev.ProviderID,
			})

			if _, err := r.capManager.UninstallProviderByID(ctx, prev.ProviderID, prev.Version); err != nil {
				slog.Warn("failed to uninstall removed capability's provider",
					"capability_id", prev.CapabilityID,
					"provider_id", prev.ProviderID,
					"error", err,
				)
			}
		}
	}

	// Report completion
	successCount := 0
	for _, res := range results {
		if res.Success {
			successCount++
		}
	}

	if len(results) > 0 {
		report("starting_providers", fmt.Sprintf("Capability phase complete: %d/%d successful", successCount, len(results)), map[string]interface{}{
			"total":     len(results),
			"succeeded": successCount,
			"failed":    len(results) - successCount,
		})
	}

	return results, nil
}

// PlanCapabilities creates a plan showing what would happen for each capability requirement
// without actually installing or starting anything.
func (r *Reconciler) PlanCapabilities(
	ctx context.Context,
	deployment *types.AppDeployment,
	lastApplied *types.LastAppliedSnapshot,
) ([]CapabilityPlan, []string, error) {
	plans := make([]CapabilityPlan, 0, len(deployment.CapabilityRequirements))
	var removedCapabilities []string

	// Plan each capability requirement
	for _, req := range deployment.CapabilityRequirements {
		plan := CapabilityPlan{
			CapabilityID: req.CapabilityID,
			Alias:        req.Alias,
			VersionRange: req.VersionRange,
		}

		// Try to resolve without installing
		endpoint, err := r.capManager.ResolveCapability(ctx, req.CapabilityID)
		if err == nil && endpoint != nil && endpoint.State == capability.ProviderStateRunning {
			plan.ProviderID = endpoint.Provider.ProviderID
			plan.ProviderVersion = endpoint.Provider.Version
			plan.Action = "already_running"
		} else {
			// Would need to install — try resolving from registry
			capReq := capability.CapabilityRequest{
				CapabilityID: req.CapabilityID,
				VersionRange: req.VersionRange,
			}
			if req.Constraints != nil {
				capReq.Constraints = capability.ResolveConstraints{
					TrustTier:    req.Constraints.TrustTier,
					Platform:     req.Constraints.Platform,
					Architecture: req.Constraints.Architecture,
				}
			}

			// Use the resolver to find what provider would be used (without installing)
			// For now, indicate install is needed
			plan.Action = "install"
			if err != nil {
				plan.Error = fmt.Sprintf("resolution pending: %v", err)
			}
		}

		plans = append(plans, plan)
	}

	// Detect removed capabilities
	if lastApplied != nil {
		desiredSet := make(map[string]bool, len(deployment.CapabilityRequirements))
		for _, req := range deployment.CapabilityRequirements {
			desiredSet[req.CapabilityID] = true
		}
		for _, prev := range lastApplied.InstalledCapabilities {
			if !desiredSet[prev.CapabilityID] {
				removedCapabilities = append(removedCapabilities, prev.CapabilityID)
			}
		}
	}

	return plans, removedCapabilities, nil
}

// UpdateManifest saves the provider ownership manifest after a successful reconciliation
func (r *Reconciler) UpdateManifest(deploymentID string, installed []CapabilityResult) error {
	manifest := r.loadManifest()

	states := make([]types.InstalledCapabilityState, 0, len(installed))
	for _, res := range installed {
		if res.Success {
			states = append(states, types.InstalledCapabilityState{
				CapabilityID: res.CapabilityID,
				ProviderID:   res.ProviderID,
				Version:      res.Version,
				Endpoint:     res.Endpoint,
			})
		}
	}

	manifest.Deployments[deploymentID] = states
	manifest.UpdatedAt = time.Now()

	return r.saveManifest(manifest)
}

// RemoveFromManifest removes a deployment from the provider manifest
func (r *Reconciler) RemoveFromManifest(deploymentID string) error {
	manifest := r.loadManifest()
	delete(manifest.Deployments, deploymentID)
	manifest.UpdatedAt = time.Now()
	return r.saveManifest(manifest)
}

// isProviderUsedByOtherDeployment checks if any other deployment is using the given provider
func (r *Reconciler) isProviderUsedByOtherDeployment(excludeDeploymentID, providerID, version string) bool {
	manifest := r.loadManifest()

	for depID, caps := range manifest.Deployments {
		if depID == excludeDeploymentID {
			continue
		}
		for _, cap := range caps {
			if cap.ProviderID == providerID && cap.Version == version {
				return true
			}
		}
	}

	return false
}

// InstalledCapabilitiesFromResults converts CapabilityResults to InstalledCapabilityStates
// for storage in the LastAppliedSnapshot.
func InstalledCapabilitiesFromResults(results []CapabilityResult) []types.InstalledCapabilityState {
	states := make([]types.InstalledCapabilityState, 0, len(results))
	for _, res := range results {
		if res.Success {
			states = append(states, types.InstalledCapabilityState{
				CapabilityID: res.CapabilityID,
				ProviderID:   res.ProviderID,
				Version:      res.Version,
				Endpoint:     res.Endpoint,
			})
		}
	}
	return states
}

// InstalledProvidersForReporting returns a list of installed provider info from the
// capability results, for inclusion in the deployment result sent back to the server API.
func InstalledProvidersForReporting(results []CapabilityResult) []map[string]interface{} {
	providers := make([]map[string]interface{}, 0, len(results))
	for _, res := range results {
		p := map[string]interface{}{
			"capability_id": res.CapabilityID,
			"success":       res.Success,
		}
		if res.ProviderID != "" {
			p["provider_id"] = res.ProviderID
			p["version"] = res.Version
			p["endpoint"] = res.Endpoint
			p["state"] = string(res.State)
		}
		if res.Error != "" {
			p["error"] = res.Error
		}
		if res.Alias != "" {
			p["alias"] = res.Alias
		}
		providers = append(providers, p)
	}
	return providers
}

// loadManifest loads the recipe provider manifest from disk
func (r *Reconciler) loadManifest() *RecipeProviderManifest {
	data, err := os.ReadFile(r.manifestPath)
	if err != nil {
		return &RecipeProviderManifest{
			Deployments: make(map[string][]types.InstalledCapabilityState),
		}
	}

	var manifest RecipeProviderManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		slog.Warn("failed to parse recipe provider manifest, starting fresh", "error", err)
		return &RecipeProviderManifest{
			Deployments: make(map[string][]types.InstalledCapabilityState),
		}
	}

	if manifest.Deployments == nil {
		manifest.Deployments = make(map[string][]types.InstalledCapabilityState)
	}

	return &manifest
}

// saveManifest writes the recipe provider manifest to disk
func (r *Reconciler) saveManifest(manifest *RecipeProviderManifest) error {
	if err := os.MkdirAll(filepath.Dir(r.manifestPath), 0755); err != nil {
		return fmt.Errorf("failed to create manifest directory: %w", err)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal manifest: %w", err)
	}

	if err := os.WriteFile(r.manifestPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	return nil
}

// ListAllManagedProviders returns all providers managed by any deployment
func (r *Reconciler) ListAllManagedProviders() map[string][]types.InstalledCapabilityState {
	manifest := r.loadManifest()
	return manifest.Deployments
}

// GetManagedProviders returns providers managed by a specific deployment
func (r *Reconciler) GetManagedProviders(deploymentID string) []types.InstalledCapabilityState {
	manifest := r.loadManifest()
	return manifest.Deployments[deploymentID]
}
