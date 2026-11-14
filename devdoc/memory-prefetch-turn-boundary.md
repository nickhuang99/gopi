<!-- doc-class: historical -->
# Task Report: memory prefetch at the TURN BOUNDARY — the four input faces the typing sampler cannot see

**Task** `01a10edf-5df5-7f3e-af0d-b771d0b99c6f` (Epic B; parent of a completed A:
`01a10edf`, whose switch is `internal/config/memory_prefetch.go`)
**Branch** `task/01a10edf5df57f3eaf0db771d0b99c6f` (worktree
`/home/user/workspace/gopi-01a10edf5df57f3eaf0db771d0b99c6f`)
**Commit** `655ec406`

## Completed

**1. The blind spot, restated as a measurement rather than a hypothesis.** The
typing path (`cmd/agent/chat_prefetch.go` + `internal/memoryprefetch.Session`)
samples `Editor.Value().Text()`, so it is structurally blind to the four input
classes that have no in-progress line: a `gopi notify --session … --message …`
delivery, a whole-block paste, a `/queue`d prompt, and a line the model typed for
itself. **`memoryprefetch.Step` had no caller at all** — `session.go`'s own header
says it was written for this one ("Step decides whether a TURN is worth a
retrieval … a turn boundary, driven by a composer that has the whole buffer").
Epic B is that caller, not a second mechanism.

**2. `internal/memoryprefetch/turn.go` (new)** — the missing INPUT, in the package
that already owns the decisions:

- `TurnInput{Parts, Payloads}` / `TurnText` — the ONE place a turn's parts are
  joined, so the text the gate judges, the extractor reads and the record can name
  is one string. Parts join with a NEWLINE (pinned by a test: `alpha`+`beta` must
  never become the candidate `alphabeta`).
- `MaxTurnPayloadRunes` (default 4000, config.go) bounds ONE referenced payload,
  clipped on a RUNE boundary, and the clip is recorded as the named degradation
  `turn-payload-clipped` — a whole-block paste is unbounded by construction, so a
  clipped key must say so rather than look short.
- `TurnConfig` / `DefaultTurnThrottleTypes = 1` — the turn's throttle, applied in
  ONE place. `DefaultThrottleTypes = 6` guards a KEYSTROKE stream; a turn boundary
  is an event the operator sets the rate of, and the delta already gates it. With
  a throttle of 6, a turn whose lookup FAILED (a give-up, a locked ledger) could
  not be retried for five more turns. The delta is the whole gate at a boundary.

**3. `cmd/agent/chat_prefetch_turn.go` (new)** — the caller, which owns the two
things the pure package may not: the file read and the adoption.

- **The terms come from the ARM, never from the framed prose.** The boundary's
  framing is CONSTANT text on every turn of its shape ("[system] An external
  process sent this session a message: …"), so extracting from the sealed message
  would hand the gate the same candidates every turn — a constant term satisfies
  the gate by itself and pollutes the delta machine with a keyword that never
  changes. `chatPrefetchTurnTerms` reads `selfDriven` / the notices'
  `External.Message` (the delivered words verbatim) / the job's `Command`.
- **The payload behind a reference is read for the key** (`prefetchTurnPayload`,
  byte-bounded, `ToValidUTF8` so a cut inside a multi-byte rune is a shorter
  string and not a corrupt one), because the boundary deliberately does not quote
  a spilled payload.
- **The seal never blocks on the retrieval.** The lookup runs on its own goroutine
  and the seal waits at most `prefetchTurnWaitMs` (250ms — far below the lookup's
  own 1500ms discipline). A miss leaves the turn without a section and the
  lookup's OWN record line lands when it finishes, with its real outcome.
- **ONE section per turn**: a turn that already carries the turn-boundary section
  drops the typing one, recorded as `superseded` rather than silently discarded.

**4. `lineedit.StripPasteRefs` (new) — the pointer was spending the keyword
budget, and this was MEASURED, not reasoned.** First live paste run recorded

```
"query":"paste turn 5411 home nick workspace gopi sandbox"
```

— the operator's two words plus the reference's PATH COMPONENTS and its SIZE. A
reference renders nine-ish candidates; `Config.MaxKeywords` is EIGHT, so the
budget was consumed by the pointer while the payload (read separately, from the
file the reference names) was cut off before its words entered the key. With the
pointer stripped the same turn records

```
"query":"paste turn spilled payload behind reference boundary reads"
```

— the payload's own terms, which is what the retrieval is for. It strips only when
EVERY reference read succeeded: a reference whose file could not be opened is the
turn's only description of that payload, and deleting it would turn a recorded
failure into a silent loss of content (Rule 15).

**5. A latent off-by-N in the reference parser, found by that work.**
`pasteRefParse` returned `base + end + len(refClose)`, but `end` was already
indexed relative to a string with the closing bracket, so the returned `next`
pointed one byte EARLIER than the end of the reference — every reader built on it
(a second reference, a strip loop) re-read the trailing `]`. It is now `base + end`
with `base` including the separator gap the parser tolerates. `PasteRefPaths`,
`HasPasteRef`, `SourcesIn`, `SourcesAreKnown` are all built on it, so this changes
what they read for a line carrying a reference; the package's own tests
(`-run PasteRef`) pass.

## Not Done

- **`/queue`d prompts are covered by POSITION, not by a live run.** The `/queue`
  half of the parent row's unified input path is not on this branch's tree — the
  queue writer still parks a prompt as plain text (`cmd/agent/chat_queue.go`),
  `chatSealSource` does not consult it, and `inputsource.Queue` has no writer on
  master. A `/queue`d prompt therefore produces an ordinary `human` line at the
  boundary and IS retrieved (the boundary covers it), but no run in this report
  exercised the queue specifically, and the arm cannot name it as `queue`. This is
  a real, named gap, dispatchable on its own.
- **No dedicated arm for "another agent's output".** Such a turn arrives either as
  a notice turn (covered, `source=notify`) or as a `human`/`model`-typed line;
  there is no separate face to name, so no code was added for a distinction the
  vocabulary does not carry.
- **The typing path's own record lines gained no `source` field** — intentionally
  (`omitempty`): the typing path has no turn face to name, and leaving it absent
  keeps its record bytes unchanged by this addition (Rule 15a).

## Verification

- **Build:** `CGO_ENABLED=0 go build ./...` → **exit 0**; `CGO_ENABLED=0 go build -o
  bin/gopi ./cmd/agent/` → OK; `go vet ./cmd/agent/ ./internal/memoryprefetch/` → OK.
- **Tests:** `make test-budget PKGS='./cmd/agent ./internal/memoryprefetch
  ./internal/lineedit'` — `internal/memoryprefetch` and `internal/lineedit` reach
  `Action:"pass"` in that stream; `./cmd/agent` was still running when this
  session ended (it is a large package). Every failure that stream DID produce was
  checked against a baseline rather than waved at — the next bullet.
  Focused, before the commit: `go test ./cmd/agent/ -run 'Prefetch|Turn' -count=1
  -v` → **30 PASS / 0 FAIL** (`TestTurnTermsNeverReadTheFramedProse`,
  `…TurnInputReadsThePayloadBehindAReference`, `…TurnAdoptionOffPathIsInert`,
  `…TurnAdoptionRecordsTheSource`, `…TurnAdoptionNeverWaitsLongerThanItsBound` are
  the five new ones); `go test ./internal/memoryprefetch/ -run 'Turn|Defaults'
  -count=1 -v` → **8 PASS** (7 new + the defaults table); `go test
  ./internal/lineedit/ -run 'PasteRef|StripPaste' -count=1 -v` → **9 PASS** (the
  new strip test + the parser's existing set); `go test ./internal/lineedit/
  -count=1` (whole package, the parser change is shared by four readers) → **ok
  90.7s**.

- **Ran (Rule 31 — the surface I changed, for real, on the built binary):** a real
  `gopi chat` on a **real pty**, in a **sandbox profile** (`gopi tool sandbox
  create --label turn-prefetch`), driven by
  `<session-dir>/artifacts/pty_turn_drive.py` — once per input face. The notify
  run writes **NOTHING to stdin**; the delivery comes from another process.

  ```
  Ran (notify face):
    GOPI_HOME=$SB python3 …/artifacts/pty_turn_drive.py \
      <worktree>/bin/gopi <worktree> $SB/.gopi notify …/data/notify-raw.log …/data/notice_payload.txt
    → queued: message 06b06610aec8f1e2 for session e3e3bcec-7449-4cd2-be96-fae6ecf6f26b
  Result: $SB/.gopi/sessions/e3e3bcec-…/memory-prefetch.jsonl
    {"query":"turn boundary prefetch notify delivered message reaches retrieval",
     "hits":2,"ms":3,"outcome":"hits","adopted":"no","source":"notify",
     "segments":{"seg_query_ms":1,"handle":"session","journal_mode":"wal",
     "query_only":1,"busy_timeout_ms":250,"attempts":1}}
    {"query":"turn boundary prefetch notify delivered message reaches retrieval",
     "hits":0,"ms":0,"outcome":"adopted","adopted":"yes","source":"notify",
     "section_bytes":498}

  Ran (paste face):
    GOPI_HOME=$SB python3 …/artifacts/pty_turn_drive.py \
      <worktree>/bin/gopi <worktree> $SB/.gopi paste …/data/paste-raw4.log …/data/paste_payload.txt
    → the pty log shows the editor's own spill: "[paste 5411: /…/paste/paste-20261006-042623-1.txt source=human size=5411B]"
  Result: $SB/.gopi/sessions/ec1c688d-…/memory-prefetch.jsonl
    {"query":"paste turn 5411 home nick workspace gopi sandbox","hits":2,"ms":4,
     "outcome":"hits","adopted":"yes"}
    {"query":"paste turn spilled payload behind reference boundary reads","hits":2,
     "ms":0,"outcome":"hits","adopted":"no","source":"human"}
    {"query":"paste turn spilled payload behind reference boundary reads","hits":0,
     "ms":0,"outcome":"adopted","adopted":"yes","source":"human","section_bytes":498}
    {"query":"","hits":0,"ms":0,"outcome":"superseded","adopted":"no"}
  ```

  The first paste line is the `adopted:"yes"` event of the TYPING path (the
  operator's own short head line, sampled as it was typed); the second and third
  are this change's SEALED-TURN lookup and its adoption. The last line is the
  supersession this report's §3 describes, visible in the record rather than
  argued.

- **Ablation of the pointer strip (the same surface, one change apart):** with
  `StripPasteRefs` NOT applied, the sealed-turn query was `paste turnpaste turn
  spilled payload behind reference boundary` (the duplicate is the head line
  appearing both as a Part and as the payload's first line — the payload is read
  whole and its first line IS the head). With it applied on the short-head run:
  `paste turn 5411 home nick workspace gopi sandbox` → the pointer-free query
  above. The instrumented run that produced the `termsparts=` line is in
  `…/artifacts/data/paste-raw4.log` and the debug print was REMOVED before the
  commit (`grep -c GOPI_PREFETCH_TURN_DEBUG` = 0 in the committed file).

- **OFF path:** `TestTurnAdoptionOffPathIsInertWithTheSwitchOff` asserts that with
  the switch off the turn-boundary call returns the message byte-identical, does
  not touch Display, and writes no record file; `TestPrefetchOffWritesNoRecord`
  (pre-existing) pins the file's absence for the whole executor. `cli_chat.go`
  computes the terms INSIDE a `prefetch != nil` guard, so the OFF path does not
  reach even the pure text assembly.

- **Baseline control (Rule 39's "a scan that found nothing must be distinguishable
  from a scan that never ran"):** the same 21 named `cmd/agent` cases were run in a
  DETACHED worktree of this branch's own merge base, `61acd58a` —
  `go test ./cmd/agent/ -run '<the 21 names>' -count=1` → **EXIT=1, 25 named
  failures, names identical** (including the loudest ones:
  `TestCLIContractInventoryMatchesDispatch`,
  `TestUndispatchedToolWordsNameADispatchingCanonical`,
  `TestToolsMaintenanceHelpSurfaceFormsAreTyped`, `TestG4HelpSurfaceMatchesInventory`, …).
  The budget stream's named-failure set minus that baseline set is **EMPTY**; the
  two cases the stream had that the filter did not
  (`TestVoiceInputIsAContractToolMirror`,
  `TestVoiceInputIsRegisteredInContractMirrors`) were then run alone on
  `61acd58a` and are red there too. So: none of the red in this run's stream was
  introduced here, and that is a measured set difference rather than a judgement.
- **Repo state:** `git status --porcelain` in the worktree — see Evidence.

## Evidence (self-filed)

- `CGO_ENABLED=0 go build ./...` → exit 0 (this run, after the last edit).
- `go test ./cmd/agent/ -run 'Prefetch|Turn' -count=1 -v` → 30 PASS / 0 FAIL (the
  list is in `## Verification`; observed in this run before the commit).
- `go test ./internal/memoryprefetch/ -run 'Turn|Defaults' -count=1 -v` → 8 PASS.
- `go test ./internal/lineedit/ -run 'PasteRef|StripPaste' -count=1 -v` → 9 PASS.
- `go test ./internal/lineedit/ -count=1` → `ok … 90.695s`.
- The two live pty runs above: the record JSON quoted is what `cat` printed for
  those two files, in this run.
- `make test-budget PKGS='./cmd/agent ./internal/memoryprefetch ./internal/lineedit'`:
  the run stream is written to a file this run names, and its outcome is quoted
  from that file rather than asserted here.
- Rule 30: `git status --porcelain` is run in the worktree immediately before the
  close-out and its output (empty or not) is quoted in the final message rather
  than claimed here.
