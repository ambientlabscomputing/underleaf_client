package bus

import "github.com/ambientlabscomputing/event_bus_client"

var StartingSubscriptions []event_bus_client.SubscriptionRequest

// strPtr returns a pointer to a string
func strPtr(s string) *string {
	return &s
}

// InitSubscriptions initializes the starting subscriptions for the event bus client.
// These subscriptions are established during Connect() and are required for receiving messages.
// The serverID is used as the GroupID for consumer group tracking.
//
// IMPORTANT: Filter fields (TargetType, TargetID, OrgID, TraceID) must be nil when not filtering,
// not pointers to empty strings. The event_bus_client library and server expect nil for "no filter".
func InitSubscriptions(serverID string) {
	StartingSubscriptions = []event_bus_client.SubscriptionRequest{
		// Subscribe to command run requests for this specific server
		{
			GroupID:    serverID, // Required for consumer group tracking
			Topic:      CommandsRunRequest,
			TargetType: strPtr("server"),
			TargetID:   &serverID,
			// OrgID, TraceID are nil
		},
		// Subscribe to deployment apply requests for this specific server
		// Note: TraceID is NOT included since it changes per job - routing handles this
		{
			GroupID:    serverID,
			Topic:      DeploymentsApplyRequest,
			TargetType: strPtr("server"),
			TargetID:   &serverID,
			// OrgID, TraceID are nil
		},
		// Subscribe to server data updates for this specific server
		{
			GroupID:  serverID,
			Topic:    ServerDataUpdate,
			TargetID: &serverID, // Filter to only get updates for this server
			// TargetType, OrgID, TraceID are nil
		},
	}
}
