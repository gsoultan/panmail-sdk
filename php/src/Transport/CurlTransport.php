<?php

declare(strict_types=1);

namespace Panmail\Transport;

use Panmail\Exception\TransportException;

/** The default transport: an ordinary POST with the bundled cURL extension. */
final class CurlTransport implements Transport
{
    /**
     * Bounds what a single response may cost in memory. A send response is a
     * message id and a status, and a page of providers is a few hundred bytes
     * for each of the fifty a page holds; anything approaching this is a proxy
     * error page, not the gateway.
     */
    private const MAX_RESPONSE_BYTES = 1 << 20;

    /**
     * One handle for the life of the transport, reset between requests, so
     * cURL can keep the connection to the gateway open. A fresh handle per
     * request paid a TCP and TLS handshake for every send and every page of
     * a listing.
     */
    private ?\CurlHandle $handle = null;

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

        if ($this->handle === null) {
            $handle = curl_init();
            if ($handle === false) {
                throw new TransportException('panmail: could not initialise curl');
            }
            $this->handle = $handle;
        } else {
            // Clears every option, including the callbacks below, which hold
            // the previous request's buffers; the connection cache survives.
            curl_reset($this->handle);
        }
        $ch = $this->handle;

        curl_setopt_array($ch, [
            CURLOPT_URL => $url,
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
                    // Keep what fits, so a refusal can still be read from
                    // its start, then stop: any count but the chunk's length
                    // aborts the transfer.
                    $room = self::MAX_RESPONSE_BYTES - strlen($payload);
                    if ($room > 0) {
                        $payload .= substr($chunk, 0, $room);
                    }
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

        if ($tooLarge && $status >= 300) {
            // A refusal is still classified when its body was cut short, as
            // the Go client does: the status and code decide what it is, and
            // a proxy's error page in front of a 429 can run long. Reporting
            // it as a transport failure would call a plain "not accepted" an
            // unknown outcome, and a rate limit would never be retried.
            return new Response($status, $responseHeaders, $payload);
        }
        if ($tooLarge) {
            throw new TransportException(
                "panmail: the gateway's response exceeded " . self::MAX_RESPONSE_BYTES . ' bytes'
            );
        }
        if ($ok === false) {
            // Deliberately not classified and never retried: for a send, a
            // transport error is the one outcome where the client does not
            // know whether the gateway took the message. "The request" rather
            // than "the send" because a listing comes through here too, and
            // this does not know which it is carrying.
            throw new TransportException("panmail: the request did not complete: $error");
        }

        return new Response($status, $responseHeaders, $payload);
    }
}
