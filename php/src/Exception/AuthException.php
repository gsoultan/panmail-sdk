<?php

declare(strict_types=1);

namespace Panmail\Exception;

/** The API key was missing, rejected, or lacks the email:send scope. */
final class AuthException extends ApiException
{
}
