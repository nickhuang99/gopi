<!-- doc-class: normative -->
# The four small top-level pages, layered — session / config / notify / tool

**Status:** landed (epic `01a10bb7` item 2/5, the help-hierarchy restructure).
**Surface:** `cmd/agent/cli_session.go` (`sessionUsage`), `cmd/agent/cli_config.go`
(`configUsage`), `cmd/agent/cli_notify.go` (`notifyUsage`), `cmd/agent/cli_tool.go`
(`toolUsage`).
**Guards that keep it honest:** `cmd/agent/cli_help_hierarchy_reverse_guard_test.go`
(every registered sub-command must be advertised by its OWNING page),
`cmd/agent/cli_session_test.go`, `cmd/agent/cli_notify_test.go`,
`cmd/agent/cli_tool_test.go`, `cmd/agent/cli_verbs*` / `cli_verb_census_test.go`
(the advertised direction of the census), `cmd/agent/help_width_test.go` (every
help constant ≤ `config.HelpWrapCol` = 88 display columns), and the G6 pair
(`cli_tools_maintenance_help_surface_*`).

## The rule this slice applies

**One page, one level.** A verb's `-h` answers one question — *what are this
verb's actions?* — and the answer is one line per action plus the address of the
page that describes that action in full. Everything that explains WHY the action
exists, which host owns it, what the record layout is, or which epic moved it
here is **not an action**: it is background, and its place is a devdoc.

The top-level index already followed this (`internal/config/help.go`;
`devdoc/help-page-layering-small-pages.md` applies the same rule one level down).
The reverse guard (item 4/5) states the invariant the restructure must satisfy:
every word a page's own dispatcher accepts appears, by name, ON that page.

## Measured before → after

The numbers are `./bin/gopi <verb> -h | wc -l`, on the real binary, in this
worktree (Rule 31); the binary was rebuilt before each measurement.

| page | before | after | what left the page |
|---|---|---|---|
| `gopi session -h` | 78 | 39 | the chat-index scope essay, the "unknown tokens" rule, the `[hidden]`/`[missing]` review |
| `gopi config -h` | 53 | 26 | `show`'s section list and secret rendering, `setup`'s provisioning detail, the env-attr pointer |
| `gopi notify -h` | 36 | 26 | the sink fan-out shell recipe, the record schema, the six status events, the synthetic alarms |
| `gopi tool -h` | 39 | 39 | (the group block is table-rendered; the prose half was already one sentence) |

`gopi tool -h` keeps its two blocks at the same height because its rows are
**generated** from `toolLeafCommands` / `toolGroupCommands` — one row per
command is already the layered shape, and the command count did not move. What
changed there is the prose: the three-paragraph preamble became one sentence,
and the pointer to each command's own page moved to the tail.

## What moved where, and why it is safe to move

Nothing was deleted. Each block below was **verified to exist** in its new home
before it left the page.

| moved off the page | new home |
|---|---|
| session: the chat index, `--mode`/`--source`/`--all` semantics, the local-only default and the `[hidden]`/`[missing]` markers | `devdoc/session-unknown-vs-zero.md`, `devdoc/task-list-archive-disclosure.md`, and the verbs' own pages (`gopi session list -h`) |
| session: the alias story (`log`/`stats`/`raw`/`resume` are the top-level verbs) | kept, one paragraph (`devdoc/session-verb-family.md` carries the record) |
| config: `show`'s sections and the credential rendering rule | `devdoc/config-show-verb.md` + `gopi config show -h` |
| config: `setup`'s provisioning detail, the env-attr pointer | `gopi config setup -h`, `devdoc/deployment-setup-verb.md`, `gopi --help-env` |
| notify: the fan-out shell recipe, the record schema, the status events, the alarms, the failure report | `devdoc/notify-subscription.md` (the standing contract — sections 2, 3, 5, 6, 8) |
| tool: the "one implementation, two spellings" preamble | one sentence + `devdoc/tool-group-verb.md` |

**The retention is deliberate in the other direction too.** `notify` keeps its
three ROUTES (the page's own job is to say *which of the three doors you want*),
`session` keeps the alias sentence (a reader who does not learn that
`gopi log` still works will not know both spellings are supported — asserted by
`TestSessionFamilyUsageAdvertisesEverySubVerb`), and `tool` keeps the internal
warning block (a capability with no door can only be tested by unit tests).

## The traps this slice hit

1. **A usage constant is a Go raw string; a backtick inside it truncates it.**
   The page texts quote `gopi session list` etc. from inside a raw string. The
   existing pages spell those as ``` "`gopi session list`" ``` concatenations;
   the rewritten pages drop the backticks and keep the plain word. A `+`
   injection of a backquoted literal is legal inside a **double-quoted**
   fragment only.
2. **A comment line starting with `//` inside the injected text aborts the
   import block** — the group-page texts are `const X = ` + text; a body whose
   line begins with `//` and lands above the imports turns the package's
   comments into code. The rewritten bodies carry no `//` lines.
3. **The census reads the top-level index, not these pages.** The G7 census
   (`cli_verb_census_test.go`) decides "advertised" from `gopi -h`'s own
   `^ {2}gopi <verb>` lines, so trimming a *group* page cannot push a verb out
   of the census — but the group pages still had to keep `gopi <verb> <sub>`
   spellings intact for the reverse guard, which matches each page's own
   grammar.
4. **`gopi config -h` renders through the G6 derivation.** The G6 surface is
   re-derived from the `configUsage` CONSTANT (`g6UsagePage`), compared in both
   directions against `devdoc/gopi-tools-maintenance-help-surface-inventory.md`.
   A trimmed page whose forms no longer match the inventory fails
   `TestToolsMaintenanceHelpSurfaceInventoryMatchesDerivation` and
   `Test...EveryFormIsTestedOrBlockedWithAReason`. This slice kept every
   advertised FORM on the `config` page (same five `gopi config …` rows, shorter
   prose), so no inventory row and no case entry moved.
5. **Width is a gate, not taste.** `help_width_test.go` measures every help
   constant with `config.DisplayWidth` (CJK counts two columns) at
   `config.HelpWrapCol` = 88 — the indented synopsis lines are the ones that
   overflow first. The table inside `toolUsage` legitimately exceeds it (a
   generated table's columns are data, not prose) and is a reviewed entry; the
   rewritten prose bodies are all under the budget.

## Verification

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/          # BUILD_OK
$ ./bin/gopi session -h | wc -l ; ./bin/gopi config -h | wc -l
$ ./bin/gopi notify -h | wc -l  ; ./bin/gopi tool -h | wc -l
39  26  26  39        # before: 78  53  36  39
$ for v in session config notify tool; do ./bin/gopi $v -h; done | grep -c 'gopi <verb> <sub> -h'
# each page carries its own one-line pointer block
$ go test ./cmd/agent/ -run 'TestHelpPagesAdvertiseTheirOwnSubcommands|TestEveryHelpConstantFitsTheWrapBudget|TestToolsMaintenanceHelpSurface'
ok
```

The task report (`task-report.md`, in the run's session directory) carries the
verbatim invocations and the before/after transcripts.
