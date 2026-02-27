package spine

import (
	umsv1 "github.com/ambientlabscomputing/mycelium_spine/proto/ums/v1"
)

// Message is a received Mycelium Spine envelope presented to handler functions.
// Payload is raw bytes — callers are responsible for JSON (or other) unmarshaling.
type Message struct {
	EnvelopeID  string
	Type        string // Envelope.Type — e.g. "commands.run.server.request"
	Payload     []byte
	OrgID       string
	TraceID     string
	MailboxID   string
	Seq         uint64
	QoS         umsv1.QoS
	RequiresAck bool
	Timestamp   int64 // created_at_ms
}

// fromEnvelope converts a proto Envelope to a Message.
func fromEnvelope(env *umsv1.Envelope) Message {
	return Message{
		EnvelopeID:  env.EnvelopeId,
		Type:        env.Type,
		Payload:     env.Payload,
		OrgID:       env.OrgId,
		TraceID:     env.TraceId,
		MailboxID:   env.MailboxId,
		Seq:         env.Seq,
		QoS:         env.Qos,
		RequiresAck: env.RequiresAck,
		Timestamp:   env.CreatedAtMs,
	}
}
