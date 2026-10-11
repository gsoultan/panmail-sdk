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
            $payload['attachments'][] = $attachment->toArray();
        }

        // In the body, never as the Idempotency-Key header the gateway also
        // accepts: one place for it, and the same request the Go client sends.
        if ($this->idempotencyKey !== '') {
            $payload['idempotencyKey'] = $this->idempotencyKey;
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
}
