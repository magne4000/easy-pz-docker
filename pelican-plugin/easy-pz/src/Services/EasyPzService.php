<?php

namespace Magne4000\EasyPz\Services;

use App\Models\Allocation;
use App\Models\Server;
use App\Models\User;
use App\Repositories\Daemon\DaemonFileRepository;
use App\Repositories\Daemon\DaemonServerRepository;
use App\Services\Servers\StartupModificationService;
use Exception;
use Illuminate\Support\Arr;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Http;
use Illuminate\Support\Facades\Log;
use Magne4000\EasyPz\Models\EasyPzServer;
use Magne4000\EasyPz\Support\EggCheck;
use Magne4000\EasyPz\Support\Jwt;
use Magne4000\EasyPz\Support\PzmanEnv;
use Magne4000\EasyPz\Support\Scopes;
use Magne4000\EasyPz\Support\Secret;
use Magne4000\EasyPz\Support\Sha256Sums;
use Magne4000\EasyPz\Support\Urls;
use RuntimeException;

/**
 * Enable, Upgrade, Disable and the per-user tokens of one server.
 */
class EasyPzService
{
    public const FEATURE = 'easy_pz';

    public const BINARY = 'pzman-linux-amd64';

    public const SUMS = 'pzman-SHA256SUMS';

    public const STARTUP = './.easy-pz/pzman';

    public static function eggHasFeature(Server $server): bool
    {
        return in_array(self::FEATURE, $server->egg->features ?? [], true);
    }

    public static function pinnedVersion(): string
    {
        return (string) config('easy-pz.pzman_version');
    }

    /**
     * The scopes of a user on a server: everything for the owner and admins
     * who may update the server (root admins included), the ticked
     * `easy-pz.*` permissions for subusers.
     *
     * @return list<string>
     */
    public static function scopesFor(User $user, Server $server): array
    {
        $full = $server->owner_id === $user->id || $user->can('update', $server);
        $subuser = $server->subusers->where('user_id', $user->id)->first();

        return Scopes::forUser($full, $subuser->permissions ?? []);
    }

    public static function mintToken(User $user, Server $server, EasyPzServer $ez): string
    {
        return Jwt::mint($ez->secret, $server->uuid, $user->uuid, $user->username, self::scopesFor($user, $server));
    }

    /**
     * The UI allocation, still assigned to this server. An owner can detach it
     * (the row stays, `server_id` becomes null) and it may then go to another
     * server: never send anything there.
     */
    public static function uiAllocation(Server $server, EasyPzServer $ez): Allocation
    {
        $allocation = $ez->uiAllocation;
        if (!$allocation || $allocation->server_id !== $server->id) {
            throw new RuntimeException(trans('easy-pz::strings.errors.no_ui_allocation'));
        }

        return $allocation;
    }

    public static function uiUrl(Server $server, EasyPzServer $ez): string
    {
        return Urls::pzmanUi($server->node->fqdn, self::uiAllocation($server, $ez)->port, $ez->ui_url_override);
    }

    /** @return list<string> why the egg does not fit, empty when it does */
    public static function eggProblems(Server $server): array
    {
        $egg = $server->egg;
        $appId = $egg->variables->firstWhere('env_variable', 'SRCDS_APPID');

        return EggCheck::problems($egg->inherit_config_stop, array_values($egg->startup_commands ?? []), $appId?->default_value);
    }

    /** @return array<int, string> allocation id => address, the server's allocations but the primary one */
    public static function uiAllocationOptions(Server $server): array
    {
        return $server->allocations()
            ->where('id', '!=', $server->allocation_id)
            ->orderBy('port')
            ->get()
            ->mapWithKeys(fn (Allocation $allocation) => [$allocation->id => $allocation->address])
            ->all();
    }

    public static function envFile(Server $server, EasyPzServer $ez): string
    {
        $panelUrl = (string) config('app.url');

        return PzmanEnv::render(PzmanEnv::values(
            self::uiAllocation($server, $ez)->port,
            $server->uuid,
            $ez->secret,
            Urls::panel($panelUrl),
            Urls::panelPublic($panelUrl, $server->uuid_short),
            Urls::gameAddress($server->node->fqdn, (int) $server->allocation?->port),
        ));
    }

    /**
     * Deploy pzman into the server and switch its startup to it, then restart it.
     *
     * @throws Exception
     */
    public function enable(Server $server, int $uiAllocationId, ?string $uiUrlOverride): EasyPzServer
    {
        $problems = self::eggProblems($server);
        if ($problems !== []) {
            throw new RuntimeException(trans('easy-pz::strings.errors.egg_mismatch', ['problems' => implode('; ', $problems)]));
        }

        $ez = EasyPzServer::forServer($server) ?? new EasyPzServer(['server_id' => $server->id]);
        if ($ez->isEnabled()) {
            throw new RuntimeException(trans('easy-pz::strings.errors.already_enabled'));
        }

        $this->assignUi($server, $ez, $uiAllocationId, $uiUrlOverride);
        $ez->secret = Secret::generate();

        // Everything that can fail on the network comes before the startup change.
        $files = (new DaemonFileRepository())->setServer($server);
        $files->putContent(PzmanEnv::PATH, self::envFile($server, $ez));
        $version = $this->deployBinary($server, $files);

        DB::transaction(function () use ($server, $ez, $version) {
            // Already pzman (state lost in the panel): Disable falls back to the egg's.
            $ez->previous_startup = $server->startup === self::STARTUP ? null : $server->startup;
            $ez->previous_auto_update = $this->variables($server)['AUTO_UPDATE'] ?? null;
            $ez->deployed_version = $version;
            $ez->enabled_at = now();
            $ez->save();

            $this->setStartup($server, self::STARTUP, '0');
        });

        $this->restart($server);

        return $ez;
    }

    /**
     * Redeploy the pinned pzman; it runs from the next restart.
     *
     * @throws Exception
     */
    public function upgrade(Server $server, EasyPzServer $ez): void
    {
        // pzman.env too: the plugin may have changed what it writes.
        $files = (new DaemonFileRepository())->setServer($server);
        $files->putContent(PzmanEnv::PATH, self::envFile($server, $ez));
        $ez->deployed_version = $this->deployBinary($server, $files);
        $ez->save();
    }

    /**
     * Give the egg its startup command and AUTO_UPDATE back. The world and
     * `.easy-pz/` stay: enabling again resumes.
     *
     * @throws Exception
     */
    public function disable(Server $server, EasyPzServer $ez): void
    {
        $egg = $server->egg;
        $startup = $ez->previous_startup ?: (string) Arr::first($egg->startup_commands ?? []);
        $autoUpdate = $ez->previous_auto_update ?? $egg->variables->firstWhere('env_variable', 'AUTO_UPDATE')?->default_value;

        DB::transaction(function () use ($server, $ez, $startup, $autoUpdate) {
            $this->setStartup($server, $startup, $autoUpdate);
            $ez->enabled_at = null;
            $ez->save();
        });
    }

    /**
     * Change the UI allocation or URL override and rewrite pzman.env; pzman
     * reads it at its next start.
     *
     * @throws Exception
     */
    public function updateUi(Server $server, EasyPzServer $ez, int $uiAllocationId, ?string $uiUrlOverride): void
    {
        $this->assignUi($server, $ez, $uiAllocationId, $uiUrlOverride);
        (new DaemonFileRepository())->setServer($server)->putContent(PzmanEnv::PATH, self::envFile($server, $ez));
        $ez->save();
    }

    public function restart(Server $server): void
    {
        app(DaemonServerRepository::class)->setServer($server)->power('restart');
    }

    private function assignUi(Server $server, EasyPzServer $ez, int $uiAllocationId, ?string $uiUrlOverride): void
    {
        if (!array_key_exists($uiAllocationId, self::uiAllocationOptions($server))) {
            throw new RuntimeException(trans('easy-pz::strings.errors.bad_ui_allocation'));
        }

        $override = trim((string) $uiUrlOverride);
        $ez->ui_allocation_id = $uiAllocationId;
        $ez->ui_url_override = $override === '' ? null : Urls::validated($override);
        $ez->unsetRelation('uiAllocation');
    }

    /**
     * Download the pinned pzman, check it against pzman-SHA256SUMS (Wings' pull
     * cannot) and replace `.easy-pz/pzman`. A running executable cannot be
     * written over (ETXTBSY), and Wings does not rename onto an existing file:
     * upload `pzman.next`, make it executable, unlink the old binary (the
     * running process keeps its copy), rename.
     *
     * @return string the deployed version
     *
     * @throws Exception
     */
    private function deployBinary(Server $server, DaemonFileRepository $files): string
    {
        set_time_limit(600);

        $version = self::pinnedVersion();
        $base = Urls::pzmanDownloadBase($version, config('easy-pz.pzman_base_url'));

        $sums = Http::timeout(30)->connectTimeout(10)->get("$base/" . self::SUMS)->throw()->body();
        $expected = Sha256Sums::find($sums, self::BINARY)
            ?? throw new RuntimeException(trans('easy-pz::strings.errors.no_checksum', ['url' => "$base/" . self::SUMS]));

        $tmp = tempnam(sys_get_temp_dir(), 'easy-pz-');
        try {
            Http::timeout(600)->connectTimeout(10)->sink($tmp)->get("$base/" . self::BINARY)->throw();
            $actual = (string) hash_file('sha256', $tmp);
            if (!Sha256Sums::matches($expected, $actual)) {
                throw new RuntimeException(trans('easy-pz::strings.errors.checksum_mismatch', ['expected' => $expected, 'actual' => $actual]));
            }

            $files->putContent('.easy-pz/pzman.next', (string) file_get_contents($tmp));
        } finally {
            @unlink($tmp);
        }

        $files->chmodFiles('.easy-pz', [['file' => 'pzman.next', 'mode' => '755']]);
        try {
            $files->deleteFiles('.easy-pz', ['pzman']);
        } catch (Exception $exception) {
            Log::info('easy-pz: no previous pzman binary to remove', ['server' => $server->uuid, 'error' => $exception->getMessage()]);
        }
        $files->renameFiles('.easy-pz', [['from' => 'pzman.next', 'to' => 'pzman']]);

        return $version;
    }

    /** @return array<string, string> the server's current variable values */
    private function variables(Server $server): array
    {
        $values = [];
        foreach ($server->variables()->get() as $variable) {
            $values[$variable->env_variable] = (string) ($variable->server_value ?? $variable->default_value ?? '');
        }

        return $values;
    }

    /**
     * StartupModificationService rewrites every variable of the egg (missing
     * ones become empty) and resets skip_scripts unless given: pass them all.
     *
     * @throws \Throwable
     */
    private function setStartup(Server $server, string $startup, ?string $autoUpdate): void
    {
        $environment = $this->variables($server);
        if ($autoUpdate !== null && array_key_exists('AUTO_UPDATE', $environment)) {
            $environment['AUTO_UPDATE'] = $autoUpdate;
        }

        app(StartupModificationService::class)
            ->setUserLevel(User::USER_LEVEL_ADMIN)
            ->handle($server, [
                'startup' => $startup,
                'skip_scripts' => (bool) $server->skip_scripts,
                'environment' => $environment,
            ]);
    }
}
