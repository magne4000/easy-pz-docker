<?php

namespace Magne4000\EasyPz\Support;

use JsonException;

/**
 * The EdDSA (Ed25519) JSON Web Tokens of the "Pelican tokens" contract, written
 * by hand: the plugin needs no composer package.
 */
final class Jwt
{
    /** Lifetime of every token, the contract's maximum (`exp` ≤ `iat` + 60 s). */
    public const TTL = 60;

    public const ISSUER = 'pelican';

    /** The header, byte for byte. */
    public const HEADER = '{"alg":"EdDSA","typ":"JWT"}';

    /**
     * Mint a token for one Pelican user and one server.
     *
     * @param  string  $secretKey  the server's 64-byte Ed25519 secret key
     * @param  list<string>  $scopes
     *
     * @throws JsonException
     */
    public static function mint(string $secretKey, string $serverUuid, string $userUuid, string $username, array $scopes, ?int $now = null, ?string $jti = null): string
    {
        $iat = $now ?? time();

        return self::sign($secretKey, [
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
    public static function sign(string $secretKey, array $claims): string
    {
        $input = self::base64url(self::HEADER)
            . '.' . self::base64url(json_encode($claims, JSON_THROW_ON_ERROR | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE));

        return $input . '.' . SigningKey::sign($secretKey, $input);
    }

    public static function base64url(string $data): string
    {
        return rtrim(strtr(base64_encode($data), '+/', '-_'), '=');
    }
}
