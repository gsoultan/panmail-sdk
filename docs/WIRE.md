# The panmail wire contract

Everything an SDK does, written down — so you can do it without one.

There are three doors into a panmail gateway and they all end in the same
place: one `SendEmailUsecase`, which means the tenant's send rate, the backlog
ceiling, suppressions and the provider's `AllowedDomains` apply identically
whichever door you came through.

| Door | Use it when |
| --- | --- |
| **HTTP JSON** | Anything that can POST. This is what the SDKs speak. |
| **SMTP submission** | Your application already speaks SMTP and you would rather not change it. |
| **The dashboard** | A human is sending. |

---

## HTTP JSON

The gateway serves the [Connect protocol](https://connectrpc.com/docs/protocol),
whose JSON mode is an ordinary HTTP POST. No protobuf runtime required.

### The request

```
POST https://mail.example.com/panmail.v1.EmailService/SendEmail
Content-Type: application/json
X-API-Key: <your key>
```

> **Not `Authorization`.** That header carries a *dashboard session*. A key sent
> as a bearer token is rejected as a malformed session rather than as a bad key,
> which is a confusing way to learn you used the wrong header.

The key carries the tenant, so there is nothing else to configure. Create one in
**Settings → API Keys** with the `email:send` scope.

```json
{
  "providerId": "3f1c2b7a-0000-4000-8000-000000000001",
  "from": "noreply@example.com",
  "to": ["someone@example.org"],
  "cc": [],
  "bcc": [],
  "subject": "Your receipt",
  "bodyHtml": "<p>Thanks for your order.</p>",
  "bodyText": "Thanks for your order.",
  "templateId": "",
  "templateData": {},
  "attachments": [
    {
      "filename": "receipt.pdf",
      "contentType": "application/pdf",
      "content": "JVBERi0xLjQ="
    }
  ]
}
```

| Field | Notes |
| --- | --- |
| `providerId` | **Required.** The gateway will not guess which provider a message goes out through — the wrong guess is a message sent from the wrong domain. |
| `from` | **Required.** Must be an address the provider is authorised to send as. |
| `to` / `cc` / `bcc` | At least one address across the three. |
| `bodyHtml` / `bodyText` | Send both when you can: the text part is what recipients with images off, screen readers and spam filters read. |
| `templateId` | Renders a stored template with `templateData` instead of the bodies. Subject comes from the template unless set here. |
| `templateData` | A plain JSON object (`google.protobuf.Struct` on the wire — JSON and nothing more). |
| `attachments[].content` | **base64.** It is a protobuf `bytes` field; raw bytes arrive as mojibake. |

Field names are the protobuf JSON names. The gateway also accepts the
snake_case spellings (`provider_id`, `body_html`), but camelCase is what its own
web UI sends and so the better-travelled path.

`body` and `isHtml` exist and are **deprecated** — use `bodyHtml` / `bodyText`.

### The response

`200 OK`:

```json
{ "messageId": "0193b2f1-...", "status": "EMAIL_EVENT_TYPE_PENDING" }
```

> **`PENDING` is not a promise of delivery.** A filter rule can quarantine a
> message for review, and the gateway answers that with the same `messageId` and
> the same `PENDING` status as an accepted one — byte for byte, with nothing in
> the response to tell them apart. A successful send means the gateway took
> responsibility for *deciding* what happens next, which is either delivering
> the message or putting it in front of a person. Subscribe to
> `WEBHOOK_TRIGGER_EVENT_MAIL_HELD` if that distinction matters, and to
> `MAIL_EXPIRED`, which is a hold nobody reviewed in time.

`messageId` identifies the message for the rest of its life — delivery events,
webhooks and the analytics pages are all keyed by it. Store it next to whatever
prompted the send.

`status` is an enum **name**, not a number (that is how protobuf JSON encodes
enums). A successful send is `EMAIL_EVENT_TYPE_PENDING`: the gateway has the
message on disk and will deliver it. Delivery is reported later. Other values
you may see on the event stream include `EMAIL_EVENT_TYPE_SENT`,
`EMAIL_EVENT_TYPE_DELIVERED`, `EMAIL_EVENT_TYPE_BOUNCED` and
`EMAIL_EVENT_TYPE_DROPPED`.

### Errors

Non-200 responses carry a Connect error envelope:

```json
{ "code": "resource_exhausted", "message": "..." }
```

| Code | HTTP | Meaning |
| --- | --- | --- |
| `invalid_argument` | 400 | The message was malformed. |
| `unauthenticated` | 401 | Key missing or rejected. |
| `permission_denied` | 403 | Key lacks the `email:send` scope. |
| `failed_precondition` | 400 | A recipient is on the tenant's suppression list. |
| `resource_exhausted` | 429 | **Two different things — see below.** |
| `unknown` | 500 | Something failed rather than refused. Retry with backoff. |

#### A refusal is not a failure

The split worth internalising is not which code you got but which kind of answer
it is. A **refusal** is a decision: the same request is answered the same way
until somebody changes something, so retrying spends attempts on an answer that
cannot move. Only `unknown` means the gateway might succeed if asked again.

| Answer | Kind | What to do |
| --- | --- | --- |
| `invalid_argument` | refusal | Fix the request — the provider or template does not exist, or the template will not render against the data sent. Retrying never helps. |
| `unauthenticated`, `permission_denied` | refusal | Fix the key or its scopes. |
| `failed_precondition` | refusal | A recipient is suppressed. The **whole** message is refused, not just that recipient's copy. Remove the address from the send, or lift the suppression. |
| `resource_exhausted` | refusal | You are asking for more than you may have. The only one with a schedule attached, and only sometimes — see below. |
| `unknown` | failure | Storage, a provider connection. Retry with backoff. |

Suppression is worth calling out because it is the one that reads like a
per-recipient problem and is not:

```json
{ "code": "failed_precondition",
  "message": "recipient bounced@example.net is suppressed: hard bounce" }
```

Nothing was sent to anybody. The SDKs give this its own type —
`SuppressedRecipientError`, `SuppressedRecipientException` — so a caller can act
on it without reading the message, though the address and the reason are only in
the message.

> This used to be worse, and recently. Before the gateway had codes for its
> refusals, everything except the two capacity ones arrived as `unknown` with a
> 500, so a suppressed address was indistinguishable from the gateway having
> broken — and a caller doing the obvious thing with a 500 retried it forever.

#### The one subtlety worth knowing

**Both capacity refusals answer `resource_exhausted` / 429.** That is
deliberate: they are the same answer to you — *you are asking for more than you
may have.* What separates them is the **`Retry-After` response header**:

| | `Retry-After` | What to do |
| --- | --- | --- |
| **Over the send rate** | **present**, in seconds | Safe to repeat after the delay. |
| **Backlog full** | **absent** | Do not retry on a timer. The queue clearing is not something you can schedule against, and retrying immediately makes the wait longer for everything already queued. Slow down, or stop. |

Every SDK in this repo keys off exactly that presence. If you write your own
client, key off it too.

"The send rate" is two rates. Each provider can have its own ceiling as well as
the tenant's, and the gateway decides both in one step so that a send one
provider refuses does not spend the tenant's allowance. The refusal does not
say which was hit. `Retry-After` is correct either way — it is how long until
the combined decision would allow the send — but it cannot tell you that a
different provider might have taken the message now.

The backlog check is the tenant's only. The outbox is shared by every provider,
so a per-provider rate says nothing about how deep it is.

#### Retrying

**Do not retry a send whose outcome you do not know.** Sending is not
idempotent and the gateway has no de-duplication key, so a retry after a
timeout or a dropped connection is a retry of a message that may already be on
its way to the recipient.

A *refusal* is different — the gateway said plainly it did not accept the
message — and is safe to repeat. That is the only retry the SDKs perform, and
only for a rate limit, and only when asked.

### cURL

```bash
curl -X POST "https://mail.example.com/panmail.v1.EmailService/SendEmail" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: $PANMAIL_API_KEY" \
  -d '{
    "providerId": "3f1c2b7a-0000-4000-8000-000000000001",
    "from": "noreply@example.com",
    "to": ["someone@example.org"],
    "subject": "Your receipt",
    "bodyText": "Thanks for your order."
  }'
```

---

## SMTP submission

For an application that already speaks SMTP. Same pipeline, same limits.

### Authentication

`AUTH PLAIN` / `AUTH LOGIN`, where:

- **password** — your API key
- **username** — the **provider UUID**

SMTP has no field for `provider_id`, and the gateway will not guess one, so it
is resolved in this order:

1. an `X-Panmail-Provider-Id` header on the message — stripped before sending
2. the **AUTH username**

Per-message routing therefore uses the header; a single-provider application can
just put the UUID in the username and forget about it.

### Bcc is the envelope, not a header

The envelope recipients (`RCPT TO`) and the header recipients are different
lists, and the difference **is** the Bcc. The gateway computes
`envelope − (To ∪ Cc)` and treats the remainder as blind.

So: put every recipient in `RCPT TO`, and put only `To`/`Cc` in the message
headers. Writing a `Bcc:` header instead exposes every blind address to every
recipient. Libraries that model the envelope for you — PHPMailer, Jakarta Mail —
get this right on their own; Go's `net/smtp` hands you both lists and lets you
get it wrong.

### Reply codes

| Code | Enhanced | Meaning |
| --- | --- | --- |
| `451` | policy refusal | Over the send rate. The delay is in the text — SMTP has no `Retry-After`. |
| `452` | no storage | Backlog full. Includes the pending count. |
| `451` | temp failure | Anything the gateway could not classify. Deliberately temporary: an unknown error should not make a sender discard the message. |
| `501` | — | Bad arguments. |
| `535` | — | Authentication failed. |
| `550` | — | Not authorised — for instance a `From` outside the provider's `AllowedDomains`. |

---

## Delivery events and webhooks

Everything above is the *send*. What happens to a message afterwards is
reported separately, keyed by the `messageId` a send returned, as one of the
values of `EmailEventType` in
[`event.proto`](../proto/panmail/v1/event.proto) — fifteen of them, from
`EMAIL_EVENT_TYPE_SENT` through the hard and soft bounce split to
`EMAIL_EVENT_TYPE_COMPLAINED`. The SDKs expose all fifteen as constants so a
receiver can compare against them rather than spell them out.

Those events reach your application as an HTTP POST to a URL you register.

### The request

```
POST https://your-app.example.com/hooks/panmail
Content-Type: application/json
X-Panmail-Event: WEBHOOK_TRIGGER_EVENT_MAIL_BOUNCED
X-Panmail-Delivery: 0193b2f1-...
X-Panmail-Timestamp: 1700000000
X-Panmail-Signature: sha256=7efcc11dba053daf...
```

| Header | What it is |
| --- | --- |
| `X-Panmail-Event` | A `WebhookTriggerEvent` name, verbatim — `WEBHOOK_TRIGGER_EVENT_MAIL_BOUNCED`. **Not** a dotted `mail.bounced`, and **not** an `EMAIL_EVENT_TYPE_*` value. Lets you route without parsing the body. |
| `X-Panmail-Delivery` | Stable across retries of the same notification. This is what you deduplicate on. |
| `X-Panmail-Timestamp` | Unix seconds. The second half of what is signed. |
| `X-Panmail-Signature` | `sha256=` followed by the HMAC, hex encoded. |

The body is an envelope with the event-specific payload inside it:

```json
{
  "event": "WEBHOOK_TRIGGER_EVENT_MAIL_BOUNCED",
  "tenant_id": "3f1c2b7a-0000-4000-8000-000000000001",
  "timestamp": 1700000000,
  "data": { "messageId": "0193b2f1-...", "reason": "mailbox full" }
}
```

### The event vocabulary

The `event` field and the `X-Panmail-Event` header both carry a value of
[`WebhookTriggerEvent`](../proto/panmail/v1/webhook.proto), spelled exactly as
the enum spells it. The gateway dispatches with `event.String()`, so there is no
translation step and no dotted form:

```
WEBHOOK_TRIGGER_EVENT_MAIL_SENT       WEBHOOK_TRIGGER_EVENT_MAIL_HELD
WEBHOOK_TRIGGER_EVENT_MAIL_DELIVERED  WEBHOOK_TRIGGER_EVENT_MAIL_RELEASED
WEBHOOK_TRIGGER_EVENT_MAIL_OPENED     WEBHOOK_TRIGGER_EVENT_MAIL_QUARANTINE_REJECTED
WEBHOOK_TRIGGER_EVENT_MAIL_CLICKED    WEBHOOK_TRIGGER_EVENT_MAIL_EXPIRED
WEBHOOK_TRIGGER_EVENT_MAIL_BOUNCED    WEBHOOK_TRIGGER_EVENT_MAIL_INBOUND
WEBHOOK_TRIGGER_EVENT_MAIL_REJECTED   WEBHOOK_TRIGGER_EVENT_UNSPECIFIED
```

> **Do not match on `mail.bounced`.** Dotted names appear in the gateway's own
> tests, where they are arbitrary strings handed to a `string` parameter, and it
> is an easy mistake to read one as the vocabulary. A handler matching a dotted
> name compiles, runs, and never fires — which is the worst way to be wrong,
> because nothing reports it.

Two of these are easy to confuse and mean opposite things.
`MAIL_REJECTED` is a *provider* refusing a send, reported by the delivery
pipeline. `MAIL_QUARANTINE_REJECTED` is a *person* refusing a message a filter
rule held. A subscriber acting on "rejected" needs to know which it received.

`MAIL_EXPIRED` is the one worth alerting on: a held message hit its retention
deadline with nobody having decided. It does not say a message was refused, it
says a review queue went unwatched.

The SDKs expose all twelve — `panmail.TriggerEventMailHeld`,
`TriggerEvent::MAIL_HELD` — checked against
[`testdata/webhook-events.json`](../testdata/webhook-events.json), which is
generated from the proto.

### Verifying it

```
signature = "sha256=" + hex(HMAC_SHA256(secret, timestamp + "." + raw_body))
```

The timestamp is inside the signed material, not just alongside it. Signing the
body alone would mean a notification captured once could be replayed forever
against the same signature; with the timestamp covered, a receiver checks it is
recent and the signature proves it was not moved.

Four things a verifier has to do, and each of them matters:

1. **Compare in constant time.** A byte-by-byte comparison that returns early
   tells an attacker how much of a guess was right.
2. **Check the timestamp is recent, in both directions.** Only checking the
   past lets a forged future timestamp keep a capture valid indefinitely. Five
   minutes is a reasonable window; wider is weaker.
3. **Use the bytes as they arrived.** The signature covers what was sent, down
   to key order and whitespace. Re-serialising a parsed payload will not
   verify — read the raw body first and decode afterwards.
4. **Refuse a delivery with no signature.** A subscription with no secret is
   delivered *unsigned* rather than not delivered, so this is a real request
   arriving at a real endpoint. Accepting it makes the whole exercise
   decorative. Set a secret on the subscription.

The SDKs do all four:

```go
event, err := panmail.VerifyWebhook(secret, r.Header, body)
```
```php
$event = Panmail\Webhook::verify($secret, getallheaders(), file_get_contents('php://input'));
```

[`testdata/webhook-signatures.json`](../testdata/webhook-signatures.json) holds
known-good vectors if you are implementing this yourself.

### Retries, and why you need the delivery id

The gateway retries a delivery that fails, backing off, and treats a 4xx other
than 408 and 429 as a permanent refusal — it has looked at your response and
believed you. `X-Panmail-Delivery` is stable across every retry of the same
notification, so store it and ignore a repeat. Without that, a bounce handler
that suppresses an address will suppress it several times, and one that issues
a refund will issue several.

---

## Generating your own client

The three protos that make up `SendEmail`'s import closure are published in
[`../proto`](../proto), copied verbatim from the gateway. You do not need them
to use an SDK or to POST JSON — they are there for the case where you would
rather generate a client than write one.
