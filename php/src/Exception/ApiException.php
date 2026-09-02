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
