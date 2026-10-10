package panmail_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
}

// A proxy page in front of a refusal can be a megabyte; the error carries
// enough of it for a log line and no more, and never half a character.
func TestALongProxyPageIsCutToALogLine(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(strings.Repeat("é", 1<<19)))
	})

	_, err := client(t, g).Send(context.Background(), hello())

	var apiErr *panmail.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("got %v, want an APIError", err)
	}
	if len(apiErr.Message) >= 4200 {
		t.Errorf("APIError.Message is %d bytes; want it cut to about 4 KiB", len(apiErr.Message))
	}
	if !utf8.ValidString(apiErr.Message) {
		t.Error("the cut left invalid UTF-8")
	}
}
