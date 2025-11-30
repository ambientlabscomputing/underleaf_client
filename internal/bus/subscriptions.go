package bus

import "github.com/ambientlabscomputing/event_bus_client"

var StartingSubscriptions []event_bus_client.SubscriptionRequest

// InitSubscriptions initializes the starting subscriptions for the event bus client.
// These subscriptions are established during Connect() and are required for receiving messages.
// The serverID is used as the GroupID for consumer group tracking.
//
// IMPORTANT: Filter fields (TargetType, TargetID, OrgID, TraceID) must be nil when not filtering,
// not pointers to empty strings. The event_bus_client library and server expect nil for "no filter".
func InitSubscriptions(serverID string) {
	StartingSubscriptions = []event_bus_client.SubscriptionRequest{
		// Subscribe to command run requests - no filters, we handle filtering in handler
		{
			GroupID: serverID, // Required for consumer group tracking
			Topic:   CommandsRunRequest,
			// TargetType, TargetID, OrgID, TraceID are nil = receive all messages on this topic
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
