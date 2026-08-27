package panmail

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// APIError is a refusal the client has no more specific type for. Code is the
// Connect error code the gateway sent — "invalid_argument", "internal" and so
// on — and Status is the HTTP status it arrived with.
type APIError struct {
	Code    string
	Message string
	Status  int
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("panmail: the gateway refused the send: %s (http %d)", e.Code, e.Status)
	}
	return fmt.Sprintf("panmail: %s: %s", e.Code, e.Message)
}

// RateLimitedError reports that the tenant is sending faster than its
// configured rate allows. The message was not accepted, so sending it again
// after RetryAfter is safe.
type RateLimitedError struct {
	RetryAfter time.Duration
	err        error
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("panmail: send rate exceeded, retry after %s: %v", e.RetryAfter, e.err)
}

func (e *RateLimitedError) Unwrap() error { return e.err }

// BacklogFullError reports that the tenant already has more queued than its
// own send rate can drain in the near future. The message was not accepted.
//
// Unlike a rate refusal this carries no delay, because there is none to give:
// a queue clearing is not something a caller can schedule against. Retrying on
// a timer will not help, and retrying immediately makes the wait longer for
// everything already queued. Slow down, or stop.
type BacklogFullError struct {
	err error
}

func (e *BacklogFullError) Error() string {
	return fmt.Sprintf("panmail: the tenant's queue is too deep to accept more: %v", e.err)
}

func (e *BacklogFullError) Unwrap() error { return e.err }

// AuthError reports that the API key was missing, rejected, or lacks the
// email:send scope.
type AuthError struct {
	err error
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("panmail: the api key was not accepted: %v", e.err)
}

func (e *AuthError) Unwrap() error { return e.err }

// Connect error codes this client acts on. The protocol sends the code in the
// body as well as mapping it to an HTTP status, and the body is what is
// authoritative — a proxy is free to rewrite a status, and some do.
const (
	codeResourceExhausted = "resource_exhausted"
	codeUnauthenticated   = "unauthenticated"
	codePermissionDenied  = "permission_denied"
)

// classify turns a refusal into something a caller can act on.
//
// The gateway answers both of its capacity refusals with resource_exhausted,
// deliberately: they are the same answer to the client — you are asking for
// more than you may have. What separates them is Retry-After, which the rate
// limiter sets and the backlog check does not, because only one of the two has
// a delay worth quoting. That is the discrimination here, and it is why
// removing Retry-After from the rate refusal would silently reclassify every
// rate limit as a full queue.
func classify(apiErr *APIError, header http.Header) error {
	// Fall back to the HTTP status when the body carried no code, which is what
	// a proxy returning its own error page looks like.
	code := apiErr.Code
	if code == "" {
		switch apiErr.Status {
		case http.StatusTooManyRequests:
			code = codeResourceExhausted
		case http.StatusUnauthorized:
			code = codeUnauthenticated
		case http.StatusForbidden:
			code = codePermissionDenied
		}
	}

	switch code {
	case codeResourceExhausted:
		if retryAfter, ok := retryAfter(header); ok {
			return &RateLimitedError{RetryAfter: retryAfter, err: apiErr}
		}
		return &BacklogFullError{err: apiErr}
	case codeUnauthenticated, codePermissionDenied:
		return &AuthError{err: apiErr}
	default:
		return apiErr
	}
}

func retryAfter(header http.Header) (time.Duration, bool) {
	value := header.Get("Retry-After")
	if value == "" {
		return 0, false
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 0 {
		// A header this client cannot read is not a reason to call the refusal
		// something else: it is still a rate limit, just one with no usable
		// delay attached.
		return 0, true
	}
	return time.Duration(seconds) * time.Second, true
}
