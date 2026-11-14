<!-- doc-class: normative -->
# Which binary ran this run — the session records its own process identity

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

**Measured 2026-10-14 → 2026-10-16** on hosta (Go 1.24.0), task
`01a1362e-290b-78e0-a707-a9c0aa477647`. Every claim below carries the command and
its output; the raw evidence is in
`~/.gopi/sessions/01a1362e-290b-78e0-a707-a9c0aa477647/artifacts/`.

## The defect, in one sentence

A task run's tool surfaces are executed by **the worker process's own binary**,
and that binary may come from **another checkout** than the run's ground — so
"the branch is fixed and the package suite is green" and "the shape the live bash
surface exhibits" are answers about **two different code versions**, and a
report's `Ran:`/`Result:` lines cannot be pinned to a revision after the fact.

## The measurement

The chain that executed this run's `bash` calls, read from `/proc`:

```console
$ readlink /proc/52579/exe                 # Gopi worker (the run's wrapper)
/home/user/work/GoPi/bin/gopi
$ /home/user/work/GoPi/bin/gopi version
v0.1.0
revision: adafe54b259ea7e10efd6dbaf2b278ad706a510b
$ cat /proc/52579/cgroup | head -1        # not the subject here, but see Rule 24
```

and the ground the same run was editing:

```console
$ git -C /home/user/workspace/gopi-01a1362e290b78e0a707a9c0aa477647 rev-parse HEAD
adafe54b259ea7e10efd6dbaf2b278ad706a510b
$ ls bin/gopi
ls: cannot access 'bin/gopi': No such file or directory      # NOTHING the run built is what bash runs
```

Two facts fall out of that pair, and the second is the one that matters:

1. The run's `bash` surfaces are executed **from a different checkout** than the
   ground. A `go build -o bin/gopi` inside the ground changes nothing about the
   process that answers the next `bash` call.
2. The two revisions happened to be **equal on this day** (`adafe54b` in both
   checkouts) — which is exactly why the defect is invisible until it bites: the
   ground's branch is the thing the run is *changing*, and the worker's binary
   was frozen when the worker started.

This is **not an edge case**: the operator's own framing, recorded in the
sibling evidence below, is that *"different binaries is the daily state — a
rebuild makes them differ"* (`01a13274-9e41`). Every time the operator rebuilds
`bin/gopi` while a worker is running, the running worker's surfaces are already
a different build.

## Why the record, and not a startup check

The first instinct — *"refuse to start when the binary does not match the
ground"* — is **explicitly out of scope here, and for a measured reason**: the
two differing is the normal state, so a refusal would refuse the ordinary case.
The sibling row (`01a13274-9e41`) records the same conclusion from the other
side: a tool whose only advice was "stop the worker and start it again" tears
down work in flight, because *"the binary differing is daily, not a reason to
refuse to stop"*.

What CAN be delivered without that collision is **after-the-fact pinning**: make
the run's own record carry the identity of the process that wrote it, so any
`Ran:`/`Result:` line can be checked against a code version later. That is what
this change delivers.

## What was recorded

Two writers, one key, one computation — the shape
`devdoc/binary-identity-on-a-worktree-ground.md` already established for
`gopi version`:

| writer | field |
|---|---|
| `session_start` (the recording's first row) | `artifactIdentity` |
| `metadata.json` (the run's own facts) | `artifactIdentity` |

```console
$ head -1 .../sessions/<id>/session.jsonl | jq -c '.artifactIdentity'
{"exePath":"/tmp/gopi-declared",
 "revision":"adafe54b259ea7e10efd6dbaf2b278ad706a510b",
 "source":"ldflags-self-declared"}
```

The block is a `sessionevent.ArtifactIdentity` (`internal/sessionevent/artifact.go`)
and it has **three arms, with the source REQUIRED and never omitted** — the shape
`TerminalGeometry` already uses one file over, and for the same Rule-15a reason
(the value whose absence carries meaning must not be written as a zero):

| `source` | meaning |
|---|---|
| `toolchain-vcs` | cmd/go recognized the build dir as a repository root and wrote `vcs.revision`. **ATTESTED** — the toolchain checked the tree. |
| `ldflags-self-declared` | the toolchain stamped nothing (a linked-worktree build gets none) and the revision came from the build recipe's `-X`. **DECLARED** — nothing verified the tree. |
| `absent` | neither, and the record carries the **reason** — a stated absence, never a blank. |

```console
$ head -1 .../sessions/c2ba1a17-…/session.jsonl | jq -c '.artifactIdentity'
{"exePath":"/home/user/workspace/gopi-01a1362e…/bin/gopi",
 "source":"absent",
 "reason":"the artifact carries no revision: the Go toolchain embedded no VCS build settings for it (a linked-worktree build gets none, and so does -buildvcs=false) and no build recipe declared one"}
```

**Precedence is the same rule `gopi version` prints**: an attested stamp always
wins over a declaration, and the declaration answers only where the stamp is
silent. Both arms were produced by real runs in the same sandbox — see
`## Rule 31 (real surface)` below.

### Why the field is not just `os.Getwd()` or a log line

- A **log line** is correlated after the fact; the run's session record is what a
  reader already opens to answer "what happened in this run".
- **`os.Executable()` alone** answers *which path*, not *which code*. The path is
  half the fact; the revision is the other half, and the two go together because
  the same path can be rebuilt between two runs.
- It is deliberately **not derived from the ground's git state**: the question is
  about the *artifact*, exactly as `gopi version`'s own doc insists ("never about
  the checkout the process happens to be standing in").

## What this does NOT do

- **It does not prevent a mismatch** and does not refuse a run on one. See
  "Why the record, and not a startup check".
- **It does not make the tool surfaces run the ground's binary.** The worker
  still executes its own artifact; the record only makes that fact visible.
- **It is not a toolchain stamp for a worktree build.** A worktree build records
  `absent` (or `ldflags-self-declared` when the recipe stamped it) and says so —
  the underlying cmd/go limitation is
  `devdoc/binary-identity-on-a-worktree-ground.md`'s subject and is unchanged.
- **No new file, no new daemon, no new knob** (Rule 35): two existing record
  writers gained one field each, through the `sessionevent` leaf package that
  already owns this wire format.

## Compatibility

The field is **additive and optional**. A session written by an older binary
simply has no such key, and every existing reader tolerates it structurally: it
is a plain JSON object with no `"message"` key, so the formatters that switch on
event type without a default branch render nothing for it (the same contract
`EventTypeSignalVerdict` states for its own event). `DecodeArtifactIdentity`
distinguishes **silence** (`ok=false`, no error — the caller decides) from
**nonsense** (`source` present but not a value this package defines → error), so
a reader can never mistake "this recording predates the field" for "this
recording says something I do not understand".

## The regression guard

`go test ./internal/sessionevent/ ./internal/agent/ -run 'ArtifactIdentity'`

- `internal/agent/session_artifact_identity_test.go` —
  `TestSessionLoggerRecordsArtifactIdentity` drives the REAL writers
  (`NewSessionLoggerForRun`) in a pinned `GOPI_HOME` and asserts, through the
  decoder a future analysis pass will use: the source is never `""`, the two
  carrying arms demand a revision, the absent arm demands a reason, the path is
  **this process's own** `os.Executable()`, and `metadata.json` and
  `session_start` agree byte-for-byte on the block.
- `internal/sessionevent/artifact_test.go` — the three arms as a **pure**
  resolution (`artifactIdentityFrom`), so precedence is executable rather than
  merely documented: attested beats declared, a whitespace-only declaration is
  the absent arm, and the silent/nonsense split is pinned.

The tests were shown **RED first**: with the producer absent, both writers
answered `ok=false — the record does not carry artifactIdentity`.

## Rule 31 — the real surface, all three arms

The block was produced by REAL runs (never by a unit test alone): three builds in
three arm-states, each run in a `gopi tool sandbox create` profile with
`GOPI_HOME` bound to the sandbox root (Rule 34 — never a hand-made directory):

| arm | how the binary was built | `session_start` / `metadata.json` |
|---|---|---|
| `absent` | `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` **inside a linked worktree** (no stamp, no `-X`) | `{"exePath":"…/gopi-01a1362e…/bin/gopi","source":"absent","reason":"the artifact carries no revision: …"}` |
| `ldflags-self-declared` | the same tree + `-ldflags "-X github.com/nick/gopi/internal/version.Revision=$(git rev-parse HEAD)"` | `{"exePath":"/tmp/gopi-declared","revision":"adafe54b…","source":"ldflags-self-declared"}` |
| `toolchain-vcs` | the same source **cloned** into the sandbox (real `.git` directory) so cmd/go stamps it | `{"exePath":"…/probe/gopi-attested/bin/gopi","revision":"adafe54b…","source":"toolchain-vcs"}` |

Three-way reconciliation for the declared arm — the recorded path against the
kernel's own answer, and the recorded revision against the repository:

```console
$ readlink /proc/881209/exe                       # pid taken from the session row
/tmp/gopi-declared
$ head -1 …/session.jsonl | jq -r .artifactIdentity.exePath
/tmp/gopi-declared
$ git rev-parse HEAD
adafe54b259ea7e10efd6dbaf2b278ad706a510b
$ … | jq -r .artifactIdentity.revision            # and metadata.json carries the same block
adafe54b259ea7e10efd6dbaf2b278ad706a510b
```

The `absent` arm is the one a **linked-worktree build on this host actually
produces** — measured, not assumed: `go version -m bin/gopi | grep vcs.` is EMPTY
for that build, while the same source cloned into the sandbox carries
`build vcs.revision=adafe54b…`. The record therefore states the absence with its
reason instead of leaving a blank a reader would have to interpret.

## The build-metadata keys have ONE definition site — `internal/buildidentity`

The first draft of `internal/sessionevent/artifact.go` typed the toolchain's
revision key as a **local** constant, with a comment asserting it "cannot be
derived from a constant in this tree". That was wrong, and it was a Rule 13/14
defect of the exact kind this repo already has a precedent for
(`TestUnknownNode_MirrorLiteralGate`): `internal/version` had **already
declared** the key, and `internal/tools/image_selfcheck.go` already referenced
it — the new file would have been a **second definition site**, and
`internal/tools/memory_sync.go:146` was a third spelling as a bare literal, left
over from before the constant existed.

Measured, before the fix:

```console
$ git grep -n '"vcs\.revision"' -- '*.go' | grep -v _test.go
internal/sessionevent/artifact.go:151:	const buildInfoKeyRevision = "vcs.revision"
internal/tools/memory_sync.go:146:	case "vcs.revision":
internal/version/self.go:121:	BuildInfoKeyRevision = "vcs.revision"     ← three sites, two of them new
```

**Why the definition could not simply be imported from `internal/version`**: it
is unreachable from `internal/sessionevent`. The TEST build closes the cycle —
`internal/version`'s `testmain_scratchroot_test.go` imports `internal/testutil`,
which imports `internal/sessionevent`, so `internal/sessionevent` →
`internal/version` is refused:

```console
$ # artifact.go temporarily importing github.com/nick/gopi/internal/version
$ go vet ./internal/sessionevent/ ./internal/version/
package github.com/nick/gopi/internal/version
	imports github.com/nick/gopi/internal/testutil from testmain_scratchroot_test.go
	imports github.com/nick/gopi/internal/sessionevent from session.go
	imports github.com/nick/gopi/internal/version from artifact.go: import cycle not allowed in test
```

A production-only build succeeds (`go build ./internal/sessionevent/` → exit 0),
which is precisely why this is easy to get wrong: **`go build` says nothing and
`go vet`/`go test` refuse it.** The answer is the one Rule 14 already prescribes
and `RevisionLdflagPath` already demonstrates in this very leaf package — a
small exported-constant package every consumer can import.

**The fix**: `BuildInfoKeyRevision` / `BuildInfoKeyModified` /
`BuildInfoKeyTime` / `BuildInfoValueModifiedTrue` are now defined in
`internal/buildidentity` (leaf, no imports) and re-exported by `internal/version`,
so every existing reader keeps its spelling while exactly one definition exists.
`internal/sessionevent/artifact.go` and `internal/tools/memory_sync.go` reference
the constants instead of typing the keys.

**And a gate, because nothing caught it.** No test scanned this literal — the
pre-existing exhaustive gates cover session event names and `"unknown-node"`, not
the build-info keys. `internal/buildidentity/literal_gate_test.go` adds the AST
scan, repo-wide, with the definition file as the one allowed holder and a floor
on the files scanned (a scan that read nothing must not pass as green). Measured
red→green: with a consumer re-typing `"vcs.revision"`, the gate prints
`internal/buildidentityprobe/redprobe.go carries "vcs.revision"` and FAILs; with
the probe removed, PASS.

**A limit the gate states rather than hides**: `BuildInfoValueModifiedTrue` is
*not* exhaustively gated, because its value is `"true"` and the first run of the
gate reported it in `internal/scratch/tmproot.go`, `internal/text2img/render.go`,
`internal/tools/tasks.go` and a hundred more — an unrelated boolean string is not
this key. That value is pinned directly instead (`TestBuildInfoWireValues`
asserts all four against what the toolchain actually writes), since a wrong
value would compile, satisfy the literal gate, and silently stop matching every
real artifact.

## A pre-existing red, disclosed (not caused by this change)

`make test-budget PKGS='./internal/sessionevent ./internal/agent'` ends FAIL on
`TestDocAnchorsResolveInNormativeDocs` — a **baseline drift** in
`internal/agent/testdata/docanchor-baseline.tsv`: hundreds of *other* devdocs'
anchors (`loop.go`, `memory.go`, `tasks.go`, `cli_tasks.go`, …) point at
CARRIERLESS lines today. Measured as NOT this change's doing: with all four files
this change adds moved out of the tree (pristine `session.go` from `HEAD`), the
same test still FAILs. Its own message names the open row
(`task 01a13645-66c3-79ec-962a-5f8776e1f216`, "filed on the HP ledger"). Nothing
in this change participates: `grep -c session-records-its-own-binary` in that
test's output = 0, and the doc corpus's own `gopi tool doccheck` reports
0 gap(s) between `devdoc/` and the manifest.

