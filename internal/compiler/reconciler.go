package compiler

import (
	"encoding/json"
	"reflect"

	"github.com/ambientlabscomputing/underleaf_client/internal/types"
)

// Reconciler performs three-way diff reconciliation
type Reconciler struct {
}

// NewReconciler creates a new reconciler
func NewReconciler() *Reconciler {
	return &Reconciler{}
}

// Reconcile performs three-way reconciliation
func (r *Reconciler) Reconcile(
	desired *types.CompiledGraph,
	observed *types.ObservedState,
	lastApplied *types.LastAppliedSnapshot,
	options types.ReconcileOptions,
) (*types.DiffResults, error) {

	results := &types.DiffResults{
		Operations: []types.Operation{},
	}

	// Track what we've seen in desired state
	seenResources := make(map[types.ResourceID]bool)

	// Reconcile each desired resource
	for resID, node := range desired.Nodes {
		seenResources[resID] = true
		op := r.reconcileResource(resID, node, observed, lastApplied)
		if op != nil {
			results.Operations = append(results.Operations, *op)
		}
	}

	// Check for resources that exist but aren't in desired state
	r.addPruneOperations(results, observed, lastApplied, seenResources, desired.DeploymentID, desired.Slug, options)

	return results, nil
}

// reconcileResource reconciles a single resource
func (r *Reconciler) reconcileResource(
	resID types.ResourceID,
	desired types.ResourceNode,
	observed *types.ObservedState,
	lastApplied *types.LastAppliedSnapshot,
) *types.Operation {

	obs := observed.Get(resID)
	var lastConfig interface{}
	if lastApplied != nil {
		lastConfig = lastApplied.Resources[resID.String()]
	}

	// Case 1: Not in observed, not in last applied → CREATE
	if obs == nil && lastConfig == nil {
		return &types.Operation{
			ResourceID:    resID,
			ResourceType:  resID.Type,
			ResourceName:  resID.Name,
			Type:          types.OpCreate,
			Description:   "Create " + string(resID.Type) + " " + resID.Name,
			DesiredConfig: desired.Config,
		}
	}

	// Case 2: Not in observed, in last applied → CREATE (was deleted externally)
	if obs == nil && lastConfig != nil {
		return &types.Operation{
			ResourceID:    resID,
			ResourceType:  resID.Type,
			ResourceName:  resID.Name,
			Type:          types.OpCreate,
			Description:   "Recreate " + string(resID.Type) + " " + resID.Name + " (was deleted)",
			DesiredConfig: desired.Config,
		}
	}

	// Case 3: In observed, not in last applied
	if obs != nil && lastConfig == nil {
		// Resource exists but we didn't create it
		if obs.ManagedBy == "" || obs.ManagedBy != resID.DeploymentID {
			// Unmanaged resource - error or adopt
			return &types.Operation{
				ResourceID:     resID,
				ResourceType:   resID.Type,
				ResourceName:   resID.Name,
				Type:           types.OpAdopt,
				Description:    "Adopt existing unmanaged " + string(resID.Type) + " " + resID.Name,
				DesiredConfig:  desired.Config,
				ObservedConfig: obs.Config,
			}
		}

		// Orphaned resource (has our label but not in last applied) - UPDATE
		return &types.Operation{
			ResourceID:     resID,
			ResourceType:   resID.Type,
			ResourceName:   resID.Name,
			Type:           types.OpUpdate,
			Description:    "Update orphaned " + string(resID.Type) + " " + resID.Name,
			DesiredConfig:  desired.Config,
			ObservedConfig: obs.Config,
		}
	}

	// Case 4: In both observed and last applied
	if obs != nil && lastConfig != nil {
		// Check if config has drifted
		if !r.configMatches(desired.Config, obs.Config) {
			return &types.Operation{
				ResourceID:     resID,
				ResourceType:   resID.Type,
				ResourceName:   resID.Name,
				Type:           types.OpUpdate,
				Description:    "Update " + string(resID.Type) + " " + resID.Name + " (config drift detected)",
				DesiredConfig:  desired.Config,
				ObservedConfig: obs.Config,
			}
		}

		// No change needed
		return &types.Operation{
			ResourceID:     resID,
			ResourceType:   resID.Type,
			ResourceName:   resID.Name,
			Type:           types.OpNoOp,
			Description:    "No change for " + string(resID.Type) + " " + resID.Name,
			DesiredConfig:  desired.Config,
			ObservedConfig: obs.Config,
		}
	}

	return nil
}

// configMatches checks if two configs are equivalent
func (r *Reconciler) configMatches(desired, observed interface{}) bool {
	// Marshal both to JSON and compare
	desiredJSON, err1 := json.Marshal(desired)
	observedJSON, err2 := json.Marshal(observed)

	if err1 != nil || err2 != nil {
		return false
	}

	// Unmarshal to normalize
	var desiredMap, observedMap map[string]interface{}
	if err := json.Unmarshal(desiredJSON, &desiredMap); err != nil {
		return false
	}
	if err := json.Unmarshal(observedJSON, &observedMap); err != nil {
		return false
	}

	return reflect.DeepEqual(desiredMap, observedMap)
}

// addPruneOperations adds delete operations for resources that exist but aren't desired
func (r *Reconciler) addPruneOperations(
	results *types.DiffResults,
	observed *types.ObservedState,
	lastApplied *types.LastAppliedSnapshot,
	seenResources map[types.ResourceID]bool,
	deploymentID string,
	slug string,
	options types.ReconcileOptions,
) {

	// Check networks
	for name, obs := range observed.Networks {
		resID := types.ResourceID{
			DeploymentID: deploymentID,
			Type:         types.ResourceTypeNetwork,
			Name:         name,
		}

		if !seenResources[resID] {
			// Not in desired state
			if lastApplied != nil && lastApplied.Resources[resID.String()] != nil {
				// We created it, now it's unwanted → DELETE
				results.Operations = append(results.Operations, types.Operation{
					ResourceID:     resID,
					ResourceType:   resID.Type,
					ResourceName:   resID.Name,
					Type:           types.OpDelete,
					Description:    "Delete unwanted " + string(resID.Type) + " " + resID.Name,
					ObservedConfig: obs.Config,
				})
			} else if options.PruneUnknown && obs.ManagedBy == deploymentID {
				// Prune unknown managed resources if flag set
				results.Operations = append(results.Operations, types.Operation{
					ResourceID:     resID,
					ResourceType:   resID.Type,
					ResourceName:   resID.Name,
					Type:           types.OpDelete,
					Description:    "Prune unknown " + string(resID.Type) + " " + resID.Name,
					ObservedConfig: obs.Config,
				})
			}
		}
	}

	// Check volumes
	for name, obs := range observed.Volumes {
		resID := types.ResourceID{
			DeploymentID: deploymentID,
			Type:         types.ResourceTypeVolume,
			Name:         name,
		}

		if !seenResources[resID] {
			if lastApplied != nil && lastApplied.Resources[resID.String()] != nil {
				results.Operations = append(results.Operations, types.Operation{
					ResourceID:     resID,
					ResourceType:   resID.Type,
					ResourceName:   resID.Name,
					Type:           types.OpDelete,
					Description:    "Delete unwanted " + string(resID.Type) + " " + resID.Name,
					ObservedConfig: obs.Config,
				})
			} else if options.PruneUnknown && obs.ManagedBy == deploymentID {
				results.Operations = append(results.Operations, types.Operation{
					ResourceID:     resID,
					ResourceType:   resID.Type,
					ResourceName:   resID.Name,
					Type:           types.OpDelete,
					Description:    "Prune unknown " + string(resID.Type) + " " + resID.Name,
					ObservedConfig: obs.Config,
				})
			}
		}
	}

	// Check containers
	for name, obs := range observed.Containers {
		resID := types.ResourceID{
			DeploymentID: deploymentID,
			Type:         types.ResourceTypeContainer,
			Name:         name,
		}

		if !seenResources[resID] {
			if lastApplied != nil && lastApplied.Resources[resID.String()] != nil {
				results.Operations = append(results.Operations, types.Operation{
					ResourceID:     resID,
					ResourceType:   resID.Type,
					ResourceName:   resID.Name,
					Type:           types.OpDelete,
					Description:    "Delete unwanted " + string(resID.Type) + " " + resID.Name,
					ObservedConfig: obs.Config,
				})
			} else if options.PruneUnknown && obs.ManagedBy == deploymentID {
				results.Operations = append(results.Operations, types.Operation{
					ResourceID:     resID,
					ResourceType:   resID.Type,
					ResourceName:   resID.Name,
					Type:           types.OpDelete,
					Description:    "Prune unknown " + string(resID.Type) + " " + resID.Name,
					ObservedConfig: obs.Config,
				})
			}
		}
	}
}
