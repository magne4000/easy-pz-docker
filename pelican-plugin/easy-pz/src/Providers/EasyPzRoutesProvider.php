<?php

namespace Magne4000\EasyPz\Providers;

use App\Http\Middleware\RequireTwoFactorAuthentication;
use Illuminate\Foundation\Support\Providers\RouteServiceProvider;
use Illuminate\Session\Middleware\AuthenticateSession;
use Illuminate\Support\Facades\Route;
use Magne4000\EasyPz\Http\Controllers\HandoffController;
use Magne4000\EasyPz\Http\Controllers\PublicProxyController;
use Magne4000\EasyPz\Support\ProxyRequest;

class EasyPzRoutesProvider extends RouteServiceProvider
{
    public function boot(): void
    {
        $this->routes(function () {
            // The panel pages' own checks (Pelican's PanelProvider): a session
            // ended by a password change, or a user who must set up 2FA first,
            // gets no pzman session either.
            Route::middleware(['web', 'auth', AuthenticateSession::class, RequireTwoFactorAuthentication::class])
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
