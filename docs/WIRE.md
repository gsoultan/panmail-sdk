# The panmail wire contract

Everything an SDK does, written down — so you can do it without one.

There are three doors into a panmail gateway and they all end in the same
place: one `SendEmailUsecase`, which means the tenant's send rate, the backlog
ceiling, suppressions and the provider's allowed senders apply identically
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
  ],
  "idempotencyKey": "order-1042/receipt",
  "replyTo": "",
  "inReplyTo": "",
  "references": [],
  "dispositionNotificationTo": "",
  "saveSentCopy": false
}
```

| Field | Notes |
| --- | --- |
| `providerId` | **Required.** The gateway will not guess which provider a message goes out through — the wrong guess is a message sent from the wrong domain. The ids are on the Email Providers page, or [listed over the same door](#listing-providers). |
| `from` | **Required.** Must be an address the provider is authorised to send as. |
| `to` / `cc` / `bcc` | At least one address across the three. |
| `bodyHtml` / `bodyText` | Send both when you can: the text part is what recipients with images off, screen readers and spam filters read. |
| `templateId` | Renders a stored template with `templateData` instead of the bodies. Subject comes from the template unless set here. |
| `templateData` | A plain JSON object (`google.protobuf.Struct` on the wire — JSON and nothing more). |
| `attachments[].content` | **base64.** It is a protobuf `bytes` field; raw bytes arrive as mojibake. |
| `attachments[].contentId` | Optional. Makes the attachment an inline part the HTML refers to as `cid:<contentId>`. Without angle brackets; printable ASCII, no spaces, at most 255 characters. |
| `replyTo` | Optional. Where replies go, when not `from`. |
| `inReplyTo` / `references` | Optional. Thread this message with earlier ones: the Message-ID being answered, and the conversation's Message-IDs oldest first. Each is `<left@right>` with the angle brackets, printable ASCII, at most 255 characters; at most 100 references. When they do not fit one header line the gateway keeps the first and the most recent. |
| `dispositionNotificationTo` | Optional. Asks for a read receipt at this address; the recipient's client decides whether to send one. |
| `saveSentCopy` | Optional. Files a copy in the sender's Sent folder as the tenant's Sent copies setting says; ignored when that is off. SMTP submission sets it. |
| `idempotencyKey` | Optional. Makes a repeated send safe — see [Idempotency keys](#idempotency-keys). At most 255 printable ASCII characters (`0x20`–`0x7E`). |

Field names are the protobuf JSON names. The gateway also accepts the
snake_case spellings (`provider_id`, `body_html`), but camelCase is what its own
web UI sends and so the better-travelled path.

The reply, threading, read-receipt, Sent-copy and `contentId` fields arrived
after gateway v2.0.0 (gsoultan/panmail#74 and #75). An older gateway discards
them without an error.

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
| `already_exists` | 409 | The `idempotencyKey` was already used for a different request. |
| `aborted` | 409 | A concurrent send with the same `idempotencyKey` holds it; ask again. |
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
| `failed_precondition` | refusal | A recipient is suppressed. The **whole** message is refused, not just that recipient's copy. Remove the address from the send, or lift the suppression — see [Suppressions](#suppressions). |
| `resource_exhausted` | refusal | You are asking for more than you may have. The only one with a schedule attached, and only sometimes — see below. |
| `already_exists` | refusal | The key names a different message. Nothing was sent; choose keys that name what is being sent. |
| `aborted` | unknown outcome | Only for a keyed send racing another with the same key. Asking again with the same key returns the first message. |
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

**Do not retry a send whose outcome you do not know — unless it carries an
idempotency key.** Without one, a retry after a timeout or a dropped
connection is a retry of a message that may already be on its way to the
recipient.

A *refusal* is different — the gateway said plainly it did not accept the
message — and is safe to repeat. The SDKs repeat a rate limit, and only when
asked; with a key, and only when asked, they also repeat an unknown outcome.

### Idempotency keys

The field is `string idempotency_key = 14` on `SendEmailRequest`, added by
gsoultan/panmail#72. A gateway older than that ignores it.

```json
{ "providerId": "…", "from": "…", "to": ["…"], "bodyText": "…",
  "idempotencyKey": "order-1042/receipt" }
```

The gateway remembers an accepted key, per tenant, for **24 hours**, together
with the message it accepted and a hash of the request:

| The same key arrives with… | Answer |
| --- | --- |
| the same request | `200`, the **original** `messageId`, status `EMAIL_EVENT_TYPE_PENDING`. Nothing new is queued. |
| a different request | `already_exists` / 409, `"idempotency key reused with a different request"`. Nothing is sent. |
| the same request, while the first is still being written | `aborted` / 409. Ask again; you will get the first message. |

Only acceptance is remembered. A refusal — a rate limit, a suppressed
recipient, an invalid request — stores nothing, so the next attempt with the
same key is decided afresh.

A key may instead travel in an `Idempotency-Key` header; sent in both places
and different, it is `invalid_argument`. The SDKs send only the field. Go's
`net/http` treats a request carrying that header as safe to resend and does so
by itself after a dropped connection, which would be a retry nobody counted.

Choose a key that names **what is being sent**, not the attempt: an order id
and the kind of mail, not a random value made per call. A key minted per
attempt makes every retry a new message.

> **A gateway that predates the key ignores it.** Connect's JSON decoding
> discards fields it does not know, so an older gateway accepts the send and
> forgets the key. The request looks the same either way; nothing in the
> response says the key was honoured. Retrying an unknown outcome is only
> safe against a gateway known to support keys, which is why the SDKs make it
> a separate opt-in.

What the SDKs do with a key:

- Send it as `idempotencyKey`, after checking the gateway's own rule (255
  printable ASCII characters) so a bad key fails before the round trip.
- Report `already_exists` on a send as its own type —
  `IdempotencyKeyConflictError`, `IdempotencyKeyConflictException`.
- With `WithTransportRetries(n)` / `transportRetries`, and only for a keyed
  send, repeat up to `n` times a send whose outcome is unknown: one that did
  not complete — a dropped connection, a timeout, a success cut off at the
  response bound — or was answered `aborted`. Each repeat waits first, 0.1 s
  doubling to at most 2 s. Go stops at the context's deadline. Every refusal,
  `unknown` included, is a known outcome and is not repeated this way.

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
| `550` | — | Not authorised — for instance a `From` outside the provider's allowed senders. |

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
  "data": {
    "id": "0193b2f1-…",
    "tenant_id": "3f1c2b7a-…",
    "provider_id": "3f1c2b7a-…",
    "provider_name": "Production SES",
    "message_id": "0193b2f1-…",
    "type": 6,
    "recipient": "gone@example.org",
    "subject": "Your receipt",
    "timestamp": "2026-09-01T10:00:02.123456789Z",
    "metadata": { "smtp_code": "550" },
    "error_message": "550 5.1.1 mailbox unknown"
  }
}
```

### The payloads

The gateway builds the envelope and its `data` with Go's `encoding/json`, not
protobuf JSON, so the payloads follow the gateway's **Go struct tags** rather
than the protos: field names are `snake_case`, a delivery event's `type` is
the `EmailEventType` **number** (`6` is `HARD_BOUNCE`), and an inbound
message's timestamp is `{"seconds": …, "nanos": …}`. None of this is in a
proto, so the drift check cannot see it change.
[`testdata/webhook-payloads.json`](../testdata/webhook-payloads.json) holds
one body of each shape, byte for byte as the gateway built it.

| Events | `data` | Fields |
| --- | --- | --- |
| `MAIL_SENT`, `MAIL_DELIVERED`, `MAIL_OPENED`, `MAIL_CLICKED`, `MAIL_BOUNCED`, `MAIL_REJECTED` | a delivery event | `id`, `tenant_id`, `provider_id`, `provider_name`, `message_id`, `type` (number), `recipient`, `subject`, `timestamp` (RFC 3339), `metadata` (object or `null`), `error_message` |
| `MAIL_HELD` | a hold | `id` (the review-queue id), `direction` (`outbound`/`inbound`), `message_id`¹, `rule_id`¹, `rule_name`, `from`, `recipients`, `subject`, `size_bytes`, `attachment_count`, `attachment_names`¹, `matched` (`field`, `operator`, `values`¹, `header`¹, `number`¹), `held_at`, `expires_at`¹ |
| `MAIL_RELEASED`, `MAIL_QUARANTINE_REJECTED`, `MAIL_EXPIRED` | how a hold ended | `id`, `direction`, `message_id`¹, `rule_id`¹, `rule_name`, `from`, `recipients`, `subject`, `held_at`, `decided_at`, `reviewed_by`¹, `note`¹ — no reviewer on an expiry |
| `MAIL_INBOUND` | an inbound message | `id`¹, `tenant_id`¹, `from`¹, `to`¹, `subject`¹, `body_html`¹, `body_text`¹, `timestamp` (`{seconds, nanos}`)¹, `headers`¹ |

¹ Left out when empty.

The trigger is coarser than the event: `MAIL_BOUNCED` fires for `BOUNCED`,
`HARD_BOUNCE` and `SOFT_BOUNCE`, and `MAIL_REJECTED` for `REJECTED` and
`DROPPED`. Read `type` for which.

A hold and its outcome carry no body and no attachment content. An inbound
message carries both bodies.

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

### Typed payloads

`ParseWebhook` in Go and `Webhook::parse` in PHP verify exactly as above, then
decode `data` by the event: `MailEvent`, `MailHeld`, `MailHoldOutcome` or
`InboundMail`. A delivery event's `type` number comes back as its `Status`
name, and an inbound timestamp is read in either form.

- **An event they have no type for is not refused.** It comes back verified,
  with no payload and the raw data alongside. The gateway adds events, and a
  receiver that refuses one gets it retried until the gateway gives up.
- **A known event whose data is the wrong shape is refused** with the webhook
  error, naming the event. It was signed, so it is the format moving, not an
  attack — but a payload decoded halfway hands back zero values nobody can
  tell from real ones. A field may be absent or `null`; one that is present
  must be the type the gateway writes.

```go
parsed, err := panmail.ParseWebhook(secret, r.Header, body)
if bounce, ok := parsed.Payload.(*panmail.MailEvent); ok && bounce.Type == panmail.StatusHardBounce { … }
```
```php
$parsed = Panmail\Webhook::parse($secret, getallheaders(), file_get_contents('php://input'));
if ($parsed->payload instanceof Panmail\MailEvent && $parsed->payload->type === Panmail\Status::HARD_BOUNCE) { … }
```

### Retries, and why you need the delivery id

The gateway retries a delivery that fails, backing off, and treats a 4xx other
than 408 and 429 as a permanent refusal — it has looked at your response and
believed you. `X-Panmail-Delivery` is stable across every retry of the same
notification, so store it and ignore a repeat. Without that, a bounce handler
that suppresses an address will suppress it several times, and one that issues
a refund will issue several.

---

## Listing providers

Every send names a provider. The ids are on the dashboard's Email Providers
page, and the procedure that page reads them from takes an API key too.

### The request

```
POST https://mail.example.com/panmail.v1.EmailProviderService/ListEmailProviders
Content-Type: application/json
X-API-Key: <your key>
```

The key needs the **`providers:read`** scope, which no key gets by default. One
minted for sending holds `email:send` alone and is refused with
`permission_denied`: `api key is missing the "providers:read" scope`.

```json
{ "pageSize": 50, "pageToken": "NTA=", "name": "prod", "type": "PROVIDER_TYPE_SMTP" }
```

| Field | Notes |
| --- | --- |
| `pageSize` | Zero or absent is 20. There is no maximum. |
| `pageToken` | The previous page's `nextPageToken`; absent for the first page. Treat it as opaque. |
| `name` | Keeps providers whose name **contains** it, ignoring case — a search, not a lookup. The gateway matches it with SQL `LIKE` and does not escape it, so `%` and `_` are wildcards. |
| `type` | Keeps one `ProviderType`, by enum name. Absent is every type. |

> **A misspelt `type` is not refused. It is ignored.** Connect decodes JSON with
> unknown values discarded, and that includes an enum name it does not know:
> `"type": "SMTP"` arrives as no filter at all, and the answer is every
> provider. Spell the name in full, and check the types that come back — the
> SDKs do the second for you.

### The response

`200 OK`:

```json
{
  "providers": [
    {
      "id": "3f1c2b7a-0000-4000-8000-000000000001",
      "name": "Production SES",
      "type": "PROVIDER_TYPE_SES",
      "allowedSenders": ["*@example.com", "billing@example.org"],
      "allowedDomains": ["example.com"],
      "sendRatePerMinute": 600,
      "sendBurst": 100,
      "createTime": "2026-09-01T10:00:00Z",
      "updateTime": "2026-09-01T10:00:00Z",
      "tenantId": "…",
      "ses": { "region": "eu-west-1", "accessKey": "AKIA…" }
    }
  ],
  "nextPageToken": "NTA="
}
```

Newest first. `nextPageToken` is absent on the last page — and present on any
full page, even when nothing follows it, so a list that is an exact multiple of
`pageSize` ends with one empty page.

`allowedSenders` are the From addresses the provider will send as. Each entry
is an exact address (`billing@example.org`) or a whole domain
(`*@example.com`), lower case. Matching ignores case and the display name, and
a domain entry does not admit its subdomains: `*@example.com` does not admit
`ops@mail.example.com`. Empty, or absent, means the operator has not restricted
the provider.

`allowedDomains` is deprecated. A current gateway still sends it, holding only
the `*@domain` entries without the `*@`, for clients written before addresses
could be listed; read `allowedSenders` instead. A gateway from before then
sends `allowedDomains` alone, and the SDKs read each of its domains as
`*@domain`.

`type` is an enum name, with one exception. A stored value the enum has no
name for is written as a bare number — that is how protobuf JSON encodes one —
and `provider_type.proto` reserves 2 to 5 because old rows may still carry them.
A client that insists on a string fails the whole list on one such row.

`PROVIDER_TYPE_IMAP` and `PROVIDER_TYPE_POP3` providers are listed too. They are
mailboxes inbound mail is read from, and the gateway passes over them when it
picks a provider to send through.

**No credential is in it.** The gateway clears every password, API key, secret
key, token and DKIM private key before answering, and never returns a webhook
secret. What remains is configuration — host, username, region, the SES access
key id, the OAuth client id — which a `providers:read` key can therefore read.
The SDKs decode `id`, `name`, `type` and `allowedSenders` and nothing else: not
because the rest is secret, but because a sender needs none of it.

### Paging is by offset

A page token is an offset, base64 encoded. A provider created while you page
moves every row down by one, so the last provider of one page comes back first
on the next. One deleted moves them up, and the provider that slides across the
boundary is on neither page. Both need the list to span pages and to change
while it is read.

An unreadable token is not refused: the gateway starts again from the top.

### What the SDKs add

`ListProviders` in Go and `listProviders` in PHP return the whole list from one
call, because providers are configuration rather than a feed:

- Pages of 50, followed to the end, each provider returned once.
- The types re-checked against the one asked for, so a filter the gateway
  ignored still filters.
- A page token seen twice, or more than 200 pages — 10,000 providers — is an
  error rather than a list. A list cut short that looks complete is how a caller
  concludes a provider does not exist.
- No retries and no cache. Listing changes nothing, so calling again is always
  safe, and how stale a list may be is the caller's to decide.

---

## Reading a message's events

What became of a message is on the same door as sending it.

### The request

```
POST https://mail.example.com/panmail.v1.EventService/ListEvents
Content-Type: application/json
X-API-Key: <your key>
```

The key needs the **`events:read`** scope, which no key gets by default; a
sending key is refused with `permission_denied`. `events:read` reads every
message's delivery history in the tenant, recipients included.

```json
{ "pageSize": 100, "messageId": "0193b2f1-...", "pageToken": "…" }
```

| Field | Notes |
| --- | --- |
| `messageId` | The id a send returned. **An empty or absent one is no filter**: the answer is every event in the tenant. |
| `pageSize` | Zero or absent is 50. The PostgreSQL store caps it at 500. |
| `pageToken` | The previous page's `nextPageToken`. Opaque, and not the same shape on the two stores. |

`ListEventsRequest` also filters by recipient, type, subject and time, and
`latestOnly` keeps the newest event per message and recipient. The SDKs use
`messageId` alone.

### The response

```json
{
  "events": [
    {
      "id": "…",
      "tenantId": "…",
      "providerId": "3f1c2b7a-…",
      "providerName": "Production SES",
      "messageId": "0193b2f1-...",
      "type": "EMAIL_EVENT_TYPE_HARD_BOUNCE",
      "recipient": "gone@example.org",
      "subject": "Your receipt",
      "timestamp": "2026-09-01T10:00:02.123456789Z",
      "metadata": { "…": "…" },
      "errorMessage": "550 5.1.1 mailbox unknown"
    }
  ],
  "nextPageToken": "…"
}
```

One event per thing that happened, per recipient. `type` is an
`EmailEventType` name — or a bare number for a value the enum has no name for,
as with provider types. `timestamp` is RFC 3339 with up to nine fractional
digits. `errorMessage` is the provider's or the gateway's explanation, where
there is one.

**The order depends on the store.** PostgreSQL answers newest first; the Pebble
store groups by recipient, newest first within each. An unknown `messageId` is
an empty list, not an error.

### What the SDKs add

`MessageEvents` in Go and `messageEvents` in PHP return a message's whole
history from one call:

- An empty message id is refused before anything is sent.
- Pages of 100, followed to the end, each event returned once, **sorted oldest
  first** — the gateway's order kept between events in the same instant.
- A page token seen twice, or more than 50 pages — 5,000 events for one
  message — is an error rather than a history.
- `id`, `messageId`, `type`, `recipient`, `timestamp`, `providerId` and
  `errorMessage` (as `Reason` / `reason`) are decoded; the subject, provider
  name, tenant and `metadata` are not. The metadata is a provider's own
  vocabulary, with no shape the gateway promises.
- No retries. Reading changes nothing, so calling again is always safe.

---

## Suppressions

The list that turns a send into `failed_precondition`. Two procedures on it
are worth a sender's while: is this address on it, and take it off.

```
POST https://mail.example.com/panmail.v1.SuppressionService/CheckSuppression
POST https://mail.example.com/panmail.v1.SuppressionService/RemoveSuppression
Content-Type: application/json
X-API-Key: <your key>
```

Both take the address and nothing else:

```json
{ "email": "Bounced <bounced@example.net>" }
```

| Procedure | Scope | Answer |
| --- | --- | --- |
| `CheckSuppression` | **`suppressions:read`** | `{ "isSuppressed": true, "reason": "hard bounce" }`, or `{}` for an address not on the list — protobuf JSON leaves `false` and `""` out. |
| `RemoveSuppression` | **`suppressions:write`** | `{}`. Removing an address that is not on the list succeeds too, so a removal is safe to repeat. |

No key has either scope by default. `suppressions:write` also lets a key add
and import suppressions.

The gateway compares **mailboxes, not strings**: it reduces an address to its
bare lowercased form before it looks, so `Alice <Alice@Example.COM>` and
`alice@example.com` are one entry. An empty address is answered "not
suppressed" by the check and refused with `invalid_argument` by the removal;
the SDKs refuse it before sending.

A removal is not undone by anything but the next bounce or complaint, and the
address was usually put there by one. Mailing it again without that reason
having gone away is how a sending reputation is spent: check the reason first.

The SDKs: `CheckSuppression` / `checkSuppression` and `RemoveSuppression` /
`removeSuppression`. Neither retries. A `failed_precondition` here is a plain
refusal, not a suppressed recipient.

---

## Generating your own client

The protos are published in [`../proto`](../proto), copied verbatim from the
gateway: the import closures of `SendEmail`, `ListEmailProviders`,
`ListEvents`, `CheckSuppression` and `RemoveSuppression`, and the webhook
vocabulary. You do not need them to use an SDK or to POST JSON — they
are there for the case where you would rather generate a client than write one.
