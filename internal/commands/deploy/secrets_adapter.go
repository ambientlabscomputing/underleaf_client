package deploy

import (
	"context"

	"github.com/ambientlabscomputing/underleaf_client/internal/compiler"
	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
)

// secretsListerAdapter adapts *controlplane.CPlaneSecretsClient to satisfy
// compiler.SecretMetadataLister without creating an import cycle.
type secretsListerAdapter struct {
	client *controlplane.CPlaneSecretsClient
}

func (a *secretsListerAdapter) ListSecretMetadata(ctx context.Context, nameContains, scope, state string, limit, offset int) ([]compiler.SecretMetadataResult, error) {
	if a.client == nil {
		return nil, nil
	}
	resp, err := a.client.ListSecretMetadata(ctx, nameContains, scope, state, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]compiler.SecretMetadataResult, len(resp.Results))
	for i, sm := range resp.Results {
		out[i] = compiler.SecretMetadataResult{
			Name:  sm.Name,
			State: sm.State,
		}
	}
	return out, nil
}

// newSecretsLister wraps the given CPlaneSecretsClient as a SecretMetadataLister
// that can be passed to compiler.ValidateSecretRefs. Returns nil if client is nil.
func newSecretsLister(client *controlplane.CPlaneSecretsClient) compiler.SecretMetadataLister {
	if client == nil {
		return nil
	}
	return &secretsListerAdapter{client: client}
}
