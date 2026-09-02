<?php

declare(strict_types=1);

namespace Panmail\Exception;

/**
 * A webhook delivery that could not be trusted.
 *
 * One class rather than several because there is one thing to do about it:
 * refuse the request. The message says which check failed, for the log.
 */
final class WebhookException extends PanmailException
{
    public function __construct(string $reason)
    {
        parent::__construct("panmail: the webhook could not be verified: $reason");
    }
}
