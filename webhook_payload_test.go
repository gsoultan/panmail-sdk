package panmail_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	panmail "github.com/gsoultan/panmail-sdk"
)

// gatewayBody is one body from testdata/webhook-payloads.json: bytes as the
// gateway builds them.
func gatewayBody(t *testing.T, name string) []byte {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "webhook-payloads.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Bodies map[string]string `json:"bodies"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	body, ok := fixture.Bodies[name]
	if !ok {
		t.Fatalf("no %s in the fixture", name)
	}
	return []byte(body)
}

// parse signs body as the gateway would, now, and parses it.
func parse(t *testing.T, body []byte) (*panmail.ParsedWebhook, error) {
	t.Helper()

	header := http.Header{}
	header.Set(panmail.WebhookDeliveryHeader, "delivery-1")
	now := time.Now()
	header.Set(panmail.WebhookTimestampHeader, strconv.FormatInt(now.Unix(), 10))
	header.Set(panmail.WebhookSignatureHeader, signLikeTheGateway(t, webhookSecret, now.Unix(), body))
	return panmail.ParseWebhook(webhookSecret, header, body)
}

func mustParse(t *testing.T, body []byte) *panmail.ParsedWebhook {
	t.Helper()
	parsed, err := parse(t, body)
	if err != nil {
		t.Fatalf("ParseWebhook: %v", err)
	}
	return parsed
}

// envelopeWith builds a body around data, for the cases the fixture does not
// cover.
func envelopeWith(t *testing.T, event panmail.TriggerEvent, data any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"event":     string(event),
		"tenant_id": "3f1c2b7a-0000-4000-8000-0000000000aa",
		"timestamp": time.Now().Unix(),
		"data":      data,
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestParseWebhookReadsAMailEvent(t *testing.T) {
	parsed := mustParse(t, gatewayBody(t, "mail_bounced"))

	if parsed.Webhook.Event != panmail.TriggerEventMailBounced || parsed.Webhook.DeliveryID != "delivery-1" {
		t.Errorf("envelope = %+v", parsed.Webhook)
	}
	got, ok := parsed.Payload.(*panmail.MailEvent)
	if !ok {
		t.Fatalf("Payload = %T, want *MailEvent", parsed.Payload)
	}
	want := &panmail.MailEvent{
		ID:           "0193b2f1-0000-7000-8000-0000000000e1",
		MessageID:    "0193b2f1-0000-7000-8000-000000000001",
		ProviderID:   "3f1c2b7a-0000-4000-8000-000000000001",
		ProviderName: "Production SES",
		Type:         panmail.StatusHardBounce,
		Recipient:    "gone@example.org",
		Subject:      "Your receipt",
		Timestamp:    time.Date(2026, 9, 1, 10, 0, 2, 123456789, time.UTC),
		Reason:       "550 5.1.1 mailbox unknown",
		Metadata:     map[string]any{"attempt": float64(1), "smtp_code": "550"},
	}
	if !got.Timestamp.Equal(want.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, want.Timestamp)
	}
	got.Timestamp = want.Timestamp
	if !reflect.DeepEqual(got, want) {
		t.Errorf("payload = %+v\nwant      %+v", got, want)
	}
}

func TestParseWebhookReadsAMailEventWithNothingOptional(t *testing.T) {
	got := mustParse(t, gatewayBody(t, "mail_opened")).Payload.(*panmail.MailEvent)

	if got.Type != panmail.StatusOpened || got.Recipient != "user@example.net" {
		t.Errorf("payload = %+v", got)
	}
	if got.Metadata != nil || got.Reason != "" || got.ProviderID != "" {
		t.Errorf("payload = %+v, want the absent fields empty", got)
	}
}

func TestParseWebhookReadsAHold(t *testing.T) {
	got, ok := mustParse(t, gatewayBody(t, "mail_held")).Payload.(*panmail.MailHeld)
	if !ok {
		t.Fatal("Payload is not a *MailHeld")
	}

	want := &panmail.MailHeld{
		ReviewID:        "0193b2f1-0000-7000-8000-0000000000q1",
		Direction:       "outbound",
		MessageID:       "0193b2f1-0000-7000-8000-000000000001",
		RuleID:          "0193b2f1-0000-7000-8000-0000000000r1",
		RuleName:        "Invoices need a look",
		From:            "billing@example.com",
		Recipients:      []string{"user@example.net", "cfo@example.net"},
		Subject:         "Invoice 1042",
		SizeBytes:       48213,
		AttachmentCount: 1,
		AttachmentNames: []string{"invoice-1042.pdf"},
		Matched: []panmail.MatchedCondition{
			{Field: "subject", Operator: "contains", Values: []string{"invoice"}},
			{Field: "header", Operator: "equals", Header: "X-Priority", Values: []string{"1"}},
			{Field: "size", Operator: "greater_than", Number: 40000},
		},
		HeldAt:    time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("payload = %+v\nwant      %+v", got, want)
	}
}

func TestParseWebhookReadsHowAHoldEnded(t *testing.T) {
	released := mustParse(t, gatewayBody(t, "mail_released")).Payload.(*panmail.MailHoldOutcome)
	if released.ReviewID != "0193b2f1-0000-7000-8000-0000000000q1" ||
		released.ReviewedBy != "reviewer@example.com" || released.Note != "expected invoice" ||
		!released.DecidedAt.Equal(time.Date(2026, 9, 1, 10, 0, 2, 123456789, time.UTC)) {
		t.Errorf("released = %+v", released)
	}

	expired := mustParse(t, gatewayBody(t, "mail_expired")).Payload.(*panmail.MailHoldOutcome)
	want := &panmail.MailHoldOutcome{
		ReviewID:   "0193b2f1-0000-7000-8000-0000000000q2",
		Direction:  "inbound",
		RuleName:   "Quarantine zips",
		From:       "someone@elsewhere.example",
		Recipients: []string{"support@example.com"},
		Subject:    "files",
		HeldAt:     time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC),
		DecidedAt:  time.Date(2026, 9, 8, 9, 0, 0, 0, time.UTC),
	}
	// An expiry has no reviewer, which is the point of it being its own event.
	if !reflect.DeepEqual(expired, want) {
		t.Errorf("expired = %+v\nwant      %+v", expired, want)
	}
}

func TestParseWebhookReadsAQuarantineRejectionAsAnOutcome(t *testing.T) {
	body := envelopeWith(t, panmail.TriggerEventMailQuarantineRejected, map[string]any{
		"id": "q3", "direction": "outbound", "rule_name": "r", "from": "a@example.com",
		"recipients": []string{"b@example.net"}, "subject": "s",
		"held_at": "2026-09-01T09:00:00Z", "decided_at": "2026-09-01T09:30:00Z", "reviewed_by": "rev@example.com",
	})

	got, ok := mustParse(t, body).Payload.(*panmail.MailHoldOutcome)
	if !ok || got.ReviewID != "q3" || got.ReviewedBy != "rev@example.com" {
		t.Errorf("Payload = %#v", got)
	}
}

func TestParseWebhookReadsInboundMail(t *testing.T) {
	got, ok := mustParse(t, gatewayBody(t, "mail_inbound")).Payload.(*panmail.InboundMail)
	if !ok {
		t.Fatal("Payload is not an *InboundMail")
	}

	want := &panmail.InboundMail{
		ID:        "0193b2f1-0000-7000-8000-0000000000i1",
		From:      "Customer <customer@elsewhere.example>",
		To:        []string{"support@example.com"},
		Subject:   "Help",
		HTML:      "<p>Hello</p>",
		Text:      "Hello",
		Timestamp: time.Date(2026, 9, 1, 10, 0, 2, 123456789, time.UTC),
		Headers:   map[string]string{"Message-Id": "<abc@elsewhere.example>"},
	}
	if !got.Timestamp.Equal(want.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, want.Timestamp)
	}
	got.Timestamp = want.Timestamp
	if !reflect.DeepEqual(got, want) {
		t.Errorf("payload = %+v\nwant      %+v", got, want)
	}
}

// encoding/json writes a protobuf Timestamp as {seconds, nanos}; protojson
// would write a string. Either reads.
func TestParseWebhookReadsAnInboundTimestampInEitherForm(t *testing.T) {
	cases := map[string]any{
		"object": map[string]any{"seconds": 1788256802, "nanos": 500000000},
		"string": "2026-09-01T10:00:02.5Z",
	}
	for name, timestamp := range cases {
		t.Run(name, func(t *testing.T) {
			body := envelopeWith(t, panmail.TriggerEventMailInbound, map[string]any{"id": "i1", "timestamp": timestamp})

			got := mustParse(t, body).Payload.(*panmail.InboundMail)
			if want := time.Date(2026, 9, 1, 10, 0, 2, 500000000, time.UTC); !got.Timestamp.Equal(want) {
				t.Errorf("Timestamp = %v, want %v", got.Timestamp, want)
			}
		})
	}
}

func TestParseWebhookNamesEveryEventTypeNumber(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "event-types.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Numbers map[string]string `json:"numbers"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Numbers) == 0 {
		t.Fatal("the fixture has no numbers")
	}

	for number, name := range fixture.Numbers {
		n, err := strconv.Atoi(number)
		if err != nil {
			t.Fatal(err)
		}
		body := envelopeWith(t, panmail.TriggerEventMailSent, map[string]any{"id": "e", "type": n})

		got := mustParse(t, body).Payload.(*panmail.MailEvent)
		if string(got.Type) != name {
			t.Errorf("type %d read as %q, want %q", n, got.Type, name)
		}
	}
}

func TestParseWebhookKeepsATypeItHasNoNameFor(t *testing.T) {
	cases := map[string]struct {
		wire any
		want panmail.Status
	}{
		"an unknown number": {99, "99"},
		"a name":            {"EMAIL_EVENT_TYPE_DELIVERED", panmail.StatusDelivered},
		"absent":            {nil, panmail.StatusUnspecified},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			data := map[string]any{"id": "e"}
			if c.wire != nil {
				data["type"] = c.wire
			}
			got := mustParse(t, envelopeWith(t, panmail.TriggerEventMailDelivered, data)).Payload.(*panmail.MailEvent)
			if got.Type != c.want {
				t.Errorf("Type = %q, want %q", got.Type, c.want)
			}
		})
	}
}

// The gateway adds events. One this version has no type for is verified and
// handed back as it came, not refused into a retry loop.
func TestParseWebhookPreservesAnEventItDoesNotKnow(t *testing.T) {
	body := envelopeWith(t, "WEBHOOK_TRIGGER_EVENT_MAIL_TELEPORTED", map[string]any{"where": "the moon"})

	parsed := mustParse(t, body)

	if parsed.Payload != nil {
		t.Errorf("Payload = %#v, want none", parsed.Payload)
	}
	if parsed.Webhook.Event != "WEBHOOK_TRIGGER_EVENT_MAIL_TELEPORTED" {
		t.Errorf("Event = %q", parsed.Webhook.Event)
	}
	if !strings.Contains(string(parsed.Webhook.Data), "the moon") {
		t.Errorf("Data = %s, want the payload as it came", parsed.Webhook.Data)
	}
}

func TestParseWebhookVerifiesFirst(t *testing.T) {
	body := gatewayBody(t, "mail_bounced")
	header := http.Header{}
	header.Set(panmail.WebhookTimestampHeader, strconv.FormatInt(time.Now().Unix(), 10))
	header.Set(panmail.WebhookSignatureHeader, signLikeTheGateway(t, "whsec_someone_else", time.Now().Unix(), body))

	parsed, err := panmail.ParseWebhook(webhookSecret, header, body)

	var refused *panmail.WebhookError
	if !errors.As(err, &refused) || parsed != nil {
		t.Fatalf("ParseWebhook = %v, %v; want a WebhookError and nothing parsed", parsed, err)
	}
	if _, err := panmail.ParseWebhook(webhookSecret, http.Header{}, body); !errors.Is(err, panmail.ErrWebhookUnsigned) {
		t.Errorf("unsigned: err = %v, want ErrWebhookUnsigned", err)
	}
}

// Signed, so not an attack — but a payload decoded halfway would hand back
// zero values nobody can tell from real ones.
func TestParseWebhookRefusesAKnownEventInAShapeItCannotRead(t *testing.T) {
	cases := map[string]struct {
		event panmail.TriggerEvent
		data  any
	}{
		"no data":                 {panmail.TriggerEventMailBounced, nil},
		"data that is a string":   {panmail.TriggerEventMailSent, "sent"},
		"recipients not a list":   {panmail.TriggerEventMailHeld, map[string]any{"recipients": "a@example.net"}},
		"a size that is text":     {panmail.TriggerEventMailHeld, map[string]any{"size_bytes": "big"}},
		"a time that is not one":  {panmail.TriggerEventMailReleased, map[string]any{"decided_at": "yesterday"}},
		"a type that is a list":   {panmail.TriggerEventMailOpened, map[string]any{"type": []int{3}}},
		"metadata not an object":  {panmail.TriggerEventMailClicked, map[string]any{"metadata": "x"}},
		"nanos out of range":      {panmail.TriggerEventMailInbound, map[string]any{"timestamp": map[string]any{"seconds": 1, "nanos": 1e9}}},
		"a timestamp that is a 1": {panmail.TriggerEventMailInbound, map[string]any{"timestamp": 1}},
		"headers not text":        {panmail.TriggerEventMailInbound, map[string]any{"headers": map[string]any{"X": 1}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parse(t, envelopeWith(t, c.event, c.data))

			var refused *panmail.WebhookError
			if !errors.As(err, &refused) {
				t.Fatalf("err = %v, want a WebhookError", err)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("its %s data", c.event)) {
				t.Errorf("err = %q, want it to name the event", err)
			}
		})
	}
}
