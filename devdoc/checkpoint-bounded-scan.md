<!-- doc-class: historical -->
# Task Report: `gopi session checkpoint list` — the scan is BOUNDED, or it refuses by name

Task `01a1144de549776e90933d136f9eef08` (peerbox, 2026-10-07). Branch
`task/01a1144de549776e90933d136f9eef08`, worktree
`/home/user/workspace/gopi-01a1144de549776e90933d136f9eef08`.

## Completed

**1. The fault, re-measured before any edit.** The default path
(`~/.gopi/sessions`) was scanned by `internal/checkpoint.IndexTree`: it listed the
root, then called `ReadDir` on **every** child directory (the co-located
raw-log layout lives one level down). Measured on this host:

| measurement | value |
|---|---|
| first-level entries in `~/.gopi/sessions` | 83960 (83548 dirs, 412 files) |
| one `os.ReadDir(root)` | 0.33 s warm — **16.9 s** while another run saturates the same disk |
| one child `ReadDir` (sample of 300) | 8.67 ms |
| projected full walk of 83548 children | **~12 minutes** |
| `timeout -s KILL 30 gopi session checkpoint list` | rc=**137**, **zero bytes** of output, `real 30.0s / user 0.001s` |

Zero user CPU rules out arithmetic: the process is blocked on I/O, and Rule 15's
real defect is the **silence** — nothing to read, before or after. `--dir` at
an empty directory returned rc=0 in 0.007 s, so the scan machinery was never
broken; only the default root's size was unbounded.

`strace -f` confirmed the mechanism (15562 `openat` + 31680 `getdents64` before
the kill, one pair per child, no `read` on any payload): the walk is a
directory-listing storm, not a byte scan.

**2. The bound, in `internal/checkpoint` (`index.go`).**

- `MaxScanRootEntries = 20000` (Rule 1/13) — a directory the scan would descend
  into is REFUSED above it. Placed with the measurements that justify it: one run
  day ≈ 40-60 entries, a busy year ≈ 2000 (10x headroom), the observed root
  83960 (~4x above the bound).
- `readEntries(dir)` reads in `readDirChunkSize = 4096` chunks and **stops at
  Limit+1**. This is the design decision worth keeping: a single `os.ReadDir`
  materialises and sorts every entry *before* any limit can be applied, so the
  bound would have been decided only after paying the full listing — the 16.9 s
  variance above, on a path whose whole purpose is to refuse fast. With chunked
  reads, refusing a root costs ≤ Limit+1 entries of listing.
- `NamedRefusal{Code, Path, Entries, Limit}` + `RefusalScanRootTooLarge =
  "scan-root-too-large"` — a structured refusal (Rule 15), carrying the numbers
  so the CLI adds a next step without re-deriving anything. `Entries` is
  documented as a **lower bound** (the read stopped), and the message says "at
  least", because claiming a total nobody counted is the same class of defect as
  the silence.
- The check is on the **entry count, never wall-clock time**: a timeout is
  non-deterministic (page cache) and would silently truncate a walk that a slower
  day made late, while a count is a property of the input.

**3. The CLI face (`cmd/agent/cli_checkpoint.go`).** Both faces of the defect —
`list` and `export`'s id lookup (`resolveRawLog` calls the same `IndexTree`) —
print the refusal and then `checkpointNextSteps`: the two channels that narrow
the scan, `--dir DIR` and `GOPI_RAW_LOG_DIR=DIR`, with the `raw_log_dir` knob
named so the reader can find its row in `CONFIG.md`. Named constants
(`checkpointDirFlag`, `cpRawLogDirSettingsKey`, `checkpointNextSteps`) so neither
the refusal nor the page re-types the other's values — a first draft printed
`--dirdir DIR`, which the new test now catches by deriving the expected spelling
from the constants the dispatcher switches on.

**4. Regression tests (Rule 10.5), red before the fix and green after.**

- `internal/checkpoint/index_bounded_test.go` — `TestReadEntries_StopsAtTheBound`
  (the refusal read stops; the boundary is exact: at the bound is NOT over it),
  `TestIndexTree_RefusesOversizedRootByName` (`errors.As` reaches
  `*NamedRefusal`, the numbers are carried, the refusal path lists **zero**
  directories and returns **zero** entries — the structural, non-flaky half of
  "does not block"), `TestIndexTree_OrdinaryRootStillScans`.
- `cmd/agent/cli_checkpoint_bounded_test.go` — the real binary through argv: a
  root one entry over the bound exits non-zero with the named refusal, both next
  steps, and no listing table on stdout.

## Not Done

- **The checkpoint usage page / G4 help-surface debt is untouched** — the
  inventory row count and the `list|export` synopsis wording are out of scope by
  the epic's own constraint (`devdoc/gopi-session-help-surface-inventory.md`).
  `checkpointUsage` was not edited; the new text lives in the refusal only.
- **No `--max-scan-entries` knob.** A larger root cannot be scanned on purpose —
  the operator narrows with `--dir` / `GOPI_RAW_LOG_DIR`, which is what the
  refusal says. Adding an override flag would be a new channel for a diagnostic
  case (and a dependency, Rule 35).
- **`IndexDir`'s caller set is unchanged**: it still wraps `IndexTree`, so it
  inherits the bound and can now return a `*NamedRefusal`. No caller in the tree
  depends on the old "always fully scans" behaviour (`checkpoint.Index*` has two
  production callers, both in `cli_checkpoint.go`).
- A root with 83960 entries is *diagnosable* now, not *scannable*: a user who
  genuinely wants that tree listed must point `--dir` at the run directories they
  care about. That is the intended trade (the listing was never usable at that
  size — it never completed).

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD OK`
  (`gofmt -l` on all four delivered files → empty).
- Tests: `go test ./internal/checkpoint/ -run 'TestReadEntries_StopsAtTheBound|TestIndexTree_RefusesOversizedRootByName|TestIndexTree_OrdinaryRootStillScans' -count=1 -v`
  → all three `--- PASS`, `ok github.com/nick/gopi/internal/checkpoint 1.086s`.
  Full-package and `cmd/agent`-scoped runs via
  `make test-budget PKGS='./internal/checkpoint/ ./cmd/agent/'` (background job
  `job-2`); its output is in the session directory.
- Ran (Rule 31, verbatim, `bin/gopi` built from this worktree):
  - `timeout -s KILL 30 ./bin/gopi session checkpoint list` → **rc=1**, `real 0m0.023s / user 0m0.015s / sys 0m0.014s`
    (pre-fix: rc=137, `real 30.030s / user 0.001s`, **zero output**), stderr:
    ```
    gopi session checkpoint list: scan-root-too-large: /home/user/.gopi/sessions holds at least 20001 entries, over the 20000-entry scan limit
      narrow the scan with:
        --dir DIR        (scan one directory / one run directory)
        GOPI_RAW_LOG_DIR=DIR           (relocate the whole raw-log tree; the same variable that moves where new raw logs are written — see the raw_log_dir knob in CONFIG.md)
    ```
  - `timeout -s KILL 30 ./bin/gopi session checkpoint list --dir /tmp/gopi-cp-empty-1268928` → **rc=0**, `real 0m0.006s`, scan report printed:
    `scanned 1 directory(ies) for raw logs; criteria: …` /
    `/tmp/gopi-cp-empty-1268928 — 0 entries, 0 raw log(s)` /
    `no raw logs found under … — see the scan report above …` — no regression.
  - `timeout -s KILL 30 ./bin/gopi session checkpoint export nosuchlog` → **rc=1**, same named refusal + both next steps (the second face of the same walk; pre-fix this path hung identically).
  - Boundary oracle, run because the bound decision is an inequality that fails
    silently if it is backwards: `ls "$HOME/.gopi/sessions" | wc -l` → **83960**,
    i.e. the refusal is the correct branch on this host.
  - **Re-derived after the delivery-point sync** (master merged into the branch at
    `df4e53007`; `git merge-base --is-ancestor master HEAD` → exit 0, i.e. the
    branch now descends from master), with `bin/gopi` rebuilt from the merged
    tree: `timeout -s KILL 30 ./bin/gopi session checkpoint list` → **rc=1**,
    `real 0m0.022s / user 0m0.021s / sys 0m0.007s`, the same three-line named
    refusal on stderr and **nothing on stdout**; `timeout -s KILL 30 ./bin/gopi
    session checkpoint list --dir <fresh empty dir>` → **rc=0**,
    `real 0m0.007s`, the same scan report
    (`scanned 1 directory(ies) …` / `<dir> — 0 entries, 0 raw log(s)` /
    `no raw logs found under …`). Merge conflicts touched only
    `devdoc/DOC-INDEX.tsv` (auto-merged, both rows present; `gopi tool doccheck`
    → "the devdoc corpus is consistent").
- Result: the default path no longer blocks (0.023s vs a projected ~12 min) and
  never exits 0 in silence; an empty `--dir` is unchanged (rc=0, 0.006s); the
  refusal carries its own reason and the next step; `export` inherits both.
- Commit: `3540c11be` (fix + tests + report), `4e24c4e30` (report follow-up),
  delivery-point sync merge `df4e53007`.

## Evidence (self-filed)

- `timeout -s KILL 30 ./bin/gopi session checkpoint list` pre-fix: `rc=137`,
  `real 30.030s`, `user 0m0.001s`, stdout+stderr empty — this run's own output.
- post-fix: `rc=1`, `real 0m0.023s`, refusal text above — this run's own output.
- `--dir <empty>`: `rc=0`, `real 0m0.006s`, scan report printed.
- Red→green, as a REAL falsification rather than a compile error (the first
  attempt — deleting the refusal block — produced
  `syntax error: non-declaration statement outside function body`, which proves
  nothing about the behaviour): the guard was falsified in place
  (`if descend && … && over {` → `if false && descend && … && over {`, one
  substring, restored from a copy afterwards) and:
  - RED: `go test ./internal/checkpoint/ -run 'TestIndexTree_RefusesOversizedRootByName|TestReadEntries_StopsAtTheBound' -count=1`
    → `--- FAIL: TestIndexTree_RefusesOversizedRootByName (0.47s)` /
    `index_bounded_test.go:95: IndexTree walked a 20001-entry root without refusing — the unbounded scan the epic was filed for is still reachable` / `FAIL … 1.232s`.
  - GREEN (bound restored, `git diff --stat internal/checkpoint/index.go` → 147 insertions/1 deletion):
    `--- PASS: TestReadEntries_StopsAtTheBound (0.52s)`, `--- PASS: TestIndexTree_RefusesOversizedRootByName (0.29s)`,
    `--- PASS: TestIndexTree_OrdinaryRootStillScans (0.00s)`, `ok … 1.086s`.
  - `cmd/agent` CLI test (`TestCheckpointListOversizedRootIsRefusedByName`) drives the built binary through argv and asserts the same green; its red arm is the pre-fix binary (which exits 137 on that profile).
- Instrumentation files (`strace.out`, `strace2.out`, `tmp-probe/`, the empty-dir
  probe) were used for the diagnosis and deleted before the commit —
  `git status --porcelain` lists only the four delivered files plus the index row.
