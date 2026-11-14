<!-- doc-class: normative -->
# CLI naming rule — underscore vs dash/space

> **PARTLY REVERSED 2026-11-06 — the command this page names is BACK, minus one flag.**
> `gopi memory sync` was restored the day after its deletion, as a MANUAL door:
> one invocation = one session, exit. `--peer` / `--push` / `--pull` / `--dry-run` /
> `--serve-assets` / `--timeout` all run again; the runnable shape and its measured
> two-way session are in `devdoc/memory-sync-manual-door-restored.md`. **What is
> still gone is the `--every <dur>` cadence flag**, whose reader and loop went in
> batch 5 — so a line below that carries `--every` is a RECORD, not a command to
> paste (it exits 1 with `unknown flag`). The one-way copy `gopi memory backup`
> is the other manual cross-host door and its guarantees differ: it REPLACES the
> peer ledger instead of merging it.

Every `gopi` command word is spelled by ONE rule. The rule is short because
the vocabulary is small; it is stated here (not invented per command) so a new
command can be placed by reading it, and so a reader can tell a deliberate
exception from an accident.

## The rule

| # | Kind of word | Spelling | Where the value comes from | Examples |
|---|---|---|---|---|
| 1 | **Tool-derived** — the command runs an LLM tool under the tool's own wire name | **underscore**, byte-identical to `internal/toolnames` | `toolnames.ToolXxx` (the argv predicate, the completion candidate and the contract-suite token all reference the constant) | `vision_analyze`, `screen_capture`, `voice_input`, `keyword_search`, `text2img` (single word — no separator to normalise) |
| 2 | **CLI verb** — a command gopi itself names | **dash** (`kebab-case`) when the word has more than one token; a single lowercase token otherwise | `cliVerbs` constants (`cmd/agent/cli_verbs.go`), or `internal/taskverbs` for `gopi task <verb>` | `code-search`, `scratch-gc`, `audit-merges`, `sweep-worktrees`, `tag-resumed`, `merged-cleanup`, `sweep-zombies`, `memory`, `task`, `merger`, `memsync`, `brew`, `shot`, `act`, `log`, `raw`, `stats`, `resume`, `checkpoint`, `proxy`, `web`, `chat`, `worker`, `version`, `completion` |
| 3 | **Group sub-verb** — the word after a group verb (`gopi <group> <sub>`) | spelled inside its family (dash where multi-token), and the argv token is the same word | the family's own list/switch (`memorySubcommandNames`, `taskverbs.FamilyVerbs`, `webCompletionNames`, …) | `memory sync`, `memory provenance`, `task audit-merges`, `task dedup-scan`, `web search`, `web fetch` |
| 4 | **Deprecated alias** — an old spelling kept dispatching | *frozen*: whatever it was, forever. It never gets re-spelled | the constant that named it at deprecation time | `web_search`, `web_fetch`, `memsync` (deprecated flat verbs), `gopi task serve` (alias of `gopi merger`) |

### Why the split exists (the rule's reason, not just its shape)

- A **tool-derived** word is a *wire identifier*. `vision_analyze` is the exact
  string the model emits and the exact string a session log records. Renaming
  the CLI word would make "the command" and "the tool call in the log" two
  different strings for one thing — grep-ability of logs is the feature. The
  underscore is not a style choice there; it is the tool's own name.
- A **CLI verb** is a *user-facing word*. gopi's whole vocabulary (flags,
  files, other verbs) is dash-cased, and `MAN`/`git`/`docker` convention is
  kebab-case for multi-token command words. So a two-token CLI verb is
  `code-search`, never `code_search`.
- The two must not be conflated: `code-search` is the CLI face of the
  `code_search` TOOL (Rule 14 — the name is *deliberately* not the tool
  constant, so a tool rename cannot silently rename a user's command).

### The one precedent that looks like a counterexample

`web_search` → `web search` (2026-09). The tool-named command was NOT
underscored as `web_search` in its canonical form: the canonical spelling is the
**group form** `gopi web search` (group verb + sub-verb, rule 3), and the
underscore flat verb became the deprecated alias (rule 4). The lesson is that a
*group* is the natural home for a pair like search/fetch — not that rule 1 is
wrong.

## The deliberate exceptions (rule 1 kept, each with its reason)

These three are **tool-derived words that stay underscored** — no deprecation
path, no dash alias. Each reason is also carried at the code site, so the
decision survives without this document.

| Command | Constant | Why it stays underscored (and is NOT deprecated) |
|---|---|---|
| `gopi vision_analyze` | `toolnames.ToolVisionAnalyze` | The command is a thin, byte-identical face of the `vision_analyze` tool call (`gopi vision_analyze <image>` → the same engine path). `vision`/`vision-analyze` would be a *new name for the same thing* — a second surface for one function, which is the exact defect this epic removes. |
| `gopi screen_capture` | `toolnames.ToolScreenCapture` | Same reason; additionally the word is the tool_use name recorded in every computer-use session log, so the CLI spelling is a *lookup key* into those logs (`cli_log.go` alias `capture` → `screen_capture`). |
| `gopi voice_input` | `toolnames.ToolVoiceInput` | CLI-only surface (no model tool yet — see the constant's doc), and the only tool-derived word with no wire-name collision pressure. Its help prose already says "voice input" while the command stays the constant's value; that prose/command split is accepted and documented here rather than fixed by inventing a second command name. |
| `gopi keyword_search` | `toolnames.ToolKeywordSearch` | The command is a byte-identical face of the `keyword_search` tool call (`tools.KeywordSearch`, the same engine path the model reaches), and the word is the `tool_use` name in every session log that ran a text search — so the CLI spelling is a lookup key into those logs (`gopi log --search` lists exactly `code_search`/`keyword_search`). Before 2026-09-26 it was in NO dispatch chain: `gopi keyword_search -h` printed the TOP-LEVEL page byte-for-byte (measured: identical md5, the word occurring zero times) and `gopi keyword_search <query>` fell through the dispatch chain into the bare-prompt path — a real agent run with a session directory, live `[auto]` ledger row and tokens (EUAUDIT slice 4 TOOLS-1, task `01a0dde8…`). |

**Not an exception: `memsync`.** It is *not* a tool-derived word (no
`toolnames` constant names it) — it is a CLI verb (rule 2) that happens to be a
single lowercase token, which is why it is spelled without a dash. Its sibling
`gopi memory sync` is the group form (rule 3) of the same subject; the epic
resolved that pair by cross-referencing the two helps and re-homing the
`--dedup-scan` audit — see `devdoc/cli-help-dedup.md`.

### The other half of the rule: a NOT-dispatched tool spelling is REFUSED BY NAME

The rule above says which spelling a command HAS. It does not yet say what
happens when a user types the OTHER one — and until 2026-09-26 the answer was
the worst possible one: the word fell through the dispatch chain (the verb is
read from the FIRST argument only) and became a **PROMPT**, so
`gopi code_search EUAuditWidget` started a real agent run — a session
directory, a live `[auto] code_search EUAuditWidget` ledger row and tokens —
for a word the operator typed as a command (`code_search` is the *tool wire
name*; the command is `code-search`, rule 2).

The shape that replaces it: a tool-named word whose command exists under a
DIFFERENT spelling is refused by name, with a pointed hint at the canonical
spelling and the explicit escape hatch (`--prompt <word>`), rc=1, no session,
no ledger row. The refusal vocabulary is small ON PURPOSE — one row per
spelling of a real command this CLI deliberately does not dispatch.

**The vocabulary is CODE, and this table is a reading of it** (spelling-table
epic, 2026-10-07). Both lists below — the refused spellings and the rule-1
exceptions — are PROJECTIONS of the ONE table in
`cmd/agent/cli_tool_spelling_table.go`: the refused word is derived from the two
spellings (`refusedSpelling`), the exception set is that table's rule-1 half
(`keepsUnderscoreSpelling` — named for the VALUE it answers, per Rule 20), and the address a refusal prints is the row's live
door. So the tables here are a description of the code, never a second source
for it, and a rename moves every surface at once.

| Refused word | Points at (the LIVE door) | Why that word is not a command |
|---|---|---|
| `code_search` | `gopi tool code-search` | the tool wire name; the CLI verb is deliberately dash-cased (rule 2) |
| `keyword-search`, `vision-analyze`, `screen-capture`, `voice-input` | the underscore spelling, reached through `gopi tool <word>` | tool-derived commands keep the tool's own name (rule 1) |
| `ui-state` | `gopi ui_state` | the reader surface is spelled with the underscore |
| `web-search`, `web-fetch` | `gopi tool web search` / `gopi tool web fetch` | the dash twins of the retired flat words; the group is the live home (rule 3) |

**The address is the DOOR, not the word** — and this is measured, not
stylistic. Until 2026-10-07 the `tool` group's refusal echoed the word it was
given, so a tool WIRE name was corrected into a second refusal: `gopi tool
code_search -h` exited 1 and said "(or `gopi code_search` for the command's own
page)", while `gopi code_search -h` exits 1 with "this one is spelled `gopi tool
code-search`". A hint that lands on another refusal is a dead end (Rule 15), and
it is now impossible by construction: the group resolves the page through the
same table (`toolRefusalPageWord`), and every address a refusal prints is RUN by
the rc gate.

Every OTHER unknown word stays ordinary prompt text (`gopi frobnicate …` still
runs): the refusal is a statement about **this binary's own surface** — "that
command exists, spelled differently" — never a guess about what the operator
meant. That is also why each row carries its address: a hint pointing at
nothing would be worse than the run it replaces.

## The `web` exception: one word, two doors, declared in ONE place

The naming rules above say which spelling a command HAS. This section records the
one place where a word is deliberately reachable by **two** doors — and why the
tests that guard the "one door" rule were re-stated for it rather than relaxed.

**The facts, measured on the built binary (2026-10-07, task
`01a1144e-17f1-7219-a9cc-786ed8246982`):**

| invocation | result |
|---|---|
| `gopi tool -h` | the group page; its Grouped block lists `web <search|fetch>` |
| `gopi tool web -h` | the **chat-UI page** (`gopi web — serve a local browser chat UI …`), exit 0 |
| `gopi web -h` | the SAME page, byte for byte (`md5` of both outputs compared equal) |
| `gopi tool web search -h` / `gopi web search -h` | the same page, byte for byte |
| `gopi tool web fetch -h` / `gopi web fetch -h` | the same page, byte for byte |
| `gopi web nosuchsub` | refused by name (`unknown subcommand "nosuchsub" — want search|fetch`), exit 1, no session |

So `web` is not two commands: `toolSubcommandPredicates["web"]` **is**
`isWebSubcommand`, the same predicate `cmd/agent/main.go`'s dispatch chain calls.
Both doors hand the same argv to the same function; the group adds no second
implementation, no second page and no second exit status.

**Why the second door is kept, and not closed either way.** The two obvious
"fixes" are both wrong, and each was measured:

- **Refuse `gopi web` and make the group the only door** — that withdraws the
  browser chat UI server (`--addr`, `--tools`, `--thinking-live`, the LAN token
  banner), a live surface with its own help page, its own contract inventory
  entry and its own contract tests. It is not a rename; it is the removal of a
  running command. Nothing in this epic authorised it, and the refusal would be
  the very "unreachable behind its own refusal" defect the one-door rule exists
  to prevent — reported as a *retirement* while the capability is still wanted.
- **List `web` in the group table while pretending it is a normal row** — that is
  what the report under repair caught: `gopi tool -h` advertised a row whose
  top-level spelling was neither refused nor reconciled, i.e. the help promised a
  refusal that does not happen while a reader is told twice, in two contradicting
  leads, what the word is.

What survives both objections is the third shape: **the exception is declared
once, printed on the page that raises the question, and pinned by tests that
assert what is true instead of asserting the general rule at a word it does not
describe.**

**Where the exception lives (Rule 13/14 — one definition, referenced).** One
constant in `cmd/agent/cli_tool.go`:

- `toolGroupSecondDoorWord` (`= CliVerbWeb`) — the declaration;
- `toolGroupSecondDoorReason` — its reason, ONE string, read by the page and by
  the tests;
- `toolGroupSecondDoorNote()` — derived from the table, so a page for a table
  without the exception carries **no** paragraph about it (the sentence cannot
  outlive the thing it describes);
- `toolGroupWordIsADeclaredSecondDoor(word)` — the predicate both gates read, so
  a second exception cannot be added by widening a list in one place.

**How the one-door rule is held now.** `TestToolGroupWordsAreTheOnlyDoor` keeps
asserting, for every OTHER word, that it is *not* dispatched at top level and
*is* refused by name. For the declared word it asserts the two facts that make a
second door harmless — **dispatched yes** (a declaration for a word no longer
dispatched at top level is STALE and fails: delete it) and **refused nowhere**
(a word that is both dispatched and refused is unreachable, and the exception may
not smuggle one past the gate). `TestToolGroupForwardsToTheSameImplementation`
keeps its word-by-word run of both doors and, for that word, replaces "the bare
spelling must refuse" with the stronger claim that **both spellings print the
same bytes** — which is exactly what the general rule protects for every other
word, asserted at the level the exception makes true.
`TestToolGroupDeclaredSecondDoorIsPrintedAndPinned` adds the three things a
silent exception would rot into: the table really carries it, `gopi tool -h`
really prints the reason, and the two doors really print one page. The reference
to this section is IN the reason string, so a reader who meets the exception on
the page is sent here.

**Not a precedent for the next one.** The declaration is deliberately a single
named word, and the test fails if a second grouped row is declared without its
own reason (`%d of the grouped rows are declared second doors, want exactly 1`).
A genuine second door needs its own argument, measured the same way.

### How the naming rule is held (not just written)

| Surface | Who enforces it |
|---|---|
| the ONE spelling table | `TestUndispatchedToolWordsRcContract` (`cmd/agent/cli_tool_spelling_rc_test.go`) — per row, on the real binary: the door runs (rc=0, non-empty page), the refused spelling does not (rc≠0), and the address the refusal PRINTS is run (`assertRefusalAddressRuns`). The self-consistency the epic asks for is here, as an execution rather than a string comparison |
| the group's page address | `TestToolGroupWordsRefuseToALivePage` (same file) — `gopi tool <refused spelling> -h` exits 1, the page it offers is extracted from the message and RUN |
| the whole vocabulary, both halves | `TestUndispatchedToolWordsVocabularyRcContract` (same file) — every refused word (spellings + synonyms) exits 1 with a `gopi:` refusal line and reaches no run path |
| the designed double door | `TestToolDoubleDoorWordsAreReallyDoubleDoors` (`cmd/agent/cli_tool_test.go`) — `web` is exempted from the one-door rule ONLY while it is both dispatched and group-served |
| tool-derived words use the constant | `TestCLIToolMirrorVerbsMatchConstants` (`cmd/agent/cli_toolnames_contract_test.go`) |
| the rule is READABLE where the commands are listed | `TestCLINamingExceptionsDocumentedInTopLevelHelp` (`cmd/agent/cli_completion_exceptions_test.go`) — the top-level help must state the rule and name every exception. NOTE for editors: the renderer wraps by display width and never re-joins, so the asserted sentence must fit ONE emitted line (it lives on its own line in the `Commands` block, not in a section title) |
| the completion exceptions are conscious, never accidental | `TestSafeExceptionsAreStillNeeded` (`cmd/agent/cli_completion_exceptions_test.go`) — each exempt token must still dispatch AND still differ from its dash-cased twin. The set itself is now the spelling table's rule-1 half (`projectedNamingExceptions`), not a hand-kept list |
| the canonical help surface | `cliContractInventory` (`cmd/agent/cli_contracts_test.go`) pins one entry per command word |
