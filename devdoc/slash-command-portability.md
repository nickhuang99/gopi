<!-- doc-class: normative -->
# Slash-command portability: which of `gopi chat`'s eight words can the table own

**The rule this page records.** `cmd/agent/commands.go`'s `slashCommands` table is
the ONE dispatch both carriers share (`execSlashCommand`). A word belongs in it iff
its ANSWER can be produced from facts the carrier supplies and its EFFECT is
identical on both. Everything else stays carrier-local, and this page says which is
which, because the next author will otherwise move a word that cannot be moved — or
leave one out that can (the defect this census closed: `/info` and `/context`
existed in the console only, and `gopi web` answered them with
`unknown command`).

## The census, as measured on 2026-11-08

| word | in the table? | reason |
|---|---|---|
| `/task` `/log` `/text2img` `/new` `/help` | **yes** (pre-existing) | pure engine calls / pure render; identical on both carriers |
| **`/info`** | **yes — MOVED here** | describes THIS session, and a web server holds the same facts (its one persistent agent's session logger, the model, the ground, the tools flag). Answer = `chatInfoBlock`, one renderer |
| **`/context`** | **yes — MOVED here** | exports this run's raw HTTP log; the web server holds one too (`agent.RawLog`). Answer = `ctxdump` + one line, one engine |
| `/quit` `/exit` | **no** | the web carrier is a long-lived SERVER: a shared `/quit` would end the service for every tab. Closing the tab is the browser's own equivalent, and it already exists |
| `/tools` | **no** | flips REPL state (`chatSession.toolsEnabled`) that no wire affordance can flip back; the browser chose its mode at start-up (`gopi web --tools`) |
| `/queue` | **no** | the OPERATOR's park-a-prompt seam. The browser fills the same queue by POST (`/api/chat/queue`), so one word would mean two different things — a second implementation of exactly the mechanism the epic forbids |
| `/voice` | **no** | already a web BUTTON (🎤) with real server routes (`handleVoiceStart/Stop/Cancel/Live`) behind it. Re-minting it as a command would duplicate a working surface |
| `/screen` | **no** | same shape: the browser has a 📷 button and the `/api/shot` route; the console's word captures the local screen, which the server process does not own |
| `/code-search` `/memory` | **no — retired 2026-11-07** | the model's own `code_search` / `memory` tools are the door (operator decision; `commands.go`'s retired-verb note) |

## The seam the two moved words needed

`CommandContext` had been declared with the note "ctx exists for future per-carrier
additions … without breaking the registry signature". This is that addition, and
nothing more (`Rule 35`: no new mechanism):

- `CommandContext.Session CommandSession` (`cmd/agent/commands_session.go`) is a
  struct of FACTS the carrier already holds — session id/file/dir, model, ground
  (dir/repo/in-repo), node, tools mode, raw-log path.
- The **zero value is meaningful**: a carrier that filled nothing leaves `SessionID`
  empty and `/info` REFUSES by name instead of printing a block of placeholders
  (Rule 15 — a command whose every row is "(unknown)" reads as "ran, found
  nothing").
- Each carrier fills it per command, cheaply, from its own state:
  `replCommandContext` (console, built from `chatInfoFieldsFor`) and
  `(*webServer).webCommandContext` (browser, from its one persistent agent).
  **The console's builder calls the same assembler `/info`'s own arm uses**, so the
  two doors cannot drift — `TestSlashInfoMatchesTheReplBlock` asserts byte identity.

## What stayed per-carrier, and why that is not a second implementation

`/info` and `/context` still have an arm in the REPL switch that runs BEFORE the
table dispatch. That arm is the **console seam**, not a second owner: it renders the
same bytes through `WriteNoticeRuns` so the block's labels draw dim
(`term_note_row_test.go` measures the cells). `/new` has had exactly this shape all
along (`webClearCommandName` owns the word; `serveResetCommand` owns the effect).
Ownership of the VOCABULARY is the table's; ownership of the rendering band is the
carrier's.

## Discoverability had to move with ownership

`chatSessionWords` (`lineedit_wire.go`) is the completion menu's list of
REPL-native words. It carried `/info` and `/context`; once the table owns them, a
second entry is the duplicate row `/new`'s alias note exists to remove, so both
were removed from it. The console `/help` page (`cli_chat.go`'s `chatUsage`) also
listed them under "Slash commands"; that listing was deleted for the same reason —
`slashHelpText()` prints every table word automatically, and the measured run
showed `/info` appearing TWICE before the deletion (once in the console list, once
in the table section).

## Verification

```bash
go build ./... && go vet ./cmd/agent
make test-budget PKGS='./cmd/agent'
printf '/help\n/info\n/context\n/quit\n' | ./bin/gopi chat --once
curl -s -X POST -H 'Content-Type: application/json' -d '{"message":"/info"}' \
     http://127.0.0.1:PORT/api/chat     # → event: command {"command":"/info","output":"…"}
```

The new tests, all through the REAL dispatch (never the Run closures directly — a
test that bypassed dispatch would have stayed green through the defect, Rule 31):

- `commands_session_test.go` — `/info` from carrier facts; its refusal on a
  no-logger carrier; `/context` exporting through a real raw logger and printing
  ONE line; its refusal on a no-log carrier; its argument grammar unchanged;
  `/help` listing both words; both words leaving `chatSessionWords`; and the
  console/browser byte identity.
- `commands_session_web_test.go` — the same two words over `/api/chat`, read off
  the `command` SSE frame: the block carries this server's model, the live session
  UUID and its pasteable replay command; `/context` refuses WITH ITS CAUSE (never
  `unknown command`); `/help` lists both; the tools row follows `--tools` at the
  moment of asking.
