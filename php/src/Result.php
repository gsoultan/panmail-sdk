<?php

declare(strict_types=1);

namespace Panmail;

/** What the gateway accepted. */
final class Result
{
    public function __construct(
        /**
         * Identifies the message for the rest of its life: delivery events,
         * webhook notifications and the analytics pages are all keyed by it.
         * Worth storing next to whatever prompted the send.
         */
        public readonly string $messageId,

        /**
         * The status at the moment of acceptance, which for a queued message
         * is Status::PENDING.
         */
        public readonly string $status,
    ) {
    }
}
