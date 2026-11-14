<!-- doc-class: historical -->
# Two cmd/agent completion defects: the `annotate` entry and the `/q` one-candidate fixture

Task `01a0eef7-1604-7e7d-97f9-af12081b2708`, branch
`task/01a0eef716047e7d97f9af12081b2708`, base master `5b15ed8f` / tree
`2b1e6dda`. Both failures reproduce on a tree byte-identical to master — they are
not this branch's regression:

```
--- FAIL: TestCompletionCandidateSetsCoverTopLevel (0.00s)
    cli_completion_test.go:47: topLevelCommandNames contains "annotate" but no dispatcher handles it
--- FAIL: TestSlashMenuOneCandidateStatesWhyTheArrowCannotMove (0.00s)
    slash_menu_test.go:258: the production completer offers 2 candidates for "/q" (want exactly 1, …)
        --- menu ---
          /quit          (alias of /exit)
          /queue         (queue a prompt for the next turn)
```

## Defect 1 — `annotate`: the completion table was RIGHT, the test's expectation had drifted

### Which side was wrong (measured, not assumed)

The test's message — "no dispatcher handles it" — is true of the loop it runs,
which compares the completion table against a **hand-maintained `want` literal**,
not against `main()`'s dispatch chain. Measured on the clone at `5b15ed8f`, all
five facts about `annotate` say the verb is real:

| evidence | measurement |
|---|---|
| dispatched | `main.go`'s chain calls `isAnnotateSubcommand(dispatchArgs)`; `cli_annotate.go:171` runs the verb |
| contract suite | `cli_contracts_test.go:186` maps `CliVerbAnnotate → "AnnotateSubcommand"`, and `:229` lists that predicate in `cliContractDispatchPredicates` — the drift guard asserts that constant equals the predicates parsed out of `main.go` (`TestCLIContractInventoryMatchesDispatch`) |
| help §1 | `internal/config/help.go:607` — `{"gopi annotate", "Draw marks onto an EXISTING PNG (--out)."}` |
| prompt guard | `config.PositionalVerbs` refuses `annotate` as prompt text (`go test -run TestPositionalVerbGuardVocabularyMatchesDispatch` names it the moment a verb is un-dispatched) |
| the real binary | `./bin/gopi -h` line 120; `./bin/gopi annotate x.png` → exit 1 with `--out is required: annotate writes a NEW png and never edits the input in place` |

So the honest verdict: **`topLevelCommandNames` is correct; the dispatcher-side
expectation in the test was incomplete.** `annotate` is a top-level verb with its
own advertised help row, promised to be completable since `9ac9f83d`
("fix(annotate): finish the verb's wiring — completion, help mapping, verb census
row"). The entry is no dead suggestion; the test's stale list was.

(The candidate "either a dispatcher exists behind the completion entry or the
entry goes" resolves to the first arm — and it was already the case; what was
broken is the expectation.)

### Why the failure appeared, and what closes the drift class

`want` is a hand-transcribed list of literals. It is a mirror of two other
registries (the dispatch chain and `dispatcherOfToken`), and `annotate` fell out
of it while both of those kept the verb — `9ac9f83d` updated the completion
table, the cli_contracts maps, the help index and the census row, and left the
completion test's own literal untouched.

Two changes (Rules 1/13/14 — declare once, reference explicitly, let a bad edit see
its own error instead of a bare `contains` failure):

1. `want` gains `CliVerbAnnotate` **and** `CliVerbCompletion` — the latter was
   previously skipped by a bare `if v == "completion"` string literal (Rule 1b:
   a keyword compared inline), which also hid it from the reverse direction.
2. `dispatcherOnlyVerbs` — ONE declaration in `cli_completion_test.go` naming the
   seven-to-five words the completion table deliberately omits, each with its
   reason (the same rule `taskCompletionNames` follows for `gopi task serve`).
   Both readers reference it:
   * its own direction 2/4 assertions: a table entry must either be in `want` or
     be a declared omission, and a declared omission may not carry an advertised
     home;
   * `cli_verb_guard_test.go`, whose inline list of omitted verbs (`CliVerbCompletion,
     CliVerbRaw, CliVerbStats, CliVerbAct, CliVerbCLI, ToolWebSearch, ToolWebFetch`)
     was a **transcribed copy that disagreed with the table it described** —
     `CliVerbCLI` is in the table, `CliVerbCompletion` is not. That copy is gone.

The class is closed by an explicit assertion rather than by a bool: the
declaration is checked against the table it describes, and the guard is checked
against the declaration, so a verb cannot leave the completion table unnoticed in
either file (the same "a new verb without a dispatcher fails red" property, read
from one place).

### The minimal repro assertion

`TestCompletionCandidateSetsCoverTopLevel` fails at `0.00s` on `dispatcherOnlyVerbs`
sliding out of step with `topLevelCommandNames` (a declared omission offered), on
a table entry in neither list (the `annotate` shape), and on `annotate` itself
being declared dispatcher-only.

## Defect 2 — `/q`: the prefix reached two commands, so the ONE-candidate fixture moved to `/qui`

### Which side was wrong

Both sides were individually right and the fixture's premise had expired, exactly
as the test's own comment anticipated ("a table change that widens the match fails
LOUDLY here rather than silently turning the test into a two-candidate case"):

* The production table is correct: `chatSessionWords` (`lineedit_wire.go:853`)
  carries `quitCommandName` **and** `chatQueueCommandName` — `/queue` was added by
  `ff76758c` ("feat(chat): /queue — park a prompt without interrupting the turn in
  flight"), both are REPL-native session commands a chat user can run.
* The menu's matching is genuinely prefix-based (`strings.HasPrefix(c.Text, prefix)`
  in `slashCandidates`), so `/q` legitimately offers both.

So this is **a genuine two-candidate menu, not an ambiguity to resolve by
disambiguation**: `/quit` is the REPL's own word for the behaviour `/exit` also
spells (the alias note on the row), `/queue` is the queue verb — different
behaviours, different words, both real. Nothing was collapsed, renamed or
uniq-prefixed.

### The fix

The one-candidate contract needs a word that is a prefix of exactly ONE command.
Measured against the shipped vocabulary, `/q` is 2, `/qu` is 2, `/qui` is 1, and
`/queue` shares `/qu`. The fixture therefore moved to the **shortest** single-match
prefix, and both halves of the seam are now pinned in the same file:

| constant | value | role |
|---|---|---|
| `slashSingleCandidateWord` / `slashSingleCandidatePrefix` | `qui` / `/qui` | the one-candidate contract (the note row, the arrow no-op, Enter) |
| `slashTwoCandidateWord` / `slashTwoCandidatePrefix` | `q` / `/q` | the prefix the fixture vacated — now the ordinary two-candidate menu it IS |
| `completionSlashPrefix` | `/` | referenced, never re-typed (Rule 1b/13) |

`TestSlashMenuTwoCandidatesWrapsAndOffersNoReason` (new) is what keeps the vacated
prefix under test instead of leaving a gap: `/q` offers exactly 2, `Menu.Rows()`
equals the candidate count with **no** note row (`lineedit.CompletionMenuNote()`
absent from the grid), Down moves the marker onto the second row and clears it
from the first, and Enter-then-Enter submits the second candidate. At two
candidates the arrow is not a no-op and the reason row must not be a permanent
banner — the mirror of the one-candidate contract.

### The minimal repro assertion

`TestSlashMenuOneCandidateStatesWhyTheArrowCannotMove` asserts `len(cands) == 1`
for its fixture prefix (the way it always did — that assertion is what caught the
widening), and now additionally asserts each offered candidate is a word the
REPL's OWN vocabulary accepts (`chatReplCommandNames()` — the table's words plus
`chatSessionWords`). A `lookupCommand`-only check was tried first and was wrong:
`/quit` is a session word the shared registry does not own, so the table-only
check both fails on a correct menu and proves nothing about the carrier.

## Verification

* `go build ./...` — OK; `gofmt -l cmd/agent/` clean for the four touched files.
* `go test ./cmd/agent/ -count=1 -run 'TestCompletionCandidateSetsCoverTopLevel|TestSlashMenu…|TestPositionalVerbGuardVocabularyMatchesDispatch|TestCLIContract…|TestTopLevelHelpCoversEveryDispatchedSubcommand|TestCLIUserVerbTokensAreAllFromCliVerbs|TestBashScriptStructure|TestZshScriptStructure|TestFishScriptStructure'` — `ok github.com/nick/gopi/cmd/agent 8.576s`.
* Real surface (Rule 31): `./bin/gopi -h` prints the `gopi annotate` row (line
  120); `./bin/gopi completion bash` emits `annotate` in the top-level `compgen -W`
  list and **not** `act`; `./bin/gopi annotate /nonexistent.png` exits 1 naming the
  missing flag, i.e. the dispatcher answers under the name completion offers.

## What was NOT done

* The ~11 other failures in a whole-`./cmd/agent` run are environmental (the suite
  reports the `/dev/shm` scratch root) and out of this task's scope, per briefing.
* No new tool, no new config knob, no new file outside `cmd/agent/*_test.go` +
  this devdoc; `internal/config/help.go` needed no edit (the `annotate` row is
  there).
