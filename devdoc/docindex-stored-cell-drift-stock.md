<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
> **The gate this page argues for is gone too.** `TestTheShippedCorpusCarriesNoStoredCellDrift`
> lived in `internal/docindex`, which was cut together with `gopi tool doccheck` in
> `e6bfa10`, so the tenant this page installed on the test path is no longer there:
> the design record below stands, its enforcement does not, and nothing measures
> stored-cell drift today. Likewise `TestDocAnchorsResolveInNormativeDocs`
> (`internal/agent`) was retired in `5a35f61` with `internal/docclass`, so a
> `file.go:NNN` reference on this page is a landing-time record, not a checked anchor.
# Stored-cell drift is a STOCK, not a write-seam leak — and now the corpus's own gate asserts it

**Status:** normative. Measured 2026-10-17, task `01a14445`, base `942b3ac6a`
(`refs/remotes/gitPushcheck/master`; `refs/heads/master` was `cc846ee` at the same
moment and `942b3ac` is its descendant — **every reading in this document is taken
on `942b3ac`**, and the ancestry verdict does not depend on which of the two you
read a file from, because the conclusion is a statement about *when a row was
written relative to `55c9bcfb0`*, not about a tree state).

Evidence: `~/.gopi/sessions/01a14445-cc60-7211-9726-9c2094ffa6f3/artifacts/`
(`doccheck-before-942b3ac.txt`, `classify-check-before.txt`, `ancestry-942b3ac.txt`,
`gate-red-arm-before-render.txt`, `gate-green-after-render.txt`,
`generator-blocked-by-duplicate-row.txt`, `classify-check-after.txt`).

## 1. The reading: 15 rows, on a tree whose suite is green

```
$ ./bin/gopi tool doccheck                       # exit 1
doccheck: stored-cell drift: 15 of 2620 rendered row(s) whose stored text is not
          what the classifier renders today
verdict: drift

$ python3 tools/docclass/classify.py . --check   # exit 1
15 row(s) whose stored text is not what the classifier renders today
```

Two earlier readings of the SAME corpus, on two ancestors of this base:

| base | drifted rows |
|---|---|
| `61b0846` | 4 of 2583 (task `01a142d1`) |
| `4d792f6` | 12 of 2583 (same task, later re-measure) |
| `942b3ac` (this run) | **15 of 2620** |

The readings are not contradictory — the bases are ancestors and the corpus grew
2583 → 2620. **The population GROWS.** That is the fact this document is about,
and it is the reason a one-off re-derivation is not a fix.

## 2. The root-cause criterion: it is a STOCK, and ancestry proves it

The write-seam repair — "re-render a stored row after the document's LAST write"
— is `55c9bcfb0` (2026-10-16 14:22:53 +0800). It is an **ancestor of this base**:

```
$ git merge-base --is-ancestor 55c9bcfb0 HEAD && echo YES
YES
```

For each of the 15 drifted paths, take its last-write commit and ask whether it is
BEFORE or AFTER that repair:

```
$ for p in <the 15 paths>; do
    c=$(git log -1 --format=%h -- "$p")
    git merge-base --is-ancestor "$c" 55c9bcfb0 && echo "BEFORE $c $p" || echo "AFTER $c $p"
  done
BEFORE-fix=13  AFTER-fix=2  total=15
```

**The verdict is a shape, not a vote.** Both numbers point the same way, and they
are not the same finding:

- **13 BEFORE-fix.** These rows were written *before the repair existed*. The
  repair re-renders a row **at write time**, so it cannot reach a row nobody writes
  again. This is the **stock**: rows frozen at the moment of their last write, and
  a hook that runs only on writes is by construction unreachable for them. The
  repair is correct and it does not, and cannot, clean up history.
- **2 AFTER-fix.** `devdoc/branch-lifecycle-ruling.md` (last write `c15c7165f`,
  07:22:54) and `devdoc/gopi-tools-maintenance-help-surface-inventory.md` (`99f3d9f4a`,
  07:16:53). These were written *after* the repair landed — so if the repair were
  the whole story they should be clean. They are not, and the reason is the more
  important half: **the drift is not caused by the document moving, it is caused by
  the document's SOURCE moving afterwards.**

Measured on `branch-lifecycle-ruling.md`: its cell drifted from
`cited by cmd/agent/merge_prompt.md:742` to `:745`. The document itself did not
change between those two readings — `cmd/agent/merge_prompt.md` did:

```
$ git log -1 --format=%h -- devdoc/branch-lifecycle-ruling.md   # c15c7165f 07:22:54
$ git log -1 --format=%h -- cmd/agent/merge_prompt.md           # 2a0d88acc 06:41:08
                                                               #   … and later commits
$ git merge-base --is-ancestor c15c7165f 2a0d88acc && echo YES
YES   # the cited SOURCE moved AFTER the row was rendered
```

Note the direction: `merge_prompt.md`'s `2a0d88acc` (06:41) is *older* than the
doc's `c15c7165f` (07:22) — but `c15c7165f` did not **re-render this row** (it added
the row of a *different* file: `git show c15c7165f -- devdoc/DOC-INDEX.tsv` is
`1 insertion`, and it names `branch-lifecycle-ruling` zero times). So the row kept
text rendered against an older `merge_prompt.md`, while the file moved to line 745
somewhere in the merge chain. Either way the pattern holds and it is the general
one:

> **A row is a snapshot of a RENDER INPUT, and part of that input lives in a file
> other than the document.** Re-rendering on write refreshes the snapshot at a
> moment; any later commit to the *cited source* — a new function above the cited
> line, a replaced block, a `stale=N` count that counts differently — invalidates
> it without touching the document and therefore without waking the hook.

The drift **shapes** confirm this directly, from the verb's own echo (which is why
it windows the two cells around their divergence):

| shape | example |
|---|---|
| line number in a citation moved | `merge_prompt.md:742` → `:745` |
| the ANCHORED TARGET moved to another file | `anchor cmd/agent/merge_lock.go:…` → `anchor cmd/agent/merge_run.go:…` |
| a derived count changed | `stale=1` → `stale=2`, `standing 3` → `standing 0` |
| the stub set drifted | `stale=4` → `stale=2`, `stale=8` → `stale=10` |

None of these is "the document was edited badly". Every one is *the source tree
moving under a citation*.

## 3. Why one-off re-derivation only POSTPONES the red

The repair is `python3 tools/docclass/classify.py . --only-file <list>` (never a
hand edit — the manifest is DATA, its only writer is the generator). It was run in
this task: `{"updated": 15, "rows": 2620}`, and both faces then read
`0 row(s) whose stored text is not what the classifier renders today` /
`stored-cell drift: 0 of 2620`.

That number is **true at the instant it was measured and expiring by construction**,
because the thing it compares — `render(doc, tree)` — is a function of a tree that
commits keep changing. Two concrete mechanisms will put rows back:

1. **Every merge that moves a cited line** re-invalidates the citing row. The
   corpus cites `cmd/agent/*.go`, `internal/tools/*.go` and `man/*.md` in hundreds
   of `reason`/`evidence` cells; this repository merges every few minutes.
2. **The corpus indexes itself.** `devdoc/DOC-INDEX.tsv` is a file under `devdoc/`,
   so a corpus-wide re-derivation writes a row whose own text is rendered from a
   tree that included the previous manifest. (This is why the manifest's own row has
   TWO renderers by design, and why both faces step aside for it — see
   `DocclassDriftRows`' step-aside and `stale_rows`' `SELF_ROW_STALE_NOTICE`.)

So the honest statement is: **the stock was cleared, the mechanism that refills it
was not removed.** 4 → 12 → 15 is that mechanism, measured three times.

## 4. What this task delivered: the gate the question was missing

Every piece of the stored-cell question already existed and every proof of it was
green while the corpus carried 15 drifted rows:

```
$ ./bin/gopi tool doccheck     → exit 1, verdict: drift, 15 of 2620
$ go test ./internal/docindex/ → PASS
```

A question that is only asked when a human runs a verb is a question the **build
does not ask**, so its answer is free to rot between operator runs — and it did.
The missing piece was therefore not a better comparison but a **tenant for it on
the test path**:

`internal/docindex/docclass_drift_corpus_test.go`

- **`TestTheShippedCorpusCarriesNoStoredCellDrift`** — this repository's own
  manifest, rendered by this repository's own classifier, must carry **zero**
  drifted rows. A read floor (`docclassDriftCorpusReadFloor`, measured 2620) keeps
  a fragmentary walk from passing by finding nothing, and an **abstention is a
  FAILURE here**, not a skip: `tools/docclass/classify.py` is data of this tree, so
  "the classifier did not render" is a fact about the checkout the build runs in.
  The failure message names every drifted row **with the field that moved** (through
  `DocclassDriftFields`, the verb's own renderer — one wording, not two) and prints
  the one repair command.
- **`TestTheStoredCellGateFiresOnADriftedFixtureRow`** — the red arm, on a fixture
  tree nobody else reads, built by the **real** generator and then given ONE wrong
  evidence cell. It asserts: clean before the drift, exactly one row red after it,
  and **green again after ONE run of the corpus's own `--only` repair**. A predicate
  that cannot go red is not a check, and a repair that does not clear what the gate
  names is advice that does not work.

The gate lives in `internal/docindex` rather than `internal/standards` because the
population is a property of the CORPUS and this package is the corpus's own reader —
it owns `DocclassDrift`, the manifest parser and the abstention contract. A gate in
`standards` would import this package and become a second call site of one policy.

## 5. Not done, and whose it is

- **The write hook's TIMING is not fixed here, and cannot be.** The remaining
  mechanism is: a row's render input includes files other than the document, so
  "re-render on write" is a partial trigger by construction. Closing it needs either
  (a) a re-render triggered by *the cited source* changing — a scheduling question
  that belongs to the system layer (**peerbox**, Rule 28), or (b) making the expiring
  anchors render in a non-expiring form (a `path` citation with no line number, a
  count that is not rendered) — a **classifier shape change** in `tools/docclass`
  with its own blast radius across ~2600 rows. Both are larger than this row's
  budget, and (a) is explicitly out of scope for a client host.
- **The gate's cost is real.** It runs the classifier over the whole corpus: ~66s in
  `internal/docindex`, inside the package's 45m budget. It is therefore in the smoke
  set by default and is the slow test in that package. Making it cheaper is a
  separate question (it renders the corpus once per package run; a shared render
  across packages would be the obvious direction).
- **A duplicate row blocked the generator for a while** and that is a genuine
  finding, filed separately: `devdoc/DOC-INDEX.tsv` carried THREE rows for
  `devdoc/merge-compile-only-policy.md` (lines 1290, 2630, 2631 — a merge union),
  and `classify.py`'s writer refuses **every** write while that holds, including the
  repair its own message recommends. Both integration refs carried it. The approved
  repair (`./bin/gopi tool doccheck --fix`, which drops duplicate rows to the last
  one) cleared it — **no hand edit of the manifest was made** (Rule 36).

## 6. The criterion, in one paragraph, for the next reader

If `gopi tool doccheck` reports stored-cell drift, do not look for a recent bad
edit. Run the ancestry check: `git merge-base --is-ancestor 55c9bcfb0 <path's
last-write>` for each drifted path. Rows BEFORE the write-seam repair are **stock**
and no hook can reach them; rows AFTER it are not a regression of that hook either —
they are rows whose **cited source moved after the row was rendered**, which the hook
has never claimed to cover. Clear the stock with the generator (`--only` /
`--only-file`, never a hand edit), and note in the report that clearing it does not
stop it refilling: the population is a function of a moving tree, and the durable
answer is either a re-render keyed on the cited source (system layer) or anchors that
cannot expire (classifier shape). What this repository now has is a gate that fails
the build when the stock is non-zero, so the number can no longer be discovered by
accident.
