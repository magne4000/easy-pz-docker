<?php

namespace Magne4000\EasyPz\Filament\Server\Pages;

use App\Models\Server;
use Exception;
use Filament\Actions\Action;
use Filament\Actions\ActionGroup;
use Filament\Facades\Filament;
use Filament\Forms\Components\Select;
use Filament\Forms\Components\TextInput;
use Filament\Forms\Components\Toggle;
use Filament\Infolists\Components\TextEntry;
use Filament\Notifications\Notification;
use Filament\Pages\Page;
use Filament\Schemas\Components\Callout;
use Filament\Schemas\Components\Component;
use Filament\Schemas\Components\Section;
use Filament\Schemas\Schema;
use Illuminate\Support\Carbon;
use Magne4000\EasyPz\Models\EasyPzServer;
use Magne4000\EasyPz\Services\EasyPzService;
use Magne4000\EasyPz\Services\PzmanClient;
use Magne4000\EasyPz\Services\PzmanException;
use Magne4000\EasyPz\Support\Scopes;
use Magne4000\EasyPz\Support\Urls;
use Magne4000\EasyPz\Support\Version;

class EasyPzPage extends Page
{
    protected static string|\BackedEnum|null $navigationIcon = 'tabler-skull';

    protected static ?string $slug = 'easy-pz';

    protected static ?int $navigationSort = 35;

    /** @var array<string, mixed>|null GET /api/system */
    public ?array $system = null;

    /** @var array<string, mixed>|null GET /api/server/status */
    public ?array $serverStatus = null;

    /** @var array<string, mixed>|null GET /api/updates */
    public ?array $updates = null;

    public ?string $pzmanError = null;

    public bool $unreachable = false;

    public static function canAccess(): bool
    {
        /** @var Server $server */
        $server = Filament::getTenant();

        return !$server->isInConflictState()
            && parent::canAccess()
            && EasyPzService::eggHasFeature($server)
            && (bool) user()?->can(Scopes::permission(Scopes::READ), $server);
    }

    public static function getNavigationLabel(): string
    {
        return trans('easy-pz::strings.page.title');
    }

    public function getTitle(): string
    {
        return static::getNavigationLabel();
    }

    public function mount(): void
    {
        $this->loadStatus();
    }

    private function server(): Server
    {
        /** @var Server $server */
        $server = Filament::getTenant();

        return $server;
    }

    private function ez(): ?EasyPzServer
    {
        return EasyPzServer::forServer($this->server());
    }

    private function enabled(): ?EasyPzServer
    {
        $ez = $this->ez();

        return $ez?->isEnabled() ? $ez : null;
    }

    private static function isRootAdmin(): bool
    {
        return (bool) user()?->isRootAdmin();
    }

    private function can(string $scope): bool
    {
        return (bool) user()?->can(Scopes::permission($scope), $this->server());
    }

    private function client(EasyPzServer $ez): PzmanClient
    {
        return new PzmanClient($this->server(), $ez, user());
    }

    public function loadStatus(): void
    {
        $this->system = $this->serverStatus = $this->updates = null;
        $this->pzmanError = null;
        $this->unreachable = false;

        $ez = $this->enabled();
        if (!$ez) {
            return;
        }

        $client = $this->client($ez);
        try {
            $this->system = $client->get('/system');
            $this->serverStatus = $client->get('/server/status');
            $this->updates = $client->get('/updates');
        } catch (PzmanException $exception) {
            $this->pzmanError = $exception->getMessage();
            $this->unreachable = $exception->unreachable;
        }

        // Resolving an action builds the page schema before the action runs; the
        // cached one still holds the previous status.
        $this->cacheSchema('content', null);
    }

    public function content(Schema $schema): Schema
    {
        $ez = $this->enabled();

        return $schema->components($ez ? $this->enabledComponents($ez) : $this->disabledComponents());
    }

    /** @return Component[] */
    private function disabledComponents(): array
    {
        $server = $this->server();
        $components = [
            Callout::make(trans('easy-pz::strings.page.not_enabled'))
                ->description(trans(self::isRootAdmin() ? 'easy-pz::strings.page.not_enabled_admin' : 'easy-pz::strings.page.not_enabled_user'))
                ->info(),
        ];

        if (!self::isRootAdmin()) {
            return $components;
        }

        $problems = EasyPzService::eggProblems($server);
        if ($problems !== []) {
            $components[] = Callout::make(trans('easy-pz::strings.page.egg_mismatch'))
                ->description(implode('; ', $problems))
                ->danger();
        }
        if (EasyPzService::uiAllocationOptions($server) === []) {
            $components[] = Callout::make(trans('easy-pz::strings.page.no_allocation'))
                ->description(trans('easy-pz::strings.page.no_allocation_help'))
                ->warning();
        }
        if ((int) $server->memory === 0) {
            $components[] = Callout::make(trans('easy-pz::strings.page.no_memory_limit'))
                ->description(trans('easy-pz::strings.page.no_memory_limit_help'))
                ->warning();
        }

        return $components;
    }

    /** @return Component[] */
    private function enabledComponents(EasyPzServer $ez): array
    {
        $components = [];

        if ($this->pzmanError !== null) {
            $components[] = Callout::make(trans($this->unreachable ? 'easy-pz::strings.page.unreachable' : 'easy-pz::strings.page.pzman_error'))
                ->description($this->pzmanError)
                ->danger();
        }

        $pinned = EasyPzService::pinnedVersion();
        if (self::isRootAdmin() && !Version::same($ez->deployed_version, $pinned)) {
            $components[] = Callout::make(trans('easy-pz::strings.page.upgrade_available', ['version' => $pinned]))
                ->description(trans('easy-pz::strings.page.upgrade_available_help', ['deployed' => $ez->deployed_version ?? '?']))
                ->info();
        }

        $running = $this->system['version'] ?? null;
        if (self::isRootAdmin() && $running !== null && $ez->deployed_version !== null && !Version::same($running, $ez->deployed_version)) {
            $components[] = Callout::make(trans('easy-pz::strings.page.restart_to_apply', ['running' => $running, 'deployed' => $ez->deployed_version]))
                ->warning();
        }

        $status = $this->serverStatus ?? [];
        $updates = $this->updates ?? [];
        $window = $updates['window'] ?? [];
        $players = $status['players'] ?? [];

        $components[] = Section::make(trans('easy-pz::strings.status.server'))
            ->columns(4)
            ->schema([
                $this->entry('state', $status['state'] ?? null)->badge(),
                $this->entry('availability', $status['availability'] ?? null)->badge()
                    ->color(fn ($state) => match ($state) {
                        'available' => 'success',
                        'restarting' => 'warning',
                        'unavailable' => 'danger',
                        default => 'gray',
                    }),
                $this->entry('players', isset($players['count']) ? (string) $players['count'] : null)
                    ->tooltip(implode(', ', $players['names'] ?? [])),
                $this->entry('operation', match (true) {
                    $this->serverStatus === null => null,
                    ($status['operation'] ?? '') !== '' => $status['operation'] . $this->since($status['operationSince'] ?? null),
                    default => trans('easy-pz::strings.status.idle'),
                }),
                $this->entry('build', $status['buildId'] ?? null),
                $this->entry('branch', ($status['branch'] ?? '') !== '' ? $status['branch'] : null),
                $this->entry('uptime', isset($status['startedAt']) && ($status['state'] ?? '') === 'running' ? $this->since($status['startedAt'], false) : null),
            ]);

        $components[] = Section::make(trans('easy-pz::strings.status.updates'))
            ->columns(4)
            ->schema([
                $this->entry('installed_build', $updates['installedBuild'] ?? null),
                $this->entry('latest_build', $updates['latestBuild'] ?? null)
                    ->badge(fn () => (bool) ($updates['gameUpdateAvailable'] ?? false))
                    ->color('warning'),
                $this->entry('game_locked', isset($updates['gameLocked']) ? trans($updates['gameLocked'] ? 'easy-pz::strings.status.yes' : 'easy-pz::strings.status.no') : null),
                $this->entry('last_checked', $this->since($updates['lastCheckedAt'] ?? null, false) ?: null),
                $this->entry('window', $this->windowText($window))->badge()
                    ->color(fn () => match ($window['state'] ?? 'idle') {
                        'idle' => 'gray',
                        'failed' => 'danger',
                        default => 'warning',
                    })
                    ->columnSpan(2),
                $this->entry('mod_updates', ($updates['modUpdates'] ?? []) === [] ? null : implode(', ', $updates['modUpdates']))
                    ->columnSpan(2),
                $this->entry('check_error', ($updates['checkError'] ?? '') !== '' ? $updates['checkError'] : null)
                    ->visible(($updates['checkError'] ?? '') !== '')
                    ->color('danger')
                    ->columnSpanFull(),
            ]);

        $components[] = Section::make(trans('easy-pz::strings.status.pzman'))
            ->columns(4)
            ->collapsible()
            ->schema([
                $this->entry('version', $running),
                $this->entry('host', $this->system['host'] ?? null),
                $this->entry('deployed', $ez->deployed_version),
                $this->entry('ui_url', $this->uiUrl($ez)),
            ]);

        return $components;
    }

    private function entry(string $name, ?string $state): TextEntry
    {
        return TextEntry::make($name)
            ->label(trans("easy-pz::strings.status.$name"))
            ->state($state ?? '—');
    }

    private function uiUrl(EasyPzServer $ez): ?string
    {
        try {
            return EasyPzService::uiUrl($this->server(), $ez);
        } catch (Exception) {
            return null;
        }
    }

    private function since(?string $time, bool $parenthesized = true): string
    {
        if (!$time) {
            return '';
        }
        try {
            $text = Carbon::parse($time)->diffForHumans();
        } catch (Exception) {
            return '';
        }

        return $parenthesized ? " ($text)" : $text;
    }

    /** @param  array<string, mixed>  $window */
    private function windowText(array $window): ?string
    {
        $state = $window['state'] ?? null;
        if ($state === null) {
            return null;
        }
        $text = $state;
        if (($window['reason'] ?? '') !== '') {
            $text .= ': ' . $window['reason'];
        }
        if (!empty($window['forceAt']) && $state === 'waiting') {
            $text .= ' — ' . trans('easy-pz::strings.status.forced', ['when' => $this->since($window['forceAt'], false)]);
        }
        if (($window['error'] ?? '') !== '') {
            $text .= ' — ' . $window['error'];
        }

        return $text;
    }

    protected function getHeaderActions(): array
    {
        if (!$this->enabled()) {
            return [$this->enableAction()];
        }

        $windowOpen = fn () => !in_array($this->updates['window']['state'] ?? 'idle', ['idle', 'failed'], true);

        return [
            Action::make('open')
                ->label(trans('easy-pz::strings.actions.open'))
                ->tooltip(trans('easy-pz::strings.actions.open'))
                ->icon('tabler-external-link')
                ->url(fn () => route('easy-pz.open', ['uuidShort' => $this->server()->uuid_short]), true),
            Action::make('refresh')
                ->label(trans('easy-pz::strings.actions.refresh'))
                ->tooltip(trans('easy-pz::strings.actions.refresh'))
                ->icon('tabler-refresh')
                ->color('gray')
                ->action(fn () => $this->loadStatus()),
            ActionGroup::make([
                $this->pzmanAction('check_updates', Scopes::CONTROL, 'tabler-cloud-search', fn () => $this->callPzman('/updates/check')),
                $this->pzmanAction('apply_update', Scopes::CONTROL, 'tabler-player-play', fn (array $data) => $this->callPzman('/updates/apply', ['force' => (bool) ($data['force'] ?? false)]))
                    ->hidden($windowOpen)
                    ->schema([
                        Toggle::make('force')->label(trans('easy-pz::strings.actions.force'))->helperText(trans('easy-pz::strings.actions.force_help')),
                    ]),
                $this->pzmanAction('cancel_update', Scopes::CONTROL, 'tabler-player-stop', fn () => $this->callPzman('/updates/cancel'))
                    ->visible(fn () => $this->can(Scopes::CONTROL) && $windowOpen())
                    ->requiresConfirmation(),
                $this->pzmanAction('restart', Scopes::CONTROL, 'tabler-reload', fn (array $data) => $this->callPzman('/server/restart', array_filter([
                    'countdownMinutes' => (int) ($data['countdownMinutes'] ?? 0),
                    'message' => trim((string) ($data['message'] ?? '')),
                ], fn ($v) => $v !== '')))
                    ->schema([
                        TextInput::make('countdownMinutes')->label(trans('easy-pz::strings.actions.countdown'))->integer()->minValue(0)->maxValue(240)->default(5)->required(),
                        TextInput::make('message')->label(trans('easy-pz::strings.actions.countdown_message'))->placeholder('Server restart')->maxLength(200),
                    ]),
                $this->pzmanAction('save', Scopes::CONTROL, 'tabler-device-floppy', fn () => $this->callPzman('/server/save')),
                $this->pzmanAction('broadcast', Scopes::CONTROL, 'tabler-speakerphone', fn (array $data) => $this->callPzman('/server/broadcast', ['message' => (string) $data['message']]))
                    ->schema([
                        TextInput::make('message')->label(trans('easy-pz::strings.actions.message'))->required()->maxLength(500),
                    ]),
                $this->pzmanAction('cancel_operation', Scopes::CONTROL, 'tabler-hand-stop', fn () => $this->callPzman('/server/cancel'))
                    ->visible(fn () => $this->can(Scopes::CONTROL) && ($this->serverStatus['operation'] ?? '') !== '')
                    ->requiresConfirmation(),
                $this->pzmanAction('backup', Scopes::BACKUPS, 'tabler-archive', fn (array $data) => $this->callPzman('/backups', array_filter([
                    'force' => true,
                    'note' => trim((string) ($data['note'] ?? '')),
                ], fn ($v) => $v !== '')))
                    ->schema([
                        TextInput::make('note')->label(trans('easy-pz::strings.actions.note'))->maxLength(200),
                    ]),
            ])
                ->label(trans('easy-pz::strings.actions.server'))
                ->tooltip(trans('easy-pz::strings.actions.server'))
                ->icon('tabler-adjustments')
                ->button()
                ->visible(fn () => $this->can(Scopes::CONTROL) || $this->can(Scopes::BACKUPS)),
            ActionGroup::make([
                $this->upgradeAction(),
                Action::make('restart_container')
                    ->grouped()
                    ->label(trans('easy-pz::strings.actions.restart_container'))
                    ->icon('tabler-reload')
                    ->requiresConfirmation()
                    ->modalDescription(trans('easy-pz::strings.actions.restart_container_help'))
                    ->action(function () {
                        $this->adminOp(fn (EasyPzService $service, Server $server) => $service->restart($server), 'restarted');
                    }),
                $this->editUiAction(),
                $this->disableAction(),
            ])
                ->label(trans('easy-pz::strings.actions.admin'))
                ->tooltip(trans('easy-pz::strings.actions.admin'))
                ->icon('tabler-shield-cog')
                ->button()
                ->color('gray')
                ->visible(fn () => self::isRootAdmin()),
        ];
    }

    private function pzmanAction(string $name, string $scope, string $icon, \Closure $call): Action
    {
        // grouped(): Pelican's icon-button preference would otherwise turn
        // dropdown items into bare icons.
        return Action::make($name)
            ->grouped()
            ->label(trans("easy-pz::strings.actions.$name"))
            ->icon($icon)
            ->visible(fn () => $this->can($scope))
            ->disabled(fn () => $this->unreachable)
            ->action($call);
    }

    /** @param  array<string, mixed>|null  $body */
    private function callPzman(string $path, ?array $body = null): void
    {
        $ez = $this->enabled();
        if (!$ez) {
            return;
        }

        try {
            $result = $this->client($ez)->post($path, $body);
            $message = $result['message'] ?? null;
            $accepted = $result['accepted'] ?? $result['cancelled'] ?? true;

            Notification::make()
                ->title(trans($accepted ? 'easy-pz::strings.notifications.done' : 'easy-pz::strings.notifications.refused'))
                ->body(is_string($message) && $message !== '' ? $message : null)
                ->status($accepted ? 'success' : 'warning')
                ->send();
        } catch (PzmanException $exception) {
            Notification::make()
                ->title(trans('easy-pz::strings.notifications.failed'))
                ->body($exception->getMessage())
                ->danger()
                ->send();
        }

        $this->loadStatus();
    }

    /** @return array<int, string> */
    private function allocationOptions(): array
    {
        return EasyPzService::uiAllocationOptions($this->server());
    }

    /** @return Component[] */
    private function uiFields(?EasyPzServer $ez = null): array
    {
        return [
            Select::make('ui_allocation_id')
                ->label(trans('easy-pz::strings.enable.allocation'))
                ->helperText(trans('easy-pz::strings.enable.allocation_help'))
                ->options(fn () => $this->allocationOptions())
                ->default($ez?->ui_allocation_id ?? array_key_first($this->allocationOptions()))
                ->required(),
            TextInput::make('ui_url_override')
                ->label(trans('easy-pz::strings.enable.override'))
                ->helperText(trans('easy-pz::strings.enable.override_help'))
                ->default($ez?->ui_url_override)
                ->rule(fn () => function (string $attribute, mixed $value, \Closure $fail) {
                    try {
                        Urls::validated((string) $value);
                    } catch (\InvalidArgumentException $exception) {
                        $fail($exception->getMessage());
                    }
                }),
        ];
    }

    private function enableAction(): Action
    {
        return Action::make('enable')
            ->label(trans('easy-pz::strings.enable.action'))
            ->tooltip(trans('easy-pz::strings.enable.action'))
            ->icon('tabler-power')
            ->visible(fn () => self::isRootAdmin())
            ->disabled(fn () => EasyPzService::eggProblems($this->server()) !== [] || $this->allocationOptions() === [])
            ->modalHeading(trans('easy-pz::strings.enable.heading'))
            ->modalDescription(fn () => trans('easy-pz::strings.enable.description', ['version' => EasyPzService::pinnedVersion()])
                . ((int) $this->server()->memory === 0 ? ' ' . trans('easy-pz::strings.page.no_memory_limit_help') : ''))
            ->modalSubmitActionLabel(trans('easy-pz::strings.enable.action'))
            ->schema(fn () => $this->uiFields($this->ez()))
            ->action(function (array $data) {
                $this->adminOp(fn (EasyPzService $service, Server $server) => $service->enable($server, (int) $data['ui_allocation_id'], $data['ui_url_override'] ?? null), 'enabled');
            });
    }

    private function upgradeAction(): Action
    {
        return Action::make('upgrade')
            ->grouped()
            ->label(fn () => trans('easy-pz::strings.actions.upgrade', ['version' => EasyPzService::pinnedVersion()]))
            ->icon('tabler-arrow-big-up-lines')
            ->visible(fn () => !Version::same($this->enabled()?->deployed_version, EasyPzService::pinnedVersion()))
            ->modalDescription(trans('easy-pz::strings.actions.upgrade_help'))
            ->schema([
                Toggle::make('restart')->label(trans('easy-pz::strings.actions.restart_now'))->default(true),
            ])
            ->action(function (array $data) {
                $this->adminOp(function (EasyPzService $service, Server $server, EasyPzServer $ez) use ($data) {
                    $service->upgrade($server, $ez);
                    if ($data['restart'] ?? false) {
                        $service->restart($server);
                    }
                }, 'upgraded');
            });
    }

    private function editUiAction(): Action
    {
        return Action::make('edit_ui')
            ->grouped()
            ->label(trans('easy-pz::strings.actions.edit_ui'))
            ->icon('tabler-plug-connected')
            ->modalDescription(trans('easy-pz::strings.actions.edit_ui_help'))
            ->schema(fn () => [
                ...$this->uiFields($this->ez()),
                Toggle::make('restart')->label(trans('easy-pz::strings.actions.restart_now'))->default(true),
            ])
            ->action(function (array $data) {
                $this->adminOp(function (EasyPzService $service, Server $server, EasyPzServer $ez) use ($data) {
                    $service->updateUi($server, $ez, (int) $data['ui_allocation_id'], $data['ui_url_override'] ?? null);
                    if ($data['restart'] ?? false) {
                        $service->restart($server);
                    }
                }, 'saved');
            });
    }

    private function disableAction(): Action
    {
        return Action::make('disable')
            ->grouped()
            ->label(trans('easy-pz::strings.actions.disable'))
            ->icon('tabler-power')
            ->color('danger')
            ->requiresConfirmation()
            ->modalDescription(trans('easy-pz::strings.actions.disable_help'))
            ->schema([
                Toggle::make('restart')->label(trans('easy-pz::strings.actions.restart_now'))->default(true),
            ])
            ->action(function (array $data) {
                $this->adminOp(function (EasyPzService $service, Server $server, EasyPzServer $ez) use ($data) {
                    $service->disable($server, $ez);
                    if ($data['restart'] ?? false) {
                        $service->restart($server);
                    }
                }, 'disabled');
            });
    }

    /** Root-admin operations: re-checked here, Livewire calls are requests of their own. */
    private function adminOp(\Closure $op, string $done): void
    {
        abort_unless(self::isRootAdmin(), 403);

        try {
            $server = $this->server();
            $op(app(EasyPzService::class), $server, $this->ez());

            Notification::make()->title(trans("easy-pz::strings.notifications.$done"))->success()->send();

            // The header actions depend on the state that just changed.
            $this->redirect(static::getUrl());

            return;
        } catch (Exception $exception) {
            report($exception);

            Notification::make()
                ->title(trans('easy-pz::strings.notifications.failed'))
                ->body($exception->getMessage())
                ->danger()
                ->persistent()
                ->send();
        }

        $this->loadStatus();
    }
}
