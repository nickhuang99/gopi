<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown — slices 1/5 and 3/5).**
> `gopi tool doccheck` and `tools/docclass/` were DELETED (`e6bfa10`,
> `29882b3`) and this page is no exception: the command quoted below is the
> RECORD of what a past run executed — it cannot be re-run, and no document may
> be classified, verified or frozen by running it.
# The anchor-NUMBER drift of `devdoc/merge-backlog-terminal-guard.md` was already frozen one hour before this task was filed

**Task:** `01a136a7-dccd-7092-921b-b71c7be980ea` · branch `task/01a136a7dccd7092921bb71c7be980ea` · cut point `c06e04e22`
**Verdict of this run: the premise of the briefing was FALSIFIED — nothing needed to change, and nothing changed.**

## What the briefing asserted, and what the tree measured

The briefing said: on master `gopi tool doccheck` exits 1, `DOC-INDEX.tsv` row #1212
(`devdoc/merge-backlog-terminal-guard.md`, class `normative`) records
`anchor internal/tools/tasks.go:2825 at }`, line 2825 now holds a comment, so the
anchor-NUMBER leg reports an UNPERMITTED drift.

Every *fact* in that paragraph is still true — row #1212 is verbatim as quoted,
`internal/tools/tasks.go` is 9677 lines, `:111` is `}`, `:2825` is
`// here come from the SHARED classification (internal/sessionlayout.Records),`.
What is no longer true is the *conclusion*: the drift is not unpermitted. It is a
**frozen permit** in `internal/docclass/testdata/anchor-number-baseline.tsv:106`:

```
devdoc/merge-backlog-terminal-guard.md	number-drifted	internal/tools/tasks.go:2825	}	first-frozen at 0b3c1de29; the recorded text is present in its file, but not on the recorded line
```

At the branch cut point `c06e04e22` the real surface is **green**:

```
$ ./bin/gopi tool doccheck          # exit 0
doccheck: anchor NUMBER coherence (recorded text still present, line number stale):
doccheck:   168 of 168 are frozen in internal/docclass/testdata/anchor-number-baseline.tsv (...)
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
```

## When it was repaired — the commit, and why the briefing missed it

`34693afb3` — *"fix(docclass): freeze the anchor-NUMBER drift of
devdoc/merge-backlog-terminal-guard.md (frozen=168)"*, Wed Oct 14 **02:45:57 2026**
— is the repair, and its message names this exact defect (row 1212, `:2825` a
comment, `}` at `:111`, the shift introduced by `a78e444fa` after the `0b3c1de29`
freeze). It is an **ancestor of the cut point**:

```
$ git merge-base --is-ancestor 34693afb3 c06e04e22 && echo YES      → YES
$ git show 34693afb3^:internal/docclass/testdata/anchor-number-baseline.tsv | grep -c merge-backlog-terminal-guard
0                                                                   # the row did NOT exist before the fix
```

The branch cut point `c06e04e22` is timestamped **03:42:03**, i.e. **56 minutes
after** the repair. So the briefing was written against a master state that the
repair had already superseded — the anchor row was itself the artifact of the
freeze file, and the briefing's "row #1212 is drifted with no permit" reading came
from the table as it stood **before** `34693afb3`.

## What this run did — verification, not repair

Because the deliverable the briefing asked for (*"a regenerated
anchor-number-baseline.tsv, produced by its own generator"*) can only be produced
by running the generator, this run ran it — and the generator's own verdict is
that the file is already exactly the tree. The run therefore produced the
**measurement** rather than a diff, and proved the freeze is not vacuous:

- **Generator write form is a no-op (idempotence).** The only writer of the table
  was run against the committed file; sha256 was
  `00d63d5cd6197e81caa37532481f8fcff8ad1f1e694dffc4ad296a0defd92e16` before **and**
  after, `diff` reported byte-identity, and the write's own delta line read
  `0 permit(s) added, 0 permit(s) removed`.
- **The freeze is exact.** `--check` prints `table matches the tree — the freeze is
  exact`, exit 0, and re-derives the header figures it stores
  (`frozen=168 pre-existing=152 first-frozen=16`).
- **The red arm fires (the permit is load-bearing).** Removing **only** row 106
  from the table — the single row the briefing is about — makes the real surface
  fail with exactly the sentence the briefing predicted:

  ```
  doccheck:   NOT PERMITTED (drifts, absent from the baseline): devdoc/merge-backlog-terminal-guard.md#1212: anchor internal/tools/tasks.go:2825 at } — number-drifted (recorded text is at :111)
  exit=1
  ```

  Restored by content, then verified against an explicit revision (not the index):
  `test "$(git hash-object <table>)" = "$(git rev-parse HEAD:<table>)"` → equal.

That last pair is the whole point: the drift is real, the permit is the thing
keeping the gate green, and the permit is already the right one.

## Why nothing was committed

Deliverable 1 asked for a *regenerated* table on this branch. Regenerating it
produces **zero bytes of change** — there is no diff to commit that would not be
a fabricated one. Committing a no-op, or committing a hand-touched row, would each
be worse than committing nothing: the table is DATA whose only writer is the
census generator (Rule 38), and its bytes are already those the generator writes.
The remaining deliverables (evidence, report, and this devdoc) are the run's real
output, and this document is the only file it adds.

## The lesson

**A frozen-permit table makes "the gate is red" a claim with an expiry date.**
`doccheck`'s two legs are independent judgments: the anchor's CLAIM passed all
along, only the line ACCELERATOR was stale, and a permit for it was minted at
`34693afb3`. A briefing written from the red output alone — without re-running the
surface on the branch's own cut point — cannot see whether the permit landed after
the red was observed. The cheap discriminator, and the one this run used first, is
`git merge-base --is-ancestor <the suspected fix> <cut point>` plus
`grep -c <the doc> $(git show <fix>^:<table>)`: together they say whether the row
existed before the fix, i.e. whether the fix is the repair of the very row the
briefing describes.

## Evidence

Artifacts under `/home/user/.gopi/sessions/01a136a7-dccd-7092-921b-b71c7be980ea/artifacts/`:

| file | what it holds |
|---|---|
| `pre-generator-check.txt` | `--check` on the committed table, exit 0, freeze exact |
| `pre-doccheck.txt` | real surface before any action, exit 0 |
| `post-doccheck.txt` | real surface after the generator round-trip, exit 0 |
| `post-generator-check.txt` | `--check` after, exit 0, header figures re-derived |
| `anchor-number-baseline.pre-regen.tsv` | the table preserved outside the repo before the write |
| `regen-write.txt` | generator write form: `0 permit(s) added, 0 permit(s) removed` |
| `redproof-doccheck.txt` | the red arm: row 106 removed → `NOT PERMITTED … :2825`, exit 1 |
| `pre-sha.txt` | sha256 before (`00d63d5c…`) — identical after |

`make test-budget PKGS='./internal/docclass'` → `packages: 1 finished, 0 with a
failure`, exit 0. `git status --porcelain` in the worktree printed nothing at every
checkpoint.
