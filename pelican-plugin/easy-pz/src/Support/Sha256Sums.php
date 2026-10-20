<?php

namespace Magne4000\EasyPz\Support;

/** Reads `sha256sum` output, as published in `pzman-SHA256SUMS`. */
final class Sha256Sums
{
    /** The lowercase hex digest listed for $file, or null. */
    public static function find(string $sums, string $file): ?string
    {
        foreach (preg_split('/\r?\n/', $sums) ?: [] as $line) {
            // "<hex>  <name>" (text mode) or "<hex> *<name>" (binary mode)
            if (preg_match('/^([0-9a-fA-F]{64}) [ *](.+)$/', trim($line), $m) === 1 && basename($m[2]) === $file) {
                return strtolower($m[1]);
            }
        }

        return null;
    }

    public static function matches(string $expected, string $actual): bool
    {
        return hash_equals(strtolower($expected), strtolower($actual));
    }
}
