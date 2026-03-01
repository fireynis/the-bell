# Create Project Directory Structure — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Create the full Go project directory layout with `.gitkeep` sentinel files so downstream tasks (config, domain types, migrations, Docker Compose) can begin immediately.

**Architecture:** Standard Go project layout — `internal/` for private packages organized by layer (config, server, middleware, handler, domain, service, repository), plus top-level directories for migrations, sqlc queries, Kratos config, and web frontend assets.

**Tech Stack:** Just filesystem and git — no code in this task.

**Beads task:** `the-bell-zhd.3`

---

## Task 1: Create internal package directories

**Files:**
- Create: `internal/config/.gitkeep`
- Create: `internal/server/.gitkeep`
- Create: `internal/middleware/.gitkeep`
- Create: `internal/handler/.gitkeep`
- Create: `internal/domain/.gitkeep`
- Create: `internal/service/.gitkeep`
- Create: `internal/repository/postgres/.gitkeep`
- Create: `internal/repository/redis/.gitkeep`
- Create: `internal/storage/.gitkeep`

**Step 1: Create all internal directories with `.gitkeep` files**

```bash
cd /home/jeremy/services/the-bell
mkdir -p internal/{config,server,middleware,handler,domain,service,repository/postgres,repository/redis,storage}
for dir in internal/config internal/server internal/middleware internal/handler internal/domain internal/service internal/repository/postgres internal/repository/redis internal/storage; do
  touch "$dir/.gitkeep"
done
```

**Step 2: Verify directory structure**

```bash
find internal -type f | sort
```

Expected output:
```
internal/config/.gitkeep
internal/domain/.gitkeep
internal/handler/.gitkeep
internal/middleware/.gitkeep
internal/repository/postgres/.gitkeep
internal/repository/redis/.gitkeep
internal/server/.gitkeep
internal/service/.gitkeep
internal/storage/.gitkeep
```

---

## Task 2: Create top-level project directories

**Files:**
- Create: `migrations/.gitkeep`
- Create: `queries/.gitkeep`
- Create: `kratos/.gitkeep`
- Create: `web/.gitkeep`

**Step 1: Create directories and `.gitkeep` files**

```bash
cd /home/jeremy/services/the-bell
mkdir -p migrations queries kratos web
touch migrations/.gitkeep queries/.gitkeep kratos/.gitkeep web/.gitkeep
```

**Step 2: Verify**

```bash
for dir in migrations queries kratos web; do
  ls -la "$dir/.gitkeep"
done
```

Expected: All four `.gitkeep` files exist.

---

## Task 3: Verify full structure and commit

**Step 1: Verify the complete directory tree**

```bash
cd /home/jeremy/services/the-bell
find . -name '.gitkeep' | sort
```

Expected output (13 files):
```
./internal/config/.gitkeep
./internal/domain/.gitkeep
./internal/handler/.gitkeep
./internal/middleware/.gitkeep
./internal/repository/postgres/.gitkeep
./internal/repository/redis/.gitkeep
./internal/server/.gitkeep
./internal/service/.gitkeep
./internal/storage/.gitkeep
./kratos/.gitkeep
./migrations/.gitkeep
./queries/.gitkeep
./web/.gitkeep
```

**Step 2: Verify build still passes**

```bash
go build ./cmd/bell/
```

Expected: Builds successfully (new empty directories don't break anything).

**Step 3: Clean up build artifact**

```bash
rm -f bell
```

**Step 4: Stage and commit**

```bash
git add internal/ migrations/ queries/ kratos/ web/
git commit -m "feat: create project directory structure

Adds internal/{config,server,middleware,handler,domain,service,
repository/postgres,repository/redis,storage}, migrations/,
queries/, kratos/, web/ with .gitkeep sentinels."
```

**Step 5: Verify clean working tree**

```bash
git status
```

Expected: nothing to commit, working tree clean (aside from untracked `.beads/` and `AGENTS.md`).

---

## Edge Cases

1. **`bell` binary in repo root** — There's a compiled binary at `/home/jeremy/services/the-bell/bell` that should be cleaned up. It's already in `.gitignore` so it won't be committed, but `rm -f bell` in Step 3 handles it.
2. **No code to break** — This is purely directory scaffolding; there's no logic to test. The only verification is that `go build` still works.
3. **`.gitkeep` convention** — These are empty sentinel files that Git needs to track empty directories. They'll be deleted naturally as real `.go` files are added in subsequent tasks.

## Test Strategy

- **Build check**: `go build ./cmd/bell/` must still succeed after adding directories.
- **Structure check**: `find . -name '.gitkeep' | wc -l` should equal 13.
- No unit tests — this task creates no Go code.

## Downstream Unblocking

Completing this task unblocks:
- `the-bell-zhd.4` — Configuration loading (`internal/config/`)
- `the-bell-zhd.5` — Domain types (`internal/domain/`)
- `the-bell-zhd.6` — Database migrations (`migrations/`)
- `the-bell-zhd.7` — Docker Compose setup (needs `kratos/`, overall structure)
