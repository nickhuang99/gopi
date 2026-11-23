# Built by the agent itself

The strongest demonstration of gopi's long-horizon, delegation-first design is
the project's own source tree: it was written almost entirely by gopi. Its
creator does not program in Go and now directs the work purely as a project
manager.

All figures below are measured from the public repository, not recalled:

```console
$ git ls-files '*.go' | xargs wc -l | tail -1
504074 total
```

That is roughly half a million lines of Go, written over about four months. It is
the project's own source tree — the agent wrote the code, ran the tests, fixed the
regressions and managed its own tasks. Cumulative output is larger still, since
much of the early work was revised and replaced; the number above is what is in
the tree today.

## A live census, not a guess

Line count alone undersells it. The repository ships an instrument that measures
the whole codebase from `git ls-tree` on every run — files, functions, packages,
dependencies, architecture by area. The figures below are a snapshot of that
census at base commit `c7b2260e` (2026-11-24); running the script again gives the
current numbers, which is the point — this is a live picture, not a typed claim.

```console
$ bash scripts/gopi-stats.sh          # from the repository root
size:     1124 production files / 503853 LOC / 12377 functions in 93 packages
what it is:  one Go binary, zero CGO, 6 direct deps (30 pinned in go.mod)
surface:  31 LLM tool names, 13 CLI verbs, 11 task subcommands, 111 env vars
```

Twelve thousand distinct functions across ninety-three packages, held together
by six direct dependencies. Every number above is re-derived from the tree by
the script; none is retyped from a previous run. This is the most complete
picture of the project's size, and it is a command anyone can run after cloning.

## Why this is a philosophy statement, not a stunt

Most agents are sold on safety by asking a human to approve each file read,
directory access, and tool call. gopi is built on the opposite premise: the
agent runs in a sandbox and is delegated nearly everything by default. Human
approval is reserved for the few actions that are destructive, systemic, or
irreversible.

This project is the proof that the premise can carry real work. A non-programmer
set the direction; the agent wrote the code, ran the tests, fixed the regressions,
and managed its own tasks. The role of the creator shifted from author to project
manager — reviewing direction and outcomes rather than typing code.

## A note on attribution

The git history carries the creator's name on many commits, because the agent
commits under the machine's configured git identity. The code itself was authored
by the agent; the signature reflects whose machine and account produced it, not
who typed each line.
