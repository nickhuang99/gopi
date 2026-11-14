<!-- doc-class: historical -->
# Task Report: the `--dump-screen` help block names its reader again — without inflating the g5 flag census

Task `01a12c42-4619-7ae6-bbc2-877d48260d35`. The `./cmd/agent` help-surface census
tests were **deterministically RED** on this branch and green on the control tree
`a3a31da42`. Both symptoms had ONE cause.

## The measured cause

Commit `f0e32cf5e` ("feat(help): move verb detail into man pages, budget every
published `-h` page") rewrote `cmd/agent/cli_chat.go` and, in the
`--dump-screen` / `--no-dump-screen` block, replaced the reader paragraph with:

```
Reading the artifact it writes, and the audit built on it:
  gopi ui_state --target chat --all    audit the WHOLE answer as ONE call
                   (man gopi-chat carries its exit contract and vocabulary).
```

That is two independent defects in three lines:

1. **The reader vocabulary was deleted.** The block no longer named who consumes
   the artifact (`gopi itself`, the `chat_surface` tool, its `see`/`compare`
   actions), the one-call audit `gopi ui_state --target chat --all`, the phrase
   `derives its authority from the dump`, the exit-contract words
   `agreed`/`disagreed`/`could-not-run`, `exit 1 = the check could NOT`, the
   arming command `gopi chat --dump-screen`, or the narrowing flags
   `--compare`/`--session`. Three tests exist so exactly those cannot go
   invisible on the page that creates the artifact.

2. **The audit line was re-indented to 2 spaces — and that is a census event.**
   The g5 scanners (`scripts/g5-chat-web-help-surface.sh`, and its Go twin
   `g5DeriveFlags`/`g5FlagHeadRe` in `cli_chat_web_help_surface_basic_test.go`)
   collect flags from THREE line shapes: a synopsis HEAD (`^ {0,2}gopi `), a
   bracket CONTINUATION (`^ {18,}\[`), and a flag DEFINITION line
   (`^ {0,2}-{1,2}NAME[ ,-]+[A-Za-z<{]`). The old block indented the audit
   command past 18 spaces, so it was **prose**. At 2 spaces it became a synopsis
   HEAD, and the scanner read `--target` and `--all` out of it as though
   `gopi chat` advertised them: chat 22 → 24, group total 47 vs the pinned 45.

So "a flag appeared" was not a new flag — it was a **help text accidentally
wearing a synopsis shape**, and the census was the instrument that caught it.
Restoring the paragraph at 2 spaces would have left (2) in place; the fix does
both at once.

## Completed

- **`cmd/agent/cli_chat.go`** — the `--dump-screen` block names its reader again,
  and the audit command line is indented **out of** the synopsis-head shape.
  - The block once more carries every word the tests require: `gopi itself`, the
    `chat_surface` tool with `action=see` / `action=compare`, the one-call audit
    `gopi ui_state --target chat --all`, `derives its authority from the dump`,
    `outcome=agreed / disagreed / could-not-run`, `exit 1 = the check could NOT`,
    `gopi chat --dump-screen`, `--compare`, `--session`.
  - The command line sits at **23 spaces** — one column past the scanner's
    bracket-continuation floor (`{18,}`) and far past the head ceiling (`{0,2}`)
    — so the scanner reads it as neither head nor wrap nor definition, and it
    contributes no flag.
  - Every word is read from the constant package that owns it (Rule 1/13):
    `toolnames.ToolChatSurface`, `agent.ChatSurfaceActionSee`,
    `agent.ChatSurfaceActionCompare`, `uiStateAuditWordAgreed`,
    `uiStateAuditWordDisagreed`, `uiStateAuditWordCouldNotRun`, `chatDumpFlag`,
    `uiStateFlagCompare`, `uiStateFlagSession`, `uiStateFlagTarget`,
    `uiStateFlagAll`, `CliVerbUIState`, `uiclient.SurfaceChat`. The page and the
    tests asserting on it read ONE spelling — which is also why the audit line
    the test extracts by prefix is built from the same constants.
- **The g5 inventory was NOT regenerated, because the derived flag set did not
  change.** After the fix the script re-derives `gopi chat` = 22 and the group
  total = **45**, exactly what the shipped
  `devdoc/gopi-chat-web-help-surface-inventory.md` already says (row 1 lists the
  same 22 names; the `**Totals:**` line says 45). Regenerating would have
  rewritten the table to say what it already said — so per the epic's pin (2),
  the table stands, and the re-derivation is recorded as the evidence instead.
- **The pin was not touched** (pin 3): `cli_chat_web_help_surface_basic_test.go`
  is unmodified — the censuses pass against the help page, not against a
  loosened expectation.
- **Budget:** the page measures **461 lines against the 462-line
  `chatUsageBudget`** — inside the cap, and the `usageBudgetNewFlagMargin` (12)
  requires it be no more than 12 below, so 461 is a valid landing point.

## Not Done

- **Three further `./cmd/agent` failures are NOT this change's and are not fixed
  here.** `TestChatCookedFallback_NonTTY`, `TestRunPathPlainPromptIsVerbatim`,
  `TestDaemonScopeNoPrivilege_TheCorrectionIsEmitted` (plus
  `TestWorkerDaemon_RequireScopeOffSwitchIsADocumentedDegradation` and the
  `TestNotifyDeliver*` pair) fail **with this file reverted to its pre-fix
  revision** — measured, not assumed (see Evidence). They are a separate defect
  family on this branch and belong to their own task (Rule 41/26), not to a
  help-page fix.
- The `man/gopi-chat.md` page was already correct after `f0e32cf5e` (it carries
  the full vocabulary, exit contract and cost paragraph) and is deliberately left
  alone: this change restores the `-h` page's **reader line**, it does not move
  detail back out of the man page. The slimming direction of `f0e32cf5e` is
  preserved.

## Verification

- **Build:** `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (no output).
- **Tests:** `go test ./cmd/agent/ -run
  'TestChatDumpHelp_NamesTheAutomaticAudit|TestChatDumpHelp_TheNamedAuditCommandIsTheAutomaticOne|TestChatDumpHelp_DescribesAnObservationTheAgentTakes|TestChatDumpArming_TheHelpTextCarriesTheCommandThatArms|TestG5InventoryMatchesTheHelp|TestG5SurfaceCensusIsDerivedFromConstants|TestUsageBudget|TestManSourcesMatchTheirHelpPage'
  -count=1` → `ok github.com/nick/gopi/cmd/agent 3.497s`.
- **Ran:** (Rule 31 — the real surface, built and executed, not `go test` alone)
  1. `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`
  2. `./bin/gopi chat -h` → 461 lines; excerpt below,
     captured at `artifacts/chat-help-after.txt`.
  3. `scripts/g5-chat-web-help-surface.sh` (no argument → `./bin/gopi`) →
     `artifacts/g5-tsv-after.txt`; the `chat` row is the same 22 flags as the
     inventory's row 1, and the eight rows total **45**.
- **Result:** every required word is on the page; the audit line's argv extracts
  and parses through `parseUIStateArgs` as the no-ceremony whole-answer audit
  (`all=true`, `target=chat`, `compare==""`, `session==""`); the derivation total
  is 45 as pinned; `git diff --name-only master...HEAD -- cmd/agent/cli_chat_web_help_surface_basic_test.go` prints **nothing**.
- **Commit:** `f64c8507c` (the fix; this report rides the commit that carries this file).

### The help block, verbatim from `./bin/gopi chat -h`

```
WHO READS IT — gopi itself, from inside, with no human in the loop:
                   - the answering turn, through its own chat_surface tool:
                     action=see reads the painted grid as a cell grid, and
                     action=compare checks a line it wrote against BOTH layers;
                   - the CLI verb, which audits the WHOLE answer as ONE call:
                       gopi ui_state --target chat --all
                     Nothing above asks a reader to assemble it by hand: the
                     audit derives its authority from the dump, so it needs no
                     line and no session. Its ledger word is outcome=agreed /
                     disagreed / could-not-run — exit 1 = the check could NOT
                     RUN, never "they agree". `gopi chat --dump-screen` arms a
                     run; `man gopi-chat` carries the damage vocabulary and the
                     single-line narrowing (--compare, --session, a different transcript).
```

### Red proof (Rule 41 — vocabulary removed again, the tests fail again)

The paragraph was deleted from the block in a scratch variant (the now-unused
`toolnames` import removed with it so the variant still **builds** — otherwise
the proof would be a compile error, not a red test):

```
--- FAIL: TestChatDumpHelp_NamesTheAutomaticAudit (0.00s)
--- FAIL: TestChatDumpHelp_TheNamedAuditCommandIsTheAutomaticOne (0.00s)
--- FAIL: TestChatDumpHelp_TheNamedAuditCommandIsTheAutomatic... (the audit line is gone)
--- FAIL: TestChatDumpArming_TheHelpTextCarriesTheCommandThatArms (0.00s)
FAIL    github.com/nick/gopi/cmd/agent
```

with `TestChatDumpHelp_NamesTheAutomaticAudit` naming each missing word
(`"gopi itself"`, `"chat_surface"`, `"compare"`,
`"gopi ui_state --target chat --all"`, `"derives its authority from the dump"`,
`"could-not-run"`, `"agreed"`). Full output at `artifacts/red-proof.txt`. The
file was then restored from the fixed copy and **the same three tests went green
again** — the red/green transition is the proof, not the red alone.

## Evidence (self-filed)

- `./bin/gopi chat -h` prints 461 lines and contains the restored paragraph —
  captured at `artifacts/chat-help-after.txt`.
- `scripts/g5-chat-web-help-surface.sh` exit 0; `chat` = 22 flags; eight rows sum
  to 45 — `artifacts/g5-tsv-after.txt`.
- Selected tests: `ok github.com/nick/gopi/cmd/agent 3.497s` —
  `artifacts/tests-after.txt`.
- Red proof: exit 1, the three tests `--- FAIL`, then green after restore —
  `artifacts/red-proof.txt`.
- Pin untouched (pin 3): `git diff --name-only master...HEAD -- cmd/agent/cli_chat_web_help_surface_basic_test.go` printed nothing.
- Pre-existing failures classified by measurement: with `git checkout
  ffba7714c~1 -- cmd/agent/cli_chat.go` (my file reverted to its pre-fix
  revision) `TestChatCookedFallback_NonTTY`,
  `TestRunPathPlainPromptIsVerbatim`,
  `TestDaemonScopeNoPrivilege_TheCorrectionIsEmitted` and
  `TestWorkerDaemon_RequireScopeOffSwitchIsADocumentedDegradation` each still
  printed `--- FAIL` — so they are not caused by this change.
- `git status --porcelain` in the worktree prints nothing at close (Rule 30).
