package io.github.gsoultan.panmail;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;

import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.UncheckedIOException;
import java.net.URI;
import java.net.URISyntaxException;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Objects;
import java.util.Optional;

/**
 * Sends mail through a panmail gateway.
 *
 * <p>It talks to the same endpoint the web UI uses, authenticating with an API
 * key rather than a session. Create the key in Settings → API Keys with the
 * email:send scope; the key carries the tenant, so there is nothing else to
 * configure.
 *
 * <pre>{@code
 * PanmailClient client = PanmailClient.builder()
 *         .baseUrl("https://mail.example.com")
 *         .apiKey(System.getenv("PANMAIL_API_KEY"))
 *         .build();
 *
 * Result result = client.send(Message.builder()
 *         .providerId("0f8b...")
 *         .from("noreply@example.com")
 *         .to("someone@example.org")
 *         .subject("Your receipt")
 *         .html("<p>Thanks for your order.</p>")
 *         .build());
 * }</pre>
 *
 * <p>{@code send} returns once the gateway has written the message to its
 * outbox, not once it has been delivered: {@link Result#messageId()} is what
 * later delivery events and webhooks are keyed by.
 *
 * <h2>Retries</h2>
 *
 * <p>This client does not retry a send whose outcome it does not know. Sending
 * is not idempotent and the gateway has no de-duplication key, so a retry after
 * a timeout or a dropped connection is a retry of a message that may already be
 * on its way to the recipient. The one exception is a refusal — the gateway
 * says plainly that it did not accept the message — which is safe to repeat and
 * which {@link Builder#rateLimitRetries(int)} turns on.
 *
 * <p>Instances are immutable and safe for concurrent use.
 */
public final class PanmailClient {

    /**
     * Not Authorization on purpose. That header carries a dashboard session,
     * and a key sent as a bearer token is rejected as a malformed session
     * rather than as a bad key — a confusing way to learn you used the wrong
     * header.
     */
    private static final String API_KEY_HEADER = "X-API-Key";

    /**
     * The Connect route for EmailService.SendEmail. Connect derives it from the
     * proto package and service name, so it changes only if the proto does.
     */
    private static final String SEND_PROCEDURE = "/panmail.v1.EmailService/SendEmail";

    /** Connect error codes this client acts on. */
    private static final String RESOURCE_EXHAUSTED = "resource_exhausted";
    private static final String UNAUTHENTICATED = "unauthenticated";
    private static final String PERMISSION_DENIED = "permission_denied";

    /**
     * Bounds a single send. Generous, because the gateway writes the message to
     * its outbox before answering and that is a disk write on a possibly busy
     * database — but finite, because a send that hangs holds whatever request is
     * waiting on it.
     */
    public static final Duration DEFAULT_TIMEOUT = Duration.ofSeconds(30);

    /**
     * Bounds what a single response may cost in memory. A send response is a
     * message id and a status; anything approaching this is a proxy error page,
     * not the gateway.
     */
    private static final long MAX_RESPONSE_BYTES = 1 << 20;

    /**
     * Reads the body but abandons the response once it passes
     * {@link #MAX_RESPONSE_BYTES}, where {@code BodyHandlers.ofString()} would
     * buffer whatever arrived.
     *
     * <p>Deliberately a subscriber rather than {@code ofInputStream}: a
     * subscriber keeps {@code send} blocked until the body is complete, so the
     * request timeout still covers the read. Handing back a stream would bound
     * the headers and leave the body to run as long as it liked.
     */
    private static HttpResponse.BodyHandler<String> boundedBody() {
        return info -> {
            ByteArrayOutputStream buffer = new ByteArrayOutputStream();
            return HttpResponse.BodySubscribers.mapping(
                    HttpResponse.BodySubscribers.ofByteArrayConsumer(chunk -> chunk.ifPresent(bytes -> {
                        if (buffer.size() + bytes.length > MAX_RESPONSE_BYTES) {
                            throw new UncheckedIOException(new IOException(
                                    "panmail: the gateway's response exceeded "
                                            + MAX_RESPONSE_BYTES + " bytes"));
                        }
                        buffer.writeBytes(bytes);
                    })),
                    ignored -> buffer.toString(StandardCharsets.UTF_8));
        };
    }

    private final URI endpoint;
    private final String apiKey;
    private final Duration timeout;
    private final int rateLimitRetries;
    private final Map<String, String> headers;
    private final HttpClient httpClient;
    private final ObjectMapper mapper = new ObjectMapper();

    private PanmailClient(Builder builder) {
        this.endpoint = validateBaseUrl(builder.baseUrl);
        if (builder.apiKey == null || builder.apiKey.isEmpty()) {
            throw new InvalidMessageException("panmail: an api key is required");
        }
        this.apiKey = builder.apiKey;
        this.timeout = builder.timeout;
        this.rateLimitRetries = Math.max(0, builder.rateLimitRetries);
        validateHeaders(builder.headers);
        this.headers = Map.copyOf(builder.headers);
        if (builder.httpClient != null) {
            // X-API-Key is a header java.net.http has never heard of, so it is
            // not among the credentials it would drop on a redirect to another
            // host. A client that follows redirects would hand the tenant's api
            // key to whatever host the Location named, which is worth refusing
            // loudly rather than inheriting quietly.
            if (builder.httpClient.followRedirects() != HttpClient.Redirect.NEVER) {
                throw new InvalidMessageException(
                        "panmail: the supplied HttpClient follows redirects, which would forward "
                                + "the api key to whatever host a Location header named; build it "
                                + "with followRedirects(Redirect.NEVER)");
            }
            this.httpClient = builder.httpClient;
        } else {
            this.httpClient = HttpClient.newBuilder()
                    .connectTimeout(builder.timeout)
                    .followRedirects(HttpClient.Redirect.NEVER)
                    .build();
        }
    }

    public static Builder builder() {
        return new Builder();
    }

    /**
     * Queues a message and returns once the gateway has it on disk.
     *
     * <p>Returning without throwing means the gateway accepted responsibility
     * for delivering the message, not that it has been delivered — that is
     * reported afterwards through delivery events and webhooks, keyed by
     * {@link Result#messageId()}.
     *
     * @throws InvalidMessageException a message refused before it was sent
     * @throws RateLimitedException    over the tenant's send rate; safe to repeat
     * @throws BacklogFullException    the queue is too deep; repeating makes it worse
     * @throws AuthException           the key was missing, rejected, or lacks email:send
     * @throws ApiException            any other refusal
     * @throws TransportException      the send did not complete
     */
    public Result send(Message message) {
        String body;
        try {
            body = mapper.writeValueAsString(message.toWire());
        } catch (IOException e) {
            throw new PanmailException("panmail: encoding the message failed", e);
        }

        for (int attempt = 0; ; attempt++) {
            try {
                return post(body);
            } catch (RateLimitedException refusal) {
                if (attempt >= rateLimitRetries || refusal.retryAfter().isZero()) {
                    throw refusal;
                }
                try {
                    Thread.sleep(refusal.retryAfter().toMillis());
                } catch (InterruptedException interrupted) {
                    Thread.currentThread().interrupt();
                    throw refusal;
                }
            }
        }
    }

    private Result post(String body) {
        HttpRequest.Builder request = HttpRequest.newBuilder()
                .uri(endpoint)
                .timeout(timeout)
                .POST(HttpRequest.BodyPublishers.ofString(body));

        headers.forEach(request::header);
        request.header("Content-Type", "application/json");
        request.header(API_KEY_HEADER, apiKey);

        HttpResponse<String> response;
        try {
            response = httpClient.send(request.build(), boundedBody());
        } catch (IOException e) {
            // Deliberately not classified and never retried: a transport error
            // is the one outcome where the client does not know whether the
            // gateway took the message.
            throw new TransportException("panmail: the send did not complete: " + e.getMessage(), e);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new TransportException("panmail: the send was interrupted", e);
        }

        if (response.statusCode() != 200) {
            throw classify(response);
        }

        try {
            JsonNode decoded = mapper.readTree(response.body());
            return new Result(
                    decoded.path("messageId").asText(""),
                    decoded.path("status").asText(""));
        } catch (IOException e) {
            throw new TransportException("panmail: the gateway's response was not json", e);
        }
    }

    /**
     * Turns a refusal into something a caller can act on.
     *
     * <p>The gateway answers both of its capacity refusals with
     * resource_exhausted, deliberately: they are the same answer to the client —
     * you are asking for more than you may have. What separates them is
     * Retry-After, which the rate limiter sets and the backlog check does not,
     * because only one of the two has a delay worth quoting. That is the
     * discrimination here, and it is why removing Retry-After from the rate
     * refusal would silently reclassify every rate limit as a full queue.
     */
    private ApiException classify(HttpResponse<String> response) {
        int status = response.statusCode();
        String payload = response.body() == null ? "" : response.body();

        String code = "";
        String message = "";
        try {
            JsonNode envelope = mapper.readTree(payload);
            code = envelope.path("code").asText("");
            message = envelope.path("message").asText("");
        } catch (IOException ignored) {
            // A body that is not the Connect envelope is a proxy's error page.
            // The status alone is still worth classifying.
        }
        if (message.isEmpty()) {
            message = payload.strip();
        }

        if (code.isEmpty()) {
            code = switch (status) {
                case 429 -> RESOURCE_EXHAUSTED;
                case 401 -> UNAUTHENTICATED;
                case 403 -> PERMISSION_DENIED;
                default -> "";
            };
        }

        if (RESOURCE_EXHAUSTED.equals(code)) {
            Optional<String> retryAfter = response.headers().firstValue("Retry-After");
            if (retryAfter.isEmpty()) {
                return new BacklogFullException(
                        "panmail: the tenant's queue is too deep to accept more: " + message,
                        code,
                        status);
            }
            // A header this client cannot read is not a reason to call the
            // refusal something else: it is still a rate limit, just one with
            // no usable delay attached.
            long seconds;
            try {
                seconds = Math.max(0, Long.parseLong(retryAfter.get().strip()));
            } catch (NumberFormatException e) {
                seconds = 0;
            }
            return new RateLimitedException(
                    "panmail: send rate exceeded, retry after " + seconds + "s: " + message,
                    Duration.ofSeconds(seconds),
                    code,
                    status);
        }

        if (UNAUTHENTICATED.equals(code) || PERMISSION_DENIED.equals(code)) {
            return new AuthException(
                    "panmail: the api key was not accepted: " + message, code, status);
        }

        return new ApiException("panmail: " + code + ": " + message, code, status);
    }

    /**
     * Refuses a header that would not survive being written to the wire as
     * written.
     *
     * {@code HttpRequest.Builder} would refuse it too, but at send time and
     * with an {@link IllegalArgumentException} — outside the hierarchy every
     * other refusal from this client belongs to, so a caller catching
     * {@link PanmailException} would not catch it. A header that cannot be sent
     * is knowable when the client is built.
     */
    private static void validateHeaders(Map<String, String> headers) {
        for (Map.Entry<String, String> header : headers.entrySet()) {
            String name = header.getKey();
            // RFC 9110's token, which is what a header name is.
            if (name == null || !name.matches("[!#$%&'*+.^_`|~0-9A-Za-z-]+")) {
                throw new InvalidMessageException(
                        "panmail: \"" + name + "\" is not a header name");
            }
            String value = header.getValue() == null ? "" : header.getValue();
            for (int i = 0; i < value.length(); i++) {
                char c = value.charAt(i);
                // Tab is allowed; nothing else below space is, and CR and LF
                // are the two that end the line.
                if ((c < 0x20 && c != '\t') || c == 0x7f) {
                    throw new InvalidMessageException(
                            "panmail: the value of header \"" + name + "\" contains a control "
                                    + "character at index " + i + "; a carriage return or newline "
                                    + "there would add a header of its own");
                }
            }
        }
    }

    private static URI validateBaseUrl(String baseUrl) {
        if (baseUrl == null || baseUrl.isEmpty()) {
            throw new InvalidMessageException("panmail: a base url is required");
        }

        URI parsed;
        try {
            parsed = new URI(baseUrl);
        } catch (URISyntaxException e) {
            throw new InvalidMessageException("panmail: base url is not a url: " + baseUrl);
        }
        // A missing scheme is the usual mistake — "mail.example.com" parses
        // happily as a relative path, and the failure it causes surfaces much
        // later as an unreadable transport error.
        String scheme = parsed.getScheme();
        if (!"http".equals(scheme) && !"https".equals(scheme)) {
            throw new InvalidMessageException(
                    "panmail: base url needs an http or https scheme, got \"" + baseUrl + "\"");
        }
        if (parsed.getHost() == null || parsed.getHost().isEmpty()) {
            throw new InvalidMessageException(
                    "panmail: base url has no host: \"" + baseUrl + "\"");
        }
        // Userinfo is deprecated in RFC 3986 for the reason it matters here:
        // the url travels into every error this client reports, and an error
        // is a thing that gets logged. The api key is the credential; if
        // something in front of the gateway wants basic auth too, a header is
        // where it goes.
        if (parsed.getUserInfo() != null) {
            throw new InvalidMessageException(
                    "panmail: base url must not carry credentials; the api key authenticates "
                            + "the send, and a password in the url ends up in logs");
        }
        return URI.create(baseUrl.replaceAll("/+$", "") + SEND_PROCEDURE);
    }

    /** Builds a {@link PanmailClient}. */
    public static final class Builder {

        private String baseUrl;
        private String apiKey;
        private Duration timeout = DEFAULT_TIMEOUT;
        private int rateLimitRetries;
        private final Map<String, String> headers = new LinkedHashMap<>();
        private HttpClient httpClient;

        /**
         * The gateway's origin — "https://mail.example.com" — not a path to a
         * procedure.
         */
        public Builder baseUrl(String baseUrl) {
            this.baseUrl = baseUrl;
            return this;
        }

        public Builder apiKey(String apiKey) {
            this.apiKey = apiKey;
            return this;
        }

        /** Bounds each send. Defaults to {@link #DEFAULT_TIMEOUT}. */
        public Builder timeout(Duration timeout) {
            Objects.requireNonNull(timeout, "panmail: timeout must not be null");
            if (timeout.isZero() || timeout.isNegative()) {
                throw new InvalidMessageException("panmail: timeout must be positive");
            }
            this.timeout = timeout;
            return this;
        }

        /**
         * Waits out up to n rate-limit refusals, sleeping for the delay the
         * gateway asks for each time.
         *
         * <p>Off by default, and only ever applied to a refusal. A refusal is
         * the one failure where the client knows the message was not accepted,
         * so repeating it cannot deliver anything twice — but it does mean
         * {@code send} blocks for as long as the gateway asks, which inside a
         * request handler is a decision the caller should make rather than
         * inherit. A queue of your own is usually the better answer.
         */
        public Builder rateLimitRetries(int n) {
            this.rateLimitRetries = n;
            return this;
        }

        /**
         * Sets an extra HTTP header on every request — a tracing header, or
         * whatever a proxy in front of the gateway requires.
         *
         * <p>The API key header is not settable this way: it is what
         * {@link #apiKey(String)} is for, and a second source for it would only
         * make a mismatch possible.
         */
        public Builder header(String name, String value) {
            if (!API_KEY_HEADER.equalsIgnoreCase(name)) {
                this.headers.put(name, value);
            }
            return this;
        }

        /** Supplies the HTTP client to send with — for a proxy, or a shared pool. */
        public Builder httpClient(HttpClient httpClient) {
            this.httpClient = httpClient;
            return this;
        }

        public PanmailClient build() {
            return new PanmailClient(this);
        }
    }
}
