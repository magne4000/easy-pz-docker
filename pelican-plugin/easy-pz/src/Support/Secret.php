<?php

namespace Magne4000\EasyPz\Support;

/**
 * `PZMAN_PELICAN_SECRET`: 32 random bytes as 64 lowercase hex characters. The
 * HMAC and JWT key is that text, not the decoded bytes.
 */
final class Secret
{
    public static function generate(): string
    {
        return bin2hex(random_bytes(32));
    }

    public static function isValid(string $secret): bool
    {
        return preg_match('/^[0-9a-f]{64}$/', $secret) === 1;
    }
}
