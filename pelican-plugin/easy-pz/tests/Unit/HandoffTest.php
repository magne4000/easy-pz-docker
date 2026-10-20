<?php

namespace Magne4000\EasyPz\Tests;

use Magne4000\EasyPz\Support\Handoff;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class HandoffTest extends TestCase
{
    private const UI = 'http://node.example:8080';

    public function testWithoutTo(): void
    {
        self::assertSame('http://node.example:8080/auth/pelican#t=a.b.c', Handoff::url(self::UI, 'a.b.c'));
        self::assertSame('http://node.example:8080/auth/pelican#t=a.b.c', Handoff::url(self::UI, 'a.b.c', null));
    }

    public function testWithAValidToItIsEncoded(): void
    {
        self::assertSame(
            'http://node.example:8080/auth/pelican#t=a.b.c&to=%2Fmods%3Ftab%3Dlocal%26q%3Da%20b%23top',
            Handoff::url(self::UI, 'a.b.c', '/mods?tab=local&q=a b#top'),
        );
        self::assertSame('http://node.example:8080/auth/pelican#t=x&to=%2F', Handoff::url(self::UI, 'x', '/'));
        self::assertSame('/caf%C3%A9:x', Handoff::validTo('/caf%C3%A9:x'));
        self::assertSame('/' . str_repeat('a', 511), Handoff::validTo('/' . str_repeat('a', 511)));
    }

    /** @return iterable<string, array{mixed}> */
    public static function invalidTo(): iterable
    {
        yield 'empty' => [''];
        yield 'relative' => ['mods'];
        yield 'protocol-relative' => ['//evil.example/x'];
        yield 'absolute URL' => ['https://evil.example/x'];
        yield 'javascript' => ['javascript:alert(1)'];
        yield 'backslash' => ['/\\evil.example'];
        yield 'backslash later' => ['/a\\b'];
        yield 'newline' => ["/a\nb"];
        yield 'tab' => ["/a\tb"];
        yield 'NUL' => ["/a\0b"];
        yield 'DEL' => ["/a\x7fb"];
        yield 'too long' => ['/' . str_repeat('a', 512)];
        yield 'array (to[]=…)' => [['/x']];
        yield 'leading space' => [' /x'];
    }

    #[DataProvider('invalidTo')]
    public function testAnInvalidToIsIgnored(mixed $to): void
    {
        self::assertNull(Handoff::validTo($to));
        self::assertSame('http://node.example:8080/auth/pelican#t=a.b.c', Handoff::url(self::UI, 'a.b.c', $to));
    }
}
