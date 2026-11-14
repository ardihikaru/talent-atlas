# Talent Atlas

A graph-based map of a child's education, competitions, and sports from primary school to university, with AI suggestions that surface where their talent shows and what to do next.

> **ID:** Talent Atlas adalah peta berbasis graf yang merekam perjalanan pendidikan, lomba, dan olahraga anak dari SD sampai kuliah, lalu memakai AI untuk menunjukkan di mana bakatnya terlihat dan langkah apa yang sebaiknya diambil berikutnya.

Live: https://talent.mfardiansyah.id

## What it does

- **Onboarding** — a short wizard captures the child, current school stage, school, abilities, interests, sports, methods, competitions and goals.
- **Graph** — everything is stored as a graph in [ArcadeDB](https://arcadedb.com): stages, schools, competencies, interests, sports, competitions, observations, majors and careers, linked by typed edges.
- **Talent map** — an interactive map from Fase A (SD) to university shows where evidence of talent accumulates, and (next) AI suggestions on what to do next.

## Architecture

```
browser ──HTTPS──> nginx (edge) ──> app (Go, stdlib only) ──HTTP API──> ArcadeDB
                                      ├─ /onboarding  wizard
                                      ├─ /graph       talent map
                                      └─ /api/*       auth, onboarding, children, graph
```

The Go service waits for ArcadeDB, creates the database if missing, runs migrations, seeds the catalog, ensures the admin account, then serves the pages and API.

## Run locally

Requirements: Docker with Compose.

```sh
cp .env.example .env
# fill ARCADEDB_PASSWORD, SESSION_SECRET (openssl rand -hex 32), ADMIN_USERNAME, ADMIN_PASSWORD
docker compose --profile local-db up -d --build
open http://localhost:8080
```

Sign in with `ADMIN_USERNAME` / `ADMIN_PASSWORD`, then complete onboarding.

## Tests

```sh
cd backend && go vet ./... && go test ./...   # Go >= 1.24
```

## Deploy

See [`deploy/README.md`](deploy/README.md).

## Repository

| Path | What |
|---|---|
| `backend/` | Go backend and embedded web pages |
| `docker-compose.yml` | App, plus a local ArcadeDB profile |
| `deploy/` | nginx vhost and deploy runbook |
| `artifacts/v1/` | Original design artifacts |
| `.claude/` | Claude Code skills for this repo |
