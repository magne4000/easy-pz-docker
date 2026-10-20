<?php

namespace Magne4000\EasyPz\Tests;

use Magne4000\EasyPz\Support\Urls;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class SharedHostTest extends TestCase
{
    /** @return iterable<string, array{string, string, bool}> */
    public static function pairs(): iterable
    {
        yield 'same host, other port' => ['https://panel.example', 'http://panel.example:26270', true];
        yield 'case and trailing dot' => ['https://Panel.Example./', 'http://panel.example:8080', true];
        yield 'same IP' => ['http://192.168.1.10:16264', 'http://192.168.1.10:26270', true];
        yield 'same IPv6' => ['http://[2001:db8::1]:80', 'http://[2001:DB8::1]:26270', true];
        yield 'panel with a path' => ['https://panel.example/pelican', 'http://panel.example:26270', true];
        yield 'name vs IP' => ['https://panel.example', 'http://192.168.1.10:26270', false];
        yield 'subdomain' => ['https://panel.example', 'https://pz.panel.example', false];
        yield 'override on another host' => ['https://panel.example', 'https://pz.example', false];
        yield 'unparsable panel URL' => ['', 'http://panel.example:26270', false];
        yield 'no host' => ['/relative', '/relative', false];
    }

    #[DataProvider('pairs')]
    public function testSameHost(string $panel, string $ui, bool $same): void
    {
        self::assertSame($same, Urls::sameHost($panel, $ui));
        self::assertSame($same, Urls::sameHost($ui, $panel));
    }

    public function testWithTheBuiltUiUrl(): void
    {
        self::assertTrue(Urls::sameHost('http://192.168.1.10:16264', Urls::pzmanUi('192.168.1.10', 26270)));
        self::assertFalse(Urls::sameHost('http://192.168.1.10:16264', Urls::pzmanUi('192.168.1.10', 26270, 'https://pz.example')));
    }
}
