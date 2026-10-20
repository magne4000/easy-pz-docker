<?php

namespace Magne4000\EasyPz\Support;

/**
 * pzman reports its version as tagged (`v1.2.3`); the plugin pins it without
 * the `v` (`1.2.3`).
 */
final class Version
{
    public static function same(?string $a, ?string $b): bool
    {
        return $a !== null && $b !== null && ltrim(trim($a), 'v') === ltrim(trim($b), 'v');
    }
}
