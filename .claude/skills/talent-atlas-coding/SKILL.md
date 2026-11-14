---
name: talent-atlas-coding
description: How to write, test, ship and deploy code in the Talent Atlas repo (Go backend on ArcadeDB, embedded HTML/JS pages, docker compose). Use for any coding task in this repo - backend handlers, migrations, Cypher/SQL, the graph or onboarding pages, compose, deploy. Inherits Emil Kowalski's design-engineering skills for every UI change.
---

# Coding in Talent Atlas

Talent Atlas maps a child's education, competitions and sports from primary school to university as a graph, and uses AI to show where talent shows and what to do next. Read `CLAUDE.md` first for layout and commands.

## Inherited skills (UI work)

Every change that touches what a person sees or touches in `backend/web/*` follows Emil Kowalski's skills, vendored in `.claude/skills/` (source: https://emilkowal.ski/skill, MIT, see `.claude/vendor/`):

| Situation | Load |
|---|---|
| Any component, form, page polish, press/hover states, popovers | `emil-design-eng` |
| Adding motion | `animate`, then `choreograph` if several things move |
| Motion feels slow / too much | `remove-animations` |
| Reviewing motion in a diff | `review-animations` |
| Janky motion | `animation-performance` |
| New list/table/card UI before merge | `break-ui` (long Indonesian names, empty graph, 0 children, 20 sports) |
| Phone layout | `mobile-native` |

House rules on top of those: no frameworks or build step (plain HTML/CSS/JS embedded in the Go binary), CSS tokens on `:root` with a dark-mode override, `prefers-reduced-motion` respected, ease-out `cubic-bezier(0.23,1,0.32,1)`, UI copy in **Indonesian**, code/comments/commits in English.

## Backend rules

1. **Go standard library only.** No third-party modules; `go.mod` stays dependency-free.
2. **ArcadeDB through `backend/arcadedb.go`** (`db.Run`, `db.Count`, `SQL(...)`, `Cypher(...)`). Pass values as parameters, never string-concatenate user input into a query.
3. **Migrations are append-only**: add `backend/migrations/NNN_name.sql`; never edit an applied one. They run automatically at boot (`Migrate`), recorded in `SchemaMigration`.
4. **Real ArcadeDB is the test that counts.** The unit tests use an HTTP mock; any new Cypher/SQL must also be run against ArcadeDB **26.7.2** (prod version): `docker compose --profile local-db up -d --build`.
5. **Secrets only in `.env`** (gitignored; the repo is public). New settings go in `.env.example` with an empty or safe value and a comment.
6. Handlers return JSON via `writeJSON`; auth-only routes wrap with `s.auth(...)`.

## Checks before every commit

```sh
docker run --rm -v "$PWD/backend":/src -w /src golang:1.24-alpine sh -c 'go vet ./... && go test ./...'
```
(or `cd backend && go vet ./... && go test ./...` with Go >= 1.24 installed)

## Shipping flow

1. Work in a worktree: `git worktree add ../talent-atlas-wt/<branch> -b <type>/<topic>` (types: feat, fix, chore, docs).
2. Small PRs: `gh pr create`, then `gh pr merge --squash --delete-branch`; `git worktree remove` and `git pull` on main.
3. Deploy main: see `deploy/README.md` (app on `ssh rnd-v2-vpn`, ArcadeDB on `ssh clinodb`, nginx on `ssh clino`).
