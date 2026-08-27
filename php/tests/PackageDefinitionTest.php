<?php

declare(strict_types=1);

namespace Panmail\Tests;

use PHPUnit\Framework\TestCase;

/**
 * The package is defined twice and must not say two different things.
 *
 * Packagist reads composer.json from the root of a repository, so that is where
 * the published package lives. php/composer.json stays because it is what local
 * development and CI install from. Two files describing one package is exactly
 * the kind of thing that drifts quietly, so it is checked rather than trusted.
 */
final class PackageDefinitionTest extends TestCase
{
    /** @return array<string, mixed> */
    private static function read(string $path): array
    {
        $raw = file_get_contents($path);
        self::assertIsString($raw, "could not read $path");

        /** @var array<string, mixed> $decoded */
        $decoded = json_decode($raw, true, 512, JSON_THROW_ON_ERROR);

        return $decoded;
    }

    /** @return array<string, mixed> */
    private static function root(): array
    {
        return self::read(__DIR__ . '/../../composer.json');
    }

    /** @return array<string, mixed> */
    private static function dev(): array
    {
        return self::read(__DIR__ . '/../composer.json');
    }

    public function testTheTwoDefinitionsAgreeOnWhatTheyRequire(): void
    {
        self::assertSame(
            self::dev()['require'],
            self::root()['require'],
            'the published package and the development one require different things'
        );
    }

    public function testTheTwoDefinitionsAgreeOnTheirIdentity(): void
    {
        foreach (['name', 'description', 'license', 'type'] as $field) {
            self::assertSame(
                self::dev()[$field] ?? null,
                self::root()[$field] ?? null,
                "the two definitions disagree on \"$field\""
            );
        }
    }

    /**
     * The published autoload maps into php/src from a directory above it, which
     * is the one path that a move would break without any test noticing.
     */
    public function testThePublishedAutoloadPointsAtTheSource(): void
    {
        /** @var array{psr-4: array<string, string>} $autoload */
        $autoload = self::root()['autoload'];
        $prefix = $autoload['psr-4']['Panmail\\'] ?? null;

        self::assertSame('php/src/', $prefix, 'the published psr-4 root moved');
        self::assertFileExists(__DIR__ . '/../../' . $prefix . 'Client.php');
        self::assertFileExists(__DIR__ . '/../../' . $prefix . 'Transport/CurlTransport.php');
    }
}
