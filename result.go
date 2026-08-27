package panmail

// Status is the state a message is in.
//
// The values are the gateway's own enum names, carried verbatim: the wire
// format is protobuf JSON, which encodes an enum as its name rather than its
// number. Comparing against these constants is comparing against exactly what
// the gateway sent.
type Status string

const (
	// StatusPending is what a successful Send reports: the gateway has the
	// message on disk and will deliver it. Delivery itself is reported later,
	// through events and webhooks.
	StatusPending Status = "EMAIL_EVENT_TYPE_PENDING"

	StatusSent      Status = "EMAIL_EVENT_TYPE_SENT"
	StatusDelivered Status = "EMAIL_EVENT_TYPE_DELIVERED"
	StatusDropped   Status = "EMAIL_EVENT_TYPE_DROPPED"
)

// Result is what the gateway accepted.
type Result struct {
	// MessageID identifies the message for the rest of its life: delivery
	// events, webhook notifications and the analytics pages are all keyed by
	// it. Worth storing next to whatever prompted the send.
	MessageID string

	// Status at the moment of acceptance, which for a queued message is
	// StatusPending.
	Status Status
}
