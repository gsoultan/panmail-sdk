<?php

declare(strict_types=1);

namespace Panmail\Transport;

use Panmail\Exception\TransportException;

/** The default transport: an ordinary POST with the bundled cURL extension. */
final class CurlTransport implements Transport
{
    /**
     * Bounds what a single response may cost in memory. A send response is a
     * message id and a status; anything approaching this is a proxy error
     * page, not the gateway.
     */
    private const MAX_RESPONSE_BYTES = 1 << 20;

    public function send(string $url, array $headers, string $body, int $timeout): Response
    {
        $headerLines = [];
        foreach ($headers as $name => $value) {
            $headerLines[] = "$name: $value";
        }

        $responseHeaders = [];
        $payload = '';
        $size = 0;
        $tooLarge = false;

        $ch = curl_init($url);
        if ($ch === false) {
            throw new TransportException('panmail: could not initialise curl');
        }

        curl_setopt_array($ch, [
            CURLOPT_POST => true,
            CURLOPT_TIMEOUT => $timeout,
            CURLOPT_HTTPHEADER => $headerLines,
            CURLOPT_POSTFIELDS => $body,
            // X-API-Key is a header cURL has never heard of, so nothing would
            // drop it if a redirect crossed to another host. Following one
            // would hand the tenant's api key to whatever host the Location
            // named. A Connect procedure has no reason to redirect. This is
            // cURL's default, set here because it is a decision, not an
            // accident.
            CURLOPT_FOLLOWLOCATION => false,
            // Metered rather than buffered whole, which is what
            // CURLOPT_RETURNTRANSFER would do.
            CURLOPT_WRITEFUNCTION => function ($ch, string $chunk) use (
                &$payload,
                &$size,
                &$tooLarge
            ): int {
                $size += strlen($chunk);
                if ($size > self::MAX_RESPONSE_BYTES) {
                    // Any count but the chunk's length aborts the transfer.
                    $tooLarge = true;

                    return 0;
                }
                $payload .= $chunk;

                return strlen($chunk);
            },
            CURLOPT_HEADERFUNCTION => function ($ch, string $line) use (&$responseHeaders): int {
                $parts = explode(':', $line, 2);
                if (count($parts) === 2) {
                    $responseHeaders[strtolower(trim($parts[0]))] = trim($parts[1]);
                }

                return strlen($line);
            },
        ]);

        $ok = curl_exec($ch);
        $status = (int) curl_getinfo($ch, CURLINFO_HTTP_CODE);
        $error = curl_error($ch);
        // No curl_close(): it has had no effect since 8.0, when the handle
        // became an object the collector frees, and 8.5 deprecates calling it.

        if ($tooLarge) {
            throw new TransportException(
                "panmail: the gateway's response exceeded " . self::MAX_RESPONSE_BYTES . ' bytes'
            );
        }
        if ($ok === false) {
            // Deliberately not classified and never retried: a transport error
            // is the one outcome where the client does not know whether the
            // gateway took the message.
            throw new TransportException("panmail: the send did not complete: $error");
        }

        return new Response($status, $responseHeaders, $payload);
    }
}
