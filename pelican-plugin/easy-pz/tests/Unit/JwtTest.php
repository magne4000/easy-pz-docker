<?php

namespace Magne4000\EasyPz\Tests;

use Magne4000\EasyPz\Support\Jwt;
use Magne4000\EasyPz\Support\ProxySignature;
use Magne4000\EasyPz\Support\SigningKey;
use PHPUnit\Framework\TestCase;

/**
 * Cross-check vectors for pzman's verifier (Phase 2). The key pair is
 * `sodium_crypto_sign_seed_keypair(str_repeat("\x01", 32))`: the Ed25519 seed is
 * 32 bytes of 0x01.
 */
final class JwtTest extends TestCase
{
    public const SEED = "\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01\x01";

    /** PZMAN_PELICAN_PUBLIC_KEY of that seed. */
    public const PUBLIC_KEY = '8a88e3dd7409f195fd52db2d3cba5d72ca6709bf1d94121bf3748801b40f6f5c';

    /**
     * Jwt::mint(secret key, '0d4c1a9e-0000-4000-8000-000000000001',
     * 'b3a1c2d4-0000-4000-8000-000000000002', 'alice', ['read', 'control'],
     * now 1700000000, jti '000102030405060708090a0b0c0d0e0f').
     */
    public const TOKEN = 'eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCJ9'
        . '.eyJpc3MiOiJwZWxpY2FuIiwiYXVkIjoicHptYW46MGQ0YzFhOWUtMDAwMC00MDAwLTgwMDAtMDAwMDAwMDAwMDAxIiwic3ViIjoiYjNhMWMyZDQtMDAwMC00MDAwLTgwMDAtMDAwMDAwMDAwMDAyIiwibmFtZSI6ImFsaWNlIiwic2NwIjpbInJlYWQiLCJjb250cm9sIl0sImlhdCI6MTcwMDAwMDAwMCwiZXhwIjoxNzAwMDAwMDYwLCJqdGkiOiIwMDAxMDIwMzA0MDUwNjA3MDgwOTBhMGIwYzBkMGUwZiJ9'
        . '.u0EV4ffjK_YUiLPen4Hw79FLj0cC-a5hgwdgGLpqraXN4L96jc_yoraIhveRVWNTiKyHI33TmpEsIYtTzaz1AQ';

    /** ProxySignature::header(secret key, 'GET', '/mods/tok/download/x.zip', 1700000000). */
    public const PROXY_HEADER = 't=1700000000,sig=0kTh6wkNGn9f8XRZKKjRaG8ruxRCXJTpIn7lezpyWaSUuOc8rL4YvBzT00Kq2D87VAh70jeaPV78n4K7TVSJCQ';

    public static function secretKey(): string
    {
        return sodium_crypto_sign_secretkey(sodium_crypto_sign_seed_keypair(self::SEED));
    }

    private static function b64d(string $s): string
    {
        self::assertMatchesRegularExpression('/^[A-Za-z0-9_-]+$/', $s, 'base64url without padding');

        return (string) base64_decode(strtr($s, '-_', '+/'), true);
    }

    /** @return array{0: string, 1: array<string, mixed>, 2: string, 3: string} */
    private static function decode(string $token): array
    {
        $parts = explode('.', $token);
        self::assertCount(3, $parts);

        return [
            self::b64d($parts[0]),
            json_decode(self::b64d($parts[1]), true, flags: JSON_THROW_ON_ERROR),
            self::b64d($parts[2]),
            $parts[0] . '.' . $parts[1],
        ];
    }

    public function testSeedIs32BytesOf0x01(): void
    {
        self::assertSame(str_repeat("\x01", 32), self::SEED);
    }

    public function testFixedVectors(): void
    {
        $sk = self::secretKey();

        self::assertSame(self::PUBLIC_KEY, SigningKey::publicKeyHex($sk));
        self::assertSame(self::TOKEN, Jwt::mint($sk, '0d4c1a9e-0000-4000-8000-000000000001', 'b3a1c2d4-0000-4000-8000-000000000002', 'alice', ['read', 'control'], 1_700_000_000, '000102030405060708090a0b0c0d0e0f'));
        self::assertSame(self::PROXY_HEADER, ProxySignature::header($sk, 'GET', '/mods/tok/download/x.zip', 1_700_000_000));
    }

    public function testClaimsFollowTheContract(): void
    {
        [$header, $claims] = self::decode(self::TOKEN);

        self::assertSame('{"alg":"EdDSA","typ":"JWT"}', $header);
        self::assertSame([
            'iss' => 'pelican',
            'aud' => 'pzman:0d4c1a9e-0000-4000-8000-000000000001',
            'sub' => 'b3a1c2d4-0000-4000-8000-000000000002',
            'name' => 'alice',
            'scp' => ['read', 'control'],
            'iat' => 1_700_000_000,
            'exp' => 1_700_000_060,
            'jti' => '000102030405060708090a0b0c0d0e0f',
        ], $claims);
    }

    public function testSignatureVerifiesWithThePublicKeyOnly(): void
    {
        [, , $sig, $input] = self::decode(Jwt::mint(self::secretKey(), 's', 'u', 'n', ['read']));
        $pk = (string) hex2bin(self::PUBLIC_KEY);

        self::assertSame(SODIUM_CRYPTO_SIGN_BYTES, strlen($sig));
        self::assertTrue(sodium_crypto_sign_verify_detached($sig, $input, $pk));
        self::assertFalse(sodium_crypto_sign_verify_detached($sig, $input . 'x', $pk));

        $other = sodium_crypto_sign_publickey(sodium_crypto_sign_keypair());
        self::assertFalse(sodium_crypto_sign_verify_detached($sig, $input, $other));
    }

    public function testJtiIsRandomAndAtLeast16Bytes(): void
    {
        [, $a] = self::decode(Jwt::mint(self::secretKey(), 's', 'u', 'n', ['read']));
        [, $b] = self::decode(Jwt::mint(self::secretKey(), 's', 'u', 'n', ['read']));

        self::assertMatchesRegularExpression('/^[0-9a-f]{32,}$/', $a['jti']);
        self::assertNotSame($a['jti'], $b['jti']);
        self::assertSame($a['iat'] + 60, $a['exp']);
        self::assertEqualsWithDelta(time(), $a['iat'], 5);
        self::assertIsString($a['aud']);
    }

    public function testUnicodeAndSlashesSurvive(): void
    {
        [, $claims] = self::decode(Jwt::mint(self::secretKey(), 's', 'u', 'zoë/admin', ['read']));
        self::assertSame('zoë/admin', $claims['name']);
    }

    public function testBase64url(): void
    {
        self::assertSame('-_-_', Jwt::base64url("\xfb\xff\xbf"));
        self::assertSame('YQ', Jwt::base64url('a'));
    }

    public function testAMalformedKeyIsRefused(): void
    {
        $this->expectException(\InvalidArgumentException::class);
        Jwt::mint(str_repeat('k', 32), 's', 'u', 'n', []);
    }
}
