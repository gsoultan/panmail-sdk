package io.github.gsoultan.panmail;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.MethodSource;

import java.io.IOException;
import java.io.InputStream;
import java.net.InetSocketAddress;
import java.nio.file.Files;
import java.nio.file.Path;
import java.net.http.HttpClient;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.ArrayList;
import java.util.Base64;
import java.util.List;
import java.util.Map;
import java.util.Spliterators;
import java.util.stream.Stream;
import java.util.stream.StreamSupport;
import org.junit.jupiter.params.provider.Arguments;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.function.BiConsumer;

import static org.junit.jupiter.api.Assertions.assertDoesNotThrow;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

/** Runs the client against a stand-in gateway on a real socket. */
class PanmailClientTest {

    private static final ObjectMapper MAPPER = new ObjectMapper();

    private HttpServer server;
    private String baseUrl;
    private final List<JsonNode> bodies = new ArrayList<>();
    private final List<Map<String, List<String>>> requestHeaders = new ArrayList<>();
    private final List<String> paths = new ArrayList<>();
    private final AtomicInteger calls = new AtomicInteger();

    /** Starts a gateway whose handler is given the exchange and the call count, one-based. */
    private void gateway(BiConsumer<HttpExchange, Integer> handler) throws IOException {
        server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        server.createContext("/", exchange -> {
            try (InputStream body = exchange.getRequestBody()) {
                bodies.add(MAPPER.readTree(body.readAllBytes()));
            }
            requestHeaders.add(Map.copyOf(exchange.getRequestHeaders()));
            paths.add(exchange.getRequestURI().getPath());
            handler.accept(exchange, calls.incrementAndGet());
        });
        server.start();
        baseUrl = "http://127.0.0.1:" + server.getAddress().getPort();
    }

    private void accepts() throws IOException {
        gateway((exchange, call) -> respond(exchange, 200,
                "{\"messageId\":\"msg_01\",\"status\":\"" + Status.PENDING + "\"}"));
    }

    private static void respond(HttpExchange exchange, int status, String body) {
        try {
            byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
            exchange.getResponseHeaders().add("Content-Type", "application/json");
            exchange.sendResponseHeaders(status, bytes.length);
            exchange.getResponseBody().write(bytes);
            exchange.close();
        } catch (IOException e) {
            throw new IllegalStateException(e);
        }
    }

    private static void refuse(HttpExchange exchange, int status, String code, String message) {
        respond(exchange, status, "{\"code\":\"" + code + "\",\"message\":\"" + message + "\"}");
    }

    @AfterEach
    void stop() {
        if (server != null) {
            server.stop(0);
            // Nulled so a test that never starts a gateway — the builder
            // validation ones — cannot stop the previous test's server twice.
            server = null;
        }
    }

    @BeforeEach
    void reset() {
        bodies.clear();
        requestHeaders.clear();
        paths.clear();
        calls.set(0);
    }

    private PanmailClient client() {
        return client(0);
    }

    private PanmailClient client(int rateLimitRetries) {
        return PanmailClient.builder()
                .baseUrl(baseUrl)
                .apiKey("test-key")
                .rateLimitRetries(rateLimitRetries)
                .timeout(Duration.ofSeconds(5))
                .build();
    }

    private static Message hello() {
        return Message.builder()
                .providerId("3f1c2b7a-0000-4000-8000-000000000001")
                .from("app@example.com")
                .to("user@example.net")
                .subject("Hello")
                .text("hi there")
                .build();
    }

    @Test
    void sendReturnsTheQueuedMessage() throws IOException {
        accepts();

        Result result = client().send(hello());

        assertEquals("msg_01", result.messageId());
        assertEquals(Status.PENDING, result.status());
    }

    // The key goes in X-API-Key. Authorization carries a dashboard session, and
    // a key sent there is rejected as a malformed session, not as a bad key.
    @Test
    void sendAuthenticatesWithTheApiKeyHeader() throws IOException {
        accepts();

        client().send(hello());

        Map<String, List<String>> headers = requestHeaders.get(0);
        assertEquals(List.of("test-key"), headers.get("X-api-key"));
        assertNull(headers.get("Authorization"));
    }

    @Test
    void sendPostsToTheConnectProcedure() throws IOException {
        accepts();

        client().send(hello());

        assertEquals("/panmail.v1.EmailService/SendEmail", paths.get(0));
    }

    @Test
    void sendCarriesTheWholeMessage() throws IOException {
        accepts();

        client().send(Message.builder()
                .providerId("prov")
                .from("app@example.com")
                .to("user@example.net")
                .cc("cc@example.net")
                .bcc("bcc@example.net")
                .subject("Hello")
                .html("<p>hi</p>")
                .text("hi")
                .templateId("welcome")
                .templateData("name", "Ada")
                .build());

        JsonNode body = bodies.get(0);
        assertEquals("prov", body.path("providerId").asText());
        assertEquals("app@example.com", body.path("from").asText());
        assertEquals("user@example.net", body.path("to").get(0).asText());
        assertEquals("cc@example.net", body.path("cc").get(0).asText());
        assertEquals("bcc@example.net", body.path("bcc").get(0).asText());
        assertEquals("Hello", body.path("subject").asText());
        assertEquals("<p>hi</p>", body.path("bodyHtml").asText());
        assertEquals("hi", body.path("bodyText").asText());
        assertEquals("welcome", body.path("templateId").asText());
        assertEquals("Ada", body.path("templateData").path("name").asText());
    }

    // A bytes field is base64 in protobuf JSON, and the gateway decodes it as
    // such. Sending raw bytes instead would arrive as mojibake.
    @Test
    void sendBase64EncodesAttachmentContent() throws IOException {
        accepts();

        client().send(Message.builder()
                .providerId("prov")
                .from("app@example.com")
                .to("user@example.net")
                .text("hi")
                .attach(new Attachment("receipt.pdf", "%PDF-1.4"))
                .build());

        JsonNode attachment = bodies.get(0).path("attachments").get(0);
        assertEquals("receipt.pdf", attachment.path("filename").asText());
        assertEquals("application/pdf", attachment.path("contentType").asText());
        assertEquals(
                "%PDF-1.4",
                new String(
                        Base64.getDecoder().decode(attachment.path("content").asText()),
                        StandardCharsets.UTF_8));
    }

    // A UTF-8 CSV guessed as latin-1 is mojibake in a spreadsheet. All four
    // clients send the same header for the same file.
    @Test
    void textAttachmentsDeclareTheirCharset() throws IOException {
        accepts();

        client().send(Message.builder()
                .providerId("prov")
                .from("app@example.com")
                .to("user@example.net")
                .text("hi")
                .attach(new Attachment("report.csv", "a,b\n1,2"))
                .build());

        JsonNode attachment = bodies.get(0).path("attachments").get(0);
        assertEquals("text/csv; charset=utf-8", attachment.path("contentType").asText());
    }

    @Test
    void sendRejectsBadMessagesWithoutCallingTheGateway() throws IOException {
        accepts();
        PanmailClient client = client();

        List<Message> bad = List.of(
                Message.builder().from("a@example.com").to("b@example.net").text("x").build(),
                Message.builder().providerId("p").to("b@example.net").text("x").build(),
                Message.builder().providerId("p").from("a@example.com").text("x").build(),
                Message.builder().providerId("p").from("a@example.com").to("b@example.net").build(),
                Message.builder()
                        .providerId("p")
                        .from("a@example.com")
                        .to("b@example.net")
                        .text("x")
                        .attach(new Attachment("", "x"))
                        .build());

        for (Message message : bad) {
            assertThrows(InvalidMessageException.class, () -> client.send(message));
        }
        assertEquals(0, calls.get());
    }

    // Both capacity refusals are resource_exhausted. Retry-After is the only
    // thing separating them, so this is the test that fails if the gateway
    // stops sending it.
    @Test
    void aRateLimitCarriesItsDelay() throws IOException {
        gateway((exchange, call) -> {
            exchange.getResponseHeaders().add("Retry-After", "7");
            refuse(exchange, 429, "resource_exhausted", "over the send rate");
        });

        RateLimitedException refusal =
                assertThrows(RateLimitedException.class, () -> client().send(hello()));

        assertEquals(Duration.ofSeconds(7), refusal.retryAfter());
    }

    @Test
    void theSameCodeWithoutADelayIsAFullQueue() throws IOException {
        gateway((exchange, call) -> refuse(exchange, 429, "resource_exhausted", "queue too deep"));

        assertThrows(BacklogFullException.class, () -> client().send(hello()));
    }

    @Test
    void anUnreadableDelayIsStillARateLimit() throws IOException {
        gateway((exchange, call) -> {
            exchange.getResponseHeaders().add("Retry-After", "Wed, 21 Oct 2026 07:28:00 GMT");
            refuse(exchange, 429, "resource_exhausted", "over the send rate");
        });

        RateLimitedException refusal =
                assertThrows(RateLimitedException.class, () -> client().send(hello()));

        assertTrue(refusal.retryAfter().isZero());
    }

    @Test
    void unauthenticatedIsAnAuthError() throws IOException {
        gateway((exchange, call) -> refuse(exchange, 401, "unauthenticated", "no"));

        assertThrows(AuthException.class, () -> client().send(hello()));
    }

    @Test
    void permissionDeniedIsAnAuthError() throws IOException {
        gateway((exchange, call) -> refuse(exchange, 403, "permission_denied", "no"));

        assertThrows(AuthException.class, () -> client().send(hello()));
    }

    @Test
    void anythingElseKeepsItsCode() throws IOException {
        gateway((exchange, call) ->
                refuse(exchange, 400, "invalid_argument", "provider_id is not a uuid"));

        ApiException refusal = assertThrows(ApiException.class, () -> client().send(hello()));

        assertEquals("invalid_argument", refusal.code());
        assertEquals(400, refusal.status());
        assertTrue(refusal.getMessage().contains("uuid"));
    }

    // A proxy in front of the gateway answers with its own error page. The
    // status is still worth classifying.
    @Test
    void aBodyThatIsNotAnEnvelopeFallsBackToTheStatus() throws IOException {
        gateway((exchange, call) -> respond(exchange, 401, "<html>401 Unauthorized</html>"));

        assertInstanceOf(AuthException.class,
                assertThrows(ApiException.class, () -> client().send(hello())));
    }

    @Test
    void sendDoesNotRetryByDefault() throws IOException {
        gateway((exchange, call) -> {
            exchange.getResponseHeaders().add("Retry-After", "1");
            refuse(exchange, 429, "resource_exhausted", "over the send rate");
        });

        assertThrows(RateLimitedException.class, () -> client().send(hello()));
        assertEquals(1, calls.get());
    }

    @Test
    void sendWaitsOutARateLimitWhenAsked() throws IOException {
        gateway((exchange, call) -> {
            if (call == 1) {
                exchange.getResponseHeaders().add("Retry-After", "1");
                refuse(exchange, 429, "resource_exhausted", "over the send rate");
                return;
            }
            respond(exchange, 200,
                    "{\"messageId\":\"msg_after_wait\",\"status\":\"" + Status.PENDING + "\"}");
        });

        Result result = client(1).send(hello());

        assertEquals("msg_after_wait", result.messageId());
        assertEquals(2, calls.get());
    }

    @Test
    void sendGivesUpAfterTheConfiguredRetries() throws IOException {
        gateway((exchange, call) -> {
            exchange.getResponseHeaders().add("Retry-After", "1");
            refuse(exchange, 429, "resource_exhausted", "over the send rate");
        });

        assertThrows(RateLimitedException.class, () -> client(2).send(hello()));
        // The first attempt is not a retry: two retries is three calls.
        assertEquals(3, calls.get());
    }

    // A full queue is not something waiting on a timer fixes, and retrying
    // makes the wait longer for everything already queued.
    @Test
    void sendDoesNotRetryAFullQueue() throws IOException {
        gateway((exchange, call) -> refuse(exchange, 429, "resource_exhausted", "queue too deep"));

        assertThrows(BacklogFullException.class, () -> client(3).send(hello()));
        assertEquals(1, calls.get());
    }

    @Test
    void builderValidatesItsInputs() {
        assertThrows(InvalidMessageException.class,
                () -> PanmailClient.builder().baseUrl("").apiKey("k").build());
        assertThrows(InvalidMessageException.class,
                () -> PanmailClient.builder().baseUrl("mail.example.com").apiKey("k").build());
        assertThrows(InvalidMessageException.class,
                () -> PanmailClient.builder().baseUrl("ftp://mail.example.com").apiKey("k").build());
        assertThrows(InvalidMessageException.class,
                () -> PanmailClient.builder().baseUrl("https://mail.example.com").apiKey("").build());
    }

    @Test
    void aBaseUrlWithATrailingSlashDoesNotDoubleTheSeparator() throws IOException {
        accepts();

        PanmailClient.builder()
                .baseUrl(baseUrl + "/")
                .apiKey("test-key")
                .build()
                .send(hello());

        assertEquals("/panmail.v1.EmailService/SendEmail", paths.get(0));
    }

    @Test
    void aCallerSuppliedHeaderCannotOverrideTheApiKey() throws IOException {
        accepts();

        PanmailClient.builder()
                .baseUrl(baseUrl)
                .apiKey("test-key")
                .header("x-api-key", "smuggled")
                .header("X-Trace", "abc")
                .build()
                .send(hello());

        Map<String, List<String>> headers = requestHeaders.get(0);
        assertEquals(List.of("test-key"), headers.get("X-api-key"));
        assertEquals(List.of("abc"), headers.get("X-trace"));
    }
    /**
     * A proxy in front of the gateway can answer with a page of its own, and
     * nothing about a send makes a megabyte of it worth buffering.
     */
    @Test
    void aRidiculousResponseIsRefusedRatherThanBuffered() throws IOException {
        gateway((exchange, call) -> {
            try {
                byte[] chunk = new byte[64 * 1024];
                java.util.Arrays.fill(chunk, (byte) 'x');
                exchange.getResponseHeaders().add("Content-Type", "application/json");
                exchange.sendResponseHeaders(200, 0);
                for (int written = 0; written < 4 * 1024 * 1024; written += chunk.length) {
                    exchange.getResponseBody().write(chunk);
                }
                exchange.close();
            } catch (IOException expected) {
                // The client hangs up once it has seen enough.
            }
        });

        TransportException thrown =
                assertThrows(TransportException.class, () -> client().send(hello()));
        assertTrue(thrown.getMessage().contains("exceeded"), thrown.getMessage());
    }

    /**
     * X-API-Key is a header java.net.http has never heard of, so it is not
     * among the credentials it drops on a redirect to another host. Following
     * one is a key disclosure, so the default client must not.
     */
    @Test
    void aRedirectIsNotFollowedWithTheApiKeyAttached() throws IOException {
        List<String> leaked = new ArrayList<>();
        gateway((exchange, call) -> {
            if (exchange.getRequestURI().getPath().endsWith("/followed")) {
                leaked.add(String.valueOf(exchange.getRequestHeaders().getFirst("X-API-Key")));
                respond(exchange, 200, "{\"messageId\":\"leaked\"}");
                return;
            }
            exchange.getResponseHeaders().add("Location", baseUrl + "/followed");
            respond(exchange, 307, "");
        });

        assertThrows(ApiException.class, () -> client().send(hello()));
        assertTrue(leaked.isEmpty(), "the api key reached the redirect target: " + leaked);
    }

    /** A caller's client that follows redirects would leak the key just the same. */
    @Test
    void aSuppliedClientThatFollowsRedirectsIsRefused() throws IOException {
        accepts();

        InvalidMessageException thrown = assertThrows(InvalidMessageException.class,
                () -> PanmailClient.builder()
                        .baseUrl(baseUrl)
                        .apiKey("test-key")
                        .httpClient(HttpClient.newBuilder()
                                .followRedirects(HttpClient.Redirect.ALWAYS)
                                .build())
                        .build());
        assertTrue(thrown.getMessage().contains("followRedirects"), thrown.getMessage());
    }

    @Test
    void aTimeoutMustBePositive() {
        assertThrows(InvalidMessageException.class,
                () -> PanmailClient.builder().timeout(Duration.ZERO));
        assertThrows(InvalidMessageException.class,
                () -> PanmailClient.builder().timeout(Duration.ofSeconds(-1)));
    }

    /**
     * Every recipient field takes a collection as well as varargs. to() did
     * from the start and cc and bcc caught up later, which is the kind of
     * asymmetry that stays invisible until someone reaches for the overload
     * that was never there.
     */
    @Test
    void everyRecipientFieldTakesACollection() throws IOException {
        accepts();

        client().send(Message.builder()
                .providerId("prov")
                .from("app@example.com")
                .to(List.of("a@example.net", "b@example.net"))
                .cc(List.of("c@example.net"))
                .bcc(List.of("d@example.net", "e@example.net"))
                .text("hi")
                .build());

        JsonNode body = bodies.get(0);
        assertEquals(2, body.path("to").size());
        assertEquals("a@example.net", body.path("to").get(0).asText());
        assertEquals("b@example.net", body.path("to").get(1).asText());
        assertEquals(1, body.path("cc").size());
        assertEquals("c@example.net", body.path("cc").get(0).asText());
        assertEquals(2, body.path("bcc").size());
        assertEquals("d@example.net", body.path("bcc").get(0).asText());
        assertEquals("e@example.net", body.path("bcc").get(1).asText());
    }

    /** The two spellings add to the same list rather than replacing it. */
    @Test
    void varargsAndCollectionRecipientsAccumulate() throws IOException {
        accepts();

        client().send(Message.builder()
                .providerId("prov")
                .from("app@example.com")
                .to("first@example.net")
                .to(List.of("second@example.net"))
                .cc("cc-first@example.net")
                .cc(List.of("cc-second@example.net"))
                .text("hi")
                .build());

        JsonNode body = bodies.get(0);
        assertEquals(2, body.path("to").size());
        assertEquals("first@example.net", body.path("to").get(0).asText());
        assertEquals("second@example.net", body.path("to").get(1).asText());
        assertEquals(2, body.path("cc").size());
        assertEquals("cc-first@example.net", body.path("cc").get(0).asText());
        assertEquals("cc-second@example.net", body.path("cc").get(1).asText());
    }

    /**
     * The mapping all four clients share, read from one file rather than
     * repeated here. A change to one implementation that the others do not
     * follow turns three languages red instead of sending the gateway four
     * different Content-Type headers for the same attachment.
     */
    static Stream<Arguments> sharedContentTypes() throws IOException {
        JsonNode fixture = MAPPER.readTree(
                Files.readString(Path.of("..", "testdata", "content-types.json")));
        JsonNode types = fixture.path("types");
        assertTrue(types.size() > 0, "the shared fixture is empty");

        return StreamSupport.stream(
                        Spliterators.spliteratorUnknownSize(types.fieldNames(), 0), false)
                .map(extension -> Arguments.of(extension, types.path(extension).asText()));
    }

    @ParameterizedTest(name = ".{0} is sent as {1}")
    @MethodSource("sharedContentTypes")
    void attachmentContentTypesMatchTheSharedFixture(String extension, String expected)
            throws IOException {
        accepts();

        client().send(Message.builder()
                .providerId("prov")
                .from("app@example.com")
                .to("user@example.net")
                .text("hi")
                .attach(new Attachment("attachment." + extension, "x"))
                .build());

        assertEquals(expected, bodies.get(0).path("attachments").get(0).path("contentType").asText());
    }

    static Stream<Arguments> headersThatWouldSplitTheRequest() {
        return Stream.of(
                Arguments.of("crlf in value", "X-Trace", "abc\r\nX-Injected: yes"),
                Arguments.of("bare lf", "X-Trace", "abc\nX-Injected: yes"),
                Arguments.of("nul in value", "X-Trace", "abc\u0000def"),
                Arguments.of("crlf in name", "X-Bad\r\nX-Injected", "yes"),
                Arguments.of("space in name", "X Trace", "yes"),
                Arguments.of("empty name", "", "yes"));
    }

    /**
     * A carriage return or newline ends a header line, so a value carrying one
     * is a second header the caller never wrote. HttpRequest.Builder refuses to
     * send it, but with an IllegalArgumentException at send time — outside the
     * hierarchy every other refusal belongs to, so a caller catching
     * PanmailException would not catch it.
     */
    @ParameterizedTest(name = "{0}")
    @MethodSource("headersThatWouldSplitTheRequest")
    void aHeaderThatWouldSplitTheRequestIsRefused(String label, String name, String value) {
        assertThrows(InvalidMessageException.class, () -> PanmailClient.builder()
                .baseUrl("https://mail.example.com")
                .apiKey("k")
                .header(name, value)
                .build());
    }

    @Test
    void aTabIsLegalInAHeaderValueAndStaysLegal() {
        assertDoesNotThrow(() -> PanmailClient.builder()
                .baseUrl("https://mail.example.com")
                .apiKey("k")
                .header("X-Trace", "a\tb")
                .build());
    }

    /**
     * A url carrying a password travels into every error this client reports,
     * and an error is a thing that gets logged.
     */
    @Test
    void credentialsInTheBaseUrlAreRefused() {
        for (String baseUrl : List.of(
                "https://user:hunter2@mail.example.com", "https://user@mail.example.com")) {
            InvalidMessageException thrown = assertThrows(InvalidMessageException.class,
                    () -> PanmailClient.builder().baseUrl(baseUrl).apiKey("k").build(),
                    baseUrl + " was accepted");
            assertFalse(thrown.getMessage().contains("hunter2"), thrown.getMessage());
        }
    }

    /**
     * The api key is a tenant-wide sending credential, and http puts it on the
     * wire in the clear. Loopback is exempt because a gateway on localhost is
     * how this is developed against, and there is no network to listen on.
     */
    @Test
    void httpIsRefusedAwayFromLoopback() {
        for (String baseUrl : List.of("http://mail.example.com", "http://169.254.169.254/")) {
            assertThrows(InvalidMessageException.class,
                    () -> PanmailClient.builder().baseUrl(baseUrl).apiKey("k").build(),
                    baseUrl + " was accepted");
        }
        for (String baseUrl : List.of(
                "https://mail.example.com", "http://localhost:8080", "http://LOCALHOST:8080",
                "http://127.0.0.1:9000", "http://127.5.5.5:9000", "http://[::1]:9000")) {
            assertDoesNotThrow(
                    () -> PanmailClient.builder().baseUrl(baseUrl).apiKey("k").build(),
                    baseUrl + " was refused");
        }
    }

}
