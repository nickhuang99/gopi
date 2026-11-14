<!-- doc-class: normative -->
# Binary identity on a linked-worktree ground

**Measured 2026-10-13** on hosta (Go 1.24.0, git 2.43.0), task
`01a1339f-cf36-7ebe-bc52-917adf3cf2b9`. Every statement below carries the command
that produced it; the raw logs are
`~/.gopi/sessions/01a1339f-cf36-7ebe-bc52-917adf3cf2b9/artifacts/`.

## The problem, in one sentence

A binary built with `go build -buildvcs` inside a **linked worktree** — the shape
every gopi task ground has (Rule 6a) — carries **no `vcs.*` stamp at all**, so
Rule 31's binary-identity evidence ("this binary is the build of this branch's
HEAD") cannot be produced where a run is allowed to build.

## The mechanism: two lines of cmd/go

Not guessed, read out of the toolchain's own source. The literal that decides a
root is at `vcs.go:252` and the comparison that rejects a gitfile is at
`vcs.go:896`:

```console
$ grep -n "RootNames" $(go env GOROOT)/src/cmd/go/internal/vcs/vcs.go
251:	RootNames: []rootName{

$ sed -n '250,256p' $(go env GOROOT)/src/cmd/go/internal/vcs/vcs.go
	Cmd:  "git",
	RootNames: []rootName{
		{filename: ".git", isDir: true},          # <- :252
	},

$ sed -n '891,901p' $(go env GOROOT)/src/cmd/go/internal/vcs/vcs.go
// isVCSRoot identifies a VCS root by checking whether the directory contains
// any of the listed root names.
func isVCSRoot(dir string, rootNames []rootName) bool {   // <- :893
	for _, root := range rootNames {
		fi, err := os.Stat(filepath.Join(dir, root.filename))
		if err == nil && fi.IsDir() == root.isDir {     // <- :896
			return true
		}
	}
	return false
}
```

A repository root is recognized **only** when `.git` is a **directory**. A linked
worktree's `.git` is a FILE:

```console
$ ls -la .git; cat .git
-rw-rw-r-- 1 nick nick 82 Oct 13 16:01 .git
gitdir: /home/user/work/GoPi/.git/worktrees/gopi-01a1339fcf367ebebc52917adf3cf2b9
```

so `fi.IsDir()` is false, `isVCSRoot` finds nothing, and `vcs.FromDir` returns
`vcsNotFoundError`. The toolchain then **never even invokes git** — the stamp is
dropped, not attempted-and-failed:

```console
$ CGO_ENABLED=0 go build -buildvcs=true -o <bin> ./cmd/agent/     # inside the worktree
$ go version -m <bin> | grep -i vcs
$ echo $?
1
```

`-buildvcs=true` — the spelling that fails **loudly** when cmd/go cannot detect a
VCS — changes nothing, because the loud arm fires only once a VCS was *detected*.
The silence is the defect: `go version -m | grep vcs` printing nothing is
indistinguishable from "nobody checked".

That git is never *invoked* (rather than invoked-and-failed) is also measured, not
inferred from the source: a `-x` trace of the same build on this ground contains
no git command at all.

```console
$ CGO_ENABLED=0 go build -buildvcs=true -x -o <bin> ./cmd/agent/ 2>&1 \
    | grep -cE '(^|[ "])git (status|log|describe|rev-parse|show)'
0
```

## Fix candidate 1 — `.git` as a symlink (measured working, then disqualified)

The mechanism names its own experiment: give the toolchain the DIRECTORY shape it
wants. `ln -s "$(git rev-parse --git-dir)" .git` does exactly that, and the stamp
comes back **and follows the worktree's HEAD**:

| arm | `vcs.*` settings | value |
|---|---|---|
| main checkout (`.git` dir) | 3 | `a88a31aebf06…` |
| linked worktree (`.git` file) | **0** | — |
| worktree, `.git` a symlink to the gitdir | **3** | `a88a31aebf06…`, and after a further commit `9fc37b4a8158…` = the new HEAD |

Git's ordinary verbs keep working through the symlink (`status --porcelain` empty,
`branch --show-current`, `commit`, `worktree list`). **But the recipe must not be
left in place**:

```console
$ git worktree remove --force <wt>
fatal: validation failed, cannot remove working tree: '<wt>/.git' is not a .git file, error code 2
remove exit=128
```

That is gopi's own cleanup path, and gopi's reclaimer compounds it — it reads the
pointer as a FILE (`internal/scratch/worktree_stub.go:418` `os.ReadFile(pointer)`,
then `strings.CutPrefix(text, WorktreeStubGitDirKey)`). A build recipe that
breaks the ground's lifecycle costs more than the defect it cures, so the symlink
survives as **a verified experiment, not a shipped recipe**. (Restoring the gitfile
does clear it: the same rig's restored arm gave `remove exit=0`.)

## Fix candidate 2 — `.git` as a real directory carrying `commondir` (hazard)

Worse than either: git **walks out of the sandbox**. The C1 experiment's
`git rev-parse --git-dir` returned **the caller's own gitdir**
(`/home/user/work/GoPi/.git/worktrees/gopi-01a1339fcf367ebebc52917adf3cf2b9`), and
the experiment's commit landed on **this run's branch**. Recovered with
`git reset --hard <baseline>` the moment it was seen. Recorded so the next reader
does not try it: an "absorbed" `.git` makes git climb to whatever repository
contains the scratch, which is the opposite of isolation (Rule 29).

## The shipped direction — the build recipe names the revision

The toolchain cannot stamp a shape it does not recognize; **the recipe can say
what it built**. So the identity is supplied by the linker:

```bash
CGO_ENABLED=0 go build -ldflags "-X github.com/nick/gopi/internal/version.Revision=$(git rev-parse HEAD)" -o bin/gopi ./cmd/agent/
```

`internal/version` carries `Revision` (empty by default, so a plain build has no
revision claim at all) and `RevisionLdflagPath`, the `-X` operand spelled once
(Rules 1b/13/14) so the usage text and the test cannot drift.

`cmd/agent/cli_version.go` reads it **only when the toolchain wrote no stamp**, and
prints its provenance on a line of its own:

```console
$ ./bin/gopi version
v0.1.0
go: go1.24.0
platform: linux/amd64
revision: b888461f2544e7baaca80048c3e9f265a2b180e8
revision-source: self-declared by the build recipe (-ldflags github.com/nick/gopi/internal/version.Revision) — NOT attested by the toolchain, which writes vcs.revision only in a checkout it recognizes (a linked worktree gets none)

$ git rev-parse HEAD
b888461f2544e7baaca80048c3e9f265a2b180e8

$ go version -m bin/gopi | grep vcs          # nothing was faked
```

### The trap this recipe carries — a wrong `-X` path is dropped in silence

The linker does not check that the `-X` operand names a variable that exists. It
**ignores the flag** and produces a binary with no revision, and nothing on stderr
says so. Measured on this ground while writing this document, twice:

```console
$ CGO_ENABLED=0 go build -ldflags "-X github.com/youngminz/gopi/internal/version.Revision=$(git rev-parse HEAD)" -o /tmp/gopi-wrongx ./cmd/agent/
$ /tmp/gopi-wrongx version | sed -n 4p
revision: unavailable — this build carries no VCS stamp: …          # the recipe looked applied; it was not
```

The module path above is wrong (`go.mod` says `module github.com/nick/gopi`) and the
build **exits 0**. So the recipe is only as good as its spelling, and the failure
mode is the *same sentence* a plain build prints — the one arm where the reader has
no signal that anything went wrong. Two habits remove the risk:

- **Take the `-X` operand from `RevisionLdflagPath`, never retype it** —
  `-ldflags "-X $(go run …)"` is not needed: `go doc` it, or read it out of
  `internal/version/self.go`, where it is a constant precisely so it cannot drift
  (Rules 1b/13/14). The usage text and the recipe both name the same constant.
- **Read the fourth line back.** `./bin/gopi version` printing a 40-hex revision
  *and* `revision-source:` is the proof the operand landed; `revision: unavailable`
  means the flag was dropped, and the next step is to check the path, not the tree.

The two facts are deliberately not merged:

- **attested** — a `vcs.revision` the toolchain wrote after inspecting the
  checkout. Wherever one exists it **wins**, and no provenance line is printed.
- **declared** — the recipe's word. Nothing verifies the tree really was that
  revision, so the report says so, and the line's mere presence is the signal
  (the same shape `modified:` already uses).

`gopiversion.RevisionOfDisplay` still recovers a revision from the report in
either arm, so the post-merge rebuild verifier (Rule 12) reads both, while
`gopiversion.IsDirty` keeps rejecting a dirty tree's answer.

## How a reader checks identity on this ground

`go version -m | grep vcs` is **the wrong instrument on a worktree ground**: it
prints nothing whether the identity is absent or merely unrecorded there, so it
cannot tell "checked" from "not checked". The one command that answers is the
artifact's own report, in **either** arm:

```console
$ ./bin/gopi version | grep '^revision:'
revision: b888461f2544e7baaca80048c3e9f265a2b180e8
```

It never silently omits the line (Rule 15): a build the recipe did not name
prints `revision: unavailable — this build carries no VCS stamp: …` instead, so
an empty answer can only mean the command itself failed. When the value came from
the recipe, the `revision-source:` line directly below it says so.

## What this does NOT do

It does **not** restore the toolchain stamp on a worktree ground, and it cannot:
the rule lives in cmd/go, and patching the toolchain is out of scope by
constraint. A reader who needs the *attested* form must build where cmd/go
recognizes a root (the main checkout, or a clone) — the recipe here covers the
ground a task is actually allowed to build on, and labels itself honestly in the
place where a reader would otherwise be misled.

## The regression guard

`TestVersionRevisionRecipeNamesTheBuildOnAnUnstampableGround`
(`cmd/agent/cli_version_stamp_test.go`) builds **real binaries** in the existing
fixture (a clone + a linked worktree of it) and pins four arms:

1. the recipe makes the worktree build report **its own HEAD**;
2. the toolchain still wrote **0** `vcs.*` settings in that arm — the recipe
   replaces a stamp, it never manufactures one;
3. a toolchain-stamped build **wins** over a decoy declaration (the report names
   the fixture's HEAD, not the decoy) and prints no provenance line;
4. an undeclared worktree build still reports an **honest absence**.

Arms 3 and 4 are what make it a proof: a fallback that always answered would pass
(1) and fail (2)/(3), and one that read the environment would pass (1) on a dirty
host and fail (4).
