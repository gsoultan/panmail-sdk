package panmail

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// readyPath is the gateway's readiness endpoint. It is a plain HTTP route
// beside the Connect procedures, not one of them, so it has no proto and no
// Connect error envelope.
const readyPath = "/readyz"

// readyState is what the gateway's readiness answer says when it can serve.
const readyState = "ready"

// maxReadyBytes bounds a readiness answer. The gateway's is a status and the
// names of what failed, a few dozen bytes; anything near this is not it.
const maxReadyBytes = 64 << 10

// NotReadyError reports that the gateway answered its readiness check and said
// it cannot serve right now: a dependency it needs is down, or it is shutting
// down.
//
// It is the gateway's own answer, not a guess from a status code, so it
// recovers by itself: the gateway is still running and says ready again once
// the dependency is back, with no restart. Every other way Ready can fail — a
// proxy's 503, a 404 from an ingress that does not route /readyz, a dropped
// connection — is a different error, because the gateway did not say.
type NotReadyError struct {
	// State is the gateway's word for it: "not ready" when a dependency
	// failed, "shutting down" when the instance is draining before it stops.
	State string

	// Failed names the dependencies whose check failed, as the gateway
	// names them — "database" — sorted. Empty while shutting down, which
	// is not a dependency failing.
	Failed []string

	err *APIError
}

func (e *NotReadyError) Error() string {
	if len(e.Failed) == 0 {
		return "panmail: the gateway is not ready: " + strconv.Quote(e.State)
	}
	quoted := make([]string, len(e.Failed))
	for i, name := range e.Failed {
		// Quoted because the gateway wrote them and a log will read them.
		quoted[i] = strconv.Quote(name)
	}
	return "panmail: the gateway is not ready: " + strconv.Quote(e.State) + ", failing " + strings.Join(quoted, ", ")
}

// Unwrap returns the refusal underneath, so a caller already handling an
// *APIError by its status still sees the 503.
func (e *NotReadyError) Unwrap() error { return e.err }

// Ready asks the gateway whether it can serve requests right now. A nil error
// means it said it can.
//
// It calls the gateway's /readyz endpoint, the one a load balancer uses, which
// checks the gateway's own dependencies — its database — rather than just
// whether the process is up. A *NotReadyError is the gateway saying no and
// naming why. Any other error means the question did not get the gateway's
// answer: treat it as not ready too, but it is the path to the gateway, not
// the gateway, that is worth looking at.
//
// It sends no API key. The endpoint is unauthenticated, because a load
// balancer has to reach it, and a tenant's sending credential has no business
// on a request that does nothing with it. Headers from WithHeader are sent,
// since a proxy that demands one for a send demands it here too.
//
// The request is a POST with an empty body. The gateway answers readiness on
// any method, and the PHP client's Transport has only ever sent POST, so both
// clients send the same request rather than one of them breaking every custom
// transport to send a GET.
//
// The gateway caches its answer for a second, so calling this in a loop does
// not reach the database any faster than that. Nothing is retried.
func (c *Client) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+readyPath, http.NoBody)
	if err != nil {
		return fmt.Errorf("panmail: building the request failed: %w", err)
	}
	for name, value := range c.opts.headers {
		req.Header.Set(name, value)
	}

	res, err := c.opts.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("panmail: the readiness check did not complete: %w", err)
	}
	defer func() {
		// As in post: drain a little for the connection's sake, never all.
		_, _ = io.CopyN(io.Discard, res.Body, maxDrainBytes)
		_ = res.Body.Close()
	}()

	payload, err := io.ReadAll(io.LimitReader(res.Body, maxReadyBytes+1))
	if err != nil {
		return fmt.Errorf("panmail: reading the readiness answer failed: %w", err)
	}
	cut := len(payload) > maxReadyBytes
	if cut {
		payload = payload[:maxReadyBytes]
	}

	// Only these two fields: the answer carries nothing else a caller needs,
	// and decoding nothing else means nothing else can be handed back.
	var answer struct {
		Status string   `json:"status"`
		Failed []string `json:"failed"`
	}
	decoded := !cut && json.Unmarshal(payload, &answer) == nil && answer.Status != ""

	switch {
	case res.StatusCode == http.StatusOK && decoded && answer.Status == readyState:
		return nil
	case res.StatusCode == http.StatusOK:
		// A 200 alone is not "ready". A gateway older than /readyz, or a
		// proxy routing every unknown path to a web app, answers it with a
		// page — and calling that ready is the one wrong answer this method
		// must not give.
		return fmt.Errorf("panmail: %s answered 200 without a readiness answer; "+
			"the gateway may be too old to have one, or something in front of it served the path", readyPath)
	case res.StatusCode == http.StatusServiceUnavailable && decoded:
		return &NotReadyError{State: answer.Status, Failed: answer.Failed, err: decodeError(payload, res.StatusCode)}
	default:
		// Not classified: /readyz is not a Connect procedure, so a status
		// means only itself here. A 401 or 403 is something in front of the
		// gateway, and an AuthError would blame a key that was never sent.
		return decodeError(payload, res.StatusCode)
	}
}
