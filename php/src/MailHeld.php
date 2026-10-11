<?php

declare(strict_types=1);

namespace Panmail;

/**
 * The payload of TriggerEvent::MAIL_HELD: a filter rule quarantined a message
 * for review. It carries no body and no attachment content — only what a
 * person needs to decide whether to look.
 */
final class MailHeld implements WebhookPayload
{
    /**
     * @param list<string>           $recipients
     * @param list<string>           $attachmentNames
     * @param list<MatchedCondition> $matched         why it was held, in the words the review page uses
     */
    public function __construct(
        /** Identifies the hold in the review queue. The outcome that ends it carries the same one. */
        public readonly string $reviewId,

        /** "outbound" or "inbound". */
        public readonly string $direction,

        /** What send() returned, for an outbound message; empty inbound. */
        public readonly string $messageId,

        public readonly string $ruleId,
        public readonly string $ruleName,
        public readonly string $from,
        public readonly array $recipients,
        public readonly string $subject,
        public readonly int $sizeBytes,
        public readonly int $attachmentCount,
        public readonly array $attachmentNames,
        public readonly array $matched,
        public readonly ?\DateTimeImmutable $heldAt,

        /**
         * When retention gives up on it; null if it never does. Escalate
         * before then: what expires unreviewed arrives as MAIL_EXPIRED.
         */
        public readonly ?\DateTimeImmutable $expiresAt,
    ) {
    }
}
