// Package panmail is the Go client for sending mail through a panmail
// gateway.
//
// It talks to the same endpoint the web UI uses, authenticating with an API
// key rather than a session. Create the key in Settings → API Keys with the
// email:send scope; the key carries the tenant, so there is nothing else to
// configure.
//
//	client, err := panmail.New("https://mail.example.com", os.Getenv("PANMAIL_API_KEY"))
//	if err != nil {
//		return err
//	}
//
//	result, err := client.Send(ctx, panmail.Message{
//		ProviderID: "0f8b...",
//		From:       "noreply@example.com",
//		To:         []string{"someone@example.org"},
//		Subject:    "Your receipt",
//		HTML:       "<p>Thanks for your order.</p>",
//	})
//
// Send returns once the gateway has written the message to its outbox, not
// once it has been delivered: result.MessageID is what later delivery events
// and webhooks are keyed by.
//
// # Retries
//
// This client does not retry a send whose outcome it does not know. Sending is
// not idempotent and the gateway has no de-duplication key, so a retry after a
// timeout or a dropped connection is a retry of a message that may already be
// on its way to the recipient. The one exception is a refusal — the gateway
// says plainly that it did not accept the message — which is safe to repeat
// and which WithRateLimitRetries turns on.
//
// # The api key
//
// The key travels in X-API-Key rather than Authorization, and a custom header
// is not on net/http's list of credentials to drop when a redirect crosses to
// another host. This client therefore refuses redirects outright rather than
// hand the key to whatever host a Location header named. A Connect procedure
// has no reason to redirect, so the safe reading of one is that something is
// wrong. The same holds for a client passed to WithHTTPClient, which is copied
// and given the same policy.
//
// # Dependencies
//
// This package uses only the standard library. It speaks the Connect
// protocol's JSON mode, which is an ordinary HTTP POST with a JSON body, so
// there is no protobuf runtime and no generated code to keep in step. The wire
// contract it implements is written down in docs/WIRE.md, which is also what
// you want if you are calling the gateway directly rather than through this
// package.
package panmail
