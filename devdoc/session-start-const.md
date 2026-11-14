<!-- doc-class: historical -->
# Task Report: session_start event name — one definition, every site references it

## Completed

### 1. Single definition site (Rule 14 leaf package)

New package `internal/sessionevent` — a leaf: **zero imports of project
packages**, so it can never create an import cycle. This is forced, not
stylistic: `internal/agent` writes the event, `internal/gopilog` /
`internal/tools` / `internal/testutil` read it, and `internal/gopilog`
already imports `internal/agent` — no consumer package can hold a definition
the others reach. Precedent: `internal/toolnames`.

`internal/sessionevent/events.go` defines both lifecycle names (the sibling
`session_end` was moved there too, so the pair lives in ONE place):

```go
const (
	EventTypeSessionStart = "session_start"
	EventTypeSessionEnd   = "session_end"
)
```

Wire values are byte-identical to before — the persisted JSONL format is
unchanged (`gopi log`, `gopi stats`, `gopi task show`, chat backfill all read
the same bytes). The doc comment on the package says so explicitly, so a
future value change requires a migration.

`internal/gopilog/reader.go`'s unexported `eventTypeSessionEnd` was **kept**
(13 test references depend on it) but is now an alias:
`eventTypeSessionEnd = sessionevent.EventTypeSessionEnd`.

### 2. Every inline production site replaced

8 production files, 16 `"session_start"` + 13 `"session_end"` sites:

| file | what changed |
|---|---|
| `internal/agent/session.go` | writer (`NewSessionLogger`) + 2 readers of `session_end` |
| `internal/agent/trajectory.go` | 2 switch cases |
| `internal/gopilog/reader.go` | 4 switch/compare sites + the alias |
| `internal/gopilog/formatter.go` | 1 compare |
| `internal/gopilog/rawlog.go` | 3 compares |
| `internal/gopilog/stats.go` | 5 sites + the two probe vars |
| `internal/tools/chat.go` | 2 switch cases |
| `internal/testutil/session.go` | 3 sites (non-test `.go` file — included) |

The two probe vars in `stats.go` are the interesting case: they were built by
inlining the literal inside a byte-slice, which is exactly the shape a naive
grep-and-replace *misses* (the literal has embedded quotes). They are now
`[]byte(`"` + sessionevent.EventTypeSessionStart + `"`)`, so the definition
drives them too.

### 3. Rule 13 exhaustive gate

`internal/sessionevent/literal_gate_test.go`:

- **Repo-wide, AST-based** (`go/parser`, `go/ast`), not a package-local byte
  grep — the package-local form would have missed every file that carried the
  literal. It walks from the module root (found by walking up to `go.mod`),
  skips `.git` / `testdata` / `vendor` / `node_modules` and `_test.go`
  files, and exempts exactly one path: `internal/sessionevent/events.go`
  (`gateDefinitionFile` — the ONE allowed holder).
- **Fixture strings are built from the constants** (`"` + EventTypeSessionStart
  + `"`), so the gate file itself never re-inlines the literal.
- **Comments do not count** (AST, so comments aren't visited) — quoting the
  event name in prose stays legal, which keeps the gate from punishing
  documentation.
- **Vacuous passes are failures**: an unparseable file is itself a violation,
  and the test fails if fewer than 50 production files were parsed (proves the
  walk is really repo-wide).

## Not Done

- `_test.go` files still build session-JSONL fixtures with the raw literal
  (e.g. `internal/tools/tasks_test.go`, `chat_test.go`). Deliberate: a fixture
  whose whole purpose is to assert the on-disk wire format *should* spell the
  literal out — deriving it from the constant would make the test
  self-fulfilling (it could no longer catch a value change). The gate exempts
  `_test.go` for that reason, and the doc comment in `literal_gate_test.go`
  records it.
- Third-party / non-`internal` trees (`cmd/`, `pkg/`) currently have no such
  literal; the gate scans the whole module anyway, so a future one fails.

## Verification

- **Acceptance grep** — `grep -rn '"session_start"' --include=*.go internal/ | grep -v _test.go`:
  returns exactly one line, `internal/sessionevent/events.go:35: EventTypeSessionStart = "session_start"`
  (the prose quoting the name in the package doc was reworded to unquoted for
  an unambiguous grep; the AST gate never needed that).
- **Gate teeth (proven, not asserted)** — planted a real re-inline
  (`internal/gopilog/zz_gate_probe.go` with `var zzGateProbe = "session_start"`):
  `go test -run TestSessionEventLiteralGate ./internal/sessionevent/` →
  `--- FAIL: TestSessionEventLiteralGate … Rule 13 violation: internal/gopilog/zz_gate_probe.go carries "session_start"`.
  Probe removed → test PASSes again. A gate that cannot fail is decoration;
  this one was made to fail. (A second, earlier proof re-inlined the literal in
  a *real* site — the `case` clause in `internal/gopilog/reader.go` — and the
  gate failed naming that file too.)
- **Build** — `CGO_ENABLED=0 go build ./...` → OK (no output).
- **gofmt** — `gofmt -l` over the touched packages → empty.
- **Tests** —
  - `go test -count=1 ./internal/sessionevent/` → `ok` (5 tests: gate,
    whole-repo scope, teeth, comment-precision, wire-value pin).
  - `go test -count=1 ./internal/gopilog/` → `ok ... 2.779s`.
  - `go test -count=1 ./internal/sessionevent/ ./internal/testutil/ ./internal/tools/`
    → `ok` ×3 (`internal/tools` 175.6s, `internal/sessionevent` 0.307s,
    `internal/testutil` 0.015s), exit 0.
  - `go test -count=1 ./internal/agent/` → `ok ... 392.106s` (the required
    pair with gopilog is green; run as a background job because the package is
    long-running).
  - `go vet ./internal/sessionevent/` → clean.
- **Leaf proof (Rule 14)** — `go list -deps ./internal/sessionevent/` contains
  no other `nick/gopi` package; `go list -f '{{.Imports}}'` on it → `[]`.
  The import cycle it avoids is real and verified: `go list` shows
  `internal/gopilog` imports `internal/agent` (via `gopilog/formatter.go`),
  so neither could host the definition.
- **Wire behaviour unchanged** — `TestSessionEventWireValues` pins
  `session_start` / `session_end` byte-for-byte, and the acceptance grep shows
  the values are now written only from the constant.

## Commit / Branch

- Worktree: `~/workspace/gopi-sess-start` (linked worktree, branch
  `fix/session-start-const`) — no clone, no push: the commit lands on the
  branch in the shared repo.
- Files added: `internal/sessionevent/events.go`,
  `internal/sessionevent/literal_gate_test.go`, this report.
