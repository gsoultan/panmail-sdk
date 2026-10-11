package panmail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// listEventsProcedure is the Connect route for EventService.ListEvents, the
// procedure the dashboard's delivery views read from.
const listEventsProcedure = "/panmail.v1.EventService/ListEvents"

// eventPageSize is how many events each request asks for. The gateway's
// PostgreSQL store caps a page at 500; a message's history is rarely more than
// a handful of events per recipient, so one page is the usual case.
const eventPageSize = 100

// maxEventPages bounds how long MessageEvents follows the gateway's page
// tokens, for the same reason maxProviderPages does: a loop that trusts the
// other side to stop is unbounded, and so is the memory behind it. At
// eventPageSize that is 5,000 events for one message — a message to a few
// hundred recipients, each opened and clicked several times.
const maxEventPages = 50

// A listing of events is a read, so its codes read as what they say and
// nothing more.
var listEventsCall = call{procedure: listEventsProcedure, what: "listing events", classify: classify}

// ErrNoMessageID reports a call for a message's events that named no message.
//
// It is refused here rather than sent because the gateway reads an empty
// message id as no filter at all, and answers with every event the tenant has.
var ErrNoMessageID = errors.New("panmail: a message id is required")

// DeliveryEvent is one thing that happened to a message on its way to one
// recipient: queued, sent, delivered, bounced, opened.
//
// It is as much of the gateway's EmailEvent as a sender needs to tell what
// became of a message. The gateway also returns the provider's name, the
// subject, the tenant id and a free-form metadata object — the click's URL, a
// provider's own bounce codes. None of it is decoded: the subject is the
// caller's own, and the metadata is a provider's vocabulary rather than the
// gateway's, with no shape to promise.
type DeliveryEvent struct {
	// ID is the event's own id.
	ID string

	// MessageID is the id Send returned.
	MessageID string

	// Type is what happened, one of the Status constants. Like a provider
	// type, a value the gateway's enum has no name for comes through as its
	// decimal spelling rather than failing the call.
	Type Status

	// Recipient is the address it happened for. A message to three people
	// has three histories, and they need not agree.
	Recipient string

	// Timestamp is when the gateway recorded it.
	Timestamp time.Time

	// ProviderID is the provider that handled the message, as
	// Message.ProviderID takes it. Empty for an event recorded before a
	// provider was chosen.
	ProviderID string

	// Reason is the provider's or the gateway's explanation, for an event
	// that has one: why a bounce bounced, why a send was rejected. Prose, for
	// a person or a log.
	Reason string
}

// listEventsRequest is the wire form of a request for one page. The field
// names are the protobuf JSON names of ListEventsRequest.
type listEventsRequest struct {
	PageSize  int    `json:"pageSize"`
	PageToken string `json:"pageToken,omitempty"`
	MessageID string `json:"messageId"`
}

// listEventsResponse is the part of ListEventsResponse this package reads.
// Fields not named here are not decoded.
type listEventsResponse struct {
	Events []struct {
		ID           string    `json:"id"`
		MessageID    string    `json:"messageId"`
		Type         enumName  `json:"type"`
		Recipient    string    `json:"recipient"`
		Timestamp    time.Time `json:"timestamp"`
		ProviderID   string    `json:"providerId"`
		ErrorMessage string    `json:"errorMessage"`
	} `json:"events"`
	NextPageToken string `json:"nextPageToken"`
}

// MessageEvents returns what has happened to a message so far, oldest first:
// every event the gateway has recorded for it, for every recipient.
//
// The key needs the events:read scope, which no key has by default. One minted
// for sending holds email:send alone and is refused with an AuthError naming
// the scope. events:read lets a key read every message's delivery history in
// the tenant — recipients included — so a second key for the job that needs it
// keeps the sending key as narrow as it was.
//
// An unknown message id is not an error: it has no events, and neither does a
// message from another tenant. The latest event per recipient is the state
// that recipient's copy is in; a later open after a delivery is the normal
// order of things, not a correction.
//
// It is one call because a message's history is short: the gateway is asked
// for pages and they are joined here. The gateway's stores order a message's
// events differently, so they are sorted here by time, keeping the gateway's
// order between two that share a timestamp. Each event is returned once. It
// stops with an error rather than return a history cut short: at the first
// page token handed out twice, or after maxEventPages pages.
//
// Nothing is retried. Reading changes nothing, so any error is safe to repeat
// by calling again.
func (c *Client) MessageEvents(ctx context.Context, messageID string) ([]DeliveryEvent, error) {
	if messageID == "" {
		return nil, ErrNoMessageID
	}
	req := listEventsRequest{PageSize: eventPageSize, MessageID: messageID}

	var events []DeliveryEvent
	seen := map[string]bool{}
	followed := map[string]bool{}

	for range maxEventPages {
		body, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("panmail: encoding the request failed: %w", err)
		}

		var page listEventsResponse
		if err := c.post(ctx, listEventsCall, body, &page); err != nil {
			return nil, err
		}

		for _, e := range page.Events {
			if seen[e.ID] {
				continue
			}
			seen[e.ID] = true

			kind := Status(e.Type)
			if kind == "" {
				kind = StatusUnspecified
			}
			events = append(events, DeliveryEvent{
				ID:         e.ID,
				MessageID:  e.MessageID,
				Type:       kind,
				Recipient:  e.Recipient,
				Timestamp:  e.Timestamp,
				ProviderID: e.ProviderID,
				Reason:     e.ErrorMessage,
			})
		}

		if page.NextPageToken == "" {
			sort.SliceStable(events, func(i, j int) bool {
				return events[i].Timestamp.Before(events[j].Timestamp)
			})
			return events, nil
		}
		if followed[page.NextPageToken] {
			return nil, fmt.Errorf("panmail: the gateway handed out page token %q a second time; "+
				"refusing to read the same pages again", page.NextPageToken)
		}
		followed[page.NextPageToken] = true
		req.PageToken = page.NextPageToken
	}

	return nil, fmt.Errorf("panmail: the gateway was still paging after %d pages of %d events for one message",
		maxEventPages, eventPageSize)
}
