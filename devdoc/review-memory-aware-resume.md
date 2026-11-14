<!-- doc-class: historical -->
# R6 Review: resume / cross-run carry-forward of the memory-aware working area

Review of commit **675b507** (`feat(write-task-area)`) — the cross-run/resume story:
`taskAreaCarryForward` (loop.go 4706-4722), `injectTaskAreaConclusions` (4621-4642),
their doc comments (4619-4620, 4702-4704), and the resume/reload paths they claim
serve them. Child R6 of the memory-aware review epic
(devdoc/review-memory-aware-EPIC-PLAN.md). Branch `feat/review-memory-aware-resume`.

## Verdict

**0 P0 / 0 P1 / 3 P2 / 4 N.** "Not wired" is an *honest* design gap (the commit
message discloses it) that still delivers in-run value — but the **in-code doc
comments** claim resume/reload re-injection as an exercised capability, which is
comment↔code drift. No real cross-run *data loss* of the DB rows (append-only
table survives; history replay carries committed markers) — the gap is narrow-window
*context loss* of the single in-flight commit when a run dies between a commit and
the next wire request. Session-id scoping makes the gap unfixable by the helper as
written.

## Evidence base

- Resume entry: cmd/agent/cli_resume.go runResume (140) → `newAgent("")` (242) →
  `ag.Resume(input, history)` (257). `Resume` (loop.go 2224) → `runEngine(..., resume=true)`
  (2233) which seeds `a.Ctx.Messages = history` verbatim (2249) and appends the
  continuation (2255-2262) — **no re-framing, no taskStartSearch, NO conclusion
  re-injection anywhere on this path.**
- History source: resume.go LoadCheckpoint (49) parses the LAST `→ REQUEST` wire
  checkpoint from the raw log; requestToMessages (151+) reconstructs the full
  `messages` array — including any injected carry-forward marker blocks (they are
  ordinary `user` text messages on the wire).
- Session id: session.go NewSessionLogger (52) mints `sessionID := newUUIDv4()` (53)
  fresh per logger. newAgent creates one SessionLogger per process run. So **every
  resume is a NEW session UUID**. cli_resume.go stamps the NEW id into the old task
  row as `resumed:<session-id>` (246-255) — linkage for the ledger, NOT for the
  working area.
- SessionConclusions callers (keyword_search): loop.go 4626 (inject, current-agent
  sessionID) and 4710 (taskAreaCarryForward — dead). No restore-by-old-session-id
  anywhere.
- "reload" paths (keyword_search `reload`): only the two loop.go doc comments
  themselves (4620, 4703) + unrelated cli_web.go R4 refresh. **No reload path exists.**
- Caller inventory (R3 finding #2, re-confirmed): taskAreaCarryForward ZERO callers;
  injectTaskAreaConclusions called only at the runEngine hook 2929.
- git_blame 4618-4620: all authored by 675b507; no later commit touches these lines.

## Findings

| ID | Sev | File:line | Finding |
|----|-----|-----------|---------|
| R6-1 | P2 | loop.go 4619-4620 | **Comment↔code drift CONFIRMED.** "across separate runs the same block is served by SessionConclusions (resume/reload paths can re-inject it)" — the resume path does NOT re-inject (proven above), and no "reload" path exists anywhere in the repo (the word appears only in this comment + the carryForward doc comment + an unrelated cli_web refresh). The comment describes a capability nothing exercises. |
| R6-2 | P2 | loop.go 4702-4704 + cli_resume.go 242 | **taskAreaCarryForward is not merely unwired — it is UNWIREABLE as written.** The helper takes a `sessionID` and renders that session's rows. The resume path never knows the OLD session id: `newAgent("")` discards it, LoadCheckpoint returns (request, history, systemTexts) — no session id — and when the resume target is a raw-log *path* the old id isn't even parsed. Wiring the helper into runResume requires plumbing the old session UUID end-to-end (the `--task` row's `session_id` column or the session JSONL filename are the only surviving sources). Doc says "Public read side used by resume/reload paths" — no caller, and the natural resume caller lacks the argument. |
| R6-3 | P2 | loop.go 2927-2930 + resume.go 151 | **Real cross-run gap (R3 #2's consequence, pinned to a scenario).** A run that dies between a write_task_area commit (DB row written, dirty cleared 2928) and the NEXT round's wire request leaves that commit's marker block never-on-the-wire. Resume then: (a) replays history WITHOUT that marker, (b) runs with a NEW session UUID whose `SessionConclusions(new)` read is empty → inject no-ops → the single in-flight committed conclusion is **implied-but-missing in the resumed context** (the H4 red line). Older commits' markers (already on prior wire requests) DO replay → chain items 1..N-1 survive; only the last commit is at risk. DB row itself is safe (append-only, old-session scope — just unreachable). Trigger is the realistic max-iterations bail / kill in the commit→next-request window. |
| R6-4 | N | resume.go 151 + loop.go 2929 | **History replay is the de-facto cross-run carry, and it is sound for completed markers.** The marker blocks ride as ordinary user text messages on the wire; requestToMessages reconstructs them verbatim; runEngine(resume) seeds them (2249). A resumed chain therefore DOES see items 1-3 conclusions as context — the "resumed run loses everything" fear is unfounded for any commit whose marker reached a wire request. Cross-run carry works *by replay accident*, not by design — which is why the gap is a P2 window, not a P1 loss. |
| R6-5 | N | loop.go 2927-2932 | **No double-injection under the normal resume path.** Resume: new Agent → taskAreaDirty=false (zero-value, 1342; NewAgent never sets it), started=false. runEngine(resume) fires NO first-round commit, so the hook (2927) cannot fire → old replay markers appear exactly once; no duplicate marker blocks. Double-injection would need a commit path that sets dirty without the hook clearing it (only the dispatch 4222 + hook 2928 do today) — theoretical only. |
| R6-6 | N | session.go 52-53 + cli_tasks.go 1162 | **Session-id lifecycle across task adoption: one-process-one-UUID, stable within a run, NEW per boundary.** `task run`/serve spawn a fresh `gopi run` child per dispatch (cli_tasks.go 1162 exec.Command) — each child a fresh UUID. Resume is a separate operator action minting yet another UUID (tagged into the row). So claim→run→crash→resume is a chain of distinct working-area scopes: the session_conclusions rows accumulate under distinct UUIDs and are never merged. No two-sessions-one-id hazard (fresh v4 per logger); the hazard is one-logical-task-many-session-ids, which is exactly why the working area cannot follow a task across runs. |
| R6-7 | N | cmd/agent/cli_resume.go 199-257 | **Resume is context-continuation, not working-area restoration.** The feature's session working area (session_conclusions) and the resume mechanism are orthogonal: resume restores *wire context* (raw-log checkpoint); the working area lives in the memory DB keyed by a UUID resume never reuses. A post-resume write_task_area commit writes to the NEW session scope and the carry-forward re-states only NEW-session rows — old committed conclusions survive as replay text until the first post-resume trim drops them ([1,ci) slice, 4694). |

## Severity call (epic question 7)

**"Not wired" is an accepted design gap, honestly disclosed — NOT a P1.** The commit
message states "taskAreaCarryForward helper is ready; in-loop carry-forward is the
shipped path", the in-run value (per-commit authoritative re-statement + trim) is
fully delivered and tested, and DB rows are never lost. The problems are (a) the
**in-code comments** overclaim an exercised resume/reload re-injection that does not
exist (P2 hygiene — fix by rewording the comments to "reserved; currently the
replayed wire history is the cross-run carrier"), and (b) the narrow window where the
final in-flight commit is implied-but-missing after a resume (P2 — recoverable only
by plumbing the OLD session id into resume + calling the helper, or by re-keying the
working area to the ledger task_id instead of the session UUID). No data loss, no
crash, no session divergence → correctly below P1. Consistent with R3's P2 (#2) and
R2's P2 (R2-1 dead taskAreaActive field, same drift class).

## Not Done

- No code change (review task; findings advisory for the epic's fix pass).
- Fix options enumerated, not implemented: (1) reword comments 4619-4620/4702-4704
  to state replay-is-the-carrier; (2) plumb old session id through runResume and
  call taskAreaCarryForward when the checkpoint predates the last commit's wire
  emission; (3) re-key working-area rows by ledger task_id (larger design change —
  session UUIDs are the scope today).

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (exit 0).
- Tests: `go test ./internal/agent/ -run 'Resume|TaskArea' -count=1` → ok 1.316s.
- Commit: see git log on `feat/review-memory-aware-resume`.
- git_blame: comments 4618-4620 authored by 675b507 (feature commit); no later
  commits touch inject/carryForward (matches R3's audit).
