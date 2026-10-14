<?php

declare(strict_types=1);

namespace Panmail;

use Panmail\Exception\InvalidMessageException;

/**
 * The mail to send.
 *
 * Either a body (html, text, or both) or a templateId is required — a message
 * with neither has nothing to say, and the gateway refuses it.
 */
final class Message
{
    /**
     * The gateway's bound on an idempotency key, in bytes. Keys are printable
     * ASCII, so bytes and characters are the same count.
     */
    private const MAX_IDEMPOTENCY_KEY_LENGTH = 255;

    /** The gateway's bounds on a Message-ID and on how many References a message may carry. */
    private const MAX_MESSAGE_ID_LENGTH = 255;
    private const MAX_REFERENCES = 100;

    /**
     * @param string             $providerId   The configured sending provider, as shown on the
     *                                         Email Providers page. Required: panmail will not
     *                                         guess which of a tenant's providers a message goes
     *                                         out through, because the wrong guess is a message
     *                                         sent from the wrong domain.
     * @param string             $from         Must be an address the provider is authorised to send as.
     * @param array<string>      $to  Keys are ignored: the wire form is a JSON
     *                                array, and array_values below is what
     *                                guarantees that after a caller has
     *                                array_filter'd a list and left holes in it
     * @param array<string>      $cc
     * @param array<string>      $bcc
     * @param string             $html         Send both bodies when you can: a text part is what
     *                                         recipients with images off, screen readers and spam
     *                                         filters read.
     * @param string             $templateId   Renders a stored template with $templateData instead
     *                                         of using the bodies above. Subject comes from the
     *                                         template unless set here.
     * @param array<string,mixed> $templateData
     * @param list<Attachment>   $attachments
     * @param string             $idempotencyKey Makes a repeated send safe. The gateway remembers
     *                                         the key for 24 hours alongside the message it
     *                                         accepted, and answers the same key with the same
     *                                         request by returning that message rather than
     *                                         queueing another; the same key with a different
     *                                         request is an IdempotencyKeyConflictException.
     *                                         Name the thing being sent, not the attempt — an
     *                                         order id and the kind of mail — so a retry from
     *                                         anywhere carries the same key. At most 255
     *                                         printable ASCII characters. Setting it is what
     *                                         lets the transportRetries option repeat a send
     *                                         whose outcome is unknown.
     * @param string             $replyTo      Where replies should go, when that is not $from — a
     *                                         support queue answering mail sent from a no-reply
     *                                         address, say.
     * @param string             $inReplyTo    The Message-ID of the message this one answers, angle
     *                                         brackets included (<id@example.com>), so the
     *                                         recipient's client threads the two.
     * @param array<string>      $references   The conversation's Message-IDs so far, oldest first,
     *                                         angle brackets included. At most 100; when they do
     *                                         not all fit on one header line the gateway keeps the
     *                                         first and the most recent, as RFC 5322 describes.
     * @param string             $readReceiptTo Asks for a read receipt, sent to this address. It
     *                                         goes out as the Disposition-Notification-To header,
     *                                         and the recipient's client decides whether to honour
     *                                         it.
     * @param bool               $saveSentCopy Files a copy of the message, as sent, in the sender's
     *                                         Sent folder, the way the tenant's Sent copies setting
     *                                         says to. Does nothing when that setting is off.
     */
    public function __construct(
        public readonly string $providerId,
        public readonly string $from,
        public readonly array $to = [],
        public readonly string $subject = '',
        public readonly string $html = '',
        public readonly string $text = '',
        public readonly array $cc = [],
        public readonly array $bcc = [],
        public readonly string $templateId = '',
        public readonly array $templateData = [],
        public readonly array $attachments = [],
        public readonly string $idempotencyKey = '',
        public readonly string $replyTo = '',
        public readonly string $inReplyTo = '',
        public readonly array $references = [],
        public readonly string $readReceiptTo = '',
        public readonly bool $saveSentCopy = false,
    ) {
    }

    /**
     * Validates the message and converts it to the wire form.
     *
     * The field names are the protobuf JSON names of SendEmailRequest. The
     * gateway accepts the original snake_case spellings too, but camelCase is
     * what its own web UI sends and so the better-travelled path.
     *
     * @return array<string, mixed>
     *
     * @throws InvalidMessageException
     */
    public function toArray(): array
    {
        if ($this->providerId === '') {
            throw new InvalidMessageException('panmail: provider id is required');
        }
        if ($this->from === '') {
            throw new InvalidMessageException('panmail: from address is required');
        }
        if (count($this->to) + count($this->cc) + count($this->bcc) === 0) {
            throw new InvalidMessageException('panmail: at least one recipient is required');
        }
        if ($this->html === '' && $this->text === '' && $this->templateId === '') {
            throw new InvalidMessageException(
                'panmail: a message needs an html body, a text body or a template id'
            );
        }

        self::validateIdempotencyKey($this->idempotencyKey);
        self::validateThreading($this->inReplyTo, $this->references);

        $payload = [
            'providerId' => $this->providerId,
            'from' => $this->from,
        ];

        if ($this->to !== []) {
            $payload['to'] = array_values($this->to);
        }
        if ($this->cc !== []) {
            $payload['cc'] = array_values($this->cc);
        }
        if ($this->bcc !== []) {
            $payload['bcc'] = array_values($this->bcc);
        }
        if ($this->subject !== '') {
            $payload['subject'] = $this->subject;
        }
        if ($this->html !== '') {
            $payload['bodyHtml'] = $this->html;
        }
        if ($this->text !== '') {
            $payload['bodyText'] = $this->text;
        }
        if ($this->templateId !== '') {
            $payload['templateId'] = $this->templateId;
        }
        if ($this->templateData !== []) {
            // The gateway's template_data is a google.protobuf.Struct, which
            // is JSON and nothing more.
            $payload['templateData'] = (object) $this->templateData;
        }

        foreach ($this->attachments as $attachment) {
            if ($attachment->filename === '') {
                throw new InvalidMessageException('panmail: every attachment needs a filename');
            }
            self::validateContentId($attachment->filename, $attachment->contentId);
            $payload['attachments'][] = $attachment->toArray();
        }

        // In the body, never as the Idempotency-Key header the gateway also
        // accepts: one place for it, and the same request the Go client sends.
        if ($this->idempotencyKey !== '') {
            $payload['idempotencyKey'] = $this->idempotencyKey;
        }

        if ($this->replyTo !== '') {
            $payload['replyTo'] = $this->replyTo;
        }
        if ($this->inReplyTo !== '') {
            $payload['inReplyTo'] = $this->inReplyTo;
        }
        if ($this->references !== []) {
            $payload['references'] = array_values($this->references);
        }
        if ($this->readReceiptTo !== '') {
            $payload['dispositionNotificationTo'] = $this->readReceiptTo;
        }
        if ($this->saveSentCopy) {
            $payload['saveSentCopy'] = true;
        }

        return $payload;
    }

    /**
     * The gateway's own rule, so a key it would refuse costs a clear error
     * here rather than a round trip.
     *
     * @throws InvalidMessageException
     */
    private static function validateIdempotencyKey(string $key): void
    {
        if (strlen($key) > self::MAX_IDEMPOTENCY_KEY_LENGTH) {
            throw new InvalidMessageException(
                'panmail: the idempotency key is ' . strlen($key) . ' characters long; the limit is '
                . self::MAX_IDEMPOTENCY_KEY_LENGTH
            );
        }
        if (preg_match('/[^\x20-\x7e]/', $key, $match, PREG_OFFSET_CAPTURE) === 1) {
            throw new InvalidMessageException(
                'panmail: the idempotency key may contain only printable ASCII; byte '
                . $match[0][1] . ' is not'
            );
        }
    }

    /**
     * The gateway's rules for In-Reply-To and References. The addresses —
     * replyTo, readReceiptTo — are left to the gateway, as from and the
     * recipients are: its parser is the one that counts.
     *
     * @param array<string> $references
     *
     * @throws InvalidMessageException
     */
    private static function validateThreading(string $inReplyTo, array $references): void
    {
        if ($inReplyTo !== '') {
            self::validateMessageId('in-reply-to', $inReplyTo);
        }
        if (count($references) > self::MAX_REFERENCES) {
            throw new InvalidMessageException(
                'panmail: ' . count($references) . ' references; the limit is ' . self::MAX_REFERENCES
            );
        }
        foreach ($references as $id) {
            self::validateMessageId('references', $id);
        }
    }

    /**
     * The shape RFC 5322 gives a msg-id, <left@right> in printable ASCII
     * without spaces — the same check the gateway makes.
     *
     * @throws InvalidMessageException
     */
    private static function validateMessageId(string $field, string $id): void
    {
        $problem = null;
        $inner = substr($id, 1, -1);
        $at = strpos($inner, '@');
        if (strlen($id) > self::MAX_MESSAGE_ID_LENGTH) {
            $problem = 'the Message-ID is ' . strlen($id) . ' characters long; the limit is '
                . self::MAX_MESSAGE_ID_LENGTH;
        } elseif (strlen($id) < 5 || $id[0] !== '<' || $id[strlen($id) - 1] !== '>') {
            $problem = 'the Message-ID "' . $id . '" must be in angle brackets, like <id@example.com>';
        } elseif (preg_match('/[^\x21-\x7e]|[<>]/', $inner) === 1) {
            $problem = 'the Message-ID "' . $id . '" may contain only printable ASCII without spaces';
        } elseif ($at === false || $at === 0 || $at === strlen($inner) - 1) {
            $problem = 'the Message-ID "' . $id . '" must have the form <left@right>';
        }
        if ($problem !== null) {
            throw new InvalidMessageException('panmail: ' . $field . ': ' . $problem);
        }
    }

    /**
     * The gateway's rule for an inline attachment's Content-ID: one token of
     * printable ASCII, without the angle brackets the header adds.
     *
     * @throws InvalidMessageException
     */
    private static function validateContentId(string $filename, string $id): void
    {
        if (strlen($id) > self::MAX_MESSAGE_ID_LENGTH) {
            throw new InvalidMessageException(
                'panmail: attachment "' . $filename . '": content id is ' . strlen($id)
                . ' characters long; the limit is ' . self::MAX_MESSAGE_ID_LENGTH
            );
        }
        if (preg_match('/[^\x21-\x7e]|[<>]/', $id) === 1) {
            throw new InvalidMessageException(
                'panmail: attachment "' . $filename
                . '": content id may contain only printable ASCII without spaces or angle brackets'
            );
        }
    }
}
