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
    public static function header(string $secretKey, string $method, string $path, ?int $now = null): string
    {
        $t = $now ?? time();

        return 't=' . $t . ',sig=' . SigningKey::sign($secretKey, self::message($t, $method, $path));
    }

    /** The signed text: `<t>\n<METHOD>\n<path>`. */
    public static function message(int $t, string $method, string $path): string
    {
        return $t . "\n" . strtoupper($method) . "\n" . $path;
    }
}
