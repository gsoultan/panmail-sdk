<?php

declare(strict_types=1);

namespace Panmail\Exception;

/**
 * The request did not complete, or what came back was not what the gateway
 * sends.
 *
 * For a send, this is the one outcome where the client cannot know whether the
 * gateway took the message, which is why it is never retried automatically. A
 * listing changes nothing, so one that ends here is safe to call again.
 */
final class TransportException extends PanmailException
{
}
