<?php

namespace Magne4000\EasyPz;

use App\Contracts\Plugins\HasPluginSettings;
use App\Traits\EnvironmentWriterTrait;
use Filament\Contracts\Plugin;
use Filament\Forms\Components\TextInput;
use Filament\Notifications\Notification;
use Filament\Panel;
use Magne4000\EasyPz\Support\Urls;

class EasyPzPlugin implements HasPluginSettings, Plugin
{
    use EnvironmentWriterTrait;

    public function getId(): string
    {
        return 'easy-pz';
    }

    public function register(Panel $panel): void
    {
        $id = str($panel->getId())->title();

        $panel->discoverPages(plugin_path($this->getId(), "src/Filament/$id/Pages"), "Magne4000\\EasyPz\\Filament\\$id\\Pages");
    }

    public function boot(Panel $panel): void {}

    public function getSettingsFormData(): array
    {
        return [
            'pzman_base_url' => config('easy-pz.pzman_base_url'),
            'max_downloads' => config('easy-pz.max_downloads'),
        ];
    }

    public function getSettingsForm(): array
    {
        return [
            TextInput::make('pzman_base_url')
                ->label(trans('easy-pz::strings.settings.pzman_base_url'))
                ->helperText(trans('easy-pz::strings.settings.pzman_base_url_help', ['version' => config('easy-pz.pzman_version')]))
                ->url()
                ->rule(function () {
                    return function (string $attribute, mixed $value, \Closure $fail) {
                        try {
                            Urls::validated((string) $value);
                        } catch (\InvalidArgumentException $exception) {
                            $fail($exception->getMessage());
                        }
                    };
                })
                ->nullable(),
            TextInput::make('max_downloads')
                ->label(trans('easy-pz::strings.settings.max_downloads'))
                ->helperText(trans('easy-pz::strings.settings.max_downloads_help'))
                ->integer()
                ->minValue(1)
                ->maxValue(14)
                ->required()
                ->default(5),
        ];
    }

    public function saveSettings(array $data): void
    {
        $this->writeToEnvironment([
            'EASYPZ_PZMAN_BASE_URL' => trim((string) ($data['pzman_base_url'] ?? '')),
            'EASYPZ_MAX_DOWNLOADS' => (int) $data['max_downloads'],
        ]);

        Notification::make()
            ->title(trans('easy-pz::strings.settings.saved'))
            ->success()
            ->send();
    }
}
