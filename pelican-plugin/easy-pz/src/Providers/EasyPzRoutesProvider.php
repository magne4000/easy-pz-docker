<?php

namespace Magne4000\EasyPz\Providers;

use Illuminate\Foundation\Support\Providers\RouteServiceProvider;
use Illuminate\Support\Facades\Route;
use Magne4000\EasyPz\Http\Controllers\HandoffController;
use Magne4000\EasyPz\Http\Controllers\PublicProxyController;
use Magne4000\EasyPz\Support\ProxyRequest;

class EasyPzRoutesProvider extends RouteServiceProvider
{
    public function boot(): void
    {
        $this->routes(function () {
            Route::middleware(['web', 'auth'])
                ->get('/easy-pz/{uuidShort}/open', HandoffController::class)
                ->name('easy-pz.open');

            // Public mod page and launcher endpoints, no session (contract
            // "public mod page through the panel"). POST: the launcher's
            // `files/<id>` (partial updates).
            Route::match(ProxyRequest::MODS_METHODS, '/easy-pz/{uuidShort}/mods/{token}/{rest?}', PublicProxyController::class)
                ->where('rest', '.*')
                ->name('easy-pz.mods');
            Route::get('/easy-pz/{uuidShort}/assets/{rest}', PublicProxyController::class)
                ->where('rest', '.*')
                ->name('easy-pz.assets');
        });
    }
}
