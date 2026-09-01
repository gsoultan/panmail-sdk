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

	// The rest are what the gateway reports afterwards, on the event stream and
	// through webhooks, keyed by Result.MessageID. A Send never returns them.
	//
	// StatusBounced is the general refusal; StatusHardBounce and
	// StatusSoftBounce are the gateway saying which kind, when the provider
	// told it. A hard bounce is an address that will never accept mail and
	// should be suppressed; a soft one is a mailbox that was full or a server
	// that was down.
	StatusSent         Status = "EMAIL_EVENT_TYPE_SENT"
	StatusDelivered    Status = "EMAIL_EVENT_TYPE_DELIVERED"
	StatusOpened       Status = "EMAIL_EVENT_TYPE_OPENED"
	StatusClicked      Status = "EMAIL_EVENT_TYPE_CLICKED"
	StatusBounced      Status = "EMAIL_EVENT_TYPE_BOUNCED"
	StatusHardBounce   Status = "EMAIL_EVENT_TYPE_HARD_BOUNCE"
	StatusSoftBounce   Status = "EMAIL_EVENT_TYPE_SOFT_BOUNCE"
	StatusSpamReport   Status = "EMAIL_EVENT_TYPE_SPAM_REPORT"
	StatusUnsubscribed Status = "EMAIL_EVENT_TYPE_UNSUBSCRIBED"
	StatusDropped      Status = "EMAIL_EVENT_TYPE_DROPPED"
	StatusRejected     Status = "EMAIL_EVENT_TYPE_REJECTED"
	StatusDeferred     Status = "EMAIL_EVENT_TYPE_DEFERRED"
	StatusComplained   Status = "EMAIL_EVENT_TYPE_COMPLAINED"

	// StatusUnspecified is protobuf's zero value, not a state a message is
	// ever in. It is here because the gateway's enum has it, and a client that
	// quietly omitted a value would be the start of the two drifting.
	StatusUnspecified Status = "EMAIL_EVENT_TYPE_UNSPECIFIED"
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
