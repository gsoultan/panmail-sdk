package panmail_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	panmail "github.com/gsoultan/panmail-sdk"
)

const webhookSecret = "whsec_test"

// signLikeTheGateway is deliberately written out rather than calling the
// package under test, so this checks against panmail's own scheme rather than
// against the SDK agreeing with itself.
func signLikeTheGateway(t *testing.T, secret string, timestamp int64, body []byte) string {
	t.Helper()

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strconv.FormatInt(timestamp, 10) + "."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func webhookBody(t *testing.T) []byte {
	t.Helper()

	body, err := json.Marshal(map[string]any{
		"event":     "mail.bounced",
		"tenant_id": "3f1c2b7a-0000-4000-8000-000000000001",
		"timestamp": time.Now().Unix(),
		"data":      map[string]any{"messageId": "msg_01", "reason": "mailbox full"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func webhookHeaders(t *testing.T, secret string, sent time.Time, body []byte) http.Header {
	t.Helper()

	header := http.Header{}
	header.Set(panmail.WebhookEventHeader, "mail.bounced")
	header.Set(panmail.WebhookDeliveryHeader, "delivery-1")
	header.Set(panmail.WebhookTimestampHeader, strconv.FormatInt(sent.Unix(), 10))
	header.Set(panmail.WebhookSignatureHeader, signLikeTheGateway(t, secret, sent.Unix(), body))
	return header
}

func TestVerifyWebhookAcceptsWhatTheGatewaySent(t *testing.T) {
	body := webhookBody(t)
	now := time.Now()

	event, err := panmail.VerifyWebhook(webhookSecret, webhookHeaders(t, webhookSecret, now, body), body)
	if err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}

	if event.Event != "mail.bounced" {
		t.Errorf("Event = %q, want mail.bounced", event.Event)
	}
	if event.TenantID != "3f1c2b7a-0000-4000-8000-000000000001" {
		t.Errorf("TenantID = %q", event.TenantID)
	}
	if event.DeliveryID != "delivery-1" {
		t.Errorf("DeliveryID = %q, want delivery-1", event.DeliveryID)
	}
	if event.Timestamp.Unix() != now.Unix() {
		t.Errorf("Timestamp = %v, want %v", event.Timestamp, now)
	}

	var data struct{ Reason string }
	if err := json.Unmarshal(event.Data, &data); err != nil {
		t.Fatalf("Data is not json: %v", err)
	}
	if data.Reason != "mailbox full" {
		t.Errorf("Data.reason = %q, want mailbox full", data.Reason)
	}
}

// The gateway delivers a subscription with no secret unsigned rather than not
// delivering it, so an unsigned request is a real thing that arrives at a real
// endpoint. Accepting one would make verification decorative.
func TestVerifyWebhookRefusesAnUnsignedDelivery(t *testing.T) {
	body := webhookBody(t)

	header := http.Header{}
	header.Set(panmail.WebhookEventHeader, "mail.sent")
	header.Set(panmail.WebhookDeliveryHeader, "delivery-1")

	_, err := panmail.VerifyWebhook(webhookSecret, header, body)
	if !errors.Is(err, panmail.ErrWebhookUnsigned) {
		t.Fatalf("err = %v, want ErrWebhookUnsigned", err)
	}
}

func TestVerifyWebhookRefusesATamperedBody(t *testing.T) {
	body := webhookBody(t)
	now := time.Now()
	header := webhookHeaders(t, webhookSecret, now, body)

	// One byte, in the payload rather than the envelope: the kind of edit that
	// changes what a handler does without changing the shape of anything.
	tampered := append([]byte(nil), body...)
	tampered[len(tampered)-2] ^= 0x20

	if _, err := panmail.VerifyWebhook(webhookSecret, header, tampered); err == nil {
		t.Fatal("VerifyWebhook accepted a body that was not the one signed")
	}
}

func TestVerifyWebhookRefusesTheWrongSecret(t *testing.T) {
	body := webhookBody(t)
	now := time.Now()

	_, err := panmail.VerifyWebhook("whsec_someone_elses", webhookHeaders(t, webhookSecret, now, body), body)
	if err == nil {
		t.Fatal("VerifyWebhook accepted a signature made with another secret")
	}
}

// The window is what stops a captured request being replayed tomorrow, so it
// has to be checked in both directions: only checking the past would let a
// forged future timestamp keep a capture valid indefinitely.
func TestVerifyWebhookRefusesATimestampOutsideTheWindow(t *testing.T) {
	body := webhookBody(t)

	for name, sent := range map[string]time.Time{
		"long ago":   time.Now().Add(-30 * time.Minute),
		"the future": time.Now().Add(30 * time.Minute),
	} {
		t.Run(name, func(t *testing.T) {
			header := webhookHeaders(t, webhookSecret, sent, body)

			if _, err := panmail.VerifyWebhook(webhookSecret, header, body); err == nil {
				t.Fatal("VerifyWebhook accepted a timestamp outside the tolerance")
			}
			// The same delivery inside a wider window is fine, which shows the
			// refusal was the clock and not the signature.
			if _, err := panmail.VerifyWebhook(webhookSecret, header, body,
				panmail.WithWebhookTolerance(time.Hour)); err != nil {
				t.Fatalf("a wider tolerance still refused it: %v", err)
			}
		})
	}
}

func TestVerifyWebhookRefusesAnUnusableTimestamp(t *testing.T) {
	body := webhookBody(t)
	header := webhookHeaders(t, webhookSecret, time.Now(), body)
	header.Set(panmail.WebhookTimestampHeader, "not-a-number")

	if _, err := panmail.VerifyWebhook(webhookSecret, header, body); err == nil {
		t.Fatal("VerifyWebhook accepted a timestamp that is not a number")
	}
}

func TestVerifyWebhookNeedsASecret(t *testing.T) {
	body := webhookBody(t)

	if _, err := panmail.VerifyWebhook("", webhookHeaders(t, webhookSecret, time.Now(), body), body); err == nil {
		t.Fatal("VerifyWebhook verified something with no secret")
	}
}

// The signature covers the bytes that arrived, not the value they decode to.
//
// Worth a test because it is the mistake this API invites, and because it does
// not always fail: a Go map re-marshals with sorted keys, so a round trip can
// come out byte-identical by luck and appear to work right up until the day
// the body carries whitespace or a key order that does not survive. This uses
// a body that differs only in whitespace — the same value, decoded the same,
// and not the bytes that were signed.
func TestVerifyWebhookRefusesABodyThatWasReformatted(t *testing.T) {
	var value map[string]any
	if err := json.Unmarshal(webhookBody(t), &value); err != nil {
		t.Fatal(err)
	}
	arrived, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	header := webhookHeaders(t, webhookSecret, now, arrived)

	// As sent, indentation and all, it verifies.
	if _, err := panmail.VerifyWebhook(webhookSecret, header, arrived); err != nil {
		t.Fatalf("the body as it arrived did not verify: %v", err)
	}

	// Compacted — same JSON value, different bytes — it does not.
	compacted, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := panmail.VerifyWebhook(webhookSecret, header, compacted); err == nil {
		t.Fatal("VerifyWebhook accepted a reformatted body")
	}
}

// A refusal has to say which check failed, because the thing a receiver does
// with it is write it down: every one of these means the same thing to the
// caller — reject the request — so the message is the only thing that
// distinguishes a rotated secret from a slow clock at three in the morning.
func TestWebhookRefusalsSayWhichCheckFailed(t *testing.T) {
	body := webhookBody(t)

	cases := map[string]struct {
		secret string
		mutate func(http.Header)
		want   string
	}{
		"a clock out of step": {
			secret: webhookSecret,
			mutate: func(h http.Header) {
				sent := time.Now().Add(-time.Hour)
				h.Set(panmail.WebhookTimestampHeader, strconv.FormatInt(sent.Unix(), 10))
				h.Set(panmail.WebhookSignatureHeader, signLikeTheGateway(t, webhookSecret, sent.Unix(), body))
			},
			want: "tolerance",
		},
		"a rotated secret": {
			secret: "whsec_the_old_one",
			mutate: func(http.Header) {},
			want:   "signature does not match",
		},
		"a timestamp that is not a number": {
			secret: webhookSecret,
			mutate: func(h http.Header) { h.Set(panmail.WebhookTimestampHeader, "yesterday") },
			want:   "not a number",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			header := webhookHeaders(t, webhookSecret, time.Now(), body)
			tc.mutate(header)

			_, err := panmail.VerifyWebhook(tc.secret, header, body)
			if err == nil {
				t.Fatal("VerifyWebhook accepted it")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Error() = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// Signed by the right secret, but not the envelope this version knows. That is
// the gateway having moved on rather than an attacker, and it is worth saying
// so differently from a signature that did not match.
func TestVerifyWebhookSeparatesASignedBodyItCannotRead(t *testing.T) {
	body := []byte(`["signed", "but not an envelope"]`)
	now := time.Now()

	_, err := panmail.VerifyWebhook(webhookSecret, webhookHeaders(t, webhookSecret, now, body), body)
	if err == nil {
		t.Fatal("VerifyWebhook accepted a body that is not a webhook envelope")
	}
	if !strings.Contains(err.Error(), "signed but not a webhook envelope") {
		t.Errorf("Error() = %q, want it to distinguish this from a bad signature", err)
	}
}

// The event name is taken from the header when the envelope does not carry
// one, so a receiver can still route.
func TestVerifyWebhookFallsBackToTheEventHeader(t *testing.T) {
	body, err := json.Marshal(map[string]any{
		"tenant_id": "t-1",
		"timestamp": time.Now().Unix(),
		"data":      map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()

	event, err := panmail.VerifyWebhook(webhookSecret, webhookHeaders(t, webhookSecret, now, body), body)
	if err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}
	if event.Event != "mail.bounced" {
		t.Errorf("Event = %q, want it taken from the header", event.Event)
	}
}

// A tolerance of zero or less is a configuration mistake, not a request for no
// window at all — which would refuse everything the moment a clock drifted.
func TestANonPositiveToleranceFallsBackToTheDefault(t *testing.T) {
	body := webhookBody(t)
	header := webhookHeaders(t, webhookSecret, time.Now(), body)

	if _, err := panmail.VerifyWebhook(webhookSecret, header, body,
		panmail.WithWebhookTolerance(0)); err != nil {
		t.Fatalf("a zero tolerance refused a delivery sent just now: %v", err)
	}
}

// The vectors in testdata are computed independently — in Python by
// scripts/sync-webhook-vectors.py — and checked against the gateway's own
// sign(). Matching them is agreeing with something other than ourselves, and
// it is what stops the three clients drifting apart on the one calculation
// where disagreeing means silently rejecting real deliveries.
func TestWebhookSignaturesMatchTheSharedVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "webhook-signatures.json"))
	if err != nil {
		t.Fatalf("reading the shared vectors: %v", err)
	}
	var fixture struct {
		Vectors []struct {
			Name      string `json:"name"`
			Secret    string `json:"secret"`
			Timestamp int64  `json:"timestamp"`
			Body      string `json:"body"`
			Signature string `json:"signature"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parsing the shared vectors: %v", err)
	}
	if len(fixture.Vectors) == 0 {
		t.Fatal("the shared vectors are empty")
	}

	for _, v := range fixture.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			body := []byte(v.Body)
			header := http.Header{}
			header.Set(panmail.WebhookTimestampHeader, strconv.FormatInt(v.Timestamp, 10))
			header.Set(panmail.WebhookSignatureHeader, v.Signature)

			// The vectors use fixed timestamps far outside any sane window, so
			// the tolerance is widened to leave only the signature under test.
			_, err := panmail.VerifyWebhook(v.Secret, header, body,
				panmail.WithWebhookTolerance(100*365*24*time.Hour))

			// A body that is valid JSON but not an envelope still proves the
			// signature matched: that refusal happens after the HMAC check.
			var webhookErr *panmail.WebhookError
			if errors.As(err, &webhookErr) && strings.Contains(webhookErr.Reason, "not a webhook envelope") {
				return
			}
			if err != nil {
				t.Errorf("the shared vector did not verify: %v", err)
			}
		})
	}
}
