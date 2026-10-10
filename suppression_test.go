package panmail_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	panmail "github.com/gsoultan/panmail-sdk"
)

func TestCheckSuppression(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		writeJSON(w, http.StatusOK, map[string]any{"isSuppressed": true, "reason": "hard bounce"})
	})

	check, err := client(t, g).CheckSuppression(context.Background(), "Bounced <bounced@example.net>")
	if err != nil {
		t.Fatalf("CheckSuppression: %v", err)
	}

	if check != (panmail.SuppressionCheck{Suppressed: true, Reason: "hard bounce"}) {
		t.Errorf("check = %+v", check)
	}
	if got := g.requests[0].URL.Path; got != "/panmail.v1.SuppressionService/CheckSuppression" {
		t.Errorf("path = %q", got)
	}
	if got := g.requests[0].Header.Get("X-API-Key"); got != "test-key" {
		t.Errorf("X-API-Key = %q", got)
	}
	// Sent as given: the gateway, not the client, decides what a mailbox is.
	if got := g.bodies[0]["email"]; got != "Bounced <bounced@example.net>" {
		t.Errorf("email = %v", got)
	}
}

// protobuf JSON leaves false and "" out, so an address not on the list is an
// empty object.
func TestCheckSuppressionOfAnAddressNotOnTheList(t *testing.T) {
	g := lists(t, map[string]any{})

	check, err := client(t, g).CheckSuppression(context.Background(), "fine@example.net")
	if err != nil {
		t.Fatalf("CheckSuppression: %v", err)
	}
	if check != (panmail.SuppressionCheck{}) {
		t.Errorf("check = %+v, want not suppressed", check)
	}
}

func TestCheckSuppressionRefusesAnEmptyAddress(t *testing.T) {
	g := lists(t, map[string]any{})

	_, err := client(t, g).CheckSuppression(context.Background(), "")

	if !errors.Is(err, panmail.ErrNoAddress) {
		t.Fatalf("err = %v, want ErrNoAddress", err)
	}
	if g.calls != 0 {
		t.Errorf("the gateway was called %d times, want 0", g.calls)
	}
}

func TestCheckSuppressionNeedsTheSuppressionsReadScope(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		refuse(w, http.StatusForbidden, "permission_denied", `api key is missing the "suppressions:read" scope`)
	})

	_, err := client(t, g).CheckSuppression(context.Background(), "someone@example.net")

	var auth *panmail.AuthError
	if !errors.As(err, &auth) || !strings.Contains(err.Error(), "suppressions:read") {
		t.Fatalf("err = %v, want an AuthError naming the scope", err)
	}
}

func TestRemoveSuppression(t *testing.T) {
	g := lists(t, map[string]any{})

	if err := client(t, g).RemoveSuppression(context.Background(), "bounced@example.net"); err != nil {
		t.Fatalf("RemoveSuppression: %v", err)
	}

	if got := g.requests[0].URL.Path; got != "/panmail.v1.SuppressionService/RemoveSuppression" {
		t.Errorf("path = %q", got)
	}
	if got := g.bodies[0]["email"]; got != "bounced@example.net" {
		t.Errorf("email = %v", got)
	}
}

func TestRemoveSuppressionRefusesAnEmptyAddress(t *testing.T) {
	g := lists(t, map[string]any{})

	err := client(t, g).RemoveSuppression(context.Background(), "")

	if !errors.Is(err, panmail.ErrNoAddress) {
		t.Fatalf("err = %v, want ErrNoAddress", err)
	}
	if g.calls != 0 {
		t.Errorf("the gateway was called %d times, want 0", g.calls)
	}
}

func TestRemoveSuppressionNeedsTheSuppressionsWriteScope(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		refuse(w, http.StatusForbidden, "permission_denied", `api key is missing the "suppressions:write" scope`)
	})

	err := client(t, g).RemoveSuppression(context.Background(), "someone@example.net")

	var auth *panmail.AuthError
	if !errors.As(err, &auth) || !strings.Contains(err.Error(), "suppressions:write") {
		t.Fatalf("err = %v, want an AuthError naming the scope", err)
	}
}

// failed_precondition is a suppressed recipient on a send, and nothing of the
// kind here; neither call is retried, whatever the options say.
func TestSuppressionCallsDoNotReadSendRefusalsIntoThem(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		w.Header().Set("Retry-After", "1")
		refuse(w, http.StatusBadRequest, "failed_precondition", "no")
	})
	c := client(t, g, panmail.WithRateLimitRetries(3), panmail.WithTransportRetries(3))

	_, checkErr := c.CheckSuppression(context.Background(), "someone@example.net")
	removeErr := c.RemoveSuppression(context.Background(), "someone@example.net")

	for name, err := range map[string]error{"check": checkErr, "remove": removeErr} {
		var apiErr *panmail.APIError
		if !errors.As(err, &apiErr) || apiErr.Code != "failed_precondition" {
			t.Errorf("%s: err = %v, want an APIError with the code", name, err)
		}
		var suppressed *panmail.SuppressedRecipientError
		if errors.As(err, &suppressed) {
			t.Errorf("%s: err = %v, a send's refusal read into a suppression call", name, err)
		}
	}
	if g.calls != 2 {
		t.Errorf("the gateway was called %d times, want 2", g.calls)
	}
}
