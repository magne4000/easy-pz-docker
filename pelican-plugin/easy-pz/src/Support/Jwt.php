<?php

namespace Magne4000\EasyPz\Support;

use InvalidArgumentException;
use JsonException;

/**
 * The HS256 JSON Web Tokens of the "Pelican tokens" contract, written by hand:
 * the plugin needs no composer package.
 */
final class Jwt
{
    /** Lifetime of every token, the contract's maximum (`exp` ≤ `iat` + 60 s). */
    public const TTL = 60;

    public const ISSUER = 'pelican';

    /**
     * Mint a token for one Pelican user and one server.
     *
     * @param  string  $secret  the server's PZMAN_PELICAN_SECRET, used as ASCII text
     * @param  list<string>  $scopes
     *
     * @throws JsonException
     */
    public static function mint(string $secret, string $serverUuid, string $userUuid, string $username, array $scopes, ?int $now = null, ?string $jti = null): string
    {
        if ($secret === '') {
            throw new InvalidArgumentException('empty token secret');
        }

        $iat = $now ?? time();

        return self::sign($secret, [
            'iss' => self::ISSUER,
            'aud' => 'pzman:' . $serverUuid,
            'sub' => $userUuid,
            'name' => $username,
            'scp' => array_values($scopes),
            'iat' => $iat,
            'exp' => $iat + self::TTL,
            'jti' => $jti ?? bin2hex(random_bytes(16)),
        ]);
    }

    /**
     * @param  array<string, mixed>  $claims
     *
     * @throws JsonException
     */
    public static function sign(string $secret, array $claims): string
    {
        $flags = JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE;
        $input = self::base64url(json_encode(['alg' => 'HS256', 'typ' => 'JWT'], $flags))
            . '.' . self::base64url(json_encode($claims, $flags));

        return $input . '.' . self::base64url(hash_hmac('sha256', $input, $secret, true));
    }

    public static function base64url(string $data): string
    {
        return rtrim(strtr(base64_encode($data), '+/', '-_'), '=');
    }
}
