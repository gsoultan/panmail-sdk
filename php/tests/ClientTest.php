<?php

declare(strict_types=1);

namespace Panmail\Tests;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Panmail\Attachment;
use Panmail\Client;
use Panmail\Exception\ApiException;
use Panmail\Exception\AuthException;
use Panmail\Exception\BacklogFullException;
use Panmail\Exception\InvalidMessageException;
use Panmail\Exception\RateLimitedException;
use Panmail\Exception\TransportException;
use Panmail\Message;
use Panmail\Status;
use Panmail\Transport\Response;

final class ClientTest extends TestCase
{
    private const BASE = 'https://mail.example.com';

    private static function accepted(string $messageId = 'msg_01'): Response
    {
        return new Response(
            200,
            ['content-type' => 'application/json'],
            json_encode(['messageId' => $messageId, 'status' => Status::PENDING], JSON_THROW_ON_ERROR)
        );
    }

    /** @param array<string, string> $headers */
    private static function refusal(
        int $status,
        string $code,
        string $message,
        array $headers = []
    ): Response {
        return new Response(
            $status,
            $headers,
            json_encode(['code' => $code, 'message' => $message], JSON_THROW_ON_ERROR)
        );
    }

    private static function hello(): Message
    {
        return new Message(
            providerId: '3f1c2b7a-0000-4000-8000-000000000001',
            from: 'app@example.com',
            to: ['user@example.net'],
            subject: 'Hello',
            text: 'hi there',
        );
    }

    /** @param array<string, mixed> $options */
    private static function client(FakeTransport $transport, array $options = []): Client
    {
        return new Client(self::BASE, 'test-key', ['transport' => $transport] + $options);
    }

    public function testSendReturnsTheQueuedMessage(): void
    {
        $transport = new FakeTransport(self::accepted());

        $result = self::client($transport)->send(self::hello());

        self::assertSame('msg_01', $result->messageId);
        self::assertSame(Status::PENDING, $result->status);
    }

    // The key goes in X-API-Key. Authorization carries a dashboard session, and
    // a key sent there is rejected as a malformed session, not as a bad key.
    public function testSendAuthenticatesWithTheApiKeyHeader(): void
    {
        $transport = new FakeTransport(self::accepted());

        self::client($transport)->send(self::hello());

        $headers = $transport->calls[0]['headers'];
        self::assertSame('test-key', $headers['X-API-Key']);
        self::assertArrayNotHasKey('Authorization', $headers);
        self::assertSame('application/json', $headers['Content-Type']);
    }

    public function testSendPostsToTheConnectProcedure(): void
    {
        $transport = new FakeTransport(self::accepted());

        self::client($transport)->send(self::hello());

        self::assertSame(
            self::BASE . '/panmail.v1.EmailService/SendEmail',
            $transport->calls[0]['url']
        );
    }

    public function testSendCarriesTheWholeMessage(): void
    {
        $transport = new FakeTransport(self::accepted());

        self::client($transport)->send(new Message(
            providerId: 'prov',
            from: 'app@example.com',
            to: ['user@example.net'],
            subject: 'Hello',
            html: '<p>hi</p>',
            text: 'hi',
            cc: ['cc@example.net'],
            bcc: ['bcc@example.net'],
            templateId: 'welcome',
            templateData: ['name' => 'Ada'],
        ));

        self::assertSame([
            'providerId' => 'prov',
            'from' => 'app@example.com',
            'to' => ['user@example.net'],
            'cc' => ['cc@example.net'],
            'bcc' => ['bcc@example.net'],
            'subject' => 'Hello',
            'bodyHtml' => '<p>hi</p>',
            'bodyText' => 'hi',
            'templateId' => 'welcome',
            'templateData' => ['name' => 'Ada'],
        ], $transport->calls[0]['body']);
    }

    // A bytes field is base64 in protobuf JSON, and the gateway decodes it as
    // such. Sending raw bytes instead would arrive as mojibake.
    public function testSendBase64EncodesAttachmentContent(): void
    {
        $transport = new FakeTransport(self::accepted());

        self::client($transport)->send(new Message(
            providerId: 'prov',
            from: 'app@example.com',
            to: ['user@example.net'],
            text: 'hi',
            attachments: [new Attachment('receipt.pdf', '%PDF-1.4')],
        ));

        $attachment = $transport->calls[0]['body']['attachments'][0];
        self::assertSame('receipt.pdf', $attachment['filename']);
        self::assertSame('application/pdf', $attachment['contentType']);
        self::assertSame('%PDF-1.4', base64_decode($attachment['content'], true));
    }

    // A UTF-8 CSV guessed as latin-1 is mojibake in a spreadsheet. All four
    // clients send the same header for the same file.
    public function testTextAttachmentsDeclareTheirCharset(): void
    {
        $transport = new FakeTransport(self::accepted());

        self::client($transport)->send(new Message(
            providerId: 'prov',
            from: 'app@example.com',
            to: ['user@example.net'],
            text: 'hi',
            attachments: [new Attachment('report.csv', "a,b\n1,2")],
        ));

        $attachment = $transport->calls[0]['body']['attachments'][0];
        self::assertSame('text/csv; charset=utf-8', $attachment['contentType']);
    }

    /** @return array<string, array{Message}> */
    public static function badMessages(): array
    {
        return [
            'no provider' => [new Message(
                providerId: '',
                from: 'a@example.com',
                to: ['b@example.net'],
                text: 'x'
            )],
            'no from' => [new Message(
                providerId: 'p',
                from: '',
                to: ['b@example.net'],
                text: 'x'
            )],
            'no recipients' => [new Message(providerId: 'p', from: 'a@example.com', text: 'x')],
            'no body or template' => [new Message(
                providerId: 'p',
                from: 'a@example.com',
                to: ['b@example.net']
            )],
            'attachment without a filename' => [new Message(
                providerId: 'p',
                from: 'a@example.com',
                to: ['b@example.net'],
                text: 'x',
                attachments: [new Attachment('', 'x')]
            )],
        ];
    }

    #[DataProvider('badMessages')]
    public function testSendRefusesBadMessagesWithoutCallingTheGateway(Message $message): void
    {
        $transport = new FakeTransport(self::accepted());

        $this->expectException(InvalidMessageException::class);
        try {
            self::client($transport)->send($message);
        } finally {
            self::assertCount(0, $transport->calls);
        }
    }

    // Both capacity refusals are resource_exhausted. Retry-After is the only
    // thing separating them, so this is the test that fails if the gateway
    // stops sending it.
    public function testARateLimitCarriesItsDelay(): void
    {
        $transport = new FakeTransport(
            self::refusal(429, 'resource_exhausted', 'over the send rate', ['retry-after' => '7'])
        );

        try {
            self::client($transport)->send(self::hello());
            self::fail('the refusal was not raised');
        } catch (RateLimitedException $refusal) {
            self::assertSame(7, $refusal->retryAfter);
        }
    }

    public function testTheSameCodeWithoutADelayIsAFullQueue(): void
    {
        $transport = new FakeTransport(
            self::refusal(429, 'resource_exhausted', 'queue too deep')
        );

        $this->expectException(BacklogFullException::class);
        self::client($transport)->send(self::hello());
    }

    public function testAnUnreadableDelayIsStillARateLimit(): void
    {
        $transport = new FakeTransport(self::refusal(
            429,
            'resource_exhausted',
            'over the send rate',
            ['retry-after' => 'Wed, 21 Oct 2026 07:28:00 GMT']
        ));

        try {
            self::client($transport)->send(self::hello());
            self::fail('the refusal was not raised');
        } catch (RateLimitedException $refusal) {
            self::assertSame(0, $refusal->retryAfter);
        }
    }

    /** @return array<string, array{int, string}> */
    public static function authRefusals(): array
    {
        return [
            'unauthenticated' => [401, 'unauthenticated'],
            'permission denied' => [403, 'permission_denied'],
        ];
    }

    #[DataProvider('authRefusals')]
    public function testAuthRefusalsAreAuthErrors(int $status, string $code): void
    {
        $transport = new FakeTransport(self::refusal($status, $code, 'no'));

        $this->expectException(AuthException::class);
        self::client($transport)->send(self::hello());
    }

    public function testAnythingElseKeepsItsCode(): void
    {
        $transport = new FakeTransport(
            self::refusal(400, 'invalid_argument', 'provider_id is not a uuid')
        );

        try {
            self::client($transport)->send(self::hello());
            self::fail('the refusal was not raised');
        } catch (ApiException $refusal) {
            self::assertSame('invalid_argument', $refusal->connectCode);
            self::assertSame(400, $refusal->status);
            self::assertStringContainsString('uuid', $refusal->getMessage());
        }
    }

    // A proxy in front of the gateway answers with its own error page. The
    // status is still worth classifying.
    public function testABodyThatIsNotAnEnvelopeFallsBackToTheStatus(): void
    {
        $transport = new FakeTransport(new Response(401, [], '<html>401 Unauthorized</html>'));

        $this->expectException(AuthException::class);
        self::client($transport)->send(self::hello());
    }

    public function testSendDoesNotRetryByDefault(): void
    {
        $transport = new FakeTransport(
            self::refusal(429, 'resource_exhausted', 'over the send rate', ['retry-after' => '1'])
        );

        try {
            self::client($transport)->send(self::hello());
        } catch (RateLimitedException) {
            // expected
        }

        self::assertCount(1, $transport->calls);
    }

    public function testSendWaitsOutARateLimitWhenAsked(): void
    {
        $transport = new FakeTransport(
            self::refusal(429, 'resource_exhausted', 'over the send rate', ['retry-after' => '1']),
            self::accepted('msg_after_wait')
        );

        $result = self::client($transport, ['rateLimitRetries' => 1])->send(self::hello());

        self::assertSame('msg_after_wait', $result->messageId);
        self::assertCount(2, $transport->calls);
    }

    // A full queue is not something waiting on a timer fixes, and retrying
    // makes the wait longer for everything already queued.
    public function testSendDoesNotRetryAFullQueue(): void
    {
        $transport = new FakeTransport(
            self::refusal(429, 'resource_exhausted', 'queue too deep')
        );

        try {
            self::client($transport, ['rateLimitRetries' => 3])->send(self::hello());
        } catch (BacklogFullException) {
            // expected
        }

        self::assertCount(1, $transport->calls);
    }

    /** @return array<string, array{string, string}> */
    public static function badConstructorInputs(): array
    {
        return [
            'no base url' => ['', 'key'],
            'no scheme' => ['mail.example.com', 'key'],
            'wrong scheme' => ['ftp://mail.example.com', 'key'],
            'no api key' => [self::BASE, ''],
        ];
    }

    #[DataProvider('badConstructorInputs')]
    public function testConstructorValidatesItsInputs(string $baseUrl, string $apiKey): void
    {
        $this->expectException(InvalidMessageException::class);
        new Client($baseUrl, $apiKey);
    }

    public function testABaseUrlWithATrailingSlashDoesNotDoubleTheSeparator(): void
    {
        $transport = new FakeTransport(self::accepted());

        (new Client(self::BASE . '/', 'test-key', ['transport' => $transport]))
            ->send(self::hello());

        self::assertSame(
            self::BASE . '/panmail.v1.EmailService/SendEmail',
            $transport->calls[0]['url']
        );
    }

    public function testACallerSuppliedHeaderCannotOverrideTheApiKey(): void
    {
        $transport = new FakeTransport(self::accepted());

        self::client($transport, ['headers' => ['x-api-key' => 'smuggled', 'X-Trace' => 'abc']])
            ->send(self::hello());

        $headers = $transport->calls[0]['headers'];
        self::assertSame('test-key', $headers['X-API-Key']);
        self::assertArrayNotHasKey('x-api-key', $headers);
        self::assertSame('abc', $headers['X-Trace']);
    }
    /**
     * A 200 whose body is not the send result must still land inside the
     * hierarchy the docblock promises. JSON_THROW_ON_ERROR raises a bare
     * JsonException, which is not a PanmailException and so slips past every
     * catch a caller wrote from that list.
     */
    public function testANonJsonSuccessBodyIsATransportError(): void
    {
        $transport = new FakeTransport(new Response(200, [], '<html>hello from a proxy</html>'));

        $this->expectException(TransportException::class);
        $this->expectExceptionMessage("panmail: the gateway's response was not json");

        self::client($transport)->send(self::hello());
    }

    /** Valid JSON, but not an object: there is no send result to read out of it. */
    public function testAJsonScalarSuccessBodyIsATransportError(): void
    {
        $transport = new FakeTransport(new Response(200, [], '"queued"'));

        $this->expectException(TransportException::class);
        $this->expectExceptionMessage("panmail: the gateway's response was not a send result");

        self::client($transport)->send(self::hello());
    }

    /**
     * A structured value where a scalar belongs is not a string, and casting
     * one raises a warning that phpunit is configured to fail on.
     */
    public function testAStructuredMessageIdDoesNotRaiseAWarning(): void
    {
        $transport = new FakeTransport(
            new Response(200, [], '{"messageId":{"nested":true},"status":"EMAIL_EVENT_TYPE_PENDING"}')
        );

        $result = self::client($transport)->send(self::hello());

        self::assertSame('', $result->messageId);
        self::assertSame(Status::PENDING, $result->status);
    }

    /**
     * The same hazard as the send result, on the refusal path: a structured
     * value where a scalar belongs raises "Array to string conversion" and
     * leaves the caller holding the word "Array" instead of a reason. phpunit
     * is configured to fail on that warning, so this test fails twice over
     * without the guard.
     */
    public function testAStructuredRefusalEnvelopeDoesNotRaiseAWarning(): void
    {
        $payload = '{"code":{"nested":true},"message":{"also":"structured"}}';
        $transport = new FakeTransport(new Response(400, [], $payload));

        try {
            self::client($transport)->send(self::hello());
            self::fail('the refusal was not raised');
        } catch (ApiException $refusal) {
            self::assertSame('', $refusal->connectCode);
            self::assertStringNotContainsString('Array', $refusal->getMessage());
            // Nothing usable was in the envelope, so the body itself is the
            // most informative thing left to report.
            self::assertStringContainsString($payload, $refusal->getMessage());
        }
    }

    /**
     * Recipients go on the wire as a JSON array, and array_filter is the
     * ordinary way a caller ends up holding a non-list: it preserves keys, so
     * dropping one recipient leaves holes. Encoded as-is that becomes a JSON
     * object, which is not what the gateway is expecting.
     */
    public function testFilteredRecipientsStillTravelAsAJsonArray(): void
    {
        $transport = new FakeTransport(self::accepted());
        $kept = array_filter(
            ['a@example.net', 'b@example.net', 'c@example.net'],
            static fn (string $address): bool => $address !== 'b@example.net'
        );
        // The holes are the point: keys 0 and 2, no key 1.
        self::assertSame([0, 2], array_keys($kept), 'the fixture has stopped being a non-list');

        self::client($transport)->send(new Message(
            providerId: 'p',
            from: 'app@example.com',
            to: $kept,
            text: 'hi',
        ));

        $sent = json_encode($transport->calls[0]['body']['to']);
        self::assertSame('["a@example.net","c@example.net"]', $sent);
    }

}
