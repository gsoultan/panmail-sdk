# gsoultan/panmail-sdk

PHP client for sending mail through a [panmail](https://github.com/gsoultan/panmail)
gateway. Needs only the bundled `curl` and `json` extensions.

```bash
composer require gsoultan/panmail-sdk
```

```php
use Panmail\Client;
use Panmail\Message;
use Panmail\Exception\BacklogFullException;
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
    echo "queued {$result->messageId}", PHP_EOL;
} catch (RateLimitedException $e) {
    // Not queued. Safe to repeat after $e->retryAfter seconds.
} catch (BacklogFullException $e) {
    // Not queued either — but retrying on a timer makes the wait longer for
    // everything already queued. Slow down, or stop.
}
```

A successful send means the gateway **wrote the message to its outbox**, not
that it was delivered. Delivery is reported afterwards through events and
webhooks, keyed by `$result->messageId`.

**The provider ids can be listed** with a key that also holds the
`providers:read` scope, which no key gets by default:

```php
use Panmail\ProviderType;

foreach ($client->listProviders(type: ProviderType::SES) as $provider) {
    echo "{$provider->id} {$provider->name}", PHP_EOL;
}
```

One call returns the whole list, following the gateway's pages for you. Each
provider carries its id, name, type and allowed From domains, and nothing else.

**A message's delivery events can be read** with a key that also holds
`events:read`, oldest first, every recipient:

```php
foreach ($client->messageEvents($result->messageId) as $event) {
    echo "{$event->recipient} {$event->type}", PHP_EOL;
}
```

**A suppression can be looked up and lifted** — `checkSuppression()` with
`suppressions:read`, `removeSuppression()` with `suppressions:write`:

```php
if ($client->checkSuppression('bounced@example.net')->suppressed) {
    $client->removeSuppression('bounced@example.net');
}
```

**Retries are off by default, and an unknown outcome is never repeated without
an idempotency key.** Pass `['rateLimitRetries' => n]` to wait out rate-limit
refusals — where the gateway said plainly it did not accept the message. Give
a message an `idempotencyKey` and pass `['transportRetries' => n]` to repeat a
send that did not complete: the gateway answers the same key with the message
it already accepted. Only against a gateway that supports keys — an older one
ignores the field, and retrying there sends duplicates.

Bring your own HTTP stack by passing `['transport' => $yourTransport]`, any
implementation of `Panmail\Transport\Transport`.

Full documentation, the other three SDKs, and the raw wire contract:
**https://github.com/gsoultan/panmail-sdk**
