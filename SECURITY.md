# Security

## Reporting a vulnerability

Report privately through
[GitHub's advisory form](https://github.com/gsoultan/panmail-sdk/security/advisories/new).
Please do not open a public issue for a vulnerability.

Include what you need to reproduce it: the SDK and language version, what you
sent, and what happened. You should get an acknowledgement within a few days.

## What this SDK protects

An API key is a sending credential for a whole tenant, so the client treats it
as the thing most worth not losing.

**The key is never sent anywhere but the endpoint you configured.** None of the
four clients follows an HTTP redirect. This is deliberate rather than
incidental: the key travels in `X-API-Key`, and a custom header is not on any
HTTP client's list of credentials to drop when a redirect crosses to another
host — that list knows about `Authorization` and `Cookie`. Following a redirect
would hand the key to whatever host the `Location` header named. A Connect
procedure has no reason to redirect, so the clients refuse one instead.

If you supply your own HTTP client, keep that property:

| Language | How |
| --- | --- |
| Go | The client is copied and given a `CheckRedirect` that refuses. Nothing to do. |
| Java | A supplied `HttpClient` must be built with `followRedirects(Redirect.NEVER)`, or the constructor refuses it. |
| Node | `redirect: 'error'` is set on every request. |
| PHP | A custom `Transport` must not enable `CURLOPT_FOLLOWLOCATION`. |

**A response is bounded before it is read.** Every client stops at 1 MiB. A send
response is a message id and a status; anything approaching that size is a
proxy's error page, and buffering it is how a misbehaving intermediary turns
into an out-of-memory error in your process.

**The gateway must be reached over https.** `http://` is refused unless the
host is loopback — `localhost`, anything in `127.0.0.0/8`, or `::1` — because a
gateway on this machine has no network to listen on. Anywhere else, plaintext
would put a tenant-wide sending credential on the wire in the clear, and a
typo'd or copy-pasted `http://` production URL is not something to discover
from a packet capture.

**The base URL cannot carry credentials.** `https://user:pass@gateway` is
refused by all four. The URL travels into every error the client reports, and
an error is a thing that gets logged — Node's `fetch` refuses such a URL anyway,
but by throwing a `TypeError` that quotes the whole thing, password included.
The API key authenticates the send; if something in front of the gateway wants
basic auth as well, set the header explicitly.

**A header cannot be made to carry a second header.** A carriage return or
newline ends a header line, so a value holding one is an extra header the
caller never wrote — and a tracing or correlation header built out of something
a user supplied is exactly where that happens. All four clients refuse a header
name outside RFC 9110's token set, and a value carrying any control character,
when the client is constructed rather than on the first send.

**The key cannot be set from two places.** The per-request header options
(`WithHeader`, `headers`, `header()`) will not set `X-API-Key`, case
insensitively. A second source for it would only make a mismatch possible.

**A send is never retried when the outcome is unknown.** Not strictly a
security property, but the same reasoning: sending is not idempotent and the
gateway has no de-duplication key, so a retry after a timeout may be a second
copy of a message already on its way to a recipient.

## What the SDKs do not cover

**Webhooks.** Delivery events reach your application through a webhook, and
neither the SDKs nor `docs/WIRE.md` say anything about how to verify one. There
is no signature scheme specified, so there is nothing for a client to check.

Until there is, a webhook endpoint is an unauthenticated write path: an event
claiming a message bounced is something anyone who finds the URL can send. Do
not let one drive a suppression, a refund or a resend on its own — check it
against the `messageId` you stored when you sent, and treat anything you cannot
match as untrusted.

## What checks this

The properties above are each held by a test in all four languages, so a change
that removes one turns the others red rather than shipping quietly. Beyond that:

- **CodeQL** runs on every push and weekly, over Go, Java and TypeScript, with
  the `security-and-quality` queries.
- **PHPStan at `max`** covers the shipped PHP, which CodeQL does not analyse —
  and which is where the one injectable bug found so far actually was.
- **golangci-lint** includes `gosec`, and `errorlint`, which guards the
  `errors.As` every typed refusal depends on.

## Supported versions

Fixes go to the latest minor release.
