package panmail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// apiKeyHeader is not Authorization on purpose. That header carries a
// dashboard session, and a key sent as a bearer token is rejected as a
// malformed session rather than as a bad key — a confusing way to learn you
// used the wrong header.
const apiKeyHeader = "X-API-Key"

// sendProcedure is the Connect route for EmailService.SendEmail. Connect
// derives it from the proto package and service name, so it changes only if
// the proto does.
const sendProcedure = "/panmail.v1.EmailService/SendEmail"

// maxResponseBytes bounds what a single response may cost in memory. A send
// response is a message id and a status, and a page of providers is a few
// hundred bytes for each of providerPageSize; anything approaching this is a
// proxy error page, not the gateway.
const maxResponseBytes = 1 << 20

// call is one Connect procedure and what its refusals mean.
//
// The same code can mean different things from different procedures:
// failed_precondition is a suppressed recipient when a send is refused, and
// nothing of the kind anywhere else. So the classification travels with the
// route rather than being applied to every response alike.
type call struct {
	procedure string
	// what names the call in a transport error: "the send did not complete".
	what     string
	classify func(*APIError, http.Header) error
}

var sendCall = call{procedure: sendProcedure, what: "the send", classify: classifySend}

// Client sends mail through a panmail gateway. Safe for concurrent use.
type Client struct {
	// baseURL is the gateway's origin with any trailing slash removed; each
	// call appends its own procedure.
	baseURL string
	apiKey  string
	opts    options
}

// New builds a client for the gateway at baseURL, authenticating with apiKey.
//
// baseURL is the gateway's origin — "https://mail.example.com" — not a path to
// a procedure.
func New(baseURL, apiKey string, opts ...Option) (*Client, error) {
	if err := validateBaseURL(baseURL); err != nil {
		return nil, err
	}
	if apiKey == "" {
		return nil, errors.New("panmail: an api key is required")
	}

	opt := newOptions(opts)
	if err := validateHeaders(opt.headers); err != nil {
		return nil, err
	}

	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		opts:    opt,
	}, nil
}

// Send queues a message and returns once the gateway has it on disk.
//
// A nil error means the gateway accepted responsibility for the message, not
// that it has been delivered — that is reported afterwards through delivery
// events and webhooks, keyed by Result.MessageID.
//
// It does not mean the message will be delivered. A filter rule can quarantine
// one for review, and the gateway answers a held message with the same message
// id and the same StatusPending as an accepted one, byte for byte. Nothing in
// the response tells them apart. Subscribe to TriggerEventMailHeld if that
// matters, and to TriggerEventMailExpired, which is a held message reaching its
// retention deadline with nobody having reviewed it.
//
// An error is either a refusal the caller can act on — see RateLimitedError,
// BacklogFullError and AuthError — or the transport error underneath. A send
// that fails with an unknown outcome is not retried; see the package
// documentation for why.
func (c *Client) Send(ctx context.Context, msg Message) (Result, error) {
	req, err := msg.request()
	if err != nil {
		return Result{}, err
	}

	body, err := json.Marshal(req)
	if err != nil {
		return Result{}, fmt.Errorf("panmail: encoding the message failed: %w", err)
	}

	for attempt := 0; ; attempt++ {
		var decoded struct {
			MessageID string `json:"messageId"`
			Status    Status `json:"status"`
		}
		err := c.post(ctx, sendCall, body, &decoded)
		if err == nil {
			return Result{MessageID: decoded.MessageID, Status: decoded.Status}, nil
		}

		wait, ok := c.waitBefore(err, attempt)
		if !ok {
			return Result{}, err
		}
		if sleepErr := sleep(ctx, wait); sleepErr != nil {
			// The caller's deadline outranks the gateway's suggestion.
			return Result{}, errors.Join(err, sleepErr)
		}
	}
}

// post performs one round trip to a procedure and decodes a 200 into out. Every
// return path has already drained and closed the response body, so the
// connection goes back to the pool.
func (c *Client) post(ctx context.Context, rpc call, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+rpc.procedure, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("panmail: building the request failed: %w", err)
	}

	for name, value := range c.opts.headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(apiKeyHeader, c.apiKey)

	res, err := c.opts.httpClient.Do(req)
	if err != nil {
		// Deliberately not classified and never retried: for a send, a
		// transport error is the one outcome where the client does not know
		// whether the gateway took the message. A listing is safe to repeat,
		// but repeating it is the caller's decision, as with any other error.
		return fmt.Errorf("panmail: %s did not complete: %w", rpc.what, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}()

	// One byte past the bound, so a body that reaches it can be told from one
	// that fits exactly.
	payload, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("panmail: reading the response failed: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		// A refusal is still classified when its body was cut short: the
		// status and code decide what it is, and a proxy's error page can run
		// long.
		if len(payload) > maxResponseBytes {
			payload = payload[:maxResponseBytes]
		}
		return rpc.classify(decodeError(payload, res.StatusCode), res.Header)
	}

	// A success cut short can never decode, and "unexpected end of JSON
	// input" would send the reader looking for a malformed body rather than a
	// long one.
	if len(payload) > maxResponseBytes {
		return fmt.Errorf("panmail: the gateway's response exceeded %d bytes", maxResponseBytes)
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("panmail: the gateway's response was not json: %w", err)
	}
	return nil
}

// decodeError reads the Connect error envelope: {"code":..., "message":...}.
// A body that is not that envelope still produces an APIError, because the
// status alone is worth reporting and a proxy's HTML is not.
func decodeError(payload []byte, status int) *APIError {
	apiErr := &APIError{Status: status}

	var envelope struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &envelope); err == nil {
		apiErr.Code = envelope.Code
		apiErr.Message = envelope.Message
	}
	if apiErr.Message == "" {
		apiErr.Message = strings.TrimSpace(string(payload))
	}
	return apiErr
}

// waitBefore reports how long to wait before repeating a refusal, and whether
// to repeat it at all. Only a rate limit is ever repeated: it is the one
// failure where the gateway said plainly it did not accept the message.
func (c *Client) waitBefore(err error, attempt int) (time.Duration, bool) {
	if attempt >= c.opts.rateLimitRetries {
		return 0, false
	}

	var limited *RateLimitedError
	if !errors.As(err, &limited) || limited.RetryAfter <= 0 {
		return 0, false
	}
	return limited.RetryAfter, true
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func validateBaseURL(baseURL string) error {
	if baseURL == "" {
		return errors.New("panmail: a base url is required")
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("panmail: base url is not a url: %w", err)
	}
	// A missing scheme is the usual mistake — "mail.example.com" parses
	// happily as a relative path, and the failure it causes surfaces much
	// later as an unreadable transport error.
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("panmail: base url needs an http or https scheme, got %q", baseURL)
	}
	if parsed.Host == "" {
		return fmt.Errorf("panmail: base url has no host: %q", baseURL)
	}
	// Userinfo is deprecated in RFC 3986 for the reason it matters here: the
	// url travels into every error this client reports, and an error is a
	// thing that gets logged. The api key is the credential; if something in
	// front of the gateway wants basic auth too, WithHeader is where it goes.
	// The api key is a tenant-wide sending credential and http puts it on the
	// wire in the clear. Loopback is exempt because a gateway on localhost is
	// how the thing is developed against, and there is no network to listen on.
	if parsed.Scheme == "http" && !isLoopback(parsed.Hostname()) {
		return fmt.Errorf("panmail: base url %q uses http, which would send the api key "+
			"in cleartext; use https, or http only against a loopback host", baseURL)
	}
	if parsed.User != nil {
		return errors.New("panmail: base url must not carry credentials; " +
			"the api key authenticates the send, and a password in the url ends up in logs")
	}
	return nil
}

// isLoopback reports whether host is this machine — the one place plaintext
// http carries the api key no further than the process next to it.
func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	// Covers 127.0.0.0/8 and ::1, rather than just the two spellings of them
	// that people write most often.
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
