# panmail-sdk

Clients for sending mail through a [panmail](https://github.com/gsoultan/panmail)
gateway, in Go and PHP.

Both are the same small library in two languages: **one call to send** and one
to list what you can send through, typed refusals you can act on, and a
deliberate refusal to retry anything whose outcome is unknown unless an
idempotency key makes that safe. None of them depends on the gateway's source — they speak
[the wire contract](docs/WIRE.md), which is also what you want if you would
rather POST JSON yourself or send over SMTP.

| Language | Package | Install | Runtime deps |
| --- | --- | --- | --- |
| **Go** | `github.com/gsoultan/panmail-sdk` | `go get github.com/gsoultan/panmail-sdk` | none — stdlib only |
| **PHP** | `gsoultan/panmail-sdk` | `composer require gsoultan/panmail-sdk` | ext-curl, ext-json |

**There is no Node package.** One was written, and withdrawn before it was ever
published: a client that is one `fetch` call deep is not worth a registry
account, a release pipeline and a second place for the wire format to drift.
From Node, POST the JSON yourself — see [Not using an SDK](#not-using-an-sdk).

## Getting a key

Create one in the dashboard under **Settings → API Keys**, with the
`email:send` scope. The key carries the tenant, so there is nothing else to
configure.

You also need a **provider id**, from the Email Providers page or
[listed by the client](#listing-providers). It is required on every send:
panmail will not guess which of a tenant's providers a message goes out
through, because the wrong guess is a message sent from the wrong domain.

## Go

```go
import panmail "github.com/gsoultan/panmail-sdk"

client, err := panmail.New("https://mail.example.com", os.Getenv("PANMAIL_API_KEY"))
if err != nil {
    return err
}

result, err := client.Send(ctx, panmail.Message{
    ProviderID: "0f8b...",
    From:       "noreply@example.com",
    To:         []string{"someone@example.org"},
    Subject:    "Your receipt",
    HTML:       "<p>Thanks for your order.</p>",
})
if err != nil {
    var limited *panmail.RateLimitedError
    if errors.As(err, &limited) {
        // Safe to repeat after limited.RetryAfter.
    }
    return err
}

log.Println("queued", result.MessageID)
```

## PHP

```php
use Panmail\Client;
use Panmail\Message;
use Panmail\Exception\RateLimitedException;

$client = new Client('https://mail.example.com', getenv('PANMAIL_API_KEY'));

try {
    $result = $client->send(new Message(
        providerId: '0f8b...',
        from: 'noreply@example.com',
        to: ['someone@example.org'],
        subject: 'Your receipt',
        html: '<p>Thanks for your order.</p>',
    ));
    echo "queued {$result->messageId}\n";
} catch (RateLimitedException $e) {
    // Safe to repeat after $e->retryAfter seconds.
}
```

## What "sent" means

A successful send means the gateway **wrote the message to its outbox**, not
that it was delivered. Delivery is reported afterwards, through delivery events
and webhooks, keyed by the message id you got back. Store that id next to
whatever prompted the send.

It is also not a promise of delivery. A filter rule can quarantine a message for
review, and the gateway answers a held message with the same id and the same
`PENDING` status as an accepted one — nothing in the response tells them apart.
Subscribe to `MAIL_HELD` if you need to know, and to `MAIL_EXPIRED`, which is a
hold nobody reviewed before its retention deadline.

## The two capacity refusals

Both answer `resource_exhausted` / HTTP 429, on purpose: they are the same
answer to you — *you are asking for more than you may have.* What separates
them is whether the gateway attached a delay.

| Refusal | Go | PHP |
| --- | --- | --- |
| Over the send rate | `*RateLimitedError` | `RateLimitedException` |
| Backlog full | `*BacklogFullError` | `BacklogFullException` |
| Recipient suppressed | `*SuppressedRecipientError` | `SuppressedRecipientException` |
| Idempotency key reused for another message | `*IdempotencyKeyConflictError` | `IdempotencyKeyConflictException` |
| Key rejected | `*AuthError` | `AuthException` |
| Anything else | `*APIError` | `ApiException` |

A **rate limit** carries a delay and is safe to repeat after it.

A **full backlog** carries none, because there is none to give: a queue
clearing is not something you can schedule against. Retrying on a timer will not
help, and retrying immediately makes the wait longer for everything already
queued. Slow down, or stop.

## Retries

**These clients do not retry a send whose outcome they do not know** — unless
you give it an idempotency key and ask. Without a key, a retry after a timeout
or a dropped connection is a retry of a message that may already be on its way
to the recipient.

A *refusal* — the gateway said plainly it did not accept the message — is
different, and the clients will wait out a rate limit if you ask:

```go
panmail.New(url, key, panmail.WithRateLimitRetries(3))                  // Go
new Client($url, $key, ['rateLimitRetries' => 3]);                      // PHP
```

It is off by default because it makes `send` block for as long as the gateway
asks, which inside a request handler is a decision you should make rather than
inherit. A queue of your own is usually the better answer.

### Idempotency keys

A key makes a repeated send safe. The gateway remembers it for 24 hours with
the message it accepted, and answers the same key and the same request with
that message instead of queueing another:

```go
client, _ := panmail.New(url, key, panmail.WithTransportRetries(3))
result, err := client.Send(ctx, panmail.Message{
    // ...
    IdempotencyKey: "order-1042/receipt",
})
```

```php
$client = new Client($url, $key, ['transportRetries' => 3]);
$result = $client->send(new Message(
    // ...
    idempotencyKey: 'order-1042/receipt',
));
```

Name **what is being sent**, not the attempt — an order id and the kind of
mail — so a retry from anywhere carries the same key. At most 255 printable
ASCII characters. The same key with a *different* message is refused with
`IdempotencyKeyConflictError` / `IdempotencyKeyConflictException`, and nothing
is sent.

`WithTransportRetries` / `transportRetries` repeats a keyed send whose outcome
is unknown — a dropped connection, a timeout, or the gateway's `aborted` for a
concurrent send with the same key — up to `n` times, waiting 0.1 s doubling to
2 s between them. In Go the context's deadline ends it. A send without a key is
never repeated this way, whatever the option says.

**Turn it on only against a gateway that supports keys.** One that predates
them ignores the field rather than refusing it, and nothing in its answer says
so; retrying there sends duplicates.

## Listing providers

The ids a send takes, for a key that also holds the **`providers:read`** scope
— which no key gets by default:

```go
providers, err := client.ListProviders(ctx, panmail.ProviderFilter{Type: panmail.ProviderTypeSES})
if err != nil {
    return err
}
for _, p := range providers {
    log.Println(p.ID, p.Name, p.AllowedDomains)
}
```

```php
use Panmail\ProviderType;

foreach ($client->listProviders(type: ProviderType::SES) as $provider) {
    echo "{$provider->id} {$provider->name}", PHP_EOL;
}
```

One call returns the whole list, newest first; the clients follow the
gateway's pages for you. A provider carries its id, name, type, the From
domains it will send as and, for SMTP, IMAP and POP3, the username it signs in
as — never a password or key, so it is safe to log. IMAP and POP3
providers are listed too, but they read inbound mail rather than send it.

The name filter is a search, not a lookup: `prod` matches `Production` and
`eu-prod`. The type filter is checked against what comes back, because the
gateway treats a type name it does not recognise as no filter at all.

`providers:read` lets a key read every provider's configuration from the
gateway, credentials excepted. If you only need the ids while setting an
application up, a second key for that job keeps your sending key as narrow as
it was.

## What happened to a message

A message's delivery history, for a key that also holds the **`events:read`**
scope — which no key gets by default:

```go
events, err := client.MessageEvents(ctx, result.MessageID)
for _, e := range events {
    log.Println(e.Timestamp, e.Recipient, e.Type, e.Reason)
}
```

```php
foreach ($client->messageEvents($result->messageId) as $event) {
    echo "{$event->recipient} {$event->type} {$event->reason}", PHP_EOL;
}
```

One call returns every event the gateway has recorded for the message, for
every recipient, oldest first; the latest per recipient is where that copy
stands. An unknown id is an empty list. Each event carries its id, the message
id, its type (a `Status` constant), the recipient, the time, the provider and
the gateway's reason where there is one — not the subject, and not the
provider's free-form metadata.

`events:read` reads every message's history in the tenant, recipients
included. A second key for the job that needs it keeps your sending key as
narrow as it was.

## Suppressions

A send to an address on the tenant's suppression list is refused whole, with
`SuppressedRecipientError` / `SuppressedRecipientException`. Look one up with a
key that holds **`suppressions:read`**, and lift it with one that holds
**`suppressions:write`** — neither is granted by default:

```go
check, err := client.CheckSuppression(ctx, "bounced@example.net")
if err == nil && check.Suppressed {
    log.Println("suppressed:", check.Reason)
    // err = client.RemoveSuppression(ctx, "bounced@example.net")
}
```

```php
$check = $client->checkSuppression('bounced@example.net');
if ($check->suppressed) {
    echo "suppressed: {$check->reason}", PHP_EOL;
    // $client->removeSuppression('bounced@example.net');
}
```

The gateway compares mailboxes, not strings, so `Alice <Alice@Example.COM>`
finds `alice@example.com`. Removing an address that is not on the list
succeeds, so a removal is safe to repeat. The address was usually suppressed by
a hard bounce or a complaint: mailing it again before that has changed is how
a sending reputation is spent.

## Not using an SDK

You do not have to. [`docs/WIRE.md`](docs/WIRE.md) documents the HTTP JSON
contract and the SMTP submission door in full — headers, field names, the
`Retry-After` discrimination, SMTP reply codes and the Bcc-via-envelope rule.
The protos are in [`proto/`](proto) if you would rather generate a client.

## Receiving delivery events

What happens to a message after it is queued arrives as a signed POST. Verify
it before you act on it — the gateway delivers a subscription with no secret
*unsigned*, so an unverified handler is one anyone who finds the URL can drive.

```go
body, _ := io.ReadAll(r.Body)          // the raw bytes: the signature covers them
event, err := panmail.VerifyWebhook(os.Getenv("PANMAIL_WEBHOOK_SECRET"), r.Header, body)
if err != nil {
    http.Error(w, "", http.StatusUnauthorized)
    return
}
log.Println(event.Event, event.TenantID, event.DeliveryID)
```

```php
$event = Panmail\Webhook::verify($secret, getallheaders(), file_get_contents('php://input'));
```

Match `event.event` against the `TriggerEvent` constants rather than a string
you typed — the gateway sends its enum name verbatim,
`WEBHOOK_TRIGGER_EVENT_MAIL_BOUNCED`, not a dotted `mail.bounced`:

```go
switch event.Event {
case panmail.TriggerEventMailBounced, panmail.TriggerEventMailQuarantineRejected:
    // A provider refused it, or a reviewer did. Not the same thing.
case panmail.TriggerEventMailExpired:
    // A held message nobody reviewed. This one is about your queue, not the mail.
}
```

`event.deliveryId` is stable across retries, and the gateway does retry: store
it and ignore a repeat, or a bounce will suppress an address more than once.

To have the data decoded as well, parse instead of verifying — same checks,
then a typed payload chosen by the event:

```go
parsed, err := panmail.ParseWebhook(secret, r.Header, body)
if err != nil {
    http.Error(w, "", http.StatusUnauthorized)
    return
}
switch p := parsed.Payload.(type) {
case *panmail.MailEvent:       // sent, delivered, opened, clicked, bounced, rejected
    log.Println(p.Type, p.Recipient, p.Reason)
case *panmail.MailHeld:        // p.ReviewID, p.Matched, p.ExpiresAt
case *panmail.MailHoldOutcome: // released, quarantine-rejected, expired
case *panmail.InboundMail:     // p.From, p.Subject, p.Text
case nil:                      // an event this version does not know: parsed.Webhook.Data
}
```

```php
$parsed = Panmail\Webhook::parse($secret, getallheaders(), file_get_contents('php://input'));
if ($parsed->payload instanceof Panmail\MailEvent) {
    echo "{$parsed->payload->recipient} {$parsed->payload->type}", PHP_EOL;
}
```

An event the client has no type for comes back verified with no payload rather
than being refused, so a gateway newer than your client does not have its
deliveries bounced and retried. A known event whose data is not the shape the
client reads is refused, like a bad signature. `MailEvent.Type` is the precise
`Status` — `MAIL_BOUNCED` fires for hard and soft bounces alike.
The full contract is in [`docs/WIRE.md`](docs/WIRE.md).

## Your API key

A key is a sending credential for a whole tenant, so the clients treat it as
the thing most worth not losing.

**None of the four follows an HTTP redirect.** The key travels in `X-API-Key`,
and a custom header is not on any HTTP client's list of credentials to drop
when a redirect crosses to another host — that list knows about `Authorization`
and `Cookie`. Following one would hand your key to whatever host the `Location`
header named. If you supply your own HTTP client, keep that property; see
[`SECURITY.md`](SECURITY.md) for what each language needs.

**A response is bounded at 1 MiB before it is read**, because a send result is
a message id and a status, a page of providers is tens of kilobytes, and a
proxy error page is not worth buffering into an out-of-memory error. Past that
bound nothing more is read: the Go client discards at most another 64 KiB
before closing, so a body that never ends cannot hold a call open. A refusal
whose body was cut short is still classified from its status and code — a 429
behind a long proxy page is still a rate limit, in both languages — and the
message an error carries is cut to 4 KiB.

The PHP client keeps one cURL handle per transport and resets it between
requests, so consecutive sends and listing pages reuse the connection.

**The gateway must be reached over `https`.** `http://` is refused unless the
host is loopback, so a gateway on `localhost` still works for development while
a plaintext production URL fails when you build the client rather than sending
your key in the clear.

**The key cannot be set from two places.** The per-request header options will
not set `X-API-Key`, case insensitively.

## Development

```bash
go test -race ./...                  # Go
cd php  && composer test             # PHP
```

[`docs/PRD.md`](docs/PRD.md) is what the clients are required to do and why;
[`CONTRIBUTING.md`](CONTRIBUTING.md) has the rest: what a change needs, how the
protos are synced, and how a release is cut.

## License

[MIT](LICENSE)
