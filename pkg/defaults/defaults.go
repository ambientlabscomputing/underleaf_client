package defaults

// Build-time configuration defaults
// These are overridden at build time using ldflags:
// -X 'github.com/ambientlabscomputing/underleaf_client/pkg/defaults.APIBaseURL=https://...'
// -X 'github.com/ambientlabscomputing/underleaf_client/pkg/defaults.SpineEndpoint=mycelium.example.com:9090'
// -X 'github.com/ambientlabscomputing/underleaf_client/pkg/defaults.UCRSBaseURL=https://...'

var (
	// APIBaseURL is the default API endpoint
	APIBaseURL = "http://localhost:8080/api/v1/servers"

	// SpineEndpoint is the default Mycelium Spine gRPC endpoint
	SpineEndpoint = "localhost:9090"

	// UCRSBaseURL is the default UCRS (Capability Registry) endpoint
	UCRSBaseURL = "http://localhost:8080/api/v1/registry"
)
