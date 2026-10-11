package panmail_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	panmail "github.com/gsoultan/panmail-sdk"
)

// keyed is hello with an idempotency key.
func keyed() panmail.Message {
	msg := hello()
	msg.IdempotencyKey = "order-1042/receipt"
	return msg
}

// drop ends the connection without an answer: the one outcome where the client
// cannot know whether the gateway took the message.
func drop() {
	panic(http.ErrAbortHandler)
}

// flaky is a gateway for tests that drop connections. It keeps its own count
// behind a mutex because a dropped connection gives the race detector no
// happens-before edge between the handler's write and the test's read, which
// an ordinary response happens to.
type flaky struct {
	server *httptest.Server
	mu     sync.Mutex
	keys   []any
}

func flakyGateway(t *testing.T, handler func(w http.ResponseWriter, call int)) *flaky {
	t.Helper()

	f := &flaky{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("gateway got a body that is not json: %v", err)
		}

		f.mu.Lock()
		f.keys = append(f.keys, body["idempotencyKey"])
		call := len(f.keys)
		f.mu.Unlock()

		handler(w, call)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *flaky) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.keys)
}

func (f *flaky) sentKeys() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]any(nil), f.keys...)
}

func (f *flaky) client(t *testing.T, opts ...panmail.Option) *panmail.Client {
	t.Helper()
	c, err := panmail.New(f.server.URL, "test-key", opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestSendCarriesTheIdempotencyKey(t *testing.T) {
	g := accepts(t)

	if _, err := client(t, g).Send(context.Background(), keyed()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := g.bodies[0]["idempotencyKey"]; got != "order-1042/receipt" {
		t.Errorf("idempotencyKey = %v, want order-1042/receipt", got)
	}
	// The field, never the header: net/http resends a request carrying the
	// header on its own, behind any count this client keeps.
	if got := g.requests[0].Header.Get("Idempotency-Key"); got != "" {
		t.Errorf("Idempotency-Key header = %q, want it absent", got)
	}
}

func TestSendLeavesOutAnAbsentIdempotencyKey(t *testing.T) {
	g := accepts(t)

	if _, err := client(t, g).Send(context.Background(), hello()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if _, present := g.bodies[0]["idempotencyKey"]; present {
		t.Error("an empty idempotency key was sent; it should be left out")
	}
}

func TestSendRejectsAnUnusableIdempotencyKey(t *testing.T) {
	cases := map[string]string{
		"too long":  strings.Repeat("k", 256),
		"a newline": "order-1\n",
		"not ascii": "commande-é",
		"a control": "order\x00",
		"delete":    "order\x7f",
	}

	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			g := accepts(t)
			msg := hello()
			msg.IdempotencyKey = key

			if _, err := client(t, g).Send(context.Background(), msg); err == nil {
				t.Fatal("Send accepted a key the gateway refuses")
			}
			if g.calls != 0 {
				t.Errorf("the gateway was called %d times, want 0", g.calls)
			}
		})
	}
}

func TestSendAcceptsTheLongestIdempotencyKey(t *testing.T) {
	g := accepts(t)
	msg := hello()
	msg.IdempotencyKey = strings.Repeat("k", 254) + " "

	if _, err := client(t, g).Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
}

func TestAReusedIdempotencyKeyIsItsOwnRefusal(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		refuse(w, http.StatusConflict, "already_exists", "idempotency key reused with a different request")
	})

	_, err := client(t, g, panmail.WithTransportRetries(3), panmail.WithRateLimitRetries(3)).
		Send(context.Background(), keyed())

	var conflict *panmail.IdempotencyKeyConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want an IdempotencyKeyConflictError", err)
	}
	var apiErr *panmail.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "already_exists" {
		t.Errorf("err = %v, want it to carry the already_exists APIError", err)
	}
	if !strings.Contains(err.Error(), "reused with a different request") {
		t.Errorf("err = %q, want the gateway's reason in it", err)
	}
	// A refusal is a known outcome, and this one is answered the same way
	// until the key changes.
	if g.calls != 1 {
		t.Errorf("the gateway was called %d times, want 1", g.calls)
	}
}

// already_exists means a reused key on a send and nothing else; on a listing
// it would be a lie.
func TestAlreadyExistsIsNotAConflictOffTheSendPath(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		refuse(w, http.StatusConflict, "already_exists", "something else")
	})

	_, err := client(t, g).ListProviders(context.Background(), panmail.ProviderFilter{})

	var conflict *panmail.IdempotencyKeyConflictError
	if errors.As(err, &conflict) {
		t.Fatalf("err = %v, a listing has no idempotency key to conflict", err)
	}
	var apiErr *panmail.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "already_exists" {
		t.Errorf("err = %v, want a plain APIError with the code", err)
	}
}

func TestSendRetriesAnUnknownOutcomeWhenKeyedAndAsked(t *testing.T) {
	g := flakyGateway(t, func(w http.ResponseWriter, call int) {
		if call == 1 {
			drop()
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"messageId": "msg_first",
			"status":    "EMAIL_EVENT_TYPE_PENDING",
		})
	})

	result, err := g.client(t, panmail.WithTransportRetries(1)).Send(context.Background(), keyed())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if result.MessageID != "msg_first" {
		t.Errorf("MessageID = %q, want msg_first", result.MessageID)
	}
	if g.calls() != 2 {
		t.Errorf("the gateway was called %d times, want 2", g.calls())
	}
	// The repeat carries the same key, or it is a second message.
	for i, key := range g.sentKeys() {
		if key != "order-1042/receipt" {
			t.Errorf("attempt %d carried idempotencyKey %v", i+1, key)
		}
	}
}

func TestSendDoesNotRetryAnUnknownOutcomeWithoutAKey(t *testing.T) {
	g := flakyGateway(t, func(http.ResponseWriter, int) { drop() })

	_, err := g.client(t, panmail.WithTransportRetries(3)).Send(context.Background(), hello())
	if err == nil {
		t.Fatal("Send returned no error")
	}
	if g.calls() != 1 {
		t.Errorf("the gateway was called %d times, want 1: without a key a repeat may be a second message", g.calls())
	}
}

func TestSendDoesNotRetryAnUnknownOutcomeByDefault(t *testing.T) {
	g := flakyGateway(t, func(http.ResponseWriter, int) { drop() })

	_, err := g.client(t).Send(context.Background(), keyed())
	if err == nil {
		t.Fatal("Send returned no error")
	}
	if g.calls() != 1 {
		t.Errorf("the gateway was called %d times, want 1", g.calls())
	}
}

func TestSendGivesUpOnAnUnknownOutcomeAfterTheConfiguredRetries(t *testing.T) {
	g := flakyGateway(t, func(http.ResponseWriter, int) { drop() })

	_, err := g.client(t, panmail.WithTransportRetries(2)).Send(context.Background(), keyed())
	if err == nil {
		t.Fatal("Send returned no error")
	}
	if !strings.Contains(err.Error(), "did not complete") {
		t.Errorf("err = %q, want the transport error", err)
	}
	if g.calls() != 3 {
		t.Errorf("the gateway was called %d times, want 3", g.calls())
	}
}

func TestSendRetriesAnAbortedKeyedSend(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, call int) {
		if call == 1 {
			refuse(w, http.StatusConflict, "aborted", "a request with this idempotency key is still being processed; retry")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"messageId": "msg_winner",
			"status":    "EMAIL_EVENT_TYPE_PENDING",
		})
	})

	result, err := client(t, g, panmail.WithTransportRetries(1)).Send(context.Background(), keyed())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if result.MessageID != "msg_winner" {
		t.Errorf("MessageID = %q, want msg_winner", result.MessageID)
	}
	if g.calls != 2 {
		t.Errorf("the gateway was called %d times, want 2", g.calls)
	}
}

func TestSendDoesNotRetryAnAbortedSendWithoutAKey(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		refuse(w, http.StatusConflict, "aborted", "retry")
	})

	_, err := client(t, g, panmail.WithTransportRetries(3)).Send(context.Background(), hello())

	var apiErr *panmail.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "aborted" {
		t.Fatalf("err = %v, want an APIError with code aborted", err)
	}
	if g.calls != 1 {
		t.Errorf("the gateway was called %d times, want 1", g.calls)
	}
}

// A refusal is a known outcome even with a key: unknown is a failure the
// gateway reported, not a request whose fate is a mystery.
func TestSendDoesNotRetryARefusalAsAnUnknownOutcome(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		refuse(w, http.StatusInternalServerError, "unknown", "storage failed")
	})

	_, err := client(t, g, panmail.WithTransportRetries(3)).Send(context.Background(), keyed())
	if err == nil {
		t.Fatal("Send returned no error")
	}
	if g.calls != 1 {
		t.Errorf("the gateway was called %d times, want 1", g.calls)
	}
}

func TestSendDoesNotRetryARedirectAsAnUnknownOutcome(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Location", "https://elsewhere.example/")
		w.WriteHeader(http.StatusTemporaryRedirect)
	})

	_, err := client(t, g, panmail.WithTransportRetries(3)).Send(context.Background(), keyed())
	if err == nil {
		t.Fatal("Send returned no error")
	}
	if g.calls != 1 {
		t.Errorf("the gateway was called %d times, want 1", g.calls)
	}
}

func TestSendStopsRetryingAnUnknownOutcomeAtTheDeadline(t *testing.T) {
	g := flakyGateway(t, func(http.ResponseWriter, int) { drop() })

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := g.client(t, panmail.WithTransportRetries(100)).Send(ctx, keyed())
	if err == nil {
		t.Fatal("Send returned no error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Send kept going for %s, want it to stop at the context deadline", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want it to say the deadline ended it", err)
	}
}

func TestNegativeTransportRetriesMeansNone(t *testing.T) {
	g := flakyGateway(t, func(http.ResponseWriter, int) { drop() })

	if _, err := g.client(t, panmail.WithTransportRetries(-5)).Send(context.Background(), keyed()); err == nil {
		t.Fatal("Send returned no error")
	}
	if g.calls() != 1 {
		t.Errorf("the gateway was called %d times, want 1", g.calls())
	}
}

// Only a keyed send repeats an unknown outcome. A listing reports one as the
// error it is, and calling again is the caller's decision.
func TestAnUnknownOutcomeOffTheSendPathIsReportedAsItIs(t *testing.T) {
	g := flakyGateway(t, func(http.ResponseWriter, int) { drop() })

	_, err := g.client(t, panmail.WithTransportRetries(3)).ListProviders(context.Background(), panmail.ProviderFilter{})
	if err == nil {
		t.Fatal("ListProviders returned no error")
	}
	if !strings.Contains(err.Error(), "did not complete") {
		t.Errorf("err = %q, want the transport error", err)
	}
	if g.calls() != 1 {
		t.Errorf("the gateway was called %d times, want 1", g.calls())
	}
}
