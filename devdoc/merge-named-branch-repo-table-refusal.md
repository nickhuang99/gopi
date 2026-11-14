<!-- doc-class: historical -->
# `gopi merge <branch>` could not tell "no repository to choose from" from "your branch was decided"

Measured and fixed 2026-10-16, baseline `3e07a4052`, in
`cmd/agent/merge_run.go` (task `01a144951a8c780dbfd0c493fd6d352d`).

## The defect

A merge run is one of the few commands whose exit status is a contract: `0` means
*every branch this run was asked about reached a terminal decision*. Two shapes of
"the repos table cannot hold the branch you named" broke that contract, and they
broke it in opposite directions.

> **Retired mechanism (merge verb removal, 2026-11-01).** The `gopi merge …` lines
> below (both the invocation and its `gopi merge:` output) are the RECORD of what
> this run measured, kept verbatim; the top-level verb was removed afterwards and
> answers **exit 1** today. The refusal arm they document is unchanged.

**① The table is EMPTY.** `runMergePass` answers an empty `repos` table with an
informational line and exit **0** — correct for a *sweep* (it names nothing, so it
asks for nothing). But a *selection* is a different question, and the arm did not
look at it:

```
$ gopi merge feat/orphan          # settings.json: {"repos":{}}
gopi merge: no repos in the settings.json "repos" table — nothing to merge
$ echo $?
0
```

The branch the operator named appears **nowhere** in either stream, and the exit
status says the merge succeeded. A caller — the durable caller is a hook or a
script — cannot tell "there was no repository to choose from" from "your branch
was decided". This is Rule 15's silent failure, and it is the worse of the two:
the run never even looked for the branch.

**② The table holds other repos, none of which holds the branch.** Here the
refusal was already correct (exit 1, naming the branch) — and then the arm
appended the whole 200-line `-h` page to the same stream:

```
$ gopi merge feat/orphan          # settings.json: {"repos":{"/some/other":{}}}
# stderr, 204 lines:
gopi merge: WARNING: the repo this run was started in (…) is NOT in the … table …
gopi merge: the "repos" table holds 1 repo(s): /some/other
gopi merge: no repo in the "repos" table holds feat/orphan — a named branch is never…
GoPi merge — one foreground merge run.        ← line 3 of 204, and 201 lines of it follow
```

The one sentence that answers the operator sat above a screen of help text. A page
of usage is the right response to a **bad flag** (an argument mistake) and the wrong
response to an unresolvable branch (a fact about the table).

Evidence for both: this run's session directory,
`…/sessions/01a14495-1a8c-780d-bfd0-c493fd6d352d/` — `before-empty.{out,err}` and
`before-absent.{out,err}`, captured with the baseline binary built from `3e07a4052`.

## The fix

Three surgical changes in `cmd/agent/merge_run.go`, no new file, no new flag, no
new dependency:

1. **The empty table is classified by what was asked.** `runMergePass`'s
   `len(repos) == 0` arm now branches on `opts.selected()`: a selection is refused
   by name with the *same sentence* the resolve step uses, plus a second line
   naming the table's own empty state (so the reader learns the fix is to populate
   the table, not to re-spell the branch); a sweep keeps its informational line and
   its `0` verbatim.
2. **The refusal no longer carries the page.** `runMergePass`'s selection-resolution
   error arm prints the diagnosis alone. The *flag* refusals in `runMerge` keep
   their page — that boundary is the point: a bad flag is an argument mistake, an
   unresolvable branch is not.
3. **One spelling of the sentence.** `mergeNamedBranchUnresolvedError(opts)` is the
   single definition both arms call, so the empty-table arm and the
   non-empty-table arm cannot drift apart (Rules 13/14). The brand-new
   `mergeSelectionUnresolvedExitStatus`, `mergeBranchListSeparator` and
   `mergeNamedBranchEmptyTableFormat` constants carry the numerics and the literal
   spellings out of the call sites (Rule 1/1b).

```
$ gopi merge feat/orphan          # settings.json: {"repos":{}}
$ echo $?
1
$ …stderr
gopi merge: no repo in the "repos" table holds feat/orphan — a named branch is never silently ignored; check the spelling, or run without names to sweep every queue
gopi merge: the "repos" table holds no repository at all — add one, or run without naming a branch
```

stdout is empty: a refused run produced no result, and stdout is where a caller
reads one.

## What was NOT changed, and why it is pinned

The refusal is keyed on `opts.selected()`, so the arms that name nothing are
untouched by construction — and each is asserted anyway, because "the empty table
became an error" is exactly the over-correction this fix could have been:

| invocation | repos table | before | after |
|---|---|---|---|
| `gopi merge` (sweep) | empty | `…nothing to merge`, exit 0 | unchanged |
| `gopi merge --status` | empty | `…nothing to report`, exit 0 | unchanged |
| `gopi merge -h` | any | page, exit 0 | unchanged |
| `gopi merge <branch>` | holds the repo | end-to-end run | unchanged |

## The regression test

`cmd/agent/merge_named_branch_repo_table_test.go` — four tests, red on the
baseline and green after:

- `…InAnEmptyRepoTableRefusesByName` — the primary gate: non-zero, **zero stdout**,
  the branch named, the `repos` key named, no `nothing to merge` success line, no
  help page on either stream, the ref untouched.
- `…AbsentFromTheRepoTableRefusesByName` — the second arm: same assertions plus
  "the other repo's refs did not move".
- `…SweepInAnEmptyRepoTableStaysInformational` — the pairing control that keeps the
  fix from being "make the empty table an error".
- `…WithTheRepoInTheTableIsUnchanged` — the epic's end-to-end control: the table
  holds the repo, the named orphan branch reaches `unrelated-history`, the
  retirement tag and its recorded reason are written, exit 0.

The tests assert on the split of the two streams (`mergeRunOnBothStreams`), because
"zero stdout, a named refusal on stderr" is a claim about that split; and on
`cliUsageTitle(mergeUsageHead)` — the same spelling the `-h` contract test uses —
so "the page is absent" is asserted about the page, not about a word it happens to
contain.

## Red proof

`go test ./cmd/agent/ -run '<the four>' -count=1` on the baseline: **FAIL**, with
two distinct failures —

```
--- FAIL: TestMergeNamedBranchInAnEmptyRepoTableRefusesByName
    … with an EMPTY "repos" table exited 0 — the caller is told the merge succeeded
    while the named branch was never even looked for
    the refusal wrote to STDOUT ("gopi merge: no repos in the settings.json …")
    the refusal does not name the branch the operator asked for ("feat/orphan-…")
--- FAIL: TestMergeNamedBranchAbsentFromTheRepoTableRefusesByName
    absent from table: the refusal printed the -h page on stderr …
```

The two controls passed on the baseline **and** after the fix — which is what makes
them controls: they were never red, so a green suite is not being read as evidence
for the wrong reason.

## Reproduce

```bash
# ① the empty table
printf '{"repos":{}}' > /tmp/settings-empty.json
cd <a repo holding branch feat/x>
GOPI_SETTINGS=/tmp/settings-empty.json gopi merge feat/x; echo $?   # pre-fix: 0 + "nothing to merge"

# ② the table without the repo
printf '{"repos":{"/some/other/repo":{"build":""}}}' > /tmp/settings-other.json
GOPI_SETTINGS=/tmp/settings-other.json gopi merge feat/x; echo $?   # pre-fix: 1 + 200 lines of help
```
