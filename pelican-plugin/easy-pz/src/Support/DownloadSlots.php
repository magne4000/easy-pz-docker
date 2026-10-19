<?php

namespace Magne4000\EasyPz\Support;

use Closure;

/**
 * A counting semaphore over named locks: at most `$max` proxied downloads run
 * at once across the panel (each holds a PHP-FPM worker). The locks come from
 * a factory so the panel can back them with its cache store.
 *
 * A lock is any object with `get(): bool` (non-blocking acquire),
 * `release(): bool` and, optionally, `refresh(): bool` (extend its TTL).
 */
final class DownloadSlots
{
    public const PREFIX = 'easy-pz:download-slot:';

    /** @param  Closure(string): object  $lockFactory  builds the lock of one slot name */
    public function __construct(private readonly Closure $lockFactory, private readonly int $max) {}

    /** The held lock of a free slot, or null when all are taken. */
    public function acquire(): ?object
    {
        // Start at a random slot: under load, requests don't all race for slot 0.
        $max = max(0, $this->max);
        $start = $max > 0 ? random_int(0, $max - 1) : 0;
        for ($i = 0; $i < $max; $i++) {
            $lock = ($this->lockFactory)(self::PREFIX . (($start + $i) % $max));
            if ($lock->get()) {
                return $lock;
            }
        }

        return null;
    }

    /** Download requests: the paths under `download/` and `files/` of the mod page. */
    public static function isDownload(string $rest): bool
    {
        $rest = ltrim($rest, '/');

        return str_starts_with($rest, 'download/') || str_starts_with($rest, 'files/');
    }
}
