<?php

namespace Magne4000\EasyPz\Tests;

use Magne4000\EasyPz\Support\Jwt;
use PHPUnit\Framework\TestCase;

final class JwtTest extends TestCase
{
    private const SECRET = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';

    /** @return array{0: array<string, mixed>, 1: array<string, mixed>, 2: string, 3: string} */
    private static function decode(string $token): array
    {
        $parts = explode('.', $token);
        self::assertCount(3, $parts);
        foreach ($parts as $part) {
            self::assertMatchesRegularExpression('/^[A-Za-z0-9_-]+$/', $part, 'base64url without padding');
        }
        $dec = fn (string $s) => base64_decode(strtr($s, '-_', '+/'), true);

        return [
            json_decode($dec($parts[0]), true, flags: JSON_THROW_ON_ERROR),
            json_decode($dec($parts[1]), true, flags: JSON_THROW_ON_ERROR),
            $dec($parts[2]),
            $parts[0] . '.' . $parts[1],
        ];
    }

    public function testClaimsFollowTheContract(): void
    {
        $token = Jwt::mint(self::SECRET, 'srv-uuid', 'user-uuid', 'alice', ['read', 'control'], 1_700_000_000, 'abc');
        [$header, $claims] = self::decode($token);

        self::assertSame(['alg' => 'HS256', 'typ' => 'JWT'], $header);
        self::assertSame([
            'iss' => 'pelican',
            'aud' => 'pzman:srv-uuid',
            'sub' => 'user-uuid',
            'name' => 'alice',
            'scp' => ['read', 'control'],
            'iat' => 1_700_000_000,
            'exp' => 1_700_000_060,
            'jti' => 'abc',
        ], $claims);
    }

    public function testSignatureIsHmacSha256WithTheSecretText(): void
    {
        $token = Jwt::mint(self::SECRET, 's', 'u', 'n', ['read']);
        [, , $sig, $input] = self::decode($token);

        self::assertSame(hash_hmac('sha256', $input, self::SECRET, true), $sig);
        self::assertNotSame(hash_hmac('sha256', $input, hex2bin(self::SECRET), true), $sig, 'the key is the ASCII text, not the decoded bytes');
    }

    public function testAudIsASingleString(): void
    {
        [, $claims] = self::decode(Jwt::mint(self::SECRET, 'x', 'u', 'n', []));
        self::assertIsString($claims['aud']);
        self::assertSame([], $claims['scp']);
    }

    public function testJtiIsRandomAndAtLeast16Bytes(): void
    {
        [, $a] = self::decode(Jwt::mint(self::SECRET, 's', 'u', 'n', ['read']));
        [, $b] = self::decode(Jwt::mint(self::SECRET, 's', 'u', 'n', ['read']));

        self::assertMatchesRegularExpression('/^[0-9a-f]{32,}$/', $a['jti']);
        self::assertNotSame($a['jti'], $b['jti']);
        self::assertSame($a['iat'] + 60, $a['exp']);
        self::assertEqualsWithDelta(time(), $a['iat'], 5);
    }

    public function testUnicodeAndSlashesSurvive(): void
    {
        [, $claims] = self::decode(Jwt::mint(self::SECRET, 's', 'u', 'zoë/admin', ['read']));
        self::assertSame('zoë/admin', $claims['name']);
    }

    public function testBase64url(): void
    {
        self::assertSame('-_-_', Jwt::base64url("\xfb\xff\xbf"));
        self::assertSame('YQ', Jwt::base64url('a'));
    }

    public function testEmptySecretIsRefused(): void
    {
        $this->expectException(\InvalidArgumentException::class);
        Jwt::mint('', 's', 'u', 'n', []);
    }
}
