<!-- doc-class: historical -->
# Task Report: `branch attribution UNAVAILABLE: ... %!w(<nil>)` — the nil-wrapped ledger error

**Branch**: `fix/attribution-ledger-nil-error` (worktree `/home/user/workspace/gopi-attrib-nilerr`)
**Date**: 2026-09-17
**Base**: `master` @ `6a6aef6d`
**Files**: `internal/tools/branch_attribution.go`, `internal/tools/branch_attribution_ledger_err_test.go` (new)

---

## Completed

### The exact `%!w(<nil>)` site

`internal/tools/branch_attribution.go`, `BranchAttributionReport` (master lines 561–584):

```go
rows, err := branchAttributionRows(ctx)          // ← the REAL ledger error is set here
ledgerRead := err == nil
if err != nil {
    rows = nil
}
branches, err := branchAttributionBranches(repo)// ← OVERWRITES err with nil on success
if err != nil {
    return nil, err
}
...
if !ledgerRead {
    return out, fmt.Errorf("branch attribution: task ledger unreadable: %w", err)  // ← err is NIL
}
```

`%!w(<nil>)` is `fmt`'s rendering of a NIL error handed to `%w`. The ledger failure was destroyed
by the very statement that reported it — the message claimed a cause it could no longer name.

**Why `err` was nil**: the ledger read failed (so `ledgerRead == false`), and then the *branch
listing* succeeded, assigning its own `nil` into the SAME `err`. Two independent calls shared one
error variable; the second one's success erased the first one's failure. The arm is reached exactly
when that happens — "ledger unreadable AND branch listing fine" — so the bug fired on the only path
that mattered, every time.

**This is a MIS-REPORTED failure, not a swallowed one** (Rule 15's family, worse than its usual
shape): the error *was* surfaced (the line is printed, exit stays 0), but the message was rendered
useless — an operator cannot act on a cause that was overwritten.

### The fix

1. **The ledger error keeps its OWN variable for its whole lifetime** — `rows, ledgerErr :=
   branchAttributionRows(ctx)`, and the `!ledgerRead` arm wraps `ledgerErr`. Reaching that arm means
   `ledgerErr != nil`, so the nil path is **UNREPRESENTABLE**, not merely avoided. No other variable
   is reused on this path; `branches, err := …` keeps its own `err`.
2. **A diagnosable cause** — `branchAttributionLedgerCause(memoryDBPath())` names WHERE it looked
   (the path), WHICH channel supplied it (`GOPI_MEMORY_DB` / the legacy alias / the default
   `$HOME/.gopi`), and WHAT was found there (`nothing to read: …` / `the path is a DIRECTORY` /
   `the file is there (N bytes) but could not be opened`). The underlying opener error is still
   WRAPPED with `%w` — the diagnosis **adds** a location verdict, it never masks the driver's words.
3. The operator-visible string `task ledger unreadable` is now the constant
   `branchAttributionLedgerUnreadable` (Rule 1b); `--status` renders the same line shape as before.

### The Maps repo verdict — graceful skip vs error (evidence)

**Verdict: the Maps repo having no ledger is NOT an error, and the fix does not change that.**

Evidence (all runs on the real configured repo `/home/user/Downloads/games/Maps`, restored after the
transcripts — see "Sandbox" below):

| state of the Maps repo | `--status` |
|---|---|
| exists, **ledger present** at `<repo>/.gopi/memory.db` (a real ledger copy) | `branches: 1 local head(s) = 1 integration + … 0 pending` — **no error line at all**, so attribution never failed |
| exists, **no ledger anywhere** (default `~/.gopi/memory.db` unreadable is the only failure) | only the GoPi row reports UNAVAILABLE; **Maps reports cleanly** with no attribution error |
| **path absent** (the host's real state) | `error: worktree probe: git worktree list: chdir …: no such file or directory` — an honest repo-level error that short-circuits the sweep, so the branch axis is never reached |

**Mechanism (why a repo with no ledger is fine)**: `BranchAttributionLines` has exactly ONE caller on
the status path, `MergerSweepPreview`, which is reached only AFTER `repoSweepClassify(repo)`
succeeds; a classify failure returns early with `preview.Err` set and no attribution attempt. And the
ledger path is the process-wide `memoryDBPath()` — the runner's own ledger (`$HOME/.gopi/memory.db`),
a GLOBAL resource, never a per-repo one. A `repos` entry with an empty build (Maps) is a pure merge
target: it is swept for branches and rebuilt by nothing. **There is no per-repo ledger in this
design, so "Maps has no ledger" is not a state the code can observe or should complain about.**

Consequence for the reported symptom: for the Maps repo the `%!w(<nil>)` line can only be produced
when the *runner's own* ledger (shared by both repos) is unreadable, and Maps must additionally be
readable enough for `repoSweepClassify` to pass — i.e. the GoPi repo's line is the honest one, and
the Maps repo's line (when the path exists on another host) is the SAME global failure necessarily
reported twice. Both now name the global ledger path and why it failed.

### Survey — the same class on the other output paths

`grep` of the status/attribution/sweep path (`merger_status.go`, `repo_sweep.go`,
`branch_attribution.go`) for a re-wrapped/shadowed error variable. Full same-class enumeration:

| site | shape | judgement |
|---|---|---|
| `merger_status.go:290` `resolve merger repos: %w` | `err` from the immediately-preceding `ResolveMergerRepos()` | **clean** (no reuse) |
| `repo_sweep.go:638/642` `worktree probe:` / `branch list: %w` | distinct vars, immediate | **clean** |
| `repo_sweep.go:563` `git %s: %w: %s` | immediate | **clean** |
| `repo_sweep.go:1250/1258` `open ledger:` / `park dedup query: %w` | immediate | **clean** |
| `branch_attribution.go:655` `branch list: %w` | immediate | **clean** |
| **`task_reclaim.go:846/936` (`DeadHolderBranches`, `ReclaimHeldBranches`)** | `rows, err := reclaimDeadHolderRows(); if err != nil \|\| len(rows) == 0 { return }` then `wtByBranch, _, err := mergedCleanupWorktrees(repo); if err != nil { return }` | **REPORTED, NOT FIXED** — same variable name reused across two calls, and the *discarded* worktree error (`_`) is then silently replaced by a later `err`: if the ledger read fails AND the worktree probe succeeds, the branch silently drops out of `dead-holder` / `held`. Not a wrap-nil (nothing is wrapped), so no `%!w(<nil>)`; it is the silent-failure sibling — an empty bucket reads as a clean bill of health. Deliberately out of scope here (the epic says report, do not fix them all); it is the natural next task in this family. |
| `merger_status.go:623` `pids, _ := fdInfoLockHolders(ino)` | discarded error (Rule 15b) | **documented as intentional**: an unreadable `/proc/<pid>/fdinfo` is "unreadable", never "no holder"; the caller's contract (`fdInfoLockHolder` → 0) is the conservative "cannot name a holder" answer, and the comment above it states the rule. |

---

### Transcripts on a repo with NO per-repo ledger (the Maps question, end to end)

Sandbox: a throwaway repo `/home/user/workspace/gopi-attrib-evidence/maps-repo`
(`git init` + 1 commit + one branch `feat/maps-sample`) selected through the **documented**
settings override `GOPI_SETTINGS=<maps-settings.json>` whose `repos` table names only it (its build
is empty, exactly like the Maps entry) — so the host's real `~/.gopi/settings.json` was never
touched. Both binaries were run on the identical sandbox.

**(a) healthy ledger — the GoPi contract, `diff` EMPTY:**
```
repo:        /home/user/workspace/gopi-attrib-evidence/maps-repo
  branches:  2 local head(s) = 1 integration + 0 worktree-held + 0 archive-tagged + 0 exempt-namespace + 0 deferred + 1 pending
    already-in-master (1): feat/maps-sample — the sweep prunes the ref with branch -d
  pending:   1 branch(es) the next sweep would act on (1 already in master → the ref is pruned)
    - feat/maps-sample
```
No ledger anywhere near that repo, and **no error at all** — attribution answered from git alone.
This is the direct proof of the verdict: *a repo without its own ledger is normal, not an error.*

**(b) runner ledger unusable (`GOPI_MEMORY_DB=<a directory>`) — the reported Maps symptom, fixed:**
```
before: … (branch attribution UNAVAILABLE: branch attribution: task ledger unreadable: %!w(<nil>))
after:  … (branch attribution UNAVAILABLE: branch attribution: task ledger unreadable:
            no ledger at /…/notaledger (GOPI_MEMORY_DB channel) — the path is a DIRECTORY,
            not a ledger file: unable to open database file: out of memory (14))
```
`grep -c '%!w(<nil>)' after-maps-badledger.txt` → **0**.

**(c) the location verdict distinguishes the three shapes an operator acts on differently**
(same repo, `GOPI_MEMORY_DB` pointed at each):
| ledger state | rendered cause tail |
|---|---|
| path is a DIRECTORY | `no ledger at <p> (GOPI_MEMORY_DB channel) — the path is a DIRECTORY, not a ledger file: …` |
| file present, mode `000` | `no ledger at <p> (GOPI_MEMORY_DB channel) — the file is there (1 bytes) but could not be opened: …` |
| path missing, parent absent | the ledger is CREATED (normal first-open) — no error line at all |

## Not Done (honest)

- The `task_reclaim.go` silent-discard above is **reported, not fixed** (per the epic's scope).
- **Pre-existing package failure, dispatched not fixed**:
  `TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy`
  (`internal/tools/worktree_sweep_memory_base_test.go:357`) fails on **master** as well as here
  (both verified with `-run` in clean worktrees). It is the worktree-sweep memory-base refusal
  label, untouched by this epic; fixing it here would smuggle an unrelated behaviour change into a
  bugfix branch (Rule 23/26: repair belongs in its own dispatch).
- The D1/D2 defects of `merger --status` (phantom pending / owner-ended worktree) live on the
  unmerged branch `fix/merger-status-three-defects` and are **not** part of this branch (Rule 23).
- Nothing was done about the Maps repo's absent path: that is the host's real state (the repo is
  gone), reported honestly by the pre-existing `worktree probe:` error. No code change is warranted,
  and this epic's fix does not depend on it.

## Sandbox / evidence recovery (mandatory disclosure)

The transcripts needed a Maps repo that EXISTS. `/home/user/Downloads/games/Maps` does **not** exist
on this host (the configured `repos` entry is stale). A **temporary** repo was created at exactly
that configured path (git init + one file + a copy of the real ledger), the three evidence runs were
taken, and the directory was then **removed** (`rm -rf Maps` from `/home/user/Downloads/games`),
restoring the host's prior state (`ls` confirms only the pre-existing `2000maps`, `6000maps`, `Mods`,
`maps-hand-picked`, `Heroes-Games`, `exe-dir`, `tmp` directories remain, none created by this task).
The `repos` table entry in `~/.gopi/settings.json` was **not** modified.

## Verification

- Build: `go build ./...` — OK (`go build ./...` exit 0)
- Vet: `go vet ./...` — OK
- Tests: `go test ./internal/tools/` — **5 PRE-EXISTING failures at the base commit, none related to this branch**
  (`TestFirstOpenSyncModeDecisionTable`, `TestMergeBuildGate_GoCacheLeavesNoScratchCache`,
  `TestSuiteScratchRootIsMemoryBacked`, `TestScratchRootFilesystemClassification`,
  `TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy`). Proven pre-existing, not mine: all five
  reproduce with the same one-line failures in a **clean detached worktree of the base commit**
  `6a6aef6d` (`/home/user/workspace/gopi-attrib-before`, a fresh `git worktree add --detach 6a6aef6d`;
  `git status --porcelain` empty), where **none of this branch's three files** exists. They exercise
  the scratch/tmpfs classification and the worktree-**sweep** memory-base policy — different
  subsystems (`TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy` is already dispatched as its own
  task, `#01a0acc2`). Everything this branch touches passes:
  `go test ./internal/tools/ -run 'TestBranchAttributionLedger|TestBranchAttributionUnreadableLedger|TestBranchAttributionHealthyLedgerIsUnaffected' -v` — **4/4 PASS** (`ok`),
  and the operator-status render is asserted through the real `BranchAttributionLines` producer.
  Per Rule 26 this is **dispatched as its own task** rather than fixed here (see "Not Done").
- Repro test (red proof, run in a **fresh detached worktree of the base commit**
  `/home/user/workspace/gopi-attrib-before` @ `6a6aef6d` — `git worktree add --detach` — with ONLY the
  new test file copied in, so it COMPILES against base code and fails on BEHAVIOUR, exactly):
  `go test ./internal/tools/ -run 'TestBranchAttributionLedger|TestBranchAttributionUnreadableLedger' -count=1`
  → **rc=1**, 4 failures, every one of them quoting the nil wrap
  (`~/workspace/gopi-attrib-evidence/redproof-master.txt`):
  ```
  --- FAIL: TestBranchAttributionLedgerErrorIsNeverNilWrapped
      the ledger error is wrapped from a NIL value — the cause was overwritten:
      "branch attribution: task ledger unreadable: %!w(<nil>)"
  --- FAIL: TestBranchAttributionLedgerCauseNamesWhereItLooked/present_but_unopenable_file
      nil-wrap marker on a present-but-unopenable ledger: "… : %!w(<nil>)"
  --- FAIL: TestBranchAttributionLedgerCauseNamesWhereItLooked/default_channel_location_is_named
      nil-wrap marker on the default ledger path: "… : %!w(<nil>)"
  --- FAIL: TestBranchAttributionUnreadableLedgerReachesTheStatusRender
      the operator line carries the nil-wrap marker: "… : %!w(<nil>)"
  ```
  Same command with the fix → **ok** (rc=0, 4/4 PASS — `greenproof-fixed.txt`).
- Operator output contract (GoPi repo, healthy ledger): `diff before after` — **EMPTY** (byte-identical).
  Regenerated end-to-end after the report's first draft by
  `~/workspace/gopi-attrib-evidence/refresh-evidence.sh` (two binaries built from `6a6aef6d` and from
  this branch; both run against the real `~/.gopi/settings.json`). The same script re-prints the
  defect on the base binary (`grep -c '%!w(<nil>)' before-*.txt` → **1** each) and its absence on the
  fixed one (→ **0** each), so both halves of the contract are re-derivable, not just asserted.
  A repo configured with NO ledger of its own (the Maps question) renders **byte-identically** before
  and after as well: attribution is answered from git alone, no error line at all.
- Operator output, unusable ledger — before / after:
  ```
  before: … (branch attribution UNAVAILABLE: branch attribution: task ledger unreadable: %!w(<nil>))
  after:  … (branch attribution UNAVAILABLE: branch attribution: task ledger unreadable:
              no ledger at /…/notaledger (GOPI_MEMORY_DB channel) — the path is a DIRECTORY,
              not a ledger file: unable to open database file: out of memory (14))
  ```
  (the `out of memory (14)` tail is the SQLite driver's own words for "cannot open a directory as a
  database" — kept verbatim because the diagnosis must not replace the real driver error).
- Transcripts: `/home/user/workspace/gopi-attrib-evidence/` (`TRANSCRIPTS.md`, `*-gopi-healthy.txt`,
  `*-gopi-badledger.txt`, `redproof-master.txt`, `greenproof-fixed.txt`). These live OUTSIDE the
  worktree on purpose (Rule 30 — a run leaves its worktree clean; the session/scratch area is the
  home of a run's outputs).
