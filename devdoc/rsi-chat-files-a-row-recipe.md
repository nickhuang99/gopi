<!-- doc-class: historical -->
# The RSI recipe: have a `gopi chat --tools` turn file a REAL row against gopi

**Status: the loop is closed, and the closure is reproducible.** A real
`gopi chat --tools --no-editor` session (session id
`a43ed4a8-987f-445f-8c1d-83372eb1837a`, one live model turn) ran its own CLI,
sensed a genuine doc/reality drift, and filed row
`01a0d59c-7983-7086-8753-34e87ad05da9` with `task_submit`, `tags=["bug"]` and an
evidence attachment. The row is `pending`, carries a non-empty briefing, and its
`task_submit` tool_use is in the chat session's JSONL.

This doc is the RECIPE (sense → decide → file → verify), the two traps that cost
three redundant ledger rows to learn, and the honest limits of the chat surface.

## 1. Where the pieces are

| verb / tool | what it gives the agent |
|---|---|
| `bash` | runs gopi's OWN verbs. This is the SENSE arm: `gopi ui_state -h`, `gopi --version`, `gopi task show <id>` — the agent reads gopi's real behaviour, not a description of it. |
| `read` / `keyword_search` | reads the doc that claims something about that behaviour (the drift needs two sides). |
| `task_submit` | the WRITE arm — files the row, with `attachments=[<abs path>]` archiving the evidence *by copy* at submit time. |
| `gopi task show <id>` / `task list --status pending` | the VERIFY arm, run by anyone afterwards (the filing session's own row is excluded from its own claim selection, so an external check is the honest one). |

The full routing prompt and every tool definition reach a `--tools` chat turn
unchanged: `internal/agent/chat_ops.go:312` passes `a.SystemPrompt` and
`claudeToolDefs` into the tool-enabled round. (The plain `gopi chat` path uses a
separate, smaller system prompt — no tools, no filing.)

## 2. The recipe

The exact driver, the exact one-line prompt and the raw pty transcript are
committed beside this doc under `devdoc/assets/rsi-chat-files-a-row/`. Two
commands reproduce it end to end:

```bash
# 0. a private profile that still files into the OPERATOR's real ledger.
#    GOPI_HOME moves sessions/ledger/settings; GOPI_MEMORY_DB picks the LEDGER.
#    Pin both to separate places or the row lands in a sandbox nobody reads.
gopi sandbox create --label rsi-recipe --task-id "$TASK_ID"    # prints the path
GOPI_HOME="$SB" GOPI_MEMORY_DB=/home/user/.gopi/memory.db \
  python3 devdoc/assets/rsi-chat-files-a-row/drive_chat_pty.py \
        /tmp/gopi-rsi4/gopi "$SB" <dir-to-sense-in> \
        devdoc/assets/rsi-chat-files-a-row/chat-turn-message.txt /tmp/raw.txt
```

and the ONE-LINE message that did the work is
`chat-turn-message.txt`: it names the sense command **verbatim**, names the tool
(`task_submit`), and gives the exact argument VALUES (`summary`, `payload`,
`tags`, `attachments`) — see trap 2 for why "file a bug, use task_submit" is not
enough.

### The shape that works (measured, in order)

1. **SENSE — one `bash` call, no interpretation.** Have the agent run the verb(s)
   under test AND the doc line that contradicts them, redirecting both into one
   evidence file with exit codes:
   `{ gopi ui_state -h; echo exit=$?; sed -n '83,95p' <doc>; } > $EV/<name>.md`.
   The agent does not paraphrase — the file is raw output.
2. **FILE — one `task_submit` call**, `tags=["bug"]`, `attachments=[<that same
   absolute path>]`. Do not pass `repo` from chat (see §3): the tool resolves it
   itself.
3. **VERIFY — from outside the chat session.** `gopi task show <id>` (status,
   tags, briefing, attachment sha256), `gopi task list --status pending` (the row
   is offered by the claim funnel), and the chat session's `session.jsonl`
   (`tool_use` entries: `bash` then `task_submit`, plus the tool_result
   `Task queued: <id> (status=pending, source=agent, tags=bug)`).

   On the pending view, read the marker carefully: `[next]` rides the **first
   claimable row only**, so a row with NO marker is not parked — `[queue-skip]`
   is the parked spelling (`internal/tools/tasks.go:6851`, `taskRowClaimable`).
   The strongest cheap proof of claimability is to make the row the only pending
   one and watch the marker land on it — done here on a **copy** of the ledger
   (`python3 -c 'sqlite3 … src.backup(dst)'` → `/tmp/ledcopy.db` → cancel the
   other pending rows *there*), never on the operator's live queue:

   ```
   $ GOPI_MEMORY_DB=/tmp/ledcopy.db gopi task list --status pending
   1 task(s) with status=pending:
   #01a0d59c-7983-7086-8753-34e87ad05da9  [pending]  stale handoff doc: … [next]
   ```
   Raw transcript: `assets/rsi-chat-files-a-row/claim-funnel-proof.txt`.

## 3. Traps — both cost real rows

**Trap 1: in cooked mode, one `\n` is one USER TURN.** `--no-editor` reads
COOKED lines (the prompt is `you> `), so a multi-line prompt is not one message:
the model receives the fragments as *separate* user turns and — behaving
correctly — files a row for each one. Measured: a 6-line prompt produced 3 rows
(`01a0d59b-79d8`, `01a0d59b-cde3` were duplicates of the intended
`01a0d59c-7983`; both were cancelled with a `--reason` naming the canonical row,
not deleted). **Send ONE single-line message.** (The model even flagged the
duplication itself — an impressive failure mode, but a failure mode.)

**Trap 2: naming the tool is not enough — give the argument VALUES.** A prose-only
request ("sense a real defect and file it as a bug row with task_submit") produced
**zero** `task_submit` calls in a 40-round turn: the model explored, sensed a
defect, then stopped to ask for the exact arguments. The turn that worked named
the tool AND supplied `summary` / `payload` / `tags` / `attachments` literally.
This is the routing evidence Rule 11 asks for: the tool is reachable and
understood, but the model will not invent a payload shape for a ledger row.

**Also measured, worth knowing before you rely on a turn.** A tool-enabled chat
turn is capped at `internal/agent/chat_ops.go:37` (`maxChatOpsToolRounds = 40`)
and hard-fails the turn with "chat turn exceeded 40 tool rounds without a final
answer" — a driver must not read that as a crash. And a driver must end the turn
on the prompt REAPPEARING (`you>` again), never on silence: a quiet 3s window
fired mid-round on this very run and the transcript read "run interrupted by
signal".

## 4. Honest limits of the chat surface

- **`repo` is resolved, not passed — and it resolves well.** With `repo` empty,
  the tool takes the process cwd and maps it to the DURABLE main checkout
  (`internal/agent/loop.go:5947` → `internal/tools/tasks.go:4669`
  `DurableRepoForSubmit`), so a chat turn running inside a linked worktree still
  stamps `deliverable.repo=/home/user/work/GoPi`, the main checkout that outlives
  the run. Well-formed: verified on the filed row (its `deliverable` JSON is
  exactly `{"attachments":[…],"repo":"/home/user/work/GoPi"}` — read out of the
  ledger row itself, not inferred from the printed view).
- **The filing SESSION is recorded, but nothing says which TASK it served.** The
  ledger row's `session_id` is the chat session's own id
  (`a43ed4a8-…`, the id `gopi task show` prints), and the chat turn passed no
  `parent_task_id` — so `parent_task_id` is the empty string in the ledger (which
  is what makes the Rule 33 parent gate pass) and `deliverable` carries no
  parent-session field at all. An auditor reading the row alone can tell *which
  chat* filed it, but not which task's work it belongs to: a chat-filed row is an
  orphan by construction unless the prompt names the relationship in the payload
  text.
- **The attachment replica lives under the CHAT process's `GOPI_HOME`, while the
  ledger row lives in `GOPI_MEMORY_DB`.** Filed from a sandbox with the real
  ledger pinned, `gopi task show` from the main profile prints
  `attachment1: /home/user/.gopi/sessions/<id>/attachments/…` — a path that does
  not exist there — and `session_log: … (not found)`. The bytes ARE safe (same
  sha256) at
  `<sandbox>/.gopi/sessions/<id>/attachments/<original abs path>`; the operator
  just has to know which profile to look in. A chat-filed row is therefore
  *reportable* but its evidence pointer is profile-relative, which is a real
  wart, not a scratch of this run's driver.
- **No `priority` and no `source` control from chat.** `source` is forced to
  `agent` (`internal/agent/loop.go:6562`, `taskSourceLLM = tasksource.Agent`), and
  priority comes from the tags derivation (bug → 1). A chat turn cannot file a
  low-priority note or claim human provenance.
- **The row's briefing is only as good as the single line you send.** The chat
  surface does not offer a `--payload @FILE` equivalent, so a long self-contained
  briefing has to survive being typed into one line of a terminal. The filed row
  is real and dispatchable; whether it is a *good* briefing is a property of the
  prompt, not of the machinery.

## 5. The row this recipe produced (the RSI artifact itself)

`01a0d59c-7983-7086-8753-34e87ad05da9` — *stale handoff doc:
`devdoc/handoff-20260924-ui-perception.md` line 85 still says the `ui_state` verb
is NOT built, but the verb landed and prints a usage page (exit 0)*. Filed from
the chat turn, `tags=bug`, priority 1, `parent_task_id —`.

The drift is the point: the doc's own §4 prescribes "if you are reading this and
that task has landed, delete this section and verify the tool instead" — the row
is gopi, sensing gopi, telling gopi to correct itself. Evidence: the one file the
turn wrote, `uistate-handoff-drift.md`, archived with the row
(sha256 `1a3a124f7a6cc5af…`, 2792 bytes — identical at both ends). The copy
committed beside this doc is the same byte-for-byte file under the name
`uistate-handoff-drift.txt`: an evidence artifact kept *inside* `devdoc/` has to
be either classified-and-bannered as a dateless `.md` or left un-classified as a
`.txt`, and `internal/standards/docclass_gate_test.go` fails a `.md` under
`devdoc/` that has no manifest row / no `<!-- doc-class: historical -->` banner.
`.txt` is the cheap answer for raw evidence.

## 6. Evidence index (committed, not in a scratch dir)

| file | what it proves |
|---|---|
| `assets/rsi-chat-files-a-row/drive_chat_pty.py` | the driver: pty, cooked-line write, end-of-turn on the prompt reappearing |
| `assets/rsi-chat-files-a-row/chat-turn-message.txt` | the single line that produced the row |
| `assets/rsi-chat-files-a-row/chat-pty-raw.txt` | the raw pty transcript: the two tool rounds, `FILED 01a0d59c-…`, clean `/quit` |
| `assets/rsi-chat-files-a-row/chat-session.jsonl` | the chat session's JSONL: the `bash` tool_use, the `task_submit` tool_use, and the `Task queued … tags=bug` tool_result (byte-identical to `<sandbox>/.gopi/sessions/a43ed4a8-…/session.jsonl`, sha256 `81d325469f924054…`, 7721 bytes) |
| `assets/rsi-chat-files-a-row/uistate-handoff-drift.txt` | the evidence the agent itself produced and attached (the agent wrote `.md`; see §5 for why the committed copy is `.txt`) |
| `assets/rsi-chat-files-a-row/filed-row-metadata.json` | the filed row's own metadata: attachment sha256, tags, source, payload |
| `assets/rsi-chat-files-a-row/claim-funnel-proof.txt` | `[next]` lands on the RSI row once it is the only pending row — run over a COPY of the ledger (`/tmp/ledcopy.db`), never the operator's queue |
