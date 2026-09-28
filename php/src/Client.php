<?php

declare(strict_types=1);

namespace Panmail;

use Panmail\Exception\ApiException;
use Panmail\Exception\AuthException;
use Panmail\Exception\BacklogFullException;
use Panmail\Exception\InvalidMessageException;
use Panmail\Exception\RateLimitedException;
use Panmail\Exception\SuppressedRecipientException;
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
 * listProviders() returns the ids providerId takes, for a key that also holds
 * the providers:read scope.
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
     * The Connect route for EmailProviderService.ListEmailProviders, the
     * procedure the dashboard's Email Providers page reads from.
     */
    private const LIST_PROVIDERS_PROCEDURE = '/panmail.v1.EmailProviderService/ListEmailProviders';

    /**
     * How many providers each request asks for. listProviders() returns the
     * whole list either way, so this decides only how many round trips that
     * takes and how much of the transport's 1 MiB response bound each provider
     * may use: about 20 KB apiece, which one exceeds only with an
     * allowedDomains list running to around a thousand names.
     */
    private const PROVIDER_PAGE_SIZE = 50;

    /**
     * Bounds how long listProviders() follows the gateway's page tokens. It is
     * there for a gateway, or something in front of one, that never stops
     * handing them out: without it the loop is unbounded, and so is the memory
     * holding what it has read. At PROVIDER_PAGE_SIZE a page that is 10,000
     * providers, which is a platform rather than a tenant.
     */
    private const MAX_PROVIDER_PAGES = 200;

    /**
     * Bounds a single request: a send, or one page of a provider listing.
     * Generous, because the gateway writes a message to its outbox before
     * answering and that is a disk write on a possibly busy database — but
     * finite, because a send that hangs holds whatever request is waiting on
     * it.
     */
    public const DEFAULT_TIMEOUT = 30;

    /** The gateway's origin with any trailing slash removed; each call appends its own procedure. */
    private readonly string $baseUrl;
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
     * } $options rateLimitRetries waits out up to n rate-limit refusals of a
     *            send, sleeping for the delay the gateway asks for each time.
     *            Off by default, and only ever applied to a refusal.
     */
    public function __construct(string $baseUrl, string $apiKey, array $options = [])
    {
        $this->validateBaseUrl($baseUrl);
        if ($apiKey === '') {
            throw new InvalidMessageException('panmail: an api key is required');
        }

        $this->baseUrl = rtrim($baseUrl, '/');
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
        self::validateHeaders($headers);
        $headers[self::API_KEY_HEADER] = $apiKey;
        $headers['Content-Type'] = 'application/json';
        $this->headers = $headers;
    }

    /**
     * Queues a message and returns once the gateway has it on disk.
     *
     * Returning without throwing means the gateway accepted responsibility for
     * the message, not that it has been delivered — that is reported
     * afterwards through delivery events and webhooks, keyed by
     * $result->messageId.
     *
     * It does not mean the message will be delivered. A filter rule can
     * quarantine one for review, and the gateway answers a held message with
     * the same message id and the same Status::PENDING as an accepted one, byte
     * for byte. Nothing in the response tells them apart. Subscribe to
     * TriggerEvent::MAIL_HELD if that matters, and to
     * TriggerEvent::MAIL_EXPIRED, which is a held message reaching its
     * retention deadline unreviewed.
     *
     * @throws InvalidMessageException a message refused before it was sent
     * @throws RateLimitedException    over the tenant's send rate; safe to repeat
     * @throws BacklogFullException    the queue is too deep; repeating makes it worse
     * @throws AuthException           the key was missing, rejected, or lacks email:send
     * @throws ApiException            any other refusal
     * @throws TransportException      the request did not complete
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
                $decoded = $this->post(self::SEND_PROCEDURE, $body, $this->classifySend(...), 'a send result');

                return new Result(
                    messageId: self::text($decoded['messageId'] ?? ''),
                    status: self::text($decoded['status'] ?? ''),
                );
            } catch (RateLimitedException $refusal) {
                if ($attempt >= $this->rateLimitRetries || $refusal->retryAfter <= 0) {
                    throw $refusal;
                }
                sleep($refusal->retryAfter);
            }
        }
    }

    /**
     * Every provider in the key's tenant, newest first: the ids a Message's
     * providerId takes, and the From domains each will send as.
     *
     * The key needs the providers:read scope, which no key has by default. One
     * minted for sending holds email:send alone and is refused with an
     * AuthException naming the scope. Granting it to a sending key lets that
     * key read every provider's configuration from the gateway, credentials
     * excepted; if provider ids are only needed while an application is set
     * up, a second key for that job keeps the sending key as narrow as it was.
     *
     *     $ses = $client->listProviders(type: ProviderType::SES);
     *
     * It is one call because providers are configuration rather than a feed:
     * the gateway is asked for pages and they are joined here. It pages by
     * offset, so a provider created meanwhile can arrive twice — it is returned
     * once — and one deleted meanwhile can push another across a page boundary
     * and out of the list. Both need the list to span pages and to change while
     * it is read.
     *
     * Nothing is retried, and rateLimitRetries does not apply: the gateway
     * limits how fast a tenant sends, not how often it reads. Listing changes
     * nothing, so any exception is safe to repeat by calling again.
     *
     * The timeout option bounds each page rather than the listing, so one that
     * spans pages can take a multiple of it; the page limit is what bounds the
     * whole.
     *
     * @param string $name keeps providers whose name contains it, ignoring
     *                     case: "prod" matches "Production" and "eu-prod". A
     *                     search, not a lookup — to find one provider by name,
     *                     compare the names that come back. The gateway matches
     *                     it with SQL LIKE and does not escape it, so % and _
     *                     in it are wildcards.
     * @param string $type keeps providers of one kind, a ProviderType constant.
     *                     Empty, or ProviderType::UNSPECIFIED, is every kind.
     *
     * @return list<Provider>
     *
     * @throws InvalidMessageException the name could not be encoded as json
     * @throws AuthException           the key was missing, rejected, or lacks providers:read
     * @throws ApiException            any other refusal
     * @throws TransportException      the request did not complete, or the gateway would not stop paging
     */
    public function listProviders(string $name = '', string $type = ''): array
    {
        if ($type === ProviderType::UNSPECIFIED) {
            $type = '';
        }

        // An empty filter is left out rather than sent as "": the gateway
        // decodes an enum name it does not know as no filter, and "" is one.
        $request = ['pageSize' => self::PROVIDER_PAGE_SIZE];
        if ($name !== '') {
            $request['name'] = $name;
        }
        if ($type !== '') {
            $request['type'] = $type;
        }

        $providers = [];
        $seen = [];
        $followed = [];

        for ($page = 0; $page < self::MAX_PROVIDER_PAGES; $page++) {
            try {
                $body = json_encode($request, JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES);
            } catch (JsonException $e) {
                // Invalid UTF-8 in the name. Left alone this escapes as a bare
                // JsonException, which no catch written from the list above
                // would stop.
                throw new InvalidMessageException(
                    'panmail: the name filter could not be encoded as json: ' . $e->getMessage(),
                    0,
                    $e
                );
            }

            $decoded = $this->post(
                self::LIST_PROVIDERS_PROCEDURE,
                $body,
                $this->classify(...),
                'a page of providers'
            );

            $listed = $decoded['providers'] ?? [];
            foreach (is_array($listed) ? $listed : [] as $item) {
                if (!is_array($item)) {
                    continue;
                }
                $provider = self::provider($item);

                if (isset($seen[$provider->id])) {
                    continue;
                }
                $seen[$provider->id] = true;

                // Checked again here because the gateway ignores a type name
                // it does not recognise rather than refusing it: a misspelt
                // filter comes back as every provider, and has to be told
                // apart from one that matched them all.
                if ($type !== '' && $provider->type !== $type) {
                    continue;
                }

                $providers[] = $provider;
            }

            $token = self::text($decoded['nextPageToken'] ?? '');
            if ($token === '') {
                return $providers;
            }
            if (isset($followed[$token])) {
                // Escaped because the gateway wrote it and a log will read it:
                // a newline in a token would otherwise start a line of its own.
                $quoted = addcslashes($token, "\0..\37\"\\\177");
                throw new TransportException(
                    "panmail: the gateway handed out page token \"$quoted\" a second time; "
                    . 'refusing to read the same pages again'
                );
            }
            $followed[$token] = true;
            $request['pageToken'] = $token;
        }

        throw new TransportException(
            'panmail: the gateway was still paging after ' . self::MAX_PROVIDER_PAGES . ' pages of '
            . self::PROVIDER_PAGE_SIZE . ' providers; narrow the list with a name or type filter'
        );
    }

    /**
     * One round trip to a procedure, returning the decoded body of a 200.
     *
     * @param callable(Response): ApiException $classify what this procedure's refusals mean
     * @param string                           $expected what the body should have been, for the
     *                                                   error when it is json but not an object
     *
     * @return array<mixed>
     */
    private function post(string $procedure, string $body, callable $classify, string $expected): array
    {
        $response = $this->transport->send($this->baseUrl . $procedure, $this->headers, $body, $this->timeout);

        if ($response->status !== 200) {
            throw $classify($response);
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
                "panmail: the gateway's response was not $expected: " . trim($response->body)
            );
        }

        return $decoded;
    }

    /**
     * The part of a listed provider a sender needs. The gateway also sends its
     * configuration, timestamps, send ceilings and tenant id; none of that is
     * read, so none of it is kept.
     *
     * @param array<mixed> $item
     */
    private static function provider(array $item): Provider
    {
        $domains = [];
        $allowed = $item['allowedDomains'] ?? [];
        foreach (is_array($allowed) ? $allowed : [] as $domain) {
            $domains[] = self::text($domain);
        }

        // protobuf JSON leaves a zero value out, so no type is UNSPECIFIED. A
        // value the enum has no name for arrives as a bare number instead —
        // provider_type.proto reserves 2 to 5 because stored rows may still
        // carry them — and text() keeps it as its decimal spelling rather than
        // failing the list over one old row.
        $type = self::text($item['type'] ?? '');

        return new Provider(
            id: self::text($item['id'] ?? ''),
            name: self::text($item['name'] ?? ''),
            type: $type === '' ? ProviderType::UNSPECIFIED : $type,
            allowedDomains: $domains,
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
     * Turns a refusal of a send into something a caller can act on.
     *
     * The gateway answers both of its capacity refusals with resource_exhausted,
     * deliberately: they are the same answer to the client — you are asking for
     * more than you may have. What separates them is Retry-After, which the rate
     * limiter sets and the backlog check does not, because only one of the two
     * has a delay worth quoting. That is the discrimination here, and it is why
     * removing Retry-After from the rate refusal would silently reclassify every
     * rate limit as a full queue.
     */
    private function classifySend(Response $response): ApiException
    {
        [$code, $message] = self::refusalOf($response);
        $status = $response->status;

        return match ($code) {
            'resource_exhausted' => $this->capacityRefusal($message, $code, $status, $response),
            // The gateway's answer to a suppressed recipient, and only to that
            // on the send path. Before it had a code for this, a suppressed
            // recipient arrived as "unknown" with a 500 and was
            // indistinguishable from the gateway having broken.
            'failed_precondition' => new SuppressedRecipientException(
                "panmail: a recipient is suppressed, so the whole message was refused: $message",
                $code,
                $status
            ),
            default => self::classifyCode($code, $message, $status),
        };
    }

    /**
     * Turns a refusal of any other call into something a caller can act on.
     */
    private function classify(Response $response): ApiException
    {
        [$code, $message] = self::refusalOf($response);

        return self::classifyCode($code, $message, $response->status);
    }

    /**
     * The part of classifySend() that holds for every call: a key the gateway
     * would not take. Everything else keeps its code as an ApiException. The
     * other refusals with classes of their own are answers to a send — a full
     * queue, a suppressed recipient — and would be lies read into the same code
     * from a listing.
     */
    private static function classifyCode(string $code, string $message, int $status): ApiException
    {
        return match ($code) {
            'unauthenticated', 'permission_denied' => new AuthException(
                "panmail: the api key was not accepted: $message",
                $code,
                $status
            ),
            default => new ApiException("panmail: $code: $message", $code, $status),
        };
    }

    /**
     * The Connect code and message a refusal carries.
     *
     * @return array{string, string}
     */
    private static function refusalOf(Response $response): array
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
        // what a proxy returning its own error page looks like. Deliberately no
        // entry for 400: the gateway sends both failed_precondition and
        // invalid_argument as 400, so a status with no code cannot tell them
        // apart and guessing would misclassify one of them.
        if ($code === '') {
            $code = match ($status) {
                429 => 'resource_exhausted',
                401 => 'unauthenticated',
                403 => 'permission_denied',
                default => '',
            };
        }

        return [$code, $message];
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

    /**
     * Refuses a header that would not survive being written to the wire as
     * written.
     *
     * CurlTransport builds a header line by joining the name and value with a
     * colon, so a carriage return or newline in either ends the line early and
     * everything after it becomes a header of its own. A tracing header built
     * out of something a user supplied is exactly where that turns into a
     * request the caller did not write.
     *
     * @param array<string, string> $headers
     */
    private static function validateHeaders(array $headers): void
    {
        foreach ($headers as $name => $value) {
            $name = (string) $name;
            if ($name === '' || preg_match('/^[!#$%&\'*+.^_`|~0-9A-Za-z-]+$/', $name) !== 1) {
                throw new InvalidMessageException(
                    "panmail: \"$name\" is not a header name"
                );
            }
            // Tab is allowed; nothing else below space is, and CR and LF are
            // the two that end the line.
            if (preg_match('/[\x00-\x08\x0a-\x1f\x7f]/', $value) === 1) {
                throw new InvalidMessageException(
                    "panmail: the value of header \"$name\" contains a control character; "
                    . 'a carriage return or newline there would add a header of its own'
                );
            }
        }
    }

    /**
     * Is this host this machine — the one place plaintext http carries the api
     * key no further than the process next to it?
     */
    private static function isLoopback(string $host): bool
    {
        // parse_url keeps the brackets on an IPv6 literal.
        $host = strtolower(trim($host, '[]'));

        if ($host === 'localhost' || $host === '::1') {
            return true;
        }

        // The whole of 127.0.0.0/8, not just 127.0.0.1.
        return filter_var($host, FILTER_VALIDATE_IP, FILTER_FLAG_IPV4) !== false
            && str_starts_with($host, '127.');
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
        // The api key is a tenant-wide sending credential and http puts it on
        // the wire in the clear. Loopback is exempt because a gateway on
        // localhost is how the thing is developed against, and there is no
        // network to listen on.
        if ($scheme === 'http' && !self::isLoopback((string) $parsed['host'])) {
            throw new InvalidMessageException(
                "panmail: base url \"$baseUrl\" uses http, which would send the api key in "
                . 'cleartext; use https, or http only against a loopback host'
            );
        }
        // Userinfo is deprecated in RFC 3986 for the reason it matters here:
        // the url travels into every error this client reports, and an error
        // is a thing that gets logged. The api key is the credential; if
        // something in front of the gateway wants basic auth too, the headers
        // option is where it goes.
        if (isset($parsed['user']) || isset($parsed['pass'])) {
            throw new InvalidMessageException(
                'panmail: base url must not carry credentials; the api key authenticates '
                . 'the send, and a password in the url ends up in logs'
            );
        }
    }
}
