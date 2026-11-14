---
title: GOPI-VERSION
section: 1
header: GoPi Manual
---

# NAME

gopi-version - print the version compiled into this binary

# SYNOPSIS

<!-- BEGIN SYNOPSIS (copied verbatim from `gopi <verb> -h` by internal/standards/mancensus) -->

```
gopi version
      Print this binary's version — the internal version constant compiled
      INTO the artifact (internal/version.DefaultVersion) — followed by the
      build metadata Go embeds in the binary itself: the VCS revision it was
      built from, the commit time, the toolchain and the target platform.
      Nothing here is read from the directory you are standing in: gopi runs
      in other people's repositories, so its identity travels with the
      binary, and this command needs no git, no filesystem and no network.
      A build the toolchain could not stamp is reported as such rather than
      guessed at: the Go toolchain embedded no VCS build settings for it — a linked-worktree build gets none, and so does -buildvcs=false — and this command reads only the artifact, so no revision can be recovered here.
      On a ground the toolchain never stamps — a linked worktree (the shape
      every gopi task runs in) — the build RECIPE can name the revision it
      built instead:
        CGO_ENABLED=0 go build -ldflags "-X github.com/nickhuang99/gopi/internal/version.Revision=$(git rev-parse HEAD)" -o bin/gopi ./cmd/agent/
      and the report then prints that value with revision-source: on
      the line below it, so a declared revision is never read as an attested
      one. A toolchain stamp always wins: it is attested, this is declared.
      --drift adds the DEPLOYMENT distance, measured on the artifact
      THIS PROCESS IS RUNNING (os.Executable(), not the directory you stand in):
      the revision that binary attests and how far it is behind or ahead of
      refs/heads/master, origin/master and the integration mirror. Read-only:
      a correct source can still leave the artifact the machine runs behind.
      The verdict at the bottom counts only the refs that can carry a commit
      the artifact LACKS: a ref the artifact is AHEAD of (it holds that ref's
      whole history) and any ref in the push-back lane's scratch namespace
      (nothing an operator rebuilds moves the artifact toward it) are measured
      and printed, and are stated NOT to vote.
```

<!-- END SYNOPSIS -->

# DESCRIPTION

**gopi version** prints this binary's version — the internal version constant
compiled INTO the artifact — followed by the build metadata the Go toolchain
embeds in the binary itself: the VCS revision it was built from, the commit
time, the toolchain and the target platform.

Nothing here is read from the directory you are standing in. gopi runs in other
people's repositories, so its identity travels with the binary, and this command
needs no git, no filesystem and no network.

A build the toolchain could not stamp is reported as such rather than guessed
at: a linked-worktree build gets no VCS settings, and so does `-buildvcs=false`.
Since the command reads only the artifact, no revision can be recovered in that
case — and it says so instead of printing a plausible-looking one.

# SEE ALSO

`gopi version -h`, `gopi -h`, `man gopi` (which also lists `version` among the
verbs, because `gopi version` and the `--version` / `-v` flags reach the same
answer).
