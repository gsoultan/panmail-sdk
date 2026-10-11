<?php

declare(strict_types=1);

namespace Panmail\Tests;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Panmail\Exception\WebhookException;
use Panmail\InboundMail;
use Panmail\MailEvent;
use Panmail\MailHeld;
use Panmail\MailHoldOutcome;
use Panmail\MatchedCondition;
use Panmail\ParsedWebhook;
use Panmail\Status;
use Panmail\TriggerEvent;
use Panmail\Webhook;

final class WebhookPayloadTest extends TestCase
{
    private const SECRET = 'whsec_test';

    /** One body from testdata/webhook-payloads.json: bytes as the gateway builds them. */
    private static function gatewayBody(string $name): string
    {
        /** @var array{bodies: array<string, string>} $fixture */
        $fixture = json_decode(
            (string) file_get_contents(__DIR__ . '/../../testdata/webhook-payloads.json'),
            true,
            512,
            JSON_THROW_ON_ERROR
        );

        return $fixture['bodies'][$name] ?? self::fail("no $name in the fixture");
    }

    /** Signs $body as the gateway would, now, and parses it. */
    private static function parse(string $body): ParsedWebhook
    {
        $now = time();

        return Webhook::parse(self::SECRET, [
            Webhook::DELIVERY_HEADER => 'delivery-1',
            Webhook::TIMESTAMP_HEADER => (string) $now,
            Webhook::SIGNATURE_HEADER => 'sha256=' . hash_hmac('sha256', $now . '.' . $body, self::SECRET),
        ], $body);
    }

    /** A body around $data, for the cases the fixture does not cover. */
    private static function envelopeWith(string $event, mixed $data): string
    {
        return json_encode([
            'event' => $event,
            'tenant_id' => '3f1c2b7a-0000-4000-8000-0000000000aa',
            'timestamp' => time(),
            'data' => $data,
        ], JSON_THROW_ON_ERROR);
    }

    private static function at(string $time): \DateTimeImmutable
    {
        return new \DateTimeImmutable($time);
    }

    public function testParseWebhookReadsAMailEvent(): void
    {
        $parsed = self::parse(self::gatewayBody('mail_bounced'));

        self::assertSame(TriggerEvent::MAIL_BOUNCED, $parsed->webhook->event);
        self::assertSame('delivery-1', $parsed->webhook->deliveryId);
        self::assertEquals(new MailEvent(
            id: '0193b2f1-0000-7000-8000-0000000000e1',
            messageId: '0193b2f1-0000-7000-8000-000000000001',
            providerId: '3f1c2b7a-0000-4000-8000-000000000001',
            providerName: 'Production SES',
            type: Status::HARD_BOUNCE,
            recipient: 'gone@example.org',
            subject: 'Your receipt',
            timestamp: self::at('2026-09-01T10:00:02.123456Z'),
            reason: '550 5.1.1 mailbox unknown',
            metadata: ['attempt' => 1, 'smtp_code' => '550'],
        ), $parsed->payload);
    }

    public function testParseWebhookReadsAMailEventWithNothingOptional(): void
    {
        $payload = self::parse(self::gatewayBody('mail_opened'))->payload;

        self::assertInstanceOf(MailEvent::class, $payload);
        self::assertSame(Status::OPENED, $payload->type);
        self::assertSame('user@example.net', $payload->recipient);
        self::assertSame([], $payload->metadata);
        self::assertSame('', $payload->reason);
        self::assertSame('', $payload->providerId);
    }

    public function testParseWebhookReadsAHold(): void
    {
        self::assertEquals(new MailHeld(
            reviewId: '0193b2f1-0000-7000-8000-0000000000q1',
            direction: 'outbound',
            messageId: '0193b2f1-0000-7000-8000-000000000001',
            ruleId: '0193b2f1-0000-7000-8000-0000000000r1',
            ruleName: 'Invoices need a look',
            from: 'billing@example.com',
            recipients: ['user@example.net', 'cfo@example.net'],
            subject: 'Invoice 1042',
            sizeBytes: 48213,
            attachmentCount: 1,
            attachmentNames: ['invoice-1042.pdf'],
            matched: [
                new MatchedCondition('subject', 'contains', ['invoice'], '', 0),
                new MatchedCondition('header', 'equals', ['1'], 'X-Priority', 0),
                new MatchedCondition('size', 'greater_than', [], '', 40000),
            ],
            heldAt: self::at('2026-09-01T09:00:00Z'),
            expiresAt: self::at('2026-09-08T09:00:00Z'),
        ), self::parse(self::gatewayBody('mail_held'))->payload);
    }

    public function testParseWebhookReadsHowAHoldEnded(): void
    {
        $released = self::parse(self::gatewayBody('mail_released'))->payload;
        self::assertInstanceOf(MailHoldOutcome::class, $released);
        self::assertSame('0193b2f1-0000-7000-8000-0000000000q1', $released->reviewId);
        self::assertSame('reviewer@example.com', $released->reviewedBy);
        self::assertSame('expected invoice', $released->note);
        self::assertEquals(self::at('2026-09-01T10:00:02.123456Z'), $released->decidedAt);

        // An expiry has no reviewer, which is the point of it being its own event.
        self::assertEquals(new MailHoldOutcome(
            reviewId: '0193b2f1-0000-7000-8000-0000000000q2',
            direction: 'inbound',
            messageId: '',
            ruleId: '',
            ruleName: 'Quarantine zips',
            from: 'someone@elsewhere.example',
            recipients: ['support@example.com'],
            subject: 'files',
            heldAt: self::at('2026-09-01T09:00:00Z'),
            decidedAt: self::at('2026-09-08T09:00:00Z'),
            reviewedBy: '',
            note: '',
        ), self::parse(self::gatewayBody('mail_expired'))->payload);
    }

    public function testParseWebhookReadsAQuarantineRejectionAsAnOutcome(): void
    {
        $payload = self::parse(self::envelopeWith(TriggerEvent::MAIL_QUARANTINE_REJECTED, [
            'id' => 'q3', 'direction' => 'outbound', 'rule_name' => 'r', 'from' => 'a@example.com',
            'recipients' => ['b@example.net'], 'subject' => 's',
            'held_at' => '2026-09-01T09:00:00Z', 'decided_at' => '2026-09-01T09:30:00Z',
            'reviewed_by' => 'rev@example.com',
        ]))->payload;

        self::assertInstanceOf(MailHoldOutcome::class, $payload);
        self::assertSame('q3', $payload->reviewId);
        self::assertSame('rev@example.com', $payload->reviewedBy);
    }

    public function testParseWebhookReadsInboundMail(): void
    {
        self::assertEquals(new InboundMail(
            id: '0193b2f1-0000-7000-8000-0000000000i1',
            from: 'Customer <customer@elsewhere.example>',
            to: ['support@example.com'],
            subject: 'Help',
            html: '<p>Hello</p>',
            text: 'Hello',
            timestamp: self::at('2026-09-01T10:00:02.123456Z'),
            headers: ['Message-Id' => '<abc@elsewhere.example>'],
        ), self::parse(self::gatewayBody('mail_inbound'))->payload);
    }

    /** @return array<string, array{mixed}> */
    public static function inboundTimestamps(): array
    {
        return [
            'object' => [['seconds' => 1788256802, 'nanos' => 500000000]],
            'string' => ['2026-09-01T10:00:02.5Z'],
        ];
    }

    // encoding/json writes a protobuf Timestamp as {seconds, nanos};
    // protojson would write a string. Either reads.
    #[DataProvider('inboundTimestamps')]
    public function testParseWebhookReadsAnInboundTimestampInEitherForm(mixed $timestamp): void
    {
        $payload = self::parse(self::envelopeWith(TriggerEvent::MAIL_INBOUND, ['id' => 'i1', 'timestamp' => $timestamp]))->payload;

        self::assertInstanceOf(InboundMail::class, $payload);
        self::assertEquals(self::at('2026-09-01T10:00:02.5Z'), $payload->timestamp);
    }

    public function testParseWebhookNamesEveryEventTypeNumber(): void
    {
        /** @var array{numbers: array<string, string>} $fixture */
        $fixture = json_decode(
            (string) file_get_contents(__DIR__ . '/../../testdata/event-types.json'),
            true,
            512,
            JSON_THROW_ON_ERROR
        );
        self::assertNotEmpty($fixture['numbers']);

        foreach ($fixture['numbers'] as $number => $name) {
            $payload = self::parse(self::envelopeWith(TriggerEvent::MAIL_SENT, ['id' => 'e', 'type' => (int) $number]))->payload;

            self::assertInstanceOf(MailEvent::class, $payload);
            self::assertSame($name, $payload->type, "type $number");
        }
    }

    /** @return array<string, array{mixed, string}> */
    public static function typesWithoutAName(): array
    {
        return [
            'an unknown number' => [99, '99'],
            'a name' => ['EMAIL_EVENT_TYPE_DELIVERED', Status::DELIVERED],
            'absent' => [null, Status::UNSPECIFIED],
        ];
    }

    #[DataProvider('typesWithoutAName')]
    public function testParseWebhookKeepsATypeItHasNoNameFor(mixed $wire, string $expected): void
    {
        $data = ['id' => 'e'];
        if ($wire !== null) {
            $data['type'] = $wire;
        }
        $payload = self::parse(self::envelopeWith(TriggerEvent::MAIL_DELIVERED, $data))->payload;

        self::assertInstanceOf(MailEvent::class, $payload);
        self::assertSame($expected, $payload->type);
    }

    // The gateway adds events. One this version has no class for is verified
    // and handed back as it came, not refused into a retry loop.
    public function testParseWebhookPreservesAnEventItDoesNotKnow(): void
    {
        $parsed = self::parse(self::envelopeWith('WEBHOOK_TRIGGER_EVENT_MAIL_TELEPORTED', ['where' => 'the moon']));

        self::assertNull($parsed->payload);
        self::assertSame('WEBHOOK_TRIGGER_EVENT_MAIL_TELEPORTED', $parsed->webhook->event);
        self::assertSame(['where' => 'the moon'], $parsed->webhook->data);
    }

    public function testParseWebhookVerifiesFirst(): void
    {
        $body = self::gatewayBody('mail_bounced');
        $now = time();

        try {
            Webhook::parse(self::SECRET, [
                Webhook::TIMESTAMP_HEADER => (string) $now,
                Webhook::SIGNATURE_HEADER => 'sha256=' . hash_hmac('sha256', $now . '.' . $body, 'whsec_someone_else'),
            ], $body);
            self::fail('parse accepted a signature made with another secret');
        } catch (WebhookException $e) {
            self::assertStringContainsString('signature does not match', $e->getMessage());
        }

        $this->expectException(WebhookException::class);
        $this->expectExceptionMessage('no signature');
        Webhook::parse(self::SECRET, [], $body);
    }

    /** @return array<string, array{string, mixed}> */
    public static function unreadableShapes(): array
    {
        return [
            'no data' => [TriggerEvent::MAIL_BOUNCED, null],
            'data that is a string' => [TriggerEvent::MAIL_SENT, 'sent'],
            'recipients not a list' => [TriggerEvent::MAIL_HELD, ['recipients' => 'a@example.net']],
            'a size that is text' => [TriggerEvent::MAIL_HELD, ['size_bytes' => 'big']],
            'a time that is not one' => [TriggerEvent::MAIL_RELEASED, ['decided_at' => 'yesterday']],
            'a type that is a list' => [TriggerEvent::MAIL_OPENED, ['type' => [3]]],
            'metadata not an object' => [TriggerEvent::MAIL_CLICKED, ['metadata' => 'x']],
            'nanos out of range' => [TriggerEvent::MAIL_INBOUND, ['timestamp' => ['seconds' => 1, 'nanos' => 1e9]]],
            'a timestamp that is a 1' => [TriggerEvent::MAIL_INBOUND, ['timestamp' => 1]],
            'headers not text' => [TriggerEvent::MAIL_INBOUND, ['headers' => ['X' => 1]]],
        ];
    }

    // Signed, so not an attack — but a payload decoded halfway would hand
    // back zero values nobody can tell from real ones.
    #[DataProvider('unreadableShapes')]
    public function testParseWebhookRefusesAKnownEventInAShapeItCannotRead(string $event, mixed $data): void
    {
        try {
            self::parse(self::envelopeWith($event, $data));
            self::fail('parse decoded a shape it cannot read');
        } catch (WebhookException $e) {
            self::assertStringContainsString("its $event data", $e->getMessage());
        }
    }
}
