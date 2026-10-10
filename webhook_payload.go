package panmail

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// WebhookPayload is the typed data of a webhook delivery: one of *MailEvent,
// *MailHeld, *MailHoldOutcome or *InboundMail. Switch on its type, or on the
// delivery's Event, which decides it.
type WebhookPayload interface {
	isWebhookPayload()
}

// ParsedWebhook is a verified delivery with its data decoded.
type ParsedWebhook struct {
	// Webhook is the verified envelope, the same VerifyWebhook returns. Its
	// Data still holds the raw payload.
	Webhook *WebhookEvent

	// Payload is the data decoded by Webhook.Event. It is nil for an event
	// this version has no type for — the gateway adds them — which is not an
	// error: the delivery was verified, and Webhook.Data has the payload.
	Payload WebhookPayload
}

// MailEvent is what the delivery pipeline reports: the payload of
// TriggerEventMailSent, Delivered, Opened, Clicked, Bounced and Rejected. It
// is one event for one recipient, the same thing MessageEvents lists.
type MailEvent struct {
	// ID is the event's own id.
	ID string

	// MessageID is the id Send returned.
	MessageID string

	ProviderID   string
	ProviderName string

	// Type is the precise event, a Status constant. The trigger is coarser:
	// TriggerEventMailBounced fires for StatusBounced, StatusHardBounce and
	// StatusSoftBounce, and TriggerEventMailRejected for StatusRejected and
	// StatusDropped. A number the gateway's enum has no name for keeps its
	// decimal spelling.
	Type Status

	Recipient string
	Subject   string

	// Timestamp is when the gateway recorded the event, which is not the
	// envelope's Timestamp: that is when this delivery was sent.
	Timestamp time.Time

	// Reason is the provider's or the gateway's explanation, where there is
	// one: why a bounce bounced.
	Reason string

	// Metadata is what the provider reported beyond that, in the provider's
	// own vocabulary — the clicked URL, an SMTP code. The gateway promises no
	// shape for it.
	Metadata map[string]any
}

// MailHeld is the payload of TriggerEventMailHeld: a filter rule quarantined a
// message for review. It carries no body and no attachment content — only
// what a person needs to decide whether to look.
type MailHeld struct {
	// ReviewID identifies the hold in the review queue. The outcome that ends
	// it carries the same one.
	ReviewID string

	// Direction is "outbound" or "inbound".
	Direction string

	// MessageID is what Send returned, for an outbound message; empty
	// inbound.
	MessageID string

	RuleID     string
	RuleName   string
	From       string
	Recipients []string
	Subject    string

	SizeBytes       int64
	AttachmentCount int
	AttachmentNames []string

	// Matched is why it was held, in the words the review page uses.
	Matched []MatchedCondition

	HeldAt time.Time

	// ExpiresAt is when retention gives up on it; zero if it never does.
	// Escalate before then: what expires unreviewed arrives as
	// TriggerEventMailExpired.
	ExpiresAt time.Time
}

// MatchedCondition is one condition of a filter rule that fired.
type MatchedCondition struct {
	Field    string
	Operator string
	Values   []string
	Header   string
	Number   int64
}

// MailHoldOutcome is the payload of the three ways a hold ends:
// TriggerEventMailReleased, TriggerEventMailQuarantineRejected and
// TriggerEventMailExpired. It repeats who and what was held, so it stands on
// its own.
type MailHoldOutcome struct {
	// ReviewID is the id MailHeld carried.
	ReviewID string

	Direction  string
	MessageID  string
	RuleID     string
	RuleName   string
	From       string
	Recipients []string
	Subject    string

	HeldAt time.Time

	// DecidedAt is when the hold ended, however it ended.
	DecidedAt time.Time

	// ReviewedBy and Note are a person's, so both are empty on an expiry.
	ReviewedBy string
	Note       string
}

// InboundMail is the payload of TriggerEventMailInbound: a message that
// arrived at one of the tenant's mailboxes.
type InboundMail struct {
	ID      string
	From    string
	To      []string
	Subject string
	HTML    string
	Text    string

	// Timestamp is when the gateway received it.
	Timestamp time.Time

	Headers map[string]string
}

func (*MailEvent) isWebhookPayload()       {}
func (*MailHeld) isWebhookPayload()        {}
func (*MailHoldOutcome) isWebhookPayload() {}
func (*InboundMail) isWebhookPayload()     {}

// ParseWebhook verifies a delivery exactly as VerifyWebhook does, then decodes
// its data into the type its event names.
//
//	parsed, err := panmail.ParseWebhook(secret, r.Header, body)
//	if err != nil {
//		http.Error(w, "", http.StatusUnauthorized)
//		return
//	}
//	switch p := parsed.Payload.(type) {
//	case *panmail.MailEvent:
//		// p.Type, p.Recipient, p.Reason
//	case *panmail.MailHeld:
//		// p.ReviewID, p.Matched, p.ExpiresAt
//	}
//
// An event this version has no type for is not refused. It comes back
// verified with a nil Payload and its data in Webhook.Data, because the
// gateway adds events, and a receiver refusing a delivery it cannot read gets
// it retried until the gateway gives up.
//
// A known event whose data is not the shape this version reads is an error,
// a *WebhookError saying so: the signature matched, so it is the gateway's
// format moving rather than an attack, but a payload decoded halfway would
// hand the caller zero values it cannot tell from real ones.
func ParseWebhook(secret string, header http.Header, body []byte, opts ...WebhookOption) (*ParsedWebhook, error) {
	event, err := VerifyWebhook(secret, header, body, opts...)
	if err != nil {
		return nil, err
	}

	var payload WebhookPayload
	switch event.Event {
	case TriggerEventMailSent, TriggerEventMailDelivered, TriggerEventMailOpened,
		TriggerEventMailClicked, TriggerEventMailBounced, TriggerEventMailRejected:
		payload, err = decodeMailEvent(event.Data)
	case TriggerEventMailHeld:
		payload, err = decodeMailHeld(event.Data)
	case TriggerEventMailReleased, TriggerEventMailQuarantineRejected, TriggerEventMailExpired:
		payload, err = decodeMailHoldOutcome(event.Data)
	case TriggerEventMailInbound:
		payload, err = decodeInboundMail(event.Data)
	default:
		return &ParsedWebhook{Webhook: event}, nil
	}
	if err != nil {
		return nil, &WebhookError{Reason: fmt.Sprintf(
			"the delivery is signed, but its %s data is not what this version reads: %v", event.Event, err)}
	}
	return &ParsedWebhook{Webhook: event, Payload: payload}, nil
}

// decodeStrict decodes data into v, refusing data that is absent: every known
// event carries an object, and a missing one would decode to a payload of zero
// values.
func decodeStrict(data json.RawMessage, v any) error {
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("there is no data")
	}
	return json.Unmarshal(data, v)
}

// The wire forms below are the gateway's Go struct tags, not its protos: it
// marshals webhook payloads with encoding/json. Hence snake_case, and a
// delivery event's type as a number.

func decodeMailEvent(data json.RawMessage) (*MailEvent, error) {
	var wire struct {
		ID           string         `json:"id"`
		ProviderID   string         `json:"provider_id"`
		ProviderName string         `json:"provider_name"`
		MessageID    string         `json:"message_id"`
		Type         enumName       `json:"type"`
		Recipient    string         `json:"recipient"`
		Subject      string         `json:"subject"`
		Timestamp    time.Time      `json:"timestamp"`
		Metadata     map[string]any `json:"metadata"`
		ErrorMessage string         `json:"error_message"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return nil, err
	}
	return &MailEvent{
		ID:           wire.ID,
		MessageID:    wire.MessageID,
		ProviderID:   wire.ProviderID,
		ProviderName: wire.ProviderName,
		Type:         statusFromWire(wire.Type),
		Recipient:    wire.Recipient,
		Subject:      wire.Subject,
		Timestamp:    wire.Timestamp,
		Reason:       wire.ErrorMessage,
		Metadata:     wire.Metadata,
	}, nil
}

func decodeMailHeld(data json.RawMessage) (*MailHeld, error) {
	var wire struct {
		ID              string             `json:"id"`
		Direction       string             `json:"direction"`
		MessageID       string             `json:"message_id"`
		RuleID          string             `json:"rule_id"`
		RuleName        string             `json:"rule_name"`
		From            string             `json:"from"`
		Recipients      []string           `json:"recipients"`
		Subject         string             `json:"subject"`
		SizeBytes       int64              `json:"size_bytes"`
		AttachmentCount int                `json:"attachment_count"`
		AttachmentNames []string           `json:"attachment_names"`
		Matched         []matchedCondition `json:"matched"`
		HeldAt          time.Time          `json:"held_at"`
		ExpiresAt       time.Time          `json:"expires_at"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return nil, err
	}
	matched := make([]MatchedCondition, 0, len(wire.Matched))
	for _, m := range wire.Matched {
		matched = append(matched, MatchedCondition(m))
	}
	return &MailHeld{
		ReviewID:        wire.ID,
		Direction:       wire.Direction,
		MessageID:       wire.MessageID,
		RuleID:          wire.RuleID,
		RuleName:        wire.RuleName,
		From:            wire.From,
		Recipients:      wire.Recipients,
		Subject:         wire.Subject,
		SizeBytes:       wire.SizeBytes,
		AttachmentCount: wire.AttachmentCount,
		AttachmentNames: wire.AttachmentNames,
		Matched:         matched,
		HeldAt:          wire.HeldAt,
		ExpiresAt:       wire.ExpiresAt,
	}, nil
}

type matchedCondition struct {
	Field    string   `json:"field"`
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
	Header   string   `json:"header"`
	Number   int64    `json:"number"`
}

func decodeMailHoldOutcome(data json.RawMessage) (*MailHoldOutcome, error) {
	var wire struct {
		ID         string    `json:"id"`
		Direction  string    `json:"direction"`
		MessageID  string    `json:"message_id"`
		RuleID     string    `json:"rule_id"`
		RuleName   string    `json:"rule_name"`
		From       string    `json:"from"`
		Recipients []string  `json:"recipients"`
		Subject    string    `json:"subject"`
		HeldAt     time.Time `json:"held_at"`
		DecidedAt  time.Time `json:"decided_at"`
		ReviewedBy string    `json:"reviewed_by"`
		Note       string    `json:"note"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return nil, err
	}
	return &MailHoldOutcome{
		ReviewID:   wire.ID,
		Direction:  wire.Direction,
		MessageID:  wire.MessageID,
		RuleID:     wire.RuleID,
		RuleName:   wire.RuleName,
		From:       wire.From,
		Recipients: wire.Recipients,
		Subject:    wire.Subject,
		HeldAt:     wire.HeldAt,
		DecidedAt:  wire.DecidedAt,
		ReviewedBy: wire.ReviewedBy,
		Note:       wire.Note,
	}, nil
}

func decodeInboundMail(data json.RawMessage) (*InboundMail, error) {
	var wire struct {
		ID        string            `json:"id"`
		From      string            `json:"from"`
		To        []string          `json:"to"`
		Subject   string            `json:"subject"`
		BodyHTML  string            `json:"body_html"`
		BodyText  string            `json:"body_text"`
		Timestamp protoTimestamp    `json:"timestamp"`
		Headers   map[string]string `json:"headers"`
	}
	if err := decodeStrict(data, &wire); err != nil {
		return nil, err
	}
	return &InboundMail{
		ID:        wire.ID,
		From:      wire.From,
		To:        wire.To,
		Subject:   wire.Subject,
		HTML:      wire.BodyHTML,
		Text:      wire.BodyText,
		Timestamp: time.Time(wire.Timestamp),
		Headers:   wire.Headers,
	}, nil
}

// statusFromWire names a delivery event's type. The gateway sends the
// EmailEventType number in a webhook, because encoding/json knows nothing of
// enum names; a name is taken as it is, in case that ever changes.
func statusFromWire(value enumName) Status {
	if value == "" {
		return StatusUnspecified
	}
	if number, err := strconv.Atoi(string(value)); err == nil {
		if name, ok := statusNumbers[number]; ok {
			return name
		}
	}
	return Status(value)
}

// statusNumbers is EmailEventType by number, checked against the "numbers" of
// testdata/event-types.json.
var statusNumbers = map[int]Status{
	0:  StatusUnspecified,
	1:  StatusSent,
	2:  StatusDelivered,
	3:  StatusOpened,
	4:  StatusClicked,
	5:  StatusBounced,
	6:  StatusHardBounce,
	7:  StatusSoftBounce,
	8:  StatusSpamReport,
	9:  StatusUnsubscribed,
	10: StatusDropped,
	11: StatusRejected,
	12: StatusPending,
	13: StatusDeferred,
	14: StatusComplained,
}

// protoTimestamp reads a google.protobuf.Timestamp as encoding/json writes the
// generated struct — {"seconds": n, "nanos": n}, either left out when zero —
// or as protojson writes it, an RFC 3339 string.
type protoTimestamp time.Time

func (t *protoTimestamp) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		return (*time.Time)(t).UnmarshalJSON(data)
	}
	var parts struct {
		Seconds int64 `json:"seconds"`
		Nanos   int32 `json:"nanos"`
	}
	if err := json.Unmarshal(data, &parts); err != nil {
		return fmt.Errorf("a timestamp is a string or {seconds, nanos}, not %s", data)
	}
	if parts.Nanos < 0 || parts.Nanos > 999_999_999 {
		return fmt.Errorf("a timestamp's nanos must be 0 to 999999999, not %d", parts.Nanos)
	}
	*t = protoTimestamp(time.Unix(parts.Seconds, int64(parts.Nanos)).UTC())
	return nil
}
