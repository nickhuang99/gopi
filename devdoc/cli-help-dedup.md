<!-- doc-class: historical -->
# cli-help-dedup — one function, one help text (the deprecation contract, applied)

Worktree `/home/user/workspace/gopi-cli-help-dedup`, branch `feat/cli-help-dedup`, base
`6bc0dfeb` (current master). The contract being applied is the one already
established for `web_search` → `web search`: **the old spelling still
dispatches, it prints ONE pointer line, it is ABSENT from the top-level `gopi
-h` index (the canonical spelling is there instead), and the CLI contract suite
pins it.**

The `web_search` template (`cmd/agent/cli_websearch.go`, inventory entries in
`cmd/agent/cli_contracts_test.go`) is the literal model for every case below —
no new deprecation mechanism was invented.

Case A and Case B touch `cmd/agent/cli_tasks.go` / `internal/taskverbs`, which
sibling task **01a0a1e7** (`feat/cli-task-surface-restructure`, still zero
commits at the time of writing) also owns. The changes here are therefore the
smallest rebaseable form: **one new `switch` arm** in the `-h` scan plus one
shared pointer function for Case A, and **one new constant + one switch arm**
for the re-homed audit. No `tasksUsage` rewrite, no divergent family text.

---

## The naming rule (underscore vs dash/space)

Stated in full, with its reason and its exceptions, in
**`devdoc/cli-naming-convention.md`** (this epic's deliverable), summarised in
the top-level help's tools section so the user sees it where the underscore
commands are listed:

| # | Kind of word | Spelling | Value comes from | Examples |
|---|---|---|---|---|
| 1 | **Tool-derived** — the command runs an LLM tool under the tool's own wire name | **underscore**, byte-identical to the tool | `internal/toolnames` (`toolnames.ToolVisionAnalyze`, …) | `vision_analyze`, `screen_capture`, `voice_input`, `text2img` |
| 2 | **CLI verb** — a command gopi itself names | **dash** (`kebab-case`) when multi-token; single lowercase token otherwise | `cliVerbs` consts (`cmd/agent/cli_verbs.go`), `internal/taskverbs` for `gopi task <verb>` | `code-search`, `scratch-gc`, `audit-merges`, `sweep-worktrees`, `merger`, `memsync` |
| 3 | **Group sub-verb** — the word after a group verb | spelled inside its family (dash where multi-token) | the family's own list (`memorySubcommandNames`, `taskverbs.FamilyVerbs`, `webCompletionNames`) | `memory sync`, `task audit-merges`, `task dedup-scan`, `web search` |
| 4 | **Deprecated alias** — an old spelling kept dispatching | *frozen* forever — never re-spelled | the constant that named it at deprecation time | `web_search`, `web_fetch`, `memsync` (kept), `gopi task serve` |

**The rule's reason (not just its shape).** A tool-derived word is a *wire
identifier*: `vision_analyze` is the exact string the model emits and the exact
string a session log records, so spelling the CLI word differently would make
"the command" and "the tool call in the log" two strings for one thing —
greppability of logs is the feature, not a style slip. A CLI verb is a
*user-facing word*: gopi's vocabulary is kebab-case (`git`/`docker`/`man`
convention), so a two-token verb is `code-search`, never `code_search`. The two
must not be conflated — `code-search` is the CLI face of the `code_search`
*tool* and is deliberately **not** the tool constant (Rule 14: the name is not
derived from the constant, so renaming the tool cannot silently rename a user's
command).

**The exception list (each with its reason; §5 of the naming devdoc, also
carried at the code site):**

| Command | Why it is an EXCEPTION (kept, not deprecated) |
|---|---|
| `gopi vision_analyze` | Byte-identical face of the `vision_analyze` tool call. `vision` / `vision-analyze` would be a *second name for the same function* — precisely the defect this epic removes, so normalising it would create the bug, not fix it. |
| `gopi screen_capture` | Same reason, plus the word is the tool_use name recorded in every computer-use session log (the CLI spelling is a lookup key into those logs). |
| `gopi voice_input` | CLI-only surface (no model tool yet). Its help prose says "voice input" while the command keeps the constant's value; that prose/command split is documented here rather than "fixed" by inventing a second command name. |

`memsync` is **not** an exception: it is a CLI verb (rule 2) that happens to be
one lowercase token, which is why it carries no dash.

---

## Case A — `gopi task serve -h` (was: the whole family page)

**Defect.** `runTasksSubcommand`'s `-h` scan (`cmd/agent/cli_tasks.go`) had an
arm for the sync family only; every other verb — including the pre-split
`serve` — fell through to `fmt.Print(tasksUsage)`, the ~400-line family page.
The **run** path already printed a one-line alias pointer
(`mergerProgFamily`/`mergerProgTopLevel`), so the same spelling behaved like
two different commands depending on whether you asked for help. The operator
who typed the old spelling had to diff two pages to learn which spelling is
canonical.

**Fix.** One new `-h` arm for `taskverbs.Serve` calling a single shared function
`printTaskServeAliasPointer()` — the SAME function shape the run path's stderr
note uses, so the two surfaces cannot drift:

```
gopi task serve is a deprecated alias — the merge daemon is now the top-level 'merger' (see 'gopi merger -h')
```

Dispatch is untouched (the arm prints and `os.Exit(0)`; the run path is
unchanged), `-h` stays side-effect free (nothing takes `serve.lock` — asserted
by the contract test), and every verb word inside the pointer comes from a
constant (`taskverbs.Serve`, `CliVerbMerger`, `mergerProgTopLevel` — Rule 1b/13).

## Case B — `memsync` vs `memory sync` (evidence, then decision)

### Evidence

**Git history of the two entry points.**

| Entry point | Born | Commit | Purpose at birth |
|---|---|---|---|
| `gopi memsync` | M4 file-patch sync | `8c244f3a` | node→node knowledge **and ledger** transfer as **patch files** (`--export`/`--import`, later `--export-tasks`/`--import-tasks`, `--export-proc`/`--import-proc`) |
| `gopi memory sync` | 2026-09-02 | `840106a8` | ONE live bidirectional **ssh session** copying `distilled_qa` **ASSET rows** directly (unique-only superset, no file) |

**Live users of the `memsync` spellings** (`keyword_search`): `scripts/e2e-m418`,
`scripts/e2e-m420`, `scripts/e2e-m421` drive `memsync --export-tasks` /
`--import-tasks`; the M4 scripts and the devdocs reference `--export`/`--import`
and `--export-proc`. **No script, hook, service unit or doc uses
`memsync --dedup-scan`** other than the M4.8 devdoc that introduced it.

**Can `memory sync` express what `memsync` does?**

| `memsync` capability | `memory sync` equivalent |
|---|---|
| `--export` / `--import` (knowledge rows → JSON patch) | **no** — `memory sync` is live ssh only, no file, no review step |
| `--export-tasks` / `--import-tasks` (task-ledger rows) | **no** — `memory sync` copies ASSET rows only |
| `--export-proc` / `--import-proc` (procedural/workflow rows) | **no** |
| works when the two nodes are not simultaneously up | **no** — needs both endpoints reachable |
| `--dedup-scan` (twin task-row audit) | **no** (and this is the one thing that does not belong to a sync verb anyway) |

### Decision

**They are siblings, not old-and-new — so NO deprecation was invented** (the
briefing forbids inventing one the operator did not authorise, and the evidence
does not support one: `memory sync` is not a superset, so deprecating `memsync`
would delete real capability). Resolution, per the briefing's second branch:

1. **Both helps cross-reference the other BY PURPOSE.** `gopi memsync`'s usage
   gained a "Sibling channel — `gopi memory sync` (not the same command)"
   paragraph ("use it when both nodes are reachable and you want convergence
   now; use THIS command when the transfer must be a reviewable patch file"),
   and `gopi memory sync`'s usage names its patch-file sibling. The spellings
   are shared constants (`memorySyncProg`), so the two helps can never disagree
   about what the other is called.
2. **The top-level index distinguishes them** (`internal/config/help.go`):
   `gopi memsync` is described as *"The patch-FILE sibling of `gopi memory
   sync`…"* instead of the previous context-free "Node-to-node knowledge and
   ledger sync, as patch files."
3. **`--dedup-scan` re-homed** to a maintenance surface: the twin-TASK-ROW
   audit is a read-only **ledger** report, so it answers to the ledger's verb,
   `gopi task dedup-scan [--live] [--db PATH]` (`internal/taskverbs.DedupScan`,
   added to `FamilyVerbs`; routed to the SAME engine call
   `tools.TaskDedupScan`, so one implementation remains). The retired
   `gopi memsync --dedup-scan` spelling **still dispatches** and is **refused**
   with a one-line pointer to its new home — never silently doing the same work
   in two places.

## Case C — the underscore commands

Per §"The exception list" above: `vision_analyze`, `screen_capture`,
`voice_input` are **documented deliberate exceptions with per-command reasons**
(not deprecated). Rationale for choosing the exception path over the
deprecation path: rule 1 says a tool-named command keeps the tool's spelling
because the underscore IS the tool's identity — adding a dash alias would
introduce the very "two names for one function" defect this epic removes, and
would break log greppability. The exceptions are **re-checked mechanically**
rather than trusted: `cmd/agent/cli_completion_exceptions_test.go`
(`TestSafeExceptionsAreStillNeeded`) asserts each exempt token still (a)
dispatches, (b) really is undashed (its dash twin is not a command) and (c)
still carries its `toolnames` constant; `TestCLIToolMirrorVerbsMatchConstants`
holds the "a tool word is spelled by its constant" half of the rule.

---

## Verification (acceptance 4 & 6 — commands actually run, real binary)

Build + suites (branch `feat/cli-help-dedup`, worktree `~/workspace/gopi-cli-help-dedup`):

```
$ go build ./...                                    → BUILD_OK
$ go test ./cmd/agent/... ./internal/taskverbs/... ./internal/config/... -count=1
  ok  github.com/nick/gopi/cmd/agent           298.665s
  ok  github.com/nick/gopi/internal/taskverbs    0.128s
  ok  github.com/nick/gopi/internal/config       2.036s
```

`gopi task serve -h` is **exactly one line** (`wc -l` = 1) and exits 0; the
isolated-`GOPI_HOME` run creates **no** `serve.lock` (both re-checked against
the final binary). `gopi task dedup-scan --db <scratch>` prints
`task: 0 rows scanned, 0 twin pair(s)`, exit 0.

Two failures were found by that run and FIXED before the green run above:

1. `TestFamilyVerbsMatchConstants` (internal/taskverbs) — adding
   `taskverbs.DedupScan` to `FamilyVerbs` without updating the test's
   independent `want` list. The list in `verbs_test.go` is a deliberate second
   copy ("a constant defined but not listed would silently produce an
   inconsistent enumeration"), so the fix is to add the new verb to BOTH sides.
2. `TestTopLevelUsageIsAlignedAndTerse` (cmd/agent) — the tools-section TITLE I
   extended with the naming rule rendered as one 157-col line. Root cause, not
   parameter tweak: `renderSection` wrote the TITLE raw, so a long title could
   always break the format contract. The renderer now wraps the title through
   the same `wrapText` (Rule 1d — every emitted line is wrapped, and it applies
   to every future long title, not just this one).
3. `TestCLINamingExceptionsDocumentedInTopLevelHelp` (cmd/agent) — after (2),
   the phrase `keeps the tool's own underscore spelling` was *split across two
   wrapped lines*, so `strings.Contains(usage, phrase)` was false. This is a
   genuine conflict between two contracts, and it was resolved by DATA, not by
   weakening either test: the naming rule now lives on its OWN line
   ("A tool-named command keeps the tool's own underscore spelling" +
   "(devdoc/cli-naming-convention.md).") where the phrase fits one line, and
   the section title shrank back to `Tools (the agent's own tools, from the
   shell):`. Adding the rule to the help body is what makes it readable
   *and* assertable — the width budget still holds for every line.

   Worth recording for the next agent: the renderer wraps by display width and
   does NOT re-join wrapped lines, so **any sentence a test asserts via
   `strings.Contains(usage, …)` must be short enough to occupy a single
   emitted line.** A rule printed in a section TITLE can never satisfy that
   when the title is long, which is why the rule went into the body.

The first run also failed two `task_session_cwd_e2e_test.go` cases with "bin/gopi
is OLDER than cmd/agent/cli_tasks.go" — a stale-binary guard, not a code defect;
the binary is rebuilt with the change (`CGO_ENABLED=0 go build -o bin/gopi
./cmd/agent/`).

Every spelling touched, run against the rebuilt binary (`head -1` of each):

| Command | First output line |
|---|---|
| `gopi task serve -h` | `gopi task serve is a deprecated alias — the merge daemon is now the top-level 'merger' (see 'gopi merger -h')` (exactly 1 line, exit 0) |
| `gopi merger -h` | `GoPi merger — merge finished branches into master, then rebuild.` |
| `gopi task dedup-scan -h` | `GoPi task subcommands — the task-as-memory ledger (跨会话追溯/审计).` |
| `gopi task dedup-scan --db <scratch>` | `task: 0 rows scanned, 0 twin pair(s)` (exit 0; the audit is the same engine call, run against a scratch DB — the shared ledger DB is large enough to block >2 min) |
| `gopi memsync -h` | `GoPi memsync — node→node sync (git-PR semantics). Two channels:` + the `Sibling channel — gopi memory sync (not the same command)` paragraph |
| `gopi memory sync -h` | `GoPi memory sync — cross-node memory-asset sync (unique-only).` + `Also reachable as gopi memsync — the SIBLING git-PR channel` |
| `gopi memsync --dedup-scan` | `gopi memsync --dedup-scan: moved — the twin-task audit is 'gopi task dedup-scan' (see 'gopi task dedup-scan -h'); gopi memsync now only transfers rows` (exit 1, dispatch KEPT) |
| `gopi vision_analyze -h` | `GoPi vision_analyze subcommand — expose the vision_analyze tool as a CLI command.` |
| `gopi vision-analyze <img>` | not a command — falls through to the AGENT LOOP: it printed the top-level `gopi -h` header (which is what the loop's usage looks like), and only the global flag parser rejected `--nonexistent-flag-zz` (`gopi: unknown flag --nonexistent-flag-zz`). The dash twin is NOT a spelling (`TestSafeExceptionsAreStillNeeded` asserts its absence from dispatch and from completion). |
| `gopi screen_capture -h` | `gopi screen_capture — capture the LIVE X11 desktop to a PNG` |
| `gopi voice_input -h` | `GoPi voice input — speech-to-text via Fun-ASR-Nano.` |
| `gopi web_search -h` | `note: gopi web_search is deprecated — use gopi web search` (the precedent, unchanged) |
| `gopi task serve --every 1m` (run path) | stderr `gopi task merger: deprecated alias — the merge daemon is now the top-level 'gopi merger' …` then `another merge daemon is already running … refusing double-serve` (exit 1 here only because a merger already holds the lock — the alias REACHED the daemon path) |

Side-effect check (the `-h` contract): `GOPI_HOME=$(mktemp -d …) gopi task serve -h`
creates **no** `serve.lock` under that home — a help query never takes the
daemon's lock (also asserted in-process by the contract test).

Top-level index (`gopi -h`), the acceptance's "absent from the index" half:
`gopi merger` is listed; `gopi task <subcommand>` enumerates the family WITHOUT
`serve` (it is an alias, not an advertised verb); `gopi memsync` is described as
the *patch-FILE sibling of `gopi memory sync`*; `dedup-scan` appears in the
task family enumeration; the underscore tools keep their own section, whose
title carries the naming rule. `web search` (canonical) is indexed, the flat
`web_search` spelling only appears inside that row's prose ("The web_search
tool from the shell") — exactly the precedent's shape.

---

## The spelling table (acceptance 5)

| Old spelling | Canonical | Still dispatches? | Pinned by which test |
|---|---|---|---|
| `gopi task serve` | `gopi merger` | **y** (one-line pointer on `-h`, stderr note on run) | `TestCLIContractTaskServeAliasPrintsOneLinePointer` + inventory entry `task serve` (`cliContractInventory`) |
| `gopi task serve -h` | `gopi merger -h` | **y** (prints exactly one line) | `TestCLIContractTaskServeAliasPrintsOneLinePointer` (single-line + family-page-absent + no-lock assertions) |
| `gopi memsync --dedup-scan` | `gopi task dedup-scan` | **y** (dispatches, then refuses with a pointer, exit 1) | `TestCLIContractLegacySpellingsStillDispatch` + `TestMemsyncDedupScanRehomed*` (`cli_memsync_test.go`) |
| `gopi memsync` | *itself* (sibling of `gopi memory sync`) | **y** | `TestCLIContractInventoryMatchesDispatch` (inventory entry `memsync`) |
| `gopi memory sync` | *itself* | **y** | inventory entry `memory sync` |
| `gopi task dedup-scan` | (new canonical) | **y** | inventory entry `task dedup-scan` |
| `gopi web_search` | `gopi web search` | **y** | `TestCLIContractWebSearchDeprecatedAlias` (pre-existing) |
| `gopi vision_analyze` | *itself* — documented exception | **y** | `TestSafeExceptionsAreStillNeeded` |
| `gopi screen_capture` | *itself* — documented exception | **y** | `TestSafeExceptionsAreStillNeeded` |
| `gopi voice_input` | *itself* — documented exception | **y** | `TestSafeExceptionsAreStillNeeded` |
| `gopi vision-analyze` / `screen-capture` / `voice-input` | — (not commands) | **n** — asserted absent | `TestSafeExceptionsAreStillNeeded` (`dashTwin` half) |

`cliContractInventory` gained entries (`task serve`, `task dedup-scan`);
**no entry was deleted or weakened** — `TestCLIContractInventoryMatchesDispatch`
still guards the list against the dispatch table.

## Without the artifact — a caveat

The one new constant (`taskverbs.DedupScan`) and the two new inventory entries
call `TestCLIContractInventoryMatchesDispatch` to account for the new dispatch
arm; without this document the *reason* for `--dedup-scan`'s move would live
only in code comments (they are extensive, but a devdoc is the contract Rule 5
asks for).
