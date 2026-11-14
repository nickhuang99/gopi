<!-- doc-class: historical -->
# R3 Review: memory-aware message-array machinery (inject + trim + carry-forward)

Review of commit **675b507** (`feat(write-task-area)`) — message-array machinery in
`internal/agent/loop.go`: `injectTaskAreaConclusions` (4621-4642),
`trimCommittedTaskArea` (4656-4698), `taskAreaCarryForward` (4706-4722), marker
(4611), runEngine hook (2919-2931). Child R3 of the memory-aware review epic
(devdoc/review-memory-aware-EPIC-PLAN.md). Branch `feat/review-memory-aware-machinery`.

## Verdict

**0 P0 / 0 P1 / 3 P2 / 4 N.** The orphan-safety and pairing-discipline claims are
**PROVEN sound** with code citations (below). Inject-before-trim order is correct
on every realistic path. The one structural H4-adjacent gap (inject swallow →
unconditional trim) is a missing guard, not a broken mechanism, on a narrow trigger.

## Evidence base

- Hook: loop.go 2919-2931 (`if a.taskAreaDirty { clear; inject; trim }`), inside the
  `len(toolCalls) > 0` block, after the per-result append loop 2846-2875.
- Append order per round: assistant message 2812 (`AddAssistantMessageWithThinking`)
  → per-tool `AddToolResult` 2871 → whitelistContextSlim 2902 → dead-end nudge 2913 →
  taskArea hook 2927. Tool results therefore **always immediately trail their owner
  assistant within the same round, before any next-round message**.
- Run-start (fresh): epic at index 0 (2268-2287). Resume: `a.Ctx.Messages = history`
  verbatim + continuation (2247-2258). Chat/ChatOps: seed history verbatim (2980,
  chat_ops.go:198+). Compaction: `TakeOldest` + summary PREPEND (context.go:337-388,
  compact.go:549).
- Tests: `go test ./internal/agent/ -run 'TaskArea|TrimCommitted|InjectTaskArea'` →
  ok (0.737s); `go build ./...` → OK. Tests exercised: DropAnalysisKeepsEpicAndCommit,
  KeepsLaterLiveWork (with orphan head-check), CarriesRecordedFacts.

## Q1. Inject message-position semantics / ordering with other injected user messages

The carry-forward block is appended at the END of the round (after commit tool results
2846-2875, after whitelist 2902 and the dead-end nudge 2911-2917). At round N+1 it is
therefore the **most recent user message** — positionally authoritative, superseding
the dead-end nudge that preceded it in the same round. The block is not displaced by
any later-append ordering concern: all three append-only operations (whitelist is
removal-only, dead-end nudge and carry-forward are appends) land after this round's
results, and the carry-forward is last. **Guaranteed seen next round.** No ordering
hazard. → N (comment at 4618-4619 "the next LLM round sees the block" — accurate).

## Q2. trimCommittedTaskArea — orphan safety PROVEN

**Head-safety (no dangling tool_result at the new head).** The slice is
`Messages[:1] + Messages[ci:]` (4694), ci being the newest assistant carrying a
`write_task_area` call. Tool results always trail their owner in the same round
(append order 2812→2871, one round's results complete before the next round's
assistant — the results loop is inside the same loop iteration as its assistant
append, with no interleaving append between them). Therefore for the assistant at
ci-1 (just inside the dropped window), any results it owns sit at ci-1+k < ci — i.e.
**inside the dropped region, removed together with their owner**. The kept tail head
is Messages[ci], an assistant by construction of the walk (it only sets ci on
Role=="assistant"). So no tool message can ever surface at the new head. The
"by construction" comment (4685-4691) is **correct** — verified against the actual
append order.

**Multi-tool round.** A round may carry several tool calls in ONE assistant message
(2812 with toolCalls slice; results appended 2846-2875 in tool-call order). The walk
finds that single assistant; `[1,ci)` drops complete earlier groups only. No earlier
round's results can be "in flight" past ci: every assistant in the array already has
(or never had) its results because results are appended in the same iteration as the
owning assistant. **PROVEN**.

**Two write_task_area commits in one round.** `taskAreaDirty` is a bool latch set at
4222 after each successful dispatch; two commits in one round both set it true, the
hook fires once (2927), and inject re-reads the whole DB-backed set (4626) → **both
commits covered, no lost signal**. The newest-assistant walk finds the single commit
message (which holds both write_task_area calls). PROVEN (R2 verified latch single
write/clear sites; this review confirms the read-all design covers multiplicity).

**Failed commit.** validateToolArgs returns "Missing required parameter..." at 4392ff
before dispatch; WriteTaskArea error returns at 4217 before the latches at 4221-22.
The hook keys off taskAreaDirty which is **only set after err==nil** → **cannot fire on
failure**. PROVEN.

## Q3. Index-0 "epic" assumption + resume + compaction interplay

- **Fresh run**: index 0 is the epic (2268-2287) — claim holds.
- **Resume**: history is seeded verbatim from the interrupted session's last wire
  request (2247-2258, resume.go LoadCheckpoint/requestToMessages). If the interrupted
  session was pre-compaction, index 0 is still the epic and survives. If compaction
  had run, index 0 is the compaction **summary** — but then the epic text was already
  gone from the wire too, so it cannot be "somewhere in the middle" for trim to drop.
  No path puts a live brief mid-array. → trim's `[:1]` slice is SAFE on every entry
  path. 
- **Compaction**: `TakeOldest` treats index 0 as the compaction boundary by design
  (context.go:330-333, 344) and `compactContext` PREPENDS the memory summary
  (compact.go:549). So **"index 0 = epic briefing, never trimmed" is a false claim
  after a compaction** — index 0 is then the summary, and the real epic (if it had
  been at 0) was compacted away. Not data loss (the compaction prompt mandates
  preserving "the overall goal", compact.go:290-301), but the comment at 4645/4685
  overclaims. → **N (comment↔code drift; mechanism safe)**.
- **Chat/ChatOps**: seed history verbatim with no epic framing; write_task_area is
  reachable there (see Q6).

## Q4. Interplay with OTHER trimmers (context_trim / RemoveToolPair / whitelist / TrimToFit)

Same-round order (all inside the tool block, before the hook): whitelistContextSlim
2902 (removes only `memory_search` pairs via RemoveToolPair — never touches a
write_task_area commit message, whose ToolCalls are named write_task_area) → dead-end
nudge 2913 (append-only) → hook 2927. **No prior same-round trim can leave the
commit assistant or its results half-removed.** RemoveToolPair removes owner+result as
a unit and refuses incomplete pairs (context.go:201-203) — never partial.

Cross-round: `trimContext` (2545) runs at the top of each round, before the LLM
request. It folds old tool results (summarizeOldToolResults, removal-free), compacts
(TakeOldest + summary prepend), or TrimToFit-drops oldest. Any of these can drop a
PREVIOUS carry-forward block sitting in `[1,...)` — fine (superseded by the newest
block). The **newest carry-forward block is a user message and TakeOldest anchors the
last user message** (context.go:346-348, 359-366), so it survives until superseded.
TrimToFit's orphan discipline (never leaves a tool at head, context.go:426-430 + 370-
374) keeps the array well-formed for the ci-walk. **No mechanism can break the
walk's invariant.** PROVEN.

## Q5. taskAreaCarryForward — comment↔code drift CONFIRMED

`keyword_search taskAreaCarryForward` → **zero callers**; only the definition
(loop.go:4867) plus doc references. Same for `injectTaskAreaConclusions` — called only
at 2929 (the in-process hook). The comment at 4619-4620 — "across separate runs the
same block is served by SessionConclusions (**resume/reload paths can re-inject it**)"
— describes a capability nothing exercises. **Gap**: a session that dies between a
commit (DB row written, dirty cleared at 2928) and the next round's request (the
carry-forward block was appended in-process but never reached the wire) resumes with
context whose recorded conclusions are absent — and if the interrupted analysis
references a committed conclusion, that is implied-but-missing **across the process
boundary** (fabrication risk H4 warns of). `taskAreaCarryForward` exists precisely to
close this and is unwired. → **P2** (real cross-run gap; consequence severity depends
on how often runs die between a commit and the next request — the R2-noted
max-iterations bail is the realistic exit). **Fix ownership: R6** (resume/cross-run
carry review) — R3 confirms the drift and the caller gap with code evidence.

## Q6. ChatOps mode: dirty latch set but no hook consumes it

ChatOps (chat_ops.go:158-357) exposes the FULL tool set (`claudeToolDefs` at 242),
shares `executeToolDefault` → the write_task_area dispatch (4200-4223) sets
`taskAreaDirty=true`, but the ChatOps loop has **no taskArea hook** (checked 198-319:
results appended at 317, no inject/trim anywhere). Consequences:
(a) a write_task_area commit in chat mode is DB-recorded but never re-stated in-chat —
the feature is silently inert there (chat has no task scaffolding, so this is arguably
consistent);
(b) if the SAME Agent object were later reused for runEngine, the stale dirty latch
would fire inject+trim on stale state in round 1. R2 verified production never reuses
an Agent across modes (web s.agent is chat-only; `gopi task run` spawns a fresh child,
cli_tasks.go `exec.Command(gopiExecutable())`; `gopi run` is one-shot per process,
main.go:250). So (b) is theoretical, but the latch-set-without-consume is a latent
trap. → **N** (out-of-scope mode; note for anyone adding agent reuse).

## Q7. Growth: inject re-statement is O(n) per round, bounded by the trim

Each commit round injects ONE block re-stating ALL recorded conclusions (O(k) for k
conclusions). Measured (300-char conclusions, ~10-char task_id):
- n=5 → ~1.6K chars/block (~400 tokens); n=20 → ~6.3K chars (~1.6K tokens).
- The trim removes the PREVIOUS block on the next commit (it sits in [1,ci) and is
  dropped), so the **live tail holds only the newest block — steady-state O(n), not
  O(n²)**. The trim keeps pace; no unbounded tail. Verified by the slice geometry.
- Cumulative wire cost over a 20-commit chain ≈ 66K chars ≈ 16K tokens (~8% of a
  200K window) — the unavoidable "re-state all" tax, ~11× restatement per conclusion.

So the O(n²) concern in the epic is real **only as total wire bytes**, never as a
resident tail. Acceptable at n=20 with typical conclusions; if a chain grew to
hundreds of commits with long conclusions, the per-round block would dominate (a
future optimization: incremental delta or cap on re-stated set). → **N** (bounded;
hygiene note).

## H3/H4: never implied-but-missing — one structural gap (no P1)

**Inject-before-trim order verified**: 2929 inject, then 2930 trim — the H4 red line
is structurally honored; the block is appended before any region is dropped, and the
trim's kept tail [ci:] includes the freshly injected block. The dirty latch is cleared
at 2928 only on success paths; failure can't fire the hook (Q2). **The one
H4-adjacent gap**: inject swallows errors and empty reads silently (4626-4628
`if err != nil || len(concs) == 0 { return }`) and returns no status; trim at 2930
runs **unconditionally** whether inject succeeded. If a commit's DB write succeeds
(dirty set) and the same-round read then fails (openTasksDB error → SessionConclusions
returns nil,nil per R1 finding #3), the committed region is **trimmed without the
conclusion ever being stated** — recorded in DB but absent from context for the rest
of the session until a later commit re-states all. This is precisely the guard R1
explicitly flagged for R3 ("verify the trim hook never trims unless the inject read
SUCCEEDED"). **The guard is absent.** Trigger is narrow (same sqlite file written then
read microseconds apart failing) → P2, not P1, but it is the one place where the
"never implied-but-missing" doc'd invariant lacks a mechanical guarantee. **Fix**: make
injectTaskAreaConclusions return a success bool and skip the trim (or log loudly)
when the read failed — a 3-line change.

## Findings table

| # | Sev | File:line | Finding |
|---|-----|-----------|---------|
| 1 | P2 | loop.go 2927-2930 + 4626-4628 | trim runs unconditionally after inject's silent swallow — committed conclusion can be DB-recorded but trimmed-without-stated (R1-flagged H4 guard absent) |
| 2 | P2 | loop.go 4619-4620, 4706-4722 | taskAreaCarryForward has ZERO callers; resume/reload re-inject is comment drift — cross-run conclusion loss when a run dies between commit and next request (fix in R6) |
| 3 | P2 | loop.go 4645, 4685 | "index 0 = epic briefing, never trimmed" is false after compaction (TakeOldest boundary + summary PREPEND compact.go:549) — head is then a summary; mechanism still safe, comment overclaims |
| 4 | N | loop.go 4656-4698 | Orphan-safety + pairing discipline PROVEN correct against the actual append order (2812→2871); ci walk and slice geometry sound on all entry paths |
| 5 | N | loop.go 4645, 2919-2931 | Re-statement is O(n) live-tail (previous block dropped by next trim) — bounded, not O(n²) resident; cumulative 20-commit chain ≈16K tokens |
| 6 | N | chat_ops.go 242, 306-318 | ChatOps exposes write_task_area + sets dirty latch but has NO taskArea hook — feature inert in chat mode; stale-latch trap if an Agent is ever reused across modes |
| 7 | N | loop.go 2902, 2913, 2917; 2545 | No same-round trimmer touches the commit pair (whitelist is memory_search-only, nudge append-only, trimContext runs pre-request and anchors the last user msg) |

## Not Done

- R6-owned cross-run resume carry fix (finding 2) — identified, not implemented (this
  review is read-only; R6 owns the area).
- No test asserts the finding-1 guard (inject-failure → trim must not run). R7
  (tests adequacy) may add it; a minimal repro would stub SessionConclusions to fail
  after a real write.
- Live-wire byte measurement of a real 20-commit chain (numbers here are estimates at
  300 chars/conclusion).

## Verification

- Build: `go build ./...` → OK (exit 0).
- Tests: `go test ./internal/agent/ -run 'TaskArea|TrimCommitted|InjectTaskArea'` → ok
  0.737s (3/3 PASS: DropAnalysisKeepsEpicAndCommit, KeepsLaterLiveWork incl. orphan
  head-check, CarriesRecordedFacts). Earlier full run also passed 3/3 at 1.456s.
- Commit: `315a4ea` on `feat/review-memory-aware-machinery` (worktree-shared; no merge, no push needed).
- git_blame: all reviewed lines authored by 675b507 (single-author feature commit); no
  later commits touch inject/trim/carryForward.
