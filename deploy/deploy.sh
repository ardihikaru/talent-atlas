#!/usr/bin/env bash
# Deploy main to prod: pull on rnd-v2-vpn, rebuild, wait for health, check the public URL.
# Usage: deploy/deploy.sh
set -euo pipefail

HOST=${HOST:-rnd-v2-vpn}
DIR=${DIR:-/home/sismedika/devel/projects/talent-atlas}
URL=${URL:-https://talent.mfardiansyah.id}

ssh "$HOST" bash -s -- "$DIR" <<'REMOTE'
set -euo pipefail
cd "$1"
test -f .env || { echo ".env missing in $1 (see deploy/README.md)"; exit 1; }
git fetch --quiet origin main
git checkout --quiet main
git merge --ff-only --quiet origin/main
git log --oneline -1
docker compose up -d --build app
for i in $(seq 1 40); do
  state=$(docker inspect -f '{{.State.Health.Status}}' "$(docker compose ps -q app)")
  [ "$state" = healthy ] && break
  sleep 3
done
docker compose logs --tail 20 app
[ "$state" = healthy ] || { echo "app not healthy: $state"; exit 1; }
REMOTE

code=$(curl -s -o /dev/null -w '%{http_code}' "$URL/healthz")
echo "public $URL/healthz -> $code"
[ "$code" = 200 ]
