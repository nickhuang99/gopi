<!-- doc-class: normative -->
# Task Report: the gopi binary's version is its own — internal, constant, checkout-independent

Worktree `/home/user/workspace/gopi-01a0d09fce9572bb9a2955df78a31e85`,
branch `task/01a0d09fce9572bb9a2955df78a31e85`.

## The problem, in one measurement

The SAME stamped binary, run twice, answered two different things about
itself — and the difference was the directory it stood in, not the binary:

```
$ cd /tmp           && /tmp/gopi-vcs2 version
536eb37301d1
revision: 536eb37301d1c0ab87d94c03c77ffe4e37430e84
name: no version name borrowed — the working directory is not a git checkout (or git is unavailable)

$ cd ~/workspace/mtest-new && /tmp/gopi-vcs2 version
536eb37301d1
revision: 536eb37301d1c0ab87d94c03c77ffe4e37430e84
checkout: /home/user/workspace/mtest-new at 3300b6fb0513
name: no version name borrowed — this binary's revision is not in this checkout's history (the name here belongs to other code)
```

`gopi-vcs2` was built from the project at commit `536eb37301d1`. Its identity
was whatever checkout it happened to stand in: it BORROWED a `version/*` tag
from the working directory's history, and with no borrowable name it fell back
to a bare revision — or to the literal string `dev`:

```
$ cd /tmp && /home/user/workspace/redproof-pre/bin/gopi version   # pre-change build
dev
```

`dev` was the top line, in the directory the command is most likely to be run
from for this purpose (not a checkout). That is the defect: gopi merges ANY git
repo, and it is run in other people's repositories, so a borrowed identity is
not an identity.

## Completed

- **`internal/version` now owns a hard-coded version.** New file
  `internal/version/self.go`: `DefaultVersion` (the compile-time
  default), `var Version = DefaultVersion` (a variable so a release build may
  stamp it with `-ldflags -X …internal/version.Version=…`), the documented bump
  policy (MAJOR/MINOR/PATCH, bumped in the commit that earns it), the format
  `^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$` as `versionShape`, `Validate`,
  and the toolchain's build-info keys (`BuildInfoKeyRevision|Time|Modified`,
  `BuildInfoValueModifiedTrue`) as exported constants (Rule 13/14: the version
  command and `internal/tools` read the same names).
- **`gopi version` reports that string as its TOP LINE**, plus build metadata
  read from the ARTIFACT (`runtime/debug.ReadBuildInfo`): toolchain, platform,
  the VCS revision and commit time `go build` stamped, and a `modified: true`
  line when the tree was dirty. `cmd/agent/cli_version.go` rewritten; a
  missing VCS stamp is stated (`revision: unavailable …`), never omitted
  (Rule 15).
- **The merge-path mint is DELETED, not hidden.** `internal/version.MintTag`
  (`tag.go`), the name/BorrowName machinery (`name.go`, `git.go`), the
  `cmd/agent` helpers `mintMergeVersionTag` / `mergeCaptureHandoverTips` /
  `mergeHandoverBranchSha` / `mergeHandoverShaKey` / `mergeMeasureHandoverLandings` /
  `mintedName` / `mergedBranchesOf` / `mintAdvancedPositionAtRunClose`, the call
  sites in `merge_run.go` (3), `cli_tasks_rebuild.go` (2) and
  `cli_tasks_adopt.go` (1), and the revert-handle field `FailedMergeRevert.MintedTag`
  plus its `RestoreMaster` step ③ drop. Landing paths keep the DECLARED rebuild
  (Rule 12) — only the tag is gone. Each deletion site carries a comment naming
  what stood there and why it is gone.
- **A regression test pins checkout-independence**:
  `cmd/agent/version_identity_test.go` (3 tests) — the top line is
  `gopiversion.Version` from a non-repo directory; the WHOLE answer is
  byte-identical between a non-repo directory and a tagless one-commit git repo;
  and gaining a `version/*` tag does not change one byte. Shown RED against the
  pre-change tree (below).
- **Reader predicates kept with their live callers named**:
  `RevisionOfDisplay` (caller `internal/tools/rebuild.go:binaryVersion`, the
  post-merge rebuild verifier), `RevisionOf`, `IsDirty`, `DirtySuffix`,
  `MetadataModifiedKey|True`, `ReportRevisionKey`, `Namespace`,
  `ShortRevisionLenText` (fixture/probe width). `IsDirty` now also witnesses the
  report's own `modified: true` line, so the verifier's dirty rejection works
  against the new producer instead of silently losing its witness.
- **Two docclass manifest rows re-pointed** after their anchors were deleted
  with the code (see "Not Done" for the honest note).

## Not Done (honest)

- **I did not delete the 183 `version/*` tags, and I did not implement the git
  hook.** The epic asked for a recommendation, not the act. The recommendation
  is in § "The 183 tags" below.
- **Two `DOC-INDEX.tsv` evidence anchors were re-pointed by me, not by a
  docclass run.** `anchor internal/version/git.go:25` (the file I deleted)
  became `anchor cmd/agent/child_stop.go:239` — the same `signalChildOnly`
  cgroup work the doc is about; and `anchor cmd/agent/merge_run_test.go:902`
  (a test I deleted) became `:802`, the seam comment that survived inside the
  file. Both new anchors resolve (the gate that checks them is green —
  `TestMergeUnionRowCheckOnTheRealManifest`), but a reviewer should confirm the
  chosen targets are the claims those rows mean to stand on.
- **`assertion_totality_allow_test.go` lost one permit and
  `assertion_totality_audit_test.go` lost one witness key** — the assertions they
  classified were about the deleted mint. Both files document the removal
  instead of keeping a stale key (a stale permit is the failure that gate
  exists to report). The audit test's surviving family is back to one key.
- **No full-tree test run.** The heavy path is off-peak (Rule 27: smoke,
  branch-scoped); I ran the packages the diff touches plus the standards gates.
- I did not run `gopi merge` against the project's own repository (it would
  merge unrelated work into a live ledger); the tag-count acceptance was run on
  two purpose-built scratch repos instead, which is a stronger test of the
  claim (they carry a pre-existing `version/*` tag, so the count can only stay
  or grow). *(That verb no longer exists: it was removed 2026-11-01, so this
  sentence is the record of a limit observed on 2026-09-24, not a choice a
  reader can still make — the surviving spelling is `gopi task merge`.)*

## Verification

```
Build:   CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/     → exit 0
         go build ./...                                      → exit 0
         go vet ./cmd/agent/ ./internal/version/ ./internal/tools/ → exit 0
Tests:   internal/version      ok  1.9s
         cmd/agent  -run TestVersion|TestServeRebuild|TestMerge|TestRunTaskMergeRebuild → ok
         internal/tools  -run TestAssertionTotality → ok 2.7s
         cmd/agent  -run TestMergeUnionRowCheckOnTheRealManifest → ok 2.0s
Ran:     see below — every command verbatim with its output
Commit:  cb40412a feat(version): the binary's identity is its own — internal, constant, checkout-independent
```

### Evidence (self-filed)

- `go build ./...` → exit 0 (no output), after the deletion edits.
- `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → exit 0; `bin/gopi version` → `v0.1.0` + 3 metadata lines, exit 0, run from `/tmp`.
- Pre-change binary at `/tmp` → `dev` (exit 0); RED of `TestVersionTopLineIsTheInternalVersion` → `top line = "dev", want … "v0.1.0"`.
- `git -C ~/workspace/mtest-pre tag -l 'version/*' | wc -l` → `2` after the pre-change merge (mint line printed twice); `git -C ~/workspace/mtest-new tag -l 'version/*' | wc -l` → `1` after this branch's merge (mint line count 0).
- `git -C /home/user/work/GoPi tag -l 'version/*' | wc -l` → `183` (baseline; this branch writes no tag).
- `go test ./internal/version/` → `ok … 1.941s`; `go test ./internal/tools/ -run TestAssertionTotality` → `ok … 2.717s`;
  `go test ./internal/standards/ -run 'Doc'` → `ok … 2.891s`; `go test ./cmd/agent/ -run TestMergeUnionRowCheckOnTheRealManifest` → `ok … 2.045s`.
- `gofmt -l .` after `gofmt -w internal/version/self.go` → the 17 listed offenders, every one of them an untouched file (verified by `git status --porcelain -- <file>` printing nothing for each).
- `git status --porcelain` in the worktree → nothing (Rule 30).

### Ran — the changed surface, executed for real (Rule 31)

**1. `gopi version`, non-git directory and tagless repo — identical, exit 0:**

```
$ cd /tmp && .../bin/gopi version            $ cd ~/workspace/mtest-new && .../bin/gopi version
v0.1.0                                        v0.1.0
go: go1.24.5                                  go: go1.24.5
platform: linux/amd64                         platform: linux/amd64
revision: unavailable — this build carries    revision: unavailable — this build carries
  no VCS stamp (built outside a git checkout)   no VCS stamp (built outside a git checkout)
```

> **CORRECTION (2026-09-26, task 01a0dcf5).** The text in that block is the
> PRE-FIX output, kept verbatim as the record of what this run measured. Its
> parenthetical — "built outside a git checkout" — was WRONG, and it is gone
> from the code: a build inside a **linked worktree** (the shape every gopi task
> run builds in) embeds no `vcs.revision` either, so `gopi version` was naming a
> cause that was false for the very build it printed it for. The cause is
> cmd/go's: a repository root is recognized by a `.git` DIRECTORY
> (`vcs.isVCSRoot`), and a worktree's `.git` is a FILE pointing at the main
> clone's gitdir, so stamping is skipped — silently. Today's line states only
> what the artifact shows, from ONE constant shared with `version -h`
> (`versionStampSkippedCause`); `cmd/agent/cli_version_stamp_test.go` pins both
> build shapes with real binaries, and `devdoc/version-stamp-absence.md` carries
> the measurements.

`gopi --version` and `gopi -v` print the same report; `gopi version -h` prints
its usage. `mtest-new` is a real git repo (one commit `3300b6f`, no `version/*`
tag) — the checkout that could lend no name under the old design.

**2. The same binary with a VCS-stamped build** (`go build -buildvcs=true`
from the main repo at `536eb37301d1`, run from both directories) — the top line
is `v0.1.0` in both, the metadata is identical, and the cwd no longer reaches
the answer. This is also the measurement quoted at the top of this report: the
pre-change binary printed `name: no version name borrowed — the working
directory is not a git checkout` from `/tmp` and
`checkout: /home/user/workspace/mtest-new at 3300b6fb0513` from the repo.

> **Retired mechanism (merge verb removal, 2026-11-01).** The `gopi merge --fast`
> invocations in the transcript below are the RECORD of what this run executed;
> the top-level verb was removed afterwards and cannot be typed today (it answers
> exit **1** with a retirement sentence). The measurement they carry is about
> `internal/version`'s tag-minting rule, which is unchanged; the engine that ran
> them is reached today as `gopi task merge`, documented by `man/gopi-merge.md`.

**3. `git tag -l 'version/*' | wc -l` before/after a real `gopi merge --fast`
(Rule 31's tag-count criterion), on two scratch repos that each START with one
purpose-made `version/*` tag:**

```
$ python3 mtest-setup.py
/home/user/workspace/mtest-new: 1 tag(s) before the run: ['version/v0.0.1-preexisting']
/home/user/workspace/mtest-pre: 1 tag(s) before the run: ['version/v0.0.1-preexisting']

# PRE-change binary (redproof-pre, git archive of the base commit)
$ GOPI_SETTINGS=~/workspace/mtest-settings.json ~/workspace/redproof-pre/bin/gopi merge --fast
[merger] [version] minted version/v20260924-0124-landed-46a33d9 (names the position this round landed; landed: 1 [feat/land-me])
[merger] [version] minted version/v20260924-0124-landed-46a33d9 (names the position this round landed; landed: 1 [feat/land-me])
  /home/user/workspace/mtest-new feat/land-me   landed  fast-forwarded into master, ref pruned
$ git -C ~/workspace/mtest-pre tag -l 'version/*' | wc -l
2                      ← MINTED

# THIS branch's binary
$ GOPI_SETTINGS=~/workspace/mtest-settings.json ./bin/gopi merge --fast
gopi merge: run over — 2 branch decision(s), lock released with the run, no child left behind
$ git -C ~/workspace/mtest-new tag -l 'version/*' | wc -l
1                      ← UNCHANGED, only the pre-existing tag
$ git -C ~/workspace/mtest-new log --oneline -2 master
3300b6f landed          ← the landing itself still happened
788b924 base
```

The landing is real (master advanced, the branch ref was pruned) and the tag
count is unchanged; the pre-change binary minted on the identical repo. The
mint's log line appears **0** times in the new run's output.

**4. The RED proof (Rule 10.5)** — `version_identity_test.go` copied onto the
pre-change tree (`git archive HEAD | tar -x` into
`/home/user/workspace/redproof-pre`, built with `CGO_ENABLED=0 go build`):

```
$ cd ~/workspace/redproof-pre && GOPI_BIN=~/workspace/redproof-pre/bin/gopi \
    go test ./cmd/agent/ -run 'TestVersion…' 
--- FAIL: TestVersionTopLineIsTheInternalVersion
    version_identity_test.go:113: gopi version (cwd …) top line = "dev",
      want the internal version "v0.1.0"
--- FAIL: TestVersionIsIdenticalInAndOutOfACheckout
--- FAIL: TestVersionDoesNotBorrowANameTheCheckoutCarries
FAIL    github.com/nick/gopi/cmd/agent
```

All three RED on the old tree, all three green here. (The old tree has no
`internal/version.Version`, so the test cannot even compile there as written —
the RED was taken by running it against the OLD BINARY via `GOPI_BIN`, which is
the same claim measured one level down.)

**5. Tag count in the project's own repo, unchanged by this work:**

```
$ git -C /home/user/work/GoPi tag -l 'version/*' | wc -l
183
```

Nothing in this branch writes a tag, so the count is untouched; it is quoted
here as the baseline the epic asked to preserve.

### The fate of the deleted machinery (measured)

```
$ git diff --stat HEAD -- internal/version cmd/agent internal/tools
 21 files changed, 407 insertions(+), 4426 deletions(-)
   internal/version/version.go          | 985 ++----------------   (→ 229 lines)
   internal/version/name.go             | 370 ----
   internal/version/name_test.go        | 288 ----
   internal/version/tag.go              | 147 ----
   internal/version/git.go              |  67 --
   internal/version/version_test.go     | 546 ----
   internal/version/describe_parse_test.go | 136 ----
   internal/tools/rebuild_tagged_version_test.go | 394 ----
   cmd/agent/merge_run.go               | 354 +--------
   cmd/agent/merge_run_test.go          | 166 ----
   cmd/agent/cli_version_borrow_test.go | (deleted)
  internal/version/self.go              | 107 (new)
  cmd/agent/version_identity_test.go    | (new)
```

**Everything kept has a named live caller** (and everything without one was
deleted rather than parked behind a flag):

| kept | live caller |
|---|---|
| `Version` / `DefaultVersion` | `cmd/agent/cli_version.go:versionReport` (the top line), `buildVersion` |
| `Validate` | `version_report_probe_test.go`, `version_identity_test.go` |
| `BuildInfoKeyRevision|Time|Modified`, `BuildInfoValueModifiedTrue` | `cmd/agent/cli_version.go:readBuildInfo` |
| `ReportRevisionKey` | `cmd/agent/cli_version.go` (producer), `RevisionOfDisplay` (reader) |
| `MetadataModifiedKey|True` | `cmd/agent/cli_version.go` (producer), `IsDirty` (reader) |
| `RevisionOfDisplay` | `internal/tools/rebuild.go:binaryVersion` — the post-merge rebuild verifier |
| `RevisionOf` | `RevisionOfDisplay` |
| `IsDirty` | `internal/tools/rebuild.go` (dirty binary is not the committed tip) |
| `DirtySuffix` | `IsDirty`, `RevisionOf`, `internal/tools/rebuild_dirty_witness_test.go` |
| `Namespace` | `cmd/agent/version_identity_test.go` (the "does not borrow a name" fixture) |
| `ShortRevisionLenText` | the display fixtures' `rev-parse --short=<n>` |
| `BuildInfo*` names | `cmd/agent/cli_version.go`, `internal/tools/memory_sync.go` |

## The 183 tags — recommendation (documented, not executed)

The 183 `version/*` tags are HISTORY: each names a position that a merge round
actually landed between the mint's introduction and this deletion. They cost
nothing at rest (a tag is a ref, not a copy) and they carry information nobody
else recorded.

**Recommendation: keep them, and change nothing.** Deleting them would erase a
landing history that no other artifact holds, in exchange for a legibility gain
that is already achieved by the mint being gone — no new tag will appear beside
them, so their boundary is visible the moment a reader sees the last one's
date. If a repo owner does want them gone, the safe spelling is
`git tag -d $(git tag -l 'version/*.*')` scoped by date, executed by whoever
owns the repository — NOT by a tool, and not as part of this or any other merge
change. What must not happen is what this epic removed: a merge writing a tag.

## Why no git hook

The epic's alternative — a post-merge git hook that cuts the release tag — was
documented, not implemented, and the reason is the same one that killed the
mint: **a tag is a release decision, and a merge is not a release.** A hook
would relocate the side effect, not remove it: it would still write into a
repository gopi merely merged (worse, into EVERY repository gopi merges, since
a hook is installed per-repo and gopi merges other people's repos), it would
still fire on a merge nobody asked to release, and it would put the write
outside gopi where gopi can neither see nor undo it. The identity problem it
was invented to solve is solved properly now — the version travels inside the
binary — so the hook has no remaining purpose. If a team wants a release tag,
`git tag` in their own CI at their own release moment is the whole answer.

## Files

- new: `internal/version/self.go` — the version constant, its scheme, `Validate`, the build-info keys
- new: `cmd/agent/version_identity_test.go` — the checkout-independence regression
- rewritten: `internal/version/version.go` (229 lines, was 985) — report/display readers with live callers
- rewritten: `cmd/agent/cli_version.go` — the version report, metadata from the artifact, no git
- rewritten: `internal/version/version_report_probe_test.go` — the reader's red proof
- edited: `cmd/agent/merge_run.go`, `cmd/agent/cli_tasks_rebuild.go`, `cmd/agent/cli_tasks_adopt.go` — mint call sites and helpers deleted, rebuild kept
- edited: `internal/tools/merge_revert.go`-adjacent field `FailedMergeRevert.MintedTag` and its drop step removed
- edited: `devdoc/DOC-INDEX.tsv` (2 anchors re-pointed), `internal/tools/assertion_totality_allow_test.go`, `internal/tools/assertion_totality_audit_test.go`
- deleted: `internal/version/{name.go,name_test.go,tag.go,git.go,version_test.go,describe_parse_test.go}`, `internal/tools/rebuild_tagged_version_test.go`, `cmd/agent/cli_version_borrow_test.go`
- edited: `cmd/agent/{merge_run_test.go,cli_tasks_rebuild_test.go}` — the deleted machinery's tests removed

## The bump log (who moved the value, and what earned it)

The value moves MANUALLY, in the commit that earns it — this section is the
record, not a step in a release process (`grep -rn 'DefaultVersion =' internal/version/self.go`
is the single source of truth; the log explains WHY it moved).

- **v0.2.0** — the git-clone / workspace GROUND channel: a cloned repository
  can be a task row's ground (`clone channel: record the workspace as the row's
  ground, not a derived worktree name`) and that row is then driven through the
  `dev_repo` switch plus a pull/build/push loop. New user-visible capability,
  no change to any parsed surface — MINOR.
- **v0.1.0** — the first value after the version became a compiled-in
  internal constant (`gopi version` no longer borrows an identity from the
  checkout it stands in; the merge path mints no `version/*` tag).

Tests that need the current value read `version.Version` from
`internal/version` instead of spelling a literal, so a bump moves the code and
the fixtures together by construction (the `gopi vX.Y.Z` banner fixture in
`internal/lineedit`, the `versionBanner` marker in `cmd/agent`, and
`chat_banner_test.go`'s spec).
