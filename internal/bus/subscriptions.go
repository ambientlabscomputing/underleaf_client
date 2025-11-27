package bus

import "github.com/ambientlabscomputing/event_bus_client"

var StartingSubscriptions []event_bus_client.SubscriptionRequest

// call InitSubscriptions externally to initialize starting subscriptions
func InitSubscriptions(serverID string) {
	StartingSubscriptions = []event_bus_client.SubscriptionRequest{
		// run server command
		{
			Topic:   CommandsRunRequest,
			GroupID: serverID,
		},
	}
}
