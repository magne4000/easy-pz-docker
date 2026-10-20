<?php

namespace Magne4000\EasyPz\Support;

use InvalidArgumentException;

/**
 * The per-server Ed25519 key: the panel keeps the secret key (encrypted in its
 * database) and signs tokens and proxied requests with it; pzman only gets the
 * public key (`PZMAN_PELICAN_PUBLIC_KEY`), so reading the server's files does
 * not let anyone sign.
 */
final class SigningKey
{
    /** A new secret key (libsodium's 64-byte form: seed followed by the public key). */
    public static function generate(): string
    {
        return sodium_crypto_sign_secretkey(sodium_crypto_sign_keypair());
    }

    /** `PZMAN_PELICAN_PUBLIC_KEY`: the 32-byte public key as 64 lowercase hex characters. */
    public static function publicKeyHex(string $secretKey): string
    {
        self::assertSecretKey($secretKey);

        return bin2hex(sodium_crypto_sign_publickey_from_secretkey($secretKey));
    }

    /** The detached Ed25519 signature of $message, base64url without padding. */
    public static function sign(string $secretKey, string $message): string
    {
        self::assertSecretKey($secretKey);

        return Jwt::base64url(sodium_crypto_sign_detached($message, $secretKey));
    }

    public static function isValidPublicKeyHex(string $publicKey): bool
    {
        return preg_match('/^[0-9a-f]{64}$/', $publicKey) === 1;
    }

    /** The secret key as stored by the panel (hex) back to bytes. */
    public static function fromHex(string $hex): string
    {
        $bytes = preg_match('/^[0-9a-f]{128}$/', $hex) === 1 ? hex2bin($hex) : false;
        if ($bytes === false) {
            throw new InvalidArgumentException('the stored signing key is not 128 lowercase hex characters');
        }

        return $bytes;
    }

    private static function assertSecretKey(string $secretKey): void
    {
        if (strlen($secretKey) !== SODIUM_CRYPTO_SIGN_SECRETKEYBYTES) {
            throw new InvalidArgumentException('an Ed25519 secret key is 64 bytes');
        }
    }
}
