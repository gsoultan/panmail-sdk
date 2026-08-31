package panmail_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	panmail "github.com/gsoultan/panmail-sdk"
)

// gateway is a stand-in for the real thing that records what it was sent and
// answers with whatever the test asked for.
type gateway struct {
	server   *httptest.Server
	requests []*http.Request
	bodies   []map[string]any
	calls    int
}

// respond builds a gateway whose handler is the given function. The handler
// receives the call count, one-based, so a test can answer differently on a
// retry.
func respond(t *testing.T, handler func(w http.ResponseWriter, call int)) *gateway {
	t.Helper()

	g := &gateway{}
	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("gateway got a body that is not json: %v", err)
		}

		g.calls++
		g.requests = append(g.requests, r)
		g.bodies = append(g.bodies, body)

		handler(w, g.calls)
	}))
	t.Cleanup(g.server.Close)
	return g
}

// accepts is the ordinary case: the gateway queues the message.
func accepts(t *testing.T) *gateway {
	t.Helper()
	return respond(t, func(w http.ResponseWriter, _ int) {
		writeJSON(w, http.StatusOK, map[string]any{
			"messageId": "msg_01",
			"status":    "EMAIL_EVENT_TYPE_PENDING",
		})
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// refuse writes a Connect error envelope with the given code.
func refuse(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"code": code, "message": message})
}

func hello() panmail.Message {
	return panmail.Message{
		ProviderID: "3f1c2b7a-0000-4000-8000-000000000001",
		From:       "app@example.com",
		To:         []string{"user@example.net"},
		Subject:    "Hello",
		Text:       "hi there",
	}
}

func client(t *testing.T, g *gateway, opts ...panmail.Option) *panmail.Client {
	t.Helper()
	c, err := panmail.New(g.server.URL, "test-key", opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestSend(t *testing.T) {
	g := accepts(t)

	result, err := client(t, g).Send(context.Background(), hello())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if result.MessageID != "msg_01" {
		t.Errorf("MessageID = %q, want msg_01", result.MessageID)
	}
	if result.Status != panmail.StatusPending {
		t.Errorf("Status = %q, want %q", result.Status, panmail.StatusPending)
	}
}

// The key goes in X-API-Key. Authorization carries a dashboard session, and a
// key sent there is rejected as a malformed session rather than as a bad key.
func TestSendAuthenticatesWithTheApiKeyHeader(t *testing.T) {
	g := accepts(t)

	if _, err := client(t, g).Send(context.Background(), hello()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := g.requests[0].Header.Get("X-API-Key"); got != "test-key" {
		t.Errorf("X-API-Key = %q, want test-key", got)
	}
	if got := g.requests[0].Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want it left unset", got)
	}
	if got := g.requests[0].Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

func TestSendPostsToTheConnectProcedure(t *testing.T) {
	g := accepts(t)

	if _, err := client(t, g).Send(context.Background(), hello()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := g.requests[0].URL.Path; got != "/panmail.v1.EmailService/SendEmail" {
		t.Errorf("path = %q, want /panmail.v1.EmailService/SendEmail", got)
	}
	if got := g.requests[0].Method; got != http.MethodPost {
		t.Errorf("method = %q, want POST", got)
	}
}

func TestSendCarriesTheWholeMessage(t *testing.T) {
	g := accepts(t)

	msg := hello()
	msg.Cc = []string{"cc@example.net"}
	msg.Bcc = []string{"bcc@example.net"}
	msg.HTML = "<p>hi there</p>"
	msg.TemplateID = "welcome"
	msg.TemplateData = map[string]any{"name": "Ada", "count": 2}
	msg.Attachments = []panmail.Attachment{{
		Filename: "receipt.pdf",
		Content:  []byte("%PDF-1.4"),
	}}

	if _, err := client(t, g).Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	body := g.bodies[0]
	for field, want := range map[string]string{
		"providerId": msg.ProviderID,
		"from":       msg.From,
		"subject":    msg.Subject,
		"bodyHtml":   msg.HTML,
		"bodyText":   msg.Text,
		"templateId": msg.TemplateID,
	} {
		if got, _ := body[field].(string); got != want {
			t.Errorf("%s = %q, want %q", field, got, want)
		}
	}

	for field, want := range map[string]string{
		"to":  "user@example.net",
		"cc":  "cc@example.net",
		"bcc": "bcc@example.net",
	} {
		list, ok := body[field].([]any)
		if !ok || len(list) != 1 || list[0] != want {
			t.Errorf("%s = %v, want [%s]", field, body[field], want)
		}
	}

	data, ok := body["templateData"].(map[string]any)
	if !ok || data["name"] != "Ada" {
		t.Errorf("templateData = %v, want it to carry name=Ada", body["templateData"])
	}
}

// A bytes field is base64 in protobuf JSON, and the gateway decodes it as
// such. Sending the raw bytes instead would arrive as mojibake.
func TestSendBase64EncodesAttachmentContent(t *testing.T) {
	g := accepts(t)

	msg := hello()
	msg.Attachments = []panmail.Attachment{{
		Filename: "receipt.pdf",
		Content:  []byte("%PDF-1.4"),
	}}

	if _, err := client(t, g).Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	attachments, ok := g.bodies[0]["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("attachments = %v, want one", g.bodies[0]["attachments"])
	}
	attachment, _ := attachments[0].(map[string]any)

	encoded, _ := attachment["content"].(string)
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("content is not base64: %v", err)
	}
	if string(decoded) != "%PDF-1.4" {
		t.Errorf("decoded content = %q, want %%PDF-1.4", decoded)
	}

	// Guessed from the extension, because a missing type makes every client
	// offer a download rather than display the file.
	if got, _ := attachment["contentType"].(string); got != "application/pdf" {
		t.Errorf("contentType = %q, want application/pdf", got)
	}
}

// A text attachment with no declared encoding is one the mail client has to
// guess at, and a UTF-8 CSV guessed as latin-1 is mojibake in a spreadsheet.
// Go's mime package appends the charset; the other three clients hard-code it
// so that all four send the same header for the same file.
func TestTextAttachmentsDeclareTheirCharset(t *testing.T) {
	g := accepts(t)

	msg := hello()
	msg.Attachments = []panmail.Attachment{{Filename: "report.csv", Content: []byte("a,b\n1,2")}}

	if _, err := client(t, g).Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}

	attachments, _ := g.bodies[0]["attachments"].([]any)
	attachment, _ := attachments[0].(map[string]any)
	if got, _ := attachment["contentType"].(string); got != "text/csv; charset=utf-8" {
		t.Errorf("contentType = %q, want text/csv; charset=utf-8", got)
	}
}

func TestSendRejectsBadMessagesWithoutCallingTheGateway(t *testing.T) {
	g := accepts(t)
	c := client(t, g)

	cases := map[string]panmail.Message{
		"no provider": {From: "a@example.com", To: []string{"b@example.net"}, Text: "x"},
		"no from":     {ProviderID: "p", To: []string{"b@example.net"}, Text: "x"},
		"no recipients": {
			ProviderID: "p", From: "a@example.com", Text: "x",
		},
		"no body or template": {
			ProviderID: "p", From: "a@example.com", To: []string{"b@example.net"},
		},
		"attachment without a filename": {
			ProviderID: "p", From: "a@example.com", To: []string{"b@example.net"}, Text: "x",
			Attachments: []panmail.Attachment{{Content: []byte("x")}},
		},
	}

	for name, msg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := c.Send(context.Background(), msg); err == nil {
				t.Fatal("Send accepted a message it should have refused")
			}
		})
	}

	if g.calls != 0 {
		t.Errorf("the gateway was called %d times, want 0", g.calls)
	}
}

func TestSendReportsNoRecipientsAsSuchToCallersThatCheck(t *testing.T) {
	g := accepts(t)

	msg := hello()
	msg.To = nil

	_, err := client(t, g).Send(context.Background(), msg)
	if !errors.Is(err, panmail.ErrNoRecipients) {
		t.Errorf("err = %v, want it to wrap ErrNoRecipients", err)
	}
}

// Both capacity refusals are resource_exhausted. Retry-After is the only thing
// separating them, so this is the test that fails if the gateway stops sending
// it.
func TestSendClassifiesRefusals(t *testing.T) {
	t.Run("a rate limit carries its delay", func(t *testing.T) {
		g := respond(t, func(w http.ResponseWriter, _ int) {
			w.Header().Set("Retry-After", "7")
			refuse(w, http.StatusTooManyRequests, "resource_exhausted", "over the send rate")
		})

		_, err := client(t, g).Send(context.Background(), hello())

		var limited *panmail.RateLimitedError
		if !errors.As(err, &limited) {
			t.Fatalf("err = %v, want a RateLimitedError", err)
		}
		if limited.RetryAfter != 7*time.Second {
			t.Errorf("RetryAfter = %s, want 7s", limited.RetryAfter)
		}
	})

	t.Run("the same code without a delay is a full queue", func(t *testing.T) {
		g := respond(t, func(w http.ResponseWriter, _ int) {
			refuse(w, http.StatusTooManyRequests, "resource_exhausted", "queue too deep")
		})

		_, err := client(t, g).Send(context.Background(), hello())

		var full *panmail.BacklogFullError
		if !errors.As(err, &full) {
			t.Fatalf("err = %v, want a BacklogFullError", err)
		}
	})

	t.Run("an unreadable delay is still a rate limit", func(t *testing.T) {
		g := respond(t, func(w http.ResponseWriter, _ int) {
			w.Header().Set("Retry-After", "Wed, 21 Oct 2026 07:28:00 GMT")
			refuse(w, http.StatusTooManyRequests, "resource_exhausted", "over the send rate")
		})

		_, err := client(t, g).Send(context.Background(), hello())

		var limited *panmail.RateLimitedError
		if !errors.As(err, &limited) {
			t.Fatalf("err = %v, want a RateLimitedError with no usable delay", err)
		}
		if limited.RetryAfter != 0 {
			t.Errorf("RetryAfter = %s, want 0", limited.RetryAfter)
		}
	})

	for name, code := range map[string]string{
		"unauthenticated":   "unauthenticated",
		"permission denied": "permission_denied",
	} {
		t.Run(name+" is an auth error", func(t *testing.T) {
			g := respond(t, func(w http.ResponseWriter, _ int) {
				refuse(w, http.StatusUnauthorized, code, "no")
			})

			_, err := client(t, g).Send(context.Background(), hello())

			var authErr *panmail.AuthError
			if !errors.As(err, &authErr) {
				t.Fatalf("err = %v, want an AuthError", err)
			}
		})
	}

	t.Run("anything else keeps its code", func(t *testing.T) {
		g := respond(t, func(w http.ResponseWriter, _ int) {
			refuse(w, http.StatusBadRequest, "invalid_argument", "provider_id is not a uuid")
		})

		_, err := client(t, g).Send(context.Background(), hello())

		var apiErr *panmail.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("err = %v, want an APIError", err)
		}
		if apiErr.Code != "invalid_argument" {
			t.Errorf("Code = %q, want invalid_argument", apiErr.Code)
		}
		if !strings.Contains(apiErr.Message, "uuid") {
			t.Errorf("Message = %q, want the gateway's explanation", apiErr.Message)
		}
	})

	// A proxy in front of the gateway answers with its own error page. The
	// status is still worth classifying.
	t.Run("a body that is not an envelope falls back to the status", func(t *testing.T) {
		g := respond(t, func(w http.ResponseWriter, _ int) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("<html>401 Unauthorized</html>"))
		})

		_, err := client(t, g).Send(context.Background(), hello())

		var authErr *panmail.AuthError
		if !errors.As(err, &authErr) {
			t.Fatalf("err = %v, want an AuthError", err)
		}
	})
}

func TestSendDoesNotRetryByDefault(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Retry-After", "1")
		refuse(w, http.StatusTooManyRequests, "resource_exhausted", "over the send rate")
	})

	if _, err := client(t, g).Send(context.Background(), hello()); err == nil {
		t.Fatal("Send returned no error")
	}

	if g.calls != 1 {
		t.Errorf("the gateway was called %d times, want 1", g.calls)
	}
}

func TestSendWaitsOutARateLimitWhenAsked(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, call int) {
		if call == 1 {
			w.Header().Set("Retry-After", "1")
			refuse(w, http.StatusTooManyRequests, "resource_exhausted", "over the send rate")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"messageId": "msg_after_wait",
			"status":    "EMAIL_EVENT_TYPE_PENDING",
		})
	})

	result, err := client(t, g, panmail.WithRateLimitRetries(1)).Send(context.Background(), hello())
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if result.MessageID != "msg_after_wait" {
		t.Errorf("MessageID = %q, want msg_after_wait", result.MessageID)
	}
	if g.calls != 2 {
		t.Errorf("the gateway was called %d times, want 2", g.calls)
	}
}

func TestSendGivesUpAfterTheConfiguredRetries(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Retry-After", "1")
		refuse(w, http.StatusTooManyRequests, "resource_exhausted", "over the send rate")
	})

	_, err := client(t, g, panmail.WithRateLimitRetries(2)).Send(context.Background(), hello())

	var limited *panmail.RateLimitedError
	if !errors.As(err, &limited) {
		t.Fatalf("err = %v, want the refusal to survive", err)
	}
	// The first attempt is not a retry: two retries is three calls.
	if g.calls != 3 {
		t.Errorf("the gateway was called %d times, want 3", g.calls)
	}
}

// A full queue is not something waiting on a timer fixes, and retrying makes
// the wait longer for everything already queued.
func TestSendDoesNotRetryAFullQueue(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		refuse(w, http.StatusTooManyRequests, "resource_exhausted", "queue too deep")
	})

	_, err := client(t, g, panmail.WithRateLimitRetries(3)).Send(context.Background(), hello())

	var full *panmail.BacklogFullError
	if !errors.As(err, &full) {
		t.Fatalf("err = %v, want a BacklogFullError", err)
	}
	if g.calls != 1 {
		t.Errorf("the gateway was called %d times, want 1", g.calls)
	}
}

func TestSendStopsWaitingWhenTheCallerGivesUp(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Retry-After", "30")
		refuse(w, http.StatusTooManyRequests, "resource_exhausted", "over the send rate")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := client(t, g, panmail.WithRateLimitRetries(1)).Send(ctx, hello())
	if err == nil {
		t.Fatal("Send returned no error")
	}

	// The caller's deadline outranks the gateway's 30-second suggestion.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Send waited %s, want it to stop at the context deadline", elapsed)
	}
	// The refusal is still reported, not just the cancellation.
	var limited *panmail.RateLimitedError
	if !errors.As(err, &limited) {
		t.Errorf("err = %v, want it to still carry the refusal", err)
	}
}

func TestNewValidatesItsInputs(t *testing.T) {
	cases := map[string]struct{ baseURL, apiKey string }{
		"no base url":  {"", "key"},
		"no scheme":    {"mail.example.com", "key"},
		"wrong scheme": {"ftp://mail.example.com", "key"},
		"no host":      {"https://", "key"},
		"no api key":   {"https://mail.example.com", ""},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := panmail.New(c.baseURL, c.apiKey); err == nil {
				t.Fatal("New accepted input it should have refused")
			}
		})
	}
}

func TestNewAcceptsABaseURLWithATrailingSlash(t *testing.T) {
	g := accepts(t)

	c, err := panmail.New(g.server.URL+"/", "test-key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Send(context.Background(), hello()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := g.requests[0].URL.Path; got != "/panmail.v1.EmailService/SendEmail" {
		t.Errorf("path = %q, want no doubled slash", got)
	}
}

// A transport error is the one outcome where the client cannot know whether
// the gateway took the message, so it is never repeated.
func TestSendDoesNotRetryATransportError(t *testing.T) {
	g := accepts(t)
	c := client(t, g, panmail.WithRateLimitRetries(3))
	g.server.Close()

	if _, err := c.Send(context.Background(), hello()); err == nil {
		t.Fatal("Send returned no error")
	}
	if g.calls != 0 {
		t.Errorf("the gateway was called %d times, want 0", g.calls)
	}
}

func TestWithHeaderCannotOverrideTheApiKey(t *testing.T) {
	g := accepts(t)

	c := client(t, g, panmail.WithHeader("X-API-Key", "smuggled"), panmail.WithHeader("X-Trace", "abc"))
	if _, err := c.Send(context.Background(), hello()); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := g.requests[0].Header.Get("X-API-Key"); got != "test-key" {
		t.Errorf("X-API-Key = %q, want the key New was given", got)
	}
	if got := g.requests[0].Header.Get("X-Trace"); got != "abc" {
		t.Errorf("X-Trace = %q, want abc", got)
	}
}

// A redirect is the one response that can hand the api key to a host the
// caller never chose: X-API-Key is not on net/http's list of credentials to
// drop when a redirect crosses hosts, because that list knows only about
// Authorization and Cookie. Following one is therefore a key disclosure.
func TestSendRefusesToFollowARedirect(t *testing.T) {
	var leaked string
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("X-API-Key")
		writeJSON(w, http.StatusOK, map[string]any{"messageId": "leaked"})
	}))
	defer elsewhere.Close()

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/steal", http.StatusTemporaryRedirect)
	}))
	defer redirecting.Close()

	c, err := panmail.New(redirecting.URL, "secret-key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := c.Send(context.Background(), hello()); err == nil {
		t.Fatal("Send followed the redirect instead of refusing it")
	}
	if leaked != "" {
		t.Fatalf("the api key reached the redirect target: %q", leaked)
	}
}

// contentTypeFixture is the mapping all four clients share. Reading it here
// rather than repeating it means a change to one implementation that the
// others do not follow turns three languages red.
func contentTypeFixture(t *testing.T) map[string]string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("testdata", "content-types.json"))
	if err != nil {
		t.Fatalf("reading the shared fixture: %v", err)
	}
	var fixture struct {
		Types map[string]string `json:"types"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parsing the shared fixture: %v", err)
	}
	if len(fixture.Types) == 0 {
		t.Fatal("the shared fixture is empty")
	}
	return fixture.Types
}

// Go guesses from mime.TypeByExtension rather than a table of its own, so this
// also pins that every extension the fixture names is one of Go's builtins. An
// extension that is not would make the answer depend on the host's mime
// database, and so on which machine the client happens to run.
func TestAttachmentContentTypesMatchTheSharedFixture(t *testing.T) {
	for extension, want := range contentTypeFixture(t) {
		t.Run(extension, func(t *testing.T) {
			g := accepts(t)

			message := hello()
			message.Attachments = []panmail.Attachment{
				{Filename: "attachment." + extension, Content: []byte("x")},
			}
			if _, err := client(t, g).Send(context.Background(), message); err != nil {
				t.Fatalf("Send: %v", err)
			}

			attachments, ok := g.bodies[0]["attachments"].([]any)
			if !ok || len(attachments) != 1 {
				t.Fatalf("attachments = %#v", g.bodies[0]["attachments"])
			}
			sent, ok := attachments[0].(map[string]any)
			if !ok {
				t.Fatalf("attachment = %#v", attachments[0])
			}
			if got := sent["contentType"]; got != want {
				t.Errorf("contentType for .%s = %v, want %v", extension, got, want)
			}
		})
	}
}

// A carriage return or newline ends a header line, so a value carrying one is
// a second header the caller never wrote. net/http refuses to send it, but
// only at send time and as a transport error — the one outcome this client
// reports as unknown, when in truth nothing was sent. New refuses it instead.
func TestNewRejectsAHeaderThatWouldSplitTheRequest(t *testing.T) {
	for name, header := range map[string]struct{ name, value string }{
		"crlf in value": {"X-Trace", "abc\r\nX-Injected: yes"},
		"bare lf":       {"X-Trace", "abc\nX-Injected: yes"},
		"nul in value":  {"X-Trace", "abc\x00def"},
		"crlf in name":  {"X-Bad\r\nX-Injected", "yes"},
		"space in name": {"X Trace", "yes"},
		"empty name":    {"", "yes"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := panmail.New("https://mail.example.com", "k", panmail.WithHeader(header.name, header.value))
			if err == nil {
				t.Fatalf("New accepted %q: %q", header.name, header.value)
			}
		})
	}

	// A tab is legal in a header value and stays legal.
	if _, err := panmail.New("https://mail.example.com", "k", panmail.WithHeader("X-Trace", "a\tb")); err != nil {
		t.Errorf("New rejected an ordinary header: %v", err)
	}
}

// A url carrying a password travels into every error this client reports, and
// an error is a thing that gets logged. Node's fetch refuses such a url by
// throwing a TypeError that quotes the whole thing, password included, so the
// leak is real rather than theoretical — all four refuse it up front instead.
func TestNewRejectsCredentialsInTheBaseURL(t *testing.T) {
	for _, baseURL := range []string{
		"https://user:hunter2@mail.example.com",
		"https://user@mail.example.com",
	} {
		_, err := panmail.New(baseURL, "k")
		if err == nil {
			t.Errorf("New accepted %q", baseURL)
			continue
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Errorf("the password is in the error: %v", err)
		}
	}
}

// The api key is a tenant-wide sending credential, and http puts it on the
// wire in the clear. Loopback is exempt because a gateway on localhost is how
// this is developed against, and there is no network to listen on.
func TestNewRequiresHTTPSAwayFromLoopback(t *testing.T) {
	refused := []string{"http://mail.example.com", "http://169.254.169.254/"}
	accepted := []string{
		"https://mail.example.com",
		"http://localhost:8080",
		"http://LOCALHOST:8080",
		"http://127.0.0.1:9000",
		"http://127.5.5.5:9000",
		"http://[::1]:9000",
	}

	for _, baseURL := range refused {
		if _, err := panmail.New(baseURL, "k"); err == nil {
			t.Errorf("New accepted cleartext %q", baseURL)
		}
	}
	for _, baseURL := range accepted {
		if _, err := panmail.New(baseURL, "k"); err != nil {
			t.Errorf("New refused %q: %v", baseURL, err)
		}
	}
}
