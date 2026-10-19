<?php

return [
    // The pzman release deployed by Enable and Upgrade; the release workflow
    // sets it to the plugin's own version.
    'pzman_version' => '0.0.0-dev',

    // A directory URL holding pzman-linux-amd64 and pzman-SHA256SUMS, instead of
    // the GitHub release of pzman_version (development and tests).
    'pzman_base_url' => env('EASYPZ_PZMAN_BASE_URL'),

    // Proxied public downloads running at once across the panel; each holds a
    // PHP-FPM worker. Beyond that, players get 429 with Retry-After.
    'max_downloads' => (int) env('EASYPZ_MAX_DOWNLOADS', 5),
];
