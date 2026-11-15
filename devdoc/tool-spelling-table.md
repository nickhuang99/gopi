<!-- doc-class: normative -->
# Tool spelling: one table, and a refusal that points at a live door

**Task** `01a1144e…` (epic *spelling-table source of truth*), 2026-10-07.
**Branch** `task/01a1144e3f6e7b05b42b0ca941f0f78b`.

## The defect (measured before the fix)

The naming rule (`devdoc/cli-naming-convention.md`) says which spelling a
tool-named command has. It does **not** decide what the refusal points at — and
after the verb-retirement epic (2026-10-05) the words it points at had moved
under the `tool` group. Two arms of one refusal then disagreed:

```
$ ./bin/gopi code_search -h            # rc=1
gopi: "code_search" is not a gopi command — this one is spelled
`gopi tool code-search` on the command line …

$ ./bin/gopi tool code_search -h       # rc=1  ← still an echo of the word
gopi tool: unknown command "code_search" — this group knows: … 
Run `gopi tool -h` for what each one does (or `gopi code_search` for the
command's own page).
```

The group's hint sent the reader to `gopi code_search` — the very word the other
arm refuses. A hint that lands on another refusal is a dead end (Rule 15), and
the epic's acceptance is exactly that no refusal may point at one.

Root cause, 5-why: the hint echoed `argv[1]` (an echo, never a resolution) —
because the group had no way to ask "which command owns this word?" — because the
vocabulary was written down **three times by hand**: the refusal table
(`cli_undispatched_tool_words.go`), the naming-exception set
(`cli_completion_exceptions_test.go`), and the group's leaf list (`cli_tool.go`).
Each hand-typed dash twin was a second copy of a *derivable* fact
(`keyword_search` → `keyword-search` is a substring), so the copies could
disagree — and in the group's arm they did.

## What was built

**1. ONE spelling table** — `cmd/agent/cli_tool_spelling_table.go`.
`toolSpellingRows` holds, per tool-shaped surface: the underscore spelling
(`underscored`), the word that dispatches it (`canonical`), the whole live command
line (`door`), and the reason. Everything else is **derived**, never typed:

| derivation | what it answers |
|---|---|
| `refusedSpelling()` | the argv[0] word this row refuses — the dash twin, or the wire name when the dash twin IS the command |
| `keepsUnderscoreSpelling()` | is this row a rule-1 exception (`canonical == underscored`)? |
| `pageAddress()` / `toolRefusalPageWord()` | which command line a refusal must print for this word |

Both other copies are now **projections** of it:
`projectedUndispatchedToolWords()` (the refusal vocabulary) and
`projectedNamingExceptions()` (the rule-1 exception set). No third list exists.

**2. The group resolves its page, it does not echo the word** —
`cli_tool.go`'s `runToolSubcommand` now calls `toolRefusalPageWord(word)`, which
looks the word up in the table and answers with the row's door minus the program
word (`tool code-search`, `ui_state`), falling back to the word itself for a word
nobody claims (`gopi zzz` keeps its pre-fix line, since the group cannot invent a
page).

**3. The rc gate** — `cmd/agent/cli_tool_spelling_rc_test.go`, three table-driven
tests on the **real binary**:

- `TestUndispatchedToolWordsRcContract` — per row: the door runs (rc=0,
  non-empty page, no session); the refused spelling exits non-zero and NAMES the
  door; and the address is then read **out of the refusal's own message** and
  RUN (`assertRefusalAddressRuns`) — self-consistency as an execution, not a
  string comparison.
- `TestToolGroupWordsRefuseToALivePage` — the defect's regression, per row:
  `gopi tool <refused spelling> -h` exits 1, the page its message offers is
  extracted and run.
- `TestUndispatchedToolWordsVocabularyRcContract` — every refused word of both
  halves (spellings + synonyms) exits 1 with a refusal line and reaches no run
  path.

**4. `ui_state`'s two forms have one home** — the table's sixth row, owner
`CliVerbUIState` (with the reasoning at the row: it is a tool-*shaped* reader the
CLI dispatches, with no `toolnames` constant, and inventing one would name a tool
that does not exist). It rides the same derivations as the tool-derived rows and
was the pre-existing entry of the exception set — no change was needed, and the
deliverable was already satisfied before the table existed.

**5. Two stale tests repaired** (`cli_keyword_search_test.go`). They drove the
bare `keyword_search` and asserted the pre-retirement surface, so they were red
on master and paid no attention to the live door: `TestKeywordSearchRunPrintsHitsAndNoMatches`
would have "passed" against a refusal if anyone had relaxed it to non-zero. Both
now drive `gopi tool keyword_search`, and the run test asserts exactly what it
always claimed (hits shape, the engine's own `No matches.`, zero sessions).

**6. The `web` double door, named rather than restated.**
`TestToolGroupWordsAreTheOnlyDoor` was red on master for `web`, and both halves of
the exact invariant it asserts (dispatched, and refused) are genuinely false for
`web` — by design (`artifacts/verb-final-table-v2.md` §三: the bare `gopi web`
stays the browser chat carrier; the group owns `web search`/`web fetch`).
Measured: `gopi web -h` rc=0 and `gopi tool web -h` rc=0, onto two different
things. `toolDoubleDoorWords` now names that exemption **exactly** (one entry,
each entry argued for itself) and `TestToolDoubleDoorWordsAreReallyDoubleDoors`
keeps it honest from the other side.

## Verification

- **Build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK; `go vet ./cmd/agent/` → clean.
- **Tests:** `go test ./cmd/agent/ -run 'UndispatchedToolWords|ToolGroupWords|KeywordSearch'` → **ok 5.6s** (0 FAIL).
  `go test ./internal/toolnames/` → no test files (the package is constants only; the acceptance's second command is vacuously green by design).
  `make test-budget PKGS='./cmd/agent ./internal/config ./internal/standards ./internal/toolnames'` → recorded in the task report.
- **Ran:** every canonical spelling / live door and every non-canonical spelling, verbatim, on the built binary; full output in the task report's `## Evidence (self-filed)`.
- **Result:** 9 live doors rc=0; 14 non-canonical spellings rc=1; **every** address a refusal prints is one of those rc=0 doors.

## Not done (honest)

- The `tool` group's *leaf* list (`cli_tool.go`'s `toolLeafCommands` /
  `toolSubcommandPredicates`) still spells the tool-derived words through
  `toolnames` constants directly (it never had a hand-typed twin), so it was left
  alone. The spelling table is pinned against the live binary by the rc gate, not
  by a source-level equality with that list — a `tool` leaf added without a
  spelling row would be caught only if its refused spelling were driven.
- `TestToolGroupWordsAreTheOnlyDoor` gained an exemption rather than losing a
  defect: `web` was red before this change too, and the exemption documents why
  the invariant does not apply to it.
