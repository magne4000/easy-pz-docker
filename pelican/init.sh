#!/bin/sh
# Runs as root before panel and Wings on every `docker compose up`. Each step
# runs once; the image entrypoint has already generated APP_KEY and run migrations.
# shellcheck disable=SC2016 # PHP code in single quotes
set -eu
cd /var/www/html
state=/pelican-data/init
mkdir -p "$state"

if [ ! -e "$state/admin" ]; then
  php artisan p:user:make -n --admin=1 --email="$PANEL_ADMIN_EMAIL" \
    --username="$PANEL_ADMIN_USER" --password="$PANEL_ADMIN_PASSWORD"
  touch "$state/admin"
fi

if [ ! -e "$state/node" ]; then
  php artisan p:node:make -n --name=local --description="Wings in this compose project" \
    --fqdn="$WINGS_FQDN" --scheme=http --public=1 --proxy=0 --maintenance=0 \
    --maxMemory=0 --overallocateMemory=-1 --maxDisk=0 --overallocateDisk=-1 \
    --maxCpu=0 --overallocateCpu=-1 --uploadSize=256 \
    --daemonListeningPort=8080 --daemonConnectingPort="$WINGS_PORT" \
    --daemonSFTPPort="$WINGS_SFTP_PORT" --daemonBase="$PELICAN_ROOT/volumes"
  touch "$state/node"
fi

# Wings rewrites its config at boot: write it only once.
cfg="$PELICAN_ROOT/config.yml"
if [ ! -e "$cfg" ]; then
  id=$(php artisan p:node:list --format=json |
    php -r 'foreach (json_decode(stream_get_contents(STDIN), true) as $n) if ($n["name"] === "local") echo $n["id"];')
  # JSON is valid YAML. Every path Wings mounts into game containers goes under
  # PELICAN_ROOT, the directory bind-mounted at the same path.
  php artisan p:node:configuration "$id" --format=json | php -r '
    $c = json_decode(stream_get_contents(STDIN), true, flags: JSON_THROW_ON_ERROR);
    $r = getenv("PELICAN_ROOT");
    $c["system"] += [
      "root_directory" => $r,
      "log_directory" => "$r/logs",
      "archive_directory" => "$r/archives",
      "tmp_directory" => "$r/tmp",
      "backup_directory" => "/backups",
      "user" => ["passwd" => ["directory" => "$r/etc"]],
      "machine_id" => ["directory" => "$r/etc/machine-id"],
    ];
    echo json_encode($c, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES), "\n";
  ' > "$cfg.tmp"
  mv "$cfg.tmp" "$cfg"
fi

# The panel runs as www-data. This includes plugins/ (the image entrypoint
# creates it), which the panel also mounts on /var/www/html/plugins: the
# panel's plugin import and `php artisan p:plugin:install` write there.
chown -R www-data:www-data /pelican-data
