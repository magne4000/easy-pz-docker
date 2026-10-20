<?php

namespace Magne4000\EasyPz\Support;

/**
 * The `easy-pz` subuser permission group and the pzman scopes it grants
 * (contract "permissions": `easy-pz.<scope>` grants `<scope>`).
 */
final class Scopes
{
    public const GROUP = 'easy-pz';

    public const READ = 'read';

    public const CONTROL = 'control';

    public const CONSOLE = 'console';

    public const MODS = 'mods';

    public const CONFIG = 'config';

    public const BACKUPS = 'backups';

    public const SCHEDULES = 'schedules';

    public const SETTINGS = 'settings';

    public const ALL = [
        self::READ,
        self::CONTROL,
        self::CONSOLE,
        self::MODS,
        self::CONFIG,
        self::BACKUPS,
        self::SCHEDULES,
        self::SETTINGS,
    ];

    public static function permission(string $scope): string
    {
        return self::GROUP . '.' . $scope;
    }

    /**
     * The scopes of a user on one server.
     *
     * @param  bool  $fullAccess  the server owner or a root admin
     * @param  list<string>  $subuserPermissions  the user's subuser permissions on the server, if any
     * @return list<string>
     */
    public static function forUser(bool $fullAccess, array $subuserPermissions): array
    {
        if ($fullAccess) {
            return self::ALL;
        }

        return array_values(array_filter(self::ALL, fn (string $scope) => in_array(self::permission($scope), $subuserPermissions, true)));
    }
}
