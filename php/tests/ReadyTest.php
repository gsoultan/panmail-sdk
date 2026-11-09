<?php

declare(strict_types=1);

namespace Panmail\Tests;

use PHPUnit\Framework\TestCase;
use Panmail\Client;
use Panmail\Exception\ApiException;
use Panmail\Exception\AuthException;
use Panmail\Exception\NotReadyException;
use Panmail\Exception\TransportException;
use Panmail\Transport\Response;

final class ReadyTest extends TestCase
{
    private const BASE = 'https://mail.example.com';

    // The gateway's bodies, byte for byte: internal/health's ReadyHandler
    // encodes them with json.Encoder, newline and all.
    private const READY_BODY = "{\"status\":\"ready\"}\n";
    private const NOT_READY_BODY = "{\"failed\":[\"database\"],\"status\":\"not ready\"}\n";
    private const SHUTTING_DOWN_BODY = "{\"status\":\"shutting down\"}\n";

    /** @param array<string, mixed> $options */
    private static function client(FakeTransport $transport, array $options = []): Client
    {
        return new Client(self::BASE, 'test-key', ['transport' => $transport] + $options);
    }

    private static function answer(int $status, string $body): FakeTransport
    {
        return new FakeTransport(new Response($status, ['content-type' => 'application/json'], $body));
    }

    public function testReady(): void
    {
        $transport = self::answer(200, self::READY_BODY);

        self::client($transport)->ready();

        $call = $transport->calls[0];
        self::assertSame(self::BASE . '/readyz', $call['url']);
        self::assertSame([], $call['body']);
    }

    // The endpoint is unauthenticated; the key would only be one more place
    // for a tenant-wide credential to be logged.
    public function testReadySendsNoApiKey(): void
    {
        $transport = self::answer(200, self::READY_BODY);

        self::client($transport)->ready();

        self::assertArrayNotHasKey('X-API-Key', $transport->calls[0]['headers']);
    }

    // A proxy that wants a header for a send wants it for this too.
    public function testReadySendsTheExtraHeaders(): void
    {
        $transport = self::answer(200, self::READY_BODY);

        self::client($transport, ['headers' => ['X-Proxy-Token' => 'letmein']])->ready();

        self::assertSame('letmein', $transport->calls[0]['headers']['X-Proxy-Token']);
    }

    public function testReadyReportsTheDependencyThatFailed(): void
    {
        try {
            self::client(self::answer(503, self::NOT_READY_BODY))->ready();
            self::fail('ready() accepted a gateway that said it was not ready');
        } catch (NotReadyException $e) {
            self::assertSame('not ready', $e->state);
            self::assertSame(['database'], $e->failed);
            self::assertSame(503, $e->status);
            self::assertStringContainsString('"database"', $e->getMessage());
        }
    }

    public function testReadyReportsAGatewayShuttingDown(): void
    {
        try {
            self::client(self::answer(503, self::SHUTTING_DOWN_BODY))->ready();
            self::fail('ready() accepted a gateway that was shutting down');
        } catch (NotReadyException $e) {
            self::assertSame('shutting down', $e->state);
            self::assertSame([], $e->failed);
            self::assertStringContainsString('shutting down', $e->getMessage());
        }
    }

    /**
     * A 200 is not "ready" unless it says so. An older gateway, or a proxy
     * that serves a web app on every path it does not know, answers /readyz
     * with a page.
     *
     * @return array<string, array{string}>
     */
    public static function notReadinessAnswers(): array
    {
        return [
            'html' => ['<!doctype html><title>panmail</title>'],
            'other json' => ['{"messageId":"msg_01"}'],
            'empty' => [''],
        ];
    }

    #[\PHPUnit\Framework\Attributes\DataProvider('notReadinessAnswers')]
    public function testReadyDoesNotTakeA200WithoutAReadinessAnswer(string $body): void
    {
        $this->expectException(TransportException::class);
        self::client(self::answer(200, $body))->ready();
    }

    // A 503 from a proxy is not the gateway's answer, and it is not dressed up
    // as one; a 401 is something in front of the gateway, since no key was
    // sent for it to refuse.
    public function testReadyReportsAnyOtherAnswerAsAnApiException(): void
    {
        foreach ([[503, '<html>upstream unavailable</html>'], [404, '404 page not found'], [401, 'auth required']] as [$status, $body]) {
            try {
                self::client(self::answer($status, $body))->ready();
                self::fail("ready() accepted a $status");
            } catch (NotReadyException | AuthException $e) {
                self::fail("$status: " . $e::class . ' read into an answer that was not the gateway\'s');
            } catch (ApiException $e) {
                self::assertSame($status, $e->status);
            }
        }
    }

    // The answer is a few dozen bytes; one that is not is read only as far as
    // the bound, and never mistaken for ready.
    public function testReadyBoundsTheAnswer(): void
    {
        $huge = '{"status":"ready","pad":"' . str_repeat('x', 1 << 17) . '"}';

        $this->expectException(TransportException::class);
        self::client(self::answer(200, $huge))->ready();
    }

    public function testReadyReportsARequestThatDidNotComplete(): void
    {
        $transport = new FakeTransport(new TransportException('panmail: the request did not complete: refused'));

        $this->expectException(TransportException::class);
        self::client($transport)->ready();
    }
}
