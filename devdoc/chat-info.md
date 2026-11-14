<!-- doc-class: historical -->
# `/info` — the chat REPL prints its own session identity

**Task:** `01a0fd00-fe4b-77cb-9eb1-2df040cedadb`
**Status:** implemented, unit + end-to-end tested.

## The problem

A person sitting in a `gopi chat` REPL could not answer the first question any
other face asks of a session: **what is this session's id?** The UUID exists from
the first instant (`session_start`), is printed nowhere the operator can see, and
is the handle every other surface takes — `gopi log <id>`, `gopi stats <id>`,
`gopi chat --resume <id>`. Recovering it meant leaving the REPL, guessing at
`~/.gopi/sessions/<newest>`, or reading the screen dump.

The same gap covered everything else about the running session: which directory
it stands in, which repo that directory belongs to, which model answers, whether
tools are on *right now* (the flag `/tools` flips mid-session), which node it is.
The banner says some of it once, at the top, and scrolls away.

## The shape — a REPL-native word, beside the two that already exist

`/info` is the third REPL-native command, after `/queue` and `/tools`: a word
whose **subject is the running process**, which the shared command table
(`commands.go`) cannot see, so it is answered in the REPL's own switch rather
than dispatched from the table.

The wire-up is six readings of ONE constant, all in `chat_info.go`'s file except
the three call sites:

| surface | where | what it contributes |
|---|---|---|
| `chatInfoCommandName` | `chat_info.go` | the word — every other site compares against it |
| `chatInfoUsage` | `chat_info.go` | `/help info`, and the `/help` page's row |
| REPL switch arm `case "/" + chatInfoCommandName` | `cli_chat.go` | answers the line |
| `chatNativeUsage` | `commands.go` | `/help <cmd>` reaches the block |
| `chatReplSlashVocabulary` | `cli_chat.go` | same |
| `chatSessionWords` + `chatSessionHints` | `lineedit_wire.go` | tab-completion and the menu hint |

Nothing is dispatched twice and no second mechanism was invented: the word is
answered by the switch that already answers `/queue`, and its help text comes out
of the same `chatNativeUsage`/`chatReplSlashVocabulary` pair.

## The design decisions, and why

### 1. No short alias, and no shortest-unique prefix

The epic forbids both. `/i` and `/in` are refused **by the existing refusal
vocabulary** — measured in a live session:

```
you> /i
[gopi] unknown command "/i" — available: /clear, /code-search, /exit, /help, /info,
/log, /memory, /new, /queue, /quit, /task, /text2img, /tools; try /help
```

The rule is enforced as a **property**, not a list: `TestChatInfoPrintsNoShortAlias`
takes the word and asserts each *proper prefix* of it is refused by all four
consumers (the `/help` page, the registry lookup, the alias table, the native-usage
lookup) and is absent from the accepted vocabulary. A second spelling added later
fails the test without anybody remembering to extend it.

### 2. The renderer is a pure function of a struct

`chatInfoBlock(chatInfoFields) string` decides nothing and reads nothing;
`chatInfoFieldsFor(ag, sess)` is the ONE place that knows where each fact comes
from. That split is what lets the acceptance assertions run against **the exact
bytes the REPL prints** without a terminal — the same shape `chatBannerSpec` uses.

### 3. Every value is read from the surface that already owns it

A **second derivation** is how two surfaces come to describe one session
differently, with no way for the reader to tell which one lied. So `/info` reads:

- the UUID and JSONL path from the **session logger** (`ag.SessionLog`), not from
  a directory scan;
- the directory from `chatStartupDir` — the same accessor the session banner names;
- the node from `chatBannerNodeIdentity()` — the same helper the banner uses;
- the tools spelling from `chatBannerToolsValue`;
- the **printed recipe's words** from `chatFlagResume` (the parser that consumes
  it), `CliVerbChat` and `CliVerbLog` — never typed inline, so a printed recipe
  cannot name a flag or a verb the binary does not accept.

### 4. Copy-cleanliness is a two-layer property

The epic's "no `▌` gutter" is a statement about **how the REPL paints a line**,
not about a string. A block that is clean in the middle of an answer band but
wrapped in the band's gutter when echoed is still uncopyable. So the block goes
out through `WriteLines` — the seam `/help`'s panel uses, which commits on the
answer band with no turn gutter — and it is asserted at both layers:
`TestChatInfoBlockIsCopyClean` walks the rendered bytes, and
`TestChatInfoBlockRendersOnTheBandWithNoGutter` walks the **resulting cell grid**.
The second one is the one that would catch a future change to the band's painting.

### 5. Undeclared facts print a spelling, not a blank

On a host whose topology was never declared, `chatBannerNodeIdentity()` returns
`""`. A blank invites the reader to fill it in with a guess, so the renderer
prints `chatInfoNotDeclared`. The same for a session with no logger (a refusal
arm a test drives directly): `chatInfoNoSession`, not an empty line (Rule 15).

## The measured end-to-end result (Rule 31)

Run inside a real sandboxed profile (`gopi sandbox create`, `GOPI_HOME` pointed at
it), piped input, `chat_info.go` built from this branch:

```
$ printf '/info\n/exit\n' | GOPI_HOME="$SB" bin/gopi chat
session info:
session  eedd09b1-8378-4c6e-bae1-efaad821a41d
session dir  $SB/.gopi/sessions/eedd09b1-8378-4c6e-bae1-efaad821a41d
session file  $SB/.gopi/sessions/eedd09b1-8378-4c6e-bae1-efaad821a41d/session.jsonl
model  deepseek-flash
cwd  /tmp
repo  (not inside a git work tree)
node  peerbox (server)
tools  tools off

copy (these lines are runnable as they stand):
gopi chat --resume eedd09b1-8378-4c6e-bae1-efaad821a41d
gopi log eedd09b1-8378-4c6e-bae1-efaad821a41d
```

The three faces agree: the printed UUID, the session directory's own name
(`ls ~/.gopi/sessions/`) and `gopi log --last`'s `Session:` line are the same
string. Both copy lines were then extracted **byte-exactly from that stdout** and
pasted into a shell.

## The one pitfall worth recording

The `--resume` line was first written `gopi chat <id> --resume <id>`, on the
reading that a positional seed and the flag would compose. The binary **refuses
the pair**: a positional seed plus `--resume` is a usage error (exit 2). Measured:
`gopi chat --resume <id>` and `gopi chat <id>` each succeed, `gopi chat <id>
--resume <id>` does not. The printed line is therefore `gopi chat --resume <id>`.

The residual, stated plainly: on a session that never completed a turn (no
`→ REQUEST` checkpoint), `--resume` refuses *by design* and says so —
`gopi chat: resume: no → REQUEST checkpoint found in <path>`. That is the resume
path's own contract, not a defect in the printed line, and `gopi log <id>` — the
other printed line — runs on the same session with exit 0.


