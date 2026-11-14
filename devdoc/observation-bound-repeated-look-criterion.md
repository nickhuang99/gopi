<!-- doc-class: historical -->
# Task Report: detector ③ judges REPEAT, not tool class — the stuck-observation false positive

Task `01a0faac-15dc-7b60-938c-9df18843acc8` (branch `task/01a0faac15dc7b60938c9df18843acc8`),
fixing a defect whose field evidence is task `01a0f8c9`.

## The defect (one paragraph)

`observationState` — the third agent-loop stuck detector, added by
`feat/look-without-fixing-bound` and documented in `devdoc/look-without-fixing-bound.md`
— billed a round as a "look" whenever **every** `tool_use` in it was an *observer*
(`read`/`bash`/`code_search`/`keyword_search`/`web_*`/`git_blame`/
`vision_analyze`/`screen_capture`), and terminated the run after
`maxObservationRounds = 40` such rounds in a row. The criterion never consulted
the round's **content**: reading 40 *different* files was 40 "identical" looks.
"Go read the code / gather evidence" is therefore a TRUNCATED run, and field task
`01a0f8c9` died that way **four times with zero output** (two of the four runs are
in the ledger with reason `TRUNCATED run (stuck-observation)`, §4).

## Completed

1. **The criterion is now REPEAT, not class** (`internal/agent/loop.go`,
   `observationState` + two new methods). A round that only looked is a
   *candidate*; it accumulates **only when both halves repeat**:
   - the **call**: `toolFingerprint(toolCalls, text)` — the ONE existing comparator
     (Rule 14, no second implementation) — tool name(s) + normalized parameter hash;
   - the **result**: one sha256 over the round's executed results, the same
     `observeResults` shape detector ② already uses for its polling rounds.
   Either half differing is a fresh observation and resets the count: a different
   file/keyword/command (real investigation) or a result that MOVED (a tailed log,
   a polled endpoint) is work, never evidence of a loop. **Detector ②'s bug055
   changing-result exemption is inherited, not re-derived** — the sibling's
   semantics are untouched (§6).
2. **Two feed points, mirroring detector ②**: `observe(toolCalls, text, hasToolUse)`
   at the existing single pre-switch feed point (class decision + candidate
   recording), `observeResults(results)` next to `toolStreak.observeResults` after
   execution (the repeat decision + the trip). A candidate whose results never
   arrive is never billed — the safe direction.
3. **Threshold unchanged** (`maxObservationRounds = 40`), **no new knob**, and
   `GOPI_STUCK_OBSERVE_ROUNDS`'s three arms (`unset`/`0`/positive/invalid,
   one reader) are untouched: the fix is in the criterion, not in the number.
4. **`observationToolClass` (internal/agent/toolclass.go)** keeps its job — the
   class test still decides ACT (mutator / polling `job` / unknown name) and still
   gates candidacy — but its doc now states that the class is **not** the judged
   quantity. Actor names live in that table only, referenced from
   `internal/toolnames` (Rules 1b/13/14).
5. **Tests** (`internal/agent/loop_observation_test.go`, 3 added, 2 rewritten,
   every old assertion annotated NEW-vs-OLD in the file's header block):
   `TestAgent_ObservationLoop_RedProofA_DistinctLooksSurvive`,
   `TestAgent_ObservationLoop_RedProofB_SameCallSameResultTrips`,
   `TestAgent_ObservationLoop_GreenProof_ChangingResultSurvives`;
   `_BoundTripsAtDefaultAndNotifies` superseded by RedProofB (same property, on
   the repeat stream); `_ThresholdOverrideTrips` moved onto the repeat stream.
6. **Report + evidence** in `devdoc/` (this file) and the session directory
   `/home/user/.gopi/sessions/01a0faac-15dc-7b60-938c-9df18843acc8/`.

### Semantics that did NOT change (each still asserted)

| arm | behaviour | why it stays |
|---|---|---|
| mutator round | streak 0 + re-arm 2N look-round exemption | real work must not be billed; the verification burst after an edit is legitimate |
| `job` round | ACT | detector ② owns polling (bug055 is load-bearing); the bound must not double-bill a wait |
| no-tool round | streak 0 **and** drops the repeat reference | a round spent talking is not a round spent looking (measured decision, kept) |
| unknown tool name | ACT | the bound never widens itself over a tool the tree grows later |
| `GOPI_STUCK_OBSERVE_ROUNDS` | unset→40, `0`→disabled, positive→N, invalid→default+stderr | Rule 2 single reader |

### One consequence, stated rather than hidden: a one-round OFFSET

`streak` counts **repeats**, and the first observation of a run has nothing to
compare against — so the Nth repeat arrives on the (N+1)th identical round and the
bound fires **one round later** than the class criterion did (measured: exactly
round 41 at the default 40; round 4 at threshold 3). That direction is deliberate
(constraint: a false negative beats a false positive) and is pinned by
`RedProofB`'s round assertion. The `looks` counter still counts candidate rounds,
so the alarm's `summary()` keeps its observability (`streak= looks= threshold=
repeated=`).

## Not Done (honest)

- **No live model was run.** Every end-to-end proof drives a mock Anthropic
  endpoint through the real binary, loop, feed points, detectors and notify
  pipeline (§5) — the same instrument the detector was originally validated with.
  A real-model run of a *repeating* observation loop was not staged.
- **The corpus replay is thin and says so** (§3). `01a0f8c9` ran four times; only
  one session's transcript survives on disk (133 rounds, longest all-observer run
  34 at master's criterion — it did **not** trip the bound), and the other three
  runs left a ledger row but no `session_id`/transcript. The strongest available
  evidence that the *criterion* was the killer is the 09:07 run: **`iter=40`** with
  a `TRUNCATED run (stuck-observation)` reason — 40 is the threshold, and at 40
  the run covered only 4.6M tokens, so it was in a lean read-heavy stretch, not a
  long investigation. That is an inference from the ledger, and it is labelled as
  one.
- **The old criterion's own corpus measurement is not re-run here.** The original
  task measured p99=6 / p99.9=24 / max=32 all-observer runs over 2317 sessions
  under a *reset* definition and picked 40 on that basis. Under the new criterion
  the same raw round sequences would produce a strictly smaller count (only exact
  call+result repeats), so the shipped 40 is at least as conservative — but that
  re-measurement was not performed in this session.
- **`gopi doccheck` / the DOC-INDEX row for this file**: written through the
  `write` tool, which classifies as it lands; `gopi doccheck` was run to confirm
  (§Verification) and must be re-run if the file is later edited by hand.

## Verification

- **Build**: `go build ./...` → exit 0 (also `CGO_ENABLED=0 go build -o bin/gopi
  ./cmd/agent/` → 29767903 bytes at 11:57).
- **Full package**: `go test ./internal/agent/ -count=1` → the ONLY failing test is
  `TestDocAnchorsResolveInNormativeDocs`, which **fails identically at master**
  (`go test ./internal/agent/ -count=1 -run TestDocAnchorsResolveInNormativeDocs`
  in the detached probe worktree `/home/user/workspace/gopi-obsbaseline-01a0faac`
  at `aa69755e` → `--- FAIL`, 6.06s) — pre-existing baseline drift, not this diff.
- **Tests**: `go test ./internal/agent/ -run
  'TestObservation|TestAgent_Observation|TestToolClasses|TestAgent_Stuck|TestAgent_Wallclock'
  -count=1` → `ok github.com/nick/gopi/internal/agent 6.131s` (after the fix).
  Red side, measured at master in a detached probe worktree
  (`/home/user/workspace/gopi-obsbaseline-01a0faac`, `aa69755e`):
  `-run TestBaselineOLD` → PASS for the *defect* probe, logging
  `BASELINE (master, old criterion): run terminated after 40 rounds with: agent
  loop stuck: 40 consecutive rounds that only observed (no mutating tool call) —
  terminated (see GOPI_TASK_NOTIFY loop-stuck-observation)` on a stream where every
  round read a **different** file and got a **different** answer. That is red proof
  A measured against the old criterion; the same stream survives on the branch
  (`RedProofA`: 60 rounds, `max iterations`).
- **Ran** (Rule 31, the real CLI against a mock endpoint, three times):
  ```
  gopi --home <evid>/e2e/home1 --settings <evid>/e2e/settings.json --timeout 40 \
       --base-url http://127.0.0.1:874<port> --api-key test-key --model mock-model "<prompt>"
  with GOPI_STUCK_OBSERVE_ROUNDS=4 GOPI_TASK_NOTIFY_EVENTS=loop-stuck-observation \
       GOPI_TASK_NOTIFY_CMD='cat >> <evid>/e2e/notifyN.jsonl' GOPI_TASK_NOTIFY_ASYNC=0
  ```
  1. **repeated + frozen result** (`--mode repeated --port 8741`, tool result
     byte-identical): `⏱ 2.0s | 5 iterations | FAIL`, and the capture received
     `{"event":"loop-stuck-observation", ..., "summary":"agent loop stuck
     (observation without act): 4 consecutive repeated observation rounds — same
     tool call and byte-identical result (streak=4 looks=5 threshold=4
     repeated=bash) — run terminated"}`.
  2. **distinct look every round** (`--mode distinct --port 8742`, `--timeout 40`):
     **204 rounds / 203 tool calls** with **no alarm and no capture file**.
  3. **repeated + moving result** (`--mode repeated --moveful --port 8745`,
     `--timeout 20`): **74 rounds / 73 tool calls** with **no alarm and no capture
     file** (the tool executed across the runs: the state moved all three times,
     `/tmp/gopi-obs-proof/state.txt` rewriting each round).
- **Result**: the alarm fires on the repeat shape and on nothing else; the ledger
  row of run 1 closed `failed` (not `completed`) with the observation reason.
- **Commit**: see §Deliverable (the hash is stamped in the closing message).

## Evidence (self-filed)

- `go build ./...` → `exit 0` (observed).
- `go test ./internal/agent/ -run 'TestObservation|TestAgent_Observation|TestToolClasses|TestAgent_Stuck|TestAgent_Wallclock' -count=1` → `ok … 6.131s` (observed).
- Baseline probe `-run TestBaselineOLD` at `aa69755e` → `--- PASS` + the logged
  `terminated after 40 rounds …` line (observed).
- Ledger census (measured, §4): `sqlite3 ~/.gopi/memory.db "SELECT COUNT(*) …
  WHERE detail LIKE '%TRUNCATED run (stuck-observation)%'"` → `events=4`,
  `distinct_tasks=3`; `event='failed'` subset → `3` (2 tasks).
- `git status --porcelain` in the worktree → empty (observed at close-out).

## 1. What the new criterion is, in code terms

```go
// observe: the pre-switch feed (same single point as detectors ①/②)
observeOnly, act := observationToolClass(toolNames(toolCalls))
if act { s.reset(); s.exempt = 2 * s.threshold; return }   // unchanged ACT arms
if !observeOnly { return }
name, params, pref := toolFingerprint(toolCalls, text)      // the ONE comparator
s.pending, s.pendName, s.pendParams, s.pendPref = true, name, params, pref
s.looks++                                                    // streak NOT touched here

// observeResults: after execution — the RESULT half
sameCall   := s.hasLast && name == s.lastName && params == s.lastParams
sameResult := s.hasLast && cur == s.lastResult
if s.exempt > 0 { s.exempt--; s.streak = 0 }                 // post-work burst
else if sameCall && sameResult { s.streak++ } else { s.streak = 0 }
```

**Why the text prefix is not part of `sameCall`** (a decision worth recording):
`toolFingerprint` returns three parts, and detector ② compares **all three**. If
detector ③ required the text prefix too, its trip condition would be a strict
**subset** of detector ②'s at a lower threshold (10 < 40) — detector ③ could only
ever fire after detector ② had already fired, i.e. dead code. Excluding the prose
gives detector ③ the shape ② structurally cannot see: the same call with the same
frozen result while the model narrates differently each round. That is also the
epic's own wording ("same command + same arguments + result no longer changing").
The epic's "reuse `toolFingerprint`" constraint is honoured — name and parameters
come from it, one comparator, no second implementation.

## 2. Unit-level semantics (the arms table above, as measured)

`TestObservationBound_StateUnit` (threshold 4) asserts, in order: eight **different**
reads never trip; the 4th *repeat* trips on the 5th identical round; a mutator
resets and the 2N burst is spent in look rounds (12 looks → trip at the 12th); 12
identical `job` polls never trip; a no-tool round drops both the streak and the
repeat reference; an unknown name never trips; a mixed round (mutator among
observers) is an ACT.

## 3. Replay of the recorded session (offline, reproducible)

Script (evidence, not in the tree):
`<evid>/replay_stuck_observation.py` — mirrors the Go criterion byte for byte
(same class tables, same `json.dumps(sort_keys=True)` parameter normalization, same
result hash, same "no-tool resets both" rule) and reports, per session, the round at
which each criterion would fire at threshold 40.

```
$ python3 replay_stuck_observation.py ~/.gopi/sessions/01a0f8c9-2022-745f-ac6e-e44b1b730383/342beb8d-….jsonl
rounds parsed: 133 (all-observer rounds: 114), threshold=40
OLD criterion (tool CLASS: every all-observer round bills)
  -> SURVIVED: the bound never fired (longest streak 34 rounds)
NEW criterion (REPEAT: same call AND same result)
  -> SURVIVED: the bound never fired (longest streak 0 rounds)
```

Two honest readings of that output: (a) the surviving transcript is **not** one of
the runs the bound killed, and the old criterion would not have killed it either —
its longest all-observer run is 34, one act short of 40; (b) under the new criterion
the same 133 rounds produce **zero** repeats, i.e. every single one of its 114
observation rounds was a distinct call or a moved result. So the fix provably
preserves this session's work, and the session cannot exhibit the fatal 09:07 run
(whose transcript is gone).

## 4. The historical damage surface (measured)

```
$ sqlite3 ~/.gopi/memory.db "SELECT COUNT(*) events, COUNT(DISTINCT task_id) tasks
    FROM task_events WHERE detail LIKE '%TRUNCATED run (stuck-observation)%';"
events  distinct_tasks
------  --------------
4       3

$ sqlite3 ~/.gopi/memory.db "SELECT DISTINCT task_id FROM task_events
    WHERE detail LIKE '%TRUNCATED run (stuck-observation)%';"
01a0cda1-0032-707a-8c37-be2c651ffdbe   (2026-09-23 17:49:53, failed)
01a0df19-4327-73fb-9b74-e13ce427fb54   (2026-09-27 04:09:31, run-end)
01a0f8c9-2022-745f-ac6e-e44b1b730383   (2026-10-02 06:04:29 failed iter=142; 09:07:59 failed iter=40)

$ sqlite3 ~/.gopi/memory.db "SELECT CASE WHEN detail LIKE '%stuck-observation%' THEN 'stuck-observation'
    WHEN detail LIKE '%stuck-toolful%' THEN 'stuck-toolful' ELSE 'stuck-loop' END kind, COUNT(*)
    FROM task_events WHERE detail LIKE '%TRUNCATED run%' GROUP BY 1;"
stuck-observation  4
```

**Four rows / three tasks**, and this detector is the **only** source of
`TRUNCATED run` rows in the whole ledger — the two sibling detectors have killed
nothing. The 09:07:59 run is the smoking gun: `iter=40`, exactly the threshold.

## 5. Rule 31 — the changed surface, RUN

Three real CLI runs against a mock Anthropic-streaming endpoint (mock committed as
evidence at `<evid>/e2e/mock_llm.py`, never into the tree), with the real binary,
loop, feed points, detectors and notify pipeline. `GOPI_STUCK_OBSERVE_ROUNDS=4` so a
4-round proof replaces a 40-round one; the threshold arm is what §2's unit test
pins, and RedProofB pins the default.

**Run 1 — repeated call + frozen result (must trip):**

```
$ GOPI_STUCK_OBSERVE_ROUNDS=4 GOPI_TASK_NOTIFY_EVENTS=loop-stuck-observation \
  GOPI_TASK_NOTIFY_CMD='cat >> <evid>/e2e/notify1.jsonl' GOPI_TASK_NOTIFY_ASYNC=0 \
  ./bin/gopi --home <evid>/e2e/home1 --settings <evid>/e2e/settings.json \
    --base-url http://127.0.0.1:8741 --api-key test-key --model mock-model \
    "watch /tmp/gopi-obs-proof/state.txt until it settles"

⏱ 2.0s | 5 iterations | FAIL | prompt=50(hit=0 miss=50) completion=25
[gopi] task ledger: failed 01a0fac3-1dfc-7c8a-8988-67e8652b3716 (auto)

$ cat <evid>/e2e/notify1.jsonl
{"event":"loop-stuck-observation", ..., "summary":"agent loop stuck (observation
 without act): 4 consecutive repeated observation rounds — same tool call and
 byte-identical result (streak=4 looks=5 threshold=4 repeated=bash) — run
 terminated", "timestamp":"2026-10-02T11:58:09+08:00"}
```

**Run 2 — a different file every round (must survive):** `--mode distinct
--port 8742`, same invocation shape, capture file `notify2.jsonl`. Measured: the
run was wallclock-capped (`--timeout 40`) after **204 rounds / 203 tool calls**
(its session `645b8883-…` in `<evid>/e2e/home1/.gopi/sessions/`), i.e. ~50× the
threshold in force — and no capture file was created at all, which is exactly the
negative the notify channel produces (`GOPI_TASK_NOTIFY_CMD` only runs when an
alarm fires). Old criterion, same stream: 40 rounds, terminated (§Verification).

**Run 3 — same call, moving result (must survive):** `--mode repeated --moveful
--port 8745`, capture `notify3b.jsonl`. The mock's command rewrites its target
every round (`date +%s%N > /tmp/gopi-obs-proof/state.txt; cat …`), so the result
moves; measured **74 rounds / 73 tool calls** with `--timeout 20` and **no capture
file** — the bug055 exemption, inherited, end to end.

> **Run 3 was run twice, and the first attempt is worth recording.** The mock's
> first version embedded the shell command in a Python `%`-format string with
> `%%`, so the tool executed the literal `date +%s%N` and returned a *constant* —
> and detector ③ **correctly tripped it in 5 rounds** (`notify3.jsonl`, 12:02),
> because a call whose result never changes IS the repeat shape regardless of what
> the author intended. The mock was fixed, the escape re-checked outside the
> product (`date +%s%N > …; cat …` → `1790913764221088024`), and the leg re-run.
> That accident is the criterion behaving as specified, not a false positive.

## 6. Sibling detectors: untouched, and how that is proven

Detector ②'s `toolStreakState` (including `observe`/`observeResults`/`pollChain`/
`lastPollResult` and the bug055 exemption) was **not edited** — the diff touches
only `observationState`, its two feed call sites, one doc comment in
`toolclass.go`, and the test file. `TestAgent_ObservationLoop_LegitimateJobPollSurvives`
(identical `job` polls at threshold **1**, changing result) and the
`loop_stuck_test.go` polling tests pass unchanged; `TestAgent_Stuck*` and
`TestAgent_Wallclock*` are in the same green run (§Verification).

## 7. Files

- `internal/agent/loop.go` — `observationState` (criterion, `observe`,
  `observeResults`, `observationResultHash`, `reset`, `summary`), the two feed
  call sites, the OFFSET doc note.
- `internal/agent/toolclass.go` — `observationToolClass` doc: the class is the
  entry condition, not the judged quantity.
- `internal/agent/loop_observation_test.go` — header semantics map (NEW vs OLD per
  test), the three new proofs, the two rewritten ones, the `obsLook`/`obsAct`/
  `obsNoTool`/`obsCall` helpers.
- `devdoc/observation-bound-repeated-look-criterion.md` — this report.
