<?php

declare(strict_types=1);

namespace Panmail;

/** One condition of a filter rule that fired. */
final class MatchedCondition
{
    /** @param list<string> $values */
    public function __construct(
        public readonly string $field,
        public readonly string $operator,
        public readonly array $values,
        public readonly string $header,
        public readonly int $number,
    ) {
    }
}
