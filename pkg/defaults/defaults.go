package defaults

// Build-time configuration defaults
// These are overridden at build time using ldflags:
// -X 'github.com/ambientlabscomputing/underleaf_client/pkg/defaults.APIBaseURL=https://...'
// -X 'github.com/ambientlabscomputing/underleaf_client/pkg/defaults.EventBusEndpoint=wss://...'
// -X 'github.com/ambientlabscomputing/underleaf_client/pkg/defaults.UCRSBaseURL=https://...'

var (
	// APIBaseURL is the default API endpoint
	APIBaseURL = "http://localhost:8080/api/v1/servers"

	// EventBusEndpoint is the default event bus WebSocket endpoint
	EventBusEndpoint = "ws://localhost:9000/ws"

	// UCRSBaseURL is the default UCRS (Capability Registry) endpoint
	UCRSBaseURL = "http://localhost:8090/api/v1/registry"
)
