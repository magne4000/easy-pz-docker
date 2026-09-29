# Launcher plan

A desktop app for **non-Steam** Project Zomboid players. It keeps the client's mods in sync with a pzman
server and launches the game straight into that server. Built with Wails so it runs on Windows, macOS and Linux.

## Decisions

| Topic | Choice |
|---|---|
| Location | This repo: `launcher/` (Wails app), sharing Go types with pzman |
| Framework | Latest Wails v3 beta (`github.com/wailsapp/wails/v3`, `wails3` CLI), even though it isn't stable yet. Pin the exact version in `go.mod` and upgrade on purpose |
| Adding servers | Paste the public mod page URL, **or** import from the game's `ServerList.db` |
| Mod storage | Shared `~/Zomboid/mods` |
| Stale mods | Kept (the launcher never deletes mods) |
| Accounts | Reuse accounts saved in `ServerList.db`; the launcher does not create or edit them there |
| Game install | Auto-detect, with a folder picker to override |
| Server not available | Sync mods now, wait until the status is `available`, then join |
| pzman changes | `connect {host, port}` and game version in `data.json` |
| Extras | Status and player count in the server list; "Start game" button that opens the game without joining |
| Distribution | GitHub Releases, and the launcher updates itself |

## Facts established from the game (build 42.21)

- `-nosteam`, `+connect host:port` and `+password <serverPw>` are the only relevant launch arguments.
  There is no argument for the account username or password.
- In non-Steam mode, `+connect` only opens `ServerConnectPopup`. It never logs in by itself. Its pre-fill from saved
  accounts can't work either: `ServerConnectPopup.lua:156` compares an `int` port to a `string`.
  **So the launcher does not use `+connect`.**
- The game's normal login call is `ConnectToServer.instance:connect(prev, serverName, user, pwd, ip, localIP, port,
  serverPw, useSteamRelay, doHash, authType)`.
- `~/Zomboid/db/ServerList.db` is SQLite with a `server` table and an `account` table. It follows `-cachedir=`.
  Saved account passwords are stored already hashed: `bcrypt(md5hex(pw), "$2a$12$O/BFHoDFPrfFaNPAACmWpu")`.
  The game connects with those using `doHash=false` (`MultiplayerUI.lua:855`).
- `~/Zomboid/Lua/<file>` can be read and written from Lua with `getFileReader` / `getFileWriter`.
- `~/Zomboid/mods/default.txt` holds the mods active in the main menu (`ZomboidFileSystem`).
- Import from `ServerList.txt` looks broken: its account INSERT has 7 columns and 8 placeholders. Don't rely on it.

## How the login works

The launcher installs a small helper mod, `EasyPZAutoConnect`, into `~/Zomboid/mods` and activates it in `default.txt`.
Before each launch, the launcher writes `~/Zomboid/Lua/easypz-autoconnect.ini`:

```
host=… port=… serverName=… serverPassword=… user=… password=<bcrypt hash> doHash=false authType=1
```

On `OnMainMenuEnter`, the mod reads the file, erases it right away (so the login only happens once), and calls
`ConnectToServer.instance:connect(...)`. The file never contains a plain-text password. The launcher hashes any password
the user types with the formula above.

When the game joins a server it switches to the server's mods, so the helper doesn't need to be on the server.
The game is launched with `-nosteam` only.

## Components

### 1. pzman changes (do first, small)

- Move `PublicData` and its related types from `internal/httpapi/public.go` into a new package `internal/publicapi` that
  contains only types. The launcher can then import them without pulling in fiber.
- Add `connect: {host, port}` to `PublicData`:
  - New env var `PUBLIC_HOST`. When it's unset, leave `connect` out, and the launcher asks the user for the host.
  - `port` comes from the ini's `DefaultPort` (default 16261).
- Add `gameVersion` (e.g. `"42.21"`), read from the server's `projectzomboid.jar` (see the game-version item below).
- Run `make gen`, update `README.md`, add tests next to `server_test.go`.

### 2. Launcher core (Go, `launcher/internal/...`)

- **config:** launcher settings in the OS config dir (`os.UserConfigDir()/easypz-launcher/config.json`). It stores the game
  path, the list of servers (`publicUrl`, name, cached host/port, linked `ServerList.db` server and account id), and a
  record of which mod folders the launcher installed, with their sha256.
- **gamepath:** auto-detect. Check the Steam library folders (parse `libraryfolders.vdf`; `internal/steam/vdf.go` can
  probably be reused) and the default install paths on each OS. Validate the folder by checking that
  `projectzomboid.jar` exists. The executable to launch depends on the OS:
  - Windows: `ProjectZomboid64.exe`
  - Linux: `projectzomboid.sh`
  - macOS: `Project Zomboid.app/Contents/MacOS/…`, or `open -a … --args`
- **userdir:** `~/Zomboid`. Changing it isn't needed for now, since the user chose shared storage without `-cachedir`.
- **pzman client:** fetch `data.json` from the pasted URL. Parse the URL (`…/mods/<token>/`) and poll status and player
  count every ~15 s while the window is open.
- **modsync:** for each item in `data.json`:
  - Compare `download.sha256` with the recorded sha256.
  - When it differs, download `download/<id>.zip`, check the sha256, and extract to a temp folder next to `mods/`.
  - Then swap the folder in with a rename, so a crash can't leave a mod half-written.
  - Refuse to overwrite a mod folder the launcher didn't install unless the user confirms.
  - If `download.ready` is false, retry with a backoff (pzman builds the zips when they're first requested).
  - Show progress events in the UI.
- **serverlist (read-only):** open `ServerList.db` read-only with `modernc.org/sqlite` (no CGO). List servers and
  accounts in the game's own order, and match them to launcher servers by ip:port.
- **accounts:** the launcher needs a hashed password for the chosen server, from one of these sources, in order:
  1. The linked `ServerList.db` account, if it has a saved password.
  2. A hash the launcher stored in its own config because the user ticked "remember" (see open questions).
  3. Otherwise, ask the user at launch and hash the password in memory.

  `authType != 1` (two-factor) is not supported for auto-login. For those accounts, open the game's normal
  connect screen instead.
- **helpermod:** embed the mod's files (`embed.FS`) and install or update the mod in `~/Zomboid/mods/EasyPZAutoConnect`
  (B42 layout: `42/mod.info`, `42/media/lua/client/…`, plus `common/`). Activate it in `default.txt` while keeping
  the other entries unchanged. Write and delete the `.ini`.
- **launch:** start the game as a separate process that keeps running after the launcher exits. Don't open anything
  else while the game is running (it holds `ServerList.db` open). "Play" waits until the server's status is
  `available`, then launches. "Start game" syncs the mods and launches without writing the `.ini`.
- **game version:** read the game version from `zombie/core/Core.class` inside `projectzomboid.jar`. The static
  initializer builds `new GameVersion(42, 21, "")`. Put this parser in a shared package (`internal/pz/gamever`) so pzman
  and the launcher use the same code. If the client and server versions differ, show a warning without blocking.
  If parsing fails, skip the check without an error.
- **selfupdate:** check GitHub Releases for a newer tag on startup, download the build for this OS/architecture, verify
  it against the release's checksum file, then replace the binary and restart (e.g. with `minio/selfupdate`). On macOS
  it replaces the whole `.app` bundle.

### 3. Launcher UI (`launcher/frontend`)

Use the same stack as `web/`: React 19, Vite, Tailwind 4, radix-ui, i18next, biome. Share UI components with `web/` by
copying them for now. Get the `data.json` types from `api/openapi.json` (`x-public-mod-page`).

Screens:
- **Servers:** one card per server showing name, status badge, player count, mod sync state (up to date / N to download /
  syncing X%), the account in use, and **Play** / **Start game** buttons.
- **Add server:** two tabs. *Paste URL* shows a preview (name, mod count, total size). *Import from game* lists the
  entries in `ServerList.db`; importing one without a pzman URL gives a server that can connect but has no mod sync.
- **Account picker:** saved accounts for the server, or a username and password form.
- **Settings:** game path (detected or chosen), launcher version and update button, links to the log folder.
- **Waiting overlay:** "Server restarting — will join automatically", with a Cancel button.

### 4. Build and CI

- The Wails app lives in `launcher/` inside the root Go module. Wails needs CGO, but that only affects the launcher build,
  since pzman keeps `CGO_ENABLED=0`.
- Add Makefile targets: `launcher-dev` (`wails3 dev`) and `launcher-build` (`wails3 build`, or its Taskfile tasks).
- Add a CI matrix (ubuntu, macos, windows) to the existing workflow on `v*` tags: `wails3 build`/`wails3 package`, create the release
  assets and a `checksums.txt`.
- Leave code signing out of the first version: macOS will need `xattr` or right-click → Open, and Windows SmartScreen
  will warn. Add signing later.
- Add `projectzomboid.jar` and `pz-lua-client/` (local reference copies) to `.gitignore`.

## Milestones

1. **Prove it by hand.** Hand-write the helper mod, the `default.txt` entry and the `.ini`, then launch with `-nosteam`
   and check that it logs in with a stored hash. This checks the only assumptions not yet confirmed in the game.
2. **pzman:** `publicapi` package, `connect`, `gameVersion`, regenerated API client, tests.
3. **Launcher core:** gamepath, pzman client, modsync, serverlist, helpermod, launch, with unit tests (use a fake
   `data.json` server and a temporary `~/Zomboid`).
4. **UI:** servers, add, account, settings, waiting overlay.
5. **Release:** CI matrix, self-update, README section.

## Open questions

- **Remembering new accounts:** for servers added by URL that have no saved account, should the launcher keep the
  bcrypt hash in its own config (proposed default: yes, when "remember" is ticked)?
- **B42 mod layout and `default.txt` format:** to confirm in milestone 1.
