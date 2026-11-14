<!-- doc-class: historical -->
# Task Report: gopi resume — minimal viable closed loop

## Completed

Implemented resume (Tier B-lite: raw-log byte-exact extraction, struct-based
replay) on branch `feat/resume-arm-trim`:

1. **`internal/checkpoint`** (new package) — raw-log checkpoint extraction:
   - `Scan` — marker-driven parser over `gopi-raw-*.log` (`→ REQUEST` /
     `← CHUNK` / `← RESPONSE` entries, multi-line SSE payloads handled
     correctly — feasibility §2.4 update).
   - `LastRequest` — extracts the **last** `→ REQUEST` (the resume point;
     crash-safe — raw log is fsync'd per round). Reads only the file TAIL in
     growing 8MB windows — never scans the whole file, so resume works on
     the 3.7GB of accumulated logs (~0.3s vs full-scan minutes).
   - `IndexDir` / `CountRequests` — `checkpoint list` registry (model,
     msg count, payload sha256, request count via streaming memmem count).
2. **`internal/agent/resume.go`** — `Agent.Resume(req, continuation)`:
   - Seeds `Ctx` from the checkpoint: system blocks (kernel prompt + GOPI.md
     at interruption time — survives cwd drift), full history converted back
     to internal messages **with thinking text + signatures preserved**,
     wire params replayed (model, thinking budget, reasoning_effort,
     max_tokens), `KeepThinking` forced on.
   - Continuation appended **verbatim** — no `[EPIC GOAL]` reframing, no
     repeated task-start memory checkpoint.
   - `Run()` refactored: loop body extracted into shared `runLoop()` —
     fresh and resumed sessions run the identical ReAct loop.
3. **CLI** — `gopi checkpoint list`, `gopi checkpoint export <log>`,
   `gopi resume <log> [continuation...]` (log = path / bare filename /
   `YYYYMMDD-HHMMSS` timestamp prefix; continuation from args, piped stdin,
   or default "Continue from where you left off.").
4. **Tests** (all green):
   - `internal/checkpoint/parse_test.go` — entry parsing, byte-exact
     multi-line chunks, last-request extraction, no-op clone invariant
     (payload re-parses to the identical request), tail-window path, empty /
     no-request logs, request counting, index.
   - `internal/agent/resume_test.go` — **round-trip invariant**
     `wire → messages → wire` reproduces identical request messages
     (thinking + signatures + tool_use args survive); `seedFromCheckpoint`
     (system blocks win, params replayed, KeepThinking on); end-to-end
     `Resume` against an httptest SSE server asserting the resumed request
     contains the full checkpoint history + verbatim continuation, no
     `[EPIC GOAL]` framing.
   - `cmd/agent/cli_checkpoint_test.go` — dispatch negatives + log-id
     resolution.
5. **Verification**: `go build ./...` OK, `go vet ./...` clean, full
   `go test ./...` green (see below), smoke: `checkpoint export` on a real
   205-message log — 0.28s, correct model/thinking/effort, last message is
   a tool_result (a genuine mid-task interruption point).
6. **Live closed-loop proof** (accidental but real): `gopi resume
   20260826-123456 "Continue: finish part 2"` against a fixture checkpoint
   hit the real API (settings.json key wins over a bogus env key) and the
   resumed agent continued — new session raw log shows its last request
   carrying "Continue: finish part 2" as the final user message, no [EPIC
   GOAL] reframing. Interruption → resume → continue verified end-to-end.

## Not Done (honest)

- **Byte-splice replay (Tier B full)** — the first resumed request is
  rebuilt from the struct (map-key-sorted re-marshal), so the DeepSeek
  prefix-cache hit may be partial. The context CONTENT is byte-exact (from
  the raw log), but the request bytes are not spliced. Cache-hit
  verification against the live API is untested.
- **Live API end-to-end** — the closed loop is verified via the httptest
  SSE integration test; a real interrupted-session resume against DeepSeek
  (with cache-hit stats) was out of budget.
- **Rollback to checkpoint N < last** — only the last checkpoint is exposed.
- `Scan`'s full-file walker is used by tests only; production paths use the
  tail window / streaming count.

## Collision note (concurrent agent)

A memory-engine-race gopi session ran `resume-race-briefing.md` via
`./bin/gopi` with cwd `~/work/gopi-resume-arm-trim` DURING this task,
implementing the same feature in the same worktree. It added a legacy
string-content `UnmarshalJSON` to `internal/deepseek/claude_client.go`
(uncommitted, not mine — left untouched) and briefly edited my
`internal/checkpoint/parse_test.go` (its tests depended on that uncommitted
change and failed). My commit fc36fb5 is self-contained and excludes both.

## Verification

- Build: `go build ./...` OK
- Vet: `go vet ./...` clean
- Tests: checkpoint 7/7, resume 3/3, cmd/agent OK (full suite below)
- Commit: see git log on `feat/resume-arm-trim`

## Design notes (pitfalls found)

- `findMarker` returns the marker **offset**, not length — Scan initially
  treated it as length → payloadStart=0 → infinite loop (caught by test
  timeout).
- Per-byte marker scanning is O(n·m) — unusable on multi-GB logs; switched
  to `bytes.Index`/`bytes.LastIndex` (memmem) + line-boundary guard. Payload
  lines can never begin with a marker (JSON payloads have no literal
  newlines; SSE lines start with `event:`/`data:`), so boundary checks are
  paranoia that costs nothing.
- Wire message count ≠ internal message count: BuildClaudeRequest coalesces
  tool results + follow-up user text into one user message; resume tests
  must assert on the wire shape, not the internal count.
