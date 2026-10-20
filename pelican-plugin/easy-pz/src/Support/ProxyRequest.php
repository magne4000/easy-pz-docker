<?php

namespace Magne4000\EasyPz\Support;

/**
 * Mapping of the panel's public routes onto pzman (contract "public mod page
 * through the panel") and the headers passed each way.
 */
final class ProxyRequest
{
    /** Request headers passed to pzman. */
    public const FORWARD = ['Range', 'If-Range', 'If-None-Match', 'If-Modified-Since', 'Accept'];

    /** Request headers passed to pzman with a request body (the launcher's `POST files/<id>`). */
    public const FORWARD_WITH_BODY = ['Content-Type', 'Content-Length'];

    /** Methods of the mods route; the assets route is GET/HEAD only. */
    public const MODS_METHODS = ['GET', 'HEAD', 'POST'];

    /** Response headers passed back to the client. */
    public const PASS_BACK = [
        'Content-Type',
        'Content-Length',
        'Content-Range',
        'Accept-Ranges',
        'ETag',
        'Last-Modified',
        'Cache-Control',
        'Content-Disposition',
        'X-Robots-Tag',
        'Referrer-Policy',
        'Retry-After',
    ];

    /**
     * The path pzman receives for a panel request path (raw, as sent by the
     * client): `/easy-pz/<uuid_short>/mods/…` → `/mods/…`,
     * `/easy-pz/<uuid_short>/assets/…` → `/assets/…`. Null when the path is
     * not a public one or has dot segments.
     */
    public static function pzmanPath(string $panelPath, string $uuidShort): ?string
    {
        $prefix = '/easy-pz/' . $uuidShort;
        if (!str_starts_with($panelPath, $prefix . '/')) {
            return null;
        }

        $path = substr($panelPath, strlen($prefix));
        if (!str_starts_with($path, '/mods/') && !str_starts_with($path, '/assets/')) {
            return null;
        }

        foreach (explode('/', $path) as $segment) {
            $decoded = rawurldecode($segment);
            if ($decoded === '.' || $decoded === '..' || str_contains($decoded, '/') || str_contains($decoded, '\\')) {
                return null;
            }
        }

        return $path;
    }

    /**
     * @param  array<string, list<string>|string>  $headers  response headers, any case
     * @return array<string, string>
     */
    public static function passBack(array $headers): array
    {
        $byLower = [];
        foreach ($headers as $name => $value) {
            $byLower[strtolower($name)] = is_array($value) ? implode(', ', $value) : $value;
        }

        $out = [];
        foreach (self::PASS_BACK as $name) {
            if (isset($byLower[strtolower($name)])) {
                $out[$name] = $byLower[strtolower($name)];
            }
        }

        return $out;
    }

    /**
     * The request headers sent to pzman, from the client's (any case).
     *
     * @param  array<string, list<string|null>|string|null>  $clientHeaders
     * @return array<string, string>
     */
    public static function forwardHeaders(string $method, array $clientHeaders): array
    {
        $byLower = [];
        foreach ($clientHeaders as $name => $value) {
            $value = is_array($value) ? ($value[0] ?? null) : $value;
            if ($value !== null) {
                $byLower[strtolower($name)] = $value;
            }
        }

        $names = strtoupper($method) === 'POST' ? [...self::FORWARD, ...self::FORWARD_WITH_BODY] : self::FORWARD;
        $out = [];
        foreach ($names as $name) {
            if (isset($byLower[strtolower($name)])) {
                $out[$name] = $byLower[strtolower($name)];
            }
        }

        return $out;
    }
}
