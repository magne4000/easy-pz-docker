# easy-pz-docker

A single-container Project Zomboid dedicated server with a built-in web panel.
One Go binary (`pzman`) is the container's main process and owns everything:
PUID/PGID, game install and updates (SteamCMD), Workshop mods (also for
**non-Steam** servers), supervision of the JVM, signal-safe shutdown, smart
backups, scheduling and the web UI.

## Features

- **Server control**: start / stop / restart with in-game countdown warnings, save, broadcast, live status and player count.
- **Console**: live server log plus an RCON terminal that recognises PZ's rejection messages.
- **Updates**: game build and Workshop mods are checked hourly; one *update window* announces, waits for an empty server (forced after 2 h by default), saves, stops, backs up, updates the game and mods, relinks mods and starts again. *Settings → Lock game version* keeps the installed build: the game is then never updated (not at start, not in update windows) while mods still are.
- **Mods**: track Workshop items or import a collection, per-mod enable, dependency-aware load order, conflict scan, `Mods=` / `WorkshopItems=` / `Map=` written for you. In non-Steam mode (`USE_STEAM=false`) mods are downloaded with anonymous SteamCMD and mirrored into the server's `mods/` folder as per-file symlinks (no copies; PZ drops a mod's scripts when its whole folder is one symlink), with a boot-time check that refuses to start with missing mods. Mods added while the server runs are downloaded right away and load at the next restart.
- **Local mods**: mods that are not on the Workshop. Upload a mod folder from the Mods page, or copy it into the data volume's `mods/` folder yourself. They work like Workshop mods (enable, load order, player mod page, launcher) and can be removed from the UI.
- **Backups**: world + account database + server config (never the multi-GB mods), tar+zstd with a checksummed manifest. A backup is only taken when the world changed, so every archive is a distinct state. This relies on `PauseEmpty=true` (PZ's default): with it off the game clock runs on an empty server, every backup is taken, and the UI says so. Keep N recent + daily + weekly, optional size budget, pinning, verified restore with an automatic safety backup.
- **Scheduler**: cron schedules for restarts, stops, saves, broadcasts and backups.
- **Player mod page**: an unlisted, `noindex` link where players download the exact mod set as a zip (with resume support and a checksum), plus the config snippet for people hosting the same set. Non-Steam servers also link the launcher released with the image's version.
- **Server config**: structured editor for the server `.ini`, with *save & reload*.
- **Sandbox options**: structured editor for `<server>_SandboxVars.lua` with ranges, defaults and choices. Enabled mods' options appear like in the game's sandbox editor: on the mod's page, with its translated names, tooltips and choices (from the mod's `sandbox-options.txt` and English `Sandbox.json`), including options the server has not written to the file yet. Changes apply at the next start.
- **Launcher** (non-Steam servers): syncs mods and joins the server, with a Steam or non-Steam copy of the game. See [Launcher](#launcher).
- **Pelican**: also runs as an add-on to the stock Project Zomboid egg of the Pelican panel, with Pelican's logins and permissions. See [Pelican](#pelican).

## Running it

See [`docker-compose.example.yml`](docker-compose.example.yml). It is a drop-in
replacement for `indifferentbroccoli/projectzomboid-server-docker`: the volume
layout and the broccoli environment names are the same.

```sh
docker compose up -d
docker compose logs -f
```

Then open `http://<host>:8080`. The image is published to
`ghcr.io/magne4000/easy-pz-docker` on tags (linux/amd64). It carries no 32-bit
runtime: SteamCMD runs as Valve's 64-bit binary, which also makes the image work
on Apple Silicon under Docker's x86-64 emulation (tested with OrbStack).

### Panel password (required)

The panel never stores or accepts a plaintext password: `PANEL_ADMIN_PASSWORD_HASH`
must be a **bcrypt** hash, and the container refuses to start without one.
Generate it offline:

```sh
docker run --rm httpd:alpine htpasswd -bnBC 12 "" 'your-password' | cut -d: -f2
```

(or use <https://bcrypt-generator.com/> with cost **12**). In a compose file,
double every `$` of the hash (`$$2y$$12$$…`).

### Environment

Variables inherited from the broccoli image keep their names:

| Variable | Default | |
|---|---|---|
| `PUID` / `PGID` | `568` | the game and SteamCMD run as this user; only wrong ownership is fixed at boot |
| `SERVER_NAME` | `pzserver` | |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | `admin` / generated | in-game admin, used on the very first boot only (a generated password is logged) |
| `DEFAULT_PORT` / `UDP_PORT` | `16261` / `16262` | seeded into the ini if missing, never overwritten |
| `MAX_PLAYERS` | `32` | seeded if missing |
| `SERVER_PASSWORD` | | seeded if missing |
| `RCON_PORT` / `RCON_PASSWORD` | `27015` / generated | an empty `RCONPassword=` is replaced so RCON always works |
| `USE_STEAM` | `true` | `false` = non-Steam mode (`-Dzomboid.steam=0`) |
| `MEMORY_XMX_GB` | `8` | JVM heap |
| `VM_ARGS` | | extra JVM args; each one *replaces* an existing arg with the same key |
| `SERVER_BRANCH` | public | SteamCMD beta branch |
| `UPDATE_ON_START` | `true` | update the game before the first start (skipped while the game version is locked) |
| `TZ` | `UTC` | used by the scheduler |

Panel variables:

| Variable | Default | |
|---|---|---|
| `PANEL_ADMIN_USER` | `admin` | |
| `PANEL_ADMIN_PASSWORD_HASH` | **required** | bcrypt |
| `PANEL_PORT` | `8080` | |
| `PANEL_JWT_SECRET` | random | set it to keep sessions across restarts |
| `PANEL_SESSION_TTL` | `12h` | |
| `PANEL_COOKIE_SECURE` | `auto` | `auto` trusts `X-Forwarded-Proto` from `PANEL_TRUSTED_PROXIES` (and loopback) only |
| `PANEL_TRUSTED_PROXIES` | | comma-separated reverse proxy addresses |
| `PANEL_MODS_TOKEN` | random, persisted | the unlisted `/mods/<token>/` path |
| `PANEL_PUBLIC_URL` | | the panel's public address, `http(s)://host[:port]`: the mod page link shown in Settings and in the mod page's launcher card. Its host is also where the launcher joins, unless `PANEL_PUBLIC_GAME_ADDRESS` is set |
| `PANEL_PUBLIC_GAME_ADDRESS` | host of `PANEL_PUBLIC_URL` | where players join the game, `host[:port]` (IPv6 in brackets). Set it when that host only carries HTTP (CDN or proxy) or the game runs elsewhere. The port defaults to the server's `DefaultPort`; set it when players reach the game through another forwarded port |
| `PANEL_BACKUP_DIR` | `/backups` | mount a different volume than the one it protects |
| `PANEL_BACKUP_INTERVAL` / `_KEEP` / `_KEEP_DAILY` / `_KEEP_WEEKLY` / `_MAX_TOTAL_GB` | `2h` / `24` / `7` / `4` / `0` | defaults; editable in the UI |
| `PANEL_UPDATE_CHECK_INTERVAL` / `PANEL_UPDATE_MAX_DELAY` | `1h` / `2h` | defaults; editable in the UI |
| `PANEL_WORKSHOP_COLLECTION` | | default collection id |
| `PANEL_STOP_TIMEOUT` | `90s` | time allowed for save + quit before the JVM is killed |
| `PANEL_DISK_WARN_PERCENT` / `PANEL_DISK_CRIT_PERCENT` | `85` / `95` | |
| `PANEL_LOG_LEVEL` | `info` | |

Give the container a `stop_grace_period` longer than `PANEL_STOP_TIMEOUT`
(120 s in the example) so `docker stop` always lets the world save.

### Exposing it

The panel speaks plain HTTP; put it behind your reverse proxy for TLS and set
`PANEL_TRUSTED_PROXIES`. The admin login and the public mod page share one port:
if you publish the mod page to the internet, restrict everything except
`/mods/<token>/` and `/assets/` to your LAN at the proxy.

The mod page runs at most 4 downloads at once per client address; the
launcher waits for a free one. Without `PANEL_TRUSTED_PROXIES`, every player
behind the proxy shares the proxy's address, and these 4 downloads.

## Pelican

Instead of the compose image, easy-pz can run inside a [Pelican](https://pelican.dev)
server made from the stock Project Zomboid egg (`pelican-eggs/games-steamcmd`).
A Pelican plugin deploys `pzman` into the server; Pelican's logins and
permissions replace the panel password, and players reach the mod page through
the panel's URL. The egg's layout stays: the game in the server folder, the
Zomboid data in `.cache/`, and pzman with its database, backups and logs in
`.easy-pz/`.

### Requirements

- An **x86-64** node: the egg's game image is amd64-only.
- Pelican panel and Wings **1.0.0-rc.1** or later.
- A **memory limit** on the PZ server (6144 MiB is a good start). pzman sizes
  the JVM heap from it: 75 %, rounded down to whole GiB (6144 MiB → 4 GiB).
  Without a limit the heap falls back to 8 GiB, and Wings caps the egg's
  install at 1024 MiB: SteamCMD is killed halfway through the download and the
  egg still reports a complete install.
- **Three allocations**: the game port (primary), the one set in the egg's
  `STEAM_PORT` variable, and one TCP port for easy-pz's web UI, which Pelican
  users' browsers open (players don't need it).

### The stack

[`pelican/compose.yml`](pelican/compose.yml) runs the panel and Wings in one
compose project (panel on 16264, Wings on 16265, SFTP on 2022);
[`pelican/init.sh`](pelican/init.sh), next to it, creates the first admin, the
local node and Wings' config on the first `docker compose up`. Replace the
placeholders marked in the file: the host address `192.168.1.10`, the admin's
email and password.

It mounts `panel/plugins` on the panel's `/var/www/html/plugins`: the panel
image keeps plugins outside its data volume, so without that mount they vanish
whenever the container is recreated.

### Installing the plugin

From a release: *Admin → Plugins → Import from URL* with
`https://github.com/magne4000/easy-pz-docker/releases/latest/download/easy-pz-pelican-plugin.zip`
(or download it and *Import from File*), then *Install*. From a checkout:

```sh
cp -r pelican-plugin/easy-pz /mnt/apps/docker/pelican/panel/plugins/
chown -R 82:82 /mnt/apps/docker/pelican/panel/plugins/easy-pz   # the panel's www-data
docker exec pelican-panel php artisan p:plugin:install easy-pz
```

The plugin follows the latest release: the plugin list offers *Update* when
there is a new one. Each plugin release pins the pzman of the same version;
servers keep the pzman they run until an admin clicks *Upgrade* on their
*Easy PZ* page. The plugin's settings hold the number of mod pack downloads the
panel proxies at once (5 by default; each takes one of the panel's 15 PHP
workers) and a pzman download URL for development builds.

### Enabling easy-pz on a server

1. Add `easy_pz` to the egg's *Features* (*Admin → Eggs → Project Zomboid*).
   Only servers of eggs with this feature get the *Easy PZ* page. Enable
   refuses an egg that no longer looks like the stock one (stop command
   `quit`, `-cachedir=/home/container/.cache` in its startup, app 380870).
2. Create the server with a memory limit and the three allocations, and let
   the install finish.
3. On the server's *Easy PZ* page, pick the UI allocation and click *Enable*.
   The plugin writes `.easy-pz/pzman.env`, downloads pzman, checks it against
   the release's `pzman-SHA256SUMS`, sets the startup command to
   `./.easy-pz/pzman` and `AUTO_UPDATE` to 0 (pzman updates the game itself, in
   update windows and respecting *Lock game version*), then restarts the server.

pzman takes the egg's variables (`SERVER_NAME`, `ADMIN_USER`,
`ADMIN_PASSWORD`, `MAX_PLAYERS`, the game port, `STEAM_PORT`, `SRCDS_BETAID`)
and reads any other variable of [Environment](#environment) from the server's
environment: to set one (`USE_STEAM=false` for a non-Steam server,
`SERVER_PASSWORD`, `VM_ARGS`, `MEMORY_XMX_GB`…), add it to the egg as a
variable (*Egg Variables* tab) and give it a value per server. `PUID`/`PGID`
and the panel password variables don't apply.

### What changes for users

- *Open easy-pz* on the *Easy PZ* page opens easy-pz in a new tab, signed in
  as the Pelican user: no password (password login is off). The session lasts
  an hour, then renews itself through the panel, so a removed permission
  applies within the hour. The page itself shows the status and the common
  actions: check for updates, update window, restart with countdown, save,
  broadcast, backup.
- The owner and admins can do everything; subusers get what is ticked in the
  *Easy PZ* group of the subuser editor:
  - `easy-pz.read`: the *Easy PZ* page and easy-pz, read-only;
  - `easy-pz.control`: restart, save, broadcast, cancel operations; check for
    updates, open or cancel update windows;
  - `easy-pz.console`: send console commands from easy-pz;
  - `easy-pz.mods`: add, remove, enable, order and upload mods;
  - `easy-pz.config`: edit the server `.ini` (RCON password included) and the
    sandbox options;
  - `easy-pz.backups`: create, restore, pin, delete and download backups;
  - `easy-pz.schedules`: create, edit, run and delete schedules (a schedule
    that restarts, saves or broadcasts also needs `control`; one that backs up,
    `backups`);
  - `easy-pz.settings`: change easy-pz's settings.
- Pelican's console is the game's: its output plus `[steamcmd]` and `[pzman]`
  lines, and commands typed there (`players`, `save`…) go to the game.
  pzman's own log is `.easy-pz/logs/pzman.log`.
- Start, Stop and Restart are Pelican's: Stop saves the world and stops the
  game. easy-pz has no Stop button and no `stop` schedule. Its own restarts
  (update windows, schedules) happen inside the container, so Pelican keeps
  showing the server running meanwhile.
- Pelican backups hold the world, the config and pzman's database: a managed
  block in `.pelicanignore` leaves out the game install, SteamCMD and
  easy-pz's own backups, cache and logs.
- The player mod page is `<panel URL>/easy-pz/<server id>/mods/<token>/`, where
  `<server id>` is the 8-character id in the server's panel URL; easy-pz's
  *Settings* shows the full link. Players open it and paste it into the
  launcher, which joins `<node address>:<game port>`. Downloads go through the
  panel; beyond its limit of concurrent downloads, the launcher waits for a
  free one.

### The shared-hostname warning

Browsers send cookies by hostname, not by port. When the panel and the node
share a hostname (both `192.168.1.10` in the compose above), opening easy-pz
sends the panel's session cookie to pzman's port, and whoever controls the
server's files (its owner can replace `.easy-pz/pzman`) could capture the
panel session of an admin who opens easy-pz. Root admins see a warning on the
*Easy PZ* page. To avoid it, give the panel and the node different hostnames
(e.g. `APP_URL=http://pelican.home.lan:16264` and `WINGS_FQDN=192.168.1.10`),
or set *Admin → UI address* to another hostname, such as a reverse proxy in
front of easy-pz.

### Migrating from the compose deployment

1. Create the Pelican server as above, with the compose's `SERVER_NAME`, and
   allocations 16261 (primary) and 16262 (`STEAM_PORT`): the server `.ini`
   keeps the ports it has (pzman only fills in missing ones). For
   `USE_STEAM=false`, add `USE_STEAM` to the egg (see above); for
   `MEMORY_XMX_GB: "8"`, give the server 11264 MiB or add `MEMORY_XMX_GB`.
   Let the install finish; don't enable easy-pz yet.
2. Stop the compose container: `docker compose stop projectzomboid` (pzman
   saves and quits).
3. Copy the volumes into the server folder, as root (the UUID is on the
   server's admin page):

   ```sh
   old=/mnt/apps/docker/zomboid
   new=/mnt/apps/docker/pelican/wings/volumes/<server UUID>
   cp -a "$old/files/." "$new/"                 # the game and its Workshop downloads, over the egg's install
   mkdir -p "$new/.cache" "$new/.easy-pz"
   cp -a "$old/data/." "$new/.cache/"           # Server/, Saves/, db/, mods/
   mv "$new/.cache"/pzman.db* "$new/.easy-pz/"  # pzman's database
   cp -a "$old/backups" "$new/.easy-pz/backups"
   chown -R 568:568 "$new"                      # WINGS_UID:WINGS_GID
   ```

4. Enable easy-pz on the *Easy PZ* page.

Mods, load order, settings, schedules, the mod page token and the backups
(listed and restorable) carry over. The mod links in `.cache/mods/` still point
into `/project-zomboid`: pzman relinks them before the first start. Players
need the new mod page link.

### Disabling

*Admin → Disable easy-pz* on the *Easy PZ* page gives the egg its startup
command and `AUTO_UPDATE` back; the stock egg runs from the next start. The
world and `.easy-pz/` (database, backups, mod cache) stay: enabling again
resumes.

### Limitations

- The stock PZ egg on x86-64 only.
- Pelican's plugin system is young (panel 1.0.0-rc.1): a panel upgrade may need
  a plugin update.
- Updating the egg from its URL removes the `easy_pz` feature and the variables
  you added: add them again.
- Each proxied download holds one of the panel's PHP workers, hence the limit
  on concurrent downloads; players beyond it queue.
- easy-pz's UI port must be reachable by Pelican users' browsers.

## Launcher

`launcher/` is a desktop app for players of a **non-Steam** server
(`USE_STEAM=false`): paste the server's mod page link, and it keeps the mods in
`~/Zomboid/mods` in sync and starts the game straight into the server with
`-nosteam`. When a mod it already has is updated, it downloads only the files
that changed (the whole mod when most of it did). A Steam copy of the game
works too: it is found in the Steam libraries and started the same way. To join
a server, the launcher uses the account the game remembers for it: add the
server and your account once in the game's *Multiplayer* menu with *Remember
password* ticked (the launcher shows the steps), and *Join* connects from then
on. Until then, *Play* syncs the mods and starts the game, and you join from the
game's menu. Set `PANEL_PUBLIC_URL` (or
`PANEL_PUBLIC_GAME_ADDRESS`) so it knows where to connect. Builds are on the
releases page; the mod page links the release of the image's version. The
launcher never looks for updates on its own: *Settings → Check for updates*
finds a newer release and installs it on request.

## Developing

Requirements: Go 1.27 and [Bun](https://bun.sh). The backend reloader (`wgo`)
is pinned in `go.mod` and run with `go tool wgo`, so there is nothing else to install.

```sh
cd web && bun install && cd ..
make dev                 # Vite on :5173 (open this) + Go on :8080 with fake drivers
make dev-api             # the Go half alone: its errors are easier to read here
make dev SCENARIO=update-window
make clean-dev           # reset the fake server tree between scenarios
```

Dev login: `admin` / `devpassword`. With `PANEL_DRIVERS=fake` there is no game,
SteamCMD or RCON: in-memory drivers replay a scenario — `idle`,
`update-window`, `backup-running`, `crash-loop`, `mod-conflict` — against a real miniature file tree under `.dev/`. The fake
server's public mod page is at `http://localhost:5173/mods/dev-token/`.

| Command | |
|---|---|
| `make test` | Go tests + Vitest |
| `make lint` | golangci-lint + Biome |
| `make gen` | regenerate `api/openapi.json` and `web/src/api/schema.d.ts` (CI fails if stale) |
| `make build` | UI build embedded into a static `pzman` |
| `docker build -f docker/Dockerfile .` | the image (linux/amd64) |
| `make launcher-dev` / `launcher-build` / `launcher-test` / `launcher-gen` | the launcher (needs `wails3`) |
| `pelican/e2e.sh` | Pelican end-to-end test with the real game, x86-64 only (CI: workflow `pelican-e2e`, on demand or on PRs labelled `pelican-e2e`) |

Layout: `cmd/pzman` (entry, `pzman openapi`), `internal/boot` (wiring, boot
order, shutdown), `internal/httpapi` (Fiber + Huma, auth, SSE, public page),
`internal/sched` (lifecycle coordinator, update window, cron),
`internal/pz` (ini/launch codecs, supervisor, RCON), `internal/steam`,
`internal/mods`, `internal/backup`, `internal/sys`, `internal/store`,
`internal/devfake`, `web/` (React 19 + Tailwind 4 + shadcn/ui),
`launcher/` (Wails v3, its own Go module), `pelican-plugin/easy-pz` (the
Pelican plugin, PHP), `pelican/` (Pelican stack and end-to-end test).
