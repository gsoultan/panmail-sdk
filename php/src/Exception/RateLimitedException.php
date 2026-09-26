<?php

declare(strict_types=1);

namespace Panmail\Exception;

/**
 * A send over a configured rate.
 *
 * The message was not accepted, so sending it again after $retryAfter seconds
 * is safe.
 *
 * The rate is the tenant's, or the provider's the message named — the gateway
 * gives each provider its own ceiling and decides both in one step, and the
 * refusal does not say which one was hit. $retryAfter is right either way: it
 * is how long until the combined decision would allow the send. What it cannot
 * tell you is whether a different provider would have taken the message now,
 * which is sometimes the thing worth knowing.
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
