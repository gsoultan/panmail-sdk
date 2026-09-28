<?php

declare(strict_types=1);

namespace Panmail\Exception;

/**
 * The API key was missing, rejected, or lacks the scope the call needs:
 * email:send to send, providers:read to list providers. The gateway names the
 * missing scope in the message.
 */
final class AuthException extends ApiException
{
}
