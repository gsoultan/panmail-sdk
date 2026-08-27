package panmail

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DefaultTimeout bounds a single send. Generous, because the gateway writes
// the message to its outbox before answering and that is a disk write on a
// possibly busy database — but finite, because a send that hangs holds
// whatever request is waiting on it.
const DefaultTimeout = 30 * time.Second

// An Option configures the client.
type Option func(*options)

type options struct {
	httpClient       *http.Client
	timeout          time.Duration
	rateLimitRetries int
	headers          map[string]string
}

// WithHTTPClient supplies the HTTP client to send with — for a custom
// transport, a proxy, or a connection pool shared with the rest of an
// application. Its own Timeout is left alone; WithTimeout is ignored when this
// is set, since a caller who brings a client has already made that decision.
//
// The client is copied, not kept: its Transport, and so its connection pool,
// is shared as you would expect, but the copy is given a CheckRedirect that
// refuses. See refuseRedirect for why that is not negotiable. A change made to
// the client after New will not be picked up.
func WithHTTPClient(client *http.Client) Option {
	return func(o *options) { o.httpClient = client }
}

// WithTimeout bounds each send. Ignored when WithHTTPClient is used.
//
// This is a ceiling, not a schedule: a context deadline shorter than it still
// wins.
func WithTimeout(timeout time.Duration) Option {
	return func(o *options) { o.timeout = timeout }
}

// WithRateLimitRetries waits out up to n rate-limit refusals, sleeping for the
// delay the gateway asks for each time.
//
// Off by default, and only ever applied to a refusal. A refusal is the one
// failure where the client knows the message was not accepted, so repeating it
// cannot deliver anything twice — but it does mean Send blocks for as long as
// the gateway asks, which inside a request handler is a decision the caller
// should make rather than inherit. A queue of your own is usually the better
// answer.
func WithRateLimitRetries(n int) Option {
	return func(o *options) { o.rateLimitRetries = n }
}

// WithHeader sets an extra HTTP header on every request — a tracing header, or
// whatever a proxy in front of the gateway requires.
//
// The API key header is not settable this way: it is what New's apiKey
// argument is for, and a second source for it would only make a mismatch
// possible.
func WithHeader(name, value string) Option {
	return func(o *options) {
		if o.headers == nil {
			o.headers = map[string]string{}
		}
		o.headers[name] = value
	}
}

func newOptions(opts []Option) options {
	resolved := options{timeout: DefaultTimeout}
	for _, opt := range opts {
		opt(&resolved)
	}
	if resolved.httpClient == nil {
		resolved.httpClient = &http.Client{Timeout: resolved.timeout}
	}
	// Copied rather than mutated: a caller's client may be shared with the rest
	// of the application, and its redirect policy is not ours to change on it.
	// The copy keeps the Transport pointer, so the connection pool is still the
	// caller's — only the redirect decision is ours.
	withoutRedirects := *resolved.httpClient
	withoutRedirects.CheckRedirect = refuseRedirect
	resolved.httpClient = &withoutRedirects

	if resolved.rateLimitRetries < 0 {
		resolved.rateLimitRetries = 0
	}
	// Case-insensitively, because a header name is: "x-api-key" set here and
	// X-API-Key set from the apiKey argument are the same header to the server.
	for name := range resolved.headers {
		if strings.EqualFold(name, apiKeyHeader) {
			delete(resolved.headers, name)
		}
	}
	return resolved
}

// refuseRedirect stops a redirect being followed.
//
// X-API-Key is a header net/http has never heard of, so it is not on the list
// of credentials it drops when a redirect crosses to another host — unlike
// Authorization, which it does drop. Following one would hand the tenant's api
// key to whatever host the Location header named. A Connect procedure has no
// reason to redirect, so the safe reading of one is that something is wrong.
func refuseRedirect(req *http.Request, _ []*http.Request) error {
	return fmt.Errorf("panmail: the gateway redirected to %s; refusing to forward the api key", req.URL.Redacted())
}
