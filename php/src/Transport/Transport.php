<?php

declare(strict_types=1);

namespace Panmail\Transport;

use Panmail\Exception\TransportException;

/**
 * How the client reaches the gateway.
 *
 * The default is CurlTransport, which needs nothing beyond the bundled cURL
 * extension. Implement this to route sends through Guzzle, a PSR-18 client, or
 * a test double.
 */
interface Transport
{
    /**
     * @param array<string, string> $headers
     *
     * @throws TransportException when the request did not complete
     */
    public function send(string $url, array $headers, string $body, int $timeout): Response;
}
