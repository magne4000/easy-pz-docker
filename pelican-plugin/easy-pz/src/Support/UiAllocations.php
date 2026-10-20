<?php

namespace Magne4000\EasyPz\Support;

/** Which of the server's allocations the UI port picker suggests. */
final class UiAllocations
{
    /**
     * The first allocation whose port is not the game's Steam port (the egg's
     * STEAM_PORT variable, which lives on an allocation too), or the first one
     * when every allocation is taken by it.
     *
     * @param  array<int, int>  $ports  allocation id => port, in display order, the primary one excluded
     */
    public static function defaultId(array $ports, ?string $steamPort): ?int
    {
        $steam = $steamPort !== null && ctype_digit(trim($steamPort)) ? (int) trim($steamPort) : null;
        foreach ($ports as $id => $port) {
            if ($port !== $steam) {
                return $id;
            }
        }

        return array_key_first($ports);
    }
}
