<?php

declare(strict_types=1);

namespace Panmail\Tests;

use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;
use Panmail\Client;
use Panmail\Exception\ApiException;
use Panmail\Exception\AuthException;
use Panmail\Exception\InvalidMessageException;
use Panmail\Exception\TransportException;
use Panmail\Provider;
use Panmail\ProviderType;
use Panmail\Transport\Response;
use Panmail\Transport\Transport;

final class ProvidersTest extends TestCase
{
    private const BASE = 'https://mail.example.com';

    /**
     * One ListEmailProviders response: the providers, and the token for the
     * next page, if there is one.
     *
     * @param array<string, mixed> ...$providers
     */
    private static function page(string $next, array ...$providers): Response
    {
        $body = ['providers' => array_values($providers)];
        if ($next !== '') {
            $body['nextPageToken'] = $next;
        }

        return new Response(200, ['content-type' => 'application/json'], json_encode($body, JSON_THROW_ON_ERROR));
    }

    /**
     * One entry in a page, holding only the fields a sender needs.
     *
     * @return array<string, string>
     */
    private static function provider(string $id, string $type): array
    {
        return ['id' => $id, 'name' => "provider $id", 'type' => $type];
    }

    /** @param array<string, mixed> $options */
    private static function client(Transport $transport, array $options = []): Client
    {
        return new Client(self::BASE, 'test-key', ['transport' => $transport] + $options);
    }

    /**
     * @param list<Provider> $providers
     *
     * @return list<string>
     */
    private static function ids(array $providers): array
    {
        return array_map(static fn (Provider $provider): string => $provider->id, $providers);
    }

    public function testListProviders(): void
    {
        $transport = new FakeTransport(self::page(
            '',
            [
                'id' => '3f1c2b7a-0000-4000-8000-000000000001',
                'name' => 'Production SES',
                'type' => 'PROVIDER_TYPE_SES',
                'allowedDomains' => ['example.com', 'example.org'],
            ],
            [
                'id' => '3f1c2b7a-0000-4000-8000-000000000002',
                'name' => 'Fallback SMTP',
                'type' => 'PROVIDER_TYPE_SMTP',
            ],
        ));

        $providers = self::client($transport)->listProviders();

        self::assertEquals(
            [
                new Provider(
                    id: '3f1c2b7a-0000-4000-8000-000000000001',
                    name: 'Production SES',
                    type: ProviderType::SES,
                    allowedDomains: ['example.com', 'example.org'],
                ),
                new Provider(
                    id: '3f1c2b7a-0000-4000-8000-000000000002',
                    name: 'Fallback SMTP',
                    type: ProviderType::SMTP,
                    allowedDomains: [],
                ),
            ],
            $providers
        );
    }

    // The same procedure the dashboard's Email Providers page reads from, with
    // the key where every call puts it.
    public function testListProvidersPostsToTheConnectProcedure(): void
    {
        $transport = new FakeTransport(self::page(''));

        self::client($transport)->listProviders();

        self::assertSame(
            self::BASE . '/panmail.v1.EmailProviderService/ListEmailProviders',
            $transport->calls[0]['url']
        );
        $headers = $transport->calls[0]['headers'];
        self::assertSame('test-key', $headers['X-API-Key']);
        self::assertArrayNotHasKey('Authorization', $headers);
        self::assertSame('application/json', $headers['Content-Type']);
    }

    // An empty filter field is left out, not sent as "": the gateway decodes an
    // enum name it does not know as no filter, and an empty string is one.
    public function testListProvidersAsksForAPageAndNoFilterByDefault(): void
    {
        $transport = new FakeTransport(self::page(''));

        self::client($transport)->listProviders();

        self::assertSame(['pageSize' => 50], $transport->calls[0]['body']);
    }

    public function testListProvidersSendsTheFilter(): void
    {
        $transport = new FakeTransport(self::page(''));

        self::client($transport)->listProviders(name: 'prod', type: ProviderType::SMTP);

        self::assertSame(
            ['pageSize' => 50, 'name' => 'prod', 'type' => 'PROVIDER_TYPE_SMTP'],
            $transport->calls[0]['body']
        );
    }

    // UNSPECIFIED is the gateway's own "no filter". Sending it would be
    // harmless, but checking the answer against it would keep nothing.
    public function testListProvidersWithUnspecifiedTypeListsEveryKind(): void
    {
        $transport = new FakeTransport(self::page(
            '',
            self::provider('a', ProviderType::SES),
            self::provider('b', ProviderType::IMAP),
        ));

        $providers = self::client($transport)->listProviders(type: ProviderType::UNSPECIFIED);

        self::assertArrayNotHasKey('type', $transport->calls[0]['body']);
        self::assertSame(['a', 'b'], self::ids($providers));
    }

    public function testListProvidersFollowsEveryPage(): void
    {
        $transport = new FakeTransport(
            self::page('NTA=', self::provider('a', ProviderType::SES), self::provider('b', ProviderType::SES)),
            self::page('MTAw', self::provider('c', ProviderType::SES)),
            self::page('', self::provider('d', ProviderType::SES)),
        );

        $providers = self::client($transport)->listProviders(name: 'prod');

        self::assertSame(['a', 'b', 'c', 'd'], self::ids($providers), 'in the order the pages gave them');
        self::assertCount(3, $transport->calls);

        // The token goes back exactly as it came, and the filter rides along on
        // every page rather than only the first.
        foreach ([null, 'NTA=', 'MTAw'] as $i => $token) {
            self::assertSame($token, $transport->calls[$i]['body']['pageToken'] ?? null, "page $i");
            self::assertSame('prod', $transport->calls[$i]['body']['name'], "page $i");
        }
    }

    // The gateway pages by offset, newest first. A provider created between two
    // requests moves every row down one, and the last of one page arrives again
    // as the first of the next.
    public function testListProvidersReturnsAProviderRepeatedAcrossPagesOnce(): void
    {
        $transport = new FakeTransport(
            self::page('Mg==', self::provider('a', ProviderType::SES), self::provider('b', ProviderType::SES)),
            self::page('', self::provider('b', ProviderType::SES), self::provider('c', ProviderType::SES)),
        );

        self::assertSame(['a', 'b', 'c'], self::ids(self::client($transport)->listProviders()));
    }

    // The gateway hands out a token for any full page, so a list that is an
    // exact multiple of the page size ends with an empty one.
    public function testListProvidersTreatsAnEmptyLastPageAsTheEnd(): void
    {
        $transport = new FakeTransport(self::page('MQ==', self::provider('a', ProviderType::SES)), self::page(''));

        self::assertSame(['a'], self::ids(self::client($transport)->listProviders()));
        self::assertCount(2, $transport->calls);
    }

    // A token that comes round again is a loop, and following it would read
    // the same pages until the page limit — then report that instead of the
    // loop.
    public function testListProvidersRefusesATokenItHasAlreadyFollowed(): void
    {
        $transport = new FakeTransport(
            self::page('A', self::provider('a', ProviderType::SES)),
            self::page('B', self::provider('b', ProviderType::SES)),
            self::page('A', self::provider('a', ProviderType::SES)),
        );

        try {
            self::client($transport)->listProviders();
            self::fail('a repeated token was followed');
        } catch (TransportException $refusal) {
            self::assertStringContainsString('a second time', $refusal->getMessage());
        }
        self::assertCount(3, $transport->calls);
    }

    // The gateway writes the token and a log reads the message it ends up in,
    // so a newline in one must not start a line of its own there. Go gets the
    // same from %q.
    public function testARepeatedTokenIsQuotedSafelyInTheMessage(): void
    {
        $transport = new FakeTransport(self::page("A\nforged: yes", self::provider('a', ProviderType::SES)));

        try {
            self::client($transport)->listProviders();
            self::fail('a repeated token was followed');
        } catch (TransportException $refusal) {
            self::assertStringNotContainsString("\n", $refusal->getMessage());
            self::assertStringContainsString('A\nforged: yes', $refusal->getMessage());
        }
    }

    // A gateway that never stops handing out fresh tokens is stopped by the
    // page limit, and the answer is an exception rather than the pages read so
    // far: a list cut short that looks complete is how a caller concludes a
    // provider does not exist.
    public function testListProvidersStopsAtThePageLimit(): void
    {
        $transport = new class () implements Transport {
            public int $calls = 0;

            public function send(string $url, array $headers, string $body, int $timeout): Response
            {
                $this->calls++;

                return new Response(200, [], json_encode([
                    'providers' => [['id' => "p{$this->calls}", 'type' => ProviderType::SES]],
                    'nextPageToken' => "t{$this->calls}",
                ], JSON_THROW_ON_ERROR));
            }
        };

        try {
            self::client($transport)->listProviders();
            self::fail('the listing never stopped paging');
        } catch (TransportException $refusal) {
            self::assertStringContainsString('still paging', $refusal->getMessage());
        }
        self::assertSame(200, $transport->calls);
    }

    /** @return iterable<string, array{string, list<string>}> */
    public static function typeFilters(): iterable
    {
        yield 'a type it knows' => [ProviderType::SMTP, ['smtp']];
        yield 'a misspelt type' => ['SMTP', []];
        yield 'a type none match' => [ProviderType::MAILGUN, []];
    }

    /**
     * The gateway decodes an enum name it does not recognise as no filter at
     * all, so a misspelt type comes back as every provider. Checking the answer
     * is what makes a filter mean what it says.
     *
     * @param list<string> $expected
     */
    #[DataProvider('typeFilters')]
    public function testListProvidersKeepsOnlyTheTypeItAskedFor(string $type, array $expected): void
    {
        $ignoresTheFilter = new FakeTransport(self::page(
            '',
            self::provider('ses', ProviderType::SES),
            self::provider('smtp', ProviderType::SMTP),
            self::provider('imap', ProviderType::IMAP),
        ));

        self::assertSame($expected, self::ids(self::client($ignoresTheFilter)->listProviders(type: $type)));
    }

    /**
     * The gateway returns every provider's configuration — credentials cleared —
     * along with its tenant id, timestamps and send ceilings. None of it is
     * needed to send, and none of it is kept, so a lapse in the gateway's
     * clearing has nowhere here to land. The password below is what such a
     * lapse would look like.
     */
    public function testListProvidersDecodesOnlyWhatASenderNeeds(): void
    {
        $transport = new FakeTransport(self::page('', [
            'id' => 'p1',
            'name' => 'Relay',
            'type' => 'PROVIDER_TYPE_SMTP',
            'allowedDomains' => ['example.com'],
            'tenantId' => 'tenant-7c1e',
            'sendRatePerMinute' => 600,
            'sendBurst' => 100,
            'createTime' => '2026-09-01T10:00:00Z',
            'updateTime' => '2026-09-01T10:00:00Z',
            'webhookSecret' => 'whsec-lapse',
            'smtp' => [
                'host' => 'smtp.internal.example',
                'port' => 587,
                'username' => 'relay-user',
                'password' => 'hunter2',
                'dkim' => ['domain' => 'example.com', 'selector' => 's1', 'privateKey' => '-----BEGIN'],
            ],
        ]));

        $providers = self::client($transport)->listProviders();

        self::assertEquals(
            [new Provider(id: 'p1', name: 'Relay', type: ProviderType::SMTP, allowedDomains: ['example.com'])],
            $providers
        );

        $logged = print_r($providers, true) . var_export($providers, true);
        foreach (['hunter2', '-----BEGIN', 'whsec-lapse', 'relay-user', 'smtp.internal.example', 'tenant-7c1e'] as $leak) {
            self::assertStringNotContainsString($leak, $logged, "a logged provider carries $leak");
        }
    }

    /**
     * protojson writes an enum value its descriptor has no name for as a bare
     * number, and provider_type.proto reserves 2 to 5 because stored rows may
     * still carry them. One such row must not fail the list.
     */
    public function testListProvidersKeepsATypeTheGatewayHasNoNameFor(): void
    {
        $transport = new FakeTransport(self::page(
            '',
            ['id' => 'old', 'name' => 'Old', 'type' => 3],
            self::provider('new', ProviderType::SES),
        ));

        $providers = self::client($transport)->listProviders();

        self::assertCount(2, $providers);
        self::assertSame('3', $providers[0]->type, 'its decimal spelling');
    }

    // protobuf JSON leaves a zero value out, so a provider with no type in the
    // body is one whose type is UNSPECIFIED — not one with a type of "".
    public function testListProvidersReadsAMissingTypeAsUnspecified(): void
    {
        $transport = new FakeTransport(self::page('', ['id' => 'p1', 'name' => 'Nameless kind']));

        self::assertSame(ProviderType::UNSPECIFIED, self::client($transport)->listProviders()[0]->type);
    }

    /**
     * Go refuses a type that is neither a name nor a number, because its
     * decoder does; here it goes through self::text() like every other field,
     * which reads a structured value as empty rather than raising "Array to
     * string conversion" mid-cast. Empty is UNSPECIFIED.
     */
    public function testAStructuredTypeDoesNotRaiseAWarning(): void
    {
        $transport = new FakeTransport(self::page('', ['id' => 'p1', 'type' => ['smtp' => true]]));

        self::assertSame(ProviderType::UNSPECIFIED, self::client($transport)->listProviders()[0]->type);
    }

    // An entry that is not an object has nothing a Provider could be built
    // from, and is passed over rather than failing the list.
    public function testAnEntryThatIsNotAnObjectIsPassedOver(): void
    {
        $transport = new FakeTransport(new Response(
            200,
            [],
            json_encode(['providers' => ['not an object', self::provider('a', ProviderType::SES)]], JSON_THROW_ON_ERROR)
        ));

        self::assertSame(['a'], self::ids(self::client($transport)->listProviders()));
    }

    public function testAJsonScalarPageIsATransportError(): void
    {
        $transport = new FakeTransport(new Response(200, [], '"providers"'));

        $this->expectException(TransportException::class);
        $this->expectExceptionMessage("panmail: the gateway's response was not a page of providers");

        self::client($transport)->listProviders();
    }

    // A key minted for sending holds email:send alone. The gateway names the
    // scope it is missing, and that is the message worth passing on.
    public function testListProvidersNeedsTheProvidersReadScope(): void
    {
        $transport = new FakeTransport(new Response(
            403,
            [],
            json_encode([
                'code' => 'permission_denied',
                'message' => 'api key is missing the "providers:read" scope',
            ], JSON_THROW_ON_ERROR)
        ));

        try {
            self::client($transport)->listProviders();
            self::fail('the listing was allowed');
        } catch (AuthException $refusal) {
            self::assertStringContainsString('providers:read', $refusal->getMessage());
        }
    }

    /** @return iterable<string, array{Response, string}> */
    public static function sendRefusals(): iterable
    {
        $envelope = static fn (string $code): string => json_encode(
            ['code' => $code, 'message' => 'no'],
            JSON_THROW_ON_ERROR
        );

        yield 'failed_precondition' => [new Response(400, [], $envelope('failed_precondition')), 'failed_precondition'];
        yield 'resource_exhausted without a delay' => [
            new Response(429, [], $envelope('resource_exhausted')),
            'resource_exhausted',
        ];
        yield 'resource_exhausted with a delay' => [
            new Response(429, ['retry-after' => '1'], $envelope('resource_exhausted')),
            'resource_exhausted',
        ];
        // No code in the body, so the status stands in for one — the same
        // fallback a send uses, which here names the code and nothing more.
        yield 'a 429 from a proxy, carrying no code' => [
            new Response(429, [], '<html>slow down</html>'),
            'resource_exhausted',
        ];
        yield 'invalid_argument, which it always was' => [
            new Response(400, [], $envelope('invalid_argument')),
            'invalid_argument',
        ];
    }

    /**
     * A full queue, a suppressed recipient and a rate limit are answers to a
     * send. Read into the same code from a listing they would be lies, and the
     * rate limit would be waited out for a call the gateway does not limit at
     * all.
     */
    #[DataProvider('sendRefusals')]
    public function testListProvidersDoesNotReadSendRefusalsIntoAList(Response $refusal, string $code): void
    {
        $transport = new FakeTransport($refusal);

        try {
            self::client($transport, ['rateLimitRetries' => 3])->listProviders();
            self::fail('the listing was accepted');
        } catch (ApiException $e) {
            // Exactly this class: a caller branching on the send's refusals
            // must not catch one of these by accident.
            self::assertSame(ApiException::class, $e::class);
            self::assertSame($code, $e->connectCode);
            self::assertSame($refusal->status, $e->status);
        }
        self::assertCount(1, $transport->calls, 'nothing on a listing is waited out');
    }

    /**
     * Invalid UTF-8 in the name cannot be encoded. Left alone that escapes as a
     * bare JsonException, which no catch written from listProviders()'s
     * docblock would stop. Go has no counterpart: its encoder replaces the bad
     * bytes rather than failing.
     */
    public function testANameThatCannotBeEncodedIsRefusedBeforeTheGatewayIsCalled(): void
    {
        $transport = new FakeTransport(self::page(''));

        try {
            self::client($transport)->listProviders(name: "prod\xB1");
            self::fail('the name was sent');
        } catch (InvalidMessageException $refusal) {
            self::assertStringContainsString('could not be encoded as json', $refusal->getMessage());
        }
        self::assertSame([], $transport->calls, 'the gateway was called anyway');
    }

    /**
     * The gateway's enum is the source of truth and the fixture is generated
     * from it by scripts/sync-status.py, as for Status.
     */
    public function testProviderTypeConstantsMatchTheSharedFixture(): void
    {
        $raw = file_get_contents(__DIR__ . '/../../testdata/provider-types.json');
        self::assertIsString($raw);

        /** @var array{constants: array<string, string>} $fixture */
        $fixture = json_decode($raw, true, 512, JSON_THROW_ON_ERROR);

        $exported = (new \ReflectionClass(ProviderType::class))->getConstants();

        $expected = $fixture['constants'];
        ksort($expected);
        ksort($exported);

        self::assertSame($expected, $exported);
    }
}
