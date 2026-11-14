<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# The session verbs' deliverable selector: `gopi log|raw|stats --branch|--worktree`

## The gap

`gopi log`, `gopi raw` and `gopi stats` each address ONE session, and the only
address they accepted was a `<session-id>` (or `--last`/`--all`/`--file`). But the
operator who wants to inspect the run happening **right now** is standing in a task
worktree and holds two things: `git branch --show-current` and `pwd`. Neither is a
session id, and the id is nowhere on their screen — so the surface that is most
useful exactly when a session is `in_progress` was the one they could not type.

The same two addresses are already accepted everywhere the *task* is addressed:
`gopi task show --branch`, `gopi worker --resume --branch`, `gopi task for-branch`.
The verbs that address the *session* were the exception.

## What was added

| layer | artifact |
|---|---|
| resolution | `internal/tools.ResolveSessionByDeliverable` (`task_deliverable_session.go`) — selector → ROW → the session id that row records |
| CLI | `cmd/agent/cli_session_selector.go` — the ONE admission/parse/copy path the three verbs share |
| admission | `--branch` / `--worktree` added to `logFlagMember`, `rawFlagMember`, `statsFlagMember` (exactly the three session verbs) |
| tests | `internal/tools/task_deliverable_session_test.go`, `cmd/agent/cli_session_selector_test.go` |
| inventory | `devdoc/gopi-session-help-surface-inventory.md` (G4 group) — 78 rows, all five new rows executed against a real sandbox ledger row |

## The three design decisions worth recording

### 1. The rows are resolved by the SHARED policy, never by a second one

The selector → row step is `ResolveTaskIDByDeliverable` — the live-first policy
`gopi task show --branch` and the merge fence already use. A tie-break written for
the session verbs would let `gopi log --branch X` and `gopi task show --branch X`
name **different rows for one address**, i.e. the same address meaning two things
depending on which verb read it. The plurality note printed on stderr comes from
the same resolver too (`DeliverableResolutionNote`).

### 2. The resolved value is a SUBSTITUTION into the existing dispatch, not a second grammar

`ResolveSessionByDeliverable` returns the **session id**, and the verb assigns it to
the field its positional `<session-id>` would have filled — before the dispatch
switch reads it. Consequences, all of them the point:

- the log is still found by the verb's own resolver (`gopilog.FindSession`,
  `resolveRawForSession`), so "where is this session's log?" has ONE answer;
- every existing view flag works through the selector with no extra code
  (`--todo`, `--response`, `--rounds`, `--sysprompt`, `--json`, …);
- stdout stays byte-identical to the id's own output — verified, not assumed:
  `stats <id>` / `stats --branch` / `stats --worktree` all hash to
  `7d2f7c5859b9fd3e`; `log <id> --todo` and `log --branch … --todo` to
  `1cc0d27baac53a45`; `raw <id> --sysprompt` and `raw --worktree . --sysprompt` to
  `0771afcf291af5de`.

**Why the resolution stops at the id (a measured constraint, not a preference).**
Resolving the row's session id to the LOG PATH inside `internal/tools` closes an
import cycle: `internal/gopilog` imports `internal/agent`, which imports
`internal/tools`, so a `tools → gopilog` edge is refused by the compiler
(`go build`: "import cycle not allowed"). The workaround of hand-joining the
session layouts inside `internal/tools` (`taskFindSessionJSONLForRow`) would be a
second, staler answer to a question `gopilog` already owns — it reads session heads
for layouts `gopilog.FindSession` resolves with one stat in the normal case
(measured 2026-09-25: 3.2 s → a stat over a 76 k-entry tree). The cycle is the
reason the split is where it is; the split is not a style choice.

### 3. `gopi raw`'s positional slots SHIFT, and the shift is stated

`gopi raw <session-id> [round] [request|response]` and
`gopi raw --file PATH [round] [request|response]` differ in what position 0 means.
The selector occupies the session-id slot, so with `--branch`/`--worktree` the
positionals are `[round] [request|response]` — computed before the positions are
read (`maxPos, roundIdx, partIdx = 2, 0, 1`). A session id typed there no longer
silently becomes `part`, it is refused as the round it now is, with the shape named:

```
$ gopi raw --branch <b> 3 request
$ gopi raw --branch <b> sess-1
gopi raw: --http takes all|last|N (a round number), got "sess-1" — with --branch/--worktree
naming the session, the positional here is <round> [request|response]
```

The input rules for the three verbs are: **exactly one input**, and two input-shaped
arguments are refused by NAME (never silently preferred) — the selector beside the
positional (the SAME input: `--branch X sess-1`) and beside a different input
(`--branch X --last` / `--last`/`--all`/`--file`). An empty value (`--branch=`) is
"needs a value", not "no selector": reading it as absent would answer with a message
about a different problem.

## Failure faces (each names its repair)

| invocation | refusal |
|---|---|
| `gopi log --branch feat/nope` | `task: no ledger row names branch "feat/nope" — `gopi task for-branch <branch>` prints every owner…` (`ErrDeliverableNoOwner`, propagated) |
| both selectors | `--branch and --worktree are two ways to name ONE session, not a filter pair — pass one` |
| a row that has not run | `task <id> (branch "…") records no session — nothing has run for it yet…` — named with the ROW, so the operator can go look at it |
| selector + id / + `--last` | `… are the SAME input — pass one` / `… are different inputs — pass one` |

## Verification (what was actually run)

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/                    → exit 0
$ go test ./internal/tools/ -run TestResolveSessionByDeliverable     → ok 4.3s
$ go test ./cmd/agent/ -run 'TestSessionVerbSelector|TestSessionVerbsAdmit'  → ok
$ go test ./cmd/agent/ -run 'TestG4'                                 → ok 9.5s (78 inventory rows,
                                                                       all five new ones executed)
$ ./bin/gopi stats <id> | sha256sum  vs  --branch <b>  vs  --worktree <pwd>
  7d2f7c5859b9fd3e  ==  7d2f7c5859b9fd3e  ==  7d2f7c5859b9fd3e
```

The G4 group was a gate on the work, not decoration: the three new synopses made
`TestG4HelpSurfaceMatchesInventory` red (5 unaccounted rows), and the fixture had to
grow a **real sandbox ledger row** because the advertised value is a ledger lookup —
a help page advertising a selector that resolves nothing would have passed a
string-matching test and failed the operator.

## Two pitfalls from this run

1. **A bulk `sed`/`python` rewrite over help-string raw literals is a footgun.** A
   backtick-quoting pass over `cli_log.go`/`cli_raw.go`/`cli_stats.go` stripped
   backticks inside the backquoted usage strings and broke the build with
   `syntax error: unexpected name git`. Recovery: `git checkout -- <file>` (the
   damage was in tracked files; only my two new files were untracked and survived),
   then re-apply with exact-match replacements. **Rule: never rewrite a file that
   contains backquoted help strings with a global quoting transform.**
2. **A test that seeds the ledger must pin the env the CHILD resolves.** The suite
   scaffold pins `GOPI_MEMORY_DB` for the run (Rule 32), so a fixture seeding only
   `GOPI_HOME` wrote its row to the suite's shared ledger while the child — whose
   env (`cliContractEnv`) drops every `GOPI_*` var — resolved the sandbox one and
   found nothing. Pin both, to the same resolved path.
