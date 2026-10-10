package panmail_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	panmail "github.com/gsoultan/panmail-sdk"
)

const messageID = "0193b2f1-0000-7000-8000-000000000001"

// eventPage is one ListEvents response: the events, and the token for the next
// page, if there is one.
func eventPage(next string, events ...map[string]any) map[string]any {
	body := map[string]any{"events": events}
	if next != "" {
		body["nextPageToken"] = next
	}
	return body
}

// event is one entry in a page.
func event(id, kind, at string) map[string]any {
	return map[string]any{
		"id":        id,
		"messageId": messageID,
		"type":      kind,
		"recipient": "user@example.net",
		"timestamp": at,
	}
}

func eventIDs(events []panmail.DeliveryEvent) []string {
	out := []string{}
	for _, e := range events {
		out = append(out, e.ID)
	}
	return out
}

func TestMessageEvents(t *testing.T) {
	g := lists(t, eventPage("",
		// Newest first, as the gateway's PostgreSQL store answers.
		map[string]any{
			"id":           "ev_3",
			"tenantId":     "3f1c2b7a-0000-4000-8000-0000000000aa",
			"providerId":   "3f1c2b7a-0000-4000-8000-000000000001",
			"providerName": "Production SES",
			"messageId":    messageID,
			"type":         "EMAIL_EVENT_TYPE_HARD_BOUNCE",
			"recipient":    "gone@example.org",
			"subject":      "Your receipt",
			"timestamp":    "2026-09-01T10:00:02.123456789Z",
			"metadata":     map[string]any{"code": "550"},
			"errorMessage": "550 5.1.1 mailbox unknown",
		},
		event("ev_2", "EMAIL_EVENT_TYPE_SENT", "2026-09-01T10:00:01Z"),
		event("ev_1", "EMAIL_EVENT_TYPE_PENDING", "2026-09-01T10:00:00Z"),
	))

	events, err := client(t, g).MessageEvents(context.Background(), messageID)
	if err != nil {
		t.Fatalf("MessageEvents: %v", err)
	}

	if got, want := eventIDs(events), []string{"ev_1", "ev_2", "ev_3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("events = %v, want %v, oldest first", got, want)
	}
	bounce := events[2]
	want := panmail.DeliveryEvent{
		ID:         "ev_3",
		MessageID:  messageID,
		Type:       panmail.StatusHardBounce,
		Recipient:  "gone@example.org",
		Timestamp:  time.Date(2026, 9, 1, 10, 0, 2, 123456789, time.UTC),
		ProviderID: "3f1c2b7a-0000-4000-8000-000000000001",
		Reason:     "550 5.1.1 mailbox unknown",
	}
	if !bounce.Timestamp.Equal(want.Timestamp) {
		t.Errorf("Timestamp = %v, want %v", bounce.Timestamp, want.Timestamp)
	}
	bounce.Timestamp = want.Timestamp
	if !reflect.DeepEqual(bounce, want) {
		t.Errorf("event = %+v, want %+v", bounce, want)
	}
}

func TestMessageEventsPostsToTheConnectProcedure(t *testing.T) {
	g := lists(t, eventPage(""))

	if _, err := client(t, g).MessageEvents(context.Background(), messageID); err != nil {
		t.Fatalf("MessageEvents: %v", err)
	}

	r := g.requests[0]
	if r.URL.Path != "/panmail.v1.EventService/ListEvents" {
		t.Errorf("path = %q", r.URL.Path)
	}
	if got := r.Header.Get("X-API-Key"); got != "test-key" {
		t.Errorf("X-API-Key = %q", got)
	}
	body := g.bodies[0]
	if body["messageId"] != messageID {
		t.Errorf("messageId = %v, want %s", body["messageId"], messageID)
	}
	if body["pageSize"] != float64(100) {
		t.Errorf("pageSize = %v, want 100", body["pageSize"])
	}
	if _, present := body["pageToken"]; present {
		t.Error("the first page carried a page token")
	}
}

// An empty message id is no filter to the gateway, which would answer with
// every event in the tenant.
func TestMessageEventsRefusesAnEmptyMessageID(t *testing.T) {
	g := lists(t, eventPage(""))

	_, err := client(t, g).MessageEvents(context.Background(), "")

	if !errors.Is(err, panmail.ErrNoMessageID) {
		t.Fatalf("err = %v, want ErrNoMessageID", err)
	}
	if g.calls != 0 {
		t.Errorf("the gateway was called %d times, want 0", g.calls)
	}
}

func TestMessageEventsOfAnUnknownMessageIsEmpty(t *testing.T) {
	g := lists(t, map[string]any{})

	events, err := client(t, g).MessageEvents(context.Background(), messageID)
	if err != nil {
		t.Fatalf("MessageEvents: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("events = %v, want none", eventIDs(events))
	}
}

func TestMessageEventsFollowsEveryPage(t *testing.T) {
	g := lists(t,
		eventPage("2026-09-01T10:00:01Z|ev_2", event("ev_3", "EMAIL_EVENT_TYPE_OPENED", "2026-09-01T10:00:02Z")),
		eventPage("", event("ev_1", "EMAIL_EVENT_TYPE_PENDING", "2026-09-01T10:00:00Z")),
	)

	events, err := client(t, g).MessageEvents(context.Background(), messageID)
	if err != nil {
		t.Fatalf("MessageEvents: %v", err)
	}

	if got, want := eventIDs(events), []string{"ev_1", "ev_3"}; !reflect.DeepEqual(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
	if g.calls != 2 {
		t.Fatalf("the gateway was called %d times, want 2", g.calls)
	}
	if got := g.bodies[1]["pageToken"]; got != "2026-09-01T10:00:01Z|ev_2" {
		t.Errorf("second page token = %v", got)
	}
	if got := g.bodies[1]["messageId"]; got != messageID {
		t.Errorf("second page dropped the message id: %v", got)
	}
}

func TestMessageEventsReturnsAnEventRepeatedAcrossPagesOnce(t *testing.T) {
	g := lists(t,
		eventPage("next", event("ev_1", "EMAIL_EVENT_TYPE_PENDING", "2026-09-01T10:00:00Z")),
		eventPage("", event("ev_1", "EMAIL_EVENT_TYPE_PENDING", "2026-09-01T10:00:00Z")),
	)

	events, err := client(t, g).MessageEvents(context.Background(), messageID)
	if err != nil {
		t.Fatalf("MessageEvents: %v", err)
	}
	if got := eventIDs(events); !reflect.DeepEqual(got, []string{"ev_1"}) {
		t.Errorf("events = %v, want ev_1 once", got)
	}
}

func TestMessageEventsRefusesATokenItHasAlreadyFollowed(t *testing.T) {
	g := lists(t,
		eventPage("loop", event("ev_1", "EMAIL_EVENT_TYPE_PENDING", "2026-09-01T10:00:00Z")),
		eventPage("loop", event("ev_2", "EMAIL_EVENT_TYPE_SENT", "2026-09-01T10:00:01Z")),
	)

	_, err := client(t, g).MessageEvents(context.Background(), messageID)
	if err == nil || !strings.Contains(err.Error(), "a second time") {
		t.Fatalf("err = %v, want a repeated-token error", err)
	}
	if g.calls != 2 {
		t.Errorf("the gateway was called %d times, want 2", g.calls)
	}
}

func TestMessageEventsStopsAtThePageLimit(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, call int) {
		writeJSON(w, http.StatusOK, eventPage(fmt.Sprintf("token-%d", call),
			event(fmt.Sprintf("ev_%d", call), "EMAIL_EVENT_TYPE_OPENED", "2026-09-01T10:00:00Z")))
	})

	_, err := client(t, g).MessageEvents(context.Background(), messageID)
	if err == nil || !strings.Contains(err.Error(), "still paging") {
		t.Fatalf("err = %v, want the page limit", err)
	}
	if g.calls != 50 {
		t.Errorf("the gateway was called %d times, want 50", g.calls)
	}
}

// The gateway's two stores order a message's events differently; the result
// is by time either way, and two events in the same instant keep the order
// the gateway gave them.
func TestMessageEventsSortsByTimeKeepingTheGatewaysOrderForTies(t *testing.T) {
	g := lists(t, eventPage("",
		event("ev_b", "EMAIL_EVENT_TYPE_DELIVERED", "2026-09-01T10:00:05Z"),
		event("ev_a", "EMAIL_EVENT_TYPE_DELIVERED", "2026-09-01T10:00:05Z"),
		event("ev_first", "EMAIL_EVENT_TYPE_SENT", "2026-09-01T09:59:59.5Z"),
	))

	events, err := client(t, g).MessageEvents(context.Background(), messageID)
	if err != nil {
		t.Fatalf("MessageEvents: %v", err)
	}
	if got, want := eventIDs(events), []string{"ev_first", "ev_b", "ev_a"}; !reflect.DeepEqual(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

func TestMessageEventsKeepsATypeTheGatewayHasNoNameFor(t *testing.T) {
	g := lists(t, eventPage("", map[string]any{"id": "ev_1", "type": 99}))

	events, err := client(t, g).MessageEvents(context.Background(), messageID)
	if err != nil {
		t.Fatalf("MessageEvents: %v", err)
	}
	if events[0].Type != "99" {
		t.Errorf("Type = %q, want 99", events[0].Type)
	}
}

func TestMessageEventsReadsAMissingTypeAsUnspecified(t *testing.T) {
	g := lists(t, eventPage("", map[string]any{"id": "ev_1"}))

	events, err := client(t, g).MessageEvents(context.Background(), messageID)
	if err != nil {
		t.Fatalf("MessageEvents: %v", err)
	}
	if events[0].Type != panmail.StatusUnspecified {
		t.Errorf("Type = %q, want %q", events[0].Type, panmail.StatusUnspecified)
	}
	if !events[0].Timestamp.IsZero() {
		t.Errorf("Timestamp = %v, want none", events[0].Timestamp)
	}
}

func TestMessageEventsRefusesATimestampThatIsNotOne(t *testing.T) {
	g := lists(t, eventPage("", event("ev_1", "EMAIL_EVENT_TYPE_SENT", "yesterday")))

	if _, err := client(t, g).MessageEvents(context.Background(), messageID); err == nil {
		t.Fatal("MessageEvents accepted a timestamp it cannot read")
	}
}

// The subject, the metadata and the tenant are not a sender's to need, so a
// DeliveryEvent has nowhere to put them.
func TestMessageEventsDecodesOnlyWhatASenderNeeds(t *testing.T) {
	var fields []string
	for f := range reflect.TypeFor[panmail.DeliveryEvent]().Fields() {
		fields = append(fields, f.Name)
	}
	want := []string{"ID", "MessageID", "Type", "Recipient", "Timestamp", "ProviderID", "Reason"}
	if !reflect.DeepEqual(fields, want) {
		t.Errorf("DeliveryEvent fields = %v, want %v", fields, want)
	}
}

func TestMessageEventsNeedsTheEventsReadScope(t *testing.T) {
	g := respond(t, func(w http.ResponseWriter, _ int) {
		refuse(w, http.StatusForbidden, "permission_denied", `api key is missing the "events:read" scope`)
	})

	_, err := client(t, g).MessageEvents(context.Background(), messageID)

	var auth *panmail.AuthError
	if !errors.As(err, &auth) {
		t.Fatalf("err = %v, want an AuthError", err)
	}
	if !strings.Contains(err.Error(), "events:read") {
		t.Errorf("err = %q, want it to name the scope", err)
	}
}

func TestMessageEventsDoesNotReadSendRefusalsIntoAList(t *testing.T) {
	cases := map[string]func(http.ResponseWriter){
		"failed_precondition": func(w http.ResponseWriter) {
			refuse(w, http.StatusBadRequest, "failed_precondition", "no")
		},
		"resource_exhausted": func(w http.ResponseWriter) {
			refuse(w, http.StatusTooManyRequests, "resource_exhausted", "no")
		},
		"already_exists": func(w http.ResponseWriter) {
			refuse(w, http.StatusConflict, "already_exists", "no")
		},
	}

	for code, answer := range cases {
		t.Run(code, func(t *testing.T) {
			g := respond(t, func(w http.ResponseWriter, _ int) { answer(w) })

			_, err := client(t, g, panmail.WithRateLimitRetries(3), panmail.WithTransportRetries(3)).
				MessageEvents(context.Background(), messageID)

			var apiErr *panmail.APIError
			if !errors.As(err, &apiErr) || apiErr.Code != code {
				t.Fatalf("err = %v, want an APIError with code %s", err, code)
			}
			var suppressed *panmail.SuppressedRecipientError
			var full *panmail.BacklogFullError
			var conflict *panmail.IdempotencyKeyConflictError
			if errors.As(err, &suppressed) || errors.As(err, &full) || errors.As(err, &conflict) {
				t.Errorf("err = %v, a send's refusal read into a listing", err)
			}
			if g.calls != 1 {
				t.Errorf("the gateway was called %d times, want 1", g.calls)
			}
		})
	}
}
