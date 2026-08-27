# @gsoultan/panmail-sdk

Node client for sending mail through a [panmail](https://github.com/gsoultan/panmail)
gateway. No dependencies — it uses `fetch`.

```bash
npm install @gsoultan/panmail-sdk
```

```ts
import { PanmailClient, RateLimitedError, BacklogFullError } from '@gsoultan/panmail-sdk';

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
    // Not queued. Safe to repeat after error.retryAfter seconds.
  }
  if (error instanceof BacklogFullError) {
    // Not queued either — but retrying on a timer makes the wait longer for
    // everything already queued. Slow down, or stop.
  }
  throw error;
}
```

A successful send means the gateway **wrote the message to its outbox**, not
that it was delivered. Delivery is reported afterwards through events and
webhooks, keyed by `result.messageId`.

**Retries are off by default and never applied to an unknown outcome.** Pass
`{ rateLimitRetries: n }` to wait out rate-limit refusals — the one failure
where the gateway said plainly it did not accept the message.

Full documentation, the other three SDKs, and the raw wire contract:
**https://github.com/gsoultan/panmail-sdk**
