<?php

namespace Magne4000\EasyPz\Tests;

use Magne4000\EasyPz\Support\DownloadSlots;
use Magne4000\EasyPz\Support\ProxyRequest;
use PHPUnit\Framework\TestCase;

final class ProxyTest extends TestCase
{
    public function testPzmanPath(): void
    {
        self::assertSame('/mods/tok/download/x.zip', ProxyRequest::pzmanPath('/easy-pz/abcd1234/mods/tok/download/x.zip', 'abcd1234'));
        self::assertSame('/mods/tok/', ProxyRequest::pzmanPath('/easy-pz/abcd1234/mods/tok/', 'abcd1234'));
        self::assertSame('/mods/tok', ProxyRequest::pzmanPath('/easy-pz/abcd1234/mods/tok', 'abcd1234'));
        self::assertSame('/assets/index-1.js', ProxyRequest::pzmanPath('/easy-pz/abcd1234/assets/index-1.js', 'abcd1234'));
        self::assertSame('/mods/tok/files/12/a%20b.lua', ProxyRequest::pzmanPath('/easy-pz/abcd1234/mods/tok/files/12/a%20b.lua', 'abcd1234'), 'percent-encoding kept as sent');

        self::assertNull(ProxyRequest::pzmanPath('/easy-pz/other/mods/tok/', 'abcd1234'));
        self::assertNull(ProxyRequest::pzmanPath('/easy-pz/abcd1234/api/system', 'abcd1234'));
        self::assertNull(ProxyRequest::pzmanPath('/easy-pz/abcd1234/mods/tok/../../api/system', 'abcd1234'));
        self::assertNull(ProxyRequest::pzmanPath('/easy-pz/abcd1234/mods/tok/%2e%2e/%2E%2E/api/system', 'abcd1234'));
        self::assertNull(ProxyRequest::pzmanPath('/easy-pz/abcd1234/mods/tok/a%2f..%2fb', 'abcd1234'));
        self::assertNull(ProxyRequest::pzmanPath('/easy-pz/abcd1234/assets/./x', 'abcd1234'));
    }

    public function testPassBack(): void
    {
        $out = ProxyRequest::passBack([
            'content-type' => ['application/zip'],
            'Content-Length' => '10',
            'content-range' => 'bytes 0-9/100',
            'Set-Cookie' => 'x=y',
            'Server' => 'pzman',
            'etag' => ['"a"'],
            'Retry-After' => '5',
        ]);

        self::assertSame([
            'Content-Type' => 'application/zip',
            'Content-Length' => '10',
            'Content-Range' => 'bytes 0-9/100',
            'ETag' => '"a"',
            'Retry-After' => '5',
        ], $out);
    }

    public function testResponseHeadersIsolateThePanelOrigin(): void
    {
        $out = ProxyRequest::responseHeaders([
            'Content-Type' => ['text/html'],
            'Content-Security-Policy' => ["default-src 'self'"],
            'x-content-type-options' => 'sniff-me',
            'Access-Control-Allow-Origin' => 'https://evil.example',
            'Access-Control-Allow-Credentials' => 'true',
            'Set-Cookie' => 'panel_session=x',
        ]);

        self::assertSame([
            'Content-Security-Policy' => 'sandbox allow-scripts allow-downloads allow-popups',
            'X-Content-Type-Options' => 'nosniff',
            'Access-Control-Allow-Origin' => '*',
            'Content-Type' => 'text/html',
        ], $out);
    }

    public function testForwardHeaders(): void
    {
        $client = [
            'range' => ['bytes=0-9'],
            'content-type' => ['application/json'],
            'content-length' => ['17'],
            'cookie' => ['pelican_session=secret'],
            'authorization' => ['Bearer x'],
            'x-forwarded-for' => ['203.0.113.7'],
            'x-easypz-proxy' => ['t=1,sig=forged'],
        ];

        self::assertSame(['Range' => 'bytes=0-9'], ProxyRequest::forwardHeaders('GET', $client));
        self::assertSame(['Range' => 'bytes=0-9'], ProxyRequest::forwardHeaders('HEAD', $client));
        self::assertSame(['Range' => 'bytes=0-9', 'Content-Type' => 'application/json', 'Content-Length' => '17'], ProxyRequest::forwardHeaders('post', $client));
        self::assertSame(['GET', 'HEAD', 'POST'], ProxyRequest::MODS_METHODS);
    }

    public function testIsDownload(): void
    {
        self::assertTrue(DownloadSlots::isDownload('download/pack.zip'));
        self::assertTrue(DownloadSlots::isDownload('files/12/a.lua'));
        self::assertFalse(DownloadSlots::isDownload('data.json'));
        self::assertFalse(DownloadSlots::isDownload(''));
        self::assertFalse(DownloadSlots::isDownload('downloads'));
    }

    public function testSlotsCapConcurrentHolders(): void
    {
        $store = new FakeLockStore();
        $slots = new DownloadSlots(fn (string $name) => new FakeLock($store, $name), 5);

        $held = [];
        for ($i = 0; $i < 5; $i++) {
            $lock = $slots->acquire();
            self::assertNotNull($lock, "slot $i");
            $held[] = $lock;
        }
        self::assertNull($slots->acquire(), 'the 6th waits');
        self::assertCount(5, array_unique(array_map(fn (FakeLock $l) => $l->name, $held)));

        $held[2]->release();
        $again = $slots->acquire();
        self::assertNotNull($again);
        self::assertSame($held[2]->name, $again->name);
        self::assertNull($slots->acquire());
    }

    public function testExpiredSlotsAreReclaimed(): void
    {
        $store = new FakeLockStore();
        $slots = new DownloadSlots(fn (string $name) => new FakeLock($store, $name), 1);

        self::assertNotNull($slots->acquire());
        self::assertNull($slots->acquire());
        $store->now += 200; // a worker died without releasing
        self::assertNotNull($slots->acquire());
    }

    public function testZeroSlotsRefusesEverything(): void
    {
        $slots = new DownloadSlots(fn (string $name) => new FakeLock(new FakeLockStore(), $name), 0);
        self::assertNull($slots->acquire());
    }
}

final class FakeLockStore
{
    public int $now = 0;

    /** @var array<string, array{string, int}> */
    public array $locks = [];
}

final class FakeLock
{
    private readonly string $owner;

    public function __construct(private readonly FakeLockStore $store, public readonly string $name)
    {
        $this->owner = bin2hex(random_bytes(4));
    }

    public function get(): bool
    {
        $current = $this->store->locks[$this->name] ?? null;
        if ($current !== null && $current[1] > $this->store->now) {
            return false;
        }
        $this->store->locks[$this->name] = [$this->owner, $this->store->now + 120];

        return true;
    }

    public function release(): bool
    {
        if (($this->store->locks[$this->name][0] ?? null) !== $this->owner) {
            return false;
        }
        unset($this->store->locks[$this->name]);

        return true;
    }
}
