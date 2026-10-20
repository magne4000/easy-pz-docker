<?php

namespace Magne4000\EasyPz\Support;

/**
 * The handoff redirect: `<pzman UI URL>/auth/pelican#t=<token>`, plus
 * `&to=<path>` when pzman sent the browser here to renew its session and
 * wants it back on a page of its own.
 */
final class Handoff
{
    public const MAX_TO = 512;

    /** $to when it is a same-origin absolute path, else null (ignored). */
    public static function validTo(mixed $to): ?string
    {
        if (!is_string($to) || $to === '' || strlen($to) > self::MAX_TO) {
            return null;
        }
        if ($to[0] !== '/' || str_starts_with($to, '//') || str_contains($to, '\\')) {
            return null;
        }
        // Control characters (C0 and DEL) never belong in a path.
        if (preg_match('/[\x00-\x1f\x7f]/', $to) === 1) {
            return null;
        }
        $parts = parse_url($to);
        if ($parts === false || isset($parts['scheme']) || isset($parts['host'])) {
            return null;
        }

        return $to;
    }

    public static function url(string $pzmanUiUrl, string $token, mixed $to = null): string
    {
        $url = $pzmanUiUrl . '/auth/pelican#t=' . $token;
        $to = self::validTo($to);

        return $to === null ? $url : $url . '&to=' . rawurlencode($to);
    }
}
