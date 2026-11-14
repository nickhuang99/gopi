<!-- doc-class: normative -->

# The task family's help page: four named groups, and three peer questions

`gopi task -h` is the one externally visible table of the ledger's verbs. This
document records what it now says and why it says it that way, so a later
reader does not have to diff the page against a memory of an older one.

## What changed

**The flat list became four NAMED sections.** The page used to enumerate every
verb in one run, in an order that grew by accretion (a verb appended where its
implementation landed). That answered "what does this family contain" — a
question nobody arrives with — and not "which verb do I want", which is the only
question a first screen can usefully answer. The sections are:

| section | question it answers | verbs |
|---|---|---|
| Read the ledger | which row am I looking for, what does it say | `list` `show` `for-branch` `status` `search` `dedup-scan` `improvements` |
| Write the ledger | how do I record a decision about a row | `submit` `update` `archive` `verify` `reopen` `sweep-worktrees` `sweep-zombies` |
| Delivery and recovery | what became of the branch this row owes | `retry-delivery` `branch-refs` `orphans` `stranded` |
| Moving a row to or from another machine | the cross-node channel | `send` `receive` `pull` `sync` `sync-pass` `diff` `harvest` `push` |

The FIRST line of each section states the property that separates it from its
neighbours — "read-only: no verb here writes a row", "a row's status, briefing,
repo, ground or verdict", "the branch a row owes, and what became of it". A
group heading that only repeated the verb names would be a table of contents,
not a grouping.

**The three peer questions are now answered once, in the cross-node section.**
This was the epic's actual finding: the page advertised `--peer` on several
verbs and answered none of the questions an operator holding a peer flag
actually has. They are now stated as three rows — WHICH peers, WHAT IT NEEDS,
WHEN NOT MET — and the two verbs whose answer is an EXCEPTION say so in their
own block:

- `branch-refs` asks git **directly** (`ls-remote`) and does **not** consult
  `sync_peers`, so an undeclared alias is a git verdict, never a whitelist
  refusal. It needs a repository (a row's recorded one, or `--repo PATH`).
- `retry-delivery` takes **no `--peer` at all**; the flag is refused by name.
  The channel resolves from the declared topology against the row's own repo,
  and a row with no resolved channel is reported `no-channel`, never silently
  skipped.

Both statements were measured before they were written (see Evidence).

## The premise the epic carried was wrong, and that is recorded here

The epic arrived with two claims, and neither survived contact with the binary:

1. **"`send` does not exist."** It does. `gopi task send -h` exits 0 and prints
   the sync-family page; `runTaskSend` is the live implementation, reached from
   the `taskverbs.Send` case. Nothing was "advertised but absent".
2. **"`dispatch.go:runDispatchOnce` has no callers — say so in the help."** The
   file and the function no longer exist in this branch: they were removed on
   2026-10-22 with the `compute_nodes` knob (task `01a160a2`). A sentence
   explaining why a deleted function's zero callers matter would have been a
   FALSE statement written into help text — the one thing a help page must
   never carry. So it was not written. The surviving mentions are comments in
   `cmd/agent/main.go`, `cmd/agent/worker_start_notices.go` and two gate files,
   all describing the removal.

Similarly, the epic asked for `serve` to be "named as retired". It already
was: `gopi task serve -h` exits 0 and prints
`deprecated alias — the merger daemon is RETIRED`, and a RUN of `gopi task
serve` (not `-h`) exits 1 naming the face that still merges. *(Correction
2026-11-01: that sentence used to read "naming `gopi merge`". Measured today the
refusal names `gopi task merge` — the top-level `gopi merge` was removed by the
merge-removal epic and now answers exit 1 itself, so it can no longer be the face
a refusal points at.)*

## The defect this work found and did NOT hide

`orphans --delivery <undeclared-alias>` does not consult `sync_peers` either —
it records the delivery point and reports `advertised refs read: false`, then
judges each row on the local/history measurements. So the page's "an unlisted
alias is refused (exit 1)" clause, written for `diff`/`sync`/`harvest`, is true
of those verbs and NOT of the two read-only audits. The paragraph names the
split explicitly rather than flattening it into a rule that is false for half
its subjects: a help line that over-generalises is the same defect as one that
is stale. The behaviour itself was NOT changed by this task — only described
accurately — and the observation is filed as a bug row per Rule 41.

## Page budget: raised by the rule, not by appetite

`taskUsageBudget` moved 188 → 210 in `cmd/agent/cli_usage_budget_test.go`.
The rule that file enforces is `cap = measured length + usageBudgetNewFlagMargin
(12)`, and the page measures 198 lines. The 10 lines the page gained are named
in the constant's own comment: three group headings, two blank lines between
sections, eight lines of peer questions plus the exceptions named beside them,
one blank after the cross-node table — net of two paragraphs that moved to the
devdocs they name (the page's layering note, and the superseded/release
detail). The cap is not slack: at 198 against 210 there is exactly the margin a
new flag's paragraph needs and no more, and `usageBudgetOffenses` fails BOTH
directions, so a page that shrinks back must lower the cap with it.

The contract detail lives in `man/gopi-task.md`, which gained a
`# PEER TOPOLOGY — THE THREE QUESTIONS` section giving the same three answers
at length. The `-h` page routes; the man page carries the contract. That split
is what keeps the page at 198 rather than 230.

## What was NOT done

- **No behaviour was changed.** Every verb runs exactly as it ran before this
  task; only the announcement moved. The one sentence that could have been
  mistaken for a behaviour change (`orphans --delivery` and the whitelist) is
  a description of the behaviour that already existed.
- **The `send` name is not renamed or redirected.** It is a real verb on the
  sync family and its `-h` correctly hands the reader the sync page; renaming
  it was in the epic's premise ("the name should refuse or redirect"), but that
  premise rested on the false belief that `send` was absent. A real verb needs
  no redirect.
- **The cross-node verb table still lists `push` once.** `push` genuinely has
  no `--peer` flag and pushes every row's branch; the table says exactly that
  and the peer paragraph says it needs no peer. Splitting it into a
  peer/no-peer pair would have invented a distinction the implementation does
  not have.

## Verification (run for real, on the built binary)

Every claim above was measured; the session directory
`~/.gopi/sessions/01a16945-c091-74f0-a781-509f5a71e50d/` holds the captures.

| measurement | command | observed |
|---|---|---|
| page length | `./bin/gopi task -h \| wc -l` | `198` |
| page width | `./bin/gopi task -h \| awk '{print length}' \| sort -rn \| head -1` | `88` (= `config.HelpWrapCol`) |
| section heads | `./bin/gopi task -h \| grep -n '^  [A-Z]'` | line 10 read, 64 write, 129 delivery, then the cross-node head |
| `send` exists | `./bin/gopi task send -h` | exit 0, `GoPi task sync — cross-node ledger reconciliation…` |
| `serve` retired | `./bin/gopi task serve` (run, not `-h`) | exit 1, `serve: retired — …`, names `gopi task merge` (the spelling that sentence named was `gopi merge` when this row was written, before the merge-removal epic removed it) |
| `dispatch.go` gone | `ls cmd/agent/dispatch*.go` | `No such file or directory` |
| list refusal | `GOPI_SYNC_PEERS=declared-peer ./bin/gopi task diff --peer zzz-undeclared` | exit 1, `not in the sync_peers whitelist … declared, never discovered` |
| `branch-refs` bypasses the list | `./bin/gopi task branch-refs --peer zzz-undeclared --repo /tmp/brrepo` | exit 3, `verdict: PEER UNREADABLE — no row is judged` |
| `orphans --delivery` reports, not refuses | `./bin/gopi task orphans --repo /tmp/brrepo --delivery zzz-undeclared` | exit 0, `delivery point: zzz-undeclared (advertised refs read: false)` |
| `retry-delivery` has no `--peer` | `./bin/gopi task retry-delivery --peer x` | exit 1, `unknown flag --peer` |
| `retry-delivery` no channel | `./bin/gopi task retry-delivery` on a clean ledger | exit 0, `no row owes a delivery` |

Gates (all green on the final tree): `TestPublishedHelpPagesStayWithinTheirUsageBudget`,
`TestUsageBudgetCoversEveryPublishedPage`, `TestTaskHelpSurface` family,
`TestNoServiceOrSyncVerbInLedgerGroup`, `TestSyncVerbsHaveExactlyOneHeadHome`,
`TestNoHelpTextIsDuplicatedAcrossPages`, `TestTaskVerbCeiling`,
`TestTaskListHelpNamesBothOrders`, `TestManSourcesMatchTheirHelpPage`,
`TestManSourceGateRefusesDrift`, plus the `./internal/taskverbs` ceiling and
inventory tests.
