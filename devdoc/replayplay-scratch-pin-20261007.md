<!-- doc-class: normative -->
# Task Report: internal/replayplay scratch pin + scripts/replay/chat_replay_xtest.sh tmp-ban (task 01a11346)

Base: pristine master `e036ea95b`. Worktree: `~/workspace/gopi-01a1134662b67965845ff5acde9f5b08`.

## Completed

Three `go test ./internal/testutil/` reds, each fixed AT THE OFFENDER'S SITE. No gate
predicate, marker list or exemption table was touched — the diff is two files plus
one new test file.

1. **`internal/replayplay` had no scratch pin** (reds 1 and 2, the same offender).
   `material_test.go` and `jsonl_writer_shape_test.go` create scratch through
   `t.TempDir()` six times, and the package carried no `TestMain`, so those
   directories landed on the AMBIENT temp root — `/tmp` on this host, a directory on
   the 147G `/` partition behind the 2026-09-11 exhaustion.
   Fix: new `internal/replayplay/testmain_scratchroot_test.go`, the ONE-LINE entry
   point the gate's own message names — `testutil.RunSuiteMain(m, "replayplay")`.
   That single call pins the scratch root (asserted off `/` by DEVICE ID, not by
   path spelling — Rule 22), installs the notify-env sandbox (`suiteenv.go`, so the
   suite cannot read the operator's live `settings.json`) and arms the fixture-lifetime
   binding. It is the same shape as `internal/cgroupscope`, `internal/scratch`,
   `internal/standards` and the other 34 suites.

2. **`scripts/replay/chat_replay_xtest.sh` created scratch without the shared helper**
   (red 3). It built its scratch directory with
   `SCRATCH_PARENT="${TMPDIR:-$HOME/.cache}"; mktemp -d -p "$SCRATCH_PARENT"` — a path
   the script invented, with no device assertion and no registry entry.
   Fix: the script now sources `scripts/lib/tmp-ban.sh` and takes its directory from
   `gopi_scratch_mkdir chat-replay-xtest` (the helper asserts `stat -c %d` of the
   scratch root differs from `/`'s and registers the directory for trap-time removal).
   The trap became `gopi_scratch_cleanup` (which kills background children FIRST —
   Xvfb, xterm — before removing the tree, exactly the ordering an X11 script needs),
   still guarded by `--keep`. No new construction, no second scratch root.

3. **Report** — this file.

## Not Done

- **The marginal shape is NOT caught by the gate** — see the third bullet in
  `## Evidence`.
- **`TestScriptsAreBoundedAndOffTmp` is happy with a helper-sourced `mktemp` as well**
  (measured: sourcing alone flips it green even when `mktemp -d -p "$(gopi_scratch_home)"`
  is kept). That is a gap in the gate's `createsScratch` predicate — it matches
  `scratch_mkdir` as a create marker and therefore stops asking. I did NOT widen it
  (the brief forbids gate edits and the marginal shape is a separate defect report);
  the fix here is the helper call on its merits, not the gate's floor.
- No devdoc index row is added by hand: the `write` hook classifies this document
  (`devdoc/DOC-INDEX.tsv`, Rule 36); `gopi doccheck` is the check.

## Verification

- Build:     `go build ./...` → exit 0
- Tests:     `go test ./internal/replayplay/ -count=1` → `ok github.com/the operator/gopi/internal/replayplay 0.559s`
- Ran:       `go test ./internal/testutil/ -count=1 -run 'TestScriptsAreBoundedAndOffTmp|TestEveryTempUsingSuiteIsPinned|TestPinCoverageTable$'`
             (the brief's exact command) — **exit 1 before / exit 0 after**; logs
             `base.log`, `final-green.log` in the session dir.
             `scripts/replay/chat_replay_xtest.sh --bin /home/user/work/GoPi/bin/gopi`
             → job-1, exit 0, `ALL XTEST CHECKS PASSED — scratch:
             /home/user/workspace/gopi-test-scratch/queue/queue-chat-replay-xtest-3446840`
             (log `script-run.log`); that directory was gone afterwards (the trap's
             `gopi_scratch_cleanup` removed it) and the queue holding it is on device
             `2052` — `/` is `2051`.
- Result:    before → `--- FAIL: TestPinCoverageTable` (twice naming
             `internal/replayplay`), `--- FAIL: TestScriptsAreBoundedAndOffTmp`
             (`scripts/replay/chat_replay_xtest.sh`), `--- FAIL:
             TestEveryTempUsingSuiteIsPinned` (`internal/replayplay`); after → `ok
             github.com/the operator/gopi/internal/testutil 1.794s` and the replayplay row of
             the coverage table reads `PIN PIN`.
- Commit:    see the branch tip (single commit).

## Evidence (self-filed)

Red-proof 1 — TestMain withdrawn, `mv internal/replayplay/testmain_scratchroot_test.go
"$S/.bak"`; measured `red1 exit=1` with:
```
--- FAIL: TestPinCoverageTable (0.17s)
    pin_coverage_test.go:92: internal/replayplay creates scratch (t.TempDir()) but no TestMain routes the pin through internal/testutil …
    pin_coverage_test.go:95: internal/replayplay                        yes        yes       -       NO-PIN
--- FAIL: TestEveryTempUsingSuiteIsPinned (0.11s)
    scripts_gate_test.go:264: internal/replayplay creates scratch directories (t.TempDir()/os.MkdirTemp) but no TestMain pins the scratch root …
```
Restored → `final exit=0`.

Red-proof 2 — the original script restored byte-for-byte (`git show
HEAD:scripts/replay/chat_replay_xtest.sh`), measured `red2 exit=1` with:
```
--- FAIL: TestScriptsAreBoundedAndOffTmp (0.96s)
    scripts_gate_test.go:234: scripts/replay/chat_replay_xtest.sh creates scratch data but does not source/import scripts/lib/tmp-ban.sh …
```
The fixed file was restored from its saved copy: `diff -q` → byte-identical
(`sha256 46e812de6ff9fdd4…`), `bash -n` → syntax OK.

Red-proof 3 (marginal, reported not hidden) — helper sourced but `mktemp` kept:
`red3 exit=0` — the gate does not catch this shape. Recorded under Not Done.

Device ids measured on this host: `/` device `2051`, `/home/user/workspace` device
`2052` — the scratch root the helper asserts is on the `2052` filesystem, never `/`.
