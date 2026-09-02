<?php

declare(strict_types=1);

namespace Panmail\Tests;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Panmail\Exception\WebhookException;
use Panmail\Webhook;

final class WebhookTest extends TestCase
{
    private const SECRET = 'whsec_test';

    /**
     * Written out rather than calling the class under test, so this checks
     * against panmail's own scheme rather than the SDK agreeing with itself.
     */
    private static function signLikeTheGateway(string $secret, int $timestamp, string $body): string
    {
        return 'sha256=' . hash_hmac('sha256', $timestamp . '.' . $body, $secret);
    }

    /** @param array<string, mixed> $over */
    private static function body(array $over = []): string
    {
        return json_encode(array_merge([
            'event' => 'mail.bounced',
            'tenant_id' => '3f1c2b7a-0000-4000-8000-000000000001',
            'timestamp' => time(),
            'data' => ['messageId' => 'msg_01', 'reason' => 'mailbox full'],
        ], $over), JSON_THROW_ON_ERROR);
    }

    /** @return array<string, string> */
    private static function headers(string $body, string $secret = self::SECRET, ?int $sentAt = null): array
    {
        $sentAt ??= time();

        return [
            Webhook::EVENT_HEADER => 'mail.bounced',
            Webhook::DELIVERY_HEADER => 'delivery-1',
            Webhook::TIMESTAMP_HEADER => (string) $sentAt,
            Webhook::SIGNATURE_HEADER => self::signLikeTheGateway($secret, $sentAt, $body),
        ];
    }

    public function testItAcceptsWhatTheGatewaySent(): void
    {
        $body = self::body();

        $event = Webhook::verify(self::SECRET, self::headers($body), $body);

        self::assertSame('mail.bounced', $event->event);
        self::assertSame('3f1c2b7a-0000-4000-8000-000000000001', $event->tenantId);
        self::assertSame('delivery-1', $event->deliveryId);
        self::assertIsArray($event->data);
        self::assertSame('mailbox full', $event->data['reason']);
    }

    /**
     * The gateway delivers a subscription with no secret unsigned rather than
     * not delivering it, so an unsigned request is a real thing that arrives
     * at a real endpoint. Accepting one would make verification decorative.
     */
    public function testItRefusesAnUnsignedDelivery(): void
    {
        $this->expectException(WebhookException::class);
        $this->expectExceptionMessageMatches('/carried no signature/');

        Webhook::verify(self::SECRET, [Webhook::EVENT_HEADER => 'mail.sent'], self::body());
    }

    public function testItRefusesATamperedBody(): void
    {
        $body = self::body();
        $headers = self::headers($body);

        $this->expectException(WebhookException::class);

        Webhook::verify(self::SECRET, $headers, str_replace('mailbox full', 'mailbox FULL', $body));
    }

    public function testItRefusesTheWrongSecret(): void
    {
        $body = self::body();

        $this->expectException(WebhookException::class);
        $this->expectExceptionMessageMatches('/does not match/');

        Webhook::verify('whsec_someone_elses', self::headers($body), $body);
    }

    /** @return iterable<string, array{int}> */
    public static function driftingClocks(): iterable
    {
        yield 'long ago' => [-1800];
        yield 'the future' => [1800];
    }

    /**
     * The window is what stops a captured request being replayed tomorrow, so
     * it is checked in both directions.
     */
    #[DataProvider('driftingClocks')]
    public function testItRefusesATimestampOutsideTheWindow(int $offset): void
    {
        $body = self::body();
        $headers = self::headers($body, self::SECRET, time() + $offset);

        // The same delivery inside a wider window is fine, which shows the
        // refusal below was the clock and not the signature.
        $event = Webhook::verify(self::SECRET, $headers, $body, 3600);
        self::assertSame('mail.bounced', $event->event);

        $this->expectException(WebhookException::class);
        $this->expectExceptionMessageMatches('/tolerance/');

        Webhook::verify(self::SECRET, $headers, $body);
    }

    public function testItRefusesATimestampThatIsNotANumber(): void
    {
        $body = self::body();
        $headers = self::headers($body);
        $headers[Webhook::TIMESTAMP_HEADER] = 'yesterday';

        $this->expectException(WebhookException::class);
        $this->expectExceptionMessageMatches('/not a number/');

        Webhook::verify(self::SECRET, $headers, $body);
    }

    public function testItNeedsASecret(): void
    {
        $body = self::body();

        $this->expectException(WebhookException::class);
        $this->expectExceptionMessageMatches('/no signing secret/');

        Webhook::verify('', self::headers($body), $body);
    }

    /** Signed by the right secret, but not the envelope this version knows. */
    public function testItSeparatesASignedBodyItCannotRead(): void
    {
        $body = '["signed", "but not an envelope"]';

        $this->expectException(WebhookException::class);
        $this->expectExceptionMessageMatches('/not a webhook envelope/');

        Webhook::verify(self::SECRET, self::headers($body), $body);
    }

    public function testItFallsBackToTheEventHeader(): void
    {
        $body = json_encode([
            'tenant_id' => 't-1',
            'timestamp' => time(),
            'data' => [],
        ], JSON_THROW_ON_ERROR);

        self::assertSame('mail.bounced', Webhook::verify(self::SECRET, self::headers($body), $body)->event);
    }

    public function testANonPositiveToleranceFallsBackToTheDefault(): void
    {
        $body = self::body();

        self::assertSame(
            'mail.bounced',
            Webhook::verify(self::SECRET, self::headers($body), $body, 0)->event
        );
    }

    /** Header names are case insensitive, and PHP hands them over in any case. */
    public function testItReadsHeadersInAnyCase(): void
    {
        $body = self::body();
        $lowercased = array_change_key_case(self::headers($body), CASE_LOWER);

        self::assertSame('mail.bounced', Webhook::verify(self::SECRET, $lowercased, $body)->event);
    }

    /** The signature covers the bytes that arrived, not the value they decode to. */
    public function testItRefusesABodyThatWasReformatted(): void
    {
        $arrived = json_encode(
            json_decode(self::body(), true, 512, JSON_THROW_ON_ERROR),
            JSON_THROW_ON_ERROR | JSON_PRETTY_PRINT
        );
        $headers = self::headers($arrived);

        // As it arrived, indentation and all, it verifies.
        self::assertSame('mail.bounced', Webhook::verify(self::SECRET, $headers, $arrived)->event);

        $this->expectException(WebhookException::class);

        Webhook::verify(self::SECRET, $headers, (string) json_encode(
            json_decode($arrived, true, 512, JSON_THROW_ON_ERROR),
            JSON_THROW_ON_ERROR
        ));
    }

    /**
     * The vectors in testdata are computed independently — in Python by
     * scripts/sync-webhook-vectors.py — and were checked against the gateway's
     * own sign(). Matching them is agreeing with something other than
     * ourselves, and it is what stops the three clients drifting apart on the
     * one calculation where disagreeing means silently rejecting real
     * deliveries.
     *
     * @return iterable<string, array{string, int, string, string}>
     */
    public static function sharedVectors(): iterable
    {
        $raw = file_get_contents(__DIR__ . '/../../testdata/webhook-signatures.json');
        if ($raw === false) {
            self::fail('the shared vectors could not be read');
        }

        /** @var array{vectors: list<array{name: string, secret: string, timestamp: int, body: string, signature: string}>} $fixture */
        $fixture = json_decode($raw, true, 512, JSON_THROW_ON_ERROR);
        foreach ($fixture['vectors'] as $v) {
            yield $v['name'] => [$v['secret'], $v['timestamp'], $v['body'], $v['signature']];
        }
    }

    #[DataProvider('sharedVectors')]
    public function testItSignsAsTheGatewayDoes(
        string $secret,
        int $timestamp,
        string $body,
        string $signature
    ): void {
        $headers = [
            Webhook::TIMESTAMP_HEADER => (string) $timestamp,
            Webhook::SIGNATURE_HEADER => $signature,
        ];

        try {
            // The vectors use fixed timestamps, so the window is widened to
            // leave only the signature under test.
            $event = Webhook::verify($secret, $headers, $body, 100 * 365 * 24 * 3600);
            self::assertSame($timestamp, $event->timestamp);
        } catch (WebhookException $refusal) {
            // A body that is valid JSON but not an envelope still proves the
            // signature matched: that refusal happens after the HMAC check.
            self::assertStringContainsString('not a webhook envelope', $refusal->getMessage());
        }
    }

}
