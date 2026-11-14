<!-- doc-class: historical -->
# The `gopi merge` page — background moved off the page

> **DISPOSITION (2026-11-01, merge-removal epic).** This document is the
> BACKGROUND of a help page that no longer exists, so it is classified
> `historical` — which is what its own `**Class: historical**` paragraph below
> has said all along. The top-level `gopi merge` verb (and its help page,
> `mergeUsageHead`) was removed: `./bin/gopi merge` exits **1** today. The
> reasoning here is not rewritten, because it is the record of why the engine
> behaves as it does, and the engine survives — under the family spelling
> `gopi task merge`, whose page is the FILE `man/gopi-merge.md` (a page of its
> own, not this background).
>
> **One live pointer to check**: `man/gopi-merge.md` used to send its reader here
> for the attribution of a withdrawn batch. **That pointer is RETIRED (2026-11-01,
> same epic, later slice):** `man/gopi-merge.md` now states plainly that no
> surface verifies the merged tree or withdraws a landing — the mechanism
> (`internal/tools/merge_pass_verify.go`, `CaptureMergePassScope` /
> `VerifyMergePass` / `WithdrawMergePass` + the `merge-withdrawn` alarm) was
> deleted with the remainder of the retired top-level verb, because its last
> production caller (`cmd/agent/merge_run.go`) was already gone. The attribution
> prose below is therefore no longer the account of a live capability: it is the
> record of one that used to exist. What a reader must not do is take any
> `gopi merge <flags>` LINE below as something to type: the flags went with the
> verb.

**Class: historical** (no current artifact cites this file, so the generator
classes it a past run's record; the banner above is the machine-checkable
half of the same claim). This is the standing background of the `gopi merge` help
page (`mergeUsageHead`, `cmd/agent/merge_run.go`). The page carries the
DECISIONS an operator needs before typing the command; the reasoning, the
measurements and the worked detail live here, because a help page is read while
choosing a command and a devdoc is read while changing one.

Slice 3/5 of the "one page = one level" epic (`01a10bb7`) moved the paragraphs
below off the page and left pointers. Nothing was deleted — this file IS the
deleted prose, verbatim.

## Naming branches — the whole paragraph, as the page carried it

ONE BRANCH ON DEMAND. Naming branches narrows the run to exactly those names:
`gopi merge feat/x` decides feat/x and nothing else, in every repo that holds
it, and each other branch keeps its ref for the next full run — the same
decision path (git-first landing, the ceilings, the hand-over, the post-pass
verification and its withdrawal) with a different CANDIDATE SET. A repo whose
queue holds none of the named branches is SKIPPED WHOLE: it is not swept, its
checkout is not restored and its build does not run, so a targeted merge never
has a side effect in a repo you did not name. A name that matches nothing
anywhere is REFUSED — a word the operator typed is never silently ignored. The
integration branch cannot be named (it is never a candidate), and neither can a
branch a worktree holds or one tagged `archive/<name>` — those are reported as
skipped/untouched, exactly as a full run reports them.

## Union-resolved conflicts — the whole paragraph

A conflict whose every path is a declared append-only generated artefact —
`devdoc/DOC-INDEX.tsv`, the devdoc truth-class manifest that every branch
adding a devdoc appends a row to — is not a judgement: the resolution is the
UNION of both sides' rows. The run builds that union in a throwaway worktree,
proves it (the union parses, no row is duplicated, and no row either side
carried is missing) and lands the merge commit itself. That is why such a
branch is reported `union-resolved` instead of being filed as a rework task. A
conflict on any other path is untouched by this rule: two branches that
disagree about what CODE should say is the merge gopi's judgement, never a rule
the command may guess. A union the run cannot prove is refused, and the branch
falls back to the ordinary ladder (measured, then handed over, or filed at the
ceiling).

## Post-pass verification and the withdrawal it may decide — the whole paragraph

> **Retired mechanism (merge pass verify/withdraw deletion, 2026-11-01).** The
> paragraph below is written in the present tense because it is the record of
> the run as it behaved on the day it was measured. **No shipped surface does
> any of it.** `internal/tools/merge_pass_verify.go` (`CaptureMergePassScope` /
> `VerifyMergePass` / `WithdrawMergePass`) and the `merge-withdrawn` alarm were
> deleted with the last slice of the retired top-level `gopi merge` verb; its
> last production caller (`cmd/agent/merge_run.go`) had already gone.
> `gopi task merge` does NOT verify the merged tree and does NOT withdraw a
> landing — see `man/gopi-merge.md` for the contract that ships, and
> `devdoc/merge-verify-withdraw.md` for the mechanism's own record.

Landing is decided by git alone; whether the tree that landing produced still
holds together is a SECOND question, and this run asks it once per repo that it
landed in: `go build ./...` then `go vet ./...`, over the tree the pass left
behind. Never the test suite (the merge path's test verdict is the smoke set,
branch-scoped), never once per branch — a batch is withdrawn as a batch, and
the pre-pass tip this run captured makes one measurement enough.

Red: the WHOLE batch is WITHDRAWN for that repo — `git reset --hard` to the
pre-pass tip (a fast-forward leaves no merge commit to `git revert -m 1`), every
branch ref kept and the ones the landing deleted RE-CREATED at their captured
tips, then reported: the tip it was withdrawn from, the pre-pass tip it came
back to, what failed (with the failing packages), the branch refs that survive,
and the best-effort attribution of those branches. The merge gopi is NOT
started over a withdrawn master — the branches are back in refs/heads for the
next run.

An ABSTENTION (not a Go module, no toolchain, an unusable cache, a budget that
expired) is NOT a failure: the landing stands, and the run says it abstained.
Only a measured red withdraws anything.

## `--every`, the lock, and the stop path — the whole paragraph

One flag, in this command: the pass runs, the process sleeps the interval, the
pass runs again, forever. No wrapper, no unit file, no daemon, no state file:
it is a normal foreground process in your own shell, and it dies with that
shell. THE LOCK IS TAKEN PER PASS, never held across a sleep: a sleeping loop
holding the single-instance lock would refuse a hand-run `gopi merge` for the
whole interval, and the lock's job is to serialise merge WRITERS, not to
reserve a process's idleness. So a hand-run merge during a tick is served in the
ordinary way (it wins the lock if the loop is between passes, and is refused by
the running pass if one is in flight), and the loop's next tick proceeds
normally.

Stopping it: SIGINT (Ctrl+C) or SIGTERM. The handler does nothing but record the
signal — the pass is never interrupted, so a stop during a pass takes effect at
that pass's own end, and a stop during a sleep takes effect immediately (the
sleep is a select on the timer and the signal, never a bare sleep). A stop
during a pass is ANNOUNCED at once ("stop requested: waiting for the in-flight
pass to finish (giving up in D)") and the wait is BOUNDED by the same D: if the
pass has not come back by then the loop stops waiting, says the bound fired, and
exits 0. A SECOND Ctrl+C ends the process at once (the handler is removed and
the signal is re-raised at this pid only), so a wedged pass can never make the
command unkillable. Either way the loop prints one line saying it stopped and
exits 0.

## Why the page is the shape it is

The rule this slice applies: **a help page lists what the reader can type, one
line each, and defers the reasoning to a document that is read on purpose.**
The page kept, in order: the synopsis, what the command does in one paragraph,
the verification and its exit status, the lock, and the notes for the flags an
operator actually sets. Everything that explains WHY a decision was made — the
withdrawal's mechanics, the per-pass lock, the stop-path bound, the union
proof — moved here.

Measured on this slice (real binary, `./bin/gopi merge -h | wc -l`, at branch
tip `048ef7a2`): **191 lines before, 144 after.** The before figure is the same
binary built at that commit without the four page edits (`git worktree add
--detach <path> HEAD` + build), so both numbers are real renders of the same
compiler, not estimates.
