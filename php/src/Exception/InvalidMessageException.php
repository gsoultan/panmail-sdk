<?php

declare(strict_types=1);

namespace Panmail\Exception;

/**
 * A message this client refused before sending it.
 *
 * Validated locally as well as on the server so that the obvious mistakes cost
 * a clear error rather than a round trip and a rejection phrased in the
 * gateway's terms.
 */
final class InvalidMessageException extends PanmailException
{
}
