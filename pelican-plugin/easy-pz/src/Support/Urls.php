<?php

namespace Magne4000\EasyPz\Support;

use InvalidArgumentException;

final class Urls
{
    public const RELEASES = 'https://github.com/magne4000/easy-pz-docker/releases/download';

    /**
     * pzman's UI (and service-call) URL: `http://<node FQDN>:<UI allocation port>`
     * unless the server page stores an override (a reverse proxy in front of pzman).
     */
    public static function pzmanUi(string $nodeFqdn, int $uiPort, ?string $override = null): string
    {
        $override = trim((string) $override);
        if ($override !== '') {
            return self::validated($override);
        }

        return 'http://' . self::host($nodeFqdn) . ':' . $uiPort;
    }

    /** `PANEL_PUBLIC_URL`: `<panel URL>/easy-pz/<uuid_short>`. */
    public static function panelPublic(string $panelUrl, string $uuidShort): string
    {
        return self::panel($panelUrl) . '/easy-pz/' . rawurlencode($uuidShort);
    }

    /** `PZMAN_PELICAN_PANEL_URL`: the panel origin, without a trailing slash. */
    public static function panel(string $panelUrl): string
    {
        return rtrim(self::validated($panelUrl), '/');
    }

    /** `PANEL_PUBLIC_GAME_ADDRESS`: `<node FQDN>:<primary allocation port>`. */
    public static function gameAddress(string $nodeFqdn, int $gamePort): string
    {
        return self::host($nodeFqdn) . ':' . $gamePort;
    }

    /**
     * The directory holding `pzman-linux-amd64` and `pzman-SHA256SUMS`: the
     * release of the pinned version, or the `EASYPZ_PZMAN_BASE_URL` setting.
     */
    public static function pzmanDownloadBase(string $version, ?string $override = null): string
    {
        $override = trim((string) $override);
        if ($override !== '') {
            return rtrim(self::validated($override), '/');
        }

        return self::RELEASES . '/v' . ltrim($version, 'v');
    }

    /** An http(s) URL without a trailing slash, or an exception. */
    public static function validated(string $url): string
    {
        $url = trim($url);
        $parts = parse_url($url);
        if ($parts === false || !in_array(strtolower($parts['scheme'] ?? ''), ['http', 'https'], true) || ($parts['host'] ?? '') === ''
            || isset($parts['query']) || isset($parts['fragment']) || preg_match('/\s/', $url) === 1) {
            throw new InvalidArgumentException("not an http(s) URL: $url");
        }

        return rtrim($url, '/');
    }

    private static function host(string $fqdn): string
    {
        $fqdn = trim($fqdn);

        return str_contains($fqdn, ':') && !str_starts_with($fqdn, '[') ? "[$fqdn]" : $fqdn;
    }
}
