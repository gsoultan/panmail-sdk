package panmail_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	panmail "github.com/gsoultan/panmail-sdk"
)

// A refusal followed by a body that never ends used to hold Send until the
// HTTP client's own timeout: the deferred drain read the whole remainder after
// the result was already decided.
func TestSendDoesNotDrainABodyThatNeverEnds(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		chunk := []byte(strings.Repeat("x", 64<<10))
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := w.Write(chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	})

	c := client(t, g, panmail.WithHTTPClient(&http.Client{}))

	start := time.Now()
	_, err := c.Send(context.Background(), hello())
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Send took %v; it should return once the refusal is read", elapsed)
	}

	var apiErr *panmail.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("got %v, want an APIError", err)
	}
	if len(apiErr.Message) > 8<<10 {
		t.Fatalf("APIError.Message is %d bytes; a proxy page should be cut to a log line", len(apiErr.Message))
	}
}
