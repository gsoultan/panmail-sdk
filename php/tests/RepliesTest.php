<?php

declare(strict_types=1);

namespace Panmail\Tests;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Panmail\Attachment;
use Panmail\Client;
use Panmail\Exception\InvalidMessageException;
use Panmail\Exception\PanmailException;
use Panmail\Message;
use Panmail\Status;
use Panmail\Transport\Response;

final class RepliesTest extends TestCase
{
    private static function accepted(): Response
    {
        return new Response(
            200,
            ['content-type' => 'application/json'],
            json_encode(['messageId' => 'msg_01', 'status' => Status::PENDING], JSON_THROW_ON_ERROR)
        );
    }

    private static function client(FakeTransport $transport): Client
    {
        return new Client('https://mail.example.com', 'test-key', ['transport' => $transport]);
    }

    /**
     * @param array<string>    $references
     * @param list<Attachment> $attachments
     */
    private static function message(
        string $replyTo = '',
        string $inReplyTo = '',
        array $references = [],
        string $readReceiptTo = '',
        bool $saveSentCopy = false,
        array $attachments = [],
        string $html = '',
    ): Message {
        return new Message(
            providerId: '3f1c2b7a-0000-4000-8000-000000000001',
            from: 'app@example.com',
            to: ['user@example.net'],
            subject: 'Hello',
            html: $html,
            text: 'hi there',
            attachments: $attachments,
            replyTo: $replyTo,
            inReplyTo: $inReplyTo,
            references: $references,
            readReceiptTo: $readReceiptTo,
            saveSentCopy: $saveSentCopy,
        );
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

    public function testSendCarriesTheReplyFields(): void
    {
        $transport = new FakeTransport(self::accepted());

        self::client($transport)->send(self::message(
            replyTo: 'support@example.com',
            inReplyTo: '<b@example.org>',
            references: ['<a@example.org>', '<b@example.org>'],
            readReceiptTo: 'app@example.com',
            saveSentCopy: true,
        ));

        $body = $transport->calls[0]['body'];
        self::assertSame('support@example.com', $body['replyTo']);
        self::assertSame('<b@example.org>', $body['inReplyTo']);
        self::assertSame(['<a@example.org>', '<b@example.org>'], $body['references']);
        self::assertSame('app@example.com', $body['dispositionNotificationTo']);
        self::assertTrue($body['saveSentCopy']);
    }

    public function testSendLeavesOutAbsentReplyFields(): void
    {
        $transport = new FakeTransport(self::accepted());

        self::client($transport)->send(self::message());

        foreach (['replyTo', 'inReplyTo', 'references', 'dispositionNotificationTo', 'saveSentCopy'] as $field) {
            self::assertArrayNotHasKey($field, $transport->calls[0]['body']);
        }
    }

    /** @return array<string, array{string, string}> */
    public static function unusableMessageIds(): array
    {
        $ids = [
            'no brackets' => 'b@example.org',
            'no at' => '<b.example.org>',
            'nothing left' => '<@example.org>',
            'nothing right' => '<b@>',
            'a space' => '<b @example.org>',
            'a newline' => "<b@example.org>\r\nBcc: x@example.net",
            'not ascii' => '<é@example.org>',
            'too long' => '<' . str_repeat('b', 250) . '@example.org>',
        ];
        $cases = [];
        foreach ($ids as $name => $id) {
            foreach (['in-reply-to', 'references'] as $field) {
                $cases[$name . '/' . $field] = [$field, $id];
            }
        }

        return $cases;
    }

    #[DataProvider('unusableMessageIds')]
    public function testSendRejectsAnUnusableMessageId(string $field, string $id): void
    {
        $transport = new FakeTransport(self::accepted());
        $message = $field === 'in-reply-to'
            ? self::message(inReplyTo: $id)
            : self::message(references: ['<a@example.org>', $id]);

        $e = self::failedSend(self::client($transport), $message);

        self::assertInstanceOf(InvalidMessageException::class, $e);
        self::assertCount(0, $transport->calls);
    }

    public function testSendRejectsTooManyReferences(): void
    {
        $references = [];
        for ($i = 0; $i < 101; $i++) {
            $references[] = '<' . $i . '@example.org>';
        }
        $transport = new FakeTransport(self::accepted());

        $e = self::failedSend(self::client($transport), self::message(references: $references));

        self::assertInstanceOf(InvalidMessageException::class, $e);
        self::assertCount(0, $transport->calls);

        self::client($transport)->send(self::message(references: array_slice($references, 0, 100)));
        self::assertCount(1, $transport->calls);
    }

    public function testSendCarriesAnInlineAttachmentsContentId(): void
    {
        $transport = new FakeTransport(self::accepted());

        self::client($transport)->send(self::message(
            html: '<img src="cid:logo">',
            attachments: [
                new Attachment('logo.png', 'png', contentId: 'logo'),
                new Attachment('receipt.pdf', '%PDF-1.4'),
            ],
        ));

        $attachments = $transport->calls[0]['body']['attachments'];
        self::assertSame('logo', $attachments[0]['contentId']);
        self::assertArrayNotHasKey('contentId', $attachments[1]);
    }

    /** @return array<string, array{string}> */
    public static function unusableContentIds(): array
    {
        return [
            'angle brackets' => ['<logo>'],
            'a space' => ['my logo'],
            'a newline' => ["logo\r\n"],
            'not ascii' => ['logó'],
            'too long' => [str_repeat('l', 256)],
        ];
    }

    #[DataProvider('unusableContentIds')]
    public function testSendRejectsAnUnusableContentId(string $id): void
    {
        $transport = new FakeTransport(self::accepted());
        $message = self::message(attachments: [new Attachment('logo.png', 'png', contentId: $id)]);

        $e = self::failedSend(self::client($transport), $message);

        self::assertInstanceOf(InvalidMessageException::class, $e);
        self::assertCount(0, $transport->calls);
    }
}
