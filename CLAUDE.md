# Talent Atlas

A graph-based map of a child's education, competitions and sports from primary school to university, with AI suggestions that surface where their talent shows and what to do next. (ID: peta berbasis graf perjalanan pendidikan, lomba, dan olahraga anak dari SD sampai kuliah.)

**Use the `talent-atlas-coding` skill for any coding task here** (it pulls in Emil Kowalski's UI skills for front-end work).

## Layout

| Path | What |
|---|---|
| `backend/` | Go service (stdlib only): ArcadeDB HTTP client, migrations, catalog seed, auth, onboarding + graph API, embedded web pages |
| `backend/migrations/` | Append-only SQL migrations, applied at boot |
| `backend/web/` | `onboarding.html` (wizard), `graph.html` (the talent map), `index.html` (home/login) |
| `docker-compose.yml` | `app`; plus `arcadedb` under the `local-db` profile for local work |
| `deploy/` | nginx vhost reference copy and deploy runbook |
| `artifacts/v1/` | Original design artifacts (read-only reference; do not edit) |

## Commands

```sh
cp .env.example .env                              # then fill secrets
docker compose --profile local-db up -d --build   # local app + ArcadeDB 26.7.2
docker compose logs -f app
docker run --rm -v "$PWD/backend":/src -w /src golang:1.24-alpine sh -c 'go vet ./... && go test ./...'
```

Local Go may be < 1.24 (the code uses `crypto/pbkdf2`), hence the container for tests.

## Data and auth

- ArcadeDB database (prod: `talent_atlas` on the shared server). Vertex/edge schema in `backend/migrations/001_schema.sql`.
- Login is username + password. The admin account is created/updated at boot from `ADMIN_USERNAME` / `ADMIN_PASSWORD` in `.env`. Session = HMAC-signed cookie (`SESSION_SECRET`).

## Deploy topology

- `ssh rnd-v2-vpn` (10.10.0.4): app container, repo at `/home/sismedika/devel/projects/talent-atlas`, bound to `10.10.0.4:18201`.
- `ssh clinodb` (10.10.0.5): shared ArcadeDB 26.7.2 (`/home/clinoid/devel/projects/shared-infra`), reached at `http://10.10.0.5:2480`.
- `ssh clino`: public nginx, `/etc/nginx/conf.d/talent.conf` → `https://talent.mfardiansyah.id`.

Details and commands: `deploy/README.md`.

## Workflow

Git worktrees per branch, small PRs, squash-merge with branch deletion, then deploy main. The repo is **public**: never commit `.env`, passwords or hostnames' credentials.
