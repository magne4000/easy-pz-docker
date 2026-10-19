<?php

namespace Magne4000\EasyPz\Models;

use App\Models\Allocation;
use App\Models\Server;
use Illuminate\Database\Eloquent\Model;
use Illuminate\Database\Eloquent\Relations\BelongsTo;
use Illuminate\Support\Carbon;

/**
 * easy-pz state of one server.
 *
 * @property int $id
 * @property int $server_id
 * @property string $secret PZMAN_PELICAN_SECRET
 * @property int|null $ui_allocation_id
 * @property string|null $ui_url_override
 * @property string|null $deployed_version
 * @property string|null $previous_startup the startup command before Enable
 * @property string|null $previous_auto_update AUTO_UPDATE before Enable
 * @property Carbon|null $enabled_at null while disabled
 * @property-read Server $server
 * @property-read Allocation|null $uiAllocation
 */
class EasyPzServer extends Model
{
    protected $table = 'easy_pz_servers';

    protected $guarded = ['id', 'created_at', 'updated_at'];

    protected $hidden = ['secret'];

    protected function casts(): array
    {
        return [
            'secret' => 'encrypted',
            'enabled_at' => 'datetime',
        ];
    }

    public static function forServer(Server $server): ?self
    {
        return self::query()->where('server_id', $server->id)->first();
    }

    public static function enabledFor(Server $server): ?self
    {
        return self::query()->where('server_id', $server->id)->whereNotNull('enabled_at')->first();
    }

    public function isEnabled(): bool
    {
        return $this->enabled_at !== null;
    }

    /** @return BelongsTo<Server, $this> */
    public function server(): BelongsTo
    {
        return $this->belongsTo(Server::class);
    }

    /** @return BelongsTo<Allocation, $this> */
    public function uiAllocation(): BelongsTo
    {
        return $this->belongsTo(Allocation::class, 'ui_allocation_id');
    }
}
