package agent

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"github.com/ambientlabscomputing/underleaf_client/internal/controlplane"
	"github.com/ambientlabscomputing/underleaf_client/internal/raft"
)

const (
	// DefaultSecretSyncInterval is the default interval for syncing secret metadata
	// to the control plane.
	DefaultSecretSyncInterval = 60 * time.Second

	// MinSecretSyncInterval is the minimum allowed interval.
	MinSecretSyncInterval = 10 * time.Second

	// serverAPIIDKey is the CustomMetadata key that stores the server_api SecretMetadata ID.
	serverAPIIDKey = "server_api_id"
)

// SecretSyncReporter periodically syncs the local SecretStore inventory
// (names, versions, fingerprints) to server_api SecretMetadata records so that
// the control plane can detect version drift across clusters.
type SecretSyncReporter struct {
	serverID    string
	clusterID   string
	secretStore *raft.SecretStore
	secrets     *controlplane.CPlaneSecretsClient
	interval    time.Duration
	stopCh      chan struct{}
	doneCh      chan struct{}
}

// NewSecretSyncReporter creates a new SecretSyncReporter.
// If interval is below MinSecretSyncInterval, DefaultSecretSyncInterval is used.
func NewSecretSyncReporter(
	serverID, clusterID string,
	secretStore *raft.SecretStore,
	secrets *controlplane.CPlaneSecretsClient,
	interval time.Duration,
) *SecretSyncReporter {
	if interval < MinSecretSyncInterval {
		interval = DefaultSecretSyncInterval
	}
	return &SecretSyncReporter{
		serverID:    serverID,
		clusterID:   clusterID,
		secretStore: secretStore,
		secrets:     secrets,
		interval:    interval,
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}
}

// Start begins the sync loop in a background goroutine.
func (r *SecretSyncReporter) Start(ctx context.Context) {
	if r.secretStore == nil {
		slog.Debug("secret store not configured, secret sync reporter not started")
		return
	}
	slog.Info("starting secret sync reporter",
		"server_id", r.serverID,
		"cluster_id", r.clusterID,
		"interval", r.interval)
	go r.run(ctx)
}

// Stop stops the sync loop and waits for it to exit.
func (r *SecretSyncReporter) Stop() {
	close(r.stopCh)
	<-r.doneCh
	slog.Info("secret sync reporter stopped")
}

func (r *SecretSyncReporter) run(ctx context.Context) {
	defer close(r.doneCh)
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("secret sync reporter panic recovered",
				"panic", rec,
				"stack", string(debug.Stack()))
		}
	}()

	// Sync immediately on start.
	r.syncInventory(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.syncInventory(ctx)
		case <-r.stopCh:
			return
		case <-ctx.Done():
			return
		}
	}
}

// syncInventory lists all local secrets and patches server_api metadata
// for any that have drifted from the control plane record.
func (r *SecretSyncReporter) syncInventory(ctx context.Context) {
	metas, err := r.secretStore.List(ctx, "", nil)
	if err != nil {
		slog.Debug("secret sync: failed to list local secrets", "error", err)
		return
	}

	for _, meta := range metas {
		if meta == nil {
			continue
		}

		// Only sync secrets that were registered with server_api (have a stored ID).
		secretID, ok := meta.CustomMeta[serverAPIIDKey]
		if !ok || secretID == "" {
			continue
		}

		r.syncSecret(ctx, secretID, meta)
	}
}

// syncSecret compares a single local secret's version against server_api and
// patches if there is a discrepancy.
func (r *SecretSyncReporter) syncSecret(ctx context.Context, secretID string, local *raft.SecretMetadata) {
	remote, err := r.secrets.GetSecretMetadata(ctx, secretID)
	if err != nil {
		slog.Debug("secret sync: failed to get remote metadata",
			"secret_id", secretID,
			"error", err)
		return
	}

	if remote.CurrentVersion == local.Version {
		return // versions match, nothing to do
	}

	slog.Info("secret sync: version drift detected, patching control plane",
		"secret_id", secretID,
		"local_version", local.Version,
		"remote_version", remote.CurrentVersion)

	newVersion := local.Version
	if _, patchErr := r.secrets.PatchSecretMetadata(ctx, secretID, controlplane.PatchSecretMetadataRequest{
		CurrentVersion: &newVersion,
	}); patchErr != nil {
		slog.Warn("secret sync: failed to patch metadata",
			"secret_id", secretID,
			"error", patchErr)
	}
}
