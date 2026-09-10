package panmail

// TriggerEvent is why a webhook fired.
//
// The values are the gateway's own WebhookTriggerEvent enum names, carried
// verbatim: it dispatches with event.String(), so what arrives in the
// X-Panmail-Event header and the envelope's "event" field is the enum name and
// nothing else. Comparing against these constants is comparing against exactly
// what the gateway sent.
//
// Not to be confused with Status, which names the state a message is in and is
// what a send returns. They are different enums in the gateway and neither is a
// superset of the other.
type TriggerEvent string

const (
	// TriggerEventMailSent through TriggerEventMailInbound are the delivery
	// pipeline reporting what a provider did.
	TriggerEventMailSent      TriggerEvent = "WEBHOOK_TRIGGER_EVENT_MAIL_SENT"
	TriggerEventMailDelivered TriggerEvent = "WEBHOOK_TRIGGER_EVENT_MAIL_DELIVERED"
	TriggerEventMailOpened    TriggerEvent = "WEBHOOK_TRIGGER_EVENT_MAIL_OPENED"
	TriggerEventMailClicked   TriggerEvent = "WEBHOOK_TRIGGER_EVENT_MAIL_CLICKED"
	TriggerEventMailBounced   TriggerEvent = "WEBHOOK_TRIGGER_EVENT_MAIL_BOUNCED"

	// TriggerEventMailRejected is a provider refusing a send. It is not
	// TriggerEventMailQuarantineRejected, which is a person refusing a held
	// message — a subscriber acting on "rejected" needs to know which it got.
	TriggerEventMailRejected TriggerEvent = "WEBHOOK_TRIGGER_EVENT_MAIL_REJECTED"

	TriggerEventMailInbound TriggerEvent = "WEBHOOK_TRIGGER_EVENT_MAIL_INBOUND"

	// TriggerEventMailHeld is a filter rule quarantining a message for review.
	// Worth subscribing to: without it a hold is silent, and a message nobody
	// reviewed expires unseen, which is worse than one that was refused because
	// nobody ever decided it.
	TriggerEventMailHeld TriggerEvent = "WEBHOOK_TRIGGER_EVENT_MAIL_HELD"

	// The three ways a hold ends.
	TriggerEventMailReleased           TriggerEvent = "WEBHOOK_TRIGGER_EVENT_MAIL_RELEASED"
	TriggerEventMailQuarantineRejected TriggerEvent = "WEBHOOK_TRIGGER_EVENT_MAIL_QUARANTINE_REJECTED"

	// TriggerEventMailExpired is a held message reaching its retention
	// deadline with nobody having decided. This is the one to alert on: it does
	// not say a message was refused, it says a queue went unwatched.
	TriggerEventMailExpired TriggerEvent = "WEBHOOK_TRIGGER_EVENT_MAIL_EXPIRED"

	// TriggerEventUnspecified is protobuf's zero value, not a reason a webhook
	// ever fires. Here because the gateway's enum has it, and a client that
	// quietly omitted a value would be the start of the two drifting.
	TriggerEventUnspecified TriggerEvent = "WEBHOOK_TRIGGER_EVENT_UNSPECIFIED"
)
