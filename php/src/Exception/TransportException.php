<?php

declare(strict_types=1);

namespace Panmail\Exception;

/**
 * The send did not complete.
 *
 * This is the one outcome where the client cannot know whether the gateway
 * took the message, which is why it is never retried automatically.
 */
final class TransportException extends PanmailException
{
}
