<?php

declare(strict_types=1);

namespace Panmail\Exception;

/**
 * The gateway answered its readiness check and said it cannot serve right
 * now: a dependency it needs is down, or it is shutting down.
 *
 * It is the gateway's own answer, not a guess from a status code, so it
 * recovers by itself: the gateway is still running and says ready again once
 * the dependency is back, with no restart. Every other way ready() can fail —
 * a proxy's 503, a 404 from an ingress that does not route /readyz, a dropped
 * connection — is a different exception, because the gateway did not say.
 *
 * $state is the gateway's word for it: "not ready" when a dependency failed,
 * "shutting down" when the instance is draining before it stops. $failed names
 * the dependencies whose check failed, as the gateway names them — "database"
 * — sorted, and is empty while shutting down. $status is the 503 it arrived
 * with; $connectCode is empty, because /readyz is not a Connect procedure.
 */
final class NotReadyException extends ApiException
{
    /** @param list<string> $failed */
    public function __construct(
        string $message,
        public readonly string $state,
        public readonly array $failed,
        int $status,
    ) {
        parent::__construct($message, '', $status);
    }
}
