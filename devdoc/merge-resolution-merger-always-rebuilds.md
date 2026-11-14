<!-- doc-class: historical -->
# Merge resolution: feat/merger-always-rebuilds → master

Branch tip `10d34388` + master tip `46838d9b` (merge-base `db1fcef6`).

## Conflict set

| path | kind | resolution |
|---|---|---|
| `devdoc/merger-always-rebuilds-mutation-redproof.sh` | add/add, 4 hunks | **master's revision (superset)** |

Nothing else conflicted; every other path auto-merged.

## Why master's side is the faithful resolution (not a silent drop of the branch)

The branch's *code* half is already upstream: merge-base `db1fcef6`
("rebuild every configured repo at the pass close loop") **is** on master, and
master then revised the same feature in `c010f3bb` ("arm the post-merge rebuild
as a pass-level invariant (defer) + cover the merge-execute face").

So the two sides of this conflict are two revisions of ONE artifact — the
branch's `10d34388` docs commit (3 mutations, `sh`, `cd "$(dirname "$0")"`,
`/dev/shm/gopi-mutation-backup-*` backups) versus master's later red-proof of
the *shipped* wiring:

- 5 mutations vs 3, and the anchors match the merged tree (`defer func() {
  mergerPassCloseLoop(repos) }()` at `cmd/agent/cli_tasks.go:2335`, the
  merge-execute face at `:4681`) — the branch's anchors (`cp "$BAK"`, an
  unguarded call site) no longer exist in the tree that merges here;
- master's version exists precisely because the branch's draft was measured to
  be vacuous (root-relative paths + a silent restore failure) — its own header
  records both lessons.

Taking master's revision is therefore the *combination*: it is the branch's
intent (prove the rebuild invariant's pinning tests are non-vacuous) carried
forward onto the wiring that actually merges. The branch's draft survives in
git history at `10d34388`.

The companion `devdoc/merger-always-rebuilds.md` was byte-identical on both
sides (`2ef97574`), so no hunk existed and none was needed.

## Hand-resolution was NOT enough: master itself did not compile

The merge's own conflict set produced a tree byte-identical to master outside
`devdoc/` (`git diff master HEAD -- internal/ cmd/` = empty), and that tree
**failed `go build ./...` with three duplicate-declaration errors plus one
arity error** — i.e. master was RED before this merge, from two features that
landed in parallel and each redeclared the same values:

| site | duplicate |
|---|---|
| `internal/scratch/tmproot.go` (46838d9b) | `BaseEnvName`, `tmpfsMagic`, `ramfsMagic` |
| `internal/scratch/rootgocache.go` (1a547ebd) | `tmpfsMagic`, `ramfsMagic` (as `TmpfsMagic`/`RamfsMagic` + aliases) |
| `internal/scratch/base_residue.go` (9739a3cf) | `BaseEnvName` (a *different* value: `GOPI_SCRATCH_BASE`) |
| `cmd/agent/cli_daemon_spawn.go` (0085797c) | `return err` in a `(bool, error)` function |

Leaving that red would leave a committed merge that cannot build, so the
duplications were repaired at the root — one definition per value, every
bypassed literal replaced by the constant (Rules 13/14), and each colliding
name given the meaning its *readers* actually need:

- `tmpfsMagic`/`ramfsMagic` have ONE declaration — `rootgocache.go` (which also
  exports `TmpfsMagic`/`RamfsMagic` for `internal/testutil`) — and
  `tmproot.go`'s statfs classifier uses it, so the two classifiers can no
  longer disagree;
- the two base knobs are two REAL knobs, not one renamed: `ResolveBaseEnvName`
  (`GOPI_SCRATCH_BASE`, read by `ResolveBase`, the fixture-overridable knob) and
  `MemoryBaseEnvName` + alias `BaseEnvName` (`GOPI_SCRATCH_MEMORY_BASE`, the
  HOST variable `scripts/setup-scratch-home.sh` mounts). `BaseEnvName` keeps
  the spelling the CLI help (`cli_scratch_gc.go`) and the suite tests use, and
  the **worktree sweep now consults `MemoryBaseEnvName`** — it was reading the
  `GOPI_SCRATCH_BASE` constant while attributing it to
  `GOPI_SCRATCH_MEMORY_BASE` in its own finding, so it could not see the
  variable its message names (that was the latent wrong-constant bug of the
  same parallel-landing family, not a rename);
- `daemonSpawn`'s waiting acquisition returns `false, err` like every sibling
  return in that function.

## Verification (bounded)

- marker scan: 0 conflict markers; `git status` shows no unmerged path;
- `git merge master --no-commit --no-ff` re-run → "Already up to date" (master
  is an ancestor — no conflict can remain);
- `CGO_ENABLED=0 go build ./...` → exit 0 (was exit 1 before the repair);
- `go test ./internal/scratch/ -run 'TestResolveBase|TestMemoryFilesystem|TestClassifyFilesystemType|TestScratchRoot|TestMemoryBase|TestBase' -count=1` → **ok** (4.4s);
- `go test ./internal/tools/ -run 'TestWorktreeSweep|TestMemoryBase' -count=1` → 2 FAILs, both **pre-existing and independent of this resolution**:
  `TestMemoryBaseKeepArmsOverrideTheNewRoot/dirty` and
  `TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy` ("the finding names no
  root"). They are the "2 assertions unmet" that the WIP commit `1e553d7f`
  (one of master's own parents) declares in its own subject; the mechanism is a
  `Root`-stamping gap in `worktreeSweepJudge` (the dirty arm returns the finding
  raw instead of through `worktreeSweepTagFinding`, and the out-of-policy arm
  stamps `PermitsAny`'s zero `Root{}`), **not** the env-constant rename this
  resolution landed. Filed as a bug task with the repro + mechanism.
- NOT run: `go test ./...` (the repo's known full-suite hang; out of scope for a
  resolver).

Not repaired (pre-existing, byte-identical to master, unrelated to this
merge): `gofmt -l internal/tools/task_deliverable_worktree.go` — a formatting
deviation, not a build/test failure.
