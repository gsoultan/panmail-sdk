<?php

declare(strict_types=1);

namespace Panmail;

/**
 * The payload of TriggerEvent::MAIL_INBOUND: a message that arrived at one of
 * the tenant's mailboxes.
 */
final class InboundMail implements WebhookPayload
{
    /**
     * @param list<string>          $to
     * @param array<string, string> $headers
     */
    public function __construct(
        public readonly string $id,
        public readonly string $from,
        public readonly array $to,
        public readonly string $subject,
        public readonly string $html,
        public readonly string $text,

        /** When the gateway received it; null if absent. */
        public readonly ?\DateTimeImmutable $timestamp,

        public readonly array $headers,
    ) {
    }
}
