<?php

declare(strict_types=1);

namespace Panmail\Tests;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Panmail\Client;
use Panmail\DeliveryEvent;
use Panmail\Exception\ApiException;
use Panmail\Exception\AuthException;
use Panmail\Exception\BacklogFullException;
use Panmail\Exception\IdempotencyKeyConflictException;
use Panmail\Exception\InvalidMessageException;
use Panmail\Exception\SuppressedRecipientException;
use Panmail\Exception\TransportException;
use Panmail\Status;
use Panmail\Transport\Response;
use Panmail\Transport\Transport;

final class EventsTest extends TestCase
{
    private const BASE = 'https://mail.example.com';
    private const MESSAGE_ID = '0193b2f1-0000-7000-8000-000000000001';

    /**
     * One ListEvents response: the events, and the token for the next page,
     * if there is one.
     *
     * @param array<string, mixed> ...$events
     */
    private static function page(string $next, array ...$events): Response
    {
        $body = ['events' => array_values($events)];
        if ($next !== '') {
            $body['nextPageToken'] = $next;
        }

        return new Response(200, ['content-type' => 'application/json'], json_encode($body, JSON_THROW_ON_ERROR));
    }

    /** @return array<string, string> */
    private static function event(string $id, string $type, string $at): array
    {
        return [
            'id' => $id,
            'messageId' => self::MESSAGE_ID,
            'type' => $type,
            'recipient' => 'user@example.net',
            'timestamp' => $at,
        ];
    }

    /** @param array<string, mixed> $options */
    private static function client(Transport $transport, array $options = []): Client
    {
        return new Client(self::BASE, 'test-key', ['transport' => $transport] + $options);
    }

    /**
     * @param list<DeliveryEvent> $events
     *
     * @return list<string>
     */
    private static function ids(array $events): array
    {
        return array_map(static fn (DeliveryEvent $event): string => $event->id, $events);
    }

    public function testMessageEvents(): void
    {
        $transport = new FakeTransport(self::page(
            '',
            // Newest first, as the gateway's PostgreSQL store answers.
            [
                'id' => 'ev_3',
                'tenantId' => '3f1c2b7a-0000-4000-8000-0000000000aa',
                'providerId' => '3f1c2b7a-0000-4000-8000-000000000001',
                'providerName' => 'Production SES',
                'messageId' => self::MESSAGE_ID,
                'type' => 'EMAIL_EVENT_TYPE_HARD_BOUNCE',
                'recipient' => 'gone@example.org',
                'subject' => 'Your receipt',
                'timestamp' => '2026-09-01T10:00:02.123456789Z',
                'metadata' => ['code' => '550'],
                'errorMessage' => '550 5.1.1 mailbox unknown',
            ],
            self::event('ev_2', 'EMAIL_EVENT_TYPE_SENT', '2026-09-01T10:00:01Z'),
            self::event('ev_1', 'EMAIL_EVENT_TYPE_PENDING', '2026-09-01T10:00:00Z'),
        ));

        $events = self::client($transport)->messageEvents(self::MESSAGE_ID);

        self::assertSame(['ev_1', 'ev_2', 'ev_3'], self::ids($events));
        $bounce = $events[2];
        self::assertSame('ev_3', $bounce->id);
        self::assertSame(self::MESSAGE_ID, $bounce->messageId);
        self::assertSame(Status::HARD_BOUNCE, $bounce->type);
        self::assertSame('gone@example.org', $bounce->recipient);
        self::assertNotNull($bounce->timestamp);
        self::assertSame('2026-09-01T10:00:02.123456+00:00', $bounce->timestamp->format('Y-m-d\TH:i:s.uP'));
        self::assertSame('3f1c2b7a-0000-4000-8000-000000000001', $bounce->providerId);
        self::assertSame('550 5.1.1 mailbox unknown', $bounce->reason);
    }

    public function testMessageEventsPostsToTheConnectProcedure(): void
    {
        $transport = new FakeTransport(self::page(''));

        self::client($transport)->messageEvents(self::MESSAGE_ID);

        $call = $transport->calls[0];
        self::assertSame(self::BASE . '/panmail.v1.EventService/ListEvents', $call['url']);
        self::assertSame('test-key', $call['headers']['X-API-Key']);
        self::assertSame(self::MESSAGE_ID, $call['body']['messageId']);
        self::assertSame(100, $call['body']['pageSize']);
        self::assertArrayNotHasKey('pageToken', $call['body']);
    }

    // An empty message id is no filter to the gateway, which would answer
    // with every event in the tenant.
    public function testMessageEventsRefusesAnEmptyMessageID(): void
    {
        $transport = new FakeTransport(self::page(''));

        try {
            self::client($transport)->messageEvents('');
            self::fail('messageEvents accepted an empty message id');
        } catch (InvalidMessageException $e) {
            self::assertStringContainsString('message id is required', $e->getMessage());
        }
        self::assertCount(0, $transport->calls);
    }

    public function testMessageEventsOfAnUnknownMessageIsEmpty(): void
    {
        $transport = new FakeTransport(new Response(200, [], '{}'));

        self::assertSame([], self::client($transport)->messageEvents(self::MESSAGE_ID));
    }

    public function testMessageEventsFollowsEveryPage(): void
    {
        $transport = new FakeTransport(
            self::page('2026-09-01T10:00:01Z|ev_2', self::event('ev_3', 'EMAIL_EVENT_TYPE_OPENED', '2026-09-01T10:00:02Z')),
            self::page('', self::event('ev_1', 'EMAIL_EVENT_TYPE_PENDING', '2026-09-01T10:00:00Z')),
        );

        $events = self::client($transport)->messageEvents(self::MESSAGE_ID);

        self::assertSame(['ev_1', 'ev_3'], self::ids($events));
        self::assertCount(2, $transport->calls);
        self::assertSame('2026-09-01T10:00:01Z|ev_2', $transport->calls[1]['body']['pageToken']);
        self::assertSame(self::MESSAGE_ID, $transport->calls[1]['body']['messageId']);
    }

    public function testMessageEventsReturnsAnEventRepeatedAcrossPagesOnce(): void
    {
        $transport = new FakeTransport(
            self::page('next', self::event('ev_1', 'EMAIL_EVENT_TYPE_PENDING', '2026-09-01T10:00:00Z')),
            self::page('', self::event('ev_1', 'EMAIL_EVENT_TYPE_PENDING', '2026-09-01T10:00:00Z')),
        );

        self::assertSame(['ev_1'], self::ids(self::client($transport)->messageEvents(self::MESSAGE_ID)));
    }

    public function testMessageEventsRefusesATokenItHasAlreadyFollowed(): void
    {
        $transport = new FakeTransport(
            self::page('loop', self::event('ev_1', 'EMAIL_EVENT_TYPE_PENDING', '2026-09-01T10:00:00Z')),
            self::page('loop', self::event('ev_2', 'EMAIL_EVENT_TYPE_SENT', '2026-09-01T10:00:01Z')),
        );

        try {
            self::client($transport)->messageEvents(self::MESSAGE_ID);
            self::fail('messageEvents followed a token twice');
        } catch (TransportException $e) {
            self::assertStringContainsString('a second time', $e->getMessage());
        }
        self::assertCount(2, $transport->calls);
    }

    public function testMessageEventsStopsAtThePageLimit(): void
    {
        $transport = new class () implements Transport {
            public int $calls = 0;

            public function send(string $url, array $headers, string $body, int $timeout): Response
            {
                $this->calls++;

                return new Response(200, [], json_encode([
                    'events' => [['id' => "ev_{$this->calls}", 'timestamp' => '2026-09-01T10:00:00Z']],
                    'nextPageToken' => "token-{$this->calls}",
                ], JSON_THROW_ON_ERROR));
            }
        };

        try {
            self::client($transport)->messageEvents(self::MESSAGE_ID);
            self::fail('messageEvents kept paging');
        } catch (TransportException $e) {
            self::assertStringContainsString('still paging', $e->getMessage());
        }
        self::assertSame(50, $transport->calls);
    }

    // The gateway's two stores order a message's events differently; the
    // result is by time either way, and two events in the same instant keep
    // the order the gateway gave them.
    public function testMessageEventsSortsByTimeKeepingTheGatewaysOrderForTies(): void
    {
        $transport = new FakeTransport(self::page(
            '',
            self::event('ev_b', 'EMAIL_EVENT_TYPE_DELIVERED', '2026-09-01T10:00:05Z'),
            self::event('ev_a', 'EMAIL_EVENT_TYPE_DELIVERED', '2026-09-01T10:00:05Z'),
            self::event('ev_first', 'EMAIL_EVENT_TYPE_SENT', '2026-09-01T09:59:59.5Z'),
        ));

        self::assertSame(
            ['ev_first', 'ev_b', 'ev_a'],
            self::ids(self::client($transport)->messageEvents(self::MESSAGE_ID))
        );
    }

    public function testMessageEventsKeepsATypeTheGatewayHasNoNameFor(): void
    {
        $transport = new FakeTransport(new Response(200, [], '{"events":[{"id":"ev_1","type":99}]}'));

        self::assertSame('99', self::client($transport)->messageEvents(self::MESSAGE_ID)[0]->type);
    }

    public function testMessageEventsReadsAMissingTypeAsUnspecified(): void
    {
        $transport = new FakeTransport(new Response(200, [], '{"events":[{"id":"ev_1"}]}'));

        $event = self::client($transport)->messageEvents(self::MESSAGE_ID)[0];

        self::assertSame(Status::UNSPECIFIED, $event->type);
        self::assertNull($event->timestamp);
    }

    public function testMessageEventsRefusesATimestampThatIsNotOne(): void
    {
        $transport = new FakeTransport(self::page('', self::event('ev_1', 'EMAIL_EVENT_TYPE_SENT', 'yesterday')));

        $this->expectException(TransportException::class);
        self::client($transport)->messageEvents(self::MESSAGE_ID);
    }

    // The subject, the metadata and the tenant are not a sender's to need,
    // so a DeliveryEvent has nowhere to put them.
    public function testMessageEventsDecodesOnlyWhatASenderNeeds(): void
    {
        $properties = array_map(
            static fn (\ReflectionProperty $property): string => $property->getName(),
            (new \ReflectionClass(DeliveryEvent::class))->getProperties()
        );

        self::assertSame(
            ['id', 'messageId', 'type', 'recipient', 'timestamp', 'providerId', 'reason'],
            $properties
        );
    }

    public function testMessageEventsNeedsTheEventsReadScope(): void
    {
        $transport = new FakeTransport(new Response(
            403,
            [],
            '{"code":"permission_denied","message":"api key is missing the \"events:read\" scope"}'
        ));

        try {
            self::client($transport)->messageEvents(self::MESSAGE_ID);
            self::fail('messageEvents returned without an exception');
        } catch (AuthException $e) {
            self::assertStringContainsString('events:read', $e->getMessage());
        }
    }

    /** @return array<string, array{Response, string}> */
    public static function sendRefusals(): array
    {
        return [
            'failed_precondition' => [new Response(400, [], '{"code":"failed_precondition","message":"no"}'), 'failed_precondition'],
            'resource_exhausted' => [new Response(429, [], '{"code":"resource_exhausted","message":"no"}'), 'resource_exhausted'],
            'already_exists' => [new Response(409, [], '{"code":"already_exists","message":"no"}'), 'already_exists'],
        ];
    }

    #[DataProvider('sendRefusals')]
    public function testMessageEventsDoesNotReadSendRefusalsIntoAList(Response $refusal, string $code): void
    {
        $transport = new FakeTransport($refusal);

        try {
            self::client($transport, ['rateLimitRetries' => 3, 'transportRetries' => 3])
                ->messageEvents(self::MESSAGE_ID);
            self::fail('messageEvents returned without an exception');
        } catch (ApiException $e) {
            self::assertSame($code, $e->connectCode);
            self::assertNotInstanceOf(SuppressedRecipientException::class, $e);
            self::assertNotInstanceOf(BacklogFullException::class, $e);
            self::assertNotInstanceOf(IdempotencyKeyConflictException::class, $e);
        }
        self::assertCount(1, $transport->calls);
    }
}
