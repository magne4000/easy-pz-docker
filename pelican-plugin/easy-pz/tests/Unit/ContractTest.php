<?php

namespace Magne4000\EasyPz\Tests;

use InvalidArgumentException;
use Magne4000\EasyPz\Support\EggCheck;
use Magne4000\EasyPz\Support\ProxySignature;
use Magne4000\EasyPz\Support\PzmanEnv;
use Magne4000\EasyPz\Support\Scopes;
use Magne4000\EasyPz\Support\Secret;
use Magne4000\EasyPz\Support\Sha256Sums;
use Magne4000\EasyPz\Support\Urls;
use PHPUnit\Framework\Attributes\DataProvider;
use PHPUnit\Framework\TestCase;

final class ContractTest extends TestCase
{
    private const SECRET = 'f00dfeedf00dfeedf00dfeedf00dfeedf00dfeedf00dfeedf00dfeedf00dfeed';

    public function testProxySignature(): void
    {
        $header = ProxySignature::header(self::SECRET, 'get', '/mods/tok/download/x.zip', 1_700_000_000);
        $expected = hash_hmac('sha256', "1700000000\nGET\n/mods/tok/download/x.zip", self::SECRET);

        self::assertSame("t=1700000000,sig=$expected", $header);
        self::assertMatchesRegularExpression('/^t=\d+,sig=[0-9a-f]{64}$/', ProxySignature::header(self::SECRET, 'HEAD', '/assets/a.js'));
    }

    public function testSecret(): void
    {
        $a = Secret::generate();
        self::assertMatchesRegularExpression('/^[0-9a-f]{64}$/', $a);
        self::assertNotSame($a, Secret::generate());
        self::assertTrue(Secret::isValid(self::SECRET));
        self::assertFalse(Secret::isValid(strtoupper(self::SECRET)));
        self::assertFalse(Secret::isValid(substr(self::SECRET, 1)));
    }

    public function testPzmanEnvHasTheContractKeys(): void
    {
        $values = PzmanEnv::values(8080, '0d4c1a9e-0000-4000-8000-000000000001', self::SECRET, 'https://panel.example', 'https://panel.example/easy-pz/0d4c1a9e', 'node.example:16261');
        $text = PzmanEnv::render($values);

        self::assertStringEndsWith("\n", $text);
        $parsed = [];
        foreach (explode("\n", $text) as $line) {
            if ($line === '' || str_starts_with($line, '#')) {
                continue;
            }
            [$k, $v] = explode('=', $line, 2);
            $parsed[$k] = $v;
        }
        self::assertSame([
            'PZMAN_HOST' => 'pelican',
            'PANEL_PORT' => '8080',
            'PZMAN_PELICAN_SERVER' => '0d4c1a9e-0000-4000-8000-000000000001',
            'PZMAN_PELICAN_SECRET' => self::SECRET,
            'PZMAN_PELICAN_PANEL_URL' => 'https://panel.example',
            'PANEL_PUBLIC_URL' => 'https://panel.example/easy-pz/0d4c1a9e',
            'PANEL_PUBLIC_GAME_ADDRESS' => 'node.example:16261',
        ], $parsed);
    }

    public function testPzmanEnvValuesAreVerbatim(): void
    {
        self::assertSame("A=x y \"z\" \$HOME #no\n", explode("\n", PzmanEnv::render(['A' => 'x y "z" $HOME #no']), 2)[1]);
    }

    /** @return iterable<string, array{array<string, string>}> */
    public static function badEnv(): iterable
    {
        yield 'newline' => [['A' => "x\nB=y"]];
        yield 'carriage return' => [['A' => "x\ry"]];
        yield 'lowercase key' => [['a' => 'x']];
        yield 'key with =' => [['A=B' => 'x']];
    }

    /** @param array<string, string> $values */
    #[DataProvider('badEnv')]
    public function testPzmanEnvRejects(array $values): void
    {
        $this->expectException(InvalidArgumentException::class);
        PzmanEnv::render($values);
    }

    public function testPzmanEnvRejectsAMalformedSecret(): void
    {
        $this->expectException(InvalidArgumentException::class);
        PzmanEnv::values(1, 's', 'nothex', 'http://p', 'http://p/easy-pz/x', 'h:1');
    }

    public function testScopes(): void
    {
        self::assertSame(['read', 'control', 'console', 'mods', 'config', 'backups', 'schedules', 'settings'], Scopes::forUser(true, []));
        self::assertSame(['read', 'backups'], Scopes::forUser(false, ['easy-pz.backups', 'control.console', 'easy-pz.read', 'file.read']));
        self::assertSame([], Scopes::forUser(false, ['control.start']));
        self::assertSame('easy-pz.read', Scopes::permission(Scopes::READ));
    }

    private const STOCK_STARTUP = 'export PATH="./jre64/bin:$PATH" ; ./ProjectZomboid64  -port {{SERVER_PORT}} -udpport {{STEAM_PORT}} -cachedir=/home/container/.cache -servername "{{SERVER_NAME}}"';

    public function testEggCheckAcceptsTheStockEgg(): void
    {
        self::assertSame([], EggCheck::problems('quit', ['Default' => self::STOCK_STARTUP], '380870'));
    }

    public function testEggCheckReportsEachMismatch(): void
    {
        $problems = EggCheck::problems('^C', ['Default' => str_replace('/home/container/.cache', '/home/container/Zomboid', self::STOCK_STARTUP)], '108600');
        self::assertCount(3, $problems);
        self::assertStringContainsString('stop command', $problems[0]);
        self::assertStringContainsString('-cachedir=/home/container/.cache', $problems[1]);
        self::assertStringContainsString('SRCDS_APPID', $problems[2]);

        self::assertCount(1, EggCheck::problems(null, [self::STOCK_STARTUP], '380870'));
        self::assertCount(1, EggCheck::problems('quit', [], '380870'));
        self::assertCount(1, EggCheck::problems('quit', ['./x -cachedir=/home/container/.cache2'], '380870'));
        self::assertCount(1, EggCheck::problems('quit', [self::STOCK_STARTUP], null));
    }

    public function testUrls(): void
    {
        self::assertSame('http://node.example:8080', Urls::pzmanUi('node.example', 8080));
        self::assertSame('http://[2001:db8::1]:8080', Urls::pzmanUi('2001:db8::1', 8080));
        self::assertSame('https://pz.example/admin', Urls::pzmanUi('node.example', 8080, ' https://pz.example/admin/ '));
        self::assertSame('http://node.example:8080', Urls::pzmanUi('node.example', 8080, ''));
        self::assertSame('https://panel.example/easy-pz/abcd1234', Urls::panelPublic('https://panel.example/', 'abcd1234'));
        self::assertSame('https://panel.example/pelican/easy-pz/abcd1234', Urls::panelPublic('https://panel.example/pelican', 'abcd1234'));
        self::assertSame('https://panel.example', Urls::panel('https://panel.example/'));
        self::assertSame('node.example:16261', Urls::gameAddress('node.example', 16261));
        self::assertSame('https://github.com/magne4000/easy-pz-docker/releases/download/v1.2.3', Urls::pzmanDownloadBase('1.2.3'));
        self::assertSame('http://192.168.1.2:8000/dist', Urls::pzmanDownloadBase('1.2.3', 'http://192.168.1.2:8000/dist/'));
    }

    /** @return iterable<string, array{string}> */
    public static function badUrls(): iterable
    {
        yield 'no scheme' => ['pz.example:8080'];
        yield 'ftp' => ['ftp://pz.example'];
        yield 'query' => ['https://pz.example/?a=b'];
        yield 'fragment' => ['https://pz.example/#x'];
        yield 'space' => ['https://pz.example/a b'];
        yield 'newline' => ["https://pz.example/\nX=1"];
    }

    #[DataProvider('badUrls')]
    public function testUrlOverrideIsValidated(string $url): void
    {
        $this->expectException(InvalidArgumentException::class);
        Urls::pzmanUi('node', 1, $url);
    }

    public function testSha256Sums(): void
    {
        $a = str_repeat('a', 64);
        $b = str_repeat('B', 64);
        $sums = "$a  pzman-linux-arm64\r\n$b *dist/pzman-linux-amd64\n\n";

        self::assertSame(str_repeat('b', 64), Sha256Sums::find($sums, 'pzman-linux-amd64'));
        self::assertSame($a, Sha256Sums::find($sums, 'pzman-linux-arm64'));
        self::assertNull(Sha256Sums::find($sums, 'pzman-linux-386'));
        self::assertNull(Sha256Sums::find('garbage', 'pzman-linux-amd64'));
        self::assertTrue(Sha256Sums::matches($b, str_repeat('b', 64)));
        self::assertFalse(Sha256Sums::matches($a, str_repeat('b', 64)));
    }
}
