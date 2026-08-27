<?php

declare(strict_types=1);

namespace Panmail\Exception;

/**
 * The tenant already has more queued than its own send rate can drain in the
 * near future. The message was not accepted.
 *
 * Unlike a rate refusal this carries no delay, because there is none to give:
 * a queue clearing is not something a caller can schedule against. Retrying on
 * a timer will not help, and retrying immediately makes the wait longer for
 * everything already queued. Slow down, or stop.
 */
final class BacklogFullException extends ApiException
{
}
