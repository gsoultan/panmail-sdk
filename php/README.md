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

**Retries are off by default and never applied to an unknown outcome.** Pass
`['rateLimitRetries' => n]` to wait out rate-limit refusals — the one failure
where the gateway said plainly it did not accept the message.

Bring your own HTTP stack by passing `['transport' => $yourTransport]`, any
implementation of `Panmail\Transport\Transport`.

Full documentation, the other three SDKs, and the raw wire contract:
**https://github.com/gsoultan/panmail-sdk**
