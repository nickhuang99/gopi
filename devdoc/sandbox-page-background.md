<!-- doc-class: historical -->
# The `gopi tool sandbox` page — background moved off the page

**Class: historical** (no current artifact cites this file, so the generator
classes it a past run's record; the banner above is the machine-checkable
half of the same claim). This is the standing background of the sandbox help page
(`sandboxUsage`, `cmd/agent/cli_sandbox.go`), which `gopi tool sandbox -h` and
`gopi sandbox -h` both print. Slice 3/5 of the "one page = one level" epic
(`01a10bb7`) moved the reasoning off the page and left a pointer. Nothing was
deleted — this file IS the moved prose, verbatim, plus the measurements behind
it.

## The three shapes, and why each is what it is

**The settings document is a COPY, not a link — a measured ruling (2026-09-27).**
A link is followed by a shell REDIRECT, so a fixture command of the form
`printf '{...}' > "$SB/.gopi/settings.json"` truncated the OPERATOR'S live
`/home/user/.gopi/settings.json` through the link and destroyed the api_key, the
`repos` table `gopi merge` keys on, and ten other keys. The copy answers the
same reads (the sandbox runs the operator's real configuration) and a write to
it is refused by the kernel instead of reaching the live file. Declare a sandbox
run's own knobs in its OWN settings file (`GOPI_SETTINGS`) — that is a
`GOPI_SETTINGS` value, not a write into the profile.

**One link remains, for a different reason: the PLUGIN TREE**, because a sandbox
must run the operator's REAL tools and a copy drifts the moment a script is
deployed. It is linked at the level the profile resolves
(`<GOPI_HOME>/.gopi/plugins`), which is where the web-search scripts are looked
up, so `GOPI_WEBSEARCH_SCRIPTS_DIR` and `GOPI_HOME=$SB gopi web search` resolve
through the SAME tree this side links. A host with no plugin tree is still a
valid host: the create line says so (`plugins=<path>(absent-not-linked)`) instead
of leaving the gap to be discovered by a failing search.

**The ledger is the file a run writes first**, so it is a real, private, writable
file at the path the ledger resolver computes for this profile
(`GOPI_HOME=this sandbox` → `.gopi/memory.db`) — never a symlink. A symlink to
the shared read-only sample would either refuse every write ("attempt to write a
readonly database") or be silently REPLACED by a writer's rename, after which
the sandbox reads a different file than it pointed at. When a seed is named
(`GOPI_SANDBOX_LEDGER_SEED`) it is COPIED, by value: the sample keeps its 0444
seal and the sandbox keeps a file it may write.

## Why `.gopi` is there

`GOPI_HOME` is a HOME, not a data root — every reader (`gopi log`, `gopi raw`,
`gopi stats`, the ledger) resolves `<GOPI_HOME>/.gopi/...`, so the sandbox
materialises the profile at that exact path and nothing has to be created by
hand before a run. `GOPI_HOME=$SB gopi log --last` finds the sandbox's own
sessions because of this, and it is the check that caught the two halves
disagreeing.

## Execution is part of the shape — the whole paragraph

A sandbox is a place to RUN something, not just a path to point at:
`gopi sandbox run [create flags] -- CMD` runs CMD with its working directory set
to the sandbox (never to the caller's, and gopi itself never chdir's), with
`GOPI_HOME`/`GOPI_SETTINGS` pointing into the sandbox, and with CMD's exit status
propagated. A child launched by absolute path instead inherits the CALLER's cwd
— inside a task run that is the run's worktree, and an orphan left standing
there makes the worktree permanently unreclaimable. See GOPI.md, the sandbox
rule.

## Never invent a sandbox — the whole paragraph

`SB=$(mktemp -d)`, a hand-built `GOPI_HOME`, an `os.MkdirTemp` inside a test
body: each is a different shape, none is reclaimed, and each is a second,
unreviewable answer to "what does an isolated profile contain?" Such a path has
no standard shape and no reclaimer — deleted by nobody, understood by nobody.
Sandboxes under the configured root are reclaimed AUTOMATICALLY once their owner
is gone and they have aged out, by `gopi scratch-gc sandboxes` and by the
resident worker/merger tick. See GOPI.md, the sandbox rule.

## Why the page is the shape it is

The rule this slice applies: **a help page lists what the reader can type, one
line each, and defers the reasoning to a document that is read on purpose.** The
sandbox page keeps the synopsis, the profile listing, the flag list, the env
list, the NEAR MISS, and the exit statuses; the paragraphs above are the
reasoning behind them. The three-tier vocabulary is a separate matter and stays
on the page: `gopi tool sandbox create|show|run` is what the reader types, and
the help-hierarchy reverse guard asserts the alternation is present.

Measured on this slice (real binary, `./bin/gopi tool sandbox -h | wc -l`, at
branch tip `048ef7a2`): **103 lines before, 99 after.** The before figure is the
same binary built at that commit without the four page edits (`git worktree add
--detach <path> HEAD` + build), so both numbers are real renders of the same
compiler, not estimates.
