<!-- doc-class: historical -->
# Requirements 09/12 — Chat & Interactive UI

**Module B9 of the reverse-requirements series.** The direction is inverted: the built
system is the evidence, the user need is *inferred* from it, and the value of this
document is §6 — the gap between the inferred need and the code.

| | |
|---|---|
| **Branch / worktree** | original pass `feat/req-b9-chat-ui` @ `~/workspace/gopi-req-b9`; **verification pass** `docs/req-09-chat-ui-verified` @ `~/workspace/gopi-req-09chat` (Rule 6a) |
| **Baseline read** | master `bf4d1941` (original pass, 2026-09-17); **re-anchored to `812f9a60`** in the 2026-09-18 verification pass — see §Verification for the drift map |
| **Task row** | `01a0ab3e-c46a-7685-a7af-ae092ee122af` (source=llm — the dispatch briefing, **not** an operator row) |
| **Deliverable** | this file, and nothing else (`git diff --name-only master..HEAD`) |
| **Method** | `devdoc/requirements/00-method-and-standards.md` @ `a2d1af6d` (8 sections, tiered canon, honesty rules) |
| **Measured** | live reads of `~/.gopi/memory.db` (ledger `tasks`: 609 `source='human'`; `chats`: 104 rows on **this** host) + `~/.gopi/sessions/`, **2026-09-18**; every number re-measured at write time. **The `chats` table is per-host local state and is not replicated** — §6.5 names the store each figure came from, and the 2026-09-17 figures were read from the other host's store |
| **Citations** | re-read line by line before commit; the 2026-09-18 passes re-anchored in-tree anchors **by symbol** (never by line offset). The no-basename-fallback resolver now reports **104 distinct `file:line` citations, 104 OK, 0 BAD** (§Verification, end of file) |

## Boundary

**In scope**

- The interactive conversation surface: the `gopi chat` REPL, its input path (the
  raw-mode line editor and the cooked fallback), its streaming renderer and status
  line, its slash-command routing, and its help text.
- The **durable thread**: the chat turn engine (the simplified no-tools path and the
  tool-enabled path), the one-session-per-conversation rule, and the chat index that
  records conversations (`chats` table) including its `list` / `backfill` subcommands.
- The line editor *as the operator's text-entry surface* — i.e. exactly the behaviours
  the chat REPL depends on (raw-mode entry/exit, history recall, paste-burst, the
  reserved answer band, the completion menu). Its **library** concerns (terminal
  restore policy, decoder completeness, render determinism) belong to B8, which owns
  the CLI surface; I state below where I draw that line and why.

**Out of scope**

- The CLI cockpit as a whole (verb taxonomy, help layout, flag conventions) → **B8**.
- The agent loop's liveness and the task ledger's semantics → **B6** / **B1**. Chat
  *touches* both (it borrows the loop's turn machinery and writes an index row) but
  owns neither.
- The browser carrier `gopi web` → the web UI is a **second carrier** of the same
  need. It is cited as evidence and as a parity obligation, never analysed as this
  module's subject. (Its code is `cmd/agent/cli_web.go`, which lives with B8.)
- The knowledge base, tools and routing → **B5** / **B7**.
- Code, tests or formatting changes of any kind (documents-only module).

**Boundary why — this is a responsibility, not a directory, and the directory version
would lose the need.**

The code for one responsibility is split across three homes: the REPL and its renderer
live in `cmd/agent` (`cmd/agent/cli_chat.go` 633 lines, `cmd/agent/chat_stream.go` 275,
`cmd/agent/lineedit_wire.go` 370), the turn engine lives in `internal/agent`
(`internal/agent/chat_ops.go` 422, `internal/agent/chatregister.go` 117), and the
text-entry machine lives in `internal/lineedit` (8,746 lines across 26 files, 57 tests —
all measured 2026-09-18). A directory-shaped boundary would put the first two in **B6/B8** (the agent
loop, the CLI) and the third in **B8**, and the *need* — "I can talk to it and keep the
thread" — would appear in no document at all.

The line is drawn at **who the behaviour is for**, not at where the code sits:

- `internal/lineedit` is **in scope only for the chat surface** — the editor exists
  because a cooked line reader turns a pasted block into N model turns
  (`internal/lineedit/doc.go:1-13`, "the defect that motivated the work"). Whether the
  editor's terminal-restore policy is correct is a B8 question; whether a pasted block
  stays one message is a B9 question, and it is the one the dependency exists for.
- `cmd/agent/cli_web.go` is **out of scope as code and in scope as parity**: it is the
  same need on a second carrier, and §6 compares the two because the operator's
  complaints (`01a0aa8b-30d2-7176-be33-169def32c0a4`, `01a0a4c7-bff8-725f-a603-faadc4878d5b`)
  and the roadmap both treat CLI and web as one front-end workstream
  (`devdoc/gui-chat-roadmap.md:1-20`).

**The sibling fix has LANDED since the first pass — a correction, not a nuance.** The
one-commit fix for `/help <cmd>` (branch `fix/chat-help-command-arg`, commit `f8cca331`,
2026-09-17) was **not** an ancestor of master when this document was first written; it is
now — measured 2026-09-18: `git merge-base --is-ancestor f8cca331 812f9a60` → true, merged
by `77fac9ac`. The sentence that once stood here ("master's REPL still ignores the
argument") is therefore **retired**: `cmd/agent/cli_chat.go:506-518` answers `/help <cmd>`
through `chatHelpText` (`cmd/agent/commands.go:740`, whose own comment reads *"Before this,
the REPL ignored the argument and dumped the whole table"*), and it shipped with four
tests of its own (`cmd/agent/cli_chat_test.go`: `TestChatHelpSwitchRoutesThroughTheArgument`,
`TestChatHelpText_TakesTheCommandArgument`, `TestChatHelpText_AgreesWithSharedDispatch`,
`TestRunChat_HelpReturnsZero` — added in the same commit). Every citation below is
re-anchored to **master
`812f9a60`** and was read from this document's own worktree; no other task's worktree was
entered (Rule 29).

---

## 1. Purpose (product-owner language)

**The need.** I want to *talk* to my agent, not only dispatch to it. I want to sit at
the terminal I am already in, say something, watch the answer arrive as it is written,
say the next thing without the thread being lost, and — if the work is real — let the
agent use its hands inside the same conversation instead of asking me to file a task.
The conversation must be a **thing**: I can leave it, come back to it, find it in a
list, and read it later. And nothing about the surface may destroy what I typed: a
block I paste is one message, not ten turns.

**The observable.** After a session I can see the conversation where I left it; a chat I
have finished is visibly finished and one I abandoned is visibly not; and a paste of N
lines produces one reply.

**[inferred]** — nothing in the evidence canon states this need as a requirement. It is
reconstructed from nine operator-typed asks and one research brief (§4), and its weakest
link is named there and in §8.

---

## 2. Who is served

| actor | the need this module serves | evidence |
|---|---|---|
| **The operator** (nick) | an interactive counterparty for work that is a *conversation*, not a filed job — and a durable record of it, so asking again does not mean re-explaining | verbatim: *"gopi project is a command line tool without chat mode like claude code or hermes"* (`93fa7eb4-3d12-4b0e-9687-9fba2a1a08c9`); *"we have struggled to have a nice beautiful cli UI for competing hermes/claude which can be very hard"* (`01a0a4f2-6166-7401-979d-bed9792454d9`) |
| **The agent** (the conversational counterparty) | a turn shape in which it is *not* asked to file a task, run a checklist or answer to a ledger — the conversation is the product | `internal/agent/loop.go:842` (`chatSystemPrompt`: "Do not mention tools, task lists, or system internals unless asked"); `internal/agent/loop.go:3420-3428` (no `[EPIC GOAL]` framing, no task-ledger registration) |
| **The operator, later** (the returning reader) | a chat index he can list and search: which conversations exist, which are still running, what they were about | operator's own question *"where can we retrieve task briefing?"* (`t-1788727187`) shows the appetite for retrieving a past thread; `cmd/agent/cli_chat.go:119-134` (`gopi chat list` usage) is the answer built for it |
| **The harness** (Bond, non-interactive) | the same turn must work with piped input, unchanged, so a test or a script drives the exact path a human does | `cmd/agent/cli_chat.go:363` + `cmd/agent/lineedit_wire.go:158`: the editor is entered only on a real terminal; `testsuite/chat_test.go` pipes stdin and is load-bearing (`01a0a97e-e39c-7d9b-9e59-b36061da7ad1`) |
| **The second carrier** (`gopi web`) | the same conversation need on a browser, sharing one slash-command table and one index | `cmd/agent/cli_chat.go:505-513` (shared command table); `internal/tools/chat.go:38-44` (`mode` is `chat` or `web`); `01a0a97e-e3c0-7a4f-b07b-e5c8d6835cdd` |

Not served by this module: a **team**. Every operator row in the canon is one person on
one machine — this is C5's world (§6), and it is the reason the surface may stay a
terminal rather than needing a deployment.

---

## 3. The workflow it sits in

```
 ENTRY                                              EXIT
 ─────                                              ────
 operator: `gopi chat`          (TTY)               /quit | EOF (Ctrl+D) | SIGINT/SIGTERM
 harness : printf ... | gopi chat  (pipe)           a turn error (exit 1)
 optional seed: raw-log path or session id          ─────────────────────────────
        │                                                     │
        ▼                                                     ▼
 ┌────────────────────────────────────────────────┐   finishChat: ONE session_end
 │ per turn                                       │   + the chat row is stamped
 │  1. read a line  ── editor (TTY) or cooked     │   (ended_at, end_reason)
 │  2. if it starts with "/" → LOCAL command      │
 │     (never a model turn)                       │
 │  3. otherwise → ONE agent turn                 │
 │       ├ plain  : Agent.Chat    (no tools)      │
 │       └ --tools: Agent.ChatOps (full toolset)  │
 │  4. stream the answer; commit finished lines   │
 │  5. capture the accumulated history            │
 │  6. upsert the chat index row (turn/tokens)    │
 └────────────────────────────────────────────────┘
        │
        ▼
 durable thread: ONE session id + ONE session JSONL + ONE raw log per conversation
 index row in `chats`: mode / model / first_msg / turns / tokens / log pointer
```

**Entry conditions.** `gopi chat` is dispatched before the generic input path
(`cmd/agent/main.go:576`, `cmd/agent/cli_chat.go:73-77`). The REPL runs with a terminal or a
pipe; an optional positional argument seeds the conversation from a prior raw log
(`cmd/agent/cli_chat.go:290-309`, resolver at `cmd/agent/cli_resume.go:151`).

**Exit conditions.** `/quit` or `/exit` (`cmd/agent/cli_chat.go:485`); EOF — read as an
explicit flag, never as an empty line (`cmd/agent/cli_chat.go:459-469`); a signal, which
is *not* allowed to kill the process outright (`cmd/agent/cli_chat.go:263-266`,
`:333-348`); or a turn failure, which still finalizes (`cmd/agent/cli_chat.go:583-586`).

**Why the exit path is part of the requirement, not an implementation detail.** An
earlier defect had a signal kill the process mid-turn, so the end-of-run defer never ran
and the conversation stayed "in progress" forever. The operator filed it in his own
words: *"bug: chat-once/bare-prompt SIGTERM 杀进程不写 session_end → auto 行僵尸"*
(`t-1788552832`, `t-1788552835`, both `source=human`). A conversation that cannot be
closed is not durable; it is a leak.

---

## 4. Why it exists — the requirement per need, with citations

Each R-row is a **need** (product-owner language) plus its **test** (the observable),
then its evidence. Rows marked **[inferred]** rest on reconstruction, with the weakest
link named in the row and again in §8.

### R1 — An interactive chat must exist at all, next to `claude` and `hermes` **[inferred]**

**Need.** When I want to converse rather than dispatch, gopi gives me a conversational
mode instead of forcing every exchange through a filed task. **Test.** `gopi chat` starts
a multi-turn session; the second thing I say is answered with the first thing still in
context.

**Evidence.** The operator's own dispatch text, carried as a ledger row (the row is
`auto` because it was registered by a chat process, not typed as a job — the payload is
the verbatim prompt the operator sent to the agent): *"You are GO developer and current
gopi project is a command line tool without chat mode like claude code or hermes which
source is at ~/work/hermes-agent, or dsh with a web interface(TUI?)"*
(`93fa7eb4-3d12-4b0e-9687-9fba2a1a08c9`, harness summary, 2026-08-28). Then *"You are
requested to implement the chat mode by cli mode which is lightweight work based on this
report devdoc/gopi-chat-mode-effort.md"* (`e261e707-a369-481f-88fc-53cc58ec8596`,
2026-08-28). Both executed the same night: `e408008a` "feat(chat): add gopi chat
interactive REPL + OnText streaming callback" and `395e2a03` "simplified Agent.Chat path",
2026-08-29.

The most direct statement of what he wanted the surface *to be* is two weeks later, in a
research brief that reached the ledger: *"Nick wants to develop gopi's chat front-ends
('gopi chat' CLI + 'gopi web' browser UI) toward a practical, Hermes-like interactive
experience. This is NOT greenfield: both front-ends already exist and are functional. The
ask is a **practical, Go-native** plan — 'just a go effort', zero/minimal third-party
deps"* (`01a0a4cf-5be7-74da-9478-01a2df75944c`, 2026-09-15; delivered as
`devdoc/gui-chat-roadmap.md`, and the constraint it names is the one §8 grades as C1).
Note the shape of the demand: **quality relative to a reference**, stated after the surface
existed — not a requirement that it exist.

**Weakest link.** The originating rows are `source=auto` and their full payload is empty
— I can read only the harness summary. The *chain* (a research doc → an implementation
ask → a commit) is unambiguous, but the summary truncates the operator's sentence, so
the words "like claude code or hermes" carry the whole inference. If that summary is the
agent's paraphrase rather than his text, R1's framing degrades to "he asked for a chat
mode", which the commits still support.

### R2 — One conversation is one durable thread, not one thread per keystroke **[inferred]**

**Need.** A conversation has one identity: one log, one session, one place to read it
later — not a fresh session per exchange. **Test.** After three exchanges, exactly one
session file exists for the conversation, and it contains all three.

**Evidence.** Operator, verbatim: *"gopi in chat mode should have one single session id
and log file by appending content to log. and for task entry, it should also reflect its
status correctly"* (`2ee4ae9f-d150-46f1-94b8-787d668ccf0d`, 2026-08-28). And for the
browser carrier, the same demand: *"gopi web chat has similar issue as gopi chat mode
which is a cli such that multiple rounds creates multiple session and log, it should
mimic gopi chat mode to use single session and log file"*
(`547ec8d4-6a26-478d-bf6b-adb10fd8e18f`). The implementation is a structural argument,
not a counter: ONE agent for the whole session (`cmd/agent/cli_chat.go:318-325`,
`ag.PersistChatSession = true`), history carried forward
(`cmd/agent/cli_chat.go:588-589`), and ONE close (`cmd/agent/cli_chat.go:626`). The
intent comment says what breaks without it — a fresh
session id + log per turn was the pre-fix behaviour (`devdoc/chat-single-session.md:6-9`,
"one conversation appending to ONE session JSONL (and raw log), not a fresh session id +
log file per turn").

**Weakest link.** The phrase *"as gopi chat mode"* in `547ec8d4` is circular — it asks the
web carrier to copy the CLI, and the CLI's rule is itself only recorded in a task **report**
(`devdoc/chat-single-session.md`). If that report is the agent's design rather than the
operator's requirement, R2 is an artifact of one night's implementation.

### R3 — The agent must be able to work inside the conversation (ops chat) **[inferred]**

**Need.** When the thing I am asking for is real work — read that file, run that test,
fix that — the agent does it inside the conversation instead of telling me to file a task.
**Test.** With tools enabled, asking the agent to read a file returns the file's contents
and the tool actually ran.

**Evidence.** Operator, verbatim: *"previous chat mode development is good direction, now
we also want to add operation functionality just like hermes/claude etc."*
(`4529788f-827c-453f-8cf5-65fe65044673`, 2026-08-28; *"good direction"* is an explicit
approval of the first path). Implemented as `Agent.ChatOps`
(`internal/agent/chat_ops.go:205`) with the full toolset, a bounded round cap
(`internal/agent/chat_ops.go:37`, `maxChatOpsToolRounds = 40`) and one continuation nudge
when a tool turn ends degenerate (`internal/agent/chat_ops.go:27-31`). Live evidence of the LLM path: `testsuite/chat_test.go`
`TestChat_ToolsToggle_ToolExecutes`.

**Weakest link.** The operator's sentence is the *only* statement of this need, and it
arrived as part of an auto-registered chat row. The default is **off**
(`cmd/agent/cli_chat.go:249-253`) and the flag is per invocation — so if the appetite were strong, the
row-to-flag ratio would say so; §6.1 measures it and finds the opposite.

### R4 — A pasted block is ONE message, never N turns

**Need.** When I paste a block of text into the chat, it goes as one message; my input is
never silently split into several model turns. **Test.** Paste a three-line block; exactly
one reply comes back, and the message the model saw has three lines.

**Evidence — this is the one requirement with a *decided constraint* behind it, at three
independent tiers.** (a) The operator's words, verbatim: *"we have struggled to have a
nice beautiful cli UI for competing hermes/claude which can be very hard"*
(`01a0a4f2-6166-7401-979d-bed9792454d9`, and the same text re-filed as
`01a0a981-2876-7ea0-8ab7-4d8b409b48ca`). (b) The pre-fix mechanism and its consequence are
recorded in a dispatch the operator's row ordered: cooked `bufio` input meant
*"a pasted N-line block becomes N separate model turns (the feeder emits one lineRead per
'\n')"* (`01a0a97e-e39c-7d9b-9e59-b36061da7ad1`). (c) The requirement is written down as an
**acceptance check** in the roadmap: *"a 3-line paste is **one** turn with a 3-line user
message (`-run TestChatPasteBurst`). This one closes a real defect, not a polish item"* —
the roadmap's shorthand resolves to the real test
`TestChatPasteBurst_ThreeLinePasteIsOneMessage` (`cmd/agent/lineedit_wire_test.go:321`)
(`devdoc/gui-chat-roadmap.md:399-400`, M1.3; the increment row is `:333`) — and it is pinned by
`testsuite/chat_pty_test.go` `TestChatPTY_PasteBurstIsOneTurn`.

**Weakest link.** (c) is authored, not operator-stated (`devdoc/gui-chat-roadmap.md`
was written by the agent, commit `d54c9822`) — but the *mechanism* in (b) is a property of
the code and the consequence is checkable, which is what makes this row the strongest
non-verbatim requirement in the document.

### R5 — The conversation is durable and findable afterwards

**Need.** I can come back to a conversation: find it in a list, see what it was about,
see whether it is still running, and read its full text. **Test.** After a session,
`gopi chat list` shows it with its first message and turn count; its log exists at the
recorded path; an unfinished one is not presented as finished.

**Evidence.** Operator, verbatim (the same row as R2, second half): *"and for task entry,
it should also reflect its status correctly which involves database task entry
creation/update/close"* (`2ee4ae9f-d150-46f1-94b8-787d668ccf0d`). The index was built on
that: `chats` table (`internal/tools/chat.go:77-98`), the CLI face `gopi chat list`
(`cmd/agent/cli_chat.go:119-134`), and a migration for the pre-split rows
(`internal/tools/chat.go:438`). The retrieval question was asked of the *task ledger*
independently: *"where can we retrieve task briefing?"* (`t-1788727187`, `source=human`) —
the operator's recurring question is "where do I find it again".

**Weakest link.** "status correctly" is the operator's phrase and it is ambiguous — §6.4
records that no one ever defined what the correct state of a *conversation* is, and the
index today reports six finished conversations as running (§6.5-C).

### R6 — Ctrl+C must not lose the conversation

**Need.** Interrupting the chat does not lose it: the thread is closed cleanly and
appears as interrupted, never as a live session that will never come back. **Test.** Send
SIGTERM mid-turn; afterwards the session has exactly one `session_end` and the index row
is stamped.

**Evidence.** Operator, verbatim: *"bug: chat-once/bare-prompt SIGTERM 杀进程不写
session_end → auto 行僵尸"* (`t-1788552832`) with the mechanism spelled out —
*"defer 只在 runEngine 正常返回执行"* and *"auto 行永久 in_progress（手动 cancel 清理）"*.
Fixed by intercepting the signal and unwinding the turn instead of dying
(`cmd/agent/cli_chat.go:333-348`, `:346` (`ag.Interrupt()`); commit `6d2e8577` "fix(chat):
SIGINT/SIGTERM graceful closeout"), with exactly one `session_end` written
(`cmd/agent/cli_chat.go:626`).

**Weakest link.** The dossier bug was filed against `chat-once`/bare-prompt (the
one-shot path) and the fix was applied to the chat REPL by analogy. The chat REPL's own
mid-turn kill is asserted by the code path, not by an operator observation of *chat*
losing a thread.

### R7 — A command I type is not a model turn **[inferred]**

**Need.** Typing `/quit`, `/clear`, `/tools`, `/help` or any `/`-command does what it
says without spending a model turn on it; changing the session's own state (clear, toggle
tools) works in the REPL it belongs to. **Test.** `/help` prints the help and no request
is logged for it.

**Evidence.** The operator's first chat ask included the interaction vocabulary (the mode
was built with `/quit` `/exit` `/clear` `/help` from `devdoc/chat-mode-report.md:30-31`),
and the shared table's existence is explained by its own comment — *"executed locally,
never a model turn"* (`cmd/agent/cli_chat.go:507-518`), with the four session-state
commands kept REPL-native because the shared table cannot see the session
(`cmd/agent/cli_chat.go:476-528`).

**Weakest link.** I found no operator row that asks for slash commands as such; the
vocabulary appears in a *report*. The inference rests on the observed shape of the
research doc (`devdoc/gopi-chat-mode-effort.md`, which lists the commands) plus the fact
that the operator used the mode and never filed a complaint about it.

### R8 — The surface must degrade honestly and never break the piped path **[inferred]**

**Need.** When the terminal cannot host the full experience (piped stdin, no TTY, raw
mode refused, a window too small), the chat still works and says so, instead of drawing
garbage or failing. A script or test that pipes input gets the same behaviour it always
did. **Test.** `--no-editor`, a pipe, and a too-small terminal each produce a working
turn and a printed notice about the degradation.

**Evidence.** The operator's stated preference is a *nicer* UI (*"a nice beautiful cli
UI"*, `01a0a4f2-...`), and the two sibling rows make the degradation a requirement rather
than a nicety: the cooked path is byte-identical when stdin is not a TTY, and the notice
is mandatory — `cmd/agent/lineedit_wire.go:16-20` names the two modes so the branch reads
as a decision; `cmd/agent/cli_chat.go:363-371` prints which mode is active and the file's own
comment names the reason ("the user is told, always", `cmd/agent/lineedit_wire.go:47-49`, Rule 15) — the correct landing spot, at the IO struct rather than the buggy site;
`testsuite/chat_test.go` pipes stdin and is described as load-bearing
(`01a0a97e-e39c-7d9b-9e59-b36061da7ad1`, "A4 — cooked fallback … testsuite/chat_test.go
pipes stdin and is load-bearing").

**Weakest link.** The notice-on-degradation rule is inferred from a *project* rule
(GOPI.md Rule 15, no silent failure) rather than from an operator sentence about chat.
The operator never wrote "tell me when the editor is off".

### R9 — A tool error inside a turn must not kill the conversation **[inferred]**

**Need.** When a tool call fails inside an ops turn, the agent gets the error and
continues the conversation; the chat does not end because one tool failed. **Test.** Ask
for something that makes a tool fail; the reply describes the failure and the session is
still alive.

**Evidence.** The ops path executes tool batches through the loop's own machinery, whose
contract is that a tool error is returned to the model as a result, not raised
(`internal/agent/chat_ops.go:1-9` — it "reuses the loop's private machinery
(`chatOnce`, `executeToolBatch`, `trimContext`, `withTick`)"); the tool-event callback is
described as *"pure display"* and a nil callback still executes the call
(`internal/agent/chat_ops.go:62-65`). The end-to-end arms exist: `testsuite/chat_test.go`
`TestChat_ErrorScenario_TurnFailure`, `TestChat_ToolsToggle_ToolExecutes`.

**Weakest link.** The strongest natural form of this need — "Ctrl+C cancels the turn and
keeps the session" — is **not** implemented; that is §6.3 gap C. R9 as written describes
what *is* served (a tool error is contained); the interrupt variant is the unmet half.

### 4a. The operator's questions, each checked against the current binary

Each row: a question an operator would actually ask, the **exact command** that answers it
today, or an explicit **NO** (the NOs become §6 findings). Line anchors in the
right-hand column are for the mechanism, not for the answer.

| question | answer today | anchor |
|---|---|---|
| "Start talking to it." | **YES** — `gopi chat` (TTY) or `printf 'hi\n/quit\n' \| gopi chat` | `cmd/agent/cli_chat.go:256-258` |
| "Where is my history (up-arrow)?" | **YES** — a per-home history file, loaded on editor entry | `cmd/agent/lineedit_wire.go:162`; `internal/config/gopihome.go:36` (`chat_history`) |
| "Did my paste go as one message?" | **YES** — paste-burst / bracketed paste hold the submit | `internal/lineedit/doc.go:1-13`; `testsuite/chat_pty_test.go` `TestChatPTY_PasteBurstIsOneTurn` |
| "Let it actually DO things." | **YES** — `gopi chat --tools`, or `/tools` mid-session | `cmd/agent/cli_chat.go:276-277`, `:498-505` |
| "Start fresh without leaving." | **YES** — `/clear` or `/new` | `cmd/agent/cli_chat.go:488-497` |
| "List my past conversations." | **YES** — `gopi chat list [query] [--limit N] [--model M]` | `cmd/agent/cli_chat.go:136-232`, `internal/tools/chat.go:324` |
| "Read a conversation's full text later." | **PARTIAL** — only if the invocation was one that filled the log pointer: 37 of 116 rows have one, 79 do not (HP store, 2026-09-17); **95 of 104** have one on this host's store, 2026-09-18 (§6.5-F) | `internal/tools/chat.go:254-283`; measured §6.5-A |
| "Show me the turns of a conversation I finished an hour ago — did it close cleanly?" | **NO** — those rows were written with no log pointer, no finish stamp and a fabricated start time | measured §6.5-A; `internal/tools/chat.go:584` |
| "Is this conversation still alive?" | **NO** — the index reports 85 of 116 as running (20 of 104 on this host, §6.5-F); among rows whose log exists, six finished conversations are shown as running | measured §6.5-B/D; `internal/tools/chat.go:379-390` |
| "Stop a long answer without losing the conversation." | **NO** — Ctrl+C is a session-level interrupt; there is no turn-level cancel that keeps the session | `cmd/agent/cli_chat.go:333-348`, `:431-441`; §6.2-G3 |
| "Resume a past conversation and continue it." | **PARTIAL** — `gopi chat <raw-log-path\|session-id>` seeds a new session from a checkpoint (a new session id, not a continuation of the old thread) | `cmd/agent/cli_chat.go:290-309`, `cmd/agent/cli_resume.go:151` |
| "Find a conversation by what was said in it." | **NO** — the search covers the first message and the title; the full text lives only in the per-session log | `internal/tools/chat.go:340`; §6.3-B |
| "Name this conversation." | **NO** — `title` exists in the schema, is offered by the list, and no writer ever sets it (measured: 0 of 116 non-empty) | `internal/tools/chat.go:82`, `:372-374`; measured §6.4 |

---

## 5. Acceptance in user terms

Written so a test author can separate a bug from allowed behaviour, and so the operator
can accept or reject each line as his own.

| # | statement | how he personally verifies it |
|---|---|---|
| A1 | I start talking to it in one command, in the terminal I am already in. | `gopi chat` gives a prompt and answers. |
| A2 | The thread survives from one thing I say to the next. | Tell it a codeword, ask for the codeword back — it answers with the codeword. |
| A3 | A block I paste is one thing I said, not ten. | Paste three lines; exactly one answer comes back. |
| A4 | I can ask it to *do* something, not just answer. | With tools enabled: "read that file" returns the file's contents. |
| A5 | One conversation is one record I can read later — in one piece. | After the session there is one log for it containing every exchange, and it is listed. |
| A6 | If I stop it, the record says it was stopped, not that it is still going. | Kill it mid-turn; the conversation is marked interrupted, not running. |
| A7 | A `/`-command is carried out, not sent to the model. | `/help` prints help; `/clear` forgets the thread; no model request is logged for either. |
| A8 | If my terminal cannot offer the nice input, the chat still works and tells me why. | Pipe input, or use the no-editor flag: the turn works and a notice is printed. |
| A9 | Two surfaces show me the same conversations. | A session started in the browser appears in the same list as one started in the terminal. |

**A9 is the acceptance rule that exposes the most.** Both carriers write the same index,
share the same command table and the same turn engine, and the operator treats them as one
front end (`devdoc/gui-chat-roadmap.md:23-30`; `internal/tools/chat.go:80`). Today the
shared index is **one index with two different qualities** — the browser rows carry a log
pointer and a clean finish; most terminal rows carry neither (§6.5-A). A9 is therefore
**not met**.

---

## 6. Gap analysis — code against need

**How to read this section.** §6.1–§6.4 are the four mandatory surfaces. §6.5 is the
live re-measurement that several rows above are graded against. §6.6 is the one finding
the operator does not already know. §6.7 records where the inferred need has **no
decision behind it at all** — the surface the method ranks highest.

### 6.1 Serves

| served | evidence | note |
|---|---|---|
| The chat exists, multi-turn, streaming to stdout as it is written | `cmd/agent/cli_chat.go:268`; `e408008a`, `395e2a03` (2026-08-29) | the one need with a same-week verbatim ask → design → commit chain |
| One conversation = one session id + one JSONL + one raw log | `cmd/agent/cli_chat.go:318-325`, `:588-589`, `:626`; `devdoc/chat-single-session.md:6-9` | serves R2 |
| Ops chat inside the conversation, with the full toolset | `internal/agent/chat_ops.go:159`; `cmd/agent/cli_chat.go:274`, `:487` | serves R3 |
| **A pasted block is one message** | `internal/lineedit/doc.go:1-13`; `testsuite/chat_pty_test.go` `TestChatPTY_PasteBurstIsOneTurn`; acceptance written at `devdoc/gui-chat-roadmap.md:399-400` | serves R4 — the defect the whole editor dependency exists for |
| A conversation index exists: list/search/filter over first message, model, time span, tokens, end reason | `internal/tools/chat.go:324`; `cmd/agent/cli_chat.go:119-134`; `01a0a97e-...` (R4/R5 shared table) | serves R5 |
| Signals close the conversation instead of killing it — one `session_end`, one stamped row | `cmd/agent/cli_chat.go:335`, `:344`, `:425-429`, `:584-591`; `6d2e8577` (2026-09-05) | serves R6; verified in the live store for sessions after 2026-09-16 (§6.5-B) |
| Slash commands are local, never model turns; four session-state commands stay REPL-native | `cmd/agent/cli_chat.go:54-56`, `:463-471`, `:474-505` | serves R7 |
| The editor is entered **only** on a terminal; a pipe keeps the old path byte-for-byte, and the degradation is announced | `cmd/agent/lineedit_wire.go:158`; `cmd/agent/cli_chat.go:363-371`, `:431-437`; `testsuite/chat_test.go` | serves R8 |
| The legacy rows were not thrown away: a migration moves them into the index with a marker | `internal/tools/chat.go:438`; measured — 112 of 112 marked | serves R5's durability in the "do not lose history" direction |

### 6.2 Falls short

**G1 — 79 of 116 conversations were recorded from a summary string, not from a session,
and every one of them is unreadable, unfinished and mis-started.** *(The 116-row store is
the one the first pass measured — the HP store. On **this** host's store the same artifact
is **9 of 104** rows, 2026-09-18; the cohort is smaller because 70 rows gained a pointer
and an ending in a later backfill. The mechanism below is identical on both — §6.5-F.)*

The migration that moved the pre-split chat rows into the index reads four facts from the
session JSONL and falls back to the ledger row when the file cannot be read
(`internal/tools/chat.go:516-534`, `:545-600`). Measured on the live store, 79 of the 116
index rows have **no log pointer at all**, and their start time is not a time — it is the
moment the migration ran:

- 79 rows, `log_path = ''`, `turn_count = 0`;
- every one of them carries the ledger row's **summary** as its "first message", including
  the runtime telemetry the ledger appends (`"… — elapsed=218.3s iter=4 tokens=3294"`);
- their start times collapse onto a single moment: **two distinct values, 2026-08-30
  14:37:45 and 14:37:46**;
- the code names this itself: `startedAt = float64(time.Now().Unix()) // last resort — the
  migration moment` (`internal/tools/chat.go:584`).

The ledger rows they came from **still exist with the real timestamps** — I verified the
join: 110 of the 116 index rows match a `tasks` row by session id, and for
`97ddb03f-…` the ledger's `created_at` is 2026-08-29 22:18:56 while the index row (backfilled
in the same pass, because it has a pointer) says 2026-08-30 14:21:18. So the ledger holds
the truth the index lost.

**Why the pointer is missing — a stale session lookup, not lost data.**
`chatBackfillRecord` keeps the ledger's summary-derived first message **even when the
session file was read successfully**, and when it was not readable it inserts with
`rec.LogPath` empty (`internal/tools/chat.go:516-534`, `:585-591`). The lookup is
`chatFindSessionJSONL`, which matches a **flat** `<id>.jsonl` at the sessions root
(`internal/tools/chat.go:758-775`: `strings.TrimSuffix(name, ".jsonl") == id`). Measured:

- all **37** pointer rows point at a file that exists — **37 of 37** — so the live path is sound;
- **0 of the 79** pointer-less ids have a flat `<id>.jsonl` at `/home/user/.gopi/sessions/` today;
- but **66 of those 79** ids *do* exist on this machine under the task-session **directory**
  layout (`/home/user/.gopi/peerbox-mirror/sessions/<id>/<id>.jsonl`), which the flat-name matcher
  cannot see; the remaining **13** exist in neither layout here.

So the honest statement is **recoverable for most, unaddressed for all**: the text is not
gone, the *lookup* is stale — it cannot see the layout the sessions root has since adopted.
Grading: **falls short of R5, fails A5/A9, and directly contradicts the durability claim the
table's own doc comment makes** ("the JSONL remains the authoritative full text",
`internal/tools/chat.go:19-20`).

**G2 — "still running" is what a conversation says when nobody knows. The index reports
85 of its 116 conversations as running** *(20 of 104 on this host's store, 2026-09-18 —
§6.5-F; the display rule is identical)* **, and one of them closed cleanly with an exit code of 0.**

`ChatList` renders a row as `running` unless `ended_at` is set
(`internal/tools/chat.go:379-390`). The close hook accepts exactly one end reason and is
idempotent (`internal/agent/chatregister.go:98-105`; `internal/tools/chat.go:285-313`), so a conversation that never
reached the REPL's closeout is never stamped. Measured:

- 85 of 116 rows render as **running**: the **79** pointer-less rows (nothing can ever
  close them — they are permanently "running") plus **6** rows that do have a log;
- six pointer rows also render as running. Five of them have **zero `session_end` events**
  in their session file (checked one by one), which is the same fact the operator filed as
  a bug on 2026-09-04 (`t-1788552832`) and which was fixed on 2026-09-05 (`6d2e8577`) —
  so the display has not recovered from a defect that no longer occurs. The sixth,
  `97ddb03f-…`, is the sharp case: its log **does** end with `session_end` (`exit_code: 0`,
  `elapsed_sec: 1195.5`, last written 2026-08-30 22:41:14) and the index still says
  `running`. This one is a *living* gap rather than a historical scar: the live registration
  path **is** wired to write the pointer and `ended_at` — `ChatEnsureRow`'s upsert carries
  both (`internal/tools/chat.go:231-243`: `log_path`, `started_at`) and `ChatCloseSession`
  stamps `ended_at` unconditionally (`:303-305`) and is idempotent by design (`:307-311`,
  "one conversation ends once", called by `internal/agent/chatregister.go:98-105`). So the
  machinery that would have rescued this row exists and is exercised by every chat run
  today. That is the reason to read the 79 pointer-less rows as **residue** rather than a
  live defect — but also the reason the display rule still misleads: the row is stamped only
  if the run reaches its close hook, and nothing in the index detects the case where the log
  says "finished" and the row says otherwise.
- the 31 rows that *are* stamped with `ended_at` are **all** pointer rows — not one
  pointer-less row has ever been closed by any path;
- the reserved third reason, `crash` (`internal/tools/chat.go:51`, "killed process (no
  clean close ran)"), is a **declared constant with no writer**: the only place the string
  appears in production code is the validation switch that accepts it
  (`internal/tools/chat.go:290`). Measured: 0 rows, and no code path can produce one.

Grading: **falls short of R5's "does not present an unfinished one as finished" and of the
operator's "it should also reflect its status correctly"** — and it is the *inverse* of the
defect he filed: previously a dead session looked live forever; today a finished one still
does.

**G3 — everything typed while a turn runs is dropped.** The REPL reads the next line only
after the turn returns (`cmd/agent/cli_chat.go:405-429`; the turn runs at
`:534-557`), and the input feeder blocks on a one-slot channel by design
(`cmd/agent/cli_chat.go:405-429` — `lineCh <- lineRead{line: line, eof: !ok}` at `:424`,
and the comment says why a drop-on-full select would lose the passenger, the EOF marker). The roadmap calls this out for the terminal
(*"No typing while a turn runs — the feeder blocks on a 1-buffer channel, so at most one
line is held; anything else waits"*, `devdoc/gui-chat-roadmap.md:245-247`) and specifies
the discipline as A6, *"interrupt & typed-ahead discipline: Ctrl+C at the prompt (discard
the line) vs mid-turn (cancel the turn, keep the session)"* (`:336`), scheduled as M2.7
with the acceptance *"Ctrl+C mid-turn cancels the turn and writes exactly one
`session_end`"* (`:435-437`). **A6/M2.7 was never executed** — the two M2 CLI dispatches
that ran cover A5 and A7 only (`01a0a97e-e402-76ab-a25a-11f4eab4266b`, whose scope is
"implement M2 CLI increments A5/A7").

The status line already has a field for it — `queued string`
(`cmd/agent/chat_stream.go:86`, rendered at `cmd/agent/chat_stream.go:134-136`) — and **nothing in the repository
ever assigns it**: the only occurrences in the tree are that field and its two render lines, so the field
is structurally always empty. Contrast with the browser carrier, where the same problem
was solved and shipped: a queued frame is flushed before the lock is taken
(`cmd/agent/cli_web.go:966-970`, `:984`, `webEventQueued` at `:1182`, landed by `01a0a97e-e3c0-…`).
Grading: **falls short of R6/R9's interrupt half and of A6; the CLI is behind the web
carrier on a need both were told about.**

**G4 — the index's search cannot find a conversation by what was said in it.** `ChatList`
matches the query against the first message, the title and the session id
(`internal/tools/chat.go:340`), and the title column is never written (§6.4). The full text
lives in the per-session log and in no index — deliberate for the *tasks* ledger (the
retrieval split, `cmd/agent/cli_chat.go:114-118`: "task search never sees chats") but the
chat side is left with a search over a single derived string. The operator asked the
retrieval question twice in his own words (`t-1788727187`: *"where can we retrieve task
briefing?"*; `t-1788723950`: *"what is definition of request in chat"*). Grading: **falls
short of R5's "find it again"; no requirement R-row demands full-text chat search**, which
is why this is a gap and not a violation (§6.7).

**G5 — resuming a conversation does not resume the conversation.** The seed argument
loads a *checkpoint* from a raw log into a **new** session
(`cmd/agent/cli_chat.go:290-309`, `cmd/agent/cli_resume.go:151`): the model's context is
carried, the log is not — the new thread is a new row, and the old one stays open. There is
no "continue session X" in either carrier. Grading: **falls short of R2's "one thread"
read strictly; the operator never asked for it** (§6.7 records the absence of a decision,
not a defect).

**G6 — the two carriers share an index and disagree about what it means.** Same table,
same close hook, same command table — but the CLI writes rows without a log pointer when
it runs on a session that predates a migration, and the browser's rows are complete
(`internal/agent/chatregister.go:60-73` populates the pointer on every live run in both
carriers; the difference is entirely the rows the migration wrote). A9 is unmet for
history, not for behaviour going forward.

**G7 (new in the 2026-09-18b pass) — the index records *paths that do not describe the
files the system writes*, and every consumer of that column silently degrades.** This is
the sharpest form of the pointer defect and it is not a count, it is a mechanism.

Measured on peerbox's store (104 rows, 95 with a pointer), by the **shipped** resolver
(`chatFindSessionJSONL`, `internal/tools/chat.go:758-775`, exercised through a throwaway
in-package probe so no re-implementation stands between the claim and the code):

| predicate | value |
|---|---|
| `log_path` names an existing **file** (the column's own contract) | **20 of 95** |
| the *reader* resolves the id | **89 of 95** |
| of those 89, the hit is a **directory**, not a file | **89 of 89** |
| the reader resolves *nothing* | **6 of 95** (`0e3be7b0`, `9ed41e9d`, `93c98902`, `778bff45`, `d7488f0c`, `adf9eee4`) |
| rows for which `chatSessionFactsOf` returns a **non-nil but wholly empty** facts struct | **89** |
| rows for which it returns usable facts (turns / first message / model / start) | **0** |

**The mechanism, and why it is silent.** The sessions root has moved to a **per-session
directory** layout — `<sessions>/<id>/<id>.jsonl` (or `/session.jsonl`) — while
`chatFindSessionJSONL` does an `os.ReadDir` of the root and a **name** comparison:

```go
if strings.TrimSuffix(e.Name(), taskSessionJSONLExt) == id { return filepath.Join(dir, e.Name()) }
```

A **directory** named `<id>` trims to `<id>` exactly as a file named `<id>.jsonl` would, so
the match succeeds and the function returns a path to a directory. `chatSessionFactsOf`
then does `os.Open(path)` — which **succeeds on a directory** on Linux — and scans it with
a `bufio.Scanner`: `Scan()` returns false immediately, `sc.Err()` is **never checked**
(`grep -n 'sc.Err()' internal/tools/chat.go` → no hits, Rule 15b), and the function returns
a non-nil struct initialized only with `LogPath`. Callers treat non-nil as success
(`internal/tools/chat.go:516-534`: `if facts := chatSessionFactsOf(sessionID); facts != nil`),
so a directory pointer is indistinguishable from a real session whose log happens to be
empty. **Nothing logs, nothing errors, and the row looks healthy.**

**Why this is more than a historical artifact — measured, on a COPY, never the live
store.** Pointing a fresh `gopi chat backfill` at a copy of the live ledger (isolated
`GOPI_HOME`, `memory.db` copied to scratch; the operator's store read-only throughout):

| | copy before | copy after a fresh `gopi chat backfill` |
|---|---|---|
| `chats` rows | 104 | **259** |
| pointers naming a **directory** | 0 | **106** |
| rows carrying the `— chat: backfilled` marker | 83 | **238** |
| directory-pointer rows with `ended_at` set | — | **0 of 106** |

So the defect is not repaired by re-running the migration — **it is manufactured at scale
by it**, because a re-run considers every unmarked `[auto]` row a fresh candidate and the
new layout makes almost every lookup land on a directory. The 83-row marker count and the
106 fresh directory pointers are one phenomenon: the migration is re-runnable, and each run
adds rows it cannot read, cannot close, and cannot tell apart from healthy ones.

**The row's own filename survives the confusion.** For `663d74a7-…` the recorded path
(`/home/user/.gopi/sessions/663d74a7-….jsonl`) does not exist, but the *content* does:
`<id>/<id>.jsonl` is present with mtime **2026-08-28 22:43:49**, while the directory entry
itself was materialized **2026-09-09 08:56:29** — the layout change is later than the log.
The text was never lost; the index simply stopped being able to name it.

Grading: **falls short of R5 and fails A5/A9 in a second, sharper way than G1** — G1 is
about rows that never had a pointer, G7 is about the pointers that *do* exist pointing at
the wrong kind of object, with the failure swallowed. The fix is small (require
`!e.IsDir()`, or descend into `<id>/`, and check `sc.Err()`), which is why the finding is
the undetected silence rather than the size of the repair.

### 6.3 Over-builds

Judged against the needs of §4 and against C5. An over-build is not a bug; it is machinery
whose existence no requirement in this document asks for.

| over-build | evidence | why no requirement asks for it |
|---|---|---|
| **The whole `backfill` subcommand.** | `cmd/agent/cli_chat.go:113-116`, `:194-202`; `internal/tools/chat.go:438-511` | It exists to move 79 heritage rows into an index that nothing in the product requires to be complete, and it manufactures false facts doing it (§6.2-G1). A one-shot data migration shipped as a permanent **user-facing verb** is surface the operator must now hold in his head (C5). |
| **A title column with no writer.** | `internal/tools/chat.go:82`; offered by search (`:340`) and by the list head (`:372-374`); measured 0 of 116 non-empty | The doc comment is explicit that the generator is deferred — *"title is reserved (LLM session titles — generator deferred; the column exists so the schema does not churn when it lands)"* (`internal/tools/chat.go:21-23`). Defensible as schema discipline; it is still a searchable column that can never match, and a `head := title` line that always falls through. |
| **A run surface inside the chat surface.** | `cmd/agent/cli_chat.go:18-22` (the usage head names a seed argument), `cmd/agent/cli_chat.go:136-232` (`list`, `backfill`); `internal/tools/chat.go:77-98` (index + FTS + trigram + triggers) | Retrieval and indexing belong to the observability surface (**B10**). Their presence here is what makes the chat module's own boundary hard to state, and it is why §Boundary had to argue about directories. |
| **The reserved `crash` end reason.** | `internal/tools/chat.go:51`, accepted at `:290`, written nowhere | A vocabulary of three states when the system can produce two. |

**Not an over-build, though it looks like one:** the zero-dependency line editor (8,746
lines across 26 files for the text-entry surface alone; 8,023 at the first pass). It is over-sized for "type a
line", and it is exactly the size the *need* demands — the operator's complaint is about
input quality against two reference products (*"we have struggled to have a nice beautiful
cli UI for competing hermes/claude"*, `01a0a4f2-...`), and the alternative was a dependency
the box cannot fetch (`devdoc/gui-chat-roadmap.md:273-297`, measured: `golang.org/x/term`
is in neither `go.mod` nor the module cache). Under C1 this is the *only* admissible shape,
so it is a cost, not a gap.

### 6.4 Never decided

This is the surface the method ranks above the others: an accident of history, not a
decision. Three, in descending order of consequence.

**ND1 — nobody ever decided that chat is a requirement the operator holds.**

The canon contains the chat mode's **genesis** in one hand and **no requirement statement**
in the other. What exists:

- a row asking for it, phrased as a comparison with two other products: *"gopi project is a
  command line tool without chat mode like claude code or hermes"*
  (`93fa7eb4-3d12-4b0e-9687-9fba2a1a08c9`, `source=auto` — registered by a chat process, so
  its payload is empty and only the harness summary survives);
- an instruction to build the lightweight option from a research doc
  (`e261e707-a369-481f-88fc-53cc58ec8596`);
- an approval of the direction, *"previous chat mode development is good direction"*
  (`4529788f-827c-453f-8cf5-65fe65044673`);
- and one hand-over of the surface question: *"the question remains. is this cli mode enough
  for such complex UI?"* (`d4583a45-0cbf-4096-95ef-d99dfc2b66fc`, pointing at the roadmap
  hook).

What does **not** exist anywhere I can find: a row, briefing, rule or document stating that
the operator *requires* an interactive chat surface — as opposed to "we discussed it, an
agent built it in a night, it worked, we kept it". Every recorded ask about the front end
is **quality-relative**: *"like claude code or hermes"* (the genesis row), *"toward a
practical, Hermes-like interactive experience"* (`01a0a4cf-5be7-74da-9478-01a2df75944c`,
2026-09-15), *"just a go effort"*. Not one of them says "we need chat", and not one of them
was written before the surface existed. The strongest evidence that the appetite
is thin is usage, measured: **32 terminal conversations in 18 days**, essentially all
two-turn smoke exchanges (**28 of 32 are two-turn**; the four one-turn rows total zero
tokens; the turn-count distribution is `0 → 80, 1 → 2, 2 → 30`), while the browser carrier
— described by the operator himself as the *earlier* decision (*"we later decide to switch
to web browser based GUI which solves almost most of issue"*, `01a0a4f2-6166-7401-979d-bed9792454d9`)
— shows one conversation of 12 turns and one of 7 in 4 rows.

**The finding, and it outranks every inference in §4:** *the demand on record is about
quality, never about existence.* Six wall-clicking days (A1→A7 across two rounds, plus
rework, plus a re-run of the web half: rows `01a0a946-*` cancelled, re-filed as
`01a0a97e-*`) were spent making one front end match two reference products, while no row
anywhere states that gopi must have a conversation surface at all. R1 and R2 are
inferences from a genesis chain; this is the absence of the thing they infer, and the
*kind* of ask that exists in its place — "make it as good as hermes" — presupposes rather
than states the requirement. Both readings remain live and the code does not decide between
them, which is why §8 grades R1's confidence low and puts the question at the top of
§Unknowns.

**ND2 — nobody decided what a conversation's "correct status" is.**

The operator asked for it in the same breath as the single-thread rule — *"and for task
entry, it should also reflect its status correctly which involves database task entry
creation/update/close"* (`2ee4ae9f-d150-46f1-94b8-787d668ccf0d`). What was built is a
three-word vocabulary with one word reserved and no writer (`normal` / `interrupt` /
`crash`, §6.3) and a display rule of "running unless ended" (§6.2-G2). What was **never
decided**: whether a conversation *has* an operational state at all. The schema comment
says it does not — *"All columns are FACTS (no state machine): a conversation is born
running and ends once"* (`internal/tools/chat.go:18-22`) — while the same table carries a
state-like column the operator experiences as "still running" (85 of 116 rows, §6.5-B).
**The schema says "facts, not a state machine"; the UI shows a state.** That inconsistency
is the un-decided thing, and it is why G2 cannot be dismissed as a display bug: nobody
decided what the display should say, because nobody decided whether a conversation is ever
"open".

**ND3 — the requirement the code was built to (a "proper CLI, not a TUI") was never
renewed after it was abandoned.**

The founding study's conclusion was a ceiling — *"完全复刻 hermes 级 TUI … 约 3–4 周 …
那属于'完整产品'，超出本次'chat 模式'的核心范畴"* (`devdoc/gopi-chat-mode-effort.md:26-29`)
— and its "方案 B / TUI" path was handed forward
(`d4583a45-0cbf-4096-95ef-d99dfc2b66fc`: *"there is a second TUI mode suggestion"*), the
agent chose the browser instead, and the operator then spent two rows on a *terminal
browser* rather than the TUI (*"suddenly it occurs to me this command line based web
browser w3m maybe a perfect UI interface for our cli … let's send gopi to do a feasibility
study"*, `01a0a4f2-...` → measured GO at `devdoc/w3m-cli-feasibility.md:3`, landed `69a4fed4`).
Then the TUI came back in through the side door: the terminal input surface was rebuilt to
terminal-UI quality anyway — an alternative-screen-safe answer band
(`internal/lineedit/region.go`, 589 lines), a status line (`cmd/agent/chat_stream.go:66-138`),
a completion menu (`internal/lineedit/menu.go`, 573 lines), 57 tests in the package (53 at the first pass). So the
recorded decision ("the lightweight CLI, not the TUI") and the shipped surface (a
scrollback-safe terminal UI) no longer describe each other, and no row, doc or rule
renewed the decision. **What an operator would want to know: the cheap option cost the
expensive option's price, and nobody said so.**

### 6.5 Quantitative claims, re-measured live (web + CLI)

Everything below is a direct query against `/home/user/.gopi/memory.db` at write time. Numbers in
§4a and §6.2 are these rows.

**A. The index's completeness (A = all rows, B = rows with a log pointer):**

| measure | value |
|---|---|
| rows in `chats` | **116** |
| rows with a log pointer (`log_path <> ''`) | **37** (31 ended, 6 not) |
| rows without one (`log_path = ''`) | **79** |
| `turn_count = 0` | **80** |
| `turn_count = 0` **and** no pointer | **79** |
| sum of `turn_count` over all rows | 83 |
| sum of `turn_count` over pointer rows | **83 — i.e. every real turn came from the 37 pointer rows** |
| distinct `started_at` values among the 79 no-pointer rows | **2** (2026-08-30 14:37:45 ×3, 14:37:46 ×76) |
| rows whose `first_msg` still carries runtime telemetry (`— elapsed=… iter=…`) | present in the no-pointer cohort (e.g. "turn 4", "ping — elapsed=4.2s iter=1 tokens=167") |
| pointer rows whose pointer resolves to an existing file | **37 of 37** |
| pointer-less rows with a flat `<id>.jsonl` at the sessions root | **0 of 79** |
| pointer-less rows whose id exists under the task-session directory layout (peerbox mirror) | **66 of 79**; the remaining **13** exist in neither layout on this machine |

**B. Endings and the "running" display:**

| measure | value |
|---|---|
| rows with `ended_at` set | **31** — and all 31 are pointer rows, i.e. **not one pointer-less row has ever been closed** |
| rows rendering as **running** | **85** = **79** pointer-less rows (nothing can ever close them) + **6** pointer rows |
| pointer rows rendering as running although their log ends with `session_end` | **1 measured by hand** (`97ddb03f-fb88-4ee9-95c7-60c6876e5e26`: `session_end`, `exit_code: 0`, `elapsed_sec: 1195.5`, written 2026-08-30 22:41:14) |
| pointer rows rendering as running with a log that has **no** `session_end` at all | **5 measured by hand** (`fd18dde7`, `825606b3`, `633f4fea`, `39f76b0a`, `b0f0403e`) — sessions killed before the 2026-09-05 fix |
| `end_reason = normal` / `interrupt` / `crash` | 29 / 2 / **0** |
| writers of `crash` in production code | **0** (the string appears only in the validation switch, `internal/tools/chat.go:290`) |

**C. The migration's own accounting:** 112 `tasks` rows carry the marker
`— chat: backfilled`; **0** of them are `source='human'` — the marker is on harness-created
rows only, so no operator-typed row was rewritten (the append-only claim holds).
`tasks.memory.db` counts: 632 rows `source='human'`, 1,393 `auto`, 415 `llm`.

**D. Usage, the blunt instrument ND1 rests on:**

| measure | value |
|---|---|
| terminal conversations (`mode='chat'`) with ≥1 turn | 32 |
| their turn distribution | 2 turns ×30, 1 turn ×2 (the two 1-turn rows total **0** tokens) |
| browser conversations (`mode='web'`) | 4, turn counts 1/1/7/12 |
| distinct days with a ≥1-turn conversation | 3 (2026-08-29 ×16 turns, 08-30 ×44, 09-14 ×2) |
| heaviest terminal conversation | 7.3 M in+out tokens |
| most recent conversation of any kind | 2026-09-16 09:44 (browser) |

**E. The sibling fix: LANDED (corrected 2026-09-18).** The first pass recorded
`fix/chat-help-command-arg` (`f8cca331`) as an unlanded sibling and warned a later reader
not to grade §6 against it. Measured now: `git merge-base --is-ancestor f8cca331 812f9a60`
→ **true**, merged by `77fac9ac` (2026-09-17). `/help <cmd>` is answered at
`cmd/agent/cli_chat.go:507-518` via `cmd/agent/commands.go:740`, and it shipped **with**
tests — four cases in `cmd/agent/cli_chat_test.go` (see §7).

**F. The 2026-09-18 re-measurement, and why it differs — the store is per-host.**

The figures in A–D were read on **2026-09-17 from the store of the other host** (the first
pass's worktree host; the session layout it saw — a `peerbox-mirror` mirror — exists there,
not here). This pass runs on **peerbox**, whose own store is a different population. Both
were measured with the same SQL; neither is wrong; **they are two machines.**

| measure | A–D (2026-09-17, other host) | F (2026-09-18, **peerbox**) |
|---|---|---|
| rows in `chats` | 116 | **104** |
| with a log pointer | 37 | **95** |
| without one | 79 | **9** |
| `turn_count = 0` | 80 | **9** (all pointer-less) |
| `ended_at` set | 31 | **84** |
| rendering as **running** | 85 | **20** (9 pointer-less + 11 pointer) |
| `end_reason` normal / interrupt / crash / unset | 29 / 2 / 0 / 85 | **81 / 3 / 0 / 20** |
| mode `chat` rows / turns | 32 ≥1-turn | **85 rows / 105 turns** |
| mode `web` rows / turns | 4 / 1,1,7,12 | **19 rows / 24 turns** |
| distinct days with a ≥1-turn conversation | 3 | **7** (2026-08-28 … 09-16) |
| pointer rows whose `log_path` names an existing **file** | 37 of 37 | **20 of 95** (corrected 2026-09-18b — see below) |
| pointer rows the **reader** resolves | 37 of 37 | **89 of 95**, and all 89 hits are a **directory entry** |
| pointer-less ids with a session file anywhere on this host | 13 of 79 absent | **9 of 9 absent** |
| `tasks.source` human / auto / llm | 632 / 1,393 / 415 | **609 / 853 / 450** at 2026-09-18 (pass 2); **609 / 854 / 450** at 2026-09-18b (pass 3) — this row is a live counter and moves as tasks are created |
| `tasks` rows marked `— chat: backfilled` | 112 (0 human) | **83 (0 human)**, unchanged in pass 3 |

**Correction to this table (2026-09-18b).** The `93 of 95` printed in the first F-pass was
produced by `[ -f "$log_path" ]` over a **substituted** path, not by the reader the code
actually runs; re-measured with the shipped resolver the number is **89 of 95, and every
one of the 89 resolves to a *directory*** — `chatFindSessionJSONL` (`internal/tools/chat.go:758-775`)
matches `ReadDir` entries by name, and a session **directory** named `<id>` satisfies
`strings.TrimSuffix("663d74a7-…", ".jsonl") == id` exactly as a file would. So "the pointer
resolves" is true and "the pointer is usable" is false, which the first F-pass collapsed
into one claim. The full mechanism is **G7** (and its accounting face, NF-3) below.

**What reproduces across both hosts is the *shape*, and that is the finding's real
support.** On peerbox the nine pointer-less rows still carry the migration's own signature:
their `started_at` takes exactly **two** distinct values (`2026-08-30 10:58:12` ×6,
`10:58:13` ×3 — the seconds the migration ran), their `first_msg` still carries runtime
telemetry (`ping — elapsed=5.4s iter=1 tokens=212`, `turn 4`, `first — elapsed=0.2s iter=2
tokens=0`), **not one of them has `ended_at` set**, and **none of their session files
exists anywhere on this host**. The mechanism in G1/G2 is therefore not an artifact of one
store's history: it is what the migration does when the session file is not where
`chatFindSessionJSONL` looks (`internal/tools/chat.go:758-765`, a **flat** `<id>.jsonl`
match), and it reproduces wherever that lookup misses.

**What does NOT reproduce is the cohort's size** — 79 of 116 there against 9 of 104 here.
**I can show the two stores differ; I cannot show why, and I will not guess.** Three
candidates are consistent with the data: (i) `chats` is not replicated, so the two hosts
simply hold different populations; (ii) this host's backfill has run again since (83 rows
carry the `— chat: backfilled` marker here, against 112 there); (iii) session files are
absent here for 13 pointer rows, so some rows outlived their logs. Distinguishing them
needs the other host's store read side by side, which this session cannot do. What is
**not** open is the artifact: nine rows here show the identical defect (migration-moment
start, telemetry first message, no pointer, never closable), the lookup that produced them
is unchanged, and nothing in the code detects a row whose log says *finished* while the
index says *running*. **Addendum (2026-09-18b):** pass 3 *did* perform one two-store
measurement, and it settles one of the three candidates negatively — a fresh
`gopi chat backfill` on a **copy** of this host's own store produced 106 new
directory-pointer rows from rows that were previously migrated with no pointer at all, so
candidate (ii) ("this host's backfill ran again") is *sufficient to produce* the difference
in cohort size and does not require the hosts to hold different populations. The
possibilities are narrowed to (i) and (ii)-plus-(iii); the artifact itself is unchanged. A reader comparing the two passes must not read the smaller cohort
as "fixed" — §Unknowns carries this as an explicit open question.

**The pointers that break — a fresh face of the same lookup.** Re-measured with the
**shipped resolver** (`chatFindSessionJSONL`, called through a throwaway probe in
`internal/tools` so the production function is the one executing): of the 95 pointer rows,
**89 resolve and 6 do not**. The two ids the first F-draft named as broken
(`663d74a7-e813-4495-9cfe-1c137f76cbd6`, `5666322d-4354-4662-bc01-23218e79ead8`) are **not**
among the six — their `log_path` names a flat `<id>.jsonl` that is indeed absent, but the
id resolves anyway, to the **directory** of the same name. The first pass measured **37 of
37** resolving. So the property it graded as sound ("the live path is sound") is *host-local*
too: a pointer names a path on the machine that wrote it, and on any other machine it
dangles. That is F7's sharpest consequence — `log_path` is an absolute path into one host's
home, stored in a table that is not replicated.

**The 11 pointer rows shown running on peerbox: adjudicated, and the answer is the opposite
of "not determinable".** All eleven have a log file present on this host (ten under the
`<id>/<id>.jsonl` layout, one — `adf9eee4-5ab3-4332-901b-55b791d68d94` — nowhere). Reading
those ten logs for `session_end`: **0 of 10 contain one**. So these are not "finished but
shown as running" (G2's hand-checked defect, which remains a property of the *other* host's
six); they are conversations whose JSONL genuinely stops without an end event, which is the
pre-2026-09-05 shape. The earlier "not performable here" note is withdrawn: it was written
before the `<id>/<id>.jsonl` layout was looked for by id rather than by the recorded path.

### 6.6 The finding the operator does not already know

**NF-1 — `gopi chat list` is a catalogue of its own migration artifacts, and it presents
some of them as live conversations.** On the store the first pass measured, the index is
116 rows: 37 real ones and 79 created from ledger summaries by a one-off data migration,
whose "start" times are the migration moment, whose "first messages" are task titles (with
`"— elapsed=218.3s iter=4 tokens=3294"` still attached), which carry no log pointer, and
which can never be finished by any code path — so they are displayed as **running** for
ever (79 of 116 rows, 85 displayed as running). **Re-measured on this host's store
(2026-09-18) the artifact survives, smaller: 9 of 104 rows are pointer-less, all nine
still show the migration-moment start (two distinct timestamps: `2026-08-30 10:58:12` ×6
and `10:58:13` ×3), all nine carry telemetry in `first_msg`, none of their session files
exists on this host, and not one of them can ever be closed — `ended_at` is set on **0 of
the 9** while 84 of 104 rows overall have been stamped** (§6.5-F). The tool's own text calls the
displayed field a state — `gopi chat list` prints `running` (`internal/tools/chat.go:379-390`)
— while the table's doc comment insists there is no state machine
(`internal/tools/chat.go:18-22`). **The operator has never seen this list: the CLI rows
that would show it are the ones nobody queries after the fact, and the browser rows are
the newest four.** The fix is cheap and bounded (drop the pointer-less rows from the list,
or backfill their `log_path` from the session JSONL the way the pointer rows were) — the
finding is that the index was never measured against its own purpose after the migration
ran.

**F7 — the chat index is per-host state, and the series' one store assumption does not
hold for it.** *(New in the 2026-09-18 verification pass; see §6.5-F and §Unknowns.)* The
ledger (`tasks`) is replicated across hosts — that is why every task id in this document
resolves — but **`chats` is local state**: the whole tree's non-test readers of the table
are `internal/tools/chat.go`, `internal/agent/{chat_ops,chatregister,loop}.go` and
`cmd/agent/{cli_chat,cli_web}.go` — none of them a transport — while the sync path
(`internal/tools/memory_sync*.go`, `task_sync_git.go`) names exactly three tables
(`FROM`/`INTO`: `tasks`, `sync_state`, `distilled_qa`) and **never `chats`**
(`grep -rln '"chats"' --include='*.go' .` → 6 files, all listed above). So each host holds
the conversations started or backfilled *on that host*. The first pass read 116 rows; this pass, on peerbox,
read 104, of which 95 carry a pointer and 9 do not. Same code, same migration, different
numbers — and a reader who compares two modules' documents without knowing this will
conclude the system changed when only the reader's host did. **Practical consequence:**
any chat-index claim in this series is a claim about one machine, and must name it.

> *Corrected 2026-09-18c.* The earlier form of this paragraph cited
> `grep -rn '"chats"' internal/sync/` → "no hits" as its proof. **That directory does not
> exist**, so the grep printed `No such file or directory` and the absence it reported was
> vacuous — a green that measured nothing (Rule 15b). The conclusion is re-established
> above by a whole-tree measurement with a non-vacuous shape: the non-test files that name
> the table are enumerated (six, all readers), and the sync path is shown positively to
> move exactly three other tables. A vacuous "no hits" is the failure mode to watch for
> whenever this document proves an absence.

**NF-3 (new in the 2026-09-18b pass) — the browser's conversations in this index are
dominated by *smoke tests*, and the index cannot tell them from the operator's own.** Of
the 19 `mode='web'` rows on this host, **15 have `first_msg = 'ping'`** and 4 are titled
`hi` / `Reply with exactly: PONG`; their `workdir` values name eleven throwaway checkouts
(`gopi-web-signal-end/cmd/agent` ×4, `gopi-envleak/cmd/agent` ×2, `gopi-web-ip`,
`GoPi/cmd/agent` ×11), and **two carry a `log_path` under `/tmp`** —
`/tmp/TestWebChat_SinglePersistentSession…/001/.gopi/sessions/<id>/session.jsonl` and
`/tmp/TestWebChat_ConcurrentTurnsSingleSession…/…` — i.e. rows written by
`cmd/agent/cli_web_test.go`'s own tests (`TestWebChat_SinglePersistentSession` at
`cmd/agent/cli_web_test.go:521`, `TestWebChat_ConcurrentTurnsSingleSession` at `:695`),
pointing into a `t.TempDir()` the test framework has since deleted. Both directories no
longer exist.

**The finding the operator does not know:** a carrier's adoption cannot be read off its own
index. §6.4-ND1 graded the web carrier's usage by counting `mode='web'` rows, and the count
is mostly test traffic — so "the browser is the carrier the operator uses" is a claim whose
evidence is, in majority, the test suite talking to itself. Measured 2026-09-18: 19 web
rows, of which at most **4** are plausibly a human at a browser (the non-`ping`/non-`PONG`
ones), and two are provably test fixtures with dead `/tmp` paths. (These two rows also date
the leak: both are stamped **2026-09-10 12:40**, in the same window as the `os.Setenv` test
leak filed as `01a08dcd-0401-7cd9-9c69-969876a43276` — "os.Setenv in tests leaks process
state (49 sites)" — and its fix `01a08975-6880-7e58-afb1-732d107d8e33`; I did **not**
establish that these rows are that leak's product, and I do not claim it.) The rule this
sharpens is C2/A9's: **an index that cannot separate a real conversation from a fixture is
not usable as a usage signal**, which is the same defect as G7 seen from the accounting side.

**NF-2 (secondary, same family) — the chat surface is an approved experiment, not a
recorded requirement** (§6.4-ND1). Six days of input-path work against 32 short
conversations and no requirement statement. Stated here because an operator who wants the
series' honesty rules applied to his own product decides differently when he sees that
sentence.

### 6.7 The requirement that was never written down

Beyond ND1–ND3, the gap between "the need" and "the record" has one more shape worth
naming, because it changes how G4 and G5 should be graded:

**a decision that exists for one carrier and not the other.** The browser carrier was given
a *product decision* the terminal carrier never got: the roadmap's invariants exist to hold
its queue discipline and its history ownership (`devdoc/gui-chat-roadmap.md:445-480`), and
its M1/M2 increments were dispatched as ruled work. The terminal carrier's equivalent
increments (A6 above all) were written down and **never dispatched** (§6.2-G3). So "the CLI
must reach parity with the web carrier" is a decision that exists only in a document the
agent wrote, and its consequence — that the same surface must be finished or explicitly
parked — has never been decided at all. An operator reading §6.2-G3 is looking at work
that a document scheduled and nobody owned.

---

## 7. Implications for tests (naming existing tests; writing none)

No test is written by this document. Each row names an existing test whose **verdict the
requirement changes** and the direction of change. "Direction" is what the test would
assert if the gap in §6 were closed.

| existing test | file | current verdict | direction the requirement demands |
|---|---|---|---|
| `TestChat_HappyPath_TwoTurnsSameSession` | `testsuite/chat_test.go` | green — two turns, one session | unchanged; this is the R2 pin. It is the test that would have to fail first if single-session consolidation were ever relaxed (§6.4) |
| `TestChatPTY_PasteBurstIsOneTurn` | `testsuite/chat_pty_test.go` | green | unchanged; extend only if a second carrier is added (A3 for the browser) |
| `TestChat_ErrorScenario_TurnFailure` | `testsuite/chat_test.go` | green — a failed turn exits | **re-aim**: today it pins "a turn failure ends the chat". Under R6/R9 the demanded behaviour is "an interrupt cancels the turn and the conversation survives"; the test's verdict inverts if A6 lands |
| `TestChat_Default_PerTurnClose` | `internal/agent/chat_session_test.go` | green — the *old* per-turn-close behaviour is still asserted as a supported mode | **must be revisited**: R2 says a conversation closes once. The test currently protects the pre-Epic shape as legitimate; after R2 is treated as a requirement, this test is the one that encodes the wrong rule |
| `TestChatOps_HistorySeededAndContinuity` | `internal/agent/chat_ops_test.go` | green | unchanged; the R3/R9 pin at unit level |
| `TestChatListQueryAndModelFilters` | `internal/tools/chat_test.go` | green — filters work | **insufficient, not wrong**: it asserts the filters, never the *displayed state*. G2 needs a case whose row is finished-in-the-log and asserted to render `normal` — today that assertion does not exist anywhere |
| `TestChatBackfillMigratesLegacyAutoRows` | `internal/tools/chat_test.go` | green — the migration inserts rows | **needs a companion verdict**: the test proves rows arrive; nothing asserts they are *usable* (a pointer, a real start time, no telemetry in `first_msg`). G1 lives exactly in that untested space |
| `TestChatBackfillDryRunWritesNothing` | `internal/tools/chat_test.go` | green | unchanged |
| `TestChatSearchIsolationFromTasks` | `internal/tools/chat_test.go` | green — chat search is isolated from the ledger | unchanged; but it is the test that makes G4's shape visible: R5's search is deliberately narrow, and no test claims otherwise |
| `TestChatEnsureRecordCloseCycle` | `internal/tools/chat_test.go` | green — one cycle, one ending | **extend**: the idempotent-close contract is asserted; the *never-closed* case (G2's 85 rows) has no test |
| `TestChat_ImageTurn_WireShape`, `TestChat_TextTurn_HasNoImageBlocks` | `internal/agent/chat_image_r5_test.go` | green | unchanged; R3's wire shape |
| `TestChatPTY_EditorRendersAndSubmits`, `TestChatPTY_ResizeDoesNotFreezeTheEditor` | `testsuite/chat_pty_test.go` | green | unchanged; R4/R8 pins |
| the 57 tests in `internal/lineedit` (e.g. `TestA5B_StatusLineShowsModelIterTokens`, `TestChatCompletion_*`) | `internal/lineedit/*_test.go` | green | unchanged, and **they are the reason §6.3's over-build note is a cost and not a gap**: the surface they pin is the surface the operator's complaint asked for |
| `TestChatHelpSwitchRoutesThroughTheArgument` | `cmd/agent/cli_chat_test.go:252` | **green** (landed 2026-09-17, `f8cca331`) | **new to this pass**: the `/help <cmd>` fix arrived with its own test, so the REPL-ignores-argv defect the first pass recorded is closed *and pinned*. It is the one §4a "NO" that has since become a "YES" — the document's §6.5-E is corrected to match |
| `TestChatHelpText_TakesTheCommandArgument`, `TestChatHelpText_AgreesWithSharedDispatch`, `TestRunChat_HelpReturnsZero` | `cmd/agent/cli_chat_test.go:178`, `:230`, `:41` | green | same commit; the shared-table agreement case is the one that would catch a REPL/web divergence in the help surface |
| `TestSlashHelpListsAllCommands` | `cmd/agent/commands_test.go:19` | green | unchanged; it pins the bare `/help` table, not the argument form — the pair together is what makes R7's help contract complete |

| `TestChatCookedFallback_NonTTY`, `TestChatCookedFallback_EditorNotEnteredWithoutTTY` | `cmd/agent/cli_lineedit_test.go:17`, `:68` | green | unchanged, and they **are** R8's pin — a pipe and a regular file both select the cooked reader. **But they pin the *absence* of the editor banner, not the *presence* of a degradation notice** (see (a) below) |
| `TestChatCookedFallback_NoEditorFlag`, `TestChatNoEditorFlagIsAccepted` | `cmd/agent/cli_lineedit_test.go:104`, `:174` | green | unchanged; `--no-editor` is still accepted and still selects the cooked reader (I checked this by hand after nearly recording the opposite — see §Verification) |
| `TestChatCookedFallback_ReaderParity` | `cmd/agent/cli_lineedit_test.go:125` | green | unchanged; byte-for-byte parity with the pre-editor reader, including the trailing partial line |

**Two directions this pass adds, and both are real holes rather than stale names:**

**(a) The degradation NOTICE is unpinned.** R8/A8 requires that the surface *tells* the
operator why the nice input is off. The code does exactly that, in two places
(`cmd/agent/lineedit_wire.go:183`: *"terminal too small for the chat layout … there is no
status line"*; `cmd/agent/lineedit_wire.go:187`: *"editor unavailable (%v) — falling back to
plain line input"*) — and **no test asserts either string**. Measured: grepping those
messages across `--include='*_test.go'` returns nothing. The tests that do exist
(`TestChatCookedFallback_*`) pin which **reader** is selected and that the editor *banner is
absent*; none pins that the operator is **told**. So A8's first half (the chat still works)
is covered and its second half (it says why) is not — which is the letter of the Rule 15
obligation this row was inferred from, and the only test gap this pass found that the first
pass had not already named.

**The one direction worth stating plainly:** no existing test would go red if `chats` held
only the 37 real conversations, and none would go red if it held 79 fiction rows presented
as running. That is not a coverage number — it is the reason G1 and G2 survived to a
reverse-requirements pass.

**(b) The session-layout change has no test at all, and the fixture is *why*.** *(New
2026-09-18b; this is the §7 face of G7.)* The only fixture that builds a session log is
`writeChatSessionJSONL` (`internal/tools/chat_test.go:33-56`), and it writes
`filepath.Join(dir, sessionID + ".jsonl")` — a **flat** file at the sessions root
(`internal/tools/chat_test.go:50`). Production this host has **zero** such files: the layout
is `<id>/<id>.jsonl` (69 rows) or `<id>/session.jsonl` (20 rows). So the fixture pins the
world `chatFindSessionJSONL` was written for, and the world it actually runs against has no
test. Measured: **0** tests name `chatFindSessionJSONL`; the two that reach it do so only
through `chatSessionFactsOf` (`internal/tools/chat_test.go:222`), which they feed a flat
path. Any behaviour change here — including the fix — would ship with no test to catch a
regression, and G7 itself could not have been caught by the suite in its current shape.

The direction the requirement demands: a case whose store holds `<id>/<id>.jsonl` (and
`<id>/session.jsonl`) and which asserts that the reader returns the **file**, that a
directory is never returned, and that `chatSessionFactsOf` yields the log's real turns —
i.e. the fixture must be built in the layout production writes, not the one the matcher
remembers.

---

## 8. Confidence and limits

### Confidence per claim

**The rule applied:** `[inferred]` means reconstruction; **weakest link** is the single
observation that would most cheaply falsify it. Rows without the marker rest on a cited
artifact I read directly.

| claim | tier | confidence | weakest link |
|---|---|---|---|
| R1 — an interactive chat must exist | operator text via an `auto` row + commits | **low** (0.45) | the genesis row's payload is empty; only the harness summary survives, and it truncates the operator's sentence. The only explicit demand rows (`01a0a4cf`, `01a0a97e-*`) ask for *quality relative to hermes* on a surface that already existed — presupposition, not requirement |
| R2 — one conversation, one durable thread | operator verbatim | **high** (0.85) | the sentence *"as gopi chat mode"* in the web row is circular; the CLI's rule is itself recorded only in a task report |
| R3 — work inside the conversation | operator verbatim ("good direction, now …") | **medium** (0.6) | single statement, single occurrence; usage shows X (2 of 32 conversations, by flag default off) |
| R4 — a pasted block is one message | operator complaint + code mechanism + written acceptance | **high** (0.9) | the acceptance text is agent-authored; the *mechanism* is not, which is what carries the row |
| R5 — durable and findable | operator verbatim ("status correctly … creation/update/close") | **medium** (0.55) | "status correctly" is undefined; G1/G2 are its direct consequence (§6.4-ND2) |
| R6 — interrupting must not lose the thread | operator verbatim (bug row) | **high** (0.85) for the close-on-signal half; **low** (0.35) for the turn-cancel half | the filed bug was on the one-shot path; the chat host's behaviour is asserted by code path, not by an operator observation |
| R7 — slash commands are local | inferred from reports + code comments | **medium** (0.6) | no operator row asks for slash commands; the vocabulary came from a research doc |
| R8 — degrade honestly, keep the piped path | project rule (GOPI.md Rule 15) + a load-bearing test | **medium** (0.65) | inferred from a *project* rule, not from an operator sentence about chat |
| R9 — a tool error must not kill the turn | code contract + e2e arms | **medium** (0.6) | the natural strong form ("Ctrl+C keeps the session") is the unimplemented half |
| **ND1 — no recorded requirement that chat must exist** (the recorded ask is quality-relative: "practical, Hermes-like", `01a0a4cf`) | absence across the whole canon + measured usage | **high** (0.85) | absence of evidence; the falsifier is a briefing/row I did not find — the queries are `source='human'` rows (632), chat-named rows, and the devdocs; a later reader can re-run them. A single row saying "we need a conversation surface because …" would move this to low |
| G1 (79 pointer-less rows on the HP store; **9 of 104** on this host, 2026-09-18) | live measurement + code | **high** (0.95) for the mechanism | none material for the artifact — it reproduces on both stores with the same shape; the *count* is store-dependent (§6.5-F), which is F7 |
| G2 (85 shown running on the HP store; **20 of 104** on this host; `crash` unwritten) | live measurement + code | **high** (0.9) | the 5 "finished but shown running" rows are hand-checked (log has no `session_end`); the *reason* they lack one pre-dates the 2026-09-05 fix and is not determinable from the data. **Corrected 2026-09-18b:** the 11 pointer-rows-shown-running *were* adjudicated here — all eleven have a log present (ten under `<id>/<id>.jsonl`, one absent), and **0 of the ten contain `session_end`** — so the "not checkable locally" note is withdrawn |
| **G7 (new 2026-09-18b)** — the reader resolves a session id to a **directory**, opens it successfully, scans zero events, never checks `sc.Err()`, returns a non-nil empty facts struct, and every caller reads non-nil as success | shipped-function probe (`chatFindSessionJSONL` + `chatSessionFactsOf`) + a fresh backfill on a **copy** | **high** (0.9) | measured, not inferred: 89 of 95 pointers resolve **to a directory**, 89 rows return empty facts, 0 return usable facts; on a copy a fresh `gopi chat backfill` grew the table 104→259 with **106** directory pointers and **0 of 106** closed. The weakest link is the word "silent": the failure is silent to the *caller*, and the counter-evidence would be a log line — `grep` for one in `chatSessionFactsOf`'s path shows none exists |
| **NF-3** — 15 of 19 `mode='web'` rows are `first_msg='ping'`, 2 provably test fixtures with dead `/tmp` paths | live query + `cmd/agent/cli_web_test.go:521`, `:695` | **high** (0.85) for the composition; **low** (0.3) for ND1's usage reading | the falsifier is one operator-started browser row I did not see; the composition number is a query, the *interpretation* (that ND1's web-usage evidence is mostly test traffic) is an inference, marked as such |
| NF-1 | the above | **high** (0.9) | as G1/G2 |
| §6.3 over-build judgements | design reading | **medium** (0.5) | "no requirement asks for it" is an argument from the §4 row set's silence, not from a stated exclusion |
| ND3 (the TUI decision was never renewed) | doc trail + measured surface | **medium** (0.6) | the doc trail records the *original* decision; the renewal's absence is an inference from silence |
| **Where the code is on master** | every `file:line` above | **high** | each was re-read before commit, and in the 2026-09-18b pass every citation was re-resolved against the tree by the gate in §Verification item 13 (104 distinct citations, 0 BAD). The sibling branch `f8cca331` **IS** an ancestor of master (merged by `77fac9ac`), so `/help <cmd>` is answered on master at `cmd/agent/cli_chat.go:507-518`; the earlier "NOT an ancestor" note here was the pass-2 error corrected in §6.5-E |

### `devdoc/gui-chat-roadmap.md` — decision or aspiration?

**Verdict: aspiration (standing high), with one decision inside it.**

The taxonomy is the method's own (an operator row or a rule is a *decided constraint*; a
plan the agent wrote is an *aspiration*). By that test:

- **It is agent-authored, one night after the mode existed.** Four commits, all by the
  same author, 2026-09-16 (`d54c9822`, `bd2fbd1e`, `52d649cc`, `d145c847`); its own
  §0 answers "what is broken today" with the roadmap's diagnosis, not the operator's.
- **Its commitments are real and were honoured — after a cancellation pass.** It filed
  five task ids (`devdoc/gui-chat-roadmap.md:503-511`), and measured: **all five of those
  rows are `cancelled`** (`01a0a946-47b8-7f36-b799-d973078723ea`,
  `01a0a946-613b-79a2-8fea-c19049ea26a3`, `01a0a948-e837-…`, `01a0a948-eaf0-…`,
  `01a0a948-e9b2-…`); the work was then re-filed as four `source='human'` rows
  (`01a0a97e-e39c-7d9b-9e59-b36061da7ad1`, `-e3c0-…`, `-e402-…`, `-e41f-…`), all
  `completed`. So it functions as a *plan of record* for the 2026-09-16 workstream —
  an aspiration that was executed through the operator's own re-filing, which is why it
  cannot be filed as noise and still does not become a requirement.
- **It does not become a requirement by being executed.** Nothing in it is stated by the
  operator as a need; its only operator-anchored content is the *comparison target*
  ("competing hermes/claude", which comes from the operator and is cited in §4-R4), and
  its own §6 is an honest "what I did not verify" list.
- **One thing inside it IS a decision, and it should be treated as one:** §5's invariant
  4 — *"Zero new dependencies. No `x/term`, no JS framework, no bundler. If an increment
  cannot be built from stdlib + `x/sys` + `modernc.org/sqlite`, it is not in this
  roadmap"* (`devdoc/gui-chat-roadmap.md:465-467`). That is C1 restated as a local rule,
  and it is the reason the line editor had to be own-rolled. It is the only reason this
  document can treat 8,746 lines of editor as *required* rather than *over-built*.
- **M3 is deferred by the roadmap's own words** — *"not scheduled; revisit with a product
  decision, not by drift"* (`devdoc/gui-chat-roadmap.md:438-441`) — which is exactly the
  product decision §6.4-ND1 says was never taken.

So: **aspiration, with one embedded decided constraint (zero new dependencies) that this
document adopts as C1's local form.** Its §7's own note that two web rows overlap on
purpose (Rule 26) is a second reason to read it as a plan, not a requirement.

### C1–C5 check (the operator's vision as a binding constraint on each requirement)

| | constraint | verdict | evidence / conflict |
|---|---|---|---|
| **C1** | One static binary, zero dependencies, no plugin architecture | **PASS — and it is the load-bearing constraint** | The line editor is own-rolled precisely because the alternative is unavailable: `golang.org/x/term` is in neither `go.mod` nor the module cache (`devdoc/gui-chat-roadmap.md:273-297`, and its invariant 4 at `:465-467` states the rule). The package exists to avoid a dependency (`internal/lineedit/doc.go:1-13`: raw mode via the termios ioctls of `x/sys/unix`, "already a direct dependency"). No plugin surface, no JS framework, no subprocess. |
| **C2** | The operator must be able to control the whole thing | **PASS, with one watched branch** | Every chat behaviour has a CLI face: start, seed, list, search, migrate, toggle tools, clear, quit (`cmd/agent/cli_chat.go:18-64`). The conversation is one file plus one row, both the operator's. **Watched:** the index is written best-effort and **silently** — a failed registration only logs (`internal/agent/chatregister.go:70-73`), so a chat can run with no row at all and the operator cannot tell. That is the C2-shaped cost of §6.2-G2. |
| **C3** | A daemon must be simple: every branch multiplies the failure surface | **PASS** | Nothing resident. Chat is a foreground REPL; its signal handler is one goroutine with a 1-buffer channel (`chatSignalBuf = 1`, `cmd/agent/chat_stream.go:43-46`; `cmd/agent/cli_chat.go:333-348`). The ops path caps its own rounds (40, `internal/agent/chat_ops.go:37`) rather than adding daemon-side nuance. |
| **C4** | Clean up after yourself; a dirty state is a violation; name the bound and the refusal | **CONFLICT — filed as a §6.2 finding (G1/G2)** | The module leaves 79 unusable index rows that nothing will ever close (bounded by nothing, refused by nothing) and reports 85 of 116 conversations as running (79 of them permanently). Refusals that *are* named: the close hook is idempotent (`internal/tools/chat.go:285-313`), and `batch cap`/line caps are named constants (`chatJSONLMaxLineBytes`, `:660`). The residue is the conflict; it is recorded here rather than glossed. |
| **C5** | Small team, limited resources; solutions one person can run | **PASS, with one measured cost** | Two carriers over one turn engine and one index is the cheap shape; nothing needs a deployment. The cost is the surface: the line editor alone is 8,746 lines / 57 tests for the *input path* (`internal/lineedit`; measured 2026-09-18), and it pushed the terminal carrier into terminal-UI territory the founding study priced at "a complete product" and put out of scope (`devdoc/gopi-chat-mode-effort.md:26-29`) — see §6.4-ND3. One person can run it; one person must also hold it. |

**Two of the five carry conflicts, and both are filed above rather than resolved here:**
C4 → §6.2-G1/G2; C5 → §6.4-ND3. C2's watched branch (best-effort, silent index writes) is
recorded in the verdict row and belongs to the same family as G2.

---

## What I could not determine

1. **Whether the operator considers the chat surface a requirement at all.** This is the
   document's first finding, not an omission (§6.4-ND1): the genesis chain is intact and
   the requirement statement is absent. I can prove the absence over the canon I searched —
   every `source='human'` row (632), the chat-named rows, the briefings directory (which
   does **not** exist at `/home/user/.gopi/briefings`, so the umbrella's "122 files on master"
   were not reread here), `GOPI.md` (29 rules, none mentioning chat), and the agent-authored
   devdocs (`devdoc/chat-*.md`, `devdoc/gui-chat-roadmap.md`, `devdoc/w3m-cli-feasibility.md`).
   I cannot prove it over the operator's head.
2. **Who decides whether a conversation is "open".** ND2 — the schema says facts-only, the
   list says `running`. I can show the contradiction; I cannot tell which side was meant,
   and the two candidate answers ("no state" / "state, but shown badly") lead to opposite
   fixes: delete the display field, or write the missing endings.
3. **Which side of the `ended_at` gap `97ddb03f-…` fell through.** Its log closed cleanly
   (`exit_code: 0`) on 2026-08-30 and the index still says `running`, so *something* wrote a
   `session_end` without a close hook. I traced the two candidate mechanics (the `ended_at`
   column is written only by the backfill; the live close writes it only when a reason is
   passed) but I cannot tell from the row which one it was, and settling it needs a
   controlled signal on a live chat — a measurement this document may not perform (Rule 27
   is about load, but the same discipline applies: do not manufacture the condition to make
   the number).
4. **Whether the `/help <cmd>` fix is the whole of the sibling branch's intent.**
   **Resolved 2026-09-18b — withdrawn as an open item, kept as a correction.** The first
   pass's premise was wrong: `git merge-base --is-ancestor f8cca331 master` exits **0**
   (merged by `77fac9ac`), so the branch IS an ancestor of master and `/help <cmd>` IS
   answered on master (`cmd/agent/cli_chat.go:507-518`), with tests (`cmd/agent/cli_chat_test.go`).
   What remains genuinely open is only the branch's *reasoning*, which I read as a subject
   line, not as a diff. The worktree was inspected read-only (Rule 29).
5. **Whether the ledger's rows can supply the real timestamps for all 79 migrated rows.** I
   verified the join (110 of 116 index rows match a `tasks` row by session id) and one case in
   detail (`97ddb03f-…`: ledger 2026-08-29 22:18:56 vs index 2026-08-30 14:21:18). The
   cohort's own start times are exactly two values (the migration ran in two seconds), and
   `created_at` on the ledger rows is per-run — but I did not compare all 79 pairs, so "the
   ledger holds the truth the index lost" is sample-backed, not proven for the cohort. Related
   and equally open: **why** the session lookup missed 79 files at the moment it ran. Today's
   root holds both layouts; whether the mirror was populated later, or whether the run's
   sessions lived on the other host, is not determinable from the store.
6. **Whether the terminal carrier's 32 conversations represent appetite or merely
   availability.** Almost every one is a two-turn smoke exchange, and the mode was being
   built and verified during that window — so low usage is equally consistent with "just
   shipped, not yet used" and with "the operator decided earlier that the browser solves
   it" (his own words, `01a0a4f2-...`). **I could not find a row where he used `gopi chat`
   for real work.** ND1's framing depends on which reading is true.

7. **Why this host's `chats` store holds 9 pointer-less rows where the first pass found
   79 on the other host** (§6.5-F, F7). Three candidates fit the evidence and I can settle
   none of them from here: the table is per-host and unreplicated (re-measured whole-tree:
   the sync path moves `tasks`/`sync_state`/`distilled_qa` only, and every non-test file
   naming `chats` is a reader, not a transport — F7); the backfill ran again on this host since (83 marker rows
   here against 112 there); or rows outlived their logs (13 pointer rows have no file
   here). **Settling it needs both stores read side by side**, which is a two-host
   measurement this session cannot perform. What I *did* settle: the artifact reproduces
   (9 rows, identical signature) and the lookup that makes it is unchanged.

8. **Whether `log_path` is meant to be portable.** *(Revised 2026-09-18b.)* Twenty of this
   host's 95 pointer rows name a file that is actually here, and six ids the shipped reader
   cannot resolve at all (§6.5-F, G7). The column is an absolute path into one machine's
   home, stored in a table that is not replicated — so a pointer is only meaningful on the
   host that wrote it. **I can show the breakage; what I cannot settle is the intent** (a
   deliberate host-local index, or an oversight). Nobody wrote it down either way.

9. **Whether the directory-pointer defect was ever the *cause* of a visible failure.**
   *(New 2026-09-18b.)* G7's mechanism is measured end to end, but I found no operator
   observation that a chat log was unreadable, no bug row, and no test failure — which is
   exactly what "silent" predicts and exactly what makes it unfalsifiable from the record
   alone. The counter-evidence would be one report of `gopi chat list` showing a healthy
   row whose log would not open; I did not find one, and I cannot distinguish "never
   happened" from "never noticed".**

10. **Whether the `<id>/<id>.jsonl` layout is the sessions root's intended shape or an
   accident of two writers coexisting.** *(New 2026-09-18b.)* On this host **69** rows have
   `<id>/<id>.jsonl` and **20** have `<id>/session.jsonl` — two different inner names in
   the same tree. I measured the coexistence; I did not find the decision that chose either,
   and `chatFindSessionJSONL`'s name-match semantics were written for a flat root that no
   longer exists. Naming the owner of that layout is the cheapest way to close G7.

---

## Verification of this document

**Pass 1 — 2026-09-17** (branch `feat/req-b9-chat-ui`, baseline master `bf4d1941`): every
`file:line` was re-read at that revision (`sed -n "${line}p"`); §6.5 was a live query.

**Pass 2 — 2026-09-18 (this one), branch `docs/req-09-chat-ui-verified`, baseline master
`812f9a60`.** The document was treated as a claim to check, not a text to rewrite; the
scope is verification and completion.

**1. Citation gate — no basename fallback.** A resolver extracts every `file:line` /
`file:a-b` from this file, requires the file to exist **by path** (a bare basename is
reported BAD, never resolved by searching the tree) and the line to be within `wc -l`.
On the untouched document: **88 distinct citations, 73 OK, 15 BAD** — all 15 bare basenames
in `cmd/agent` (`cli_chat.go` ×9, `chat_ops.go` ×3, `lineedit_wire.go` ×2 plus one
`chat.go`-family). Every one was **re-anchored by symbol** (read the function the sentence
names, then cite its current lines), never by guessing an offset. After the pass the gate
is re-run; the output is the evidence for this claim.

**2. Test-name resolver.** Every test name §7 cites was resolved with
`grep -rn '^func <TestName>' --include='*_test.go'`; all resolve to a real `func TestX`
(§7 carries the file and line for each). Two names were **corrected by resolution** rather
than by memory: the roadmap's own shorthand `TestChatPasteBurst` is
`TestChatPasteBurst_ThreeLinePasteIsOneMessage` (`cmd/agent/lineedit_wire_test.go:321`),
and the `/help <cmd>` fix's tests live in `cmd/agent/cli_chat_test.go` (four of them).
Re-run in pass 3 over the whole document: **30 names cited, 29 resolve, 1 does not** — and
that one is deliberate: `TestChatPasteBurst` appears exactly once, inside the quotation of
the roadmap's own `-run` shorthand (`devdoc/gui-chat-roadmap.md`'s text, quoted in §R4 and
here), and the sentence names its real form in the same breath. **§7 in isolation resolves
25 of 25.** No test name in this document is left pointing at nothing.

**3. Section check.** §1–§8, `## Boundary`, `## What I could not determine` and the C1–C5
constraints table were confirmed present by grep of the headings; the original 8-section
shape was preserved and the additions are subsections inside it, never new top-level
sections.

**4. Live numbers, re-measured and date-stamped.** §6.5-A–D keep the originals with their
own date; §6.5-F adds the 2026-09-18 re-measurement on **this** host and the comparison
table. The re-measurement also produced the pass's largest correction: the `chats` store
differs between hosts, so **the counts in A–D describe one machine and not the system**.

**5. Two claims I retracted mid-pass, recorded because the retraction is the check
working.** (a) I first wrote that `--no-editor` had been removed; reading
`cmd/agent/cli_chat.go:279-280` showed the flag is still accepted and
`TestChatNoEditorFlagIsAccepted` pins it — the paragraph was reverted. (b) I first wrote
that the `/help <cmd>` fix "shipped without a test"; `git show --stat f8cca331` lists
`cmd/agent/cli_chat_test.go +107`, so the claim was corrected to name its four tests. In
both cases a sentence that would have entered the record **wrong** was stopped by reading
the code the sentence was about.

**6. Deliverable statement (Rule 6d).** Branch `docs/req-09-chat-ui-verified`, worktree
`/home/user/workspace/gopi-req-09chat`, off master `812f9a60`. Recover this file with
`git show docs/req-09-chat-ui-verified:devdoc/requirements/09-chat-ui.md`. **No other
task's worktree was entered or modified** — the pre-existing `/home/user/workspace/gopi-req-b9v`
(which shares this branch's name pattern) was read with `git -C … status --porcelain` and
left untouched (Rule 29). The sibling `fix/chat-help-command-arg` was inspected read-only
(`git merge-base --is-ancestor`, `git show --stat`) and never checked out.

**7. What this pass does NOT claim.** It does not re-grade §6.1–§6.4 or the C1–C5 verdicts
— those are reading judgements over evidence that did not change, and re-deriving them
would be a rewrite, not a verification. It corrects anchors, the landed/unlanded sibling
line, the store-dependence of every count, and the two test-list facts above.

**Pass 3 — 2026-09-18b, same branch/worktree, baseline master `812f9a60`.** A second
verification pass was warranted because pass 2's own §6.5-F introduced a measurement it did
not take from the shipped code. Scope: execute the claims instead of re-reading them.

**8. The resolver claim was re-executed against the shipped function, and it was wrong.**
Pass 2 asserted "**93 of 95** pointer rows resolve, 2 do not". A throwaway in-package probe
(kept out of the commit) called the production `chatFindSessionJSONL` and
`chatSessionFactsOf` directly and measured: **89 of 95 resolve, 6 do not** — and **all 89
resolutions are to a DIRECTORY**, with **89 rows returning a non-nil but wholly empty facts
struct and 0 returning usable facts**. The two ids pass 2 named as broken are not among the
six. §6.5-A/F, G7 and §Confidence are corrected. The pass-2 number came from a `-f` test on
a substituted path; the difference between "the pointer resolves" and "the pointer is
usable" is the whole finding.

**9. A fresh `gopi chat backfill` was run — on a COPY, never the live store.** Isolated
`GOPI_HOME` + a copied `memory.db`: 104 → **259** rows, **106** pointers naming a
**directory**, marker rows 83 → **238**, **0 of 106** closed. The live store was read-only
throughout and re-measured after (104 rows, 0 directory pointers, 83 markers — unchanged).
So G7 is not a legacy artifact: **re-running the migration manufactures it at scale.**

**10. G2's "not checkable locally" note was withdrawn by looking where the reader looks.**
By id rather than by the recorded `log_path`, all 11 pointer-rows-shown-running have a log
here (ten under `<id>/<id>.jsonl`, one absent), and **0 of the ten contain `session_end`**.

**11. NF-3 is new: a carrier's usage cannot be read off its own index here.** 19 `mode='web'`
rows, **15** with `first_msg='ping'`, **2** with a `log_path` under a deleted `/tmp` test
directory, `workdir` naming eleven throwaway checkouts. §6.4-ND1's usage reading is
corrected in §6.6-NF-3.

**12. The one item that got *smaller*, recorded because the correction is the check
working.** §Unknowns #4 ("is `f8cca331` an ancestor of master?") is **withdrawn**: it is
(merged by `77fac9ac`). Pass 2 had already corrected the document's §Boundary claim but left
the question open in §Unknowns; both now agree.

**13. Gates re-run at the end of this pass** (commands and outputs in the task report):
citation gate with **no basename fallback** (every `file:line` must exist BY PATH and be
within `wc -l`); a negative probe proving the gate fails on a bare basename, a non-existent
file, and an out-of-range line; the test-name resolver; the section-heading check; and
`git status --porcelain` in the worktree. **No test was written and no production file was
modified** — the probe lived in a file deleted before the commit.
