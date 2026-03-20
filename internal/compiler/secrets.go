package compiler

import (
	"context"
	"fmt"
	"regexp"

	"github.com/ambientlabscomputing/underleaf_client/internal/types"
)

// secretRefPattern matches the ${secret:key/path} interpolation syntax used
// inside ServiceSpec.Environment values.
//
// Capture group 1 is the secret key/path.
var secretRefPattern = regexp.MustCompile(`\$\{secret:([a-zA-Z0-9_\-\.\/]+)\}`)

// ExtractSecretRefs scans all ServiceSpec.Environment values in a deployment
// and returns the deduplicated list of referenced secret key/paths.
//
// Example: "${secret:db/password}" → "db/password"
func ExtractSecretRefs(deployment *types.AppDeployment) []string {
	if deployment == nil {
		return nil
	}

	seen := make(map[string]struct{})
	var refs []string

	for _, svc := range deployment.Services {
		for _, val := range svc.Environment {
			for _, match := range secretRefPattern.FindAllStringSubmatch(val, -1) {
				if len(match) < 2 {
					continue
				}
				key := match[1]
				if _, exists := seen[key]; !exists {
					seen[key] = struct{}{}
					refs = append(refs, key)
				}
			}
		}
	}

	return refs
}

// SecretValidationWarning is a non-fatal warning returned when a secret
// referenced in a deployment cannot be found or is not usable.
type SecretValidationWarning struct {
	SecretRef string // The ${secret:...} key path
	Reason    string
}

func (w SecretValidationWarning) Error() string {
	return fmt.Sprintf("secret ref %q: %s", w.SecretRef, w.Reason)
}

// SecretMetadataResult is a minimal view of a secret metadata record as needed
// by the compiler. It matches the controlplane.SecretMetadata fields used here.
type SecretMetadataResult struct {
	Name  string
	State string // "active" | "rotating" | "revoked"
}

// SecretMetadataLister is the minimal interface the compiler needs for secret
// validation. *controlplane.CPlaneSecretsClient satisfies this interface.
type SecretMetadataLister interface {
	ListSecretMetadata(ctx context.Context, nameContains, scope, state string, limit, offset int) ([]SecretMetadataResult, error)
}

// SecretValidationWarning is returned for each missing/revoked secret ref.

// ValidateSecretRefs checks that every ${secret:...} reference in the
// deployment exists and is in "active" or "rotating" state according to lister.
//
// Returns a slice of non-fatal warnings. A nil or empty slice means all
// references are satisfied.
func ValidateSecretRefs(ctx context.Context, deployment *types.AppDeployment, lister SecretMetadataLister) []SecretValidationWarning {
	refs := ExtractSecretRefs(deployment)
	if len(refs) == 0 || lister == nil {
		return nil
	}

	results, err := lister.ListSecretMetadata(ctx, "", "", "", 500, 0)
	if err != nil {
		return []SecretValidationWarning{{
			SecretRef: "*",
			Reason:    fmt.Sprintf("could not validate secrets (server_api unreachable): %v", err),
		}}
	}

	byName := make(map[string]SecretMetadataResult, len(results))
	for _, sm := range results {
		byName[sm.Name] = sm
	}

	var warnings []SecretValidationWarning
	for _, ref := range refs {
		sm, found := byName[ref]
		if !found {
			warnings = append(warnings, SecretValidationWarning{
				SecretRef: ref,
				Reason:    "not found in control plane (run `ufctl secrets add` to register it)",
			})
			continue
		}
		if sm.State == "revoked" {
			warnings = append(warnings, SecretValidationWarning{
				SecretRef: ref,
				Reason:    fmt.Sprintf("secret is revoked (state: %s)", sm.State),
			})
		}
	}

	return warnings
}
