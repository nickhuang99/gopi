<!-- doc-class: historical -->
# Task Report: master red — TestScriptsAreBoundedAndOffTmp / scripts/probe-daemon-term.sh

Task: `01a0a81c-106d-7c8b-9912-940878ebd207` (sibling reported: `01a0a6f1`, reporting-only change in the gate → no overlap here).

## Outcome (TL;DR)

**The briefing's repro is STALE — the defect was already fixed on master before this
task started. No content change was needed and none was made.**

`scripts/probe-daemon-term.sh` DOES source `scripts/lib/tmp-ban.sh` and routes its
scratch through `gopi_scratch_mkdir`; the gate is GREEN at the current master tip
`796b3333`. The fix landed in `611a0be6` (2026-09-16 12:53) — *after* the briefing's
repro point `30450fc6` (2026-09-16 09:36), which is why the repro still reproduced on
that older revision. Red-before/green-after is proven mechanically below.

## Root cause of the stale repro (5-why)

1. Symptom: `TestScriptsAreBoundedAndOffTmp` fails naming `scripts/probe-daemon-term.sh` at `30450fc6`.
2. Mechanism: at that revision the script carried `S="$(mktemp -d /dev/shm/gopi-term3-XXXXXX)"` and no `. scripts/lib/tmp-ban.sh`; `createsScratch` matched `mktemp` and the helper-name containment check failed.
3. Norm: the gate requires every scratch-creating script to source the shared helper (GOPI.md Rule 22).
4. Why it was fixed: `611a0be6` ("gate: forbid device-load generators (GOPI.md Rule 27) — the bounded-loop bypass") needed a green gate to ship the new Rule 27 detector, and repaired this script in the same commit ("Also fixes scripts/probe-daemon-term.sh … which was the last red in this gate").
5. Why the briefing was stale: it was written against `30450fc6` and not re-checked against the master tip before dispatch — the repro reproduced *by construction* on the old rev, so the report looked live. **The gate itself was never red at the current master tip.**

## Completed

- Reproduced the briefing's failure byte-identically on a **pristine master worktree**: `git worktree add --detach /home/user/workspace/gopi-probe-daemon-term 796b3333` + `go test ./internal/testutil/ -run TestScriptsAreBoundedAndOffTmp -count=1` → **PASS (green)**.
- Established that `611a0be6` (the fixing commit) is an **ancestor of HEAD** and is NOT contained in the repro point `30450fc6` (the red window is one commit wide).
- **Red-before proof (anti-vacuity, same worktree):** with `git checkout 30450fc6 -- scripts/probe-daemon-term.sh` the gate fails with *exactly* the briefing's message, naming this file:
  `scripts_gate_test.go:218: scripts/probe-daemon-term.sh creates scratch data but does not source/import scripts/lib/tmp-ban.sh …`
  Restoring the tip revision (`git checkout HEAD -- …`) makes it green again → the gate really does audit this file, and the green is earned, not vacuous.
- **Anti-vacuity, scan side:** the gate logs `Go /tmp gate: 592 test files scanned, 3 exempted by name (3/3)` and the script-side audit runs over `collectScriptFiles` (scripts/ + devdoc/ + testdata/), so the script is inside the audited set (the pre-fix run above proves it matches the predicate).
- **Rule 22 measured truth, not a path spelling:** the script's own scratch call site was exercised (`GOPI_TEST_HOME` pointed at a private base):
  `dir=/dev/shm/gopi-probe-check-…/workspace/gopi-test-scratch/queue/queue-gopi-term3-…`,
  `dir_dev=28 root_dev=2051` → **OFF `/` (measured)**; `gopi_scratch_cleanup` removed it → the helper's trap-time cleanup contract holds. `scripts/lib/tmp-ban.sh` carries `gopi_assert_off_root_fs` (compares the resolved path's device id against `/`) and the trap body `gopi_scratch_cleanup`.
- No content edit; the only artifact of this task is this report (truthful-reporting step of GOPI.md Rule 5: the investigation IS the deliverable).

## Verification

- `CGO_ENABLED=0 go test ./internal/testutil/ -run TestScriptsAreBoundedAndOffTmp -count=1` @ master `796b3333` (clean worktree): `ok github.com/nick/gopi/internal/testutil` → **exit 0**
- same command, pre-fix script (`30450fc6` revision) restored in place: `--- FAIL: TestScriptsAreBoundedAndOffTmp` naming `scripts/probe-daemon-term.sh` → **red as briefed**; re-restored tip → green again.
- `bash -n scripts/probe-daemon-term.sh` → exit 0 ("bash -n: OK")
- `/tmp` literal scan of the script: only the helper's own import path `scripts/lib/tmp-ban.sh` (lines 7–8) — **no `/tmp` literal** (the gate's rule 3 also passes).
- `CGO_ENABLED=0 go build ./...` → OK.
- helper-measured off-`/`: `dir_dev=28` vs `root_dev=2051` (device-id comparison, per Rule 22).
- `611a0be6 2026-09-16 12:53:56` is an ancestor of `796b3333`; `30450fc6 2026-09-16 09:36:06` is its parent side → the red existed only in that ~3h window.

## Not Done (honest)

- **No code change was made, deliberately** — there is nothing on master to fix for this file. A change here would be churn on a green gate.
- The **whole `internal/testutil` package** does not finish green in one 300 s budget run in this environment (it has many slow, host-bound probes; not this gate's business). Not claimed as green: the report's acceptance is scoped to `-run TestScriptsAreBoundedAndOffTmp`, which is green as shown. The broader package run was left to a background job and is not evidence for or against this task.
- `gopi task show 01a0a6f1` reports `no task with task_id="01a0a6f1"` — the sibling attributed in the briefing could not be read from the ledger, so the overlap claim is taken from the briefing text (reporting-only edits in `scripts_gate_test.go`, no overlap with this script's content).
- No devdoc beyond this report: no behaviour was authored, and the double-accounting hazard of claiming a docs fix on a docs-only branch outweighs its value.

## Recovery (Rule 6d declaration)

- Branch: `docs/probe-daemon-term-gate-verification` in the main repo (`/home/user/work/GoPi`), created as a **worktree** at `/home/user/workspace/gopi-probe-daemon-term` → the commit is already shared; no push needed.
- Recovery command: `git show docs/probe-daemon-term-gate-verification:devdoc/probe-daemon-term-gate-verification.md`
- The worktree is a verification stand (verification-only) and is removed after the report commit — the report is on the branch in the main repo.
