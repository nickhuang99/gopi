<!-- doc-class: historical -->
# Task Report: gopi version must be meaningful (post-merge version tags)

> **SUPERSEDED 2026-09-24 by `devdoc/version-identity.md`** (task
> `01a0d09fce9572bb9a2955df78a31e85`). This doc is the record of the design that
> made a merged position carry a NAME (`version/<stamp>-<rev>`, minted by the
> merger) and had `gopi version` borrow it. That design is REVERSED: the mint
> and its callers are deleted, and the version is now a constant compiled into
> the binary (`internal/version.DefaultVersion`). Kept as the record of the
> path walked — do NOT implement from it (§ Completed below describes code that
> no longer exists; the 183 `version/*` tags it created remain as history).

Epic: `gopi version` printed `8380ce9caeef-dirty` — a git internal plus a
defect marker, which tells a user nothing about what the binary is and reads
as a warning about a product they have not even used. The fix: **the merged
position in the repo carries a NAME** (the merger mints an annotated
`version/<stamp>-<rev>` tag at the end of every successful merge round, bugfix
rounds included) and `gopi version` borrows that name plus the exact revision
the binary was built from.

## Completed

- `internal/version` (new leaf package, Rule 14: stdlib + git only, no
  project imports → importable by both `cmd/agent` and `internal/tools`
  without a cycle):
  - `MintTag(repo, branches, now)` — annotated tag
    `version/v<YYYYMMDD-HHMM>-<short-rev>`, **idempotent** (an already-named
    position returns its existing name; a re-run cannot stack names on one
    commit), with a per-command `-c user.name/user.email` identity so it never
    reads or mutates the operator's git config.
  - The tag MESSAGE carries the merged branch names —
    `git show <tag>` answers "what does this version entail" (the promise the
    epic makes; no name short enough to print can hold that).
  - `Describe(dir)` — the display ladder: nearest `version/*` **or** `v*`
    (a human release tag) → `v20260913-0830 (177375b5a6be)`; no tag →
    the honest legacy bare revision; `archive/*` / `safety/*` tags are
    deliberately NOT version names.
  - `RevisionOf(display)` — the inverse of `Display`, so the one consumer
    that parses a display it did not format (the rebuild verifier) has a
    single owner for the format (Rule 13).
- `cmd/agent/cli_version.go` — `buildVersion()` three-source ladder:
  build-stamped `-ldflags` name → the checkout it RUNS IN (only trusted when
  that checkout's revision equals the binary's embedded `vcs.revision`, so a
  released binary run from an unrelated directory never borrows a wrong name)
  → the embedded revision → `dev`. Runtime-tree dirtiness is deliberately not
  projected onto a binary built from clean source.
- `cmd/agent/cli_tasks_rebuild.go` — the merger mints the name at the merge
  round end (`serveRebuildAfterMerge`), placed so EVERY exit (script refresh /
  build / skip) reaches the rebuild with the tag in place.
- `internal/tools/{repo_sweep,task_merge,merge_revert}.go` — the landed branch
  names flow `TaskMerge counts.Outcomes → RepoSweepResult.MergedBranches →
  FailedMergeRevert.MergedBranches → MintTag`; the revert handle
  (`RestoreMaster` step ③) DELETES the minted tag when a failed landing build
  unwinds master, so a name can never point at a position master no longer has.
- `internal/tools/rebuild.go` — the verifier now recovers the REVISION from
  whatever display shape the artifact prints (`gopiversion.RevisionOf`),
  keeping the display as `ActualVersion`. Without this, the new tagged
  display would fail `versionMatches` and every merge round would end in a
  false `version mismatch` + auto-revert of a good merge. The probe also runs
  through `sh -c` with the repo's `bin/` prepended to PATH (an atomically
  replaced / non-executable artifact is an honest "no answer", not a crash).
- Named constants (Rule 1b / 13): `mergeActionMerged` / `mergeActionDeleted`,
  `rebuildBinDirName` / `rebuildBinName` / `pathEnvName`,
  `binaryVersionInterpreter` / `binaryVersionProbeCommand`,
  `ShortRevisionLenText`, `BorrowedName`.

## Not Done

- No change to the **release** path (`-ldflags "-X main.version=..."`): a
  hand-cut `v1.0.0` is still the operator's call, and `Describe` honours it.
- Because Go's vcs stamping does not fire in a linked worktree (below), a
  binary built in THIS worktree embeds no revision and `gopi version` prints
  `dev`. That is the honest answer for a build with no identity — it is not
  fixed here and must not be faked.
- The `archive/*` loser-archive tag namespace is intentionally NOT a version
  name; a branch archived as a loser cannot make a binary introduce itself as
  `archive/...`.
- Known, pre-existing gap in the **tracked-dirt guard**, deliberately NOT
  widened here: the verifier's reject set is still `-dirty` (the embedded
  `vcs.modified` flag, which the toolchain computes from the tree the build
  ran in). A probe that crossed a `@sha`-stamped build (source from a commit,
  cwd dirty) would carry a clean display; the guard reads master's git state
  for the defer decision, so that case is the guard's to widen, not this
  epic's. The gate itself is now display-independent only insofar as
  `-dirty` is modelled in one place (`gopiversion.DirtySuffix` — the reject
  and `RevisionOf`'s dirty detection can no longer drift, Rule 13).

## Review fixes (this task's closing verification pass, 2026-09-13)

Three findings surfaced reviewing the committed work against the epic; two
are fixed here, one is recorded:

1. **FIXED — duplicated doc comment on `binaryVersion`.** The rework left the
   old comment's stale tail (`Returns "" when no binary/probe failure.`) fused
   above the new one, so `go doc` answered a question about a signature that
   no longer exists. Replaced with the single current contract (display,
   revision-carrying the display, dirty) — and the *reason* the `sh -c`
   wrapper is not a direct exec.
2. **FIXED — probe stderr could wedge the whole probe.** `cmd.Output()`
   captures stdout and pipes stderr **only when `cmd.Stderr == nil`**; a
   `--version` child writing more than a pipe buffer (64 KiB) to stderr blocks
   forever and the probe can only end by burning
   `rebuildVersionProbeTimeout`. Now `cmd.Stderr = io.Discard`: the stream is
   *drained*, not read (Rule 15b — an unread descriptor is a silent failure
   that costs the entire timeout).
3. **RECORDED — `MintTag` resolves the stamp from the wall clock, not from
   git.** A round that re-runs later mints a different name for the same
   position. Idempotence holds only for tags *reachable from the round's
   tip*, so an unresolvable `git describe` (shallow clone) can name one commit
   twice. Not a defect for this epic — the same position is the same version
   and the name is stable once minted — so it is accounted here rather than
   changed under a review pass (Rule 5: report what stays undone).

Both fixes are behaviour-preserving except #2 (which turns a hang into the
honest "no version answer"); verified by rebuild + the same test set as above.

## Verification

- Build: `go build ./...` — OK (gofmt clean on every touched file).
- Tests: `go test ./internal/version/` — PASS (incl. the acceptance test
  `TestMintTagNamesTheMergeRound`: feature round + bugfix round each named,
  `git describe` reads the name, `git show` carries the branch).
- Tests: `go test ./internal/tools/ -run 'TestRebuildVerifiesTaggedVersionDisplay'`
  — PASS (both probe paths: fresh build and the already-current shortcut).
- **Red-proof** (the regression guard actually guards): reverting the fix to
  the pre-fix `versionMatches(got, tip)` makes
  `TestRebuildVerifiesTaggedVersionDisplay` FAIL with
  `version mismatch: expected <tip>, binary reports "v… (…)"`; restoring it
  passes. This is the exact false-alarm + auto-revert the seam would have
  caused in production.
- Tests: `go test ./cmd/agent/ -run TestVersion` — PASS. The full
  `cmd/agent` package has a PRE-EXISTING failure
  (`TestIsBrewSubcommand`, needs `ANTHROPIC_AUTH_TOKEN`); it fails
  identically with these changes stashed, so it is not a regression.
- Commit: see the deliverable declaration in the final message.

## The vcs-stamping discovery (worth a future agent's time)

Measured while building the acceptance test, and the reason the test
synthesises the display instead of `go build`-ing `cmd/agent`:

```
go build ./cmd/agent   in  /home/user/work/GoPi  (main checkout, .git = dir)
    → build vcs.revision=571c3d30…        ✅ stamped
go build .             in  a linked worktree   (.git = gitfile → main/.git/worktrees/x)
    → NO vcs.* settings at all            ❌ never stamped
```

Same for `go version -m` on the resulting binary: the worktree build carries
no `vcs` block. `-buildvcs=true` does not change it. So a binary built in a
worktree has `embeddedRevision() == ""` and `gopi version` correctly prints
`dev` (there is no identity to print) — the epic's UX is delivered on the
MAIN checkout and by the declared build in `settings.json`, which builds in
the repo root. This is environment truth, not a code defect; recorded here so
nobody "fixes" `dev` into a fabricated revision.

## Acceptance walk-through (MEASURED on commit 22951fe8, not synthesised)

The table below is the real binary, built in a **main-checkout clone** (so
`go build` stamps `vcs.revision` — see the worktree caveat above) and run
against a real repo:

```
RUNG A — nearest reachable tag, no minted tag at HEAD
$ ./gp version
v0.1.0-perf-final+2509 (22951fe85a51)
revision: 22951fe85a5177bf85df0cc3847b6b16878b288d
tag:      v0.1.0-perf-final
commits since tag: 2509

RUNG B — a minted tag AT the position (what the merger does)
$ git tag -a version/v20260913-0740-22951fe85a51 -m "version: merge round — feat/version-meaningful-tag" HEAD
$ ./gp version
v20260913-0740-22951fe85a51 (22951fe85a51)
revision: 22951fe85a5177bf85df0cc3847b6b16878b288d
tag:      version/v20260913-0740-22951fe85a51
$ git show --no-patch version/v20260913-0740-22951fe85a51
    version: merge round — feat/version-meaningful-tag      ← what this version entails

RUNG C — tag 1 commit behind (post-merge commits landed)
$ ./gp version
v20260913-0730-fff3c9a3c0aa+1 (22951fe85a51)

RUN FROM AN UNRELATED / NON-GIT DIRECTORY (the epic's own case)
$ cd /tmp && /path/to/gp version
22951fe85a51                     ← the binary's OWN revision; never a borrowed name
```

| repo state of the running binary | `gopi version` prints |
| --- | --- |
| merged, minted tag at the position | `v20260913-0740-22951fe85a51 (22951fe85a51)` |
| minted/release tag behind | `v…+N (rev12)` (distance is explicit) |
| no tag (pre-tag history) | nearest older tag `v0.1.0-perf-final+2509 (rev12)` |
| unrelated cwd / no git | `22951fe85a51` (embedded revision — never a wrong borrowed name) |
| no identity at all (worktree build) | `dev` |

`-dirty` appears ONLY when the BINARY was built from a dirty tree (the
embedded `vcs.modified` flag) — it no longer leaks in from a runtime checkout,
which is what made `8380ce9caeef-dirty` read as a defect warning.

### Verification-tooling pitfall measured here (worth a future agent's time)

`git clone <worktree>` copies **only committed state**. The first acceptance
run cloned the worktree *before committing* and therefore tested master's old
`cli_version.go` (2209 bytes, no `embeddedRevision`) — the binary printed a
bare revision and looked like a ladder-2 bug. It was a stale probe, not a
defect: commit first, then clone, or build in place.

## Post-merge close loop (why the tag is safe)

`serveRebuildAfterMerge`: mint → script refresh / build / skip → verify.
The verifier answers "same revision?" (never "same string?"), so the tagged
display cannot self-inflict a `version mismatch` + auto-revert; the red-proof
in Verification pins that. If the landing build fails, `RestoreMaster` step ③
deletes the minted tag along with the reset, so master and its names move
together — a version name can never survive pointing at an unwound commit.
