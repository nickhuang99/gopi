<!-- doc-class: normative -->
# An absent VCS stamp: state the absence, never blame a checkout

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task `01a0dcf527a27d279490d46cb107c2bc`, branch
`task/01a0dcf527a27d279490d46cb107c2bc`, 2026-09-26.

## Completed

- `cmd/agent/cli_version.go`: the false cause is gone — `versionNoVCSStamp` is
  now `"unavailable — this build carries no VCS stamp: " + versionStampSkippedCause`
  (the cause the artifact supports: the toolchain embedded no VCS build settings,
  a linked-worktree build gets none and so does `-buildvcs=false`), and
  `versionUsage` quotes the SAME constant.
- `cmd/agent/cli_version_stamp_test.go` (new): the red proof — real binaries in a
  `git clone --shared` main checkout and a `git worktree add` of it, `go version -m`
  ground truth (1 vs 0 `vcs.revision`), both printed reports pinned, the pre-fix
  wording carried as a fixture constant so the test reds against master.
- `internal/version/self.go`: the `BuildInfoKeyRevision` comment no longer claims
  the stamp is missing only outside a repository (a worktree is not recognized).
- `internal/version/version_report_probe_test.go`: the probe now feeds the reader
  the OLD wording too, proving the reader refuses both shapes.
- `devdoc/version-stamp-absence.md` + its `DOC-INDEX.tsv` row (through the write
  path, `internal/docindex`) and one line in `devdoc/version-identity.md`. The
  write path ALSO dropped master's duplicate row for
  `devdoc/docindex-write-path-row-ownership.md` (a second, colliding row carried
  in by merge `673c8883`) — `sort | uniq -d` on master's manifest prints that one
  path, on this branch it prints nothing, and every row is 4-field in both.

## The defect, in one measured table

`gopi version` printed, for any binary whose Go build carries no `vcs.*` stamp:

```
revision: unavailable — this build carries no VCS stamp (built outside a git checkout)
```

The parenthetical is a **cause claim**, and it is false on the build shape a
dispatched run uses — a linked worktree handed to the run by its runner. Both
facts were measured, not assumed (Go 1.24.5, `go version -m` on real builds):

| build directory | `.git` node | `git rev-parse --is-inside-work-tree` | `vcs.*` settings in the artifact | what the binary printed |
|---|---|---|---|---|
| main checkout | directory | `true` | 3 | the real revision |
| **linked worktree** (`git worktree add`) | **file** (`gitdir: …/.git/worktrees/<name>`) | **`true`** | **0** | **"built outside a git checkout"** |

The binary stood inside a git checkout — git itself says so — and the binary
said the opposite. That is the defect: an artifact asserting a fact about a
directory it never looked at, about a build that stood exactly where it denies.

## Root cause: cmd/go recognizes a repository by its `.git` DIRECTORY

`internal/vcs/vcs.go` in the toolchain walks up from the build directory looking
for a VCS root, and a git root is a `.git` **directory**:

```go
func isVCSRoot(dir string, rootNames []rootName) bool {
	fi, err := os.Stat(filepath.Join(dir, root.filename))
	return err == nil && fi.IsDir() == root.isDir
}
```

A linked worktree's `.git` is a **file** (`gitdir: <main>/.git/worktrees/<name>`),
so no root is found, `repoDir` stays empty, and `stamp()` omits the `vcs.*`
settings. Measured consequences of that one cause:

- **not a "repo root" rule**: building a package from a SUBDIRECTORY of a main
  checkout still stamps (3 settings) — so the earlier phrasing in memory records
  ("cmd/go only stamps when the build dir is the repo root") was wrong, and the
  subdirectory case is why it could not have been the cause.
- **not escapable with `-buildvcs=true`**: the spelling that makes cmd/go fail
  loudly on an undetectable VCS still yields **0** settings in a worktree, with
  no error — the skip is silent either way.
- **not distinguishable from an explicit opt-out**: `-buildvcs=false` in a plain
  checkout produces the identical artifact shape (0 settings).

So the artifact alone cannot say WHY there is no stamp, and the message must not
pretend otherwise — least of all by naming a checkout.

## The fix (surgical, cmd/agent/cli_version.go)

- `versionStampSkippedCause` — ONE new named constant carrying the cause the
  artifact can support and the toolchain actually has: the toolchain embedded no
  VCS build settings for it (a linked-worktree build gets none, and so does
  `-buildvcs=false`), and the command reads only the artifact, so no revision can
  be recovered here.
- `versionNoVCSStamp` = `"unavailable — this build carries no VCS stamp: " +
  versionStampSkippedCause` — composed from that constant (Rule 13: one spelling).
- `versionUsage` states the same absence, quoting the same constant, so `gopi
  version -h` and the report cannot drift (Rule 31's agreement requirement).
- No checkout is named anywhere, and nothing is computed at runtime: the message
  is a compile-time string, so `gopi version` still needs no git, no filesystem
  and no network (the design constraint of `internal/version/self.go`).
- The `revision:` KEY, `RevisionOfDisplay`, `RevisionOf` and `IsDirty` are
  untouched and byte-compatible: the unstamped line still carries no revision, so
  `versionMatches` still refuses it by name (the rebuild verifier must never
  accept an artifact with no provenance).

## The red proof (cmd/agent/cli_version_stamp_test.go)

The claim is a property of the ARTIFACT, so the proof builds REAL binaries:

1. `git clone --shared` of this repository — a MAIN checkout, which is what makes
   stamping possible — then `git worktree add` of it: the shape under test. The
   clone is ~1s and only ever READS this repo (Rule 29: no foreign worktree).
2. The commit under test is checked out explicitly (a clone otherwise checks out
   the origin's default branch, which is not the tree under test), and this
   worktree's uncommitted tracked changes are materialized into both checkouts —
   the same contract the suite's own gopi build has.
3. Both binaries are built by the SAME invocation, differing only in the build
   directory, with `GOFLAGS`/`GOVCS` dropped from the environment so no exported
   knob can silently disable stamping.
4. Ground truth one layer below the report: `go version -m` counts
   `vcs.revision` inside each artifact — **1** for the main checkout, **0** for
   the worktree — and the fixture FAILS (not skips) if either half changed, so it
   can never pass vacuously.
5. Assertions: the stamped arm's report yields the checked-out revision through
   `gopiversion.RevisionOfDisplay` (the merge path's own reader); the worktree
   arm's `revision:` line equals `versionNoVCSStamp`; the report does not carry
   the false cause; the unstamped report yields NO revision.
6. Two more cases: the same in-a-worktree build with `-buildvcs=true` (still 0
   settings, still the same message), and the wording/help half on the constant
   (`version -h` prints the shared cause; the false-cause substring appears in
   neither the constant nor the usage).

Scratch is `t.TempDir()` (the suite's pinned root, off the `/` filesystem —
Rule 22/32); nothing is created in the source tree.

## Verification

Build: `go build ./...` → exit 0; `go vet ./cmd/agent/ ./internal/version/` → exit 0.

RED (fixture code final, pre-fix constant restored by hand):
`cmd/agent/cli_version_stamp_test.go:393` — "a binary built INSIDE a git checkout
(…/001/wt, worktree of …/001/main) claims \"built outside a git checkout\"" →
`--- FAIL: TestVersionReportDoesNotBlameACheckoutTheBuildStoodIn`.
And with the pre-fix COMMIT as the fixture source (HEAD `79807932`, fix
uncommitted at that moment): both fixture cases failed the same way.

GREEN: `go test ./cmd/agent/ -run
'TestVersionReport|TestVersionNoVCSStampMessageNamesNoCheckout' -count=1` →
all PASS; `go test ./internal/version/` → ok.

## Ran — the changed surface, executed for real (Rule 31)

A scratch `git clone --shared` of this branch + `git worktree add` of it, the
fixed source in both, binaries built by `CGO_ENABLED=0 go build ./cmd/agent`
in each:

```
ground truth        main checkout vcs.revision settings: 1
                    linked worktree vcs.revision settings: 0
                    git -C main rev-parse --is-inside-work-tree: true
                    git -C wt   rev-parse --is-inside-work-tree: true
                    wt/.git content: "gitdir: …/run31/main/.git/worktrees/wt"

$ gopi-main version                      (binary built in the MAIN checkout)
v0.1.0
go: go1.24.5
platform: linux/amd64
revision: e44723c08ae29893c1d55b35b6b1af5edc253fe7
built: 2026-09-26T09:18:46Z
modified: true

$ gopi-wt version                        (binary built in the LINKED WORKTREE)
v0.1.0
go: go1.24.5
platform: linux/amd64
revision: unavailable — this build carries no VCS stamp: the Go toolchain embedded no VCS build settings for it — a linked-worktree build gets none, and so does -buildvcs=false — and this command reads only the artifact, so no revision can be recovered here

$ gopi-main version | grep -c 'built outside a git checkout'   → 0
$ gopi-wt   version | grep -c 'built outside a git checkout'   → 0

$ gopi-wt version -h
Usage: gopi version
      … A build the toolchain could not stamp is reported as such rather than
      guessed at: the Go toolchain embedded no VCS build settings for it — a
      linked-worktree build gets none, and so does -buildvcs=false — and this
      command reads only the artifact, so no revision can be recovered here.
```

## Ran again on the delivered commit (`38c3a7b5`)

The same A/B, re-executed against the tree that is being handed over (the fix is
in the tip, not only in the earlier commit):

```
$ cd <this branch's worktree> && CGO_ENABLED=0 go build -o /tmp/bin-wt-gopi ./cmd/agent/
$ /tmp/bin-wt-gopi version
v0.1.0
go: go1.24.5
platform: linux/amd64
revision: unavailable — this build carries no VCS stamp: the Go toolchain embedded no VCS build settings for it — a linked-worktree build gets none, and so does -buildvcs=false — and this command reads only the artifact, so no revision can be recovered here

$ git clone --shared . <scratch>/clone && cd <scratch>/clone && CGO_ENABLED=0 go build -o <scratch>/gopi-main ./cmd/agent/
$ <scratch>/gopi-main version
v0.1.0
go: go1.24.5
platform: linux/amd64
revision: 38c3a7b532b58a168d75adfc05d5cad448db4cfd      ← the branch tip, for real
built: 2026-09-26T09:28:50Z

$ go version -m <scratch>/gopi-main | grep -c 'vcs\.revision'   → 1
$ go version -m /tmp/bin-wt-gopi  | grep -c 'vcs\.revision'    → 0
$ /tmp/bin-wt-gopi version -h    → the shared cause, exit 0     (text in the section above)

$ grep -rn 'built outside a git checkout' cmd/ internal/   → only the red proof's
  own false-cause fixture constant and the code comments that say it WAS the old,
  wrong wording; no production string carries it.
```

`stat -c %d /` = 66307 vs the scratch clone's 66309 — the scratch was off the `/`
filesystem (Rule 22); it was removed after the run, and the worktree is clean.

## Evidence (self-filed)

- `git status --porcelain` in the worktree (this branch, after the report commit)
  → printed nothing.
- `grep -rn 'built outside a git checkout' cmd/ internal/` → no production string;
  the remaining hits are the fixture's own false-cause constant, the comments
  recording what the OLD wording said, and this report's history section.
- `grep -rn 'built outside a git checkout' cmd/agent/cli_version.go
  internal/version/self.go internal/tools/rebuild.go` → 1 hit, `cli_version.go:62`,
  a `//` comment saying the replaced string was FALSE — no string literal.
- `/tmp/bin-wt-gopi version | grep -c 'built outside a git checkout'` → `0`
  (that binary was built in this linked worktree).
- `go build -o /tmp/bin-wt-gopi ./cmd/agent/` → exit 0; `/tmp/bin-wt-gopi version -h`
  → exit 0, and the usage text contains the shared cause substring.
- `go version -m <script>/gopi-main | grep -c 'vcs\.revision'` → `1`;
  `go version -m /tmp/bin-wt-gopi | grep -c 'vcs\.revision'` → `0`.
- `stat -c %d /` → 66307; `stat -c %d <scratch clone>` → 66309 (scratch off `/`).
- `go build ./...` → exit 0 (`=== BUILD ./... OK ===` from the job's own output).
- `go test ./cmd/agent/ ./internal/version/` → PASS (the job's tail carries the
  package results; the fixture's own `-run` GREEN is quoted in the Verification
  section above).

## Not Done (honest)

- `versionUsage` is a raw multi-line string, so the cause is inlined there by a
  compile-time quote of the constant; there is no runtime composition to gate.
  The agreement is enforced by a test assertion
  (`TestVersionNoVCSStampMessageNamesNoCheckout`) and by the executed `-h` run
  above, not by construction.
- The message still cannot say which of the two causes it was (worktree vs
  `-buildvcs=false`): `debug.ReadBuildInfo` does not record it, and `gopi version`
  must not consult git or the filesystem. A reader who needs that distinction has
  `go version -m <binary>` and the table above.
- The fixture's source repo must be a real git checkout: a `git archive` extract
  (no `.git`) has nothing to clone and nothing to stamp from, so the fixture
  SKIPS there with that reason — the suite stays green in a toolchain-less or
  archive-only environment, which is the pre-existing convention for this package.
