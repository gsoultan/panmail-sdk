<?php

declare(strict_types=1);

namespace Panmail;

use Panmail\Exception\ApiException;
use Panmail\Exception\AuthException;
use Panmail\Exception\BacklogFullException;
use Panmail\Exception\InvalidMessageException;
use Panmail\Exception\RateLimitedException;
use Panmail\Exception\TransportException;
use Panmail\Transport\CurlTransport;
use Panmail\Transport\Response;
use Panmail\Transport\Transport;

use JsonException;

/**
 * Sends mail through a panmail gateway.
 *
 * It talks to the same endpoint the web UI uses, authenticating with an API
 * key rather than a session. Create the key in Settings → API Keys with the
 * email:send scope; the key carries the tenant, so there is nothing else to
 * configure.
 *
 *     $client = new Client('https://mail.example.com', getenv('PANMAIL_API_KEY'));
 *     $result = $client->send(new Message(
 *         providerId: '0f8b...',
 *         from: 'noreply@example.com',
 *         to: ['someone@example.org'],
 *         subject: 'Your receipt',
 *         html: '<p>Thanks for your order.</p>',
 *     ));
 *
 * send() returns once the gateway has written the message to its outbox, not
 * once it has been delivered: $result->messageId is what later delivery events
 * and webhooks are keyed by.
 *
 * # Retries
 *
 * This client does not retry a send whose outcome it does not know. Sending is
 * not idempotent and the gateway has no de-duplication key, so a retry after a
 * timeout or a dropped connection is a retry of a message that may already be
 * on its way to the recipient. The one exception is a refusal — the gateway
 * says plainly that it did not accept the message — which is safe to repeat
 * and which the rateLimitRetries option turns on.
 */
final class Client
{
    /**
     * Not Authorization on purpose. That header carries a dashboard session,
     * and a key sent as a bearer token is rejected as a malformed session
     * rather than as a bad key — a confusing way to learn you used the wrong
     * header.
     */
    private const API_KEY_HEADER = 'X-API-Key';

    /**
     * The Connect route for EmailService.SendEmail. Connect derives it from
     * the proto package and service name, so it changes only if the proto does.
     */
    private const SEND_PROCEDURE = '/panmail.v1.EmailService/SendEmail';

    /**
     * Bounds a single send. Generous, because the gateway writes the message
     * to its outbox before answering and that is a disk write on a possibly
     * busy database — but finite, because a send that hangs holds whatever
     * request is waiting on it.
     */
    public const DEFAULT_TIMEOUT = 30;

    private readonly string $endpoint;
    private readonly int $timeout;
    private readonly int $rateLimitRetries;

    /** @var array<string, string> */
    private readonly array $headers;

    private readonly Transport $transport;

    /**
     * @param string $baseUrl the gateway's origin — "https://mail.example.com" —
     *                        not a path to a procedure
     * @param array{
     *     timeout?: int,
     *     rateLimitRetries?: int,
     *     headers?: array<string, string>,
     *     transport?: Transport
     * } $options rateLimitRetries waits out up to n rate-limit refusals,
     *            sleeping for the delay the gateway asks for each time. Off by
     *            default, and only ever applied to a refusal.
     */
    public function __construct(string $baseUrl, string $apiKey, array $options = [])
    {
        $this->validateBaseUrl($baseUrl);
        if ($apiKey === '') {
            throw new InvalidMessageException('panmail: an api key is required');
        }

        $this->endpoint = rtrim($baseUrl, '/') . self::SEND_PROCEDURE;
        $this->timeout = max(1, $options['timeout'] ?? self::DEFAULT_TIMEOUT);
        $this->rateLimitRetries = max(0, $options['rateLimitRetries'] ?? 0);
        $this->transport = $options['transport'] ?? new CurlTransport();

        $headers = $options['headers'] ?? [];
        // The API key header is not settable this way: it is what the $apiKey
        // argument is for, and a second source for it would only make a
        // mismatch possible.
        foreach (array_keys($headers) as $name) {
            if (strcasecmp((string) $name, self::API_KEY_HEADER) === 0) {
                unset($headers[$name]);
            }
        }
        $headers[self::API_KEY_HEADER] = $apiKey;
        $headers['Content-Type'] = 'application/json';
        $this->headers = $headers;
    }

    /**
     * Queues a message and returns once the gateway has it on disk.
     *
     * Returning without throwing means the gateway accepted responsibility for
     * delivering the message, not that it has been delivered — that is
     * reported afterwards through delivery events and webhooks, keyed by
     * $result->messageId.
     *
     * @throws InvalidMessageException a message refused before it was sent
     * @throws RateLimitedException    over the tenant's send rate; safe to repeat
     * @throws BacklogFullException    the queue is too deep; repeating makes it worse
     * @throws AuthException           the key was missing, rejected, or lacks email:send
     * @throws ApiException            any other refusal
     * @throws TransportException      the send did not complete
     */
    public function send(Message $message): Result
    {
        try {
            $body = json_encode($message->toArray(), JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES);
        } catch (JsonException $e) {
            // Left alone this escapes as a bare JsonException, which is not a
            // PanmailException and so slips past every catch a caller wrote
            // from the list above.
            throw new InvalidMessageException(
                'panmail: the message could not be encoded as json: ' . $e->getMessage(),
                0,
                $e
            );
        }

        for ($attempt = 0; ; $attempt++) {
            try {
                return $this->post($body);
            } catch (RateLimitedException $refusal) {
                if ($attempt >= $this->rateLimitRetries || $refusal->retryAfter <= 0) {
                    throw $refusal;
                }
                sleep($refusal->retryAfter);
            }
        }
    }

    private function post(string $body): Result
    {
        $response = $this->transport->send($this->endpoint, $this->headers, $body, $this->timeout);

        if ($response->status !== 200) {
            throw $this->classify($response);
        }

        try {
            /** @var mixed $decoded */
            $decoded = json_decode($response->body, true, 512, JSON_THROW_ON_ERROR);
        } catch (JsonException $e) {
            throw new TransportException(
                "panmail: the gateway's response was not json: " . trim($response->body),
                0,
                $e
            );
        }
        if (!is_array($decoded)) {
            throw new TransportException(
                "panmail: the gateway's response was not a send result: " . trim($response->body)
            );
        }

        return new Result(
            messageId: self::text($decoded['messageId'] ?? ''),
            status: self::text($decoded['status'] ?? ''),
        );
    }

    /**
     * Casts what the gateway sent, without letting a structured value where a
     * scalar belongs raise a warning mid-cast.
     */
    private static function text(mixed $value): string
    {
        return is_scalar($value) ? (string) $value : '';
    }

    /**
     * Turns a refusal into something a caller can act on.
     *
     * The gateway answers both of its capacity refusals with resource_exhausted,
     * deliberately: they are the same answer to the client — you are asking for
     * more than you may have. What separates them is Retry-After, which the rate
     * limiter sets and the backlog check does not, because only one of the two
     * has a delay worth quoting. That is the discrimination here, and it is why
     * removing Retry-After from the rate refusal would silently reclassify every
     * rate limit as a full queue.
     *
     */
    private function classify(Response $response): ApiException
    {
        $payload = $response->body;
        $status = $response->status;
        $code = '';
        $message = '';

        $decoded = json_decode($payload, true);
        if (is_array($decoded)) {
            // Through self::text for the same reason the send result is: a
            // structured value where a scalar belongs raises "Array to string
            // conversion" mid-cast and leaves the caller holding the word
            // "Array" instead of a reason.
            $code = self::text($decoded['code'] ?? '');
            $message = self::text($decoded['message'] ?? '');
        }
        if ($message === '') {
            $message = trim($payload);
        }

        // Fall back to the HTTP status when the body carried no code, which is
        // what a proxy returning its own error page looks like.
        if ($code === '') {
            $code = match ($status) {
                429 => 'resource_exhausted',
                401 => 'unauthenticated',
                403 => 'permission_denied',
                default => '',
            };
        }

        return match ($code) {
            'resource_exhausted' => $this->capacityRefusal($message, $code, $status, $response),
            'unauthenticated', 'permission_denied' => new AuthException(
                "panmail: the api key was not accepted: $message",
                $code,
                $status
            ),
            default => new ApiException("panmail: $code: $message", $code, $status),
        };
    }

    private function capacityRefusal(
        string $message,
        string $code,
        int $status,
        Response $response,
    ): ApiException {
        $raw = $response->header('Retry-After');
        if ($raw === null) {
            return new BacklogFullException(
                "panmail: the tenant's queue is too deep to accept more: $message",
                $code,
                $status
            );
        }

        // A header this client cannot read is not a reason to call the refusal
        // something else: it is still a rate limit, just one with no usable
        // delay attached.
        $seconds = ctype_digit(trim($raw)) ? (int) trim($raw) : 0;

        return new RateLimitedException(
            "panmail: send rate exceeded, retry after {$seconds}s: $message",
            $seconds,
            $code,
            $status
        );
    }

    private function validateBaseUrl(string $baseUrl): void
    {
        if ($baseUrl === '') {
            throw new InvalidMessageException('panmail: a base url is required');
        }

        $parsed = parse_url($baseUrl);
        if ($parsed === false) {
            throw new InvalidMessageException("panmail: base url is not a url: $baseUrl");
        }
        // A missing scheme is the usual mistake — "mail.example.com" parses
        // happily as a path, and the failure it causes surfaces much later as
        // an unreadable transport error.
        $scheme = $parsed['scheme'] ?? '';
        if ($scheme !== 'http' && $scheme !== 'https') {
            throw new InvalidMessageException(
                "panmail: base url needs an http or https scheme, got \"$baseUrl\""
            );
        }
        if (($parsed['host'] ?? '') === '') {
            throw new InvalidMessageException("panmail: base url has no host: \"$baseUrl\"");
        }
    }
}
