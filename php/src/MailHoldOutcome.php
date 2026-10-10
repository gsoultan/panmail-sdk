<?php

declare(strict_types=1);

namespace Panmail;

/**
 * The payload of the three ways a hold ends: TriggerEvent::MAIL_RELEASED,
 * MAIL_QUARANTINE_REJECTED and MAIL_EXPIRED. It repeats who and what was held,
 * so it stands on its own.
 */
final class MailHoldOutcome implements WebhookPayload
{
    /** @param list<string> $recipients */
    public function __construct(
        /** The id MailHeld carried. */
        public readonly string $reviewId,

        public readonly string $direction,
        public readonly string $messageId,
        public readonly string $ruleId,
        public readonly string $ruleName,
        public readonly string $from,
        public readonly array $recipients,
        public readonly string $subject,
        public readonly ?\DateTimeImmutable $heldAt,

        /** When the hold ended, however it ended. */
        public readonly ?\DateTimeImmutable $decidedAt,

        /** A person's, so empty on an expiry. */
        public readonly string $reviewedBy,

        /** A person's, so empty on an expiry. */
        public readonly string $note,
    ) {
    }
}
