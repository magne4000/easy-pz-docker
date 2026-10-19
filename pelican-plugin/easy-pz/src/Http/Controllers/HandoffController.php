<?php

namespace Magne4000\EasyPz\Http\Controllers;

use App\Models\Server;
use Illuminate\Http\RedirectResponse;
use Illuminate\Routing\Controller;
use Magne4000\EasyPz\Models\EasyPzServer;
use Magne4000\EasyPz\Services\EasyPzService;
use Magne4000\EasyPz\Support\Scopes;

/**
 * "Open easy-pz": a single-use token in the fragment of pzman's
 * `/auth/pelican` page, which trades it for a pzman session.
 */
class HandoffController extends Controller
{
    public function __invoke(string $uuidShort): RedirectResponse
    {
        $server = Server::query()->where('uuid_short', $uuidShort)->first();
        abort_if(!$server, 404);

        $user = user();
        abort_unless($user && $user->can(Scopes::permission(Scopes::READ), $server) && EasyPzService::eggHasFeature($server), 404);

        $ez = EasyPzServer::enabledFor($server);
        abort_if(!$ez, 404);

        $token = EasyPzService::mintToken($user, $server, $ez);

        return redirect()->away(EasyPzService::uiUrl($server, $ez) . '/auth/pelican#t=' . $token)
            ->header('Cache-Control', 'no-store')
            ->header('Referrer-Policy', 'no-referrer');
    }
}
