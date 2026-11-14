<!-- doc-class: historical -->
# The same retirement dispatched twice: reworking `feat/unrun-demo` (2026-10-15)

**Verdict: NO-MERGE, no source change, and the fixture stays archived.** This is the
second time a *merge-backlog* row was filed against an already-retired unrelated-history
branch (the first: `01a13322`, the `zz-r31-orphan` case before it). The reusable content is
the **refutation recipe** and the **fixture-home criterion**, both measured below.

Epic/task: `01a135d236db75a69f19a9583b964cb5` — rework of the row that asked to merge
`feat/unrun-demo` into master. Baseline `0bbb41c`; master `0bbb41c6f`; tip
`45511777006b89b48cff077fb504273bc5a6319c`; root `66d8b80d67534a6380cabad7a9513b27af4531de`.

## 1. The recipe — refuting an unlandable ref in six readings, all cheap

Run in the run's OWN worktree (Rule 29: never borrow a checkout to look at another ref):

```bash
git rev-parse --verify --quiet refs/heads/<b>; echo rc=$?     # 1  ref gone? rc=1
git rev-parse --verify --quiet refs/tags/archive/<b>          # 2  durable retirement?
git cat-file -p refs/gopi/archive-reason/<b>                  # 3  WHY, with actor+timestamp
git for-each-ref --contains <tip> --format='%(refname)'       # 4  who still holds the object
git merge-base --all master <tip>; echo rc=$?                 # 5  empty + rc=1 = no common ancestor
git rev-list --max-parents=0 <tip>                            # 6  the ROOT (see §3)
```

`for-each-ref --contains` is the one worth insisting on: it answers "does deleting a branch
lose the work" with **which refs hold it** (here: exactly the archive tag + the reason ref), and
it is the reading that distinguishes *retired* from *still owned by someone*. Two-dot
`git diff --shortstat master <tip>` then puts a number on the pathology (4 files vs 7841;
`7843 files changed, 7 insertions(+), 1783416 deletions(-)`): an unrelated root's "deletions"
are an artifact of its own empty base, not a judgement about the payload.

**The guard is not the cause.** `bash scripts/safe-merge.sh feat/unrun-demo` refuses (rc=1),
but its `[e]` missing-ref arm short-circuits before the `[f]` unrelated-history probe, so the
printed kind is `archive-tag missing-ref` — it names the **absence**, never the **cause**. Exit
1 either way; only the message is less precise. Do not read arm `[e]` as the ancestry check.

## 2. Landing it would corrupt master — the payload criterion

An unrelated root has no "diff to review": the useful question is **what would master's tree
become**, which is answered per-path by `git show <tip>:<path>`:

| path | tip | master |
|---|---|---|
| `go.mod` | `module demofixture` | `module github.com/nick/gopi` |
| `internal/foo/foo.go` | 39 B stub | **absent** (0 hits) |
| `cmd/agent/main.go` | 29 B (`package main` stub) | the real agent package (thousands of lines) |

3 of 4 paths are stubs; the one that matters is `go.mod`, whose **module path would be
rewritten** (`github.com/nick/gopi` → `demofixture`) and whose `internal/foo` import would
appear from nowhere. A fixture like this is landed only by `--allow-unrelated-histories`, which
every rule here forbids — the durable retirement (a tag + a reason ref) is the correct end state.

**Fixture fingerprint** (three independent signals, none of them a path pattern): author
`t <t@t>` (matches no contributor), subjects `init` / `branch: one package`, and a 2-commit
history minted 2026-10-13 10:39:26 — i.e. minutes inside a merge-run window. Say "fixture" in
the report rather than treating it as debris to salvage.

## 3. The correction: "the tip is a root" is the wrong sentence

The briefing, and the acceptance clause built on it, said *"the tip is a root commit with no
parent"*. Measured: `git rev-list --parents -n1 <tip>` prints **two** hashes — `45511777 66d8b80d`
— so the **tip has one parent**; the **root** `66d8b80d` is the parentless commit and
`--max-parents=0` names it alone.

The distinction is not pedantic: a 2-commit orphan is `git init` + one commit on top, and a
reader who checks only the TIP for parentlessness concludes "this ref is a 1-commit root" and
then looks for a different explanation for the second commit. **Check parentlessness on the
ROOT (`--max-parents=0`), and check the tip's parent count separately.** The property the
original clause was protecting is unaffected — the root sets of master and the branch are
disjoint, and the three-dot diff refuses with `fatal: … no merge base`, rc=128.

## 4. The fixture-home criterion — and why "no home" is the answer here

The epic asked whether the fixture still needs a home for merge-path testing, and to place it
outside the root `go.mod` only if it does. **It does not.** Four measured reasons, in the order
that decided it:

1. **Demand is measurable, and it is zero**: `grep -rn '<tip-hash>\|demofixture'` across
   `*.go *.sh *.md *.tsv` → no hits. Nothing in the repo references this artifact; a fixture
   with no consumer is a payload with no reader.
2. **The tracked producer already exists, is cheaper, and is used**: the merge path's own
   orphan fixture is built **in-test** — `cmd/agent/merge_unrelated_history_test.go:35-56`
   (`git checkout --orphan` + `rm -rq --cached .` + a root commit), with the same recipe in
   `internal/tools/task_merge_executor_safemerge_test.go:83`, `run_ground_anchor_test.go:280`,
   `worktree_sweep_ground_checklist_test.go:51`. It runs green in 5.5 s. Prefer the 20-line
   constructor to a committed blob (Rule 35 zero-dependency, Rule 39 a blob belongs in no tree).
3. **A committed fixture would be a home for an artifact nobody reads** — a new tracked file
   whose lifecycle nobody owns, created to satisfy an "outside the go.mod" phrasing rather than
   an observed failure. The criterion the epic actually wanted is satisfied by (2).
4. **The real residue is a LEDGER residue, and it was already filed**: an unrelated-history
   retirement writes a tag + a reason blob and **no ledger state**, so the row that asked for the
   merge stays open against a ref that no longer exists. That class is row
   `01a1383b-ef5b-7307-af5c-6536bcfee1c8` (Rule 41, completed). The filer-side half is fixed
   (`archiveTagAncestry` / `archiveTipReachable`, `internal/tools/task_merge_backlog.go`); the
   already-filed row is what remains, and re-filing it for a second branch would mint a duplicate.

## 5. Attached-diff provenance, and what a diff cannot show

`git diff <root> <tip> | sha256sum` = `561f027a83d8…` — **the first 8 hex digits are exactly the
attachment's `sha256_16`**. So the attachment was a faithful export of the tip's second-commit
diff — and structurally **incomplete as a picture of the branch**: a diff between two commits
cannot contain the ROOT commit's own content (`a.txt`, 6 B, `1 file changed, 1 insertion(+)`),
because a root has no first parent. Any reader judging such a fixture from its diff alone will
under-count the payload by exactly the root commit. Snapshot the tree (`git ls-tree -r -l`) when
the diff's frame is in question — 537 bytes of diff vs a 4-file tree is that gap.

## Check

```bash
go test ./cmd/agent/ -run 'TestMergeUnrelatedHistory|TestMergeHasCommonAncestor'   # the in-test orphan recipe, green
bash scripts/safe-merge.sh feat/unrun-demo                                        # rc=1, (拒绝种类: archive-tag missing-ref)
git merge --no-commit --no-ff refs/tags/archive/feat/unrun-demo                    # exit 128, MERGE_HEAD left ABSENT
```
