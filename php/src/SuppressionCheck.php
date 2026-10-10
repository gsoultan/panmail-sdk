<?php

declare(strict_types=1);

namespace Panmail;

/** Whether the tenant's suppression list holds an address. */
final class SuppressionCheck
{
    public function __construct(
        /** True when a send to the address would be refused with a SuppressedRecipientException. */
        public readonly bool $suppressed,

        /**
         * Why it was suppressed, as recorded: a bounce, a complaint, or
         * whatever an operator typed. Empty when not suppressed.
         */
        public readonly string $reason,
    ) {
    }
}
