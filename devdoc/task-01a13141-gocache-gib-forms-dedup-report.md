<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# Task Report: deduplicate the orphaned gocache GiB-flag branch onto master's already-typed three

**Task**: `01a13141614272db8d96e0ede7ddb436` · branch `task/01a13141614272db8d96e0ede7ddb436`

## Completed

- **`cmd/agent/cli_scratch_gc.go`** — landed the genuinely-missing half of the
  orphaned branch `task/01a1310d8cc87b70b558bf7639593264` (tip `770ae6da4`): the
  **11 lines of usage text that ADVERTISE `--max-gib GiB` and `--headroom-gib GiB`**.
  Both flags were already parsed, resolved and printed on master (`b511fd90`'s
  predecessor landed the arm); only the advertisement was absent, so the two forms
  were invisible on `gopi tool scratch-gc -h`. Flag spellings are taken from the
  arm's own constants (`scratchGCGocacheMaxGiBFlag`,
  `scratchGCGocacheHeadroomGiBFlag`, `scratch.ToolchainCacheMaxEnvName`) — no bare
  literals (Rules 1/13/14).
- **`cmd/agent/cli_tools_maintenance_help_surface_cases_test.go`** — added the **two
  extra typed `g6Cases`** (`--max-gib GiB`, `--headroom-gib GiB`) **deduplicated
  against master's already-landed three** (bare, `--path DIR`, `--trim` already on
  master, NOT re-added). Reuses master's `g6SeedGocacheFixture` and its derived
  byte tokens rather than duplicating the branch's parallel fixture — the branch's
  second fixture was dropped entirely. New code: two case entries, five constants
  (`g6GocacheBoundGiB` 9, `g6GocacheEnvBoundGiB` 3, `g6GocacheHeadroomGiB` 1e6),
  two derived byte helpers, one env builder (`g6GocacheEnvBoundEnv`), and two
  tokens derived from the arm's own `scratch.FormatBytes`.
  - Both cases are **falsifiers**, not shape checks. `--max-gib` exports
    `GOPI_BUILD_CACHE_MAX_GIB=3` while the form says `9`, so the printed
    `bound=` separates "the flag won" from "the environment did" — a form that
    accepted the flag and ignored it prints the environment's number and fails.
    `--headroom-gib` carries a floor no filesystem satisfies, so the low-headroom
    verdict is reached on every host, and the assertion reads the flag's operand
    back out of the arm's own verdict sentence.
- **`devdoc/gopi-tools-maintenance-help-surface-inventory.md`** — regenerated
  through `scripts/g6-inventory-doc.py` (Rule 38: the census figure names its
  generator) at the merged baseline: the surface is now **one form set** — 175
  rows / 171 distinct / 140 typed / 31 blocked (master was 173/169/138/31; the
  branch alone was 175/171/140/31 *at its own older baseline*). No stale numbering:
  the branch's own row numbering, which conflicted with master's, is gone — the
  doc is a pure function of the live surface.
- **`internal/docclass/testdata/anchor-number-baseline.tsv`** — regenerated with
  its generator (`go run internal/docclass/testdata/anchor-number-census.go`).
- **`COMMANDS.md`** — regenerated with its generator
  (`go run internal/standards/testdata/commands-census.go . COMMANDS.md`); the only
  change is the `cli_scratch_gc.go:614` → `:625` anchor line number that the usage
  text's 11 new lines produced. DOC-INDEX needed no change (`classify.py --check`:
  `0 gap(s)`).

## Not Done (honest)

- **`anchor-number-census --check` still exits 1 with 3 "REASON NOT REPRODUCIBLE"
  rows.** This is **not introduced by this run** — it is red on pristine `HEAD`
  too, and it is **not fixable by regeneration** (which is what the message
  advises). Root cause measured, filed as a bug row
  **`01a131ac-f2d7-724e-ac04-0b4848cfdfa4`** (tags `bug`): the census writer's
  carry-forward tests only the *provenance half* of a stored reason
  (`internal/docclass/testdata/anchor-number-census.go:252`), while the reader
  compares the *whole* reason
  (`anchorNumberReasonMismatches`, `internal/docclass/anchor_number_baseline_table.go:418`)
  — so a row whose `verdict` changed class keeps a stale **class tail**, and every
  regeneration re-carries the exact bytes the checker rejects. A second, wider gap
  is recorded in the same row: `anchorNumberReasonMismatches` skips every
  `first-frozen at …` row outright (`strings.Contains(p.Reason, ReasonMark)`),
  so 6 further tail mismatches are **silently unchecked** (a probe measured 9, the
  check reports 3). Evidence, reproduction, and a suggested fix are in the row's
  payload. **This run's regeneration strictly improves the artifact** (unpermitted
  1 → 0) and is idempotent; the 3 unreproducible rows are left exactly as they
  were, because repairing them means changing the writer, which is that bug's call
  and not this epic's scope.
- The orphaned branch `task/01a1310d8cc87b70b558bf7639593264` was **read, not
  checked out** (Rule 23/29): `git diff master...770ae6da4` was the input, and no
  worktree of it was touched. Archiving it is the merger's/operator's call; this
  run neither merged nor deleted it.

## Verification

- **Build**: `go build ./...` → rc=0
- **Vet**: `go vet ./...` → rc=0
- **Tests**:
  - `go test ./cmd/agent/ -count=1 -run 'TestToolsMaintenanceHelpSurfaceFormsAreTyped'` → **ok** (15.6s)
  - `go test ./cmd/agent/ -count=1 -run 'TestToolsMaintenanceHelpSurface|ScratchGc'` → **ok** (14.8s)
  - `go test ./internal/docclass/ -count=1` → **ok** (5.0s)
  - `python3 tools/docclass/classify.py . --check` → `0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv` (rc=0)
- **Ran** (Rule 31 — the changed surface against the real binary, real arguments):
  1. `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` (rc=0)
  2. `./bin/gopi tool scratch-gc -h` → lines 114 and 120 carry
     `gopi tool scratch-gc gocache --max-gib GiB` and
     `gopi tool scratch-gc gocache --headroom-gib GiB` — the newly-advertised
     forms are on the real page.
  3. Fixture: a sparse Go-toolchain cache built at
     `/home/user/workspace/gopi-sandbox/sandbox-r31-bbc0b97c/cache-fixture`
     (2 shards, 4 entries as `<64 hex>` `-a`/`-d` pairs, 3 aged 50h + 1 fresh,
     plus `README`) — **2.4 GiB apparent / 16 KiB real** (sparse; Rule 27 holds).
     Device `66305` ≠ `/`'s `66308` (off the root partition).
  4. `GOPI_HOME=<sandbox> GOPI_BUILD_CACHE_MAX_GIB=3 ./bin/gopi tool scratch-gc
     gocache --path <fixture> --max-gib 9` → rc=0, prints **`bound=9.0 GiB`**
     while the environment says 3 — **the flag wins**, which is exactly the claim
     the new g6Case asserts.
  5. `GOPI_HOME=<sandbox> ./bin/gopi tool scratch-gc gocache --path <fixture>
     --headroom-gib 1000000` → rc=0, prints
     `... has 535.9 GiB free of 3666.4 GiB — below the 1000000.0 GiB headroom
     floor; ... the cache is a shared asset and is NOT touched here` — the floor
     is the **flag's own operand**, and `trim=false`.
  6. `... --max-gib 0` → rc=**1** with `--max-gib requires a positive integer
     number of GiB, got "0"`.
  7. Disk after every report form: all 4 entries and both shard directories still
     present, `du --apparent-size` still 2.4G — the arm's DELETES NOTHING promise
     read off the filesystem.
- **Generators re-run, no diff (the true test that no stale numbering froze in)**:
  - `scripts/g6-inventory-doc.py <surface.tsv> cmd/agent <out>` run a second time
    into a separate file → `diff` **empty** (rc=0).
  - `anchor-number-census . internal/docclass/testdata/anchor-number-baseline.tsv`
    (the recipe the file's own header documents) → **fixpoint-stable**: `git status
    --porcelain` prints nothing afterwards, so the committed table is exactly what
    the generator produces at this baseline — no stale numbering is frozen in.
    Re-run a third time: still clean.
    **One caveat, measured, so the next reader is not misled**: the generator is
    stable at the file's OWN path, but NOT in general. Seeded with the table at a
    *different* out path and run twice, runs 1 and 2 differ — the writer treats the
    existing prior table at its output path as the carry-forward source and flips
    the reason CLASS TAIL of some rows (`recorded-line-blank` ⇄ `number-drifted`
    for `devdoc/merge-resolve-effort-audit.md`, `sync-taskid-session-refuse.md`,
    `task-notify-terminal-gap-20260912.md`). So the earlier claim "run twice →
    byte-identical" holds **only** for the in-place form; the out-path form converges
    on run 2. The body of a seeded regen is otherwise identical to the committed
    table (`diff` on the non-`#` lines = empty).
  - `anchor-number-census . <table> --check` → see `## Not Done`: 3 rows flagged,
    all pre-existing (HEAD's own table flags 1 unpermitted + the same 3).
- **Result**: build/vet green, both scoped suites green, both generators
  **fixpoint** at their own documented in-place recipe (re-running changes nothing),
  the two GiB flags observed **on the real binary** exactly as their usage text
  advertises.

### Evidence (self-filed)

- `./bin/gopi tool scratch-gc -h | grep -n 'max-gib\|headroom-gib'` →
  `114:  gopi tool scratch-gc gocache --max-gib GiB` and
  `120:  gopi tool scratch-gc gocache --headroom-gib GiB`.
- `--max-gib 9` run (env `GOPI_BUILD_CACHE_MAX_GIB=3`) → the report line carries
  `bound=9.0 GiB trim=false`; artifact `artifacts/r31-maxgib.txt`.
- `--headroom-gib 1000000` run → the verdict sentence carries
  `below the 1000000.0 GiB headroom floor`; artifact `artifacts/r31-headroom.txt`.
- `--max-gib 0` → `gopi scratch-gc: --max-gib requires a positive integer number
  of GiB, got "0"`, exit status **1**.
- Fixture disk after the report forms: `ls` shows 8 entry files across `ab/` and
  `cd/`; `du -sh --apparent-size` = `2.4G`, `du -sh` = `16K` (sparse).
- `stat -c %d` of the fixture root and of `/` → `66305` vs `66308` (off `/`).
- `scripts/g6-inventory-doc.py <surface.tsv> cmd/agent <out>` second run vs checked-in
  file → `diff` empty; the in-place pipeline (`g6-tools-maintenance-help-surface.sh`
  → `g6-inventory-doc.py … devdoc/gopi-tools-maintenance-help-surface-inventory.md`)
  leaves `git status --porcelain` empty — fixpoint.
- `anchor-number-census . internal/docclass/testdata/anchor-number-baseline.tsv`
  (the file's own documented recipe, in place) → `git status --porcelain` **empty**
  — fixpoint; the independent out-path copy is NOT stable across two runs (the
  prior-table carry-forward tail flip described above), so the in-place form is the
  one that certifies the artifact.
- `anchor-number-census . <HEAD's own table> --check` → exit 1 with
  `1 unpermitted, 0 stale permit(s), 3 unreproducible reason(s)` — the
  pre-existing red, captured in `artifacts/head-anchor.tsv`.

## Files

| file | why |
|---|---|
| `cmd/agent/cli_scratch_gc.go` | the two advertised forms' usage text (the branch's missing half) |
| `cmd/agent/cli_tools_maintenance_help_surface_cases_test.go` | the two typed g6Cases + their constants/helpers |
| `devdoc/gopi-tools-maintenance-help-surface-inventory.md` | regenerated through its generator |
| `internal/docclass/testdata/anchor-number-baseline.tsv` | regenerated through its generator |
| `COMMANDS.md` | regenerated through its generator (anchor line moved) |

Evidence lives outside the worktree, per Rule 30, under
`~/.gopi/sessions/01a13141-6142-72db-8d96-e0ede7ddb436/artifacts/`.
