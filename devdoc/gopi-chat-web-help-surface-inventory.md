<!-- doc-class: normative -->
# G5 inventory — the chat/UI + web-tool command group's advertised surface

Truth class: **normative** — a standing claim about master, re-derivable at any
commit (registered as such in `devdoc/DOC-INDEX.tsv`).

This is the G5 deliverable of the help-surface hardening epic: every verb and
flag the group's help advertises, whether a **basic** test drives it, and — for
everything that is not driven — the honest reason and what exercising it would
take. `-h` is never counted as a run (PROJECT.md Rule 31); where the only faithful
probe would be `-h` reaching a handler, the row says so in plain words instead
of claiming coverage.

## MERGE NOTE (rework run 01a110043f0e7999936adfcbcdb7dbb9) — two G5 reworks met here; all three halves are wanted

<!-- Three doors now name this surface's family address, and the third was the
one current master lacked:
  (1) master 86cdb3162 (sibling rework aa6a83f28): the `gopi tool proxy` table
      row, the test-map row, the gap row, and `derive "tool proxy"` in
      scripts/g5-chat-web-help-surface.sh.
  (2) master 86cdb3162 ALSO carries the "NINE lines against this table's EIGHT
      rows … `chat list <TAB> <TAB> 0` and is a scope anchor" paragraph. Kept
      deliberately: measured TRUE by this run (the derivation prints 10 lines,
      `chat list` → 0), and the archived tip 995cdfe1f does NOT contain it
      (line-set diff: master-unique == that paragraph). It is master's
      restatement of the same intent, not a lost edit.
  (3) THIS branch, commit 8acc520bb — `g5DerivationScript` +
      TestG5DerivationScriptDrivesEverySurfaceAddress in
      cmd/agent/cli_chat_web_help_surface_basic_test.go, byte-identical to the
      archived post-image (a2482ff068049b1d4d0c20caec932a15d6c9399b). This is
      the gate that fails the build when door (1) ever spells the surface with
      the retired bare word, which the dispatcher refuses (exit 1). -->

## How to re-derive this table

```bash
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
scripts/g5-chat-web-help-surface.sh ./bin/gopi
```

The script reads each surface's page from the binary's own usage text and
prints `surface <TAB> flags <TAB> count`. It prints NINE lines against this
table's EIGHT rows on purpose: `chat list` is still derived, because its
synopsis head is what scopes `chat backfill`'s block on the shared
`chatListUsage` page — the row itself is gone (below), so the extra line reads
`chat list <TAB> <TAB> 0` and is a scope anchor, not a surface.
A surface is named by the ADDRESS the dispatcher accepts — since
verb-retirement phase 1 that is the family form (`gopi tool proxy`, not the
retired bare `gopi proxy`, which exits 1 with a refusal naming its replacement).
A row naming the retired spelling derives `0` flags and under-reports the group
by that surface's whole flag set, which
`TestG5DerivationScriptDrivesEverySurfaceAddress` now fails the build on.
The same derivation runs inside the
build: `cmd/agent/cli_chat_web_help_surface_basic_test.go` derives the flags from
the usage **constants** (`chatUsage`, `webUsage`, `termUsage`, `proxyUsage`, …)
and `TestG5InventoryMatchesTheHelp` reads this table back and fails if either
side moved. Both directions are checked, so a flag added to the help without a
row fails, and a row claiming a flag the help no longer prints fails. The
`**Totals:**` line under the table is read the same way — it is the number a
reader quotes, so it is gated against the derivation rather than maintained by
hand (`g5ExpectedSurfaces` / `g5ExpectedFlags` in the test file are the
non-vacuity pin behind it, so a scanner that stops seeing synopsis lines fails
instead of reporting a smaller surface as "the help").

The derivation rule (a flag is the surface's iff its `--token` sits on a
**synopsis head**, a **bracketed continuation**, or a **flag definition line**)
is what keeps prose out: `webFetchUsage`'s own description of its JSON payload
names `--max-chars`, and a whole-page grep reports it twice. `--help` is excluded
by name — it is the universal spelling a reader already typed, not a knob of the
surface.

`gopi chat backfill` shares its page with the RETIRED `gopi chat list` (the
single `chatListUsage`), so the row is scanned from **its own synopsis block**.
Without that scope the row would be handed the retired word's flags (`--db`,
`--limit`, `--model`), which is a table that looks authoritative and is wrong —
and the retired half's synopsis was withdrawn from that page in the same change,
since a page advertising a word the dispatcher refuses is the help lying.

## The surface table

| # | surface | advertised flags | basic test drives it | why not | what it would need |
|---|---|---|---|---|---|
| 1 | `gopi chat` | `ack-timeout-ms,ambiguous-width,chat-ideal-geometry,chat-surface-turns,cols,film,film-terminal,from-turn,max-beats,max-seconds,max-turns,no-color,no-dump-screen,no-editor,no-screen-stream,once,out,policy,prompt,replay,resume,rows,scratch,screen-replay,stop-on,tools,verbose,voice-audio,wechat` | yes | — | — |
| 2 | `gopi chat backfill` | `dry-run,repair-first-msg,verbose` | yes | — | — |
| 3 | `gopi web` | `addr,thinking-live,tools` | yes | — | — |
| 4 | `gopi web search` | `count,method` | yes | — | — |
| 5 | `gopi web fetch` | `max-chars` | yes | — | — |
| 6 | `gopi cli` | `addr,once,tools` | yes | — | — |
| 7 | `gopi cli --once` | `once,prompt,tools` | yes | — | — |
| 8 | `gopi tool proxy` | `anthropic-base-url,anthropic-key,anthropic-model,listen,openai-base-url,openai-key,openai-model,upstream,verbose` | yes | — | — |

**Totals:** 8 surfaces, 53 advertised flags, 8 driven by a basic test, 0
surfaces with no test at all. (`gopi chat list` held row 2 with `db,limit,model`
until the verb restructure retired it — the index has ONE door, `gopi session
list` — which is why the count moved 47 → 44 and the surface count 9 → 8: the
table was re-derived with `scripts/g5-chat-web-help-surface.sh`, never edited by
hand.) (`cli --once` is listed separately from `cli`
because the help documents it as its own face — "the shape a script drives" —
with its own flags and its own exit-status contract. The two one-shot faces are
now symmetric: `gopi chat --once` carries the same `once,prompt` spellings as
`gopi cli --once`, so `gopi chat`'s flag list grew from 4 to 6 in the commit that
added the one-shot face — the count is re-derived, never incremented by hand. It
grew again, 6 → 9, in the commit that added the pipe-back driver's
`--turns`/`--max-seconds`/`--stop-on`, for the same reason: the derivation is the
source, the table is its transcription. It grew once more, 9 → 10, in the commit
that added `--resume` — the flag a crashed chat is continued with — again by
re-running `scripts/g5-chat-web-help-surface.sh`, never by editing the number. It
grew past 13 to 26, then to 27 (2026-10-27) in the commit that
renamed the pipe-back cap `--turns` → `--max-turns` and gave
`--ambiguous-width` the row it never had — both re-derived, never edited by
hand. grew once more, 10 → 13, across the commits that added the replay surface
(`--replay`), the dump's off switch (`--no-dump-screen`), and this one's driver
flag (`--film DIR`) — every time re-derived with the script above and
transcribed, never incremented by hand.)

Where each row's test lives, and what it actually runs:

| surface (plain) | test | the invocation it drives |
|---|---|---|
| gopi chat | `TestG5ChatRunsAsAdvertised`, `TestChatOnce_FlagSpelling_OneTurnThenExit` (testsuite/chat_once_test.go) | `gopi chat --tools --no-editor -v <raw-log>` with `/quit` on stdin, then an unresolvable seed; and the one-shot face: `gopi chat --once -p "say OK"` / `echo … \| gopi chat --once --tools` |
| gopi web | `TestG5WebServerRunsAsAdvertised` | `gopi web --addr HOST:PORT --tools`, then `GET` the printed URL |
| gopi web search | `TestG5WebSearchRunsAsAdvertised`, `TestG5WebSearchMethodNeedsAValue` | `gopi web search "<q>" --method wiki --count 3` |
| gopi web fetch | `TestG5WebFetchRunsAsAdvertised`, `TestG5WebFetchNonPositiveRestoresTheDefault` | `gopi web fetch <url> --max-chars 1200` and `--max-chars=40` |
| gopi cli | `TestG5CLICarrierRunsAsAdvertised` | `gopi cli --addr HOST:PORT`, then `gopi cli open --addr …` |
| gopi cli --once | `TestG5CLIOnceRunsAsAdvertised` | `gopi cli "<prompt>" --once`, `-p "<prompt>"`, `--once --tools` |
| gopi chat list (RETIRED — no row: the word is refused by name, and the index is `gopi session list`) | `TestG5ChatListAndBackfillRunAsAdvertised` asserts the retirement contract itself | `gopi chat list --db <ledger>` must exit non-zero naming `gopi session list` |
| gopi chat backfill | `TestG5ChatListAndBackfillRunAsAdvertised` | `gopi chat backfill --dry-run --verbose --db <ledger>` |
| gopi tool proxy | `TestG5ProxyRunsAsAdvertised` | `gopi tool proxy --upstream openai --openai-* … --listen HOST:PORT`, then a TCP connect |

## The honest gap list

These are capabilities the help advertises that **no test in this group
exercises**. They are not surfaces (no argv drives them on their own), so they
have no row above; `TestG5InventoryListsEveryGapTheTestsCannotCover` gates this
table against the same list in the test file, so an entry cannot be dropped
quietly — deleting a "why not" is how a partial run starts reporting full
coverage.

| gap (what the help promises) | why the tests here cannot exercise it | what exercising it needs |
|---|---|---|
| `gopi web search` — the real search engines (bing/google/arxiv/github/deepseek) and the merged default | each method is a python3 plugin that talks to a live engine; the network AND the operator's plugin credentials are outside a test's reach, and the briefing forbids standing in a fake engine | a network egress plus the plugins' own API keys (the operator's `~/.gopi/plugins/websearch` config); the routing contract (which script, which argv) is what the tests here cover instead |
| `gopi web fetch` — a real page fetched through the search-bot VPS jump host | the fetch runs on a remote jump host with the operator's own trust relationship; a test that reached it would be testing that host, and stubbing it would prove nothing about it (measured: a direct fetch of `example.com` answers HTTP 403, i.e. the jump host is the only working path) | the jump host's access (ssh target + any proxy credentials) and network egress; the URL/flag routing and the documented payload are what the tests here cover instead |
| `gopi web --addr=<LAN address>` — the non-loopback bind, the per-run access token and the LAN URL | the claim is about ANOTHER machine reaching this one; a second host on the LAN is not available inside a test, and binding `0.0.0.0` in a test would only re-test the same process | a second device on the LAN (or a second network namespace) to open `?t=<token>` from; the loopback bind, the served page and the token plumbing are what the tests here cover |
| `gopi tool proxy` — the Anthropic→OpenAI translation against a real upstream provider | the translation needs an upstream that speaks the OpenAI wire protocol and a key for it; `internal/proxy`'s own unit tests cover the translation, and no credential for a provider is available in a test | an OpenAI-compatible upstream plus its API key (the operator's `--openai-key`); the argv admission, the listener and the refusals are what the tests here cover |
| `gopi cli` — the chat page RENDERED by a real text browser (links2/w3m-class) | the carrier only execs a browser with the `/cli` URL, and the vendored links2 cannot run in this sandbox (measured: `links -dump https://example.com` answers "This version of Links does not contain SSL/TLS support", and under the test harness it dies with "Epoll ADD(1) on fd 0 failed: Operation not permitted") | a terminal/X11 environment where the vendored links2 can open an event loop, plus visual confirmation of the rendered page; the carrier's contract (server started, `/cli` URL computed, browser exec'd with argv) is what the tests here cover |
| `gopi chat --tools` / `gopi cli --tools` — a real TOOL-CALLING turn (the model calls a tool and the result is rendered) | the tests here assert that the toolset is SHIPPED on the wire and that the carrier's tool frames reach fd 3 (the pre-existing in-process gates), but no turn in this file has the model actually call a tool: that needs a scripted multi-round provider conversation | a stub model scripted to answer round 1 with a `tool_use` and round 2 with text (the shape `cmd/agent/cli_term_once_test.go` already drives in-process), driven through the CLI entry point |

## What the stand-ins are (and what they are not)

The tests use two kinds of stand-in. Neither is a mocked peer, and the gap list
above is where their limits are recorded rather than hidden:

- **The plugin-script directory** (`GOPI_WEBSEARCH_SCRIPTS_DIR`) — the CLI's
  documented contract is "run `<dir>/<method>_search.py` / `<dir>/web_fetch.py`
  as python3 and print its JSON", so a real subprocess answering the documented
  argv *is* the surface. This removes the search engine, not the surface.
  (`internal/tools`'s own tests stub the same seam.)
- **The model endpoint** (`DEEPSEEK_BASE_URL`) — a local server answering the
  Claude SSE dialect, so the non-interactive faces can be driven end to end.
  This removes the provider, not the surface.
- **The text-browser binary** (`GOPI_LINKS_BIN`) — a script that records its
  argv. The carrier's whole contract with a browser is "exec it with the chat
  URL", which is exactly what is asserted. What this cannot prove — that a real
  browser renders the page — is a gap row above.

A browser, a LAN peer, a jump host and a provider credential are **not** stubbed
anywhere: those surfaces have gap rows.

## Defects this run found (each fixed or filed, never papered over)

The probe found three disagreements between the group's help and its code. All
three are fixed in this branch, each with the red→green transcript in the test
that covers it:

1. **`gopi web fetch --max-chars` refused the values its own help blesses.** The
   page says "Values `<= 0` restore the default"; the parser rejected them with
   `--max-chars must be a positive integer`. The tool layer already clamps
   (`tools.WebFetch` → `clampWebFetchMaxChars`), and the page claims this CLI is
   the same function path, so the two had to agree. Fixed in
   `cmd/agent/cli_webfetch.go`; pinned by `TestG5WebFetchNonPositiveRestoresTheDefault`
   and by `TestParseWebFetchNonPositiveMaxCharsRestoresDefault`.
2. **`gopi web search --method` with no value was silently accepted as
   `merged`**, while its sibling `--count` refused a missing value. The help
   spells the flag `--method X`; omitting the flag is how merged is asked for, so
   a bare `--method` is now a usage error in `cmd/agent/cli_websearch.go`, pinned
   by `TestG5WebSearchMethodNeedsAValue`.
3. **`gopi chat backfill --db PATH` was advertised and ignored.** The synopsis
   lists `--db PATH` for both chat-index verbs, but the export happened inside
   the `list` branch only, so `backfill` parsed the flag, accepted it, and then
   read the *default* database (measured: `--db <a fresh ledger>` failed with
   "unable to open database file" while `GOPI_CHAT_DB=<same path>` succeeded).
   Fixed in `cmd/agent/cli_chat.go`; pinned by the by-effect assertions in
   `TestG5ChatListAndBackfillRunAsAdvertised`.

Two more defects are **filed, not fixed here** (they live outside this group's
word — reported rather than rewritten, per the briefing):

4. **`gopi chat backfill` fails on a sandbox whose default database has never
   been created.** Reproduced on a fresh `GOPI_HOME`:

   ```
   $ gopi chat backfill --dry-run
   gopi chat backfill error: unable to open database file: out of memory (14)   exit=1
   $ gopi task list >/dev/null          # creates <home>/.gopi/memory.db + its dir
   $ gopi chat backfill --dry-run
   chat backfill: 0 legacy auto row(s) to migrate
   chat backfill: migrated=0 skipped(unreadable-log)=0 total=0 dryrun=true      exit=0
   ```

   Root cause: the chat index opens `chatDBPath()` directly and the parent
   directory (`<home>/.gopi`) does not exist yet, so SQLite cannot create the
   file and reports its generic OOM-shaped error. The ledger path works because
   `gopi task list` ensures the directory first. Filed as a bug task (the fix
   is a `MkdirAll` in the chat index's open, or a refusal that names the path) —
   reported rather than rewritten, per the briefing.
   (This paragraph replaces a withdrawn entry: an earlier probe of this run read
   `--count 0` as accepted, because its exit status came from the `head` in a
   pipeline rather than from gopi. Re-measured directly, `--count 0` IS refused
   — `gopi web search: --count must be a positive integer, got "0"`, exit 1. The
   lesson is recorded rather than the claim: an exit status read after a pipe
   measures the pipe.)
