<?php

declare(strict_types=1);

namespace Panmail;

use Panmail\Exception\PanmailException;

/**
 * Carries an error that leaves a send's outcome unknown from where it is
 * raised to the loop in Client::send() that decides whether to ask again.
 * Never escapes the client: send() throws the error it carries.
 *
 * @internal
 */
final class UnknownOutcome extends \RuntimeException
{
    public function __construct(public readonly PanmailException $error)
    {
        parent::__construct($error->getMessage(), 0, $error);
    }
}
