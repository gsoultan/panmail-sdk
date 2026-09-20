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
 * A $connectCode of "unknown" with $status 500 is worth reading the message for
 * rather than retrying. The gateway maps only its two capacity refusals to a
 * Connect code; every other refusal a send can make arrives as a bare error,
 * which Connect renders as "unknown". So this one value covers both "the
 * gateway broke, try later" and refusals that will never succeed — a suppressed
 * recipient being the common one, which refuses the whole message and stays
 * refused until the suppression is lifted.
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
