<!-- doc-class: historical -->
> **Retired mechanism (merge pass verify/withdraw deletion, 2026-11-01).** The
> post-pass verification and its withdrawal (`internal/tools/merge_pass_verify.go`:
> `CaptureMergePassScope` / `VerifyMergePass` / `WithdrawMergePass`, plus the
> `merge-withdrawn` notify alarm) were DELETED on 2026-11-01 with the last slice
> of the retired top-level `gopi merge` verb — its last production caller,
> `cmd/agent/merge_run.go`, was already gone. **`gopi task merge` does not verify
> the merged tree and does not withdraw a landing.** Everything below that speaks
> of the post-pass in the present tense is the record of a capability that
> existed, measured on the day each page states; it is not a promise about the
> shipped engine. `man/gopi-merge.md` carries the corrected contract.
# devdoc: `gopi merge` is COMPILE-ONLY for a mechanical landing


**Status:** standing claim about the merge path's gate (epic
merge-maximise-one-run, task `01a14205-670f-7beb-8863-2ea7929ea9e9`, 2026-10-16).

## The decision in one paragraph

A merge run's job is to land as many branches as it can in ONE invocation, and
its scarce resource is its own context (`cmd/agent/merge_prompt.md` §3.3.1, §3.4.0
— the per-branch ceiling). A **mechanical landing** — a fast-forward, a clean
`--no-ff` merge committed without anybody editing a file, the mechanical union
lane — puts on the integration branch a tree **nobody in this run authored**: it
is byte-for-byte the tree the branch's own worker already built and tested before
it handed the branch over. Re-running a suite over it buys nothing anybody did
not already pay for; it only spends the budget of the run whose whole purpose is
to get through MORE branches. So a mechanical landing owes the repo's **DECLARED
build** (Rule 12) plus `go build ./...` + `go vet ./...`, and **no test run**. A
test run is owed in exactly one case: a branch the merger **resolved by hand** —
there the run authored a tree no branch's own run ever tested — and it is owed
**scoped to what that resolution touched** (`make test-budget PKGS=…`,
`internal/smokegate`), never the whole tree (Rule 27).

**The worker owns its own quality.** That sentence is the whole rule; everything
else is its consequence.

## What was true before this change (and what was already right)

The Go **post-pass** (`internal/tools.VerifyMergePass`) was ALREADY compliant: it
measures `go build ./...` + `go vet ./...` over the integration branch after the
landings, and it runs no tests. Nothing in the Go path had to change for this
policy — **and that was the problem**: the measurement was right by habit, with
nothing in the repo asserting it must stay right, so a single future commit could
add a `test` leg and no gate would fire (the epic's own words: "no machine check
pins the policy either way").

The shipped **prompt** was not compliant. `cmd/agent/merge_prompt.md` §3.8 carried

> **Run the SMOKE tests, SCOPED to what the merge touched — never the whole tree.**

as an unconditional instruction to the merge child, mechanical landings included.
That is the redundancy this change removes — with a measured cost, not a
theoretical one: on a busy day the merge run's parent is the process that must
survive several branches inside a 45-minute hand-over budget, and a scoped suite
after every landed branch is exactly what turns a run that could land six
branches into one that lands two.

## What changed

1. **`cmd/agent/merge_prompt.md` §3.8** — the unconditional smoke instruction is
   replaced by the policy: the mechanical default first (`A MECHANICAL LANDING
   OWES THE DECLARED BUILD AND NO TEST RUN.`), then the one exception
   (`THE ONE CASE THAT OWES A TEST RUN: a branch YOU resolved by hand.`) with its
   scoped command and the honest-reporting obligation ("a run that resolved
   nothing by hand runs no test at all and must say so in its report").
2. **§3.3.1** (the fast-forward batch — where the bulk of a run's landings
   happen) states the same thing where the batch is described, so the policy is
   not only findable in one section.
3. **§3.11** (the merge-run report skeleton) — the `landed:` line now carries the
   compile-only default and requires a run that resolved nothing by hand to SAY
   so, rather than leave a reader to guess whether a suite was skipped or
   forgotten (Rule 15 — silence is the defect, not the inconvenience).
4. **Gate, prompt half** — `cmd/agent/merge_prompt_compile_only_test.go` asserts,
   inside §3.8's own region, that the mechanical default is stated, that the
   hand-resolution exception is named, and that **every** prescription of the
   scoped smoke command sits BEHIND that exception; plus the retired heading's
   absence.
5. **Gate, post-pass half** — `internal/tools/merge_pass_compile_only_test.go`:
   the build half's argv is now spelled once (`mergePassBuildArgv`, the value the
   process is actually executed from) and is asserted to carry no test verb; a
   real Go module is then verified through the real `VerifyMergePass` with a
   **canary test** that FAILS and writes a sentinel when executed — so a green
   verification plus an absent sentinel is a direct measurement that no test body
   ran.

## Why the check is an ORDER and not a keyword search

The prompt legitimately contains `make test` and `make test-budget` — they are
the declared smoke command and its scoped form. "The words are present" is true
of the correct prompt and of the defective one alike; what changed is **where**
they may appear and **under which condition**. So the gate asserts an ORDER
inside §3.8: default first, every scoped-smoke prescription behind the exception.
Reversing that order is the natural shape of a regression — and it is precisely
the text that shipped before this change — so the gate's planted arm runs the
same predicate over the retired text and over the reversed text and requires it
to fire on both. (The first draft of that arm failed for a real reason and is
worth remembering: an early `return` on the first missing phrase made the
predicate report only its own finding and pass the retired text through
unnoticed. A red-proof arm that is not itself observed failing proves nothing.)

## Rejected alternatives

- **Keep the smoke run but make it cheaper** (a smaller package set, a cache, a
  "quick" target). Rejected: the run is not too expensive, it is **redundant**.
  The tree is the branch's own tested tree; a cheaper re-answer to a settled
  question is still a settled question.
- **Make the post-pass run the suite instead of the child.** Rejected for the
  same reason, plus a second one: the post-pass runs ONCE per repo after the
  batch, so it would be the worst place to put a per-branch cost — and a broken
  test would then blame the merge for a defect the branch's own run owned.
- **Trust the prompt alone (no gate).** Rejected: the prompt is the only thing
  that ever told a merge gopi to run the suite, so the prompt is exactly where a
  regression comes back, silently, and the run's symptom (fewer branches landed)
  is indistinguishable from a slow day.
- **Gate the prompt by a forbidden-word list.** Rejected as unsound: the words
  must stay (they are the declared command), so a word ban would either be
  vacuous or would force the correct text to be paraphrased into something the
  `internal/smokegate` constants no longer recognise.

## What this does NOT claim (honest partial coverage)

- The prompt gate reads §3.8's region and the retired heading; a hand-written
  `go test ./...` elsewhere in the prompt is caught by the existing whole-tree
  ban, not here.
- The post-pass gate covers the post-pass's own vector and a real tree. It cannot
  prove that no *other* future surface starts running a suite — a rule is only as
  wide as the surfaces it enumerates, and a surface nobody wrote down has no gate.
- The `/tmp/gopi-co merge` evidence run is a **fixture** (three branches, one
  diverged left for a full run): it proves the ff batch lands several branches in
  one invocation with compile-only verification and no test call, and it proves
  nothing about a real repo's conflict load.
