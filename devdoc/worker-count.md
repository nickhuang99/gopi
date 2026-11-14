<!-- doc-class: normative -->
# Task Report: gopi-worker-up.sh live-worker count (the 2026-09-12 miscount)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

## Summary

`scripts/gopi-worker-up.sh` ended with `pgrep -x gopi | wc -l`, which answers
"how many processes are **named** like the gopi binary" — task-serve daemons,
the merger, and one-shot `gopi task …` children all match. The number the
operator reads before deciding whether to launch more workers was therefore
inflated by other roles. The fix counts by **argv shape** over procfs
(`comm == gopi AND argv[1] == worker`), subtracts the `task` verb deliberately
and reports it as its own line, and the two values the count depends on live in
a new leaf constant package, `internal/procname`, that the script reads through
the environment and a Go gate asserts against.

## Before / after on the live host (measured this session)

Ground truth, independent of the script (`ps -eo comm,args` + a direct
`/proc/*/cmdline` walk), on the incident host:

```
$ ps -eo comm,args | awk '$1=="gopi"{n++; if ($3=="worker"||$2 ~ /worker$/) w++} END{print "named="n" workers="w}'
named=12 workers=5
$ for p in /proc/[0-9]*; do ... comm==gopi && argv[1]==worker ... done | wc -l
5
```

| | old script (`pgrep -x gopi \| wc -l`) | new script (`--count`) |
|---|---|---|
| reported | **10** at the first sampling / **14** later (a host-wide name count) | **5 workers** (+ 7 other gopi processes, reported separately) |
| vs ground truth | 5 workers → **2–3× inflated** | 5 workers → **exact** |

```
$ ~/workspace/gopi-wc/scripts/gopi-worker-up.sh --count
live gopi workers: 5 (pids: 380024 380057 463445 463556 463612)
other gopi processes: 7 (not workers — task-daemon/merger/task-run children)
```

The `pgrep -x gopi | wc -l` figure was re-measured at the same moment (10 = 5
workers + 5 non-worker gopi processes; the non-worker count moved during the
session because `gopi task run` children come and go — the worker count stayed
at 5 throughout, which is the point: the old number was not reproducible and the
new one is).

### The old predicate, re-executed

The pre-fix counting line was reconstructed (`sed`-replaced back into
`report_counts`, i.e. the script with its only change reverted) and run against
a synthetic `GOPI_PROC_ROOT` holding exactly **3** worker shapes:

```
$ GOPI_PROC_ROOT=<3-worker fixture> scripts/gopi-worker-up.sh --count
live gopi workers: 3 (pids: 2001 2002 2003)
$ GOPI_PROC_ROOT=<same fixture> bash <old-line script> --count
live gopi workers: 14          # = the host's 14 gopi-named processes, fixture ignored
```

Two things at once: the old line returns the WRONG number for the fixture (14,
not 3 — it never looks at the fixture at all, it counts the host by name), and
on the live host at that moment it printed 14 against 5 real workers.

The same reconstruction was used as a **canary against the test**: with the old
`pgrep -x … | wc -l` line patched back into the script,
`TestWorkerUpCountsArgvShapeNotName` FAILS (`cannot read the count from "(not"`);
with the new counter it passes. That is the Rule 10.5 statement — the regression
test is red on the old code, green on the new.

## Completed

1. **`internal/procname/procname.go`** (new leaf package, imports nothing) —
   the single source of truth for the three process-identity values:
   `NameGopi`, `VerbWorker`, `VerbTask`, plus the four env names
   (`EnvName` / `EnvVerbWorker` / `EnvVerbTask` / `EnvProcRoot`). It is the Go
   definition site; the launcher's env-default lines are the shell-side
   definition site, and a gate keeps the two in agreement (Rule 13/14).
2. **`scripts/gopi-worker-up.sh`** — the miscounting last line is replaced by
   `count_gopi_processes()`, a procfs walk that reads `<root>/<pid>/comm` (the
   name) **and** `<root>/<pid>/cmdline` (NUL-delimited argv). A process is a
   worker iff `comm == $PROC_NAME` and `argv[1] == $WORKER_VERB`; anything else
   named like the binary is counted as `other` and printed as its own line
   together with the worker PID list. Additions:
   - `GOPI_PROC_ROOT` (default `/proc`) — the parameterized procfs root, so the
     counter is testable against a synthetic tree instead of whatever the host
     happens to be running;
   - `--count` — a counting seam that prints the breakdown and exits without
     launching anything;
   - no `pgrep` / `pkill` / `killall` remains anywhere in the script;
   - an empty procfs root now **fails** (exit 3) rather than printing
     "live gopi workers: 0" as if it were a fact.
   Launch semantics (`setsid` + `nohup` + `/dev/null` stdin, 1 s cadence, log
   path contract, `--dry-run`, `--install`) are unchanged.
3. **Tests** — `internal/testutil/procname_gate_test.go` (in `testutil`, which
   already owns the repo-level script gates):
   - `TestWorkerUpCountsArgvShapeNotName` — the minimal repro (Rule 10.5): a
     synthetic procfs fixture with 3 real worker shapes and 6 non-worker
     gopi-shaped decoys (`gopi task run <id>` one-shot child, `gopi task`, `gopi
     serve`, `argv[1] == "workerx"`, missing `argv[1]`, empty cmdline, a bash
     process whose **command line** carries the pattern, `gopirunner`, and a
     `comm` that is the whole command with spaces) plus a non-pid directory and a
     pid-shaped file. Asserts the count is exactly 3 workers / 6 others and that
     the PIDs are the fixture's workers; then evaluates the **old predicate**
     (name-only) on the same fixture and asserts it is strictly larger — so the
     test fails on the old code instead of merely passing on the new one.
   - `TestWorkerUpScriptNamesItsValues` — the Rule 1b/13/14 gate: the script's
     env defaults must equal `internal/procname`'s constants (env name **and**
     value); a knob the gate does not know is an error, not a silent pass.
   - `TestWorkerUpScriptHasNoPgrepOrPkill` — pins the counting method (no
     name/pattern matcher) and executability.
4. **`devdoc/worker-count.md`** — this report.

### A real bug the fixture found (not a test artefact)

The first run of the decoy test returned `1 worker / 0 others` where the fixture
had 3 workers and 6 others. Root cause: `IFS= read -r name < "$d/comm"` returns
**non-zero at EOF when the file has no trailing newline**, and the script had
`|| continue` — so any `/proc/<pid>/comm` without the kernel's trailing newline
was skipped silently, along with every subsequent entry that hit the same path
(the fixture's worker 1001, then all the decoys). The kernel always appends a
newline today, so this was latent, not the live bug — but it is exactly the
class of silent-drop the fix must not have. Fixed with `|| true` plus a comment;
the fixture (entries `1001` and `1012`) now pins it (Rule 10.5).

## Not Done

- **The installed launcher is not refreshed.** `~/bin/gopi-worker-up.sh` →
  `~/.local/bin/gopi-worker-up.sh` still holds the OLD (miscounting) copy; the
  live-host "after" number above was measured by running the branch copy
  (`~/workspace/gopi-wc/scripts/gopi-worker-up.sh --count`). Refreshing the
  installed copy is `scripts/gopi-worker-up.sh --install`, and I deliberately did
  **not** run it: it would install an unmerged branch's launcher over the host's
  stable copy. Run it after the merge (the merge/rebuild loop, Rule 12).
- The launcher lives in `scripts/` **and** is symlinked into `~/bin`; only the
  repo copy is tracked, so the symlink target stays a host-level `--install`
  step, unchanged from before.
- No unit-test coverage of the launch path (`setsid`/`nohup`) — unchanged
  behaviour, `--dry-run` remains the only seam there, as before.
- `pgrep`/`pkill` elsewhere in the repo were not audited — out of scope.
- Two tiny staging copies were left behind because the sandbox refuses `rm`
  outside the main repo's working directory: `~/workspace/gopi-wcount` and
  `~/workspace/gopi-wcount-wt` (two files each — the pre-worktree copies of
  `procname.go` / `gopi-worker-up.sh` from the salvaged artifacts, used while the
  first `worktree add` was still in flight). They are NOT the deliverable and
  hold no unique content; delete them by hand.

## Verification

- `bash -n scripts/gopi-worker-up.sh` → OK (also enforced by
  `TestScriptsAreBoundedAndOffTmp`).
- `CGO_ENABLED=0 go build ./...` → **BUILD_OK**.
- `CGO_ENABLED=0 go test ./...` → **exit 0, no failing package** (the run
  filters every `ok`/`no test files` line and prints only failures; it printed
  nothing, `TEST_EXIT_PIPE=0`). Run at commit `49ac43f7`; the only later change
  was this report's own text, no Go or shell code.
- Targeted:
  `go test ./internal/testutil/ -run TestWorkerUp -count=1 -v` → **3/3 PASS**
  (fixture log line: `shape count = 3 workers / 6 others; the OLD name-only
  predicate = 10 (the 7 difference is exactly the reported inflation)`).
- The gates that own this area:
  `go test ./internal/testutil/ ./internal/standards/ ./internal/stylegate/ -count=1`
  → `ok testutil 7.0s`, `ok standards 26.1s`, `ok stylegate 15.5s`.
- **Canary (Rule 10.5 proof):** with `pgrep -x "$PROC_NAME" | wc -l` patched back
  into the script, `TestWorkerUpCountsArgvShapeNotName` **FAILS**
  (`cannot read the count from "(not"`); with the new counter it passes.
- Live host, final sampling:
  `--count` → **5 workers** (pids 380024 380057 463445 463556 463612) vs the
  independent `ps -eo comm,args` truth `named=12 workers=5`, while the old line
  prints **12**.
- `git status` self-check: every edit is inside the worktree
  `~/workspace/gopi-wc`; `~/work/GoPi` does not carry `internal/procname` and
  lists none of these files.

### Commit

fix(worker-up): count workers by argv shape, not by process name — ONE commit on
branch `fix/worker-count` (worktree `/home/user/workspace/gopi-wc`). The hash is
resolved with `git log -1 --format=%h fix/worker-count`, never hard-coded: this
report is committed INSIDE that same commit, so a literal here would be stale the
moment it is written.
