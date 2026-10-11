<?php

declare(strict_types=1);

namespace Panmail\Exception;

/**
 * A send whose idempotency key the gateway had already accepted, within the
 * last 24 hours, for a different request.
 *
 * Nothing was sent. Answering with the first message would tell the caller
 * something it did not ask for had gone out, and queueing this one would make
 * the key mean nothing, so the gateway does neither. It is a bug in how keys
 * are chosen — two different messages given the same one — and repeating it
 * gets the same answer until the key changes or the day is out.
 */
final class IdempotencyKeyConflictException extends ApiException
{
}
