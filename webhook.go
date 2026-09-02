package panmail

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Headers the gateway sets on a webhook delivery.
const (
	// WebhookSignatureHeader carries the HMAC, prefixed with the algorithm so
	// a future scheme can be introduced without a receiver having to guess
	// which one it is looking at.
	WebhookSignatureHeader = "X-Panmail-Signature"

	// WebhookTimestampHeader is the second half of what is signed. Without it
	// a notification captured once could be replayed forever against the same
	// signature.
	WebhookTimestampHeader = "X-Panmail-Timestamp"

	// WebhookEventHeader lets a receiver route without parsing the body. The
	// value is a dotted name — "mail.sent", "mail.bounced" — and not one of
	// the Status constants, which name delivery states rather than events.
	WebhookEventHeader = "X-Panmail-Event"

	// WebhookDeliveryHeader is stable across retries of the same notification,
	// which is what lets a receiver make its own handling idempotent. The
	// gateway retries, so a handler that is not idempotent will act twice.
	WebhookDeliveryHeader = "X-Panmail-Delivery"
)

// DefaultWebhookTolerance is how far a delivery's timestamp may be from now.
//
// It exists because clocks disagree, not because old notifications are
// acceptable: the window is what stops a captured request being replayed
// tomorrow, so wider is weaker.
const DefaultWebhookTolerance = 5 * time.Minute

// ErrWebhookUnsigned reports a delivery that carried no signature at all.
//
// The gateway sends a subscription with no secret unsigned rather than not
// sending it, so this is a real thing that arrives at a real endpoint — and
// accepting it would make verification decorative. Set a secret on the
// subscription.
var ErrWebhookUnsigned = errors.New("panmail: the webhook carried no signature")

// WebhookError reports a delivery that could not be trusted.
//
// There is one type rather than several because there is one thing to do about
// it: refuse the request. The message says which check failed, for the log.
type WebhookError struct {
	Reason string
}

func (e *WebhookError) Error() string {
	return "panmail: the webhook could not be verified: " + e.Reason
}

// WebhookEvent is a verified delivery.
type WebhookEvent struct {
	// Event is the dotted name from the header, e.g. "mail.bounced".
	Event string

	// TenantID is the tenant the event belongs to. Worth checking against the
	// tenant you expected, since one endpoint can serve several.
	TenantID string

	// Timestamp is when the gateway sent it.
	Timestamp time.Time

	// DeliveryID is stable across retries. Store it and ignore a repeat.
	DeliveryID string

	// Data is the event-specific payload, left as raw JSON because its shape
	// depends on Event and this package does not model every one of them.
	Data json.RawMessage
}

// WebhookOption configures VerifyWebhook.
type WebhookOption func(*webhookOptions)

type webhookOptions struct {
	tolerance time.Duration
	now       func() time.Time
}

// WithWebhookTolerance widens or narrows the window a timestamp may fall in.
//
// Narrower is stronger. The default is DefaultWebhookTolerance.
func WithWebhookTolerance(d time.Duration) WebhookOption {
	return func(o *webhookOptions) { o.tolerance = d }
}

// VerifyWebhook checks that a delivery came from the gateway, and returns it.
//
// body must be the bytes as they arrived. Re-serialising a decoded payload
// will not verify: the signature covers what was sent, down to key order and
// whitespace, so read the raw body first and parse afterwards.
//
//	body, err := io.ReadAll(r.Body)
//	if err != nil {
//		http.Error(w, "", http.StatusBadRequest)
//		return
//	}
//	event, err := panmail.VerifyWebhook(secret, r.Header, body)
//	if err != nil {
//		http.Error(w, "", http.StatusUnauthorized)
//		return
//	}
//
// An error means the request is not known to have come from the gateway.
// There is nothing to salvage from one: do not act on the payload, and do not
// echo the reason back to the caller.
func VerifyWebhook(secret string, header http.Header, body []byte, opts ...WebhookOption) (*WebhookEvent, error) {
	if secret == "" {
		return nil, &WebhookError{Reason: "no signing secret was supplied"}
	}

	resolved := webhookOptions{tolerance: DefaultWebhookTolerance, now: time.Now}
	for _, opt := range opts {
		opt(&resolved)
	}
	if resolved.tolerance <= 0 {
		resolved.tolerance = DefaultWebhookTolerance
	}

	signature := header.Get(WebhookSignatureHeader)
	rawTimestamp := header.Get(WebhookTimestampHeader)
	if signature == "" || rawTimestamp == "" {
		return nil, ErrWebhookUnsigned
	}

	seconds, err := strconv.ParseInt(strings.TrimSpace(rawTimestamp), 10, 64)
	if err != nil {
		return nil, &WebhookError{Reason: fmt.Sprintf("the timestamp %q is not a number", rawTimestamp)}
	}
	sent := time.Unix(seconds, 0)

	// Both directions: a timestamp far in the future is as much a sign of
	// something wrong as one far in the past, and only checking the past would
	// let a forged future timestamp keep a capture valid indefinitely.
	if drift := resolved.now().Sub(sent); drift > resolved.tolerance || drift < -resolved.tolerance {
		return nil, &WebhookError{Reason: fmt.Sprintf(
			"the timestamp is %s away from now, outside the %s tolerance", drift.Round(time.Second), resolved.tolerance)}
	}

	// hmac.Equal rather than ==, so the comparison does not return early on
	// the first differing byte and leak how much of a guess was right.
	if !hmac.Equal([]byte(signature), []byte(signWebhook(secret, seconds, body))) {
		return nil, &WebhookError{Reason: "the signature does not match the body"}
	}

	var envelope struct {
		Event     string          `json:"event"`
		TenantID  string          `json:"tenant_id"`
		Timestamp int64           `json:"timestamp"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		// The signature already matched, so this is the gateway sending
		// something this version does not understand rather than an attack.
		return nil, &WebhookError{Reason: fmt.Sprintf("the body is signed but not a webhook envelope: %v", err)}
	}

	event := envelope.Event
	if event == "" {
		event = header.Get(WebhookEventHeader)
	}

	return &WebhookEvent{
		Event:      event,
		TenantID:   envelope.TenantID,
		Timestamp:  sent,
		DeliveryID: header.Get(WebhookDeliveryHeader),
		Data:       envelope.Data,
	}, nil
}

// signWebhook is the gateway's scheme: the timestamp, a dot, then the body,
// under HMAC-SHA256, hex encoded and prefixed with the algorithm.
func signWebhook(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	// hash.Hash.Write never returns an error, which is why nothing here checks
	// for one — and why this is two Writes rather than an Fprintf whose error
	// would have to be ignored explicitly.
	mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
