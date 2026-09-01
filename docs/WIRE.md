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
| `resource_exhausted` | 429 | **Two different things — see below.** |
| `internal` | 500 | The gateway failed. |

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

**The SDKs do not receive webhooks, and this document does not specify them.**
That is a gap rather than a decision: the delivery of an event to your endpoint
has a contract — a payload shape, and some way of telling a real event from
anything else that can reach a public URL — and none of it is written down
here.

> **Until it is, treat the endpoint as unauthenticated.** A webhook receiver is
> a public write path into your application. If nothing signs the request, an
> event claiming a message bounced is a thing anyone can send you, and acting
> on it — suppressing an address, marking an order unpaid, retrying a send —
> is acting on input from a stranger. Confirm anything that matters against
> your own record of the `messageId` you stored at send time.

Specifying this, and adding signature verification to the four clients once it
is, is [tracked](https://github.com/gsoultan/panmail-sdk/issues) rather than
quietly assumed to be somebody's problem.

---

## Generating your own client

The three protos that make up `SendEmail`'s import closure are published in
[`../proto`](../proto), copied verbatim from the gateway. You do not need them
to use an SDK or to POST JSON — they are there for the case where you would
rather generate a client than write one.
