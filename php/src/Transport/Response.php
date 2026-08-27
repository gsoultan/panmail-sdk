<?php

declare(strict_types=1);

namespace Panmail\Transport;

/** One HTTP response, reduced to the three things this client reads. */
final class Response
{
    /** @param array<string, string> $headers keyed by lowercased header name */
    public function __construct(
        public readonly int $status,
        public readonly array $headers,
        public readonly string $body,
    ) {
    }

    public function header(string $name): ?string
    {
        return $this->headers[strtolower($name)] ?? null;
    }
}
