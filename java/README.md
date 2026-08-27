# io.github.gsoultan:panmail-sdk

Java client for sending mail through a [panmail](https://github.com/gsoultan/panmail)
gateway. The transport is the JDK's own `HttpClient`; `jackson-databind` is the
only dependency.

```xml
<dependency>
    <groupId>io.github.gsoultan</groupId>
    <artifactId>panmail-sdk</artifactId>
    <version>0.1.0</version>
</dependency>
```

```java
PanmailClient client = PanmailClient.builder()
        .baseUrl("https://mail.example.com")
        .apiKey(System.getenv("PANMAIL_API_KEY"))
        .build();

try {
    Result result = client.send(Message.builder()
            .providerId("0f8b...")
            .from("noreply@example.com")
            .to("someone@example.org")
            .subject("Your receipt")
            .html("<p>Thanks for your order.</p>")
            .build());

    System.out.println("queued " + result.messageId());
} catch (RateLimitedException e) {
    // Not queued. Safe to repeat after e.retryAfter().
} catch (BacklogFullException e) {
    // Not queued either — but retrying on a timer makes the wait longer for
    // everything already queued. Slow down, or stop.
}
```

A successful send means the gateway **wrote the message to its outbox**, not
that it was delivered. Delivery is reported afterwards through events and
webhooks, keyed by `result.messageId()`.

**Retries are off by default and never applied to an unknown outcome.** Call
`.rateLimitRetries(n)` to wait out rate-limit refusals — the one failure where
the gateway said plainly it did not accept the message.

Instances are immutable and safe for concurrent use.

Full documentation, the other three SDKs, and the raw wire contract:
**https://github.com/gsoultan/panmail-sdk**
