<?php

namespace Magne4000\EasyPz\Tests;

use Magne4000\EasyPz\Support\Version;
use PHPUnit\Framework\TestCase;

final class VersionTest extends TestCase
{
    public function testSameIgnoresTheVPrefix(): void
    {
        self::assertTrue(Version::same('v1.2.3', '1.2.3'));
        self::assertTrue(Version::same('1.2.3', 'v1.2.3'));
        self::assertTrue(Version::same('1.2.3', '1.2.3'));
        self::assertFalse(Version::same('v1.2.3', '1.2.4'));
        self::assertFalse(Version::same(null, '1.2.3'));
        self::assertFalse(Version::same('1.2.3', null));
    }
}
