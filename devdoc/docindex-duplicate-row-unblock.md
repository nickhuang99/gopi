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
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# devdoc: the DOC-INDEX duplicate row that deadlocked its own writer — attribution and the unblock

**Record of one run.** Task `01a14763-2d2e-7d01-84a0-4caf1788779e`, branch
`task/01a147632d2e7d0184a04caf1788779e`, cut point `942b3ac6a`. This is the record of a
repair, not a standing rule: the class is `historical` for that reason.

## The symptom

`devdoc/DOC-INDEX.tsv` carried **three** rows for `devdoc/merge-compile-only-policy.md` —
two byte-identical `normative` rows plus one `historical` row. `tools/docclass/classify.py`'s
`splice_rows()` validated the **whole** manifest before writing, so **any** duplicated path
made it `SystemExit("refusing to write — duplicate row")` for **every** write, including a
brand-new document's row. That closed the sanctioned writer (Rule 36) and with it
`gopi tool doccheck --fix`.

```
$ grep -n "^devdoc/merge-compile-only-policy.md" devdoc/DOC-INDEX.tsv
1290:  ... normative   md5 629c50014871f1e3c61a1beac4b01ec0
2630:  ... normative   md5 629c50014871f1e3c61a1beac4b01ec0   <- byte-identical copy
2631:  ... historical  md5 eafb3877...

$ awk -F'\t' '{print $1}' devdoc/DOC-INDEX.tsv | sort | uniq -d
devdoc/merge-compile-only-policy.md          <- exactly ONE duplicated path
```

## The class: `historical`, from the document itself and from the generator

The document's own line 1 is `<!-- doc-class: historical -->`, and the generator's would-be
row (measured, `--dry-run`) is the `historical` one:

```
$ python3 tools/docclass/classify.py . --only devdoc/merge-compile-only-policy.md --dry-run
devdoc/merge-compile-only-policy.md	historical	historical (3 standing / 2 record): the doc's own title/name ...
```

Both faces agree, so the **`normative` row is the stale one**, whatever its line number. The
manifest needs no opinion here — the corpus decides it, and the two surviving `normative`
copies were the wrong class for this file.

## Who wrote the duplicate — four separate writes, not one rewrite

The recovery order that worked, cheap and exact: **walk the first-parent chain of the file
and record, per commit, the ordered list of that path's rows** (`git rev-list
--first-parent --reverse <base>..HEAD -- <path>`; per commit read the blob and collect
`(class, md5)` per row). Every change of that list names a writer:

| commit | when | rows after | what it did |
|---|---|---|---|
| `c5fca47cf` | 10-16 07:56 | 0 | the doc did not exist yet |
| **`fb1754f9d`** | 10-17 **01:55:14** | **2** (`historical` `eafb3877`, `normative` `629c5001`) | **`merge: the landing gate is COMPILE-ONLY for a mechanical landing`** — wrote **both** rows for the new doc in ONE commit (`git show --numstat` → `devdoc/DOC-INDEX.tsv \| 2 +`, both `+` lines are this path). Two writers disagreed about the class and neither removed the other's row. |
| `3b2d5f9e0` | 10-17 06:40 | 3 (`historical`,`normative`,`historical`) | appended a second `historical` copy (a third row, still not the byte-identical pair) |
| **`2a0d88acc`** | 10-17 **06:41** | **3** (`normative` `629c5001`, `normative` `629c5001`, `historical` `eafb3877`) | **`Merge master into task/01a14205670f7beb88632ea7929ea9e9 (delivery-point sync)` — THE producer of the byte-identical pair.** A **whole-file rewrite**: `git diff --numstat 3b2d5f9e0 2a0d88acc -- devdoc/DOC-INDEX.tsv` → `119 63`. Its first parent carried `historical,normative,historical`; the tree it wrote carries `normative,normative,historical` — it dropped a `historical` row and **re-emitted the `normative` row**, so one `629c5001` became two. Its other parent (`9adad635b`) carried **no row at all**, so the duplicate is not a union of two sides: it is one rewrite that re-rendered the path's row while keeping the row it was replacing. |
| `a98ec90a2` | 10-17 07:41 | 3 (same) | merge carried it forward; no conflict, because both sides agreed |

The single fact the briefing asked for: **`2a0d88acc` wrote the byte-identical duplicate.** Its
own shape is the advice for the next one — a whole-file rewrite re-derives rows instead of
replacing the row it meant to change, and a re-render whose output is *appended* rather than
matched by path produces exactly this pair. Replacing one row means "drop every row for this
path, then write the one the corpus decides"; it never means "write the new row and keep the old".

An earlier draft of this section named `2cec03314` as the producer. That was **wrong** and the
measurement is what corrected it: `git show --numstat 2cec03314 -- devdoc/DOC-INDEX.tsv` prints
**nothing** — that commit never touched the manifest. The row content it left behind is
identical to its parent's, which is why a class-signature scan alone appeared to implicate it;
the ordered `(class, md5)` scan above is the one that names a writer, because it compares a
commit to **all** of its parents, with the other parent's manifest read rather than assumed.

## Why no gate caught it before it was committed

Two separate answers, and the second is the load-bearing one.

1. **The gate that WOULD catch it exists and is red on it.** `TestDocManifestCoversEveryDevdocFile`
   (`internal/standards/docclass_gate_test.go:91`) fails on the second row, and
   `TestDocClassBannerMatchesManifest` would ALSO fail on any tree that keeps a `normative` row
   for this file (its banner says `historical`). Measured on the cut point, both red; after the
   repair, both green.
2. **The merge path does not run tests.** `internal/tools/merge_pass_verify.go` states the policy
   in code: after a pass's landings, ONE verification per repo — `go build ./...` then
   `go vet ./...`, deliberately **compile-only** (`mergePassTestVerb` = the verb the file must
   never spell; "NEVER THE TEST SUITE"). The corpus gates are `internal/standards` tests, so a
   merge can land a duplicated manifest with every merge-path check green. The branch's own
   worker is the only actor that could have run them, and `fb1754f9d` landed the doc and both
   disagreeing rows in one commit without them.

That second answer is the residual exposure, and it is unchanged by this repair: nothing on the
merge path rejects a manifest that carries a path twice.

## The repair (surgical, Rule 4 / Rule 36)

`devdoc/DOC-INDEX.tsv` is ~2600 lines of data. Two rows were deleted, the `historical` one kept.
Measured against the cut point, the manifest carries **two row deletions and exactly one
insertion** — and that insertion is **not** this repair's: it is this record's own row, written
by the generator (Rule 36) when this document landed:

```diff
$ git diff 942b3ac..HEAD --numstat -- devdoc/DOC-INDEX.tsv
1	2	devdoc/DOC-INDEX.tsv

$ git diff -U0 942b3ac..HEAD -- devdoc/DOC-INDEX.tsv | grep -E '^[+-][^+-]'
+devdoc/docindex-duplicate-row-unblock.md	historical	historical (5 standing / 2 record): …
-devdoc/merge-compile-only-policy.md	normative	normative: standing claim about master that names code that res…
-devdoc/merge-compile-only-policy.md	normative	normative: standing claim about master that names code that res…
```

The two `-` lines are the byte-identical `normative` rows, dropped; the `+` line is this
document's row. `grep -c '^devdoc/merge-compile-only-policy.md'` → `1`.

## The unblock, observed end to end

```
$ printf '<!-- doc-class: historical -->\n# probe: generator write acceptance\n\nRecord of a probe.\n' > devdoc/zz-genprobe-accept.md
$ python3 tools/docclass/classify.py . --only devdoc/zz-genprobe-accept.md --add-missing
{ "added": 1, "updated": 0, "rows": 2622 } total 2622
EXIT=0
$ grep -n '^devdoc/zz-genprobe-accept.md' devdoc/DOC-INDEX.tsv
2629:devdoc/zz-genprobe-accept.md	historical	historical (0 standing / 0 record): the doc's own title/name declares a past run's record, …
```

The invocation is slow by nature (212.9s for one path — the corpus render, not a hang), so it was
run as a background job and its output read once.

The probe document and its row were then removed (the probe proved the writer; it is not a
deliverable).

**`gopi tool doccheck --fix` — the surface the duplicate had actually deadlocked — writes too.**
Measured with a second probe:

```
$ printf '<!-- doc-class: historical -->\n# probe: doccheck --fix must write a row again\n…\n' > devdoc/zz-fixprobe.md
$ /usr/local/bin/gopi tool doccheck --fix
doccheck: file-with-no-row devdoc/zz-fixprobe.md — every file under devdoc/ must be classified
doccheck: repaired 1 path(s): devdoc/zz-fixprobe.md
$ grep -c '^devdoc/zz-fixprobe.md' devdoc/DOC-INDEX.tsv
1
```

It reaches the same writer through the Go face, and the row lands. Both probes were deleted and
the manifest restored afterwards (`git checkout --`, `git status --porcelain` empty).

**`gopi tool doccheck` writes rows again, and its exit code is 1 — for a reason that predates
this run.** Measured on the repaired tree (`gopi tool doccheck`, 293.7s, `EXIT=1`):

```
doccheck: 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
doccheck: stored-cell drift: 15 of 2621 rendered row(s) whose stored text is not what the classifier renders today
verdict: drift
```

The **gap** class — the one the duplicate lived in — is **0**, which is the unblock. The exit is
`drift`: 15 rows elsewhere carry stored text the classifier renders differently today (e.g.
`devdoc/branch-lifecycle-ruling.md`: stored `merge_prompt.md:742`, rendered `:745` — a line
number that moved under a doc this branch never touches). None of the 15 is
`devdoc/merge-compile-only-policy.md`, and `git diff --numstat 942b3ac..HEAD -- devdoc/` lists
only `DOC-INDEX.tsv` and the new record beside this one, so the drift is the cut point's, not
this repair's. Re-deriving those rows is each row's own generator run
(`classify.py . --only <path>`) and is **not** part of this task — it is left, named, for
whoever takes it.

## The red proof of the duplicate-row arm

Injecting the duplicate back (duplicate the surviving row one line below itself, giving the
path three rows again) and running the gate:

```
$ grep -c '^devdoc/merge-compile-only-policy.md' devdoc/DOC-INDEX.tsv
3
$ go test ./internal/standards/ -run 'TestDocManifestCoversEveryDevdocFile' -count=1
--- FAIL: TestDocManifestCoversEveryDevdocFile (0.59s)
    docclass_gate_test.go:91: devdoc/DOC-INDEX.tsv:2631: duplicate row for devdoc/merge-compile-only-policy.md
    docclass_gate_test.go:91: devdoc/DOC-INDEX.tsv:2632: duplicate row for devdoc/merge-compile-only-policy.md
    docclass_gate_test.go:130: devdoc/DOC-INDEX.tsv: 2623 rows cover 2621 files under devdoc/ (classes: map[historical:1893 index-meta:1 normative:717 roadmap:12])
FAIL
exit=1
```

Restored afterwards, and the restore is verified against a **revision** rather than the index
(a restore seeded from the index writes back the very bytes the index holds — Rule 5):

```
$ git checkout -- devdoc/DOC-INDEX.tsv
$ git hash-object devdoc/DOC-INDEX.tsv
a230bb787ad0a3ec5996a510b3179bab26623770
$ git rev-parse HEAD:devdoc/DOC-INDEX.tsv
a230bb787ad0a3ec5996a510b3179bab26623770
$ git diff HEAD --numstat -- devdoc/DOC-INDEX.tsv
                              (empty)
```

The arm needed no new code — it already fails on a duplicated path; what was missing was a
**run** of it on the merge path, not an assertion.

## Related work

`0ca141d7b` on branch `task/01a1473687cb7823aff395fb56c3ad7c` ("converge a duplicated manifest
row instead of vetoing every write") fixes the *writer* side — `duplicate_convergence()` keeps
the LAST row and announces the convergence instead of vetoing. That is the durable fix and is
NOT re-implemented here; this run repairs the data at this cut point and records who produced
it. When that branch lands, the same shape converges to the `historical` row — the same row this
repair keeps, which is the cross-check that the two answers agree.
