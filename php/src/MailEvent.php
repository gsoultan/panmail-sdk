<?php

declare(strict_types=1);

namespace Panmail;

/**
 * What the delivery pipeline reports: the payload of TriggerEvent::MAIL_SENT,
 * MAIL_DELIVERED, MAIL_OPENED, MAIL_CLICKED, MAIL_BOUNCED and MAIL_REJECTED.
 * One event for one recipient, the same thing messageEvents() lists.
 */
final class MailEvent implements WebhookPayload
{
    /**
     * @param array<array-key, mixed> $metadata what the provider reported beyond the reason, in
     *                                          its own vocabulary — the clicked URL, an SMTP code.
     *                                          The gateway promises no shape for it. Decoded as
     *                                          json_decode does, so a key that is a number is an
     *                                          int.
     */
    public function __construct(
        /** The event's own id. */
        public readonly string $id,

        /** The id send() returned. */
        public readonly string $messageId,

        public readonly string $providerId,
        public readonly string $providerName,

        /**
         * The precise event, a Status constant. The trigger is coarser:
         * MAIL_BOUNCED fires for BOUNCED, HARD_BOUNCE and SOFT_BOUNCE, and
         * MAIL_REJECTED for REJECTED and DROPPED. A number the gateway's enum
         * has no name for keeps its decimal spelling.
         */
        public readonly string $type,

        public readonly string $recipient,
        public readonly string $subject,

        /**
         * When the gateway recorded the event, which is not the envelope's
         * timestamp: that is when this delivery was sent. Null if absent.
         */
        public readonly ?\DateTimeImmutable $timestamp,

        /** The provider's or the gateway's explanation, where there is one. */
        public readonly string $reason,

        public readonly array $metadata,
    ) {
    }
}
