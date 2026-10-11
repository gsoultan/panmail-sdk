<?php

declare(strict_types=1);

namespace Panmail\Tests;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Panmail\Client;
use Panmail\Exception\ApiException;
use Panmail\Exception\IdempotencyKeyConflictException;
use Panmail\Exception\InvalidMessageException;
use Panmail\Exception\PanmailException;
use Panmail\Exception\TransportException;
use Panmail\Message;
use Panmail\Status;
use Panmail\Transport\Response;

final class IdempotencyTest extends TestCase
{
    private const BASE = 'https://mail.example.com';
    private const KEY = 'order-1042/receipt';

    private static function accepted(string $messageId = 'msg_01'): Response
    {
        return new Response(
            200,
            ['content-type' => 'application/json'],
            json_encode(['messageId' => $messageId, 'status' => Status::PENDING], JSON_THROW_ON_ERROR)
        );
    }

    private static function refusal(int $status, string $code, string $message): Response
    {
        return new Response($status, [], json_encode(['code' => $code, 'message' => $message], JSON_THROW_ON_ERROR));
    }

    /** The one outcome where the client cannot know whether the gateway took the message. */
    private static function dropped(): TransportException
    {
        return new TransportException('panmail: the request did not complete: connection reset by peer');
    }

    private static function message(string $key = ''): Message
    {
        return new Message(
            providerId: '3f1c2b7a-0000-4000-8000-000000000001',
            from: 'app@example.com',
            to: ['user@example.net'],
            subject: 'Hello',
            text: 'hi there',
            idempotencyKey: $key,
        );
    }

    private static function keyed(): Message
    {
        return self::message(self::KEY);
    }

    /** @param array<string, mixed> $options */
    private static function client(FakeTransport $transport, array $options = []): Client
    {
        return new Client(self::BASE, 'test-key', ['transport' => $transport] + $options);
    }

    /** Runs a send expected to fail, and returns what it threw. */
    private static function failedSend(Client $client, Message $message): PanmailException
    {
        try {
            $client->send($message);
        } catch (PanmailException $e) {
            return $e;
        }
        self::fail('send returned without an exception');
    }

    public function testSendCarriesTheIdempotencyKey(): void
    {
        $transport = new FakeTransport(self::accepted());

        self::client($transport)->send(self::keyed());

        self::assertSame(self::KEY, $transport->calls[0]['body']['idempotencyKey']);
        // The field, never the header.
        foreach (array_keys($transport->calls[0]['headers']) as $name) {
            self::assertNotSame('idempotency-key', strtolower($name));
        }
    }

    public function testSendLeavesOutAnAbsentIdempotencyKey(): void
    {
        $transport = new FakeTransport(self::accepted());

        self::client($transport)->send(self::message());

        self::assertArrayNotHasKey('idempotencyKey', $transport->calls[0]['body']);
    }

    /** @return array<string, array{string}> */
    public static function unusableKeys(): array
    {
        return [
            'too long' => [str_repeat('k', 256)],
            'a newline' => ["order-1\n"],
            'not ascii' => ['commande-é'],
            'a control' => ["order\0"],
            'delete' => ["order\x7f"],
        ];
    }

    #[DataProvider('unusableKeys')]
    public function testSendRejectsAnUnusableIdempotencyKey(string $key): void
    {
        $transport = new FakeTransport(self::accepted());

        $e = self::failedSend(self::client($transport), self::message($key));

        self::assertInstanceOf(InvalidMessageException::class, $e);
        self::assertCount(0, $transport->calls);
    }

    public function testSendAcceptsTheLongestIdempotencyKey(): void
    {
        $transport = new FakeTransport(self::accepted());

        $result = self::client($transport)->send(self::message(str_repeat('k', 254) . ' '));

        self::assertSame('msg_01', $result->messageId);
    }

    public function testAReusedIdempotencyKeyIsItsOwnRefusal(): void
    {
        $transport = new FakeTransport(
            self::refusal(409, 'already_exists', 'idempotency key reused with a different request')
        );

        $e = self::failedSend(
            self::client($transport, ['transportRetries' => 3, 'rateLimitRetries' => 3]),
            self::keyed()
        );

        self::assertInstanceOf(IdempotencyKeyConflictException::class, $e);
        self::assertSame('already_exists', $e->connectCode);
        self::assertStringContainsString('reused with a different request', $e->getMessage());
        self::assertCount(1, $transport->calls);
    }

    public function testAlreadyExistsIsNotAConflictOffTheSendPath(): void
    {
        $transport = new FakeTransport(self::refusal(409, 'already_exists', 'something else'));

        try {
            self::client($transport)->listProviders();
            self::fail('listProviders returned without an exception');
        } catch (ApiException $e) {
            self::assertNotInstanceOf(IdempotencyKeyConflictException::class, $e);
            self::assertSame('already_exists', $e->connectCode);
        }
    }

    public function testSendRetriesAnUnknownOutcomeWhenKeyedAndAsked(): void
    {
        $transport = new FakeTransport(self::dropped(), self::accepted('msg_first'));

        $result = self::client($transport, ['transportRetries' => 1])->send(self::keyed());

        self::assertSame('msg_first', $result->messageId);
        self::assertCount(2, $transport->calls);
        foreach ($transport->calls as $call) {
            self::assertSame(self::KEY, $call['body']['idempotencyKey']);
        }
    }

    public function testSendDoesNotRetryAnUnknownOutcomeWithoutAKey(): void
    {
        $transport = new FakeTransport(self::dropped());

        $e = self::failedSend(self::client($transport, ['transportRetries' => 3]), self::message());

        self::assertInstanceOf(TransportException::class, $e);
        self::assertCount(1, $transport->calls);
    }

    public function testSendDoesNotRetryAnUnknownOutcomeByDefault(): void
    {
        $transport = new FakeTransport(self::dropped());

        $e = self::failedSend(self::client($transport), self::keyed());

        self::assertInstanceOf(TransportException::class, $e);
        self::assertCount(1, $transport->calls);
    }

    public function testSendGivesUpOnAnUnknownOutcomeAfterTheConfiguredRetries(): void
    {
        $transport = new FakeTransport(self::dropped());

        $e = self::failedSend(self::client($transport, ['transportRetries' => 2]), self::keyed());

        self::assertInstanceOf(TransportException::class, $e);
        self::assertStringContainsString('did not complete', $e->getMessage());
        self::assertCount(3, $transport->calls);
    }

    public function testSendRetriesAnAbortedKeyedSend(): void
    {
        $transport = new FakeTransport(
            self::refusal(409, 'aborted', 'a request with this idempotency key is still being processed; retry'),
            self::accepted('msg_winner'),
        );

        $result = self::client($transport, ['transportRetries' => 1])->send(self::keyed());

        self::assertSame('msg_winner', $result->messageId);
        self::assertCount(2, $transport->calls);
    }

    public function testSendDoesNotRetryAnAbortedSendWithoutAKey(): void
    {
        $transport = new FakeTransport(self::refusal(409, 'aborted', 'retry'));

        $e = self::failedSend(self::client($transport, ['transportRetries' => 3]), self::message());

        self::assertInstanceOf(ApiException::class, $e);
        self::assertSame('aborted', $e->connectCode);
        self::assertCount(1, $transport->calls);
    }

    public function testSendDoesNotRetryARefusalAsAnUnknownOutcome(): void
    {
        $transport = new FakeTransport(self::refusal(500, 'unknown', 'storage failed'));

        self::failedSend(self::client($transport, ['transportRetries' => 3]), self::keyed());

        self::assertCount(1, $transport->calls);
    }

    public function testSendDoesNotRetryARedirectAsAnUnknownOutcome(): void
    {
        $transport = new FakeTransport(new Response(307, ['location' => 'https://elsewhere.example/'], ''));

        self::failedSend(self::client($transport, ['transportRetries' => 3]), self::keyed());

        self::assertCount(1, $transport->calls);
    }

    public function testNegativeTransportRetriesMeansNone(): void
    {
        $transport = new FakeTransport(self::dropped());

        self::failedSend(self::client($transport, ['transportRetries' => -5]), self::keyed());

        self::assertCount(1, $transport->calls);
    }

    public function testAnUnknownOutcomeOffTheSendPathIsReportedAsItIs(): void
    {
        $transport = new FakeTransport(self::dropped());

        $this->expectException(TransportException::class);
        self::client($transport, ['transportRetries' => 3])->listProviders();
    }
}
