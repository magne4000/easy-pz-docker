<?php

namespace Magne4000\EasyPz\Support;

/**
 * What pzman relies on in the stock Project Zomboid egg. Enable refuses a
 * server whose egg no longer matches, instead of deploying a broken setup.
 */
final class EggCheck
{
    public const STOP_COMMAND = 'quit';

    public const CACHEDIR = '-cachedir=/home/container/.cache';

    public const APP_ID = '380870';

    /**
     * @param  array<array-key, string>  $startupCommands  the egg's startup commands
     * @return list<string> the problems, empty when the egg matches
     */
    public static function problems(?string $stopCommand, array $startupCommands, ?string $appIdDefault): array
    {
        $problems = [];

        if (trim((string) $stopCommand) !== self::STOP_COMMAND) {
            $problems[] = sprintf('the stop command is "%s", not "%s"', (string) $stopCommand, self::STOP_COMMAND);
        }

        $cachedir = array_filter($startupCommands, fn (string $command) => preg_match('/(^|\s)' . preg_quote(self::CACHEDIR, '/') . '(\s|$)/', $command) === 1);
        if ($cachedir === []) {
            $problems[] = sprintf('the startup command does not pass %s', self::CACHEDIR);
        }

        if (trim((string) $appIdDefault) !== self::APP_ID) {
            $problems[] = sprintf('the default SRCDS_APPID is "%s", not "%s"', (string) $appIdDefault, self::APP_ID);
        }

        return $problems;
    }
}
