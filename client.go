package panmail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
// response is a message id and a status; anything approaching this is a proxy
// error page, not the gateway.
const maxResponseBytes = 1 << 20

// Client sends mail through a panmail gateway. Safe for concurrent use.
type Client struct {
	endpoint string
	apiKey   string
	opts     options
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
		endpoint: strings.TrimRight(baseURL, "/") + sendProcedure,
		apiKey:   apiKey,
		opts:     opt,
	}, nil
}

// Send queues a message and returns once the gateway has it on disk.
//
// A nil error means the gateway accepted responsibility for delivering the
// message, not that it has been delivered — that is reported afterwards
// through delivery events and webhooks, keyed by Result.MessageID.
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
		result, err := c.post(ctx, body)
		if err == nil {
			return result, nil
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

// post performs one round trip. Every return path has already drained and
// closed the response body, so the connection goes back to the pool.
func (c *Client) post(ctx context.Context, body []byte) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("panmail: building the request failed: %w", err)
	}

	for name, value := range c.opts.headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(apiKeyHeader, c.apiKey)

	res, err := c.opts.httpClient.Do(req)
	if err != nil {
		// Deliberately not classified and never retried: a transport error is
		// the one outcome where the client does not know whether the gateway
		// took the message.
		return Result{}, fmt.Errorf("panmail: the send did not complete: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
	}()

	payload, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return Result{}, fmt.Errorf("panmail: reading the response failed: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return Result{}, classify(decodeError(payload, res.StatusCode), res.Header)
	}

	var decoded struct {
		MessageID string `json:"messageId"`
		Status    Status `json:"status"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return Result{}, fmt.Errorf("panmail: the gateway's response was not json: %w", err)
	}

	return Result{MessageID: decoded.MessageID, Status: decoded.Status}, nil
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
	return nil
}
