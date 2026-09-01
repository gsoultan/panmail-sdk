<?php

declare(strict_types=1);

namespace Panmail\Tests;

use PHPUnit\Framework\TestCase;
use Panmail\Exception\TransportException;
use Panmail\Transport\CurlTransport;

/**
 * Drives the default transport against a real server.
 *
 * FakeTransport stands in for this everywhere else, which leaves the code that
 * actually talks to a gateway in production untested. The two behaviours worth
 * the cost of a socket are the ones a fake cannot show: that an oversized body
 * is refused rather than buffered, and that a redirect is not followed with the
 * api key attached.
 */
final class CurlTransportTest extends TestCase
{
    /** @var resource|null */
    private static $server;
    private static string $base = '';

    public static function setUpBeforeClass(): void
    {
        $socket = stream_socket_server('tcp://127.0.0.1:0', $errno, $errstr);
        if ($socket === false) {
            self::markTestSkipped("no loopback socket available: $errstr");
        }
        $port = (int) explode(':', (string) stream_socket_get_name($socket, false))[1];
        fclose($socket);

        self::$base = "http://127.0.0.1:$port";
        $descriptors = [1 => ['file', '/dev/null', 'w'], 2 => ['file', '/dev/null', 'w']];
        $process = proc_open(
            [PHP_BINARY, '-S', "127.0.0.1:$port", __DIR__ . '/server.php'],
            $descriptors,
            $pipes
        );
        if (!is_resource($process)) {
            self::markTestSkipped('could not start the built-in server');
        }
        self::$server = $process;

        // The server needs a moment before it accepts.
        for ($i = 0; $i < 100; $i++) {
            $probe = @fsockopen('127.0.0.1', $port, $errno, $errstr, 0.1);
            if ($probe !== false) {
                fclose($probe);

                return;
            }
            usleep(50_000);
        }
        self::markTestSkipped('the built-in server never came up');
    }

    public static function tearDownAfterClass(): void
    {
        if (is_resource(self::$server)) {
            proc_terminate(self::$server);
            proc_close(self::$server);
        }
        self::$server = null;
    }

    public function testAnOrdinaryResponseComesBackWhole(): void
    {
        $response = (new CurlTransport())->send(self::$base . '/ok', [], '{}', 5);

        self::assertSame(200, $response->status);
        self::assertStringContainsString('msg_01', $response->body);
        self::assertSame('application/json', $response->header('Content-Type'));
    }

    public function testARidiculousResponseIsRefusedRatherThanBuffered(): void
    {
        $this->expectException(TransportException::class);
        $this->expectExceptionMessageMatches('/exceeded/');

        (new CurlTransport())->send(self::$base . '/huge', [], '{}', 15);
    }

    /**
     * X-API-Key is a header cURL has never heard of, so nothing would drop it
     * on a redirect to another host. Following one is a key disclosure.
     */
    public function testARedirectIsNotFollowedWithTheApiKeyAttached(): void
    {
        $run = bin2hex(random_bytes(8));
        $marker = sys_get_temp_dir() . "/panmail-followed-$run";
        @unlink($marker);

        $response = (new CurlTransport())->send(
            self::$base . "/redirect?run=$run",
            ['X-API-Key' => 'secret-key'],
            '{}',
            5
        );

        self::assertSame(307, $response->status, 'the redirect was followed');
        self::assertFileDoesNotExist($marker, 'the api key reached the redirect target');
    }
    /**
     * The one outcome the client never retries and reports as unknown. Worth a
     * real socket rather than a fake, because it is cURL's failure being
     * translated, not the client's own.
     */
    public function testAConnectionThatNeverCompletesIsATransportException(): void
    {
        $this->expectException(TransportException::class);
        $this->expectExceptionMessageMatches('/did not complete/');

        // Port 1 on loopback: nothing listens, and the refusal is immediate.
        (new CurlTransport())->send('http://127.0.0.1:1/send', [], '{}', 5);
    }

}
