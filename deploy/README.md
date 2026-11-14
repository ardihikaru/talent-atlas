# Deploying Talent Atlas

```
browser ─HTTPS─> clino (nginx, talent.conf) ─wg─> rnd-v2-vpn 10.10.0.4:18201 (app) ─wg─> clinodb 10.10.0.5:2480 (ArcadeDB)
```

| Host | Role | Where |
|---|---|---|
| `ssh clino` | Public nginx edge, TLS | `/etc/nginx/conf.d/talent.conf` (reference copy: `deploy/nginx/talent.conf`) |
| `ssh rnd-v2-vpn` | App container | `/home/sismedika/devel/projects/talent-atlas`, `.env` beside `docker-compose.yml` |
| `ssh clinodb` | Shared ArcadeDB 26.7.2 | `/home/clinoid/devel/projects/shared-infra`, database `talent_atlas` |

The app reaches ArcadeDB at `http://10.10.0.5:2480`, published on the WireGuard address by the `gaos-arcade-bridge` socat container on clinodb (it forwards to `arcadedb-master` on `master-compose-ai-net`). If that bridge is removed, Talent Atlas needs its own.

## Routine deploy

From a checkout of main:

```sh
deploy/deploy.sh
```

It fast-forwards the clone on rnd-v2-vpn, rebuilds the `app` container, waits for the health check, and calls `https://talent.mfardiansyah.id/healthz`. Migrations and catalog seeds run automatically when the app starts; check the log for `migration applied:` / `catalog seeded:` lines.

## First-time setup (done 2026-11-14, kept for rebuilds)

### 1. Database and user on clinodb

Use the shared root password from `shared-infra/.env` without printing it. Create the database and a user that can only reach it:

```sh
ssh clinodb
cd ~/devel/projects/shared-infra
ROOT=$(grep ^ARCADEDB_ROOT_PASSWORD= .env | cut -d= -f2-)
PW=$(openssl rand -hex 24)     # keep for step 2
api() { curl -s -u "root:$ROOT" -H 'Content-Type: application/json' http://127.0.0.1:2480/api/v1/server -d "$1"; }
api '{"command":"create database talent_atlas"}'
api "{\"command\":\"create user {\\\"name\\\":\\\"talent_atlas\\\",\\\"password\\\":\\\"$PW\\\",\\\"databases\\\":{\\\"talent_atlas\\\":[\\\"admin\\\"]}}\"}"
```

### 2. `.env` on rnd-v2-vpn

`chmod 600 .env`. Never commit it.

```sh
ARCADEDB_URL=http://10.10.0.5:2480
ARCADEDB_DATABASE=talent_atlas
ARCADEDB_USER=talent_atlas
ARCADEDB_PASSWORD=<from step 1>
SESSION_SECRET=<openssl rand -hex 32>
ADMIN_USERNAME=ardi
ADMIN_PASSWORD='<admin password>'
APP_BIND=10.10.0.4
APP_PORT=18201
COOKIE_SECURE=true
TZ=Asia/Jakarta
```

Changing `ADMIN_PASSWORD` and restarting re-hashes the admin's password; nothing else changes.

### 3. nginx and certificate on clino

1. Copy `deploy/nginx/talent.conf` to `/etc/nginx/conf.d/talent.conf` with the 443 server block commented out.
2. `sudo nginx -t && sudo systemctl reload nginx`
3. `sudo certbot certonly --webroot -w /var/www/acme --cert-name talent -d talent.mfardiansyah.id --agree-tos --non-interactive --deploy-hook 'nginx -t && systemctl reload nginx'`
4. Restore the 443 block, `sudo nginx -t && sudo systemctl reload nginx`.

## Checks

```sh
curl -sI https://talent.mfardiansyah.id/ | head -1              # 302 to /onboarding or 200
curl -s https://talent.mfardiansyah.id/api/setup/status
ssh rnd-v2-vpn 'cd ~/devel/projects/talent-atlas && docker compose ps && docker compose logs --tail 30 app'
```

## Backups

The data lives in the shared ArcadeDB volume on clinodb (`shared-infra/data/arcadedb/databases/talent_atlas`). Back it up with the shared-infra routine, e.g. `BACKUP DATABASE` through the ArcadeDB API as root.
