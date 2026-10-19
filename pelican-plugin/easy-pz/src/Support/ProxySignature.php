<?php

namespace Magne4000\EasyPz\Support;

/**
 * The `X-EasyPZ-Proxy` header of proxied public requests: pzman trusts
 * `X-Forwarded-For` only when it carries a valid, fresh signature.
 */
final class ProxySignature
{
    public const HEADER = 'X-EasyPZ-Proxy';

    /**
     * @param  string  $path  the path exactly as sent to pzman, without the query
     */
    public static function header(string $secret, string $method, string $path, ?int $now = null): string
    {
        $t = $now ?? time();

        return 't=' . $t . ',sig=' . self::sign($secret, $t, $method, $path);
    }

    public static function sign(string $secret, int $t, string $method, string $path): string
    {
        return hash_hmac('sha256', $t . "\n" . strtoupper($method) . "\n" . $path, $secret);
    }
}
