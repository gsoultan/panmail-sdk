<?php

declare(strict_types=1);

namespace Panmail\Tests;

use Panmail\Transport\Response;
use Panmail\Transport\Transport;

/** A transport that records what it was sent and answers from a script. */
final class FakeTransport implements Transport
{
    /** @var list<array{url: string, headers: array<string, string>, body: array<string, mixed>}> */
    public array $calls = [];

    /** @var list<Response> */
    private array $queue;

    public function __construct(Response ...$responses)
    {
        $this->queue = $responses;
    }

    public function send(string $url, array $headers, string $body, int $timeout): Response
    {
        $this->calls[] = [
            'url' => $url,
            'headers' => $headers,
            'body' => json_decode($body, true) ?? [],
        ];

        // The last scripted response repeats, so a test that retries need not
        // spell out every attempt.
        return count($this->queue) > 1 ? array_shift($this->queue) : $this->queue[0];
    }
}
