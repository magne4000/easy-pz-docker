<?php

namespace Magne4000\EasyPz\Tests;

use Magne4000\EasyPz\Support\UiAllocations;
use PHPUnit\Framework\TestCase;

final class UiAllocationsTest extends TestCase
{
    public function testSkipsTheSteamPort(): void
    {
        // the stock egg's STEAM_PORT default 16262 on an allocation of its own
        self::assertSame(12, UiAllocations::defaultId([11 => 16262, 12 => 16263, 13 => 16264], '16262'));
        self::assertSame(11, UiAllocations::defaultId([11 => 16261, 12 => 16262], ' 16262 '));
    }

    public function testFirstWhenTheSteamPortIsNotAnAllocation(): void
    {
        self::assertSame(11, UiAllocations::defaultId([11 => 26270, 12 => 26271], '16262'));
        self::assertSame(11, UiAllocations::defaultId([11 => 26270], null));
        self::assertSame(11, UiAllocations::defaultId([11 => 26270], ''));
        self::assertSame(11, UiAllocations::defaultId([11 => 26270], 'abc'));
    }

    public function testOnlyTheSteamPortLeft(): void
    {
        self::assertSame(11, UiAllocations::defaultId([11 => 16262], '16262'));
    }

    public function testNoAllocation(): void
    {
        self::assertNull(UiAllocations::defaultId([], '16262'));
    }
}
