package panmail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// The Connect routes for the two SuppressionService procedures this package
// calls. The service also adds, imports and lists; a sender needs none of it.
const (
	checkSuppressionProcedure  = "/panmail.v1.SuppressionService/CheckSuppression"
	removeSuppressionProcedure = "/panmail.v1.SuppressionService/RemoveSuppression"
)

// Neither is a send, so failed_precondition here is not a suppressed recipient
// and resource_exhausted is not a queue.
var (
	checkSuppressionCall  = call{procedure: checkSuppressionProcedure, what: "checking a suppression", classify: classify}
	removeSuppressionCall = call{procedure: removeSuppressionProcedure, what: "removing a suppression", classify: classify}
)

// ErrNoAddress reports a suppression call that named no address.
//
// The gateway answers a check of no address with "not suppressed", which is
// true and useless, and refuses a removal of one. Neither is worth a round
// trip.
var ErrNoAddress = errors.New("panmail: an email address is required")

// SuppressionCheck is whether the tenant's suppression list holds an address.
type SuppressionCheck struct {
	// Suppressed is true when a send to the address would be refused with a
	// SuppressedRecipientError.
	Suppressed bool

	// Reason is why it was suppressed, as recorded: a bounce, a complaint, or
	// whatever an operator typed. Empty when Suppressed is false.
	Reason string
}

// suppressionRequest is the wire form of both requests: CheckSuppressionRequest
// and RemoveSuppressionRequest each carry the address and nothing else.
type suppressionRequest struct {
	Email string `json:"email"`
}

// CheckSuppression reports whether address is on the tenant's suppression
// list, the list that turns a send to it into a SuppressedRecipientError.
//
// The gateway compares addresses as mailboxes, not strings: "Alice
// <Alice@Example.COM>" and "alice@example.com" are the same entry.
//
// The key needs the suppressions:read scope, which no key has by default; one
// without it is refused with an AuthError naming the scope. Nothing is
// retried, and checking changes nothing, so any error is safe to repeat.
func (c *Client) CheckSuppression(ctx context.Context, address string) (SuppressionCheck, error) {
	body, err := suppressionBody(address)
	if err != nil {
		return SuppressionCheck{}, err
	}

	var decoded struct {
		IsSuppressed bool   `json:"isSuppressed"`
		Reason       string `json:"reason"`
	}
	if err := c.post(ctx, checkSuppressionCall, body, &decoded); err != nil {
		return SuppressionCheck{}, err
	}
	return SuppressionCheck{Suppressed: decoded.IsSuppressed, Reason: decoded.Reason}, nil
}

// RemoveSuppression takes address off the tenant's suppression list, so that
// sends to it are accepted again.
//
// Removing an address that is not on the list succeeds, so a removal is safe
// to repeat; that is also why nothing is retried here — calling again is the
// caller's decision, and costs nothing.
//
// The address was usually put there for a reason: a hard bounce or a
// complaint. Mailing it again without that reason having gone away is how a
// sending reputation is spent. Check the reason first.
//
// The key needs the suppressions:write scope, which no key has by default and
// which also lets the key add and import suppressions. One without it is
// refused with an AuthError naming the scope.
func (c *Client) RemoveSuppression(ctx context.Context, address string) error {
	body, err := suppressionBody(address)
	if err != nil {
		return err
	}

	var decoded struct{}
	return c.post(ctx, removeSuppressionCall, body, &decoded)
}

func suppressionBody(address string) ([]byte, error) {
	if address == "" {
		return nil, ErrNoAddress
	}
	body, err := json.Marshal(suppressionRequest{Email: address})
	if err != nil {
		return nil, fmt.Errorf("panmail: encoding the request failed: %w", err)
	}
	return body, nil
}
