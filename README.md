# easy-pz-docker

A single-container Project Zomboid dedicated server with a built-in web panel.
One Go binary (`pzman`) is the container's main process and owns everything:
PUID/PGID, game install and updates (SteamCMD), Workshop mods (also for
**non-Steam** servers), supervision of the JVM, signal-safe shutdown, smart
backups, scheduling and the web UI.

## Features

- **Server control**: start / stop / restart with in-game countdown warnings, save, broadcast, live status and player count.
- **Console**: live server log plus an RCON terminal that recognises PZ's rejection messages.
- **Updates**: game build and Workshop mods are checked hourly; one *update window* announces, waits for an empty server (forced after 2 h by default), saves, stops, backs up, updates the game and mods, relinks mods and starts again.
- **Mods**: track Workshop items or import a collection, per-mod enable, dependency-aware load order, conflict scan, `Mods=` / `WorkshopItems=` / `Map=` written for you. In non-Steam mode (`USE_STEAM=false`) mods are downloaded with anonymous SteamCMD and mirrored into the server's `mods/` folder as per-file symlinks (no copies; PZ drops a mod's scripts when its whole folder is one symlink), with a boot-time check that refuses to start with missing mods. Mods added while the server runs are downloaded right away and load at the next restart.
- **Backups**: world + account database + server config (never the multi-GB mods), tar+zstd with a checksummed manifest. A backup is only taken when the world changed, so every archive is a distinct state. This relies on `PauseEmpty=true` (PZ's default): with it off the game clock runs on an empty server, every backup is taken, and the UI says so. Keep N recent + daily + weekly, optional size budget, pinning, verified restore with an automatic safety backup.
- **Scheduler**: cron schedules for restarts, stops, saves, broadcasts and backups.
- **Player mod page**: an unlisted, `noindex` link where players download the exact mod set as a zip (with resume support and a checksum), plus the config snippet for people hosting the same set.
- **Server config**: structured editor for the server `.ini`, with *save & reload*.
- **Sandbox options**: structured editor for `<server>_SandboxVars.lua` with ranges, defaults and choices. Enabled mods' options appear like in the game's sandbox editor: on the mod's page, with its translated names, tooltips and choices (from the mod's `sandbox-options.txt` and English `Sandbox.json`), including options the server has not written to the file yet. Changes apply at the next start.
- **Launcher** (non-Steam players): syncs mods and joins the server. See [Launcher](#launcher).

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
| `UPDATE_ON_START` | `true` | update the game before the first start |
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
| `PANEL_PUBLIC_HOST` | | address players join, for the launcher |
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

## Launcher

`launcher/` is a desktop app for players of a **non-Steam** server: paste the
server's mod page link, and it keeps the mods in `~/Zomboid/mods` in sync and
starts the game straight into the server. Set `PANEL_PUBLIC_HOST` so it knows
where to connect. Builds are on the releases page and update themselves. Pull
requests that touch the launcher get test builds, linked from a bot comment.

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

Layout: `cmd/pzman` (entry, `pzman openapi`), `internal/boot` (wiring, boot
order, shutdown), `internal/httpapi` (Fiber + Huma, auth, SSE, public page),
`internal/sched` (lifecycle coordinator, update window, cron),
`internal/pz` (ini/launch codecs, supervisor, RCON), `internal/steam`,
`internal/mods`, `internal/backup`, `internal/sys`, `internal/store`,
`internal/devfake`, `web/` (React 19 + Tailwind 4 + shadcn/ui),
`launcher/` (Wails v3, its own Go module).
