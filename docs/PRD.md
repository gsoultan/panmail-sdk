# panmail-sdk — Product Requirements

Status: living document. Last revised 2026-10-10.

The gateway's own requirements are in
[gsoultan/panmail `docs/PRD.md`](https://github.com/gsoultan/panmail/blob/main/docs/PRD.md);
this page covers only what the clients promise. The wire contract they speak is
[`WIRE.md`](WIRE.md).

## Problem

An application that sends through panmail needs three things from a client:
one call that sends, an answer it can act on when the gateway refuses, and the
certainty that the client never sends a message twice on its behalf — which,
with an idempotency key, includes being able to ask again safely. Anything
beyond that is surface the application has to learn and the client has to keep
in step across two languages.

## Users

- **Application developers** in Go or PHP who send transactional mail through a
  panmail tenant with an API key.
- **Operators** who receive panmail's outbound webhooks and must verify them.

## Requirements

### Sending

| ID | Requirement |
| --- | --- |
| C1 | One call, `Send` / `send`, posts `EmailService.SendEmail` in Connect JSON mode and returns the message id and status. |
| C2 | Refusals are typed by what they mean on that procedure: rate limited (with `RetryAfter`), backlog full, suppressed recipient, idempotency key reused for a different request, invalid message, authentication, and a generic API error with the Connect code. |
| C3 | **No retry of an unknown outcome without an idempotency key.** A transport error or timeout on a send without a key is reported, never retried. A rate-limit refusal may be repeated when the caller opts in (`WithRateLimitRetries` / `rateLimitRetries`). A send that carries a key (`Message.IdempotencyKey` / `idempotencyKey`) may also have an unknown outcome — a request that did not complete, or the gateway's `aborted` — repeated, when the caller opts in separately (`WithTransportRetries` / `transportRetries`), with backoff. Both within the caller's deadline (Go's context; PHP bounds each attempt by its timeout and the attempts by count). |
| C4 | `ListProviders` / `listProviders` pages through the tenant's providers (key needs `providers:read`), bounded in pages. |
| C5 | An idempotency key is validated against the gateway's rule (at most 255 printable ASCII characters) before it is sent, and sent only in the body, never as the `Idempotency-Key` header. |
| C6 | `MessageEvents` / `messageEvents` returns one message's delivery events, oldest first (key needs `events:read`), bounded in pages; an empty message id is refused before it is sent, because the gateway reads it as no filter. |

### Safety of the key

| ID | Requirement |
| --- | --- |
| K1 | The key travels only in `X-API-Key`; no redirect is ever followed. |
| K2 | `https` is required except for loopback hosts. |
| K3 | The key cannot be set a second time through custom headers. |

### Bounded cost

| ID | Requirement |
| --- | --- |
| B1 | At most 1 MiB of a response is read. |
| B2 | After that bound nothing more is downloaded: Go discards at most 64 KiB before closing; PHP aborts the transfer. A body that never ends cannot hold a call open past the point its result is known. |
| B3 | A refusal cut short at the bound is still classified from its status, code and headers, identically in both languages. A success cut short is an error. |
| B4 | The message an error carries is at most 4 KiB, cut on a character boundary. |
| B5 | Connections are reused: Go keeps the caller's transport; PHP keeps one cURL handle per transport, reset between requests. |

### Webhooks

| ID | Requirement |
| --- | --- |
| W1 | Signature verification in constant time over exactly the bytes the gateway signs. |
| W2 | Timestamp tolerance checked in both directions, overflow-safe. |

## Non-functional

- **Parity.** Go and PHP behave the same, with the same test names in both
  suites, or the difference is written down in `CONTRIBUTING.md`.
- **Dependencies.** Go: standard library only. PHP: ext-curl and ext-json.
- **Verification.** `scripts/check.sh` runs what CI runs; coverage floors are
  95% in both languages; every fix ships a test that fails before it.

## Out of scope

- A Node or Java client (use the HTTP API or the gateway's SMTP door).
