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
}
