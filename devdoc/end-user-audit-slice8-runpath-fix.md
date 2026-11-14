<!-- doc-class: historical -->
# Task Report: RP-1 — the plain run path sent the prompt verbatim

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task:** `01a0dec6-3a8a-7a5d-986f-2d6c1a5d8dee` (bug / end-user-audit / slice8 / runpath / F1)
**Branch:** `task/01a0dec63a8a7a5d986f2d6c1a5d8dee`
**Worktree:** `/home/user/workspace/gopi-01a0dec63a8a7a5d986f2d6c1a5d8dee`

## Completed

The defect: `gopi -h`'s Inputs row promises `"<prompt>"  The task text`, while the
run path wrapped that text in the agent-harness frame (`[EPIC GOAL]` + the
task-list directive + the reframe protocol, ~1000 chars) — uniform for `@PATH`,
a positional file, stdin, `\@text` and `--prompt`, and reproduced with `env -i`
so it was the run path's own doing, not an inherited environment.

- **`internal/agent/loop.go` — the frame is now confined to the surfaces that ARE
  tasks.** `Agent.RunPrompt` is a new entry point whose first user message is the
  caller's text verbatim; `Agent.Run` is unchanged (frame + task-list deadline +
  reframe directive). The decision is the `plainPrompt` field, read once in
  `runEngine`'s first-turn branch.
- **The discriminator is the wrapper identity, not the entry point.** A
  `gopi task run` child reaches the *same* `main` (the wrapper spawns
  `<gopi>` with the briefing on stdin), so "called from main.go" cannot
  distinguish the two. `Agent.RunPrompt` sets `plainPrompt = taskChildRowID == ""`
  — `taskChildRowID` comes from the single `NewAgent` read of
  `tools.TaskWrapperEnv`, the same read that already selects the session-dir
  layout. A task child therefore keeps the frame; the operator's own
  `gopi "<prompt>"` does not.
- **`cmd/agent/main.go`** calls `RunPrompt`. The merger
  (`cmd/agent/merge_run.go:2493`) still calls `Run`, so its framing is untouched.
- **`internal/config/help.go` — the page now states the difference.** The Inputs
  block carries `FrameNoteText` (exported, Rule 14): the prompt is the model's
  first user message as typed, the run is still governed by the system prompt and
  its own checkpoints, and a DISPATCHED run (`gopi task run` / `gopi worker` /
  `gopi merge`) IS framed.
- **Rule 1/13/14 cleanups the fix needed:** the framing template moved from an
  inline `fmt.Sprintf` literal to `epicGoalFramingFormat` (`internal/agent/reframe.go`);
  the three frame markers (`config.FrameMarkerEpic|TaskList|Reframe`) are named
  constants the page and the test both read; `cliverbs.Merge` is a new leaf
  constant (the top-level verb `internal/config` could not otherwise spell, since
  `cmd/agent` is `package main`) and `cmd/agent/cli_verbs.go`'s `CliVerbMerge`
  now aliases it instead of holding the literal.
- **Regression test:** `cmd/agent/cli_runpath_boundary_rp1_test.go` — three tests,
  two arms, asserting at the REQUEST BOUNDARY against the REAL binary.

## Not Done (honest)

- **The `@PATH` / positional-file / stdin input forms are not separately
  exercised by the new test.** The fix is at the loop, not the argv parser, so
  all input forms funnel through the one `Agent.RunPrompt` call in `main.go`;
  the test drives `--prompt`, which reaches the same call site. The other forms
  were measured at the boundary during the audit (0 tokens, on the pre-fix tree)
  and are `Not Done` here as *re-verification*, not as a known gap.
- **The harness's own checkpoints still ride the first turn** — with the memory
  enforcement on, a plain prompt's conversation gets a `[system] TASK-START
  CHECKPOINT` after it (measured: 862 chars). That is a separate mechanism from
  the epic frame; it is named in the help page (`FrameNoteText`'s second
  sentence) and excluded from the arm-A assertion by
  `GOPI_NO_ENFORCE_MEMORY=1`. Whether those checkpoints should also be suppressed
  for a shell prompt is a NEW question, not part of RP-1, and no row covers it.
- **No test asserts the reverse direction on the argv surface** (that a plain
  run has no ledger row etc.) — out of scope for this defect.
- **`internal/tools` is RED in this worktree, and I did not prove it is red on the
  base.** Five tests fail (`TestAssertionTotalityAuditGate`,
  `TestSweepKeepsTheWorktreeOfALiveRecordedRunner`, `TestClaimOrderHasOneDefinition`,
  `TestEveryWorktreeRemovalArmQuestionsTheGround`,
  `TestWorktreeSweepRefusesEvictionOfAPeerCheckout`). The evidence that they are
  not this change's: `git show --name-only HEAD` lists no file under
  `internal/tools/`, and none of those test files reads `internal/config`,
  `cliverbs`, `RunPrompt` or the help page (`grep` over the three named files →
  nothing). That is a strong argument, not a measurement — the base-commit run was
  not made, so "pre-existing" here is inferred, not observed. One of the five is an
  environment precondition (`live_claim_worktree_veto_test.go:269`: "the raw /proc
  spelling equals procCmdline()'s (\"\")"), which reads as host-dependent. Whoever
  next touches `internal/tools` should re-run these on master before attributing
  them.
- **The task-child arm sets the wrapper env by hand** rather than going through
  `gopi task run`. Driving the real wrapper would need a real ledger row and
  would leave a completed row behind; the env it exports is the contract
  (`cmd/agent/cli_tasks.go:2308`), and the arm asserts exactly that contract.

## Verification

- **Build:** `go build ./...` → OK (and `CGO_ENABLED=0 go build -o <evidence>/bin/gopi ./cmd/agent/` → OK).
- **Tests:** branch-scoped, the packages the diff touches —
  `go test ./internal/agent/ ./internal/config/ ./internal/standards/ ./internal/cliverbs/ ./internal/tools/ ./cmd/agent/`
  → `internal/agent`, `internal/config`, `internal/standards`, `internal/cliverbs`
  and `cmd/agent` are GREEN; `internal/tools` shows 5 failures whose test files no
  line of this diff reaches (see `## Not Done`).

  ```
  go test ./internal/tools/ -count=1        # exit=1
  --- FAIL: TestAssertionTotalityAuditGate                    assertion_totality_audit_test.go:118
  --- FAIL: TestSweepKeepsTheWorktreeOfALiveRecordedRunner     live_claim_worktree_veto_test.go:269
  --- FAIL: TestClaimOrderHasOneDefinition                     tasklist_order_test.go:258
  --- FAIL: TestEveryWorktreeRemovalArmQuestionsTheGround
  --- FAIL: TestWorktreeSweepRefusesEvictionOfAPeerCheckout
  go test ./cmd/agent/ -count=1 -run TestWorkerDaemon_OnceDrainsAndExits   # ok
  go test ./cmd/agent/ -count=1 -run 'TestRunPathPlainPromptIsVerbatim|TestRunPathTaskChildKeepsTheFraming|TestHelpStatesWhereTheFramingApplies'  # ok
  ```
- **Ran:** (verbatim, in the worktree, against the binary built from it)

  ```
  # arm A — a plain prompt, request boundary, 0 real tokens
  env -i PATH=/usr/local/bin:/usr/bin:/bin HOME=/home/user \
    GOPI_HOME=/home/user/workspace/gopi-sandbox/sandbox-slice8-fix-08d2736a \
    /home/user/workspace/gopi-01a0dec63a8a7a5d986f2d6c1a5d8dee/evidence-bin/gopi \
    --base-url http://127.0.0.1:18131 \
    --db /home/user/workspace/gopi-sandbox/sandbox-slice8-fix-08d2736a/.gopi/memory.db \
    --prompt "SLICE8TOKEN-FIXED plain shell prompt"
  # arm B — the task-child shape (the wrapper's own env contract)
  env -i PATH=... HOME=/home/user GOPI_HOME=<sandbox> \
    GOPI_TASK_WRAPPER_ID=t-01a0dec6test GOPI_TASK_RUN_ID=t-01a0dec6test \
    GOPI_TASK_RUN_CLOSEOUT=off <built-bin> --base-url http://127.0.0.1:18131 \
    --db <sandbox>/.gopi/memory.db --prompt "SLICE8TOKEN-TASKCHILD briefing text"
  # the red-proof: pre-fix behaviour, one line flipped
  #   a.plainPrompt = false
  GOPI_TEST_BIN=/tmp/gopi-prefix go test ./cmd/agent/ -run 'TestRunPathPlainPromptIsVerbatim|TestRunPathTaskChildKeepsTheFraming'
  ```
- **Result:**
  - arm A, after the fix: `req-000` first user message = `SLICE8TOKEN-FIXED plain
    shell prompt` — 36 chars, byte-identical to the prompt, no `[EPIC GOAL]`,
    `[TASK_LIST_BEGIN]` or `[EPIC REFRAME]` anywhere in the request. (Before the
    fix, the same probe recorded **2194 chars** of framed body —
    `evidence/echo/req-000.json` on the audit's branch.)
  - arm B, after the fix: first user message 1293 chars, opening
    `[EPIC GOAL]\nSLICE8TOKEN-TASKCHILD briefing text\n\nBased on this Epic, …`.
  - red-proof: with `plainPrompt = false`, `TestRunPathPlainPromptIsVerbatim`
    **FAILS** (7 assertions: not verbatim + epic marker + task-list + reframe,
    on requests 0 and 1) while `TestRunPathTaskChildKeepsTheFraming` **PASSES** —
    the two arms separate the defect from the contract.
  - `gopi -h` prints the new Inputs row: *"gopi adds no epic goal, task-list
    directive or reframe protocol of its own to it: the text is the model's first
    user message as typed…"*.
- **Commit:** see the branch tip (`git log --oneline -1`).

## Evidence (self-filed)

- `env -i … --prompt "SLICE8TOKEN-FIXED plain shell prompt"` → `rc=0`; the echo
  endpoint recorded `req-000.json` whose `messages[0].content` is
  `SLICE8TOKEN-FIXED plain shell prompt` (36 chars, no frame) — read back with
  `python3` from
  `<session>/evidence/echo/req-000.json`.
- `env -i … GOPI_TASK_WRAPPER_ID=t-01a0dec6test … --prompt "SLICE8TOKEN-TASKCHILD briefing text"`
  → `rc=0`; `req-003.json` first user message 1293 chars beginning
  `[EPIC GOAL]\nSLICE8TOKEN-TASKCHILD briefing text` — the frame survives on the
  task surface.
- Red-proof: `GOPI_TEST_BIN=/tmp/gopi-prefix go test ./cmd/agent/ -run …` printed
  `--- FAIL: TestRunPathPlainPromptIsVerbatim` with the line
  `the FIRST user message is not the prompt verbatim:` (got 862 chars) and
  `--- PASS: TestRunPathTaskChildKeepsTheFraming`.
- `go build ./...` → `exit=0`, no output.
- `GOPI_TEST_BIN=$PWD/evidence-bin/gopi go test ./cmd/agent/ -run 'TestRunPathPlainPromptIsVerbatim|TestRunPathTaskChildKeepsTheFraming|TestHelpStatesWhereTheFramingApplies' -v`
  → three `--- PASS` lines, `ok github.com/nick/gopi/cmd/agent`.
- Cost of the boundary evidence: **0 real tokens** — a local endpoint on
  127.0.0.1; no upstream host was contacted.
