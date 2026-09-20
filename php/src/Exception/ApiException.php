<?php

declare(strict_types=1);

namespace Panmail\Exception;

/**
 * A refusal this client has no more specific type for.
 *
 * $connectCode is the Connect error code the gateway sent —
 * "invalid_argument", "internal" and so on — and $status is the HTTP status it
 * arrived with.
 *
 * $connectCode tells you whether retrying can help. "unknown" is a failure —
 * storage, a provider connection — and is the one worth a backoff. Everything
 * else is a refusal: the same request answered the same way until something
 * changes, so "invalid_argument" wants the request fixed rather than repeated.
 * The refusals worth branching on have their own types; this is what is left.
 *
 * It is not called `$code`, as it is in the Go and Node clients, because
 * \Exception already declares `$code` as a non-readonly int. Redeclaring it as
 * a readonly string is a fatal error, not a warning.
 */
class ApiException extends PanmailException
{
    public function __construct(
        string $message,
        public readonly string $connectCode = '',
        public readonly int $status = 0,
    ) {
        parent::__construct($message);
    }
}
