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

**The key cannot be set from two places.** The per-request header options
(`WithHeader`, `headers`, `header()`) will not set `X-API-Key`, case
insensitively. A second source for it would only make a mismatch possible.

**A send is never retried when the outcome is unknown.** Not strictly a
security property, but the same reasoning: sending is not idempotent and the
gateway has no de-duplication key, so a retry after a timeout may be a second
copy of a message already on its way to a recipient.

## Supported versions

Fixes go to the latest minor release.
