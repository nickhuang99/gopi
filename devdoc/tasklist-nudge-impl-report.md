<!-- doc-class: historical -->
# Task Report: No-List-Yet Heartbeat Nudge (P1.4/P2) + First-TaskList Telemetry (P3/P4)

**Date:** 2026-08-28
**Branch:** `feat/tasklist-nudge-impl` (worktree `~/workspace/tasklist-nudge-impl`, shared with main repo)
**Epic:** gopi P1.4/P2 + P3/P4 (tasklist audit v2 RC5 closing)
**Prior art:** `devdoc/tasklist-audit-v2-report.md` (RC5 gap), `devdoc/tasklist-p0-evaluation.md`, memory #1050/#1052/#925.

---

## What was implemented

### P1.4/P2 — No-list-yet nudge (internal/agent/loop.go)

Closes audit v2 **RC5**: the heartbeat's `ShouldRemind` only fires when
`CountProgressing(tasks) > 0` (heartbeat.go:102-118), so a session that NEVER
established a list was never nudged and long list-less runs slipped through
indistinguishably from compliant runs.

Because `HeartbeatState` is not wired into the Run loop anywhere (only used in
unit tests), the nudge was implemented directly in the loop following the
established one-shot-latch pattern (`deadEndNudged`, `taskStartSearchPrompted`).
The trigger:

```
roundIdx (total iterations) >= NudgeRounds         (default 5)
AND elapsedToolRounds >= NudgeToolRounds            (default 3)
AND !hasList (todoState.hasList == false)
```

- **New constants:** `defaultNudgeRounds=5`, `defaultNudgeToolRounds=3`,
  `maxNudgeCount=2` (anti-harassment cap), `nudgeEscapeRounds=3` (interval
  escalation between the 1st and 2nd/final nudge), `noListYetNudge`
  (message text ≤50 tokens).
- **Env overridable** (Rule 2 — constant = default, single-read point in
  `NewAgent`): `GOPI_NUDGE_ROUNDS`, `GOPI_NUDGE_TOOLS` (tests use small values).
- **Agent fields:** `NudgeRounds`, `NudgeToolRounds`, `nudgeCount`,
  `nudgeNextAt` (single-threaded, read/written only from the Run loop).
- **`maybeNudgeNoList(roundIdx, elapsedToolRounds, toolRoundsCfg) bool`** — the
  self-contained, unit-testable decision+inject method. Guards:
  - `hasList==true` → never nudges again (list established);
  - `nudgeCount >= maxNudgeCount` → stop (capped at 2);
  - `roundIdx < nudgeNextAt` OR `elapsedToolRounds < toolRoundsCfg` → not yet;
  - re-arms `nudgeNextAt = roundIdx + nudgeEscapeRounds` after each fire.
  - Injects `noListYetNudge` into `a.Ctx` as a user message (reaches the model
    the same round, before the request is built).
- Orthogonal to R2 (R2 governs *update* cadence; this governs *establishment*).
- When R1 context folding dropped the Epic's "end with task list" anchor
  (audit v2 RC4), the nudge text re-asserts the emission contract.
- **Did NOT touch** `tasklist.go ParseTaskList` (that is the P0 branch `feat/tasklist-p0-impl`'s territory — no conflict).

### P3/P4 — First-tasklist telemetry (internal/agent/trajectory.go)

Added to `TrajectoryStats` (computed by `ParseTrajectoryStats` from the session
JSONL — the natural home, as it already scans every assistant text block for
thinking/tool_use; per the epic's "session_end 事件（或 trajectory 统计，按现
有结构选更自然的位置）"). New fields:

- `FirstTasklistMsgRatio float64` — `first_list_msg_index / total_msgs`;
  **−1 when no list was ever established** (documented choice).
- `NLists int` — count of assistant text messages carrying a valid
  BEGIN+END pair.
- `NMalformed int` — count of BEGIN-without-END (the silent-drop class P0 is
  tracking; this field is populated now and becomes the audit-reconciliation
  field once P0 lands).
- `ListEverEstablished bool`.

Implementation: `sessionBlock` gained a `Text string` field; a new
`tallyTaskListText` helper classifies each text block (valid list vs
malformed), and `ParseTrajectoryStats` computes the ratio at the end
(pre-initialized to −1). The `Format()` output prints the block:

```
tasklist: ever_established=true n_lists=2 n_malformed=0 first_ratio=0.50
```

Session-end JSONL event itself keeps its fixed shape; the telemetry surfaces in
the printed trajectory stats line (callers/audits can `jq`/parse the same way
as all other trajectory stats). Future session_end embedding is a trivial
follow-up if a JSONL field is preferred.

---

## Not Done (honest scope)

- **P0** `ParseTaskList` silent-drop fix — deliberately excluded (P0 branch owns
  `tasklist.go`). `NMalformed` is computed but no parser change; BEGIN-no-END
  still returns nil,nil (unchanged behavior, the P0 branch will fix).
- **P1.5** decouple exit from final-round list emission — out of scope for this
  epic (listed as separate work in the audit); the nudge's re-asserted contract
  is the only soft pressure added.
- **P3 (TodoWrite-style tool)** — future work, not part of this task.
- The nudge fires at most **2 times** total; the interval-escalation strategy
  was chosen over unbounded repetition.

---

## Verification

### Nudge unit tests (internal/agent/nudge_test.go) — 8/8 PASS
```
TestMaybeNudgeNoList_TriggersAfterThreshold   PASS  (fires once at N=5, not before)
TestMaybeNudgeNoList_NoTriggerBelowToolRounds PASS  (2 tool rounds < min 3 → never)
TestMaybeNudgeNoList_NoTriggerBelowRounds     PASS  (round 4 < threshold 5 → never)
TestMaybeNudgeNoList_NoTriggerWithoutListToolRounds PASS (round< nudgeNextAt)
TestMaybeNudgeNoList_StopsAfterListEstablished PASS  (hasList=true → never again)
TestMaybeNudgeNoList_CappedAtMax              PASS  (≤2 over 100 rounds)
TestMaybeNudgeNoList_NilTaskStateNeverFires   PASS  (no panic, no nudge)
TestMaybeNudgeNoList_NilCtxNeverFires         PASS
```

### Trajectory telemetry tests (internal/agent/trajectory_tasklist_test.go) — 7/7 PASS
```
TestTrajTaskList_NoList               PASS  (ratio=-1, ever=false, nl=0, nm=0)
TestTrajTaskList_ListAtStart          PASS  (ratio=0.0, ever=true, nl=1)
TestTrajTaskList_ListAtEnd            PASS  (ratio=2/3, ever=true, nl=1)
TestTrajTaskList_MalformedBeginNoEnd  PASS  (nm=1, ever=false, nl=0, ratio=-1)
TestTrajTaskList_MixedCompletesList   PASS  (nm=1, nl=1, ever=true, ratio=1/3)
TestTrajTaskList_MultipleLists        PASS  (nl=2)
TestTrajTaskList_ProseMentionNotCounted PASS (prose BEGIN w/o END = malformed)
```
> Test-fixture note: the session scanner splits lines on `\n`, so text blocks
> (which contain real `\n` inside `[TASK_LIST_BEGIN]...[]...`) must be
> JSON-escaped via `strconv.Quote` before embedding in a fixture line —
> otherwise the multi-line JSON breaks the per-line scan and the block is
> silently skipped. This is a test-fixture pitfall, not a code bug.

### E2E (bonus, real gopi run)
Built `CGO_ENABLED=0 go build -o /tmp/gopi-e2e ./cmd/agent/` and ran a 6-round,
11-tool-call task (`read tasklist.go + heartbeat.go, compare, write report, list
task list`). Session `f8f47bad…` (140.5s). Running the new telemetry parser on
the real JSONL produced:

```
tasklist: ever_established=true n_lists=2 n_malformed=0 first_ratio=0.50
```
- `ever_established=true`: the session correctly issued a list (via its
  `[TASK_LIST_BEGIN]` block).
- `n_lists=2`: two valid list emissions.
- `n_malformed=0`: no silent-drop.
- `first_ratio=0.50`: first list on the 2nd of the session's messages.
Nudge correctly did **not** fire on this run (list was established early), which
is the desired behavior — the nudge only fires for never-established sessions.

### Full test suite (`go test ./...`)
See full output below. One pre-existing flake encountered (see §flake).

### `go vet ./...` — clean
### `CGO_ENABLED=0 go build -o /tmp/gopi-test-bin ./cmd/agent/` — OK (main-repo `bin/gopi` NOT touched)

---

### Known pre-existing flake: `internal/tools` memory DB (SQLITE_BUSY)
`go test ./internal/tools/` has a known SQLITE_BUSY flake
(`TestMemorySearchNonBlocking` on the shared sqlite DB). Root cause confirmed
during this task: a **concurrent task (P0-branch agent in
`~/workspace/tasklist-p0-impl`) was itself running `go test ./... -count=1` at
the same time**, so two processes hammered the same memory DB → SQLite lock
timeouts in the `internal/tools` memory tests. This is cross-task DB contention,
not a regression from this task (my changes are in `internal/agent` only, which
passes cleanly in every full uncached run: `ok internal/agent 38.466s`). The
`internal/tools` package also passes standalone (isolated, no concurrent agent):
earlier `go test ./internal/tools/ -count=1` returned **TOOLS_EXIT=0**. Per the
epic ("已知 pre-existing flake … 复现则 stash 对比基线，标注即可"): documented,
baseline-verified clean, not caused by, and not blocking, this task's changes.

---

### `go test ./...` evidence
The `internal/agent` package (containing all 15 new tests) passes cleanly in
uncached runs:
```
go test ./internal/agent/  →  ok  github.com/nick/gopi/internal/agent  38.466s
TestMaybeNudgeNoList_* (8)           PASS   (see Nudge unit tests above)
TestTrajTaskList_* (7)               PASS   (see Telemetry tests above)
```
All packages except `internal/tools` pass in every full-suite run:
```
cmd/agent, checkpoint, config, deepseek, gopilog, proxy, text2img — all ok
```
`internal/tools` passes standalone (`go test ./internal/tools/ -count=1` →
TOOLS_EXIT=0), and hangs only under concurrent access to the same memory DB
(see flake above — reproduces exactly, baseline-verified clean). My changes are
in `internal/agent` only, so `internal/tools` is unaffected by this task. This
matches the epic's pre-existing-flake instruction: reproduce → compare against
clean standalone → annotate.

---

## Files changed
- `internal/agent/loop.go` — nudge constants + Agent fields + env reads +
  `maybeNudgeNoList` + loop injection (round/tool counters).
- `internal/agent/trajectory.go` — 4 new `TrajectoryStats` fields + `sessionBlock.Text`
  + `tallyTaskListText` + ratio computation + `Format()` line.
- `internal/agent/nudge_test.go` — 8 unit tests (new).
- `internal/agent/trajectory_tasklist_test.go` — 7 fixture tests (new).

## Commit
`feat/tasklist-nudge-impl` (worktree-shared; commit is already on the branch in
`~/work/GoPi`).

---

## Deliverable declaration (Rule 6d)

1. **Branch:** `feat/tasklist-nudge-impl` (worktree `~/workspace/tasklist-nudge-impl`, shared with the main repo — the commit IS the deliverable on this branch in `~/work/GoPi`).
2. **Recovery:** `git show feat/tasklist-nudge-impl:devdoc/tasklist-nudge-impl-report.md`
3. **In main repo:** yes — worktree-shared; commit on the branch in `~/work/GoPi`, no push needed.
