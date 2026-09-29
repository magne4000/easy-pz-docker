# syntax=docker/dockerfile:1.7
# Stages: UI → static Go binary → SteamCMD bootstrap → Debian slim runtime.

FROM --platform=$BUILDPLATFORM oven/bun:1 AS ui
WORKDIR /src/web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
COPY api/ ../api/
# vite writes into ../internal/webui/dist (the go:embed package)
RUN mkdir -p ../internal/webui/dist && bun run build

FROM --platform=$BUILDPLATFORM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=ui /src/internal/webui/dist/ internal/webui/dist/
ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64
# CGO_ENABLED=0 is why the store uses the pure-Go SQLite driver.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/pzman ./cmd/pzman

# Valve's installer tarball holds only the 32-bit bootstrapper; its first run
# self-updates into the full package, which ships a 64-bit linux64/steamcmd.
# Bootstrap here, where the 32-bit runtime lives, so the runtime image needs none.
FROM debian:trixie-slim AS steamcmd
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl lib32gcc-s1 lib32stdc++6; \
    mkdir -p /opt/steamcmd; \
    curl -fsSL https://steamcdn-a.akamaihd.net/client/installer/steamcmd_linux.tar.gz | tar -xz -C /opt/steamcmd; \
    HOME=/tmp/steam /opt/steamcmd/steamcmd.sh +quit || true; \
    test -x /opt/steamcmd/linux64/steamcmd

FROM debian:trixie-slim
# tini reaps orphaned grandchildren (steamcmd helpers, the JVM's children) for us.
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends ca-certificates curl tini libsdl2-2.0-0 procps; \
    rm -rf /var/lib/apt/lists/*

# STEAM_PLATFORM makes steamcmd.sh run the 64-bit linux64/steamcmd (pzman passes
# its environment on); the 32-bit one also segfaults under Rosetta.
ENV STEAM_PLATFORM=linux64 \
    PANEL_STEAMCMD=/opt/steamcmd/steamcmd.sh \
    PANEL_STEAM_HOME=/var/lib/pzman/steam \
    PANEL_CACHE_DIR=/var/lib/pzman/cache \
    PANEL_INSTALL_DIR=/project-zomboid \
    PANEL_DATA_DIR=/project-zomboid-config \
    PANEL_BACKUP_DIR=/backups \
    PANEL_DB_PATH=/project-zomboid-config/pzman.db

COPY --from=steamcmd /opt/steamcmd /opt/steamcmd
# Prove the 64-bit binary runs and warm its Steam home, so the first boot does
# not depend on a self-update. "Unloading Steam API" only follows a session that
# loaded; the banner is on stdout (with 2>&1 it is lost) and its "OK" may be
# interleaved with IPC warnings.
RUN set -eux; \
    mkdir -p "$PANEL_STEAM_HOME" "$PANEL_CACHE_DIR"; \
    HOME="$PANEL_STEAM_HOME" /opt/steamcmd/steamcmd.sh +quit >/tmp/steamcmd.log || true; \
    cat /tmp/steamcmd.log; \
    grep -q 'Unloading Steam API' /tmp/steamcmd.log; \
    rm /tmp/steamcmd.log

COPY --from=build /out/pzman /usr/local/bin/pzman

EXPOSE 8080/tcp 16261/udp 16262/udp
VOLUME ["/project-zomboid", "/project-zomboid-config", "/backups"]
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s CMD curl -fsS http://127.0.0.1:${PANEL_PORT:-8080}/livez || exit 1
# pzman runs as root only to chown what is wrong; the game and SteamCMD run as PUID/PGID.
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/pzman"]
