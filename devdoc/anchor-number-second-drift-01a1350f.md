<!-- doc-class: historical -->
# The anchor-number freeze table's "second drift" was ALREADY permitted in the tree this run inherited — and the briefing was RIGHT when it was claimed

Task `01a1350f-6fe6-72a9-9133-5cc90c5b1012` · host **peerbox** · base `fcc335e` ·
branch `task/01a1350f6fe672a991335cc90c5b1012` ·
worktree `/home/user/workspace/gopi-01a1350f6fe672a991335cc90c5b1012` ·
session evidence `~/.gopi/sessions/01a1350f-6fe6-72a9-9133-5cc90c5b1012/artifacts/`

**Read the CORRECTION section (§ "The briefing's premise, measured") before the
conclusion**: this report's first two drafts blamed the submitter, and the
measurement refutes that. The short version — the brief was accurate at the
claim instant (19:39:26, `--check` RED with no permit for either key) and its
premise expired before this run's ground was cut (20:03:01, base `fcc335e`, where
the permit IS present). The ask arrived already satisfied, through no one's error.

## The briefing's premise, measured

The briefing stated an unpermitted anchor-NUMBER drift was live:

> `devdoc/hp-push-topology-audit.md#996` records
> `anchor cmd/agent/cli_tasks_worker.go:638 at serveGitSyncSweep(workerRoleLabel)`,
> which has drifted to `:650`, and this row is **not** in task `01a134d4`'s
> complement list (which named only the `scratch-gc-flight-join.md` row).

Both halves of the FACT are true, and neither implies the conclusion. The permit
was already in the shipped table — at data row 97, carrying the same reason shape
as its sibling:

```
$ grep -n 'hp-push-topology' internal/docclass/testdata/anchor-number-baseline.tsv
97:devdoc/hp-push-topology-audit.md  number-drifted  cmd/agent/cli_tasks_worker.go:638  serveGitSyncSweep(workerRoleLabel)  first-frozen at 0b3c1de29; the recorded text is present in its file, but not on the recorded line
```

and the three acceptance commands were **green on the untouched base** (`fcc335e`),
before any edit in this run:

```
$ go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check
anchor-number-census: scanned 249 anchor row(s), 166 incoherent; table carries 166 row(s)
anchor-number-census: header figures re-derived and equal (frozen=166 pre-existing=152 first-frozen=14)
anchor-number-census: table matches the tree — the freeze is exact
exit=0

$ ./bin/gopi tool doccheck
doccheck:   166 of 166 are frozen in internal/docclass/testdata/anchor-number-baseline.tsv ...
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
exit=0
```

**Where the briefing's reading came from, re-derived (Rule 38 — every figure
re-run here).** `devdoc/hp-push-topology-audit.md#996` is a `DOC-INDEX.tsv` LINE
number, and that line moved three times inside one afternoon, because the
manifest is append-heavy:

```
38577cc3e 19:17:31   row at DOC-INDEX line 996
cd0ef2d18 19:56:43   row at DOC-INDEX line 997
fcc335ebb 19:02:01   row at DOC-INDEX line 998   ← the base this run was cut at
```

The row `01a134d4` measured its drift list on tip `82f8b9529` at **18:34:39**, and
that same table revision carried **zero** `hp-push-topology` rows:

```
$ git show 82f8b9529:internal/docclass/testdata/anchor-number-baseline.tsv | grep -c 'hp-push-topology'
0
```

**CORRECTION (this is the third revision of this paragraph; the first two were
wrong, and my own measurements refuted them).** The briefing's tip `6ef428c1` is
real, its `--check` RED was real, and — measured — **the briefing was ACCURATE at
the instant the row was claimed.** It went stale before this run's GROUND was cut.
Two moments, and I collapsed them, which produced a cause that does not exist:

```
$ TIP=$(git rev-list --first-parent -1 --before='2026-10-13 19:40:00' master)   # the claim instant, created_at 19:39:26
$ echo $TIP
6ef428c1624e4d7490e459b0022490dc1195ab1d        # == the tip the brief itself names
$ git show $TIP:internal/docclass/testdata/anchor-number-baseline.tsv | grep -c '^[^#]'
163                      # the brief's own "table carries 163 row(s)" — reproduced exactly
$ git show $TIP:internal/docclass/testdata/anchor-number-baseline.tsv | grep -c 'hp-push-topology'
0                        # no permit → the drift really WAS unpermitted then
$ git show $TIP:cmd/agent/cli_tasks_worker.go | grep -n 'serveGitSyncSweep(workerRoleLabel)'
650:                     # against a manifest cell that says :638 → genuinely drifted
$ git merge-base --is-ancestor 38577cc3e $TIP; echo rc=$?
rc=1                     # the repair was NOT in master yet
$ git merge-base --is-ancestor $TIP 38577cc3e; echo rc=$?
rc=0
```

So the sequence, on COMMITTER time and ancestry (never author time — see below):

```
claim      19:39:26   master tip 6ef428c1   RED, 163 rows, no permit, code at :650   ← brief CORRECT
repair     38577cc3e (19:48:14), cd0ef2d18 (19:56:43) — both permits land
ground cut 20:03:01   base fcc335e          permit present as table row 97
repair reaches master's first-parent line   5604465ef (20:11:12)
```

**Blame is on the claim→ground gap, not on the submitter**: at claim time the
briefing was true, and the executor inherited a briefing whose premise had expired
in the interval. That is `01a0a028-f9e7-7a13-af01-0bee871b3cd0`'s shape ("the row
sat unclaimed… and the fix was re-landed before the row was claimed") **mirrored** —
here the fix landed after the claim and before the ground. No human and no model
wrote anything false at either end.

**The method error, recorded because it is the cheap trap.** I first dated the
chain by AUTHOR time, which is not monotone with history on this branch (delivery-
point syncs rebase/carry commits):

```
38577cc3e  author 19:17:31  committer 19:48:14
fcc335e    author 19:02:01  committer 20:00:18
9de0a2c63  author 19:16:48  committer 19:59:19
```

Author dates put the repair "before" a base that is not its ancestor, which is
exactly how the wrong story got told. **Judge dispatch-time state with committer
dates AND ancestry:**

```
TIP=$(git rev-list --first-parent -1 --before='<the row's created_at>' master)
git merge-base --is-ancestor <fix-commit> $TIP          # rc=1 ⇒ the premise WAS valid at claim
git show $TIP:<the generated table> | grep -c '<key>'   # 0  ⇒ the drift really was unpermitted then
```

Note also that `#996` in the briefing is a `DOC-INDEX.tsv` LINE number and it
moves with every append; the same measurement in author dates gave 995/996/997/998
across five revisions of one afternoon, so it is not a stable identifier for
anything.

Separately — and this half is solid — the row's `source` attribution for the drift
is HALF WRONG. `30350b652` introduced the doc AND the manifest cell together:

```
$ git show 30350b652 -- devdoc/DOC-INDEX.tsv
-devdoc/hp-push-topology-audit.md	normative	… (removed)
+devdoc/hp-push-topology-audit.md	normative	… (re-added)
+devdoc/integration-ref-cadence-on-worker-tick.md	normative	…
$ git log -S 'anchor cmd/agent/cli_tasks_worker.go:638 at serveGitSyncSweep(workerRoleLabel)' --oneline -- devdoc/DOC-INDEX.tsv
30350b652 feat(sync): advance the integration ref from the worker tick, and say when it is stale
$ git show 30350b652:cmd/agent/cli_tasks_worker.go | grep -n 'serveGitSyncSweep(workerRoleLabel)'
638:
$ git show 30350b652:devdoc/DOC-INDEX.tsv | grep 'hp-push-topology' | cut -f5
anchor cmd/agent/cli_tasks_worker.go:638 at serveGitSyncSweep(workerRoleLabel)      # coherent by construction
```

The mover was 8 minutes later and it was NOT the doc's commit:

```
$ git log --format='%h %cd %s' --date=iso -2 -- cmd/agent/cli_tasks_worker.go
c37219666 2026-10-13 18:32:27 +0800 delivery-point sync: merge master into the delivered branch before close-out
30350b652 2026-10-13 18:24:47 +0800 feat(sync): advance the integration ref from the worker tick, and say when it is stale
```

i.e. a **delivery-point sync (Rule 30a's own close-out merge)** moved the line a
same-afternoon commit had anchored — the `docclass-freeze-sibling-merge.md` family.

The permit landed at **19:17:31**, in `38577cc3e` — a commit that names BOTH drifts
in its own message:

```
$ git show 38577cc3e --format=%B -s
chore(docclass): re-emit the frozen anchor-NUMBER baseline on current master
...
Measured on 9d8626db9: 249 anchor row(s) scanned, 165 incoherent; the table
carried 163 rows and 2 drifts were unpermitted, so
TestAnchorNumberGateOnTheRealTree was RED on the clean master tip:
  devdoc/hp-push-topology-audit.md  cmd/agent/cli_tasks_worker.go:638
    serveGitSyncSweep(workerRoleLabel)                -> recorded text at :650
  devdoc/scratch-gc-flight-join.md  cmd/agent/cli_worker_control.go:2167
    func (c workerDaemonCtx) installFileWatcher()...  -> recorded text at :2183

$ git merge-base --is-ancestor 38577cc3e HEAD; echo exit=$?
exit=0
```

So `01a134d4`'s list was **correct for its own tree**, `38577cc3e` closed both
rows 43 minutes later, and the briefing — written from `38577cc3e`'s manifest —
compared an old task's list against a newer table and read the difference as open
work. **Nothing was broken; the ask was already delivered.**

**The mechanism that moved the line** is worth naming, because it is the same
family already on record as `devdoc/docclass-freeze-sibling-merge.md`: the drift
was minted by `c37219666 "delivery-point sync: merge master into the delivered
branch before close-out"` (18:32:27), i.e. the branch-alignment merge the Rule 30a
close-out runs — not by the feature commit that first wrote the call site
(`30350b652`, 18:24:47, where the text still sat on `:638`).

```
$ git show 30350b652:cmd/agent/cli_tasks_worker.go | grep -n 'serveGitSyncSweep(workerRoleLabel)'
638:
$ git show c37219666:cmd/agent/cli_tasks_worker.go | grep -n 'serveGitSyncSweep(workerRoleLabel)'
650:
```

## What this run did, and what it deliberately did NOT do

The deliverable as written ("regenerated table carrying the second drift's
permit") is satisfied by the SHIPPED table, so the honest act was to **prove it**
rather than manufacture a diff:

1. **Ran every acceptance command on the clean base** — all green (above and below).
2. **Proved the shipped table is its own writer's fixpoint**: regenerated
   in-place with the table's only legal writer, and the worktree came back
   byte-identical (`git status --porcelain` empty, `git diff` empty). A
   regeneration commit here would have been a **no-op commit** — Rule 38's
   "the table is a generated artifact" plus Rule 30's clean-tree criterion.
3. **Measured the two sibling freeze tables** — both already current, so no
   sibling work exists:

```
$ go run internal/docclass/testdata/prose-path-line-census.go . <tmp>   # regenerate
...: wrote 17 frozen citation(s), 1843 mention(s) scanned, 924 resolvable, 21 past EOF
$ diff internal/docclass/testdata/prose-path-line-baseline.tsv <tmp>    # → empty, exit 0
$ go run internal/docclass/testdata/prose-anchor-census.go . <tmp>
...: UP TO DATE — 0 frozen citation(s), 12 scanned
$ diff internal/docclass/testdata/prose-anchor-baseline.tsv <tmp>       # → empty, exit 0
```

4. **Filed the finding as its own bug row** rather than leaving it in this report
   only, per Rule 41. **Two rows, and the second corrects the first**: my initial
   row (`01a13534-13bf`) claimed the briefing was stale at submit time; the
   measurement above refuted it, so its payload was **revised** to the accurate
   shape (premise valid at CLAIM, expired before GROUND). The sibling row
   (`01a13534-c0e1`, the unowned devdoc-BODY number carrier) was un-parked from
   that chain, since its evidence does not depend on this row's conclusion.

**Why not "regenerate anyway and commit the file"**: the shipped table at
`fcc335e` already carries row 97 for exactly this key, so a regeneration writes
the same bytes; committing it would be an empty commit claiming a repair. The
counterfactual was run to show the row is load-bearing (see Evidence `C2`).

## Not Done (honest)

- **No table bytes changed in this branch.** The branch is base + this report +
  its manifest row. That is the measured truth: there was nothing to repair.
- **`01a134d4`'s other rows untouched** — `[B]` (`gopi merge -h` over budget) and
  `[C]` (web shutdown credential) are other work items, out of this row's scope.
- **The 166 frozen accelerators are still wrong numbers** for these documents.
  That is the accepted residual of `devdoc/docclass-anchor-residual-decision-20261012.md`
  (the classifier copies a doc's inline `path.go:NNN` reference verbatim, so the
  pair is coherent at write time and the number cannot be repaired by a
  regeneration). Not re-litigated here; naming it so it is not read as an oversight.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK
- Tests: `make test-budget PKGS='./internal/docclass ./internal/docindex'` →
  `exit=0 outcome=pass`, 2 packages finished, **0 with a failure**, 149 tests ran
  (17s wall); raw stream `run-1791893385-2845809.json`. The counterfactual test
  the briefing warned about (`TestProsePathLineOneStaleRowEventOneVerdict`,
  1.63s) is inside that scope and passed — a legitimate regeneration drains
  nothing here, because **no regeneration happened**.
- Ran: the three acceptance commands verbatim, plus the two sibling generators —
  `go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv --check` ;
  `./bin/gopi tool doccheck` ; `go test ./internal/docclass/ -run 'TestAnchorNumber' -count=1` ;
  `for t in prose-path-line prose-anchor; do go run internal/docclass/testdata/$t-census.go . <artifacts>/$t.tsv; done`
- Result: `--check` → `table matches the tree — the freeze is exact`, **exit 0** ;
  `doccheck` → `the devdoc corpus is consistent`, **exit 0** ;
  `go test -run TestAnchorNumber` → `ok github.com/nick/gopi/internal/docclass 7.070s` ;
  both sibling tables byte-identical to their own regeneration (**diff exit 0**) ;
  in-place regeneration of the anchor-number table left `git status --porcelain`
  **empty** ⇒ generator is a fixpoint (idempotent) at this base.
- Commit: see the branch's tip (report + manifest row; no table change).

## Evidence (self-filed)

Raw output of each claim, archived under
`~/.gopi/sessions/01a1350f-6fe6-72a9-9133-5cc90c5b1012/artifacts/`:

| claim | command | observation |
|---|---|---|
| `--check` green | `anchor-number-census.go . <table> --check` | `accept-a2-check.out` — `table matches the tree — the freeze is exact`, exit 0 |
| `doccheck` green | `./bin/gopi tool doccheck` | `accept-a3-doccheck.out` — `166 of 166 are frozen …`, exit 0 |
| anchor gate green | `go test ./internal/docclass/ -run TestAnchorNumber -count=1` | `accept-a4-anchornumber-test.out` — `ok … 7.070s` |
| scoped suite green | `make test-budget PKGS='./internal/docclass ./internal/docindex'` | `accept-a5-…out` — `exit=0 outcome=pass`, 2 pkg, 0 failures |
| generator idempotent | in-place regeneration | `accept-a1-regen-inplace.out` — wrote 166 rows; `git status --porcelain` printed **nothing** |
| siblings current | both generators + `diff` | `accept-a6-siblings-and-idempotence.out` — both diffs empty, exit 0 |
| the permit is load-bearing | drop row 97 from a copy, re-run `--check` | `counterfactual-no-hppush.out` — `1 unpermitted`, `HEADER FIGURE IS NOT THE MEASUREMENT: … frozen=166 while this run re-derived frozen=165`, exit 1 |
| **the brief was VALID at claim** | `TIP=$(git rev-list --first-parent -1 --before='2026-10-13 19:40:00' master)` → `6ef428c1`; `git merge-base --is-ancestor 38577cc3e $TIP` → rc=1; `git show $TIP:<table> \| grep -c hp-push` → **0**; `$TIP:cmd/agent/cli_tasks_worker.go` → text at **:650** | `evidence-was-the-brief-stale-at-dispatch.out` |
| the repair was NOT in master at claim | `git merge-base --is-ancestor $TIP 38577cc3e` → rc=0 | same file |
| author ≠ committer on this chain | `%ad` vs `%cd`: 38577cc3e 19:17:31/19:48:14; fcc335e 19:02:01/20:00:18 | `evidence-dispatch-vs-grading-timeline.out` |
| the drift's real mover | `git log -2 -- cmd/agent/cli_tasks_worker.go` | `evidence-attribution-check.out` — `30350b652` (638, coherent with the cell it wrote) then `c37219666` (delivery-point sync → 650) |
| DOC-INDEX line drift (author-time framing, superseded) | `git show <rev>:devdoc/DOC-INDEX.tsv \| grep -n hp-push-topology-audit` | `996` @38577cc3e, `997` @cd0ef2d18, `998` @fcc335e |

Measured vs inferred, kept apart: every row above is a command and its raw output.
This report's first two drafts of the "why" paragraph were **inferences and both
were wrong**; they are superseded by the table above and the CORRECTION section.
The remaining inference — that the drift recurs because a close-out merge moves
lines that same-afternoon commits anchored — is labelled as the reading.

## The lesson worth keeping

**Two lessons, and the second one cost this report two rewrites.**

1. A briefing that names a defect by a FREEZE-TABLE row must say which table
   revision it measured, because the table is regenerated on whatever branch is at
   hand and one afternoon can carry five revisions of it. The cheap preflight that
   would have caught this in seconds, run against the tree you actually have:

```
go run internal/docclass/testdata/anchor-number-census.go . \
    internal/docclass/testdata/anchor-number-baseline.tsv --check   # exit 0 ⇒ nothing to repair
```

2. **Do not judge a briefing by the tree you inherited — judge it by the tree that
   was current when the row was CLAIMED.** A claim and a ground cut are different
   instants and this branch moves fast between them. The instrument:

```
TIP=$(git rev-list --first-parent -1 --before='<created_at>' master)
git merge-base --is-ancestor <the fix> $TIP ;  echo rc=$?     # rc=1 ⇒ the premise WAS valid
git show $TIP:<the generated table> | grep -c '<key>'          # 0    ⇒ the drift really was live
```

and date the chain by **committer** time (`%cd`), never author time (`%ad`):
delivery-point syncs carry and rebase commits, so author dates do not sort with
history and they will tell you a false story that looks fully evidenced.
