# panmail-sdk

Clients for sending mail through a [panmail](https://github.com/gsoultan/panmail)
gateway, in Go, PHP and Node.

All three are the same small library in three languages: **one call**, typed
refusals you can act on, and a deliberate refusal to retry anything whose
outcome is unknown. None of them depends on the gateway's source — they speak
[the wire contract](docs/WIRE.md), which is also what you want if you would
rather POST JSON yourself or send over SMTP.

| Language | Package | Install | Runtime deps |
| --- | --- | --- | --- |
| **Go** | `github.com/gsoultan/panmail-sdk` | `go get github.com/gsoultan/panmail-sdk` | none — stdlib only |
| **PHP** | `gsoultan/panmail-sdk` | `composer require gsoultan/panmail-sdk` | ext-curl, ext-json |
| **Node** | `@gsoultan/panmail-sdk` | `npm i @gsoultan/panmail-sdk` | none — `fetch` |

## Getting a key

Create one in the dashboard under **Settings → API Keys**, with the
`email:send` scope. The key carries the tenant, so there is nothing else to
configure.

You also need a **provider id**, from the Email Providers page. It is required
on every send: panmail will not guess which of a tenant's providers a message
goes out through, because the wrong guess is a message sent from the wrong
domain.

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

## Node

```ts
import { PanmailClient, RateLimitedError } from '@gsoultan/panmail-sdk';

const client = new PanmailClient('https://mail.example.com', process.env.PANMAIL_API_KEY!);

try {
  const result = await client.send({
    providerId: '0f8b...',
    from: 'noreply@example.com',
    to: ['someone@example.org'],
    subject: 'Your receipt',
    html: '<p>Thanks for your order.</p>',
  });
  console.log('queued', result.messageId);
} catch (error) {
  if (error instanceof RateLimitedError) {
    // Safe to repeat after error.retryAfter seconds.
  }
  throw error;
}
```

---

## What "sent" means

A successful send means the gateway **wrote the message to its outbox**, not
that it was delivered. Delivery is reported afterwards, through delivery events
and webhooks, keyed by the message id you got back. Store that id next to
whatever prompted the send.

## The two capacity refusals

Both answer `resource_exhausted` / HTTP 429, on purpose: they are the same
answer to you — *you are asking for more than you may have.* What separates
them is whether the gateway attached a delay.

| Refusal | Go | PHP | Node |
| --- | --- | --- | --- |
| Over the send rate | `*RateLimitedError` | `RateLimitedException` | `RateLimitedError` |
| Backlog full | `*BacklogFullError` | `BacklogFullException` | `BacklogFullError` |
| Key rejected | `*AuthError` | `AuthException` | `AuthError` |
| Anything else | `*APIError` | `ApiException` | `ApiError` |

A **rate limit** carries a delay and is safe to repeat after it.

A **full backlog** carries none, because there is none to give: a queue
clearing is not something you can schedule against. Retrying on a timer will not
help, and retrying immediately makes the wait longer for everything already
queued. Slow down, or stop.

## Retries

**These clients do not retry a send whose outcome they do not know.** Sending is
not idempotent and the gateway has no de-duplication key, so a retry after a
timeout or a dropped connection is a retry of a message that may already be on
its way to the recipient.

The one exception is a *refusal* — the gateway said plainly it did not accept
the message — which every SDK will wait out if you ask:

```go
panmail.New(url, key, panmail.WithRateLimitRetries(3))                  // Go
new Client($url, $key, ['rateLimitRetries' => 3]);                      // PHP
new PanmailClient(url, key, { rateLimitRetries: 3 })                    // Node
```

It is off by default because it makes `send` block for as long as the gateway
asks, which inside a request handler is a decision you should make rather than
inherit. A queue of your own is usually the better answer.

## Not using an SDK

You do not have to. [`docs/WIRE.md`](docs/WIRE.md) documents the HTTP JSON
contract and the SMTP submission door in full — headers, field names, the
`Retry-After` discrimination, SMTP reply codes and the Bcc-via-envelope rule.
The protos are in [`proto/`](proto) if you would rather generate a client.

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
a message id and a status, and a proxy error page is not worth buffering into
an out-of-memory error.

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
cd node && bun test src              # Node
```

[`CONTRIBUTING.md`](CONTRIBUTING.md) has the rest: what a change needs, how the
protos are synced, and how a release is cut.

## License

[MIT](LICENSE)
