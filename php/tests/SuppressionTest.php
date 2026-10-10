<?php

declare(strict_types=1);

namespace Panmail\Tests;

use PHPUnit\Framework\TestCase;
use Panmail\Client;
use Panmail\Exception\ApiException;
use Panmail\Exception\AuthException;
use Panmail\Exception\InvalidMessageException;
use Panmail\Exception\SuppressedRecipientException;
use Panmail\SuppressionCheck;
use Panmail\Transport\Response;

final class SuppressionTest extends TestCase
{
    private const BASE = 'https://mail.example.com';

    /** @param array<string, mixed> $options */
    private static function client(FakeTransport $transport, array $options = []): Client
    {
        return new Client(self::BASE, 'test-key', ['transport' => $transport] + $options);
    }

    private static function empty(): Response
    {
        return new Response(200, ['content-type' => 'application/json'], '{}');
    }

    public function testCheckSuppression(): void
    {
        $transport = new FakeTransport(new Response(200, [], '{"isSuppressed":true,"reason":"hard bounce"}'));

        $check = self::client($transport)->checkSuppression('Bounced <bounced@example.net>');

        self::assertEquals(new SuppressionCheck(suppressed: true, reason: 'hard bounce'), $check);
        $call = $transport->calls[0];
        self::assertSame(self::BASE . '/panmail.v1.SuppressionService/CheckSuppression', $call['url']);
        self::assertSame('test-key', $call['headers']['X-API-Key']);
        // Sent as given: the gateway, not the client, decides what a mailbox is.
        self::assertSame('Bounced <bounced@example.net>', $call['body']['email']);
    }

    // protobuf JSON leaves false and "" out, so an address not on the list
    // is an empty object.
    public function testCheckSuppressionOfAnAddressNotOnTheList(): void
    {
        $check = self::client(new FakeTransport(self::empty()))->checkSuppression('fine@example.net');

        self::assertFalse($check->suppressed);
        self::assertSame('', $check->reason);
    }

    public function testCheckSuppressionRefusesAnEmptyAddress(): void
    {
        $transport = new FakeTransport(self::empty());

        try {
            self::client($transport)->checkSuppression('');
            self::fail('checkSuppression accepted an empty address');
        } catch (InvalidMessageException $e) {
            self::assertStringContainsString('email address is required', $e->getMessage());
        }
        self::assertCount(0, $transport->calls);
    }

    public function testCheckSuppressionNeedsTheSuppressionsReadScope(): void
    {
        $transport = new FakeTransport(new Response(
            403,
            [],
            '{"code":"permission_denied","message":"api key is missing the \"suppressions:read\" scope"}'
        ));

        $this->expectException(AuthException::class);
        $this->expectExceptionMessage('suppressions:read');
        self::client($transport)->checkSuppression('someone@example.net');
    }

    public function testRemoveSuppression(): void
    {
        $transport = new FakeTransport(self::empty());

        self::client($transport)->removeSuppression('bounced@example.net');

        $call = $transport->calls[0];
        self::assertSame(self::BASE . '/panmail.v1.SuppressionService/RemoveSuppression', $call['url']);
        self::assertSame('bounced@example.net', $call['body']['email']);
    }

    public function testRemoveSuppressionRefusesAnEmptyAddress(): void
    {
        $transport = new FakeTransport(self::empty());

        try {
            self::client($transport)->removeSuppression('');
            self::fail('removeSuppression accepted an empty address');
        } catch (InvalidMessageException) {
            // expected
        }
        self::assertCount(0, $transport->calls);
    }

    public function testRemoveSuppressionNeedsTheSuppressionsWriteScope(): void
    {
        $transport = new FakeTransport(new Response(
            403,
            [],
            '{"code":"permission_denied","message":"api key is missing the \"suppressions:write\" scope"}'
        ));

        $this->expectException(AuthException::class);
        $this->expectExceptionMessage('suppressions:write');
        self::client($transport)->removeSuppression('someone@example.net');
    }

    // failed_precondition is a suppressed recipient on a send, and nothing of
    // the kind here; neither call is retried, whatever the options say.
    public function testSuppressionCallsDoNotReadSendRefusalsIntoThem(): void
    {
        $transport = new FakeTransport(new Response(
            400,
            ['retry-after' => '1'],
            '{"code":"failed_precondition","message":"no"}'
        ));
        $client = self::client($transport, ['rateLimitRetries' => 3, 'transportRetries' => 3]);

        foreach ([
            static fn () => $client->checkSuppression('someone@example.net'),
            static fn () => $client->removeSuppression('someone@example.net'),
        ] as $call) {
            try {
                $call();
                self::fail('the call returned without an exception');
            } catch (ApiException $e) {
                self::assertSame('failed_precondition', $e->connectCode);
                self::assertNotInstanceOf(SuppressedRecipientException::class, $e);
            }
        }
        self::assertCount(2, $transport->calls);
    }
}
