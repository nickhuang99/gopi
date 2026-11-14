<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The stored-cell drift on master, cleared by the corpus's own generator

**Task**: `01a136a3-88dc-729c-8960-417a881d87c5` · **Measured**: 2026-10-16,
hosta · **Tree**: branch `task/01a136a388dc729c8960417a881d87c5` at `10268e802`
(the delivery-point sync merged `master` `00af487b4` into it; every figure below
was re-measured on the merged tree — `artifacts/postmerge-verify.txt`)

## The briefing's premise no longer holds on this tree

The task was filed as: `gopi tool doccheck` exits 1 on a clean checkout because
`devdoc/DOC-INDEX.tsv` line 1212 records `anchor internal/tools/tasks.go:2825 at
}` and that text is no longer on `tasks.go:2825`. Measured here, each half of
that premise is gone:

- **Line 1212 is a different document.** On this tree the manifest's line 1212
  is `devdoc/memory-prefetch-rework.md`.
- **The anchor cell is numberless.** `anchor internal/tools/tasks.go at } else {`
  is what the manifest stores for `devdoc/merge-backlog-terminal-guard.md`;
  across the whole manifest, numbered anchor rows today number **0** (272 anchor
  rows scanned).
- **The freeze table is empty and that is its terminal state.**
  `internal/docclass/testdata/anchor-number-baseline.tsv` carries 0 data rows,
  and `anchor-number-census … --check` exits **0** with `scanned 272 anchor
  row(s), 0 incoherent; table carries 0 row(s)`.

So the drift this task was to freeze was already removed by the corpus rework
(`fabcf3c96`, `fix(docclass): regenerate the anchor-number freeze table — 141
stale permits removed`) that landed on `master` after the briefing was written.
Constraint 5 of the epic decides what to do with that: **a generator whose
`--check` exits 0 must not be given a permit**, and none was minted — the table
is untouched by this run, still 0 rows.

## What DID make `doccheck` exit 1 on master

The run measured the real surface (Rule 31) rather than the briefing's reading of
it. On the pre-fix tree `./bin/gopi tool doccheck` printed `verdict: drift`,
exit **1**, with **no** `NOT PERMITTED` line at all — a different arm:

```
doccheck: stored-cell drift: 6 of 2551 rendered row(s) whose stored text is not
what the classifier renders today
```

Six manifest rows stored a cell that `tools/docclass/classify.py` does not render
today — e.g. `devdoc/requirements/11-build-and-repos.md` stored `anchor
cmd/agent/cli_tasks.go at } else { stale=2` while the classifier renders
`stale=3`. The arm is independently reported by the Python face too
(`classify.py --check`, six `DOCCLASS STALE` lines).

**Root cause (5-why, and it is not this run's defect).** The `write`/`edit`
hook stores a cell computed by `internal/docindex`'s own renderer, while the
arm compares that stored text against a *different* renderer,
`tools/docclass/classify.py`. Where the two definitions of the same cell
disagree, a row is reported drifted the moment it lands and no amount of
re-running a generator would clear it — which is why the same defect is
already filed as bug row `01a141a7-30fb-7473-b944-c6103912e304`
(`bug(docindex)`, filed 2026-10-16 by an earlier run, still `pending`). This
run did not mint a permit for it either: a permit belongs to the anchor-number
family and this arm is not that family.

## The fix, and why it is the sanctioned one

Each of the six rows was re-derived **by its own generator**, one path at a
time — never by hand, never by a whole-corpus regeneration:

```
python3 tools/docclass/classify.py . --only <path>        # dry-run first, then write
```

`git diff --numstat` on the result is exactly `6 6 devdoc/DOC-INDEX.tsv` — six
rows rewritten, no churn, no other cell touched. After it:
`doccheck` prints `verdict: clean`, exit **0**, `stored-cell drift: none —
every rendered row matches the manifest byte for byte`;
`classify.py --check` prints `0 row(s) whose stored text is not what the
classifier renders today`, exit **0**.

## What this run did not do

- **No permit was added to the anchor-number freeze table**, and the table is
  byte-identical to `master` (constraint 5; its `--check` exits 0). The drift
  the epic names does not exist on this tree.
- **No whole-corpus regeneration.** The six drifted rows were re-derived
  individually; a full rewrite of 2551 rows is a separate decision with its own
  review (Rule 38's census discipline).
- **The stored-cell drift is not caused by this run** — its mechanism is filed
  as `01a141a7-30fb-7473-b944-c6103912e304`, and this report does not claim to
  have fixed the hook. What is fixed here is the *master tree's redness*: the
  six stored rows now agree with the classifier that judges them.

## Re-measured after the delivery-point sync (the figures this branch ships)

The delivery-point sync merged `master` `00af487b4` into this branch (tip
`10268e802`); every acceptance figure was then re-taken on the merged tree, and
the verdict is unchanged (`artifacts/postmerge-verify.txt`):

```
$ go run internal/docclass/testdata/anchor-number-census.go . \
      internal/docclass/testdata/anchor-number-baseline.tsv --check
anchor-number-census: scanned 272 anchor row(s), 0 incoherent; table carries 0 row(s)
anchor-number-census: table matches the tree — the freeze is exact
verdict: clean                                            # exit 0
$ python3 tools/docclass/classify.py . --check            # 0 stale   → exit 0
$ ./bin/gopi tool doccheck                                # verdict: clean → exit 0
$ git status --porcelain                                  # 0 lines
```

The freeze table is byte-identical to `master`'s (constraint 5) and that is
machine-checked, not asserted:
`git hash-object internal/docclass/testdata/anchor-number-baseline.tsv` →
`1a93e86f837bf687d370c92a9d5768f3a9fcee41`, and
`git rev-parse master:…` → the same hash.

## A second, independent defect found while taking that evidence

`go test ./internal/docclass/` does not build on `master` (`undefined:
gitDirEnvName`) — filed as a Rule 41 bug row; the evidence and the in-flight
sibling repair are in
`~/.gopi/sessions/01a136a3-88dc-729c-8960-417a881d87c5/artifacts/bug-docclass-test-build-break.md`.
It is untouched by this branch (`git diff master..HEAD --name-only -- internal/docclass/ | wc -l` → `0`).

## Evidence

| check | before | after |
|---|---|---|
| `./bin/gopi tool doccheck` | exit 1, `verdict: drift`, 6 stored-cell drift | exit 0, `verdict: clean`, 0 drift |
| `python3 tools/docclass/classify.py . --check` | 6 `DOCCLASS STALE`, exit 1 | 0 stale, exit 0 |
| `go run internal/docclass/testdata/anchor-number-census.go . <table> --check` | exit 0, 0 incoherent | exit 0, 0 incoherent (unchanged) |
| `git diff --numstat devdoc/DOC-INDEX.tsv` | — | `6 6` |

Raw output of each command is under this run's session directory
(`~/.gopi/sessions/01a136a3-88dc-729c-8960-417a881d87c5/artifacts/`).

## The same family came back one merge later (and was cleared the same way)

`master` advanced to `c5fca47cf` while this run was in flight; the delivery-point
merge (`fa39a0b70`) brought a conflict in `devdoc/DOC-INDEX.tsv` (resolved by
taking master's copy, then generating) and **23 fresh stale stored cells**, none
of them on a path this branch touches. They were cleared in ONE generator write —
`python3 tools/docclass/classify.py . --only-file <23 paths>` →
`{"added": 0, "updated": 23, "rows": 2558}` — landed as `75d8a156d`. Nothing was
hand-edited; `git status --porcelain` on `75d8a156d` is empty and `doccheck`
prints `verdict: clean`, exit 0.

## Honest scope note: a RED suite in `internal/standards` that this branch did not cause

The branch-scoped `make test-budget` over `./internal/docindex ./internal/standards
./cmd/agent` **fails** in `internal/standards` — `TestGate6EnvReaderRedProof`
(1 new unbaselined env constant) and `TestGate9GitEnvRedProof` (4 new git-env
hits + 1 stale baseline entry `cli_tasks_help_surface_basic_test.go:1365`).
That red is **pre-existing on `master`**, measured rather than asserted: this
branch touches no Go file at all, and every path the failing gates name is
byte-identical to its `master` revision (`git hash-object <f>` ==
`git rev-parse master:<f>`; raw evidence in
`artifacts/gate-red-is-pre-existing-on-master.txt`). It is already filed as
pending Rule 41 rows `01a14199-3d6d-7714-8535-c61b76d5471b` and
`01a141d2-8913-7b17-9b57-636f227d655e`, so this run filed no duplicate.
