<?php

declare(strict_types=1);

namespace Panmail\Exception;

/**
 * A send addressed to someone on the tenant's suppression list.
 *
 * It refuses the whole message, not just that recipient's copy, and it will be
 * refused identically until the address is removed from the send or the
 * suppression is lifted. Retrying spends attempts on an answer that cannot move.
 *
 * The address and the reason are in the message rather than in properties. The
 * gateway sends them as prose, and parsing prose would break this client the
 * next time somebody rewords it — a worse dependency than reading the message.
 */
final class SuppressedRecipientException extends ApiException
{
}
