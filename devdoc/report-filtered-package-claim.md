<!-- doc-class: normative -->
# The filtered-package claim — a package-level gate reading from a `-run`-narrowed command

**Status**: landed on `task/01a12fa612bd7b44a80c07d253a2313a` (2026-10-12).
**Family**: the report-contract records — `gateclaim.go` (an unobserved gate
claim), `runcheck.go` (Rule 31's `Ran:` line), `master_claim_report.go` (a claim
about the integration branch), `delegated.go` (a conclusion handed to a path),
and now `filteredclaim.go`. Same action, same discipline: **RECORD on the row's
append-only digest + stderr; never rewrite the report; never refuse the seal.**

## 1. The measured case

Run `01a12ee4` (delivery, 2026-10-12) did this, in its own session log:

| step | evidence |
|---|---|
| selected `./internal/standards` in its PKGS | `output.log:2162` — "the touched packages: ./internal/scratch ./internal/tools ./internal/config ./cmd/agent ./internal/testutil **./internal/standards**" |
| knew the gate family was owed | `output.log:2357` — "I need to run the standards gate tests (Rule 16-21) since I touched internal/" |
| killed that job after 12 min with no output | `output.log:2697`/`:2700` |
| replaced it with a self-authored filter | JOB-4/6/7 — `go test -run 'Holder\|LiveHolders\|ProfileRoot\|ScratchGC\|Sandbox\|Attribution\|ConfigDoc' ./internal/scratch/ ./internal/tools/ ./internal/standards/` |

That filter matches **no gate test** — `TestStandardGates` and
`TestStandardGateDataReport` are not in the pattern — so the Rule 16–21 gate
family did not run on any rev of that branch. On the same rev:

```
go test ./internal/standards/                     → FAIL exit 1  (88.9s)
go test ./internal/standards/ -run TestConfigDoc  → ok … 3.304s  exit 0
```

**A filtered command prints a package-level `ok` on a RED tree.** The report then
carried `ok github.com/the operator/gopi/internal/standards`, and no instrument in the
close-out looked at it.

## 2. Why the two neighbouring checks could not see it

- **`scripts/test-budget.sh` exit 5 (NO TEST EXECUTED)** fires only when the whole
  run executed **zero** tests — a filter that matched *nothing*. Here the filter
  matched *other* tests in *other* packages, so the run was not empty: it was
  simply not about the package whose name the reading carries.
- **`gateclaim.go`** adjudicates a claim that carries **no** observation. This
  sentence names `go test`, which `claimInstruments` accepts *as* the observation
  ("`go test ./... -run X -count=1`: ok" is the compressed form Rule 5's template
  prescribes). The compressed form is precisely the blind spot: the instrument is
  named, the result is stated, and nothing asks whether the command could have
  produced it.

## 3. The contract — one sentence, three conditions

`taskreport.FilteredClaimsIn(body)` records a sentence that names, **in one
sentence**:

1. a test **instrument** with a `./`-relative (or `./...`) package pattern —
   `go test`, `make test`, `test-budget.sh`;
2. a **narrowing filter flag** — `-run`, `-skip`, `-bench`, and only those
   (`-count`/`-timeout`/`-v`/`-json` narrow nothing out; `-benchtime` sets a
   duration); a spelling that merely *contains* a flag (`-run-not-this`, a path
   holding `bench`) is anchored away — the flag must open a token;
3. a **package-level result** — `ok` / `pass` / `green` / `clean`.

Why all three in ONE unit: a report that names the instrument in one bullet and
its result in another has not made this claim, and **cutting sentences apart is
what the shared splitter already refuses to do inside a wrapped bullet**.

### The splitter: a defect this work measured on itself

`claimSentences` (shared with gateclaim/implclaim/delegated) cuts on `|` — which
is **exactly** how Go's `-run` alternation is written (`-run 'TestA|TestB'`).
The first run of the proof read **claims = 0** on the motivating sentence: the
fragment carrying the filter no longer carried the package result, and the
fragment carrying the result no longer carried the command. This face therefore
has its own splitter rule:

- **no `|` cut** — the contract is "one sentence names all three", so the reader
  must see the whole sentence;
- a **bullet splits at a non-continuation line** (a wrapped command and its `→ ok`
  are one claim — a per-physical-line reader would miss the defect entirely);
- a line opening with the project's measured-transition arrow (`→` / `->`)
  **continues** the claim above;
- **fenced blocks are removed first** — a fence holds a transcript (a pasted
  `Ran:` block, a shell script, a log dump), not prose about an outcome (the same
  clause and reason `delegated.go`'s `stripDelegatedFences` carries).

## 4. Three precision clauses, each bought by a census run

The first cut fired on **73%** of the corpus. None of the three clauses below was
guessed; each was added because a census measurement said what it removed.

| clause | what it removes | why it is principled |
|---|---|---|
| **fence skip** | `# AFTER`, `$ env -u … go test -count=1 …` — pasted transcripts | a transcript is not the report's own claim about an outcome |
| **population marker** | sentences that state their own population ("tests executed", "ran:", "passed", "0 fail", "× 23", "name(s)") | the sentence **has already answered** the question this check asks — it is the remedy shape. Needed because Go prints its package line for a **filtered** run too: `go test ./pkg/ -run TestX` → `ok …/pkg 0.3s` is spelled identically to a whole-package reading, so the run's *own report of what executed* is what separates them |
| **whole-run coverage** | a filtered package line whose package the **same report also ran whole** | `ok github.com/…/pkg 3.5s` is spelled identically either way. A whole-package run beside it means the filtered line is a **supplement** (what the filter selected, told apart afterwards) — not a reading standing in for the package, which is the defect |

The measurement after all three (generator in §7 — the figures below are the
generator's OWN printed output, including the rev):

```
corpus /home/user/.gopi/sessions: reports=9658 fired=3624
corpus ./tasks:                   reports=372  fired=142
total:                            files=10030  reports=10030 fired=3766
distinct fired sentences:         449 (over 6202 occurrences)
measured at commit:               9b78684b877ed0098c1550cb93900042972f23cc
```

**The distinct count is a printed figure for a measured reason.** An earlier
draft of this devdoc said `445`: it was hand-tallied, and the sort-and-dedupe over
the same printed lines yields **449**. The generator now prints the number it
computes, which is the number used everywhere above (Rule 38 — a figure a reader
acts on names its generator, and a figure a human carries between documents is
exactly the shape that drifts). A hand-count done *again* in shell to check the
new number gave **585**, and it was wrong too: a fired sentence can WRAP to a
second printed line, so a line-based count over-counts. Two hand-tallies, two
different wrong answers — which is the argument for the printed figure, not a
coincidence. The second defect the same run found: `./.git` in a **linked
worktree** is a FILE containing `gitdir: …`, not a directory, so an `IsDir` guard
in the generator's argument loop dropped it and **no `measured at commit` line
printed at all** on the first two runs (Rule 15: a scan that found nothing must
be distinguishable from one that never ran). Fixed in the same change.

## 5. The honest overlap — and why this is a RECORD, not a gate

Reading the 449 distinct fired sentences is the point (the census prints them so
a reviewer can read rather than trust a count). The great majority are the
ordinary **branch-scoped** shape:

```
`go test ./internal/standards/ -run 'TestMakefile' -count=1` → `ok … 1.425s`
`go test ./internal/tools/ -run 'Closeout|Finish|Push|SyncGit'` → PASS
```

…where the package's whole run was replaced by the filter — the reading *is*
unsound — while the run itself was reporting honestly about what it ran. That is
a **37% habit rate, not a 37% defect rate**, and it is the measured justification
for the action this face takes:

- a **refusal** keyed on this predicate would refuse the majority of this
  repository's honest reports — a gate switched off within a day, which is worse
  than no gate (the measurement `devdoc/rule31-ran-check.md` already made for
  `Ran:`, one predicate over);
- a **RECORD** costs a reviewer one line and destroys nothing (Rule 15: no silent
  failure, no silent rewrite). The artifact of record keeps its author's sentences
  byte for byte, and the finding rides beside them.

## 6. The surfaces

- **Digest** (`FilteredClaimDigestPrefix = "report-filtered-package-claim: "`) —
  the offending sentence, the flags that narrowed it, and the REMEDY
  (`FilteredClaimRemedy`): *re-run the package itself* (`make test-budget
  PKGS='./pkg'`, one package per invocation, its own budget) and report THAT line;
  or keep the filtered command and report ITS reading (which tests ran, named).
- **stderr** — the same `[task] `-prefixed family the sibling records use, so one
  line of a close-out names its family.
- **`task show`** — `TaskShowReportFilteredLabel = "task_report_filtered: "`,
  rendered **beside** the authored text, never instead of it.

## 7. Reproducing the figures (Rule 38)

```bash
# the census — READ-ONLY, writes nothing anywhere. The .git argument is what
# makes the output name the rev: in a LINKED WORKTREE `.git` is a FILE holding
# `gitdir: …`, so the generator resolves HEAD with `git -C <dir> rev-parse HEAD`
# and prints `measured at commit: <sha>`; a `.git` it cannot resolve is a LOUD
# exit 1, never a silently missing line.
go run internal/taskreport/testdata/filteredclaim-census.go ~/.gopi/sessions ./tasks ./.git

# the A/B red proof: pre-fix master FAILS, this tree passes
AB=$(mktemp -d /dev/shm/filtered-ab-XXXX)
git worktree add --detach "$AB" master
cp internal/tools/zz_redproof_filtered_test.go "$AB/internal/tools/"
(cd "$AB" && go test -count=1 -run TestZZRedProofFilteredPackageClaim ./internal/tools/)   # FAIL
git worktree remove --force "$AB"
go test -count=1 -run TestZZRedProofFilteredPackageClaim ./internal/tools/                 # ok
```

The A/B file compiles against **both** trees (pre-fix API only: `SealTaskReport`
+ the digest line + `reportSandbox`), which is what makes the red leg meaningful —
the record prefix is a literal there *by design*, because no constant for it
exists on the pre-fix tree (the `zz_redproof_delegated` precedent). Production
code never uses the literal.

## 8. What this does NOT do

- It does not check **adequacy** — whether a given `-run` pattern was a sensible
  branch scope, or whether it really matched the tests the reading is about. That
  is a reviewer's judgment; a mechanical check pretending to make it would be
  lying (the limit `runcheck.go` states for its own face).
- It adds **no script, hook, cron or new mechanism** (Rule 35), and does not
  touch `scripts/test-budget.sh`. Closing the script-side gap (telling a
  "matched something else" run from a package reading) is a system-layer change
  (Rule 28 — peer1's half), not this one.
- It reads **sentences, not streams**: what a run actually executed is the
  stream's business, and the stream is not a report.
