<!-- doc-class: normative -->
# The chat policy — the operator's rules decide WHAT runs; the posture decides WHEN THEY ARE SILENT

**Class**: normative (the rule layer + the startup posture are the current criterion)
**Landed**: 2026-10-17 · epic `01a1510e` items 1–5 (task `01a1510e-9d42-733e-b1e0-6084810d79db`, this worktree)
**Files**: `internal/chatpolicy/{doc,decision,parse,decide,posture,defaults,category}.go`,
`internal/agent/chat_policy_posture.go`, `cmd/agent/chat_policy_flag.go`,
`internal/tools/chat_policy_gate.go`, `internal/tools/chat_write_guard.go`,
`internal/tools/chat_shell_tokens.go`
**Supersedes**: the verb-shaped judgement this package replaced — see
`devdoc/chat-write-boundary.md` (§"What landed, and where it lives" is the old
mechanism) and `devdoc/chat-write-guard-ssh-payload.md` (the payload reader that
no longer exists).

## 1. The two questions, and why they may not share a table

The chat console asks two questions about a command line, and the whole package is
built on never answering them from one place:

| question | who answers | vocabulary |
|---|---|---|
| "may this line RUN?" | the operator's ```chat-policy fence + the startup POSTURE | `allow` \| `prompt` \| `forbidden` |
| "WHAT is this action?" | the ```chat-op-category fence (`category.go`) | `operator-work` \| `repo-write` \| `long` |

The second fence is documented in `devdoc/chat-ops-authorization.md` §8; this doc
owns the first. They are parsed by the same `<word> <words…> [*]` grammar and
frozen by two SEPARATE holders (`tools.SetChatPolicy` / `tools.SetChatOpCategories`)
on purpose: the executable proof that a category edit cannot widen what runs is
`internal/agent/chat_ops_category_test.go`'s
`TestChatOpCategoryTableCannotDecideWhatRuns` (empty the category table, and the
execution arm's byte-for-byte answer for `git push origin master` does not move).

## 2. The rule table (`internal/chatpolicy`)

```chat-policy
allow git status
allow git log *
allow git worktree list *
allow make *
prompt git push *
prompt git reset *
forbidden rm -rf *
```

Three properties, each with the measurement that chose it:

- **The matcher is a WORD PREFIX, never a substring.** A rule matches when its
  words are a prefix of the command's words; a trailing `*` matches any remaining
  words (and may match none). The LONGEST matching prefix wins, and an equal-length
  tie is decided by FILE ORDER, first written wins — the operator already resolved
  that conflict by writing one line above the other, and file order needs no extra
  syntax. This is why `xmake test` no longer reads as `make test`.
- **THE PACKAGE DOES NOT TOKENISE.** Every entry point takes WORDS. The one
  tokenizer is `tools.ChatShellTokens` (→ the guard's `chatShellTokens`), and the
  one clause splitter is `tools.chatShellClauses`. A second splitter would
  eventually disagree with the first about where a command ends, and a boundary
  that tokenises differently from the engine that judges it is a boundary with a
  hole in it.
- **The whole line takes the MOST RESTRICTIVE clause** (`forbidden > prompt >
  allow`) — `git status; rm -rf build` is not judged by the clause written first.

`Defaults()` is `prompt *` — text in the SAME syntax, parsed by the SAME parser, so
the shipped table cannot drift from the operator's. It names no verb and no path:
"which commands are dangerous" is the operator's sentence, not a list compiled into
the binary. That compiled list — fourteen git subcommands, a partly-allowed pair
judged by pathspec predicates, and a refusal for every line whose references the
reader could not resolve — is what this replaced.

## 3. The posture: the answer when the rules are silent

Resolved ONCE at process start by `internal/agent/chat_policy_posture.go`, then
FROZEN into a value and handed to `tools.SetChatPolicy`. Nothing running inside the
session — a model, a tool, a later decision — can widen it.

Precedence, highest first: `--policy VALUE` → `GOPI_CHAT_POLICY` → settings.json
`"chat_policy"` → default. An unrecognised spelling is REPORTED and the next
channel decides; it is never silently read as the default.

| spelling | decision for an unmatched line |
|---|---|
| `allow` \| `yolo` \| `auto` | run it — **every** command, the rule table is not consulted at all |
| `on-request` \| `ask` \| `prompt` | ask the operator (**the default**) |
| `forbidden` \| `deny` \| `never` | refuse without asking |
| `unless-trusted` | refuse without asking — the STRICTEST reading, see below |

Two properties that are safety rather than taste:

- **The zero value is on-request, not allow.** A `Posture` a caller forgot to
  resolve asks rather than runs; `Decision()` intercepts the unset flag instead of
  letting Go's zero `Decision` (= Allow) mean a silent widening.
- **`unless-trusted` is its own spelling because an approval request with no reader
  allows nothing.** For a console whose operator is not watching, a `prompt` rule
  still raises a question, but a line the table is SILENT about is refused outright
  rather than turned into text nobody answers. It maps to `forbidden` as the
  posture; a rule that says `prompt` still says `prompt`.

`chat_op_auto` is MAPPED, not reimplemented (`true` → allow, `false` → on-request),
and the mapping prints one sentence only when `chat_policy` itself is silent AND the
answer changes — so the next operator retires the old key instead of guessing which
knob won.

## 4. The mapping from a decision to what a tool call gets back

`internal/tools/chat_policy_gate.go` is the ONE execution point
(`chatBashPolicyMatch` → `chatBashWriteRefusalIn`):

| decision | bash arm | write/edit arm (`ChatWritePathRefusal`) |
|---|---|---|
| `allow` | `""` — the command runs | `""` — the write proceeds |
| `prompt` | `[NEEDS OPERATOR APPROVAL — command NOT run]` + the deciding rule's `source:line`; the model ASKs its operator, and the operator's own affirmative in their next message runs the retried call (§4) | the same approval text, satisfied by the same turn message |
| `forbidden` | `Refused by the CHAT COMMAND BOUNDARY` — it names the decisive CLAUSE (a multi-clause line is refused by its strictest clause, so the account lists each clause's own answer) and which of the TWO CAUSES applied: a rule the operator wrote as `forbidden`, or a line the fence is silent about, in which case the startup posture answered and the missing fence line is named | `Refused by the CHAT WRITE BOUNDARY` — the same refusal, naming the rule |

**Two markers, because they answer two questions.** A bash refusal is about whether
a COMMAND may run; a write/edit refusal is about whether this console may DEPOSIT
at a path. They shared one string ("…CHAT WRITE BOUNDARY") until task `01a1ae0b`
measured the cost: a console whose fence listed `allow ls`/`allow cat`/`allow
printf` and no `allow echo` was refused `echo hello` — a command that writes nothing
— and read a text claiming a WRITE boundary had stopped it. `chatBashRefusalMarker`
and `chatWriteBoundaryRefusalMarker` in `internal/tools/chat_policy_gate.go` are the
two constants; a reader can tell the answers apart without parsing prose, and a test
on either side of the package boundary keys on them.

A PROMPT is neither an allow nor a refusal, and the constant
`chatWriteApprovalPrefix` exists so a caller (and a test) can tell the two apart
without parsing prose. There is no interactive prompt — and there WAS no way to
answer one either, which made `prompt` a deadlock rather than a question: the model
was told "ask your operator and retry only on a yes", the retry met the same rule,
and the same text came back. Under the shipped defaults (`prompt *` +
on-request) that made every bash and write call dead on a surface with no
`settings.json` document (the testsuite fixtures), measured as
`[NEEDS OPERATOR APPROVAL — command NOT run] … matched rule `*` (built-in
chat-policy defaults:2)` with the file never landing.

The answer is the OPERATOR'S OWN NEXT MESSAGE, read by
`ChatTurnApprovesTheToolQuestion` (`internal/tools/chat_approval_authorization.go`)
from the turn the agent publishes with `publishChatTurn` — the same
no-stored-state shape the cleanup gate already uses. Three properties follow, and
they are why no other design was taken:

- **it cannot become an always-on switch** — nothing is stored; a turn that says
  nothing approves nothing, and a later turn inherits nothing;
- **a negation always wins** — "don't run it" contains "run it" and must not
  authorize anything;
- **it never reaches `forbidden`** — the channel is asked only on the `prompt`
  branch, and an approved prompt then runs the same remaining gate an `allow`
  does (the cleanup faces' destructive half still needs the operator to have named
  cleanup). A settings key or an env var would have been a persistent dependency
  (Rule 35) invented to make one sentence unnecessary.

Its honest limit: the boundary holds no memory of WHAT it asked, so an affirmative
message answers the prompt decision it meets in that turn. That is bounded by what
a prompt decision already is — a command the operator's own rules marked "ask me
first" — and the approval text tells the model to retry the SAME command, never a
re-spelt one. Correlating an answer to one specific question would need state,
which is the debt this channel exists to avoid.

The write arm maps a write TARGET into the same vocabulary — the tool verb then one
word per path segment, repo-relative: `write devdoc/chat-notes.md` →
`[write, devdoc, chat-notes.md]`, so `allow write devdoc *` permits every file under
`devdoc/`. A target outside the repository is spelled by its leading segments
(`home the operator …`), which no rule mentions, so the POSTURE answers — the safe direction
for a path nobody wrote a rule for.

The ONE gate kept beside the rules is the cleanup faces' authorization
(`chat_cleanup_authorization.go`): it NARROWS an allow and is therefore asked LAST,
and only after the rules already said allow.

## 5. Evidence — the three postures, really run

The matrix below is `make test-budget`-driven, from a temporary harness inside
`internal/tools` (deleted after capture), judging the SAME thirteen command lines
under three postures against one demo fence. Raw output:
`/home/user/.gopi/sessions/01a1510e-9d42-733e-b1e0-6084810d79db/evidence/matrix-new.txt`
; the pre-change answer for the same lines (build 585507c) is
`…/evidence/matrix-baseline.txt`, and the before/after table is in
`task-report.md`.

The line the acceptance names — `git push --force origin master` under
`--policy yolo` — is **ALLOW**, because `Posture.Resolve` returns
`Match{Matched: true, Decision: DecisionAllow, Rule: {Source: "yolo"}}` before the
table is consulted. That is the declared meaning of yolo (every command runs, no
exception branch), not an oversight; a console that must not push sets
`on-request` (ask) or `forbidden`/`unless-trusted` (refuse).

Three readings worth keeping:

- **`git worktree list 2>&1` was ALLOW before and PROMPT after.** Not because the
  old answer was right: the old reader had no rule about it and no `git worktree`
  entry that matched a redirection, so it fell through to "not a mutation, allow" —
  a silence read as permission. Now the line matches `allow git worktree list *`
  (a rule the operator wrote) or the posture, and under the demo fence it takes the
  `prompt` the operator chose.
- **`ssh -o ConnectTimeout=8 peer1 uptime` was ALLOW before, and now takes the
  posture.** The old reader allowed it only because no clause looked like a
  mutation; a `ssh -o StrictHostKeyChecking=no host git push` was refused with
  `its payload is a string this boundary does not expand` — a REFUSAL FOR BEING
  UNREADABLE, which is a different fact from "the operator said no".
- **`git reset --hard HEAD~1` was refused for the wrong reason** (`~` was in the
  expansion set), and is now decided by whether a rule or the posture says so.

## 6. What this deliberately does NOT do

No verb list, no path list, no grammar for git or ssh, no process, no state file,
no daemon. The startup posture is a value, the table is text, and every function in
`internal/chatpolicy` is a pure function of its arguments (no environment, no
filesystem read) — which is what makes its tests deterministic and its behaviour
impossible to widen at runtime.
