package capability

import (
	"testing"
	"time"

	ucrstypes "github.com/ambientlabscomputing/ucrs/sdk/types"
)

func TestResolver_Resolve(t *testing.T) {
	// Create a test registry with sample data
	registry := NewRegistry()

	// Create test snapshot with actual UCRS types
	snapshot := &ucrstypes.RegistrySnapshot{
		Version:   "test-v1",
		Timestamp: time.Now(),
		Capabilities: []ucrstypes.Capability{
			{
				ID:              "test.capability",
				Version:         "1.0.0",
				Description:     "A test capability",
				RiskClass:       ucrstypes.RiskLow,
				AllowedVerbs:    []string{"test"},
				PermissionClass: "test.basic",
				CreatedAt:       time.Now(),
				UpdatedAt:       time.Now(),
			},
		},
		Providers: []ucrstypes.Provider{
			{
				ProviderID:  "test.provider1",
				Version:     "1.0.0",
				Description: "First test provider",
				Capabilities: []ucrstypes.CapabilityRef{
					{ID: "test.capability", VersionRange: "^1.0"},
				},
				TrustTier: ucrstypes.TrustCertified,
				Artifact: ucrstypes.Artifact{
					Type:   ucrstypes.ArtifactOCI,
					URI:    "localhost:5000/test/provider1:1.0.0",
					Digest: "sha256:abc123",
				},
				SandboxProfile: "basic",
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			},
			{
				ProviderID:  "test.provider2",
				Version:     "2.0.0",
				Description: "Second test provider (newer version)",
				Capabilities: []ucrstypes.CapabilityRef{
					{ID: "test.capability", VersionRange: "^1.0"},
				},
				TrustTier: ucrstypes.TrustOfficial,
				Artifact: ucrstypes.Artifact{
					Type:   ucrstypes.ArtifactOCI,
					URI:    "localhost:5000/test/provider2:2.0.0",
					Digest: "sha256:def456",
				},
				SandboxProfile: "basic",
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			},
			{
				ProviderID:  "test.provider3",
				Version:     "1.5.0",
				Description: "Third test provider (experimental)",
				Capabilities: []ucrstypes.CapabilityRef{
					{ID: "test.capability", VersionRange: "^1.0"},
				},
				TrustTier: ucrstypes.TrustExperimental,
				Artifact: ucrstypes.Artifact{
					Type:   ucrstypes.ArtifactOCI,
					URI:    "localhost:5000/test/provider3:1.5.0",
					Digest: "sha256:ghi789",
				},
				SandboxProfile: "basic",
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			},
		},
		Manifest: ucrstypes.SignedManifest{
			KeyID:     "test-key",
			Signature: "test-signature",
			Algorithm: "Ed25519",
			SignedAt:  time.Now(),
		},
		ETag: "test-etag",
	}

	// Load into registry
	if err := registry.LoadSnapshot(snapshot); err != nil {
		t.Fatalf("Failed to load snapshot: %v", err)
	}

	// Create resolver
	resolver := NewResolver(registry)

	tests := []struct {
		name             string
		request          CapabilityRequest
		expectedProvider string
		expectedTier     ucrstypes.TrustTier
		shouldError      bool
	}{
		{
			name: "resolve with official trust tier constraint",
			request: CapabilityRequest{
				CapabilityID: "test.capability",
				VersionRange: "^1.0",
				Constraints: ResolveConstraints{
					TrustTier: "official_only",
				},
			},
			expectedProvider: "test.provider2",
			expectedTier:     ucrstypes.TrustOfficial,
			shouldError:      false,
		},
		{
			name: "resolve with certified+ trust tier constraint",
			request: CapabilityRequest{
				CapabilityID: "test.capability",
				VersionRange: "^1.0",
				Constraints: ResolveConstraints{
					TrustTier: "certified+",
				},
			},
			expectedProvider: "test.provider2",
			expectedTier:     ucrstypes.TrustOfficial,
			shouldError:      false,
		},
		{
			name: "resolve with all trust tiers allowed",
			request: CapabilityRequest{
				CapabilityID: "test.capability",
				VersionRange: "^1.0",
				Constraints: ResolveConstraints{
					TrustTier: "all",
				},
			},
			expectedProvider: "test.provider2",
			expectedTier:     ucrstypes.TrustOfficial,
			shouldError:      false,
		},
		{
			name: "fail to resolve non-existent capability",
			request: CapabilityRequest{
				CapabilityID: "nonexistent.capability",
				VersionRange: "^1.0",
				Constraints: ResolveConstraints{
					TrustTier: "all",
				},
			},
			shouldError: true,
		},
		{
			name: "resolve with all trust tiers selects best by trust tier then version",
			request: CapabilityRequest{
				CapabilityID: "test.capability",
				VersionRange: "^1.0",
				Constraints: ResolveConstraints{
					TrustTier: "all",
				},
			},
			expectedProvider: "test.provider2", // Official trust tier wins even though all match ^1.0
			expectedTier:     ucrstypes.TrustOfficial,
			shouldError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, capability, err := resolver.Resolve(tt.request)

			if tt.shouldError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if provider == nil {
				t.Errorf("Expected provider but got nil")
				return
			}

			if provider.ProviderID != tt.expectedProvider {
				t.Errorf("Expected provider %s, got %s", tt.expectedProvider, provider.ProviderID)
			}

			if provider.TrustTier != tt.expectedTier {
				t.Errorf("Expected trust tier %s, got %s", tt.expectedTier, provider.TrustTier)
			}

			if capability == nil {
				t.Errorf("Expected capability but got nil")
				return
			}

			if capability.ID != "test.capability" {
				t.Errorf("Expected capability test.capability, got %s", capability.ID)
			}
		})
	}
}

func TestRegistry_LoadSnapshot(t *testing.T) {
	registry := NewRegistry()

	snapshot := &ucrstypes.RegistrySnapshot{
		Version:   "test-v1",
		Timestamp: time.Now(),
		Capabilities: []ucrstypes.Capability{
			{
				ID:              "cap1",
				Version:         "1.0.0",
				Description:     "Capability 1",
				RiskClass:       ucrstypes.RiskLow,
				AllowedVerbs:    []string{"test"},
				PermissionClass: "test",
				CreatedAt:       time.Now(),
				UpdatedAt:       time.Now(),
			},
			{
				ID:              "cap2",
				Version:         "1.0.0",
				Description:     "Capability 2",
				RiskClass:       ucrstypes.RiskMedium,
				AllowedVerbs:    []string{"test"},
				PermissionClass: "test",
				CreatedAt:       time.Now(),
				UpdatedAt:       time.Now(),
			},
		},
		Providers: []ucrstypes.Provider{
			{
				ProviderID:  "provider1",
				Version:     "1.0.0",
				Description: "Provider 1",
				Capabilities: []ucrstypes.CapabilityRef{
					{ID: "cap1", VersionRange: "^1.0"},
				},
				TrustTier: ucrstypes.TrustCertified,
				Artifact: ucrstypes.Artifact{
					Type:   ucrstypes.ArtifactOCI,
					URI:    "test/provider1:1.0.0",
					Digest: "sha256:test",
				},
				SandboxProfile: "basic",
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			},
		},
		Manifest: ucrstypes.SignedManifest{
			KeyID:     "test",
			Signature: "test",
			Algorithm: "Ed25519",
			SignedAt:  time.Now(),
		},
	}

	err := registry.LoadSnapshot(snapshot)
	if err != nil {
		t.Fatalf("LoadSnapshot failed: %v", err)
	}

	// Verify capabilities were indexed
	cap, err := registry.GetCapability("cap1")
	if err != nil {
		t.Errorf("Failed to get capability cap1: %v", err)
	}
	if cap == nil || cap.ID != "cap1" {
		t.Errorf("Expected capability cap1, got %v", cap)
	}

	// Verify providers were indexed
	providers, err := registry.FindProviders("cap1")
	if err != nil {
		t.Errorf("Failed to find providers for cap1: %v", err)
	}
	if len(providers) != 1 {
		t.Errorf("Expected 1 provider for cap1, got %d", len(providers))
	}
	if providers[0].ProviderID != "provider1" {
		t.Errorf("Expected provider1, got %s", providers[0].ProviderID)
	}

	// Verify provider by ID lookup
	provider, err := registry.GetProvider("provider1", "1.0.0")
	if err != nil {
		t.Errorf("Failed to get provider: %v", err)
	}
	if provider == nil || provider.ProviderID != "provider1" {
		t.Errorf("Expected provider1, got %v", provider)
	}

	// Verify stats
	stats := registry.Stats()
	if stats.CapabilityCount != 2 {
		t.Errorf("Expected 2 capabilities, got %d", stats.CapabilityCount)
	}
	if stats.ProviderCount != 1 {
		t.Errorf("Expected 1 provider, got %d", stats.ProviderCount)
	}
}
