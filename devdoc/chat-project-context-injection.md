<!-- doc-class: historical -->
# Chat Carries the Project Rule File (GOPI.md) — and Rule 37 States What a Chat May Write

**Task**: 01a0e689-3724-76dc-b037-da846cf469c5 · branch `task/01a0e689372476dcb037da846cf469c5`
**Class**: normative (a standing claim about the chat path's behaviour + the rule file's content).

## The defect this closes

A chat session is started in a real checkout — usually the MAIN working area —
and it answered every question about that checkout's rules from the KERNEL prompt
plus its own chat-surface block, never from the project's own `GOPI.md`. The task
path had loaded the file since `projectctx.go` landed (`loop.go:2994` →
`Agent.Ctx.ProjectContext` → the 2nd system block); the chat path passed `""` at
BOTH of its request builders (`loop.go`'s plain `Agent.Chat`, `chat_ops.go:597`'s
`ChatOps`), with the deliberate comment *"no project GOPI.md in chat mode"*.

The cost of that omission became visible the same week the chat write boundary
landed (`chat_write_boundary.go`, task 01a0e454): the boundary was told to the
model in the CHAT block, while the project's own rule file — where the same
subject is a numbered rule — was withheld from exactly the run that needed it.

## What changed (3 files, one commit)

| file | change |
|---|---|
| `internal/agent/loop.go` | `Agent.Chat`'s `BuildClaudeRequest` call: `""` → `a.Ctx.ProjectContext` |
| `internal/agent/chat_ops.go` | `ChatOps`'s `BuildClaudeRequest` call: `""` → `a.Ctx.ProjectContext` (+ the doc-comment that claimed the opposite) |
| `internal/agent/chat_project_context_test.go` | new regression file (4 tests, below) |
| `GOPI.md` | new **Rule 37** after Rule 36 |

**No second reader, no cache, no new knob** (Rule 35). The field the task path
already fills is the field the chat paths now read: `LoadProjectContext("")` runs
ONCE in `NewAgent` (`loop.go:2994`), so a chat session resolves the file at
session start exactly like a task run — same upward walk, same two boundaries
(`projectctx.go`), same `[gopi] loaded GOPI.md (N bytes)` / `no GOPI.md found —
searched: …` lines on stderr. `BuildClaudeRequest` (deepseek/claude_client.go:475)
drops an empty `projectContext`, so the optional-file guarantee is structural, not
an `if` in the chat path.

**Placement**: the file rides its OWN system block (block index 1), exactly as on
the task path — it is not concatenated into the chat prompt block. That keeps the
Anthropic prompt cache working (the wire dump below shows `cache_control:
ephemeral` on the project block and `None` on the prompt block: a chat prompt
that changes per run — host · node · repo · this run's transcript — must not sit
in the same block as 64 KB of stable rules), and it keeps the existing readers of
`System[1]` (`context_test.go`, the chat prompts' own tests) truthful.

**Direction preserved**: the injection is one-way — GOPI.md flows INTO the chat
prompt. Nothing from the chat machinery is written into GOPI.md, and no task
scaffolding rides along: no task row, no task-list nudge, no task-start memory
enforcement. The chat path's own doc-comment now says precisely that (the
scaffolding list stayed; only the GOPI.md clause changed).

## Measured cost (the number the epic asked for)

Measured on this tree, test `TestChatProjectContextCostIsMeasured` (recomputable
with `go test ./internal/agent/ -run TestChatProjectContextCostIsMeasured -v`):

- **`GOPI.md` today: 66 284 bytes on disk, 64 602 runes** — the request's system
  block carries the runes (64 602 bytes of UTF-8 body after the wire's encoding),
  and the harness's own estimate (`EstimateTokens`, the chars/4 rule the trim
  decision uses) is **~16 153 tokens** per request, against a 1 M-token context
  window (~1.6 %). It is sent **once per request**, with prompt caching marking it
  `ephemeral` — so on a multi-round chat it is paid at full rate once per request
  and at cache rate thereafter.
- Rule 37 itself adds **3 006 bytes / 2 936 runes** to that block (~734 tokens by
  the same rule); the pre-change file was 63 278 bytes.

A caveat worth stating: 64 KB of project rules is not free — it is ~16 K tokens
on every round-trip. That is the price of the correctness half, and it is the
SAME price the task path already pays for the same file; the alternative (a
curated chat-only excerpt) would be a SECOND source of truth about the project's
rules, which is the failure mode Rule 35 exists to prevent.

## Rule 37 (GOPI.md, after Rule 36 — the 16–21 gap is untouched)

`## Rule 37: A Chat Does Not Commit — It Dispatches a Task, and Keeps Its Files in the
Session Directory` states the three routes by artifact type:

1. anything that must be COMMITTED → a TASK (`gopi task submit`), which gets a
   deterministic worktree of its own;
2. anything the operator asks to KEEP that need not be committed → this run's own
   session directory (`~/.gopi/sessions/<session-id>/`), never a tracked path;
3. a normal task's verification material (diffs, logs) → that task's session
   directory; only the report worth keeping is committed, in the task's worktree.

Plus the rejected alternative (a worktree for a chat: probabilistic writer, no
merge path claims a conversation's branch) and the `Check:` line. It is a
statement of an existing mechanism, not a new gate — the runtime half already
exists in `chat_write_boundary.go` + `internal/tools/chat_write_guard.go`, and the
rule names the same verbs through that module rather than typing them.

## Verification

- Build: `go build ./...` → exit 0; `go vet ./internal/agent/` → exit 0;
  `gofmt -l` on the three touched Go files → empty.
- Tests: `go test ./internal/agent/ -run 'TestChatProjectContext|TestChatPlainPath|TestChatOpsPath|TestChatPathsWithout'` → **6/6 PASS**.
- Red-proof (the acceptance's red-then-green): with both injections reverted to
  `""` (a `sed` on the two argument lines), `TestChatPlainPathCarriesProjectContext`,
  `TestChatOpsPathCarriesProjectContext` and `TestChatProjectContextComesFromTheLoader`
  FAIL while `TestChatPathsWithoutProjectContextAreUnchanged` still PASSes —
  removal is detected, and the no-file case is not affected either way. Restored
  → 6/6 PASS (evidence: the run's session dir `evidence/red-proof.txt`).
- Real binary, both paths, wire-level (a local SSE stub on `127.0.0.1`,
  `ANTHROPIC_BASE_URL` pointed at it, request bodies written to disk):
  - `--tools`: **2 system blocks** — 8 740 B chat/tool prompt, then **64 602 B
    project context with `cache_control: ephemeral`**, 18 tool defs, and the
    emitted block contains `## Rule 37`.
  - plain (`--once`, no `--tools`): **2 system blocks** — 4 567 B chat prompt,
    then the same 64 602 B project block; 0 tool defs.
  - a run started where no GOPI.md exists above the cwd (`/home/user/workspace/
    gopi-test-scratch`; the walk reaches `/`): `[gopi] no GOPI.md found — searched:
    …`, and the request carries **1 system block, 8 707 B** — byte-for-byte the
    behaviour before this change. Also pinned by
    `TestChatPathsWithoutProjectContextAreUnchanged`.
- `go test ./internal/testutil/ ./internal/standards/` → `ok` (the GOPI.md
  rule-file gates, the doc-class gates, `TestDocAnchorsResolveInNormativeDocs`:
  **237 pre-existing drift entries permitted, 0 new** after the `loop.go` edit was
  made line-count-neutral — the first draft shifted a normative doc's anchor into
  `devdoc/rsi-chat-files-a-row-recipe.md` and the gate caught it).
- `gopi doccheck` → see the task report.

## Not done / honest limits

- **The six `chat/*` branches were counted, not deleted** (6, per the epic's
  constraint) — the cleanup they need is its own task.
- **The interactive REPL path** (`runChatWithInput`, `gopi chat` with a tty) is
  covered by construction — it calls the same `ag.Chat`/`ag.ChatOps` the tests and
  the one-shot binary exercise — but this run did not drive a pty. The one-shot
  face (`chat_once.go`) runs the SAME REPL and is what was measured on the wire.
- **Cost is measured, not tuned**: a chat now pays ~16 K tokens of project rules
  per request. If that turns out to dominate a chat's cost, the fix is a
  deliberate, single-source decision (e.g. a chat-scoped section of the same file)
  — NOT a second excerpt maintained beside it.
