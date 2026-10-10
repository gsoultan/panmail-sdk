<?php

declare(strict_types=1);

namespace Panmail;

/**
 * Reads an RFC 3339 time, as protobuf JSON and Go's encoding/json write one:
 * up to nine fractional digits, Z or an offset.
 *
 * Checked against that shape first, because DateTimeImmutable reads
 * "yesterday" too, and a time that is a guess is worse than an error.
 *
 * @internal
 */
final class Rfc3339
{
    private const SHAPE = '/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?(Z|[+-]\d{2}:\d{2})$/';

    /** Null when $text is not an RFC 3339 time. */
    public static function parse(string $text): ?\DateTimeImmutable
    {
        if (preg_match(self::SHAPE, $text) !== 1) {
            return null;
        }
        try {
            return new \DateTimeImmutable($text);
        } catch (\Exception) {
            return null;
        }
    }
}
