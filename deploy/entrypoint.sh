#!/bin/sh
set -eu
umask 007
if [ "$(id -u)" = 0 ] && { [ "$1" = caddy ] || [ "$1" = manager ]; }; then
  mkdir -p /var/lib/manager/secrets /srv/snapshots /run/caddy /data /config
  chown -R 10001:10001 /var/lib/manager /srv/snapshots /run/caddy /data /config
  chmod 700 /var/lib/manager /var/lib/manager/secrets /srv/snapshots /data /config
  chmod 770 /run/caddy
fi
cloudflare_token_path="${CLOUDFLARE_API_TOKEN_FILE:-${DATA_DIR:-/var/lib/manager}/secrets/cloudflare_token}"
if [ -f "$cloudflare_token_path" ]; then
  CLOUDFLARE_API_TOKEN=$(cat "$cloudflare_token_path")
  export CLOUDFLARE_API_TOKEN
fi
if [ "$1" = caddy ] && [ "${TEST_TLS:-false}" != true ]; then
  manager check-secret
fi
if [ "$(id -u)" = 0 ] && { [ "$1" = caddy ] || [ "$1" = manager ]; }; then
  exec su-exec 10001:10001 "$@"
fi
exec "$@"
