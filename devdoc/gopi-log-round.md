<!-- doc-class: historical -->
# Task Report: `gopi log <session-id> <round>` — the session round view


**Task:** 01a0dda1-67de-7a3c-b840-d7a6457bf383 · **Branch:**
`task/01a0dda167de7a3cb840d7a6457bf383` · **Worktree:**
`/home/user/workspace/gopi-01a0dda167de7a3cb840d7a6457bf383`

## Completed

- **`gopi log <session-id> <round>`** — one round of a session: its header
  (round N of M, usage, elapsed, tool names), the prompt that opened it, its
  thinking/text blocks, and every tool call with its result
  (`internal/gopilog/sessionround.go`, `cmd/agent/cli_log_round.go`).
- **`<round> request|response`** are NOT new flag names: `--request` /
  `--response` already exist on `gopi log`, so the round BYTE views reuse them —
  `gopi log <id> 3 --request` prints round 3's request body verbatim, sourced
  from the raw HTTP log (stdout stays pure bytes; the provenance line goes to
  stderr). One printer serves both verbs (`printRawRoundAt` in
  `cmd/agent/cli_raw.go`, extracted from `printRawRound`).
- **`gopi log <session-id> --rounds`** — the session ROUND TABLE: round number →
  the raw-log ordinal it pairs with → that round's raw byte span, plus usage.
  This is the map an operator needs before naming a round.
- **The primary work was the round-numbering correspondence**, because "round 3"
  had to mean the same exchange on both surfaces. Measured, not assumed:
  - raw round N ≡ JSONL iteration N−1 (**0-based** in the JSONL), confirmed by
    token identity — raw `output_tokens` == JSONL `completion_tokens` per round
    (e.g. session `01a0dd87-82d5`: raw round 1 input 2011 / output 184 ↔ JSONL
    iteration 0 prompt 26075 (= cache_hit 24064 + miss 2011) / completion 184).
  - **the offset is NOT a constant**: the raw log also numbers sends the JSONL
    never recorded (non-streaming meta calls — a round whose only response is a
    `← RESPONSE` body — and retried sends). Measured across finished runs: raw
    rounds = JSONL request events + k with k = 0 (e.g. `01a0dd7c`: 42/42) or
    k = 1..4 (`01a0ddd83`: 127/126 + 1 meta; `01a0dd4f`: 185/181 + 4).
  - Hence `internal/gopilog/sessionround.go` **derives** the pairing
    (`SessionRoundRawOrdinals`): confirm by usage-token identity, order-preserving,
    each raw round taken once; when the session log carries no usage at all, pair
    positionally and mark it (`Exact=false`, rendered `N?` in the table) instead of
    claiming a match.
- **Numbering contract for the session view:** `1` = the session's FIRST API call;
  `0` / `-1` / `last` = its last; `-N` = Nth-from-last; out of range is REFUSED in
  both directions (`gopilog.SessionRoundIndex` range-checks, unlike
  `HTTPDetailIndex` which clamps because the raw byte view needs a best-effort
  answer). Documented in `logUsage`.
- **One from-the-end rule** (`gopilog.selectIndex`, `internal/gopilog/httpview.go`):
  `HTTPDetailIndex` and `SessionRoundIndex` now share the arithmetic instead of
  mirroring it (Rule 1b/13).
- **The advertised surface is gated, and the gate runs each row.** Six new
  `logUsage` synopsis lines; the G4 harness
  (`cmd/agent/cli_session_help_surface_basic_test.go`) derives them from the
  usage constants and EXECUTES each against the real binary + real fixture, so
  the new rows carry pinned expectations, cases and inventory rows (a two-place
  change by design).
- **`--rounds` accepts no combination that cannot nest**: with a round, `--chat`,
  `--all`, `--rounds`, `--last` are each refused by name (a round is one
  exchange); the content flags (`--thinking`, `--memory`, `--filter`, `--request`,
  `--response`, `--json`, `--text`) DO nest — that is the feature.

## Not Done (honest)

- **`gopi log <id> --rounds --last N`** (the round table for the newest N
  sessions) is refused rather than implemented: `--rounds` is per-session today.
  The refusal says so and names the working spelling. It is a natural follow-up,
  not silently dropped.
- **`--json` does not change the round view's shape** — a round renders as text
  under `--json` (the flag is accepted and ignored for this scope). A structured
  round payload is a follow-up.
- **The pairing is per-SESSION, and the raw log may be SHARED** between sessions
  (the raw-file rotation `gopi raw` warns about): `--rounds`/`--request` pair
  against whatever `ResolveRawLogForSession` resolves, which for a shared log is
  the whole rotation. Token identity usually keeps the pairing honest, but a
  session whose exchanges are absent from the resolved log pairs POSITIONALLY —
  and the table marks that (`N?`) rather than hiding it.
- I could not attribute the pre-existing 603s full-`cmd/agent` failure (leaked
  `database/sql` connection-opener goroutines) from this branch's state: it
  reproduces in the whole-package run, which is a heavy path outside the
  merge/verify scope. Touched-surface and standards suites are green; see below.

## Verification

- Build: `go build ./...` → OK (`build-exit=0`); CLI binary
  `CGO_ENABLED=0 go build -o …/gopi ./cmd/agent/` → OK.
- Tests:
  - `go test ./internal/gopilog/ -run 'TestEveryTaskEmitSite|TestFormat|TestSession|TestHTTP|TestRaw|TestParse|TestStats|TestGopiRaw|TestCov' -count=1` → `ok … 4.606s`
  - `go test ./cmd/agent/ -run 'TestG4' -count=1` → `ok … 13.549s` (the help-surface gate: 87 derived rows, of which the six new `log` round rows and four repaired `resume` rows are all executed against the real binary)
  - `go test ./cmd/agent/ -run 'TestParseLogArgs|TestSessionRound|TestGopiLogRound' -count=1` → `ok … 3.214s`
  - `go test ./internal/standards/ -count=1` → `ok … 80.726s` (all six machine gates)
- Ran (Rule 31 — the changed surface, real arguments, real session
  `9662feb3-0fb3-468d-a606-1e42c8aa4121`, 53 rounds / 55 raw rounds — a session
  whose raw log carries the extra rounds):
  - `gopi log 9662feb3-… --rounds` → table; last row `53 54` and the tail rows
    show the +1 offset explicitly; the raw columns are LIVE (not dashed) for
    paired rounds.
  - `gopi log 9662feb3-… 1` → `SESSION … ROUND 1 of 53` + usage + the round's
    content (also `0`, `last` → `ROUND 53 of 53`, stdin-equivalent equality with
    round 53 asserted in the unit test).
  - `gopi log 9662feb3-… 1 --request` → the request body bytes on stdout
    (`{"model":"deepseek-flash","max_tokens":16384,"system":…`); stderr:
    `[gopi log] session round 1 is raw round 1 of 55 — request bytes below …`.
  - `gopi log 9662feb3-… 1 --response` → the logged stream lines (`← CHUNK` …
    `"input_tokens":1554 … "output_tokens":0`); stderr names raw round 1 of 55.
  - `gopi log <id> 999` → exit 1, stdout empty, stderr
    `gopi log: round 999 does not exist — session … has 53 round(s) (numbering is
    1-based and counts the session's own API calls, first round = 1; --rounds lists them)`.
  - `gopi log <id> all` → exit 1,
    `gopi log: <round> must name one round — a round number, 0, or last/-N; "all"
    asks for the whole-session view (drop <round>, or use --rounds)`.
  - `gopi log <id> abc` → exit 1, `gopi log: <round> "abc" is not a round …`.
  - `gopi log <id> 3 --chat` → exit 1, `gopi log: <round> scopes the view to one
    round — --chat renders the whole conversation`.
- Result: all of the above observed on the built binary; stdout stayed a pure
  byte stream for `--request`/`--response` (provenance on stderr), and both
  refusals exited 1 with the naming message.

## Evidence (self-filed)

- Pairing measurement (raw round ↔ JSONL iteration), session `01a0dd87-82d5-…`:
  JSONL request events `(iter, prompt, completion)` =
  `(0,26075,184) (1,30900,1318) (2,32753,107) …`; raw rounds
  `(n, input_tokens, output_tokens)` = `(1,2011,184) (2,4916,1318) (3,8689,107) …`
  → raw N ≡ iter N−1 by BOTH token fields (raw `input_tokens` = JSONL `cache_miss`).
- Offset census (raw rounds / JSONL request events / non-streaming `← RESPONSE`
  rounds): `01a0ddd83 127/126/1`, `01a0dd4f 227/226/1`, `01a0dcfa 206/205/1`,
  `01a0dd7c 42/42/0`, `01a0dd4f 185/181/1`.
- `gopi log 9662feb3-… 1 --request` stderr line quoted verbatim:
  `[gopi log] session round 1 is raw round 1 of 55 — request bytes below (stdout carries the bytes only)`.
- Advertised-surface derivation, `scripts/g4-session-help-surface.sh …/gopi` →
  87 rows, matching the doc's table row for row (`diff` clean).
- Pre-existing drift found and repaired in the file I had to edit: HEAD's
  `g4ExpectedRows` pinned `resume Examples gopi resume <target> --task …` while
  the untouched `cmd/agent/cli_resume.go` prints eight concrete examples —
  `git show HEAD:cmd/agent/cli_session_help_surface_basic_test.go` shows the
  stale pin, so the mismatch predates this branch.
- Commit: `c7c62eaf feat(log): gopi log <session-id> <round> — the session round view`
