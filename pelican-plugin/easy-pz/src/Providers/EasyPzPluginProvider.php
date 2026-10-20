<?php

namespace Magne4000\EasyPz\Providers;

use App\Models\Subuser;
use Illuminate\Support\ServiceProvider;
use Magne4000\EasyPz\Support\Scopes;

class EasyPzPluginProvider extends ServiceProvider
{
    public function register(): void
    {
        // Labels: easy-pz::permissions.easy-pz_title, …_desc and …_<scope>.
        Subuser::registerCustomPermissions(Scopes::GROUP, Scopes::ALL, 'easy-pz::permissions', 'tabler-skull', false);
    }
}
