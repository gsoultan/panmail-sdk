<?php

declare(strict_types=1);

namespace Panmail\Exception;

/**
 * The tenant is sending faster than its configured rate allows.
 *
 * The message was not accepted, so sending it again after $retryAfter seconds
 * is safe.
 */
final class RateLimitedException extends ApiException
{
    public function __construct(
        string $message,
        /** Seconds to wait, or 0 when the gateway quoted no usable delay. */
        public readonly int $retryAfter = 0,
        string $connectCode = '',
        int $status = 0,
    ) {
        parent::__construct($message, $connectCode, $status);
    }
}
