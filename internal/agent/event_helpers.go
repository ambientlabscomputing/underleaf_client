package agent

import (
	"context"
	"fmt"
	"time"
)

// toInterfaceSlice converts a string slice to an interface slice for structpb compatibility
func toInterfaceSlice(strs []string) []interface{} {
	result := make([]interface{}, len(strs))
	for i, s := range strs {
		result[i] = s
	}
	return result
}

// toInterfaceMap converts a string map to an interface map for structpb compatibility
func toInterfaceMap(m map[string]string) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// PublishMemberJoined publishes a member.joined event to MMA subscribers.
func (e *EventStreamServer) PublishMemberJoined(nodeID string, endpoints []string, tags map[string]string) error {
	payload := map[string]interface{}{
		"node_id":   nodeID,
		"endpoints": toInterfaceSlice(endpoints),
		"tags":      toInterfaceMap(tags),
	}
	return e.PublishEvent("member.joined", payload, "member", nodeID)
}

// PublishMemberLeft publishes a member.left event to MMA subscribers.
func (e *EventStreamServer) PublishMemberLeft(nodeID string, reason string) error {
	payload := map[string]interface{}{
		"node_id": nodeID,
		"reason":  reason,
	}
	return e.PublishEvent("member.left", payload, "member", nodeID)
}

// PublishMemberUpdated publishes a member.updated event to MMA subscribers.
func (e *EventStreamServer) PublishMemberUpdated(nodeID string, endpoints []string, tags map[string]string, status string) error {
	payload := map[string]interface{}{
		"node_id": nodeID,
	}
	if len(endpoints) > 0 {
		payload["endpoints"] = toInterfaceSlice(endpoints)
	}
	if len(tags) > 0 {
		payload["tags"] = toInterfaceMap(tags)
	}
	if status != "" {
		payload["status"] = status
	}
	return e.PublishEvent("member.updated", payload, "member", nodeID)
}

// PublishServiceStarted publishes a service.started event to MMA subscribers.
func (e *EventStreamServer) PublishServiceStarted(serviceID, serviceIdentity, nodeID string, endpoints []map[string]interface{}, capabilities []map[string]interface{}, labels map[string]string) error {
	payload := map[string]interface{}{
		"service_id":            serviceID,
		"service_identity":      serviceIdentity,
		"node_id":               nodeID,
		"endpoints":             endpoints,
		"capabilities_provided": capabilities,
		"labels":                labels,
	}
	return e.PublishEvent("service.started", payload, "service", serviceID)
}

// PublishMeshPolicyUpdated publishes a mesh_policy.updated event to MMA subscribers.
func (e *EventStreamServer) PublishMeshPolicyUpdated(policyVersion string, policyRef string, effectiveAt time.Time) error {
	payload := map[string]interface{}{
		"policy_version": policyVersion,
		"policy_ref":     policyRef,
		"effective_at":   effectiveAt.Format(time.RFC3339),
	}
	return e.PublishEvent("mesh_policy.updated", payload, "policy", policyVersion)
}

// PublishCapabilityCacheSnapshotUpdated publishes a capability_cache.snapshot.updated event to MMA subscribers.
func (e *EventStreamServer) PublishCapabilityCacheSnapshotUpdated(cacheVersion, signedSnapshotRef, schemaIndexDigest string, verifiedAt time.Time) error {
	payload := map[string]interface{}{
		"cache_version":       cacheVersion,
		"signed_snapshot_ref": signedSnapshotRef,
		"verified_at":         verifiedAt.Format(time.RFC3339),
		"schema_index_digest": schemaIndexDigest,
	}
	return e.PublishEvent("capability_cache.snapshot.updated", payload, "capability_cache", cacheVersion)
}

// PublishIdentityTrustRootsUpdated publishes an identity.trust_roots.updated event to MMA subscribers.
func (e *EventStreamServer) PublishIdentityTrustRootsUpdated(clusterTrustBundle string, validFrom, validTo time.Time, rotationID string) error {
	payload := map[string]interface{}{
		"cluster_trust_bundle": clusterTrustBundle,
		"valid_from":           validFrom.Format(time.RFC3339),
		"valid_to":             validTo.Format(time.RFC3339),
		"rotation_id":          rotationID,
	}
	return e.PublishEvent("identity.trust_roots.updated", payload, "identity", rotationID)
}

// PublishIdentityServiceIssued publishes an identity.service.issued event to MMA subscribers.
func (e *EventStreamServer) PublishIdentityServiceIssued(serviceID, spiffeID, certRef string, claims map[string]string, expiresAt time.Time) error {
	payload := map[string]interface{}{
		"service_id": serviceID,
		"spiffe_id":  spiffeID,
		"cert_ref":   certRef,
		"claims":     toInterfaceMap(claims),
		"expires_at": expiresAt.Format(time.RFC3339),
	}
	return e.PublishEvent("identity.service.issued", payload, "service", serviceID)
}

// PublishIdentityServiceRevoked publishes an identity.service.revoked event to MMA subscribers.
func (e *EventStreamServer) PublishIdentityServiceRevoked(serviceID, reason string, revokedAt time.Time) error {
	payload := map[string]interface{}{
		"service_id": serviceID,
		"reason":     reason,
		"revoked_at": revokedAt.Format(time.RFC3339),
	}
	return e.PublishEvent("identity.service.revoked", payload, "service", serviceID)
}

// PublishConsentStateUpdated publishes a consent_state.updated event to MMA subscribers.
func (e *EventStreamServer) PublishConsentStateUpdated(subjectRef, capabilityID, decision, version string, constraints map[string]interface{}) error {
	payload := map[string]interface{}{
		"subject_ref":   subjectRef,
		"capability_id": capabilityID,
		"decision":      decision,
		"constraints":   constraints,
		"version":       version,
	}
	return e.PublishEvent("consent_state.updated", payload, "consent", subjectRef)
}

// PublishClusterSnapshot publishes a cluster.snapshot event to MMA subscribers.
// This is typically sent on MMA startup or when the MMA reconnects.
func (e *EventStreamServer) PublishClusterSnapshot(members []map[string]interface{}, clusterCAFingerprint, meshConfigVersion string) error {
	payload := map[string]interface{}{
		"members":                members,
		"cluster_ca_fingerprint": clusterCAFingerprint,
		"mesh_config_version":    meshConfigVersion,
	}
	return e.PublishEvent("cluster.snapshot", payload, "", "")
}

// SendTestEvents sends a series of test events for development/testing.
// This helps verify the event stream is working end-to-end.
func (e *EventStreamServer) SendTestEvents(ctx context.Context) error {
	e.logger.Info("sending test events to MMA subscribers")

	// Wait a moment to let subscribers connect
	time.Sleep(2 * time.Second)

	// 1. Member joined (simpler than cluster snapshot)
	if err := e.PublishMemberJoined("node-001", []string{"tcp://10.0.1.10:8080", "quic://10.0.1.10:8443"}, map[string]string{"region": "us-west", "zone": "a"}); err != nil {
		return fmt.Errorf("failed to publish member.joined: %w", err)
	}
	e.logger.Info("published member.joined event")
	time.Sleep(500 * time.Millisecond)

	// 2. Service started (simplified - avoid nested complex structures)
	// Instead of passing complex nested maps, use simple structures
	payload := map[string]interface{}{
		"service_id":         "svc-001",
		"service_identity":   "spiffe://underleaf.local/service/svc-001",
		"node_id":            "node-001",
		"endpoint_proto":     "http",
		"endpoint_host":      "localhost",
		"endpoint_port":      float64(8080),
		"capability_id":      "com.example.api",
		"capability_version": "1.0",
		"app_label":          "web-api",
		"env_label":          "dev",
	}
	if err := e.PublishEvent("service.started", payload, "service", "svc-001"); err != nil {
		return fmt.Errorf("failed to publish service.started: %w", err)
	}
	e.logger.Info("published service.started event")
	time.Sleep(500 * time.Millisecond)

	// 3. Trust roots updated
	if err := e.PublishIdentityTrustRootsUpdated("-----BEGIN CERTIFICATE-----\n...\n-----END CERTIFICATE-----", time.Now(), time.Now().Add(365*24*time.Hour), "rotation-001"); err != nil {
		return fmt.Errorf("failed to publish trust roots: %w", err)
	}
	e.logger.Info("published identity.trust_roots.updated event")
	time.Sleep(500 * time.Millisecond)

	// 4. Capability cache updated
	if err := e.PublishCapabilityCacheSnapshotUpdated("1.0.0", "file:///tmp/cache.json", "sha256:def456", time.Now()); err != nil {
		return fmt.Errorf("failed to publish capability cache: %w", err)
	}
	e.logger.Info("published capability_cache.snapshot.updated event")
	time.Sleep(500 * time.Millisecond)

	// 5. Policy updated
	if err := e.PublishMeshPolicyUpdated("1.0.0", "file:///tmp/policy.json", time.Now()); err != nil {
		return fmt.Errorf("failed to publish policy update: %w", err)
	}
	e.logger.Info("published mesh_policy.updated event")

	e.logger.Info("test events sent successfully", "count", 5)
	return nil
}
