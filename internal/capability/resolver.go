package capability

import (
	"fmt"
	"runtime"
	"sort"

	"github.com/Masterminds/semver/v3"
)

// Resolver selects the best provider for a capability request
type Resolver struct {
	registry *Registry
}

// NewResolver creates a new capability resolver
func NewResolver(registry *Registry) *Resolver {
	return &Resolver{
		registry: registry,
	}
}

// Resolve finds the best provider for a capability request
func (r *Resolver) Resolve(req CapabilityRequest) (*Provider, *Capability, error) {
	// Get capability definition
	capability, err := r.registry.GetCapability(req.CapabilityID)
	if err != nil {
		return nil, nil, fmt.Errorf("capability not found: %w", err)
	}

	// Find all providers for this capability
	providers, err := r.registry.FindProviders(req.CapabilityID)
	if err != nil {
		return nil, nil, fmt.Errorf("no providers found: %w", err)
	}

	// Apply filters
	filtered := r.applyFilters(providers, req.Constraints)
	if len(filtered) == 0 {
		return nil, nil, fmt.Errorf("no providers match constraints for capability %s", req.CapabilityID)
	}

	// Apply version constraints
	// Check if the capability's version matches the requested version range
	if req.VersionRange != "" {
		capVersion, err := semver.NewVersion(capability.Version)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid capability version %s: %w", capability.Version, err)
		}

		constraint, err := semver.NewConstraint(req.VersionRange)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid version range %s: %w", req.VersionRange, err)
		}

		if !constraint.Check(capVersion) {
			return nil, nil, fmt.Errorf("capability version %s does not match requested range %s", capability.Version, req.VersionRange)
		}

		// Filter providers that support this capability version
		var versionFiltered []*Provider
		for _, provider := range filtered {
			if r.providerSupportsCapabilityVersion(provider, req.CapabilityID, capability.Version) {
				versionFiltered = append(versionFiltered, provider)
			}
		}
		filtered = versionFiltered

		if len(filtered) == 0 {
			return nil, nil, fmt.Errorf("no providers support capability %s version %s", req.CapabilityID, capability.Version)
		}
	}

	// Sort and select best provider
	best := r.selectBest(filtered)

	return best, capability, nil
}

// applyFilters applies constraint filters to providers
func (r *Resolver) applyFilters(providers []*Provider, constraints ResolveConstraints) []*Provider {
	filtered := make([]*Provider, 0, len(providers))

	for _, provider := range providers {
		// Apply trust tier filter
		if constraints.TrustTier != "" && !r.matchesTrustTier(provider, constraints.TrustTier) {
			continue
		}

		// Apply platform filter (default to current platform if not specified)
		platform := constraints.Platform
		if platform == "" {
			platform = runtime.GOOS
		}

		// Apply architecture filter (default to current arch if not specified)
		architecture := constraints.Architecture
		if architecture == "" {
			architecture = runtime.GOARCH
		}

		// Check if provider supports the requested platform
		// Empty SupportedPlatforms means platform-agnostic (OCI, npm, pypi, etc.)
		if len(provider.Artifact.SupportedPlatforms) > 0 {
			platformSupported := false
			for _, supportedPlatform := range provider.Artifact.SupportedPlatforms {
				if supportedPlatform.OS == platform && supportedPlatform.Arch == architecture {
					platformSupported = true
					break
				}
			}
			if !platformSupported {
				// Skip this provider - platform not supported
				continue
			}
		}

		filtered = append(filtered, provider)
	}

	return filtered
}

// matchesTrustTier checks if a provider matches the trust tier constraint
func (r *Resolver) matchesTrustTier(provider *Provider, constraint string) bool {
	switch constraint {
	case "official_only":
		return provider.TrustTier == TrustOfficial

	case "certified+":
		return provider.TrustTier == TrustOfficial ||
			provider.TrustTier == TrustCertified

	case "community+":
		return provider.TrustTier == TrustOfficial ||
			provider.TrustTier == TrustCertified ||
			provider.TrustTier == TrustCommunity

	case "all":
		return true

	default:
		// Default to certified+
		return provider.TrustTier == TrustOfficial ||
			provider.TrustTier == TrustCertified
	}
}

// providerSupportsCapabilityVersion checks if a provider supports a specific capability version
func (r *Resolver) providerSupportsCapabilityVersion(provider *Provider, capabilityID string, capabilityVersion string) bool {
	// Find the capability reference in the provider's capabilities list
	for _, capRef := range provider.Capabilities {
		if capRef.ID != capabilityID {
			continue
		}

		// Parse the provider's version range for this capability
		constraint, err := semver.NewConstraint(capRef.VersionRange)
		if err != nil {
			continue
		}

		// Parse the capability version
		version, err := semver.NewVersion(capabilityVersion)
		if err != nil {
			continue
		}

		// Check if the capability version matches the provider's supported range
		if constraint.Check(version) {
			return true
		}
	}

	return false
}

// selectBest selects the best provider from a list of candidates
func (r *Resolver) selectBest(providers []*Provider) *Provider {
	if len(providers) == 0 {
		return nil
	}

	// Sort providers by:
	// 1. Trust tier (official > certified > community > experimental > local)
	// 2. Version (newer is better)
	sort.Slice(providers, func(i, j int) bool {
		// Compare trust tier
		tierI := r.trustTierRank(providers[i].TrustTier)
		tierJ := r.trustTierRank(providers[j].TrustTier)

		if tierI != tierJ {
			return tierI > tierJ // Higher rank is better
		}

		// Compare versions
		versionI, errI := semver.NewVersion(providers[i].Version)
		versionJ, errJ := semver.NewVersion(providers[j].Version)

		if errI != nil || errJ != nil {
			// If version parsing fails, maintain current order
			return false
		}

		return versionI.GreaterThan(versionJ) // Newer version is better
	})

	return providers[0]
}

// trustTierRank assigns a numeric rank to trust tiers for sorting
func (r *Resolver) trustTierRank(tier TrustTier) int {
	switch tier {
	case TrustOfficial:
		return 5
	case TrustCertified:
		return 4
	case TrustCommunity:
		return 3
	case TrustExperimental:
		return 2
	case TrustLocal:
		return 1
	default:
		return 0
	}
}
