package panmail_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	panmail "github.com/gsoultan/panmail-sdk"
)

// probed is a gateway that answers /readyz with a fixed status and body and
// records the requests it was sent.
type probed struct {
	server   *httptest.Server
	requests []*http.Request
	bodies   []string
}

func readiness(t *testing.T, status int, body string) *probed {
	t.Helper()

	p := &probed{}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		read, _ := io.ReadAll(r.Body)
		p.requests = append(p.requests, r)
		p.bodies = append(p.bodies, string(read))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(p.server.Close)
	return p
}

func readyClient(t *testing.T, p *probed, opts ...panmail.Option) *panmail.Client {
	t.Helper()
	c, err := panmail.New(p.server.URL, "test-key", opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// The bodies below are the gateway's, byte for byte: internal/health's
// ReadyHandler encodes them with json.Encoder, newline and all.
const (
	readyBody        = "{\"status\":\"ready\"}\n"
	notReadyBody     = "{\"failed\":[\"database\"],\"status\":\"not ready\"}\n"
	shuttingDownBody = "{\"status\":\"shutting down\"}\n"
)

func TestReady(t *testing.T) {
	p := readiness(t, http.StatusOK, readyBody)

	if err := readyClient(t, p).Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}

	req := p.requests[0]
	if req.URL.Path != "/readyz" {
		t.Errorf("path = %q, want /readyz", req.URL.Path)
	}
	// POST because the PHP transport only sends POST; the gateway answers
	// readiness on any method.
	if req.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", req.Method)
	}
	if p.bodies[0] != "" {
		t.Errorf("body = %q, want none", p.bodies[0])
	}
}

// The endpoint is unauthenticated; the key would only be one more place for a
// tenant-wide credential to be logged.
func TestReadySendsNoApiKey(t *testing.T) {
	p := readiness(t, http.StatusOK, readyBody)

	if err := readyClient(t, p).Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if got := p.requests[0].Header.Get("X-API-Key"); got != "" {
		t.Errorf("X-API-Key = %q, want it left off", got)
	}
}

// A proxy that wants a header for a send wants it for this too.
func TestReadySendsTheExtraHeaders(t *testing.T) {
	p := readiness(t, http.StatusOK, readyBody)

	c := readyClient(t, p, panmail.WithHeader("X-Proxy-Token", "letmein"))
	if err := c.Ready(context.Background()); err != nil {
		t.Fatalf("Ready: %v", err)
	}
	if got := p.requests[0].Header.Get("X-Proxy-Token"); got != "letmein" {
		t.Errorf("X-Proxy-Token = %q", got)
	}
}

func TestReadyReportsTheDependencyThatFailed(t *testing.T) {
	p := readiness(t, http.StatusServiceUnavailable, notReadyBody)

	err := readyClient(t, p).Ready(context.Background())

	var notReady *panmail.NotReadyError
	if !errors.As(err, &notReady) {
		t.Fatalf("err = %v, want a NotReadyError", err)
	}
	if notReady.State != "not ready" || !slices.Equal(notReady.Failed, []string{"database"}) {
		t.Errorf("err = %+v", notReady)
	}
	if !strings.Contains(err.Error(), `"database"`) {
		t.Errorf("message %q does not name what failed", err)
	}
	var apiErr *panmail.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable {
		t.Errorf("err = %v, want it to unwrap to the 503", err)
	}
}

func TestReadyReportsAGatewayShuttingDown(t *testing.T) {
	p := readiness(t, http.StatusServiceUnavailable, shuttingDownBody)

	err := readyClient(t, p).Ready(context.Background())

	var notReady *panmail.NotReadyError
	if !errors.As(err, &notReady) || notReady.State != "shutting down" || len(notReady.Failed) != 0 {
		t.Fatalf("err = %v, want a NotReadyError shutting down with nothing failed", err)
	}
	if !strings.Contains(err.Error(), "shutting down") {
		t.Errorf("message %q does not say why", err)
	}
}

// A 200 is not "ready" unless it says so. An older gateway, or a proxy that
// serves a web app on every path it does not know, answers /readyz with a
// page.
func TestReadyDoesNotTakeA200WithoutAReadinessAnswer(t *testing.T) {
	for name, body := range map[string]string{
		"html":       "<!doctype html><title>panmail</title>",
		"other json": `{"messageId":"msg_01"}`,
		"empty":      "",
	} {
		t.Run(name, func(t *testing.T) {
			p := readiness(t, http.StatusOK, body)

			err := readyClient(t, p).Ready(context.Background())

			if err == nil {
				t.Fatal("Ready took a 200 with no readiness answer for ready")
			}
			var notReady *panmail.NotReadyError
			if errors.As(err, &notReady) {
				t.Errorf("err = %v; the gateway did not say it was not ready", err)
			}
		})
	}
}

// A 503 from a proxy is not the gateway's answer, and it is not dressed up as
// one.
func TestReadyReportsAnyOtherAnswerAsAnAPIError(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{
		{http.StatusServiceUnavailable, "<html>upstream unavailable</html>"},
		{http.StatusNotFound, "404 page not found"},
		// Something in front of the gateway: no key was sent to refuse.
		{http.StatusUnauthorized, "auth required"},
	} {
		p := readiness(t, tc.status, tc.body)

		err := readyClient(t, p).Ready(context.Background())

		var notReady *panmail.NotReadyError
		if errors.As(err, &notReady) {
			t.Errorf("%d: err = %v, want no NotReadyError", tc.status, err)
		}
		var auth *panmail.AuthError
		if errors.As(err, &auth) {
			t.Errorf("%d: err = %v, want no AuthError", tc.status, err)
		}
		var apiErr *panmail.APIError
		if !errors.As(err, &apiErr) || apiErr.Status != tc.status {
			t.Errorf("%d: err = %v, want an APIError with the status", tc.status, err)
		}
	}
}

func TestReadyRefusesToFollowARedirect(t *testing.T) {
	var reached bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		_, _ = io.WriteString(w, readyBody)
	}))
	defer elsewhere.Close()

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/readyz", http.StatusTemporaryRedirect)
	}))
	defer redirecting.Close()

	c, err := panmail.New(redirecting.URL, "secret-key", panmail.WithHeader("X-Proxy-Token", "letmein"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if err := c.Ready(context.Background()); err == nil {
		t.Fatal("Ready followed the redirect and called the other host ready")
	}
	if reached {
		t.Error("the request, and its headers, reached the redirect target")
	}
}

// The answer is a few dozen bytes; one that is not is read only as far as the
// bound, and never mistaken for ready.
func TestReadyBoundsTheAnswer(t *testing.T) {
	huge := `{"status":"ready","pad":"` + strings.Repeat("x", 1<<20) + `"}`
	p := readiness(t, http.StatusOK, huge)

	if err := readyClient(t, p).Ready(context.Background()); err == nil {
		t.Fatal("Ready decoded an answer past its bound")
	}
}

func TestReadyHonoursTheContext(t *testing.T) {
	hung := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer hung.Close()

	c, err := panmail.New(hung.URL, "test-key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err = c.Ready(ctx)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context's deadline", err)
	}
}
