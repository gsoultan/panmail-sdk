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
     * @param string             $providerId   The configured sending provider, as shown on the
     *                                         Email Providers page. Required: panmail will not
     *                                         guess which of a tenant's providers a message goes
     *                                         out through, because the wrong guess is a message
     *                                         sent from the wrong domain.
     * @param string             $from         Must be an address the provider is authorised to send as.
     * @param list<string>       $to
     * @param list<string>       $cc
     * @param list<string>       $bcc
     * @param string             $html         Send both bodies when you can: a text part is what
     *                                         recipients with images off, screen readers and spam
     *                                         filters read.
     * @param string             $templateId   Renders a stored template with $templateData instead
     *                                         of using the bodies above. Subject comes from the
     *                                         template unless set here.
     * @param array<string,mixed> $templateData
     * @param list<Attachment>   $attachments
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

        return $payload;
    }
}
