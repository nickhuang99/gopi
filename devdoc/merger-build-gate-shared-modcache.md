<!-- doc-class: historical -->
# The merger's BUILD gate resolves its modules from a shared read-only cache

2026-09-15 · branch `fix/mergebuildgate-shared-modcache` (worktree
`~/workspace/gopi-sharedmodcache`), off master `e8afcdfb`.

THIS is the **resolution** half of the 2026-09-15 false-refusal family. The
sibling branch `fix/mergebuildgate-module-download-abstain` (`45f56317`) owns the
**classification** half: it teaches the gate that a build which could not resolve
its modules was never compiled, so it ABSTAINS instead of refusing the branch —
but the branch is still never built, so the gate still measures nothing. With a
cache the build can actually read, the fetch never happens and neither half is
needed on this host. They compose: this branch makes the ABSTAIN path unreachable
in the ordinary case; the sibling keeps it honest when it is not. This branch
does not touch `mergeBuildGateUnresolvedModules` (it lives only on the sibling).

## The defect

`mergeBuildGateScratch` created a **per-process COLD module cache** and
`mergeBuildGateEnv` pointed `GOMODCACHE` at it:

```go
cacheDir    = filepath.Join(classDir, mergeBuildGateCacheDirName)      // gocache
modCacheDir = filepath.Join(classDir, mergeBuildGateModCacheDirName)  // gomodcache — 0 bytes
...
mergeBuildGateGoModCacheEnv+"="+modCache
```

`proxy.golang.org` is unreachable on this host, so every build of a branch with
any external dependency died on

```
go: downloading modernc.org/sqlite v1.44.0
internal/tools/tasks.go:16:2: modernc.org/sqlite@v1.44.0: ... i/o timeout
```

The gate therefore measured the **environment**, not the branch. Whether that
landed as a refusal or an abstention was a coin flip on the toolchain's output
text.

## The fix: resolution by construction, not by output matching

`merge_build_gate_modcache.go` (new) resolves a **populated, read-only,
operator-owned** module cache and points `GOMODCACHE` at it. The build then has
no fetch to attempt: the modules are already on disk exactly where the toolchain
looks. The gate does not inspect the build's output to decide this — a string
match is a symptom reading, and it is the sibling's classification job, not a
resolution.

Resolution order (one reader, Rule 2):

1. `GOPI_MERGE_BUILD_GATE_GOMODCACHE` (named const `mergeBuildGateModCacheEnvName`)
   — the operator escape hatch, and what the regression test seeds.
2. `go env GOMODCACHE` — the cache the operator's **own** builds read and fill,
   so it is warm for exactly the modules this repo needs (every briefing tells an
   agent to run `go build ./...`).

The value is then **validated as a populated module cache**: the directory must
exist, be a directory, and carry the `<cache>/cache/download` subtree. Anything
else → ok=false → the gate **ABSTAINS and logs the exact path** (Rule 15b). There
is no silent fallback to a network fetch: an empty-but-existing directory — the
trap that re-creates the defect while looking configured — is refused too.

### What stayed where it was

- **GOCACHE stays per-process scratch** (`mergeBuildGateGoCache`,
  `scratch.RuntimeScratchDir(scratch.RuntimeClassMergeBuild)`). A merge probe must
  not fill the operator's `~/.cache/go-build`, and a per-call cache would make
  every branch pay a cold toolchain compile. Rule 22 is untouched — the scratch
  stays under `internal/scratch`, never `/tmp`.
- **The gate never writes into the shared module cache.** Measured below: zero
  files written into `/home/user/go/pkg/mod` across the whole probe.
- **Non-Go repos are unchanged.** The shared-cache requirement applies only to a
  repo that IS a Go module (`mergeBuildGateIsGoRepo`); a repo whose declared build
  cannot read modules keeps the gate's own scratch module cache
  (`mergeBuildGateScratchModCache`), i.e. exactly the old never-touch-the-
  operator's-cache behaviour. Without that scoping every non-Go task repo would
  abstain for an irrelevant reason.
- **No vendoring** (rejected in the briefing: 233MB repo / 808MB sqlite), **no
  sudo/root** (Rule 25 — nothing here needs a privilege).

## Verification (raw output)

### Acceptance 3 — build + the whole package

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
build-exit=0

$ go test -count=1 ./internal/tools/
ok  	github.com/nick/gopi/internal/tools	278.022s
test-exit=0

$ gofmt -l internal/tools/
(no output)
```

### Acceptance 1 + 2 — the gate's fixture arms, verbose

```
$ go test -count=1 -run 'TestMergeBuildGate' -v ./internal/tools/
--- PASS: TestMergeBuildGateCleanup_RemoveAllAloneLeavesTheRegistration (0.02s)
--- PASS: TestMergeBuildGateCleanupReapsTheRegistration (0.02s)
--- PASS: TestMergeBuildGate_MasterContainsBranchAbstainsOnAncestry (0.07s)
--- PASS: TestMergeBuildGate_AncestryDirectionKeepsTheRefusal (0.11s)
--- PASS: TestMergeBuildGate_SharedModCacheResolvesWithProxyOff (22.39s)
--- PASS: TestMergeBuildGate_SharedModCacheStillRefusesBrokenBranch (1.04s)
--- PASS: TestMergeBuildGate_SharedModCacheAbsentAbstains (0.02s)
--- PASS: TestMergeBuildGate_SharedModCacheReadOnly (0.41s)
--- PASS: TestMergeBuildGate_RefusesTheIncidentShape (0.07s)
--- PASS: TestMergeBuildGate_TargetAlsoRedAbstains (0.05s)
--- PASS: TestMergeBuildGate_NoBuildContractAbstains (0.02s)
--- PASS: TestMergeBuildGate_DisabledAbstains (0.03s)
--- PASS: TestMergeBuildGate_TimeoutAbstainsNotFails (6.06s)
--- PASS: TestMergeBuildGateSpec_GoModuleGetsTheDefaultAndNoContractAbstains (0.00s)
--- PASS: TestMergeBuildGateErrorText_CapsAndKeepsTheHead (0.00s)
--- PASS: TestMergeBuildGate_GoDefaultRefusesBrokenModule (0.77s)
PASS
ok  	github.com/nick/gopi/internal/tools	31.412s
```

What each new arm proves (file `internal/tools/merge_build_gate_modcache_test.go`):

- `..._SharedModCacheResolvesWithProxyOff` — ACCEPTANCE 1. A real Go-module
  fixture (`github.com/google/uuid@v1.6.0`, this repo's own dependency) is built by
  the GATE'S OWN path (`mergeBuildGateBuild`) with a fixture-local COPY of the
  operator cache, `GOPROXY=off`, `GOSUMDB=off`, and a COLD scratch GOCACHE: the
  branch PASSES and the captured output contains no `go: downloading`. In the same
  test, the NEGATIVE CONTROL runs the identical build against a COLD module cache
  and asserts it does report a download attempt — so the positive arm cannot pass
  vacuously (measured on this host: `go: downloading github.com/google/uuid v1.6.0`
  then `module lookup disabled by GOPROXY=off`).
- `..._SharedModCacheStillRefusesBrokenBranch` — ACCEPTANCE 2's negative arm. The
  branch DIVERGES and does not compile: the gate is REFUSED and the refusal quotes
  the compiler's own `undefined: missingSymbolA`.
  (First version of this arm used `rsMakeBranch`'s empty commit — that leaves the
  branch tree identical to master, so `mergeBuildGateBranchAlreadyInTarget`
  correctly ABSTAINS before any build. The arm was measuring an abstention, not a
  refusal; it now writes a real non-compiling commit.)
- `..._SharedModCacheAbsentAbstains` — Rule 15b. Three shapes (missing path, an
  EXISTING BUT EMPTY directory, and a `cache/download` that is a FILE) all resolve
  to ok=false and the gate abstains with the path in the log.
- `..._SharedModCacheReadOnly` — the seeded cache is chmod'ed 0555 and the build
  still resolves every module, proving the read-only claim.

### Acceptance 1, measured on the REAL repo (not a fixture): the gate's exact build

The gate's own build environment (cold scratch `GOCACHE`, shared `GOMODCACHE`,
`GOPROXY=off`) applied to this repository's master, in a scratch checkout:

```
$ export GOCACHE=$S/gocache-A GOMODCACHE=/home/user/go/pkg/mod GOPROXY=off GOSUMDB=off
$ cd $S/src && CGO_ENABLED=0 go build -o /dev/null ./cmd/agent/
(no output)
arm-A-exit=0
download-lines=0

# the CONTROL: the same build with the gate's OLD cold module cache
$ GOCACHE=$S/gocache-B GOMODCACHE=$S/cold-modcache … go build …
go: downloading modernc.org/sqlite v1.44.0
go: downloading golang.org/x/sys v0.37.0
go: downloading github.com/google/uuid v1.6.0
internal/agencyscan/store.go:14:2: module lookup disabled by GOPROXY=off
internal/tools/tasks.go:32:2: module lookup disabled by GOPROXY=off
internal/daemonlifetime/binding.go:40:2: module lookup disabled by GOPROXY=off
arm-B-exit=1

# the INCIDENT shape: cold module cache + a proxy that answers and fails
$ GOCACHE=$S/gocache-C GOMODCACHE=$S/cold-modcache GOPROXY=http://127.0.0.1:1 …
go: downloading modernc.org/sqlite v1.44.0
internal/agencyscan/store.go:14:2: modernc.org/sqlite@v1.44.0: Get "http://127.0.0.1:1/…": dial tcp 127.0.0.1:1: connect: connection refused
internal/tools/tasks.go:32:2: github.com/google/uuid@v1.6.0: Get "http://127.0.0.1:1/…": dial tcp 127.0.0.1:1: connect: connection refused
internal/daemonlifetime/binding.go:40:2: golang.org/x/sys@v0.37.0: Get "http://127.0.0.1:1/…": dial tcp 127.0.0.1:1: connect: connection refused
```

So: cold cache + `GOPROXY=off` = **3 downloads attempted**; cold cache + a failing
proxy = the incident's `i/o timeout` family; shared read-only cache + cold
`GOCACHE` + `GOPROXY=off` = **exit 0 with zero `go: downloading` lines**.

### The gate does not write into the operator's cache

```
$ find /home/user/go/pkg/mod -newermt '-8 minutes' | wc -l
0
```

Zero files written into `/home/user/go/pkg/mod` across the entire probe — the
shared cache is read-only in practice as well as in intent.

### Rule 15b — a cache the gate cannot see is announced, never silently fetched

Extracted from the abstain arm's run (the gate's own `taskNotifyLog` lines):

```
merger: BUILD gate shared module cache unusable: the shared module cache <tmp>/does-not-exist does not exist or is not a directory (the gate never populates one over the network) (stat …: no such file or directory)
merger: BUILD gate ABSTAINS for feat/clean-build (<repo> is a Go module and no populated shared module cache is resolvable — the reason is logged above) — merging unchanged
merger: BUILD gate shared module cache unusable: the shared module cache <tmp>/003 has no cache/download subtree — it is not a populated Go module cache, and the gate will not fetch into it
merger: BUILD gate shared module cache unusable: the shared module cache <tmp>/004/shape-only has no cache/download subtree — it is not a populated Go module cache, and the gate will not fetch into it
```

## Not Done (honest)

- The gate does NOT populate the shared cache (by design — that is the defect it
  removes). A host with no warm module cache now ABSTAINS where it previously
  refused; with the sibling's classification half merged, such a host would have
  abstained anyway. The resolution requires an operator cache; the env const is
  the escape hatch for one that lives elsewhere.
- The shared-cache requirement is scoped by `mergeBuildGateIsGoRepo`, i.e. by the
  presence of `go.mod`. A repo that is not a module root but whose DECLARED build
  runs a Go build in a subdirectory keeps the scratch module cache (old
  behaviour) — such a repo is not representable in the current contract without a
  new field, and it was not part of the reported defect.
- MEASURED nuance worth recording: a module cache that carries ONLY
  `cache/download` (a download-only cache, no extracted `<module>@<version>`
  trees) IS written into by the toolchain — it extracts the module into the cache
  on first use. The fixture therefore seeds both halves. Any operator cache used
  here must be a real, warm one (which is what `go env GOMODCACHE` guarantees).
