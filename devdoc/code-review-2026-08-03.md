<!-- doc-class: normative -->
# GoPi Code Review — 2026-08-03

> Reviewer: automated GoPi agent review
> Scope: 3,634 lines Go across 18 source files, 5 test files
> Build: ✅ `go build ./...` clean
> Vet: ✅ `go vet ./...` clean
> Race: ✅ `go test -race -count=1 ./...` clean (all 3 packages pass)
> Coverage: agent 62.4%, deepseek 63.2%, tools 56.1%

---

## Summary

The codebase is clean, well-structured, and follows its own coding rules (GOPI.md)
consistently. All 4 hard rules (no magic numbers, single source of truth for
deadlines, minimal tools, surgical edits) are respected.  The test suite is
solid for the core paths.

Three actionable findings below — one dead code, one missing test, and one
documentation that overstates implementation status.

---

## Finding 1: Dead Orphan Code — `runNonStreaming` (Medium)

**File:** `internal/agent/loop.go:588-670`

`runNonStreaming()` is a ~80-line function that duplicates the ReAct loop but
uses non-streaming `Chat()` instead of `ChatStream()`.  It is **never called
from any production path**.

The actual streaming-fallback story is handled *inline* in `chatOnce()` (lines
~340-370): when `ChatStream()` fails, it calls `Chat()` directly and returns the
result.  `runNonStreaming` is bypassed entirely.

**Why this matters:**

- `runNonStreaming` lacks **session logging** — no `SessionLog` calls at all.
  If someone refactored the code to use it, JSONL session files would be
  silently incomplete.
- It lacks **file logging** for tool calls (`a.LogFile.Info("TOOL …")`).
- It lacks **finish_reason handling** (`length`, `content_filter`).
- It lacks **timing stats** printed at exit.
- It has **no `trimContext()` call** — context window trimming is absent.

**Recommendation:** Either delete the function (it's dead code), or if it's
kept as a reference implementation, add a `// Deprecated:` godoc comment and
a compile-time assertion that it's never called from production paths.

```go
// Deprecated: runNonStreaming is a legacy reference implementation.
// All production paths use Run() → chatOnce() which handles streaming
// with inline fallback to Chat().  This function is only called from
// tests and will be removed in a future cleanup.
func (a *Agent) runNonStreaming(userInput string) (string, error) {
```

---

## Finding 2: Missing `edit_test.go` (Medium)

**Files:** `internal/tools/edit.go` (63 lines), no corresponding test file.

The `edit` tool is the only tool without dedicated tests:

| Tool  | Test file            | Lines |
|-------|---------------------|-------|
| read  | `read_test.go`      | 222   |
| write | `write_test.go`     | 120   |
| bash  | `bash_test.go`      | 123   |
| edit  | **none**            | 0     |

The `edit` tool has non-trivial logic:
- `~/` expansion (different approach than `read`/`write` — uses `home + path[1:]`
  instead of `filepath.Join`)
- String uniqueness check (`strings.Count(content, oldStr)`)
- OldStr-not-found error
- Non-unique error with count in message
- The `EditDetails()` helper function

These are all untested. If the `~/` expansion breaks or the uniqueness check
has an off-by-one, no test will catch it.

**Recommendation:** Add `internal/tools/edit_test.go` covering:
- Basic replacement
- `~/` expansion
- `oldStr` not found
- `oldStr` appears multiple times (non-unique)
- Empty path rejection
- Empty `oldStr` rejection

---

## Finding 3: Devdoc Overstates Implementation Status (Low-Medium)

**File:** `devdoc/tasklist-heartbeat-kernel.md`

This document describes `tasklist.go`, `tasklist_test.go`, `heartbeat.go`,
`heartbeat_test.go` as "NEW" files and `loop.go` as "EDITED" with specific
kernel prompt changes.  It includes a "Files Changed" table and "Testing"
section claiming "36 tasklist tests" and "14 heartbeat tests."

**None of these files exist in the repository.**  The kernel prompt is still
the original 4-tool line:

```go
systemPrompt := "You are GoPi, a coding agent. Tools: read, write, edit, bash."
```

The task list + heartbeat feature is entirely in the design phase.  The
document reads as if it were implemented, which will confuse future developers.

**Recommendation:** Either:
- Add a clear `**STATUS: DESIGN ONLY — NOT YET IMPLEMENTED**` banner at the
  top of `tasklist-heartbeat-kernel.md`, or
- Move unimplemented design docs to a `devdoc/designs/` subdirectory to
  separate them from documentation of existing features.

---

## Finding 4: `ContextOverflow` Unused Function (Low)

**File:** `internal/deepseek/types.go:162-173`

`ContextOverflow(messages []Message, maxTokens int) bool` is defined but never
called anywhere.  The agent uses `EstimatedTokens()` + `TrimToFit()` instead.

**Recommendation:** Remove or add a comment explaining why it's kept.

---

## Finding 5: `truncateArg` is a No-Op (Low)

**File:** `internal/agent/log.go:180`

```go
func truncateArg(s string) string { return s }
```

The function name implies truncation, but it returns the string unchanged.
The comment says "returns the full string, no truncation."

**Recommendation:** Either implement actual truncation (e.g., to 200 chars
with `…` suffix for very long commands) or inline the call sites and delete
the function.  A no-op wrapper adds indirection with no benefit.

---

## Minor Observations (No Action Required)

These are not bugs but worth noting for future cleanup:

1. **`loop.go:173` — `0 * time.Second`** is valid Go but `0` alone would
   be clearer for the default timeout value.

2. **`tools/edit.go:22` — tilde expansion differs from `read.go`/`write.go`:**
   `edit` uses `home + path[1:]` while `read`/`write` use `filepath.Join(home,
   path[2:])`. Both work for `~/` paths but differ for bare `~` (edit doesn't
   handle it, read does).  Worth unifying in a shared helper.

3. **No `go.sum` committed:** The `go.mod` declares `go 1.22` with zero
   dependencies (admirable!), but if any dependency is ever added, the
   `go.sum` should be committed for reproducible builds.

---

## What's Working Well

- **Rule compliance:** All 4 GOPI.md rules followed consistently. No magic
  numbers anywhere — every timeout, limit, threshold is a named constant.
- **Timeout architecture:** Single source of truth (`GOPI_TIMEOUT_SEC`),
  resolved once in `NewAgent()`. The 4-timeout-bug era is squarely behind us.
- **Session logging:** Thread-safe, fsync'd, comprehensive JSONL format.
  Thinking/reasoning content captured in both streaming and non-streaming paths.
- **Context window trimming:** Well-tested, orphan-proof (tool results always
  travel with their owning assistant message).
- **Retry logic:** Exponential backoff with jitter, Retry-After header
  support, network vs HTTP error distinction.
- **Tool argument validation:** `validateToolArgs()` prevents tools from
  running on empty/blank parameters, returning standardized error messages
  the LLM can self-correct from.
- **Test quality:** 36 tests in `session_test.go` alone (including concurrent
  write safety), 15+ tests in `loop_test.go` with mock SSE servers, solid
  config resolution tests.

---

## Test Results

```
$ go build ./...          ✅ clean
$ go vet ./...            ✅ clean
$ go test -race ./...     ✅ all pass (no races)

ok   github.com/the operator/gopi/internal/agent     5.040s  coverage: 62.4%
ok   github.com/the operator/gopi/internal/deepseek  13.437s  coverage: 63.2%
ok   github.com/the operator/gopi/internal/tools     1.051s  coverage: 56.1%
```

The deepseek package took 13s because integration tests (`TestChatSimple`,
`TestChatWithToolCall`, `TestChatStream`) hit the live API — these are
correctly gated behind `requireAPIKey(t)`.
