#!/usr/bin/env bash
# End-to-end test of easy-pz on Pelican with the real game: the stack of
# compose.yml + init.sh, the plugin from pelican-plugin/easy-pz, a server from
# the stock Project Zomboid egg, Enable, then what users and players do.
#
# x86-64 Linux only (the egg's game image is amd64-only), on a throwaway host:
# it starts containers named pelican-*, binds ports 16261-16266, 2022 and
# DIST_PORT, and needs docker, curl, jq, python3 and root (sudo) for ROOT.
#
#   DIST     directory holding pzman-linux-amd64 and pzman-SHA256SUMS (plugin
#            stage), e.g.
#              (cd web && bun install && bun run build)
#              CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o "$DIST/pzman-linux-amd64" ./cmd/pzman
#              (cd "$DIST" && sha256sum pzman-linux-amd64 >pzman-SHA256SUMS)
#   STAGES   space-separated, in order: setup plugin pz easypz (default: all)
#   OUT      results ($OUT/results.md), steps log and collected container logs
#   ROOT     host directory of the stack (compose.yml's /mnt/apps/docker/pelican)
#   HOST_IP  host address for the panel URL and the node (default: first of hostname -I)
#   PZ_EXTRA_ENV  KEY=value pairs added to the egg as variables, so they reach
#            pzman's environment (e.g. USE_STEAM=false)
#   SKIP_INSTALL=1  create the server without running the egg's install
#            (with PZ_EXTRA_ENV=PANEL_DRIVERS=fake: the plugin path without the game)
# SC2016: PHP and inner shell code in single quotes; SC2329: helpers run
# through check and wait_for.
# shellcheck disable=SC2016,SC2329
set -uo pipefail

here=$(cd "$(dirname "$0")" && pwd)
REPO=${REPO:-$(cd "$here/.." && pwd)}
OUT=${OUT:-$PWD/e2e-out}
DIST=${DIST:-$OUT/dist}
ROOT=${ROOT:-/srv/pelican-e2e}
HOST_IP=${HOST_IP:-$(hostname -I 2>/dev/null | awk '{print $1}')}
STAGES=${STAGES:-setup plugin pz easypz}
PZ_EXTRA_ENV=${PZ_EXTRA_ENV:-}
SKIP_INSTALL=${SKIP_INSTALL:-0}
SUDO=${SUDO-sudo}
EGG_URL=https://raw.githubusercontent.com/pelican-eggs/games-steamcmd/main/project_zomboid/egg-project-zomboid.json
MOD_ID=${MOD_ID:-2169435993} # a small Workshop item
DIST_PORT=${DIST_PORT:-8099}
PZMAN_PORT=16266
PZ_MEMORY=6144
SERVER_NAME=e2e
panel_url=http://$HOST_IP:16264

mkdir -p "$OUT/stack" "$OUT/logs"
res=$OUT/results.md
: >"$res"
FAILED=0
log() { printf '%s %s\n' "$(date -u +%T)" "$*" | tee -a "$OUT/steps.log" >&2; }
note() { printf -- '- %s\n' "$*" | tee -a "$res" >&2; }
pass() { note "PASS: $*"; }
fail() { note "FAIL: $*"; FAILED=1; }
check() { # check DESCRIPTION COMMAND...
  local what=$1
  shift
  if "$@"; then pass "$what"; else fail "$what"; fi
}
section() { printf '\n## %s\n\n' "$*" | tee -a "$res" >&2; }
block() { { printf -- '- %s:\n\n```\n' "$1"; cat; printf '```\n'; } | tee -a "$res" >&2; }
has_stage() { [[ " $STAGES " == *" $1 "* ]]; }
now() { date -u +%FT%TZ; }

# The panel: tinker runs PHP in the panel's app.
tinker() { docker exec pelican-panel php artisan tinker --execute "$1" 2>&1; }
val() { sed -n "s/^$1=//p" <<<"$2" | tail -n1; }
server() { printf 'App\\Models\\Server::where("uuid", "%s")->sole()' "$1"; }
daemon() { printf 'app(App\\Repositories\\Daemon\\DaemonServerRepository::class)->setServer(%s)' "$(server "$1")"; }
power() { tinker "$(daemon "$1")->power(\"$2\");" >>"$OUT/steps.log"; }
send() { tinker "$(server "$1")->send(\"$2\");" >>"$OUT/steps.log"; }
wings_state() { val STATE "$(tinker "echo 'STATE=', $(daemon "$1")->getDetails()['state'] ?? '?', PHP_EOL;")"; }
wings_is() { [ "$(wings_state "$1")" = "$2" ]; }
server_status() { val STATUS "$(tinker "echo 'STATUS=', $(server "$1")->status?->value ?? 'installed', PHP_EOL;")"; }
install_finished() { case $(server_status "$1") in installed | install_failed) return 0 ;; esac; return 1; }

# The game server's container (named after the server's UUID): Pelican's console.
container_state() { docker inspect -f '{{.State.Status}} {{.State.ExitCode}}' "$1" 2>/dev/null; }
exited() { [[ $(container_state "$1") == exited* ]]; }
logs_since() { docker logs --since "$2" "$1" 2>&1; }
# grep -c reads everything: with pipefail, grep -q would SIGPIPE docker logs.
in_logs_since() { logs_since "$1" "$2" | grep -c -- "$3" >/dev/null; }
# The container exited after $2 (a container stopped earlier does not count).
exited_since() {
  local finished
  finished=$(docker inspect -f '{{.State.FinishedAt}}' "$1" 2>/dev/null) || return 1
  exited "$1" && [[ $finished > "$2" ]]
}
# SERVER STARTED printed, or the container already gave up.
started_or_exited() { in_logs_since "$1" "$2" "SERVER STARTED" || exited_since "$1" "$2"; }
crashes() { root cat "$ROOT/wings/logs/wings.log" 2>/dev/null | grep -c 'crashed state'; }

# Files under ROOT belong to Wings' user and the panel's: read them as root.
root() { if [ -n "$SUDO" ]; then "$SUDO" "$@"; else "$@"; fi; }
public_key_only() { root grep -q '^PZMAN_PELICAN_PUBLIC_KEY=[0-9a-f]\{64\}$' "$1" && ! root grep -qi secret "$1"; }
changed_since() { [ "$(root find "$1" -newermt "$2" -type f | wc -l)" -gt 0 ]; }
same_hash() { [ -n "$2" ] && [ "$(root sha256sum "$1" | cut -d' ' -f1)" = "$2" ]; }

WAITED=0
wait_for() { # wait_for SECONDS DESCRIPTION COMMAND...
  local limit=$1 what=$2 start=$SECONDS
  shift 2
  until "$@" >/dev/null 2>&1; do
    if ((SECONDS - start >= limit)); then
      WAITED=$((SECONDS - start))
      log "timeout (${limit}s): $what"
      return 1
    fi
    sleep 5
  done
  WAITED=$((SECONDS - start))
  log "after ${WAITED}s: $what"
}

dist_pid=""
collect() {
  log "collecting logs"
  [ -n "$dist_pid" ] && kill "$dist_pid" 2>/dev/null
  docker ps -a --format '{{.Names}}\t{{.Image}}\t{{.Status}}' >"$OUT/logs/docker-ps.txt" 2>&1
  for c in $(docker ps -a --format '{{.Names}}'); do docker logs "$c" >"$OUT/logs/container-$c.log" 2>&1; done
  root cp -r "$ROOT/wings/logs" "$OUT/logs/wings" 2>/dev/null
  [ -n "${pz:-}" ] && root cp -r "$ROOT/wings/volumes/$pz/.easy-pz/logs" "$OUT/logs/pzman" 2>/dev/null
  root chmod -R a+rX "$OUT/logs" 2>/dev/null
}
trap collect EXIT

# ---------------------------------------------------------------------------
if has_stage setup; then
  section "Setup"
  [ -n "$HOST_IP" ] || { fail "HOST_IP unknown: set it"; exit 1; }
  if docker ps -a --format '{{.Names}}' | grep -c '^pelican-' >/dev/null; then
    fail "pelican-* containers already exist: run this on a throwaway host"
    exit 1
  fi
  note "host: $(uname -m), $(getconf _NPROCESSORS_ONLN) CPUs, docker $(docker version -f '{{.Server.Version}}'), host IP $HOST_IP"
  root mkdir -p "$ROOT"
  sed -e "s#/mnt/apps/docker/pelican#$ROOT#g" -e "s#192\.168\.1\.10#$HOST_IP#g" "$here/compose.yml" >"$OUT/stack/compose.yml"
  cp "$here/init.sh" "$OUT/stack/init.sh"
  docker compose -f "$OUT/stack/compose.yml" up -d >>"$OUT/steps.log" 2>&1 || { fail "docker compose up"; exit 1; }
  wait_for 240 "panel healthy" bash -c '[ "$(docker inspect -f "{{.State.Health.Status}}" pelican-panel)" = healthy ]' ||
    { fail "panel healthy"; exit 1; }
  wait_for 240 "Wings loaded its servers" bash -c 'docker logs pelican-wings 2>&1 | grep -c "processing servers returned by the API"' ||
    { fail "Wings reached the panel"; exit 1; }
  pass "stack up"
fi

# ---------------------------------------------------------------------------
if has_stage plugin; then
  section "Plugin install"
  if [ ! -s "$DIST/pzman-linux-amd64" ] || [ ! -s "$DIST/pzman-SHA256SUMS" ]; then
    fail "pzman-linux-amd64 and pzman-SHA256SUMS in $DIST (see the header of $0)"
    exit 1
  fi
  # Into the plugins mount, as the README's manual install does.
  docker cp "$REPO/pelican-plugin/easy-pz" pelican-panel:/var/www/html/plugins/ >>"$OUT/steps.log" 2>&1
  docker exec -u 0 pelican-panel chown -R www-data:www-data /var/www/html/plugins/easy-pz
  docker exec pelican-panel php artisan p:plugin:install easy-pz >>"$OUT/steps.log" 2>&1
  check "plugin installed and enabled" bash -c "docker exec pelican-panel php artisan p:plugin:list 2>&1 | grep -c 'Easy PZ.*enabled' >/dev/null"
  check "plugin kept in the host's plugins directory" root test -e "$ROOT/panel/plugins/easy-pz/plugin.json"
  # Enable downloads pzman from here instead of the GitHub release.
  python3 -m http.server -d "$DIST" "$DIST_PORT" >"$OUT/logs/dist-http.log" 2>&1 &
  dist_pid=$!
  docker exec pelican-panel sh -c "sed -i '/^EASYPZ_PZMAN_BASE_URL=/d' /pelican-data/.env && echo 'EASYPZ_PZMAN_BASE_URL=http://$HOST_IP:$DIST_PORT' >>/pelican-data/.env"
  check "the panel reaches the pzman download dir" wait_for 30 "pzman download dir" \
    docker exec pelican-panel php -r "exit(@file_get_contents('http://$HOST_IP:$DIST_PORT/pzman-SHA256SUMS') === false ? 1 : 0);"
fi

# ---------------------------------------------------------------------------
if has_stage pz; then
  section "Stock PZ server"
  extra=""
  for kv in $PZ_EXTRA_ENV; do extra+="'${kv%%=*}' => '${kv#*=}', "; done
  [ -n "$extra" ] && note "extra egg variables: $PZ_EXTRA_ENV"
  skip=false
  [ "$SKIP_INSTALL" = 1 ] && skip=true
  out=$(tinker "
    \$egg = app(App\Services\Eggs\Sharing\EggImporterService::class)->fromUrl('$EGG_URL');
    \$egg->features = array_values(array_unique(array_merge(\$egg->features ?? [], ['easy_pz'])));
    \$egg->save();
    \$extra = [$extra];
    foreach (\$extra as \$k => \$v) {
      \$egg->variables()->updateOrCreate(['env_variable' => \$k], ['name' => \$k, 'description' => 'added by the e2e test',
        'default_value' => \$v, 'user_viewable' => true, 'user_editable' => true, 'rules' => ['nullable', 'string']]);
    }
    \$ids = array_map(fn (\$p) => App\Models\Allocation::create(['node_id' => 1, 'ip' => '0.0.0.0', 'port' => \$p])->id, [16261, 16262, $PZMAN_PORT]);
    \$s = app(App\Services\Servers\ServerCreationService::class)->handle([
      'name' => 'e2e', 'owner_id' => 1, 'egg_id' => \$egg->id,
      'allocation_id' => \$ids[0], 'allocation_additional' => array_slice(\$ids, 1),
      'memory' => $PZ_MEMORY, 'swap' => 0, 'disk' => 0, 'io' => 500, 'cpu' => 0,
      'environment' => ['SERVER_NAME' => '$SERVER_NAME', 'ADMIN_USER' => 'admin', 'ADMIN_PASSWORD' => 'e2epw', 'STEAM_PORT' => '16262',
        'MAX_PLAYERS' => '8', 'SRCDS_APPID' => '380870', 'AUTO_UPDATE' => '0'] + \$extra,
      'start_on_completion' => false, 'skip_scripts' => $skip,
    ]);
    echo 'UUID=', \$s->uuid, PHP_EOL;")
  echo "$out" >>"$OUT/steps.log"
  pz=$(val UUID "$out")
  [ -n "$pz" ] || { fail "create the PZ server"; echo "$out" | tail -n 20 | block "tinker output"; exit 1; }
  d=$ROOT/wings/volumes/$pz
  if [ "$SKIP_INSTALL" = 1 ]; then
    check "server created without the egg's install" wait_for 300 "server ready" install_finished "$pz"
  else
    # SteamCMD sometimes fails an anonymous install ("Missing configuration")
    # and the egg still reports success: reinstall up to twice.
    for attempt in 1 2 3; do
      wait_for 2400 "PZ install finished (attempt $attempt)" install_finished "$pz"
      root test -x "$d/ProjectZomboid64" && break
      root tail -n 5 "$ROOT/wings/logs/install/$pz.log" 2>&1 | block "stock install attempt $attempt failed"
      [ "$attempt" = 3 ] && break
      tinker "$(daemon "$pz")->reinstall();" >>"$OUT/steps.log"
      sleep 10
    done
    check "stock install completes ($(server_status "$pz") after ${WAITED}s, attempt $attempt)" root test -x "$d/ProjectZomboid64"
    # Keep the game small on the runner.
    root sed -i 's/-Xmx[0-9]*[gGmM]/-Xmx3g/' "$d/ProjectZomboid64.json"
  fi
fi

# ---------------------------------------------------------------------------
EZ='Magne4000\EasyPz'
ez_php() { # PHP prefix: $s (server), $ez (EasyPzServer), $u (admin)
  printf '$s = %s; $ez = %s\\Models\\EasyPzServer::forServer($s); $u = App\\Models\\User::find(1);' "$(server "$pz")" "$EZ"
}
session_for() { # session_for TOKEN: pzman session from a handoff token
  curl -sS -X POST -H 'Content-Type: application/json' -d "{\"token\":\"$1\"}" "http://127.0.0.1:$PZMAN_PORT/api/auth/pelican" | jq -r '.token // empty'
}
api() { # api SESSION METHOD PATH [curl args]: pzman's API as the SPA calls it
  local s=$1 m=$2 p=$3
  shift 3
  curl -sS -m 60 -X "$m" -H "Authorization: Bearer $s" -H 'Content-Type: application/json' "http://127.0.0.1:$PZMAN_PORT/api$p" "$@"
}
code() { api "$@" -o /dev/null -w '%{http_code}'; }
all_running() { [[ $states =~ ^(running\ )+$ ]]; }

if has_stage easypz; then
  section "easy-pz: Enable, users, players, lifecycle, Disable"
  [ -n "${pz:-}" ] || pz=$(val UUID "$(tinker "echo 'UUID=', App\Models\Server::where('name', 'e2e')->sole()->uuid, PHP_EOL;")")
  d=$ROOT/wings/volumes/$pz
  short=${pz:0:8}

  t=$(now)
  out=$(tinker "\$s = $(server "$pz"); \$a = \$s->allocations()->where('port', $PZMAN_PORT)->sole();
    \$ez = app($EZ\Services\EasyPzService::class)->enable(\$s, \$a->id, null);
    echo 'VERSION=', \$ez->deployed_version, PHP_EOL;")
  echo "$out" >>"$OUT/steps.log"
  check "Enable deployed pzman ($(val VERSION "$out"))" test -n "$(val VERSION "$out")"
  check "pzman.env carries the public key, not a secret" public_key_only "$d/.easy-pz/pzman.env"
  st=$(tinker "\$s = $(server "$pz"); echo 'STARTUP=', \$s->startup, PHP_EOL, 'AUTO=', \$s->variables()->where('env_variable', 'AUTO_UPDATE')->first()?->server_value, PHP_EOL;")
  check "startup is ./.easy-pz/pzman and AUTO_UPDATE=0" test "$(val STARTUP "$st")|$(val AUTO "$st")" = "./.easy-pz/pzman|0"
  if wait_for 1500 "SERVER STARTED after Enable" started_or_exited "$pz" "$t" && in_logs_since "$pz" "$t" "SERVER STARTED"; then
    pass "Enable restarted the server into pzman; SERVER STARTED in Pelican's console (${WAITED}s)"
  else
    logs_since "$pz" "$t" | tail -n 40 | block "container output"
    fail "Enable restarted the server into pzman"
    exit 1
  fi
  logs_since "$pz" "$t" | head -n 20 | block "Pelican's console after Enable (first 20 lines)"
  wait_for 30 "Wings running" wings_is "$pz" running
  check "Wings state running" wings_is "$pz" running
  check "no JSON log lines in Pelican's console" bash -c "[ \"\$(docker logs $pz 2>&1 | grep -c '\"level\":')\" = 0 ]"
  check "pzman logs in .easy-pz/logs" root test -s "$d/.easy-pz/logs/pzman.log"
  check ".pelicanignore managed block" root grep -q 'BEGIN easy-pz (managed)' "$d/.pelicanignore"

  t=$(now)
  send "$pz" players
  sleep 10
  logs_since "$pz" "$t" | tail -n 5 | block "console after 'players'"
  check "'players' typed in Pelican's console gets the game's reply" in_logs_since "$pz" "$t" "Players connected"

  # Users: the handoff, the session, scopes.
  code=$(curl -s -o /dev/null -w '%{http_code} %{redirect_url}' "$panel_url/easy-pz/$short/open")
  check "Open easy-pz needs a panel login ($code)" bash -c "[[ '$code' == 302*login* ]]"
  token=$(val TOKEN "$(tinker "$(ez_php) echo 'TOKEN=', $EZ\Services\EasyPzService::mintToken(\$u, \$s, \$ez), PHP_EOL;")")
  curl -sS -D "$OUT/handoff.headers" -o "$OUT/handoff.json" -X POST -H 'Content-Type: application/json' -d "{\"token\":\"$token\"}" \
    "http://127.0.0.1:$PZMAN_PORT/api/auth/pelican"
  owner=$(jq -r '.token // empty' "$OUT/handoff.json")
  check "the owner's handoff token gives a pzman session" test -n "$owner"
  check "no Set-Cookie on the handoff" bash -c "[ \"\$(grep -ci '^set-cookie' '$OUT/handoff.headers')\" = 0 ]"
  replay=$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d "{\"token\":\"$token\"}" "http://127.0.0.1:$PZMAN_PORT/api/auth/pelican")
  check "handoff token is single-use (replay: $replay)" test "$replay" = 401
  login=$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d '{"username":"admin","password":"e2epw"}' "http://127.0.0.1:$PZMAN_PORT/api/auth/login")
  check "password login is off (HTTP $login)" test "$login" != 200
  sys=$(api "$owner" GET /system)
  check "pzman runs in Pelican mode" test "$(jq -r .host <<<"$sys")" = pelican
  stop=$(code "$owner" POST /server/stop -d '{}')
  check "easy-pz's Stop is refused, Pelican's Stop is the way (HTTP $stop)" test "$stop" = 409
  svc=$(tinker "$(ez_php) echo 'STATE=', (new $EZ\Services\PzmanClient(\$s, \$ez, \$u))->get('/server/status')['state'] ?? '?', PHP_EOL;")
  check "the plugin's service call reaches pzman (state $(val STATE "$svc"))" test "$(val STATE "$svc")" = running

  out=$(tinker "$(ez_php) \$r = App\Models\User::firstOrCreate(['email' => 'reader@e2e.test'], ['uuid' => (string) Illuminate\Support\Str::uuid(),
      'username' => 'e2e-reader', 'password' => bcrypt(Illuminate\Support\Str::random(32))]);
    App\Models\Subuser::firstOrCreate(['user_id' => \$r->id, 'server_id' => \$s->id], ['permissions' => ['easy-pz.read']]);
    echo 'TOKEN=', $EZ\Services\EasyPzService::mintToken(\$r, \$s->fresh(), \$ez), PHP_EOL;")
  echo "$out" >>"$OUT/steps.log"
  reader=$(session_for "$(val TOKEN "$out")")
  check "a subuser with easy-pz.read only gets a session" test -n "$reader"
  check "… which reads" test "$(code "$reader" GET /server/status)" = 200
  denied=$(code "$reader" POST /backups -d '{}')
  check "… and cannot change anything (backup: HTTP $denied)" test "$denied" = 403

  # Players: the mod page and the launcher's requests through the panel.
  t=$(now)
  api "$owner" POST /mods -d "{\"workshopIds\":[\"$MOD_ID\"]}" >>"$OUT/steps.log"
  check "Workshop download through pzman" wait_for 300 "Workshop item downloaded" in_logs_since "$pz" "$t" "Downloaded item $MOD_ID"
  page="$panel_url/easy-pz/$short$(jq -r .modsPagePath <<<"$sys")"
  pack_ready() { curl -s "${page}data.json" | jq -e '(.items | length > 0) and ([.items[].download.ready] | all)' >/dev/null; }
  wait_for 300 "mod pack ready" pack_ready
  curl -s -D "$OUT/public.headers" "${page}data.json" -o "$OUT/data.json"
  check "data.json through the panel" jq -e '.items | length > 0' "$OUT/data.json"
  check "data.json links carry the panel prefix" bash -c "jq -r '.items[0].download.url' '$OUT/data.json' | grep -c '/easy-pz/$short/mods/' >/dev/null"
  check "public responses are sandboxed" bash -c "grep -ci '^content-security-policy: sandbox' '$OUT/public.headers' >/dev/null && grep -ci '^access-control-allow-origin: \*' '$OUT/public.headers' >/dev/null"
  asset=$(curl -s "$page" | grep -o "/easy-pz/$short/assets/[^\"]*\.js" | head -n1)
  check "the page's assets load through the panel ($asset)" bash -c "[ -n '$asset' ] && [ \"\$(curl -s -o /dev/null -w '%{http_code}' '$panel_url$asset')\" = 200 ]"
  url=$(jq -r '.items[0].download.url' "$OUT/data.json")
  case $url in http*) ;; *) url="$panel_url$url" ;; esac
  want=$(jq -r '.items[0].download.sha256' "$OUT/data.json")
  curl -s -o "$OUT/pack.zip" "$url"
  check "pack download through the panel matches its sha256" test "$(sha256sum "$OUT/pack.zip" | cut -d' ' -f1)" = "$want"
  check "Range through the panel answers 206" test "$(curl -s -o /dev/null -w '%{http_code}' -r 10-99 "$url")" = 206
  size=$(wc -c <"$OUT/pack.zip")
  curl -s -r "0-$((size / 2 - 1))" -o "$OUT/resumed.zip" "$url"
  curl -s -C - -o "$OUT/resumed.zip" "$url"
  check "a cut download resumes through the panel" cmp -s "$OUT/pack.zip" "$OUT/resumed.zip"
  files_url=$(jq -r '.items[0].files' "$OUT/data.json")
  case $files_url in http*) ;; *) files_url="$panel_url$files_url" ;; esac
  body=$(curl -s "$files_url" | jq -c '{sha256: .sha256, files: [0]}')
  check "the launcher's POST files/<id> through the panel" test "$(curl -s -o /dev/null -w '%{http_code}' -X POST -H 'Content-Type: application/json' -d "$body" "$files_url")" = 200

  # An update window restarts the game inside the container: Pelican keeps
  # showing it running.
  t=$(now)
  tinker "$(ez_php) (new $EZ\Services\PzmanClient(\$s, \$ez, \$u))->post('/updates/apply', ['force' => true]);" >>"$OUT/steps.log"
  states=""
  for _ in $(seq 1 60); do
    states+="$(wings_state "$pz") "
    in_logs_since "$pz" "$t" "SERVER STARTED" && break
    sleep 10
  done
  note "Wings states during the update window: $states"
  check "the plugin's update window restarted PZ" in_logs_since "$pz" "$t" "SERVER STARTED"
  check "Wings stayed running" all_running

  # Pelican's Stop sends "quit": pzman saves, stops the game and exits 0.
  t=$(now)
  before=$(crashes)
  power "$pz" stop
  check "Pelican's Stop stops pzman" wait_for 300 "exited" exited "$pz"
  check "exit code 0 ($(container_state "$pz"))" test "$(container_state "$pz")" = "exited 0"
  logs_since "$pz" "$t" | tail -n 10 | block "Pelican's console after Stop"
  check "world saved on Stop" changed_since "$d/.cache/Saves/Multiplayer/$SERVER_NAME" "$t"
  check "no crash handling after Stop" test "$(crashes)" = "$before"

  # Disable: the stock egg runs the same server, same world.
  worldgen=$d/.cache/Saves/Multiplayer/$SERVER_NAME/map_worldgen.bin
  world=$(root sha256sum "$worldgen" 2>/dev/null | cut -d' ' -f1)
  out=$(tinker "$(ez_php) app($EZ\Services\EasyPzService::class)->disable(\$s, \$ez); \$s->refresh(); echo 'STARTUP=', \$s->startup, PHP_EOL;")
  echo "$out" >>"$OUT/steps.log"
  check "Disable restores the egg's startup command" bash -c "grep -c 'ProjectZomboid64' <<<'$(val STARTUP "$out")' >/dev/null"
  t=$(now)
  power "$pz" start
  if wait_for 900 "stock egg started again" started_or_exited "$pz" "$t" && in_logs_since "$pz" "$t" "SERVER STARTED"; then
    pass "the stock egg runs the same server again after Disable"
    check "world kept (map_worldgen.bin unchanged)" same_hash "$worldgen" "$world"
  else
    logs_since "$pz" "$t" | tail -n 30 | block "stock start output"
    fail "the stock egg runs the same server again after Disable"
  fi
  power "$pz" stop
  wait_for 300 "stopped" exited "$pz"
fi

section "Result"
if [ "$FAILED" = 0 ]; then note "all checks passed"; else note "some checks failed"; fi
exit "$FAILED"
