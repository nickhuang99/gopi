<!-- doc-class: historical -->
# The chats index records WHO held the console

Task `01a101b3-e158-7a14-b68a-73015803d3cc`, 2026-10-03.

## 1. The defect, measured

`gopi session list --mode chat` is the operator's only index of his own
conversations. On 2026-10-03 it listed 222 `chat` rows, of which
`Say the single word: pong. Do not call any tool.` appeared 21 times — turns 1,
tokens 0/0, workdirs under `workspace/gopi-01a0ffbb…`. Those were not
conversations. They were **verification and replay runs driving the REAL `gopi
chat` binary in the operator's own profile**, and every one of them left a row
rendered exactly like a person's.

The producer is ordinary code, not a bug in isolation:
`cmd/agent/cli_chat.go:3149` → `Agent.RegisterChatSession`
(`internal/agent/chatregister.go:85`) → `tools.ChatEnsureRow`
(`internal/tools/chat.go:276`). Each verified run also left its own session
directory (`session.jsonl`, `*.raw.log`, `chat-screen.dump`, `console.log`,
`metadata.json`) — 11 of them, 1 332 016 bytes measured — while the tasks ledger
held **no** row for any of those session ids. So the conversations existed in the
chats index and nowhere else, with nothing to distinguish them.

## 2. Why it did not run in a private profile

Nothing in the chat path isolates a profile. `cmd/agent/main.go:1250-1258` is the
only place a gopi process publishes its execution context, and the chats row
lands wherever `GOPI_HOME` resolved — which, for a probe run from an agent's own
shell, is the operator's. The isolation exists and is cheap (`gopi sandbox`,
Rule 34); the defect is that a VERIFICATION path reached the operator's index
without using it, and nothing downstream could tell.

## 3. The fix: provenance, not hiding

Two properties were required and they pull against each other — an operator must
be able to tell verification traffic from his own conversations, and the rows
must not simply vanish (distinguishing is not hiding).

**`chats.source`** carries `internal/tasksource`'s existing vocabulary
(`human|agent|worker|merger|server|forward|agency-proposal|mirror-terminal`) —
the SAME words the tasks ledger's own `source` column uses, so a reader learns one
provenance vocabulary for both tables. It arrives on the operator's live index by
the `ensureSchema` ALTER pattern the tasks table already uses (`CREATE TABLE IF
NOT EXISTS` never touches an existing table; a duplicate-column error is the
no-op case).

**The reading must be taken before it is destroyed — the correction that made the
field work.** The first implementation derived provenance at row-write time and
recorded `human` for a probe driven from an agent shell. The cause is an ordering
fact: `exportAgentSessionAnchor` (`cmd/agent/agent_spawn_guard.go`) re-exports the
agent-chain anchor `GOPI_AGENT_SESSION` with the running process's **own** pid at
startup, because for the spawn guard that is exactly right — this process IS the
session, and a re-exec'd agent must read as self. A chat row is written minutes
later, on the first completed turn, so a live derivation there sees the process's
own anchor and concludes "no agent above me". `agent.CaptureConsoleProvenance()`
now takes the one reading while the anchor is still the INHERITED one
(`sync`-free bool latch + pair, reset-able so the contract is testable), and
`chatSessionSource()` answers with that snapshot. The guard's semantics are
untouched; the tasks ledger keeps deriving live, because its rows are written by
the process that would have exported the anchor.

The snapshot is held in an `atomic.Bool` latch + `atomic.Value` rather than a
plain pair or a `sync.Once`: the write happens on the startup path and the read
happens later from whichever goroutine first registers a chat (a `gopi web` turn
is not the main goroutine), and a `Once` cannot be reset — so the ordering
contract would have been untestable, which is how this defect shipped in the
first place. `resetConsoleProvenanceForTest` lives beside the state it resets and
no production path can reach it.

**The render never guesses.** A row with no provenance prints
`tools.ChatSourceUnrecorded` — the word `(unrecorded)`, not a blank and not
`human`. The 222 pre-existing rows therefore say what is true about themselves:
nobody recorded who held that console. They are also askable FOR by name
(`--source (unrecorded)`), which is the only way to reach them, since their stored
value is the empty string. `session list --source person` is REFUSED with the
accepted vocabulary named — a typo that returned an empty list would be
indistinguishable from "the operator has no sessions" (Rule 15).

Surfaces carrying the fact: the `session list` table row, `session list --json`,
`session show` and `session show --json`. One predicate
(`tools.chatSourceFilter`) backs both the table and the JSON faces, so they cannot
disagree about which rows matched.

## 4. The existing rows: disposition, and why nothing was rewritten

Measured on `~/.gopi/memory.db`: 21 `chat`-mode rows whose first message is the
probe text, 25 including `web` mode; 60 `chat` rows with turns ≤1 and tok 0/0.
Every one of them now renders `(unrecorded)`.

**They were not back-filled with `agent`, and that is the deliberate call.** A
row's provenance is a fact about the run that wrote it, and the only current
evidence is the first-message text — i.e. a pattern match asserted as history. The
column exists precisely to stop provenance from being fabricated; manufacturing it
retroactively from a guess would defeat the fix in the same commit that ships it.
What the fix owes those rows is that they stop being counted as the operator's,
and `(unrecorded)` delivers that without asserting anything untrue.

Their own files (11 dirs, ~1.3 MB) were left in place: they belong to those runs'
session directories, and deleting another run's record is not this task's decision
(Rule 35 also forbids standing up a cleaner for it — the sandbox/scratch GC that
already ticks is the only reclaimer this repo adds).

## 5. Honest limits

- The signature travels in the **environment**. It proves "no agent chain sat
  above this process when it started"; it cannot prove a person was typing.
  `env -u GOPI_AGENT_SESSION gopi chat` still records `human`. This is the same
  evidence the tasks ledger already trusts, and it is strictly better than a
  column that recorded nothing.
- `gopi web` / `gopi ui` share the read path but have no PTY probe in this task's
  evidence; their row write goes through the same `RegisterChatSession`, and the
  web row's own `source` was verified in the index, not end to end.
- The live operator index was read through a COPY
  (`~/.gopi/sessions/01a101b3-…/evidence/operator-ledger-copy.db`); the migration
  runs on the real file the next time a chat run opens it.

## 6. Verification recipe (reusable)

The before/after of an operator-facing index is cheap to produce and worth
keeping:

```bash
EV=~/.gopi/sessions/<task-id>/evidence
git archive HEAD | tar -x -C $EV/prefix-src          # the PRE-fix tree, same commit
(cd $EV/prefix-src && CGO_ENABLED=0 go build -o $EV/gopi-prefix ./cmd/agent/)
cp ~/.gopi/memory.db $EV/operator-ledger-copy.db     # never point a probe at the live ledger
GOPI_MEMORY_DB=$EV/operator-ledger-copy.db $EV/gopi-prefix session list --mode chat > $EV/before.txt
GOPI_MEMORY_DB=$EV/operator-ledger-copy.db ./bin/gopi     session list --mode chat > $EV/after.txt
```

`git archive` never touches a worktree (Rule 29), the ledger copy keeps the
operator's append-only file out of reach (Rule 32), and the two binaries are the
same commit's before/after — which is what makes the diff evidence rather than
narrative.
