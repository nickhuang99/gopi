<!-- doc-class: historical -->
# Task Report: the four internal/testutil gates on master's tip are green

Branch: `fix/testutil-gates-green` · worktree `/home/user/workspace/gopi-testutil-reds`
Base: master `0dac2ae7` (2026-09-16)

## What the epic asked

Master was red with four pre-existing failures in `internal/testutil`. Each had
to become green by **fixing the condition it names**, or by using the gate's own
reasoned dismissal — never by weakening or deleting a gate. Four gates, four
different dispositions, and one of them hid a real defect.

| gate | what it wanted | disposition |
|---|---|---|
| `TestPinCoverageTable` + `TestEveryTempUsingSuiteIsPinned` | every test package that creates scratch must pin its scratch root through `testutil.RunSuiteMain` | **fixed** (backfill `internal/taskreport`) |
| `TestTestBudgetHasASingleSourceAndNamesATimeout` | every committed workflow's `go test` invocation names its own budget | **fixed** (named constant in `scripts/accept-daemon-scope.sh`) |
| `TestWorkerOwnershipPointerPresent` | a file that teaches a worker-launch shape carries the operator-only pointer | **6 files annotated with the canonical pointer + 1 shell rig annotated** |
| `TestWorkerOwnershipPointerPresent` also found `devdoc/verify-01a0a72c-merger-stop-immediate.md` | — | **not a repo file**: untracked, session-local residue in the shared main checkout (gitignored-out of the worktree); it disappears with `git clean`, no gate change |
| `TestScriptsAreBoundedAndOffTmp` (5th red, surfaced on the first FULL run) | a script that creates scratch must source `scripts/lib/tmp-ban.sh` | **fixed** (adopted a sibling branch's exact bytes — see below) |

## Per-gate detail

### 1. `TestPinCoverageTable` + `TestEveryTempUsingSuiteIsPinned` — REAL DEFECT

**What it wanted.** Any Go test package whose sources call `t.TempDir()`/
`os.MkdirTemp` must route its `TestMain` through `testutil.RunSuiteMain`, so the
suite's scratch root is asserted OFF the `/` partition (GOPI.md Rule 22 — `/tmp`
is a directory on `/` on this host, the partition the 2026-09-11 exhaustion
filled).

**What changed.** Added `internal/taskreport/testmain_scratchroot_test.go`:

```go
func TestMain(m *testing.M) {
	testutil.RunSuiteMain(m, "taskreport")
}
```

`internal/taskreport/own_test.go` builds its fixtures with `t.TempDir()` at two
call sites (`:17`, `:93`) and the package had **no** `TestMain` at all.

**This hid a real defect, and it is the only one of the four that did.** Before
the pin, `os.TempDir()` inside `internal/taskreport` WAS the ambient root, i.e.
`/tmp` on `/` (measured: `stat -c %d /` = `2051`, `stat -c %d /tmp` = `2051` —
same device; `stat -c %d /dev/shm` = `28`). Measured proof, same probe on both
trees, `GOPI_TEST_HOME=/etc/hostname TMPDIR=/tmp` (a home that cannot be made):

- worktree (branch, with the pin): the suite now takes the GOTESTSUITE_NO_PIN
  escape path in that broken-home probe and prints the Rule-22 FATAL naming the
  root filesystem — the pin is present and engaged;
- master control (`/home/user/work/GoPi`, no pin): `ok … 0.031s` — the suite ran
  to completion with its scratch landing on the ambient `/tmp`.

The code path: `internal/taskreport/own_test.go` fixtures →
`os.TempDir()` → ambient `/tmp` (device 2051), before the fix.

**After:** the coverage table row reads
`internal/taskreport  yes  yes  PIN  PIN`, and `internal/taskreport` itself is
still green (`0.411s`).

### 2. `TestTestBudgetHasASingleSourceAndNamesATimeout` — fixed, named constant

**What it wanted.** `scripts/accept-daemon-scope.sh:105` ran `go test` with no
`-timeout`, so it inherited Go's 10m per-package cap; the gate's message: *"it
inherits Go's 10m per-package cap, which a loaded host trips on internal/tools
(a 600s FAIL that proves nothing). Pass a budget, or route through
scripts/test-budget.sh (Rule 1a/2/15)"*.

**What changed.** The budget is declared ONCE as a named constant (Rule 1a/2),
reusing the repo's single source `scripts/test-budget.sh` (whose `test_budget`
is `25m`) and its one override `GOPI_TEST_TIMEOUT` (Rule 2):

```bash
acc_go_timeout="${GOPI_TEST_TIMEOUT:-25m}"
```

and the call site now reads `go test ./cmd/agent -count=1 -timeout "$acc_go_timeout" \`
with the `-run` filter on the following line. **Note the predicate detail:** the
gate is line-anchored (`^\s*…go test\b`) and requires `-timeout` on the SAME
line, so `-timeout` had to move up next to `go test` — the original ordering
(`-run … \` then `-count=1 -timeout …`) is invisible to it. The rig's own
header keeps a comment naming `scripts/test-budget.sh` as the routable
equivalent (this rig cannot use the script: `[3]` must run inside a window
SHARED with two resident daemons).

### 3. `TestWorkerOwnershipPointerPresent` — 7 files annotated with the gate's OWN pointer

**What it wanted.** Each of 8 files (7 real) *teaches* a worker-launch / task-run
shape without the operator-only pointer. The three sanctioned doors: the
canonical pointer line, the `workerOwnershipDismissed` table (with a reason), or
— where a file's mention is evidence rather than instruction — a dismissal. No
gate was touched; no dismissal was added.

**What changed.** Six devdocs got the canonical markdown pointer immediately
after their H1 (matching the corpus style, e.g. `devdoc/worker-once-vs-task-run-next.md`):

```
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
```

in `devdoc/daemon-scope-per-profile.md`, `devdoc/why-worktree-residue.md`,
`devdoc/nested-report-path-clobber.md`, `devdoc/worker-idle-drained-stop.md`,
`devdoc/worktree-reap-reachable.md`, `devdoc/worktree-refusal-arms.md`.

`scripts/accept-daemon-scope.sh` got the shell form plus a reason block, because
its three literal `gopi worker` spawns are the rig's deliberate simulation of the
**operator's** shell (it strips the agent-spawn signature with `env -u` for
exactly that reason) — quoting the added text:

```bash
# WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md
# (The rig below starts `gopi worker --daemon` as the OPERATOR's shell shape —
#  it strips the agent-spawn signature with `env -u` for that reason, and step
#  [3] is a `go test <pkg>` invocation. Nothing here is a recipe for an agent:
#  an agent's verb is `gopi task submit`, and the refusal is the contract
#  working. This line names the ruling; the canonical block owns the policy.)
```

**The 8th file was not a missed annotation.** `devdoc/verify-01a0a72c-merger-stop-immediate.md`
exists only in the shared main checkout (`/home/user/work/GoPi`), where it is
**untracked** session residue (`git status --short devdoc/` → `??`, mtime today
08:15, no commit), and it is invisible in the worktree because the repo ignores
it. It never enters the gate on a clean tree. Nothing was changed for it.

### 4. `TestScriptsAreBoundedAndOffTmp` — 5th red, discovered by the full run

**Not in the epic's list**, and it only appears when the whole package runs (the
four-gate `-run` filter skips it). Measured identically on unmodified master
(`go test ./internal/testutil/ -run TestScriptsAreBoundedAndOffTmp` in
`/home/user/work/GoPi` → **FAIL**, same message), so it is pre-existing too:

> `devdoc/loop-proof-merge-rebuild.sh creates scratch data but does not source/import scripts/lib/tmp-ban.sh — the shared helper is what asserts the path is off / and registers trap-time cleanup`

**What changed.** The script created its root with
`mktemp -d "${GOPI_LOOP_PROOF_ROOT:-/dev/shm}/loop-proof-XXXXXX"` — a
hand-chosen path, unpinned to the shared contract. It now sources
`scripts/lib/tmp-ban.sh`, uses `gopi_scratch_mkdir` (which asserts the device id
differs from `/` and registers trap-time cleanup), and traps
`gopi_scratch_cleanup`.

**Sibling overlap, declared (Rule 26).** Branch `fix/loop-proof-script-scratch`
(`f478baed`, first commit on top of master `0dac2ae7`) fixes THIS SAME FILE.
Rather than land two conflicting fixes for one defect, this branch adopted that
branch's bytes verbatim (`git checkout fix/loop-proof-script-scratch --
devdoc/loop-proof-merge-rebuild.sh`), following the repo's own documented
precedent that two branches should converge byte-identically so the merge is a
no-op instead of a contradiction (`devdoc/tmpdir-merge-resolution.md` §3, cited
in the gate's own dismiss table). The file is therefore NOT modified by this
branch's net effect against that sibling — merge order does not matter.

## Is any of the four hiding a REAL defect?

**Yes — one.** `TestEveryTempUsingSuiteIsPinned` / `TestPinCoverageTable`:
`internal/taskreport`'s two `t.TempDir()` fixture sites
(`internal/taskreport/own_test.go:17`, `:93`) ran with no `TestMain` pin, so the
suite's scratch landed on the ambient temp root — `/tmp`, on the same device as
`/` (both `2051`, measured). That is precisely the Rule-22 hazard the gate
guards; the pin now makes it impossible, and the refusal probe above shows the
difference between the pre-fix and post-fix trees.

The other three named a genuine **drift** but no code defect:
- the budget gate named a real missing declaration in an acceptance rig (a
  loaded-host hazard, not a bug — the rig's window is short);
- the pointer gate named real documentation drift (7 files, one of them a script
  that starts the operator's daemon);
- the 5th (scratch-helper) gate named a real Rule-22 hole in a devdoc rig.

## Files changed

```
 internal/taskreport/testmain_scratchroot_test.go   (new — the pin backfill)
 scripts/accept-daemon-scope.sh                     (budget constant + ownership pointer)
 devdoc/daemon-scope-per-profile.md                 (ownership pointer)
 devdoc/why-worktree-residue.md                     (ownership pointer)
 devdoc/nested-report-path-clobber.md               (ownership pointer)
 devdoc/worker-idle-drained-stop.md                 (ownership pointer)
 devdoc/worktree-reap-reachable.md                  (ownership pointer)
 devdoc/worktree-refusal-arms.md                    (ownership pointer)
 devdoc/loop-proof-merge-rebuild.sh                 (adopted sibling fix/loop-proof-script-scratch's bytes)
 devdoc/testutil-gates-green.md                     (this report)
```

## Verification

```
$ CGO_ENABLED=0 go build ./...            → BUILD OK (exit 0)
$ go vet ./...                            → VET OK (exit 0)

$ go test ./internal/testutil/ -count=1
ok  	github.com/nick/gopi/internal/testutil	53.060s

$ go test ./internal/taskreport/ -count=1
ok  	github.com/nick/gopi/internal/taskreport	0.411s

$ go test ./internal/testutil/ -count=1 -v -run '<the four gates>'
--- PASS: TestPinCoverageTable (0.07s)
    pin_coverage_test.go:95: internal/taskreport   yes   yes   PIN   PIN
    pin_coverage_test.go:99: packages: 34 scanned | scratch-using: 28 by the ORIGINAL markers, 28 by the widened markers (+0 newly detected) | pinned: 28
--- PASS: TestEveryTempUsingSuiteIsPinned (0.05s)
--- PASS: TestTestBudgetHasASingleSourceAndNamesATimeout (0.02s)
    testbudget_gate_test.go:103: budget gate: 14 `go test` invocation(s) found, 7 exempt by name (of 3 exemptions)
--- PASS: TestWorkerOwnershipPointerPresent (0.04s)
PASS
ok  	github.com/nick/gopi/internal/testutil	0.648s
```

Rule 22 partition measurement used throughout (the criterion, never a path
spelling): `stat -c '%d %n' / /tmp /dev/shm` → `2051 /`, `2051 /tmp`, `28 /dev/shm`.

Pin-refusal probe (proof the new pin ENGAGES; `GOPI_TEST_HOME` on a regular file
so every `MkdirAll` under it fails):

```
$ GOPI_TEST_HOME=/etc/hostname TMPDIR=/tmp go test ./internal/taskreport/ -count=1   # worktree (pinned)
testutil: scratch home /etc/hostname/workspace/gopi-test-scratch: mkdir /etc/hostname: not a directory
FATAL: suite "taskreport": the scratch-root pin did NOT engage, so the suite fell back to the AMBIENT temp root /tmp …
       scratch root "/tmp" is on the SAME filesystem as / (device …) … GoPi bans that
$ …… same probe in /home/user/work/GoPi (master, NO pin)
ok  	github.com/nick/gopi/internal/taskreport	0.031s
```

## Not Done (honest)

- **No `workerOwnershipDismissed` entry was added** — every one of the 7 real
  files was annotatable with the canonical pointer, and a dismiss permit is a
  weaker instrument than a pointer. If a future reviewer judges one of the six
  devdocs' mention to be evidence rather than instruction, the dismissal door is
  still open and un-touched.
- **The 8th file (`devdoc/verify-01a0a72c-merger-stop-immediate.md`) is
  session-local untracked residue in the MAIN checkout**, not a repo file. I did
  not delete it (another agent's live artifact); it is invisible on any clean
  tree, so no gate change was warranted.
- **`devdoc/loop-proof-merge-rebuild.sh` is a sibling branch's fix, adopted
  byte-identically**, not written here. This branch still CARRIES the change (it
  had to, or the full-package run stays red on a tree without that branch) — but
  the credit and the decision are `fix/loop-proof-script-scratch`'s, and the two
  merge as a no-op.
- The 4-gate `-run` filter the epic used would have hidden the 5th red
  (`TestScriptsAreBoundedAndOffTmp`). It is fixed, but the lesson is that the
  epic's "4 failures" was measured with a filter, not by the package outcome.
