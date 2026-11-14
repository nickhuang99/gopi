<!-- doc-class: historical -->
# Task Report: bash kill-reason attribution is decided by BUDGET SHAPE, not by watcher arrival order

Task `01a11edccab678729c80588192bf10f7` · branch `task/01a11edccab678729c80588192bf10f7`

## Completed

- **Root cause (5-why).** `TestBashWithTimeout_PositiveKills` intermittently
  failed with `command killed: no output for 1s` instead of `timed out after 1s`.
  Why: the kill reason was decided *at the kill site* from `cmdCtx.Err()`
  (`internal/tools/bash.go`, step 8, idle monitor). Why that is racy:
  `bashBudget` clamps the idle net down to the absolute budget
  (`idle = min(idle, abs)`), so with `timeout_sec=1` the 120s net becomes 1s —
  the net and the budget come due in the **same second**. Two watchers can then
  reach `killReasonSlot` (`internal/tools/bash.go` step 6b ctx watcher / step 8
  idle ticker) in either order, and `set()` is **first-writer-wins**: the
  goroutine the scheduler ran first decided the wording the model reads.
  Observed: `TestBashWithTimeout_PositiveKills` red inside `-count=20`.
- **Fix (option 1 of the two candidates — deterministic attribution from budget
  shape).** New pure helper `killReasonIdleOrBudget(cmdCtx, absTimeout,
  idleTimeout)` (`internal/tools/bash.go`), called by the idle watcher in place
  of the inline `cmdCtx.Err()` check:
  - `idleTimeout == absTimeout` → `KillReasonAbsoluteTimeout`. An equal pair is
    proof that the net was clamped to the budget: `bashBudget` is the ONE place
    that clamp happens (Rule 14), so the statement is true **before** the
    deadline is observable through `cmdCtx` and holds under either arrival order.
  - `absTimeout > 0 && cmdCtx.Err() == context.DeadlineExceeded` → same answer
    for an **uncapped** net (`idle < abs`) that fires as its own deadline passes.
  - otherwise → `KillReasonIdleTimeout`: the net doing its own job, silence well
    before the budget. The wording `no output for …` is unchanged, and an
    unlimited command (`timeout_sec=0`, `abs == 0`) can only take this branch —
    a command with no deadline has no budget death.
- **Regression guard** `internal/tools/bash_kill_reason_race_test.go`:
  - `TestBashKillReasonAttribution_IsDecidedByBudgetShape` — table-drives the
    rule over the two shapes (clamped / uncapped) × two arrival states
    (deadline observable / not), plus the unlimited case.
  - `TestBashBudget_ClampIsWhatMakesTheShapeDecidable` — guards the **premise**:
    a budget below the configured net clamps, `timeout_sec=0` does not. Without
    this the shape test above would silently start lying if the clamp were
    removed.
  - `TestBashWithTimeout_PositiveKills_MessageIsStable` — end-to-end on
    `BashWithTimeout("sleep 2", 1)`: asserts `timed out` is present AND
    `no output for` is absent, i.e. the exact wording the flake corrupted.
- **Comment at the kill site** updated to name the rule and point at the helper.

## Not Done

- **No `[attachmentN:]` files existed for this epic** — the raw briefing was
  archived in the session log and the reframed version was the only input, so no
  attachment was read or consumed. The report is written at
  `devdoc/bash-kill-reason-attribution.md` on the branch (the deliverable is the
  branch, Rule 6c); the raw transcripts are under this run's session directory.
- **Watchdog jitter is untouched, and was not part of the ask.** The idle
  monitor ticks at a hard-coded `1 * time.Second`, so with `timeout_sec=1` a
  tail case exists in principle: if the ticker lands at t=1.00s and the 1s
  deadline is scheduled a hair later, the idle watcher wins with the budget
  still unexpired. It is attributed `KillReasonAbsoluteTimeout` anyway — the
  clamped shape is the deciding fact — so the **message** is deterministic
  either way, which is the observed acceptance criterion. Not reproduced in
  `-count=50` + `-count=20`, and deliberately not "fixed" by adding a timer
  slack (that would be a behaviour change outside the ask).
- **Whole-package run kept out of the report's main claim, but it was run** and
  is green: `make test-budget PKGS='./internal/tools'` → exit 0, 272s,
  `outcome=pass` (the repo's declared entry point; a superset of the `-run`
  filters). The briefing's acceptance is still the `-count=50` run.

## Verification

- Build: `go build ./...` → **OK** (no output, exit 0)
- Tests:
  - `go test ./internal/tools/ -run 'TestBashWithTimeout_PositiveKills' -count=50`
    → **50 PASS / 0 FAIL**, `ok github.com/nick/gopi/internal/tools 200.719s`
  - `go test ./internal/tools/ -run 'TestBashIdleTimeout_|TestBashCommandTimeout|TestBashKillReason|TestBashBudget_Clamp|TestBashWithTimeout' -count=3`
    → **ok, 52.411s** (the `BashIdleTimeout_*` family and `TestBashCommandTimeout`
    still green — untouched)
  - RED guard proof (mutant): with the pre-fix rule restored *inside a
    compiling file*, `TestBashKillReasonAttribution_IsDecidedByBudgetShape`
    fails: `killReasonIdleOrBudget(abs=1s, idle=1s) = "idle-timeout", want
    "absolute-timeout"`
- Ran: (verbatim)
  - RED, before the fix:
    `go test ./internal/tools/ -run 'TestBashWithTimeout_PositiveKills' -count=20`
  - GREEN guard: `go test ./internal/tools/ -run 'TestBashKillReasonAttribution|TestBashBudget_Clamp|TestBashWithTimeout_PositiveKills_MessageIsStable' -count=1`
  - RED guard (mutant): same `-run` filter against `bash.go` with
    `if idleTimeout == absTimeout` mutated to `if false && idleTimeout == absTimeout`
  - GREEN acceptance: `go test ./internal/tools/ -run 'TestBashWithTimeout_PositiveKills' -count=50 -v`
- Result:
  - RED transcript (pre-fix, `-count=20`, exit 1):
    ```
    --- FAIL: TestBashWithTimeout_PositiveKills (1.00s)
        bash_test.go:62: expected 'timed out' in error, got: command killed: no output for 1s
        bash_test.go:65: expected 'timed out' in output, got: "exit=-1\n\n[command killed: no output for 1s]"
    FAIL	github.com/nick/gopi/internal/tools	20.664s
    ```
  - GREEN after fix, `-count=50`: `COUNT50_EXIT=0`, 50 × `--- PASS:
    TestBashWithTimeout_PositiveKills`, `--- FAIL` count = 0 (transcript:
    `artifacts/green-after-count50.txt`, 71 KB, every run's PASS line present).
  - Contract wording preserved: with `idle < abs` the message is still
    `command killed: no output for 1s` (`TestBashIdleTimeout_KillsStalledCommand`,
    `TestBashIdleTimeout_InteractivePrompt`,
    `TestBashWithTimeout_IdleKillUnderUnlimited` all green).
- Commit: `62fa4ce1d fix(tools): attribute a clamped idle kill to the absolute
  budget, deterministically` (the whole-package run above is a post-commit
  measurement and changed no file).

## Evidence (self-filed)

- RED, before any edit — `.gopi/sessions/01a11edc-…/artifacts/red-before-count20.txt`,
  measured `exit=1` with `FAIL github.com/nick/gopi/internal/tools 20.664s`.
- `go build ./...` → printed nothing, exit 0.
- `-count=50` run → `COUNT50_EXIT=0`, and `grep -c '^--- FAIL'` on the transcript
  printed `0` while `grep -c '^--- PASS: TestBashWithTimeout_PositiveKills '`
  printed `50`.
- Family run (`-count=3`) → `ok github.com/nick/gopi/internal/tools 52.411s`, exit 0.
- RED guard proof → `bash_kill_reason_race_test.go:106: … = "idle-timeout", want
  "absolute-timeout"` against the mutant, and the same filter green against the
  fix (`ok … 3.577s`).
- `git status --porcelain` in the worktree → empty after the commit
  (measured at close-out; the two scratch files this run created stayed under
  the session directory, never in the worktree).
- Artifacts: `/home/user/.gopi/sessions/01a11edc-cab6-7872-9c80-588192bf10f7/artifacts/`
  — `red-before-count20.txt`, `green-after-count50.txt`,
  `green-family-count3.txt`, `red-guard-mutant.txt`, `green-guard-after-fix.txt`,
  `bash.go.fixed`, `bash.go.mutant`, `red-guard-prefix.txt`.
- Environment note (why the long run went to a background job): a foreground
  `-count=50` run produces **no output for >2 minutes** (go test buffers per
  package), and the bash tool's own idle net killed it at 120s with
  `command killed: no output for 2m0s` — the very mechanism this task fixes,
  working as designed on an unlimited-budget-omitted call. Re-run as
  `job(action=start)`.

## Post-mortem note (Tier B, for the next reader)

Two traps met while red-proving this fix, both worth not re-discovering:

1. **`git show HEAD:internal/tools/bash.go > bash.go` is NOT a usable red-proof
   once a guard test calls the new symbol** — the guard then fails to *compile*
   (`undefined: killReasonIdleOrBudget`), which proves nothing about the rule.
   The working shape is a **mutant of the fixed file** (`if false &&
   idleTimeout == absTimeout`) that keeps it compiling and restores exactly the
   pre-fix logic; the guard then fails on its assertion, not on the build.
2. **`git stash` is refused in this repo** by the shared-stash guard
   (`internal/tools/shared_stash_guard.go`) — `refs/stash` is common to the 4
   linked worktrees, so a WIP swap must be done with `cp`/`sed` on files, never
   the stash stack.
