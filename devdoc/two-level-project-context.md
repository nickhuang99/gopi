<!-- doc-class: normative -->
# Two-level project context: system rules (`<GopiDir>/GOPI.md`) + repository rules

**Contract**: `internal/agent/projectctx.go` — `LoadProjectContext`, `mergeProjectContext`,
`systemProjectContextPath`.

## The standing claim

The session's project context is assembled from **two independent levels**, and both
are optional:

1. **SYSTEM level** — `<config.GopiDir()>/GOPI.md`, the file that rides the gopi
   *profile* (this machine, this operator) rather than any checkout.
2. **REPOSITORY level** — `GOPI.md` found by the existing upward walk from the run's
   working directory (`ResolveProjectContextFile`; its criteria are unchanged).

They are merged **system first, repository second**. Neither level existing is a
valid state: the run continues, the exit status is untouched, and stderr names
which level is missing.

## Why the system level exists

`GOPI.md` is gitignored on purpose (`.gitignore:119` — it is the client's own rule
file and must not travel by clone). The consequence measured 2026-10-19: a bare
clone, a worker's worktree and a sandbox profile all start with **no project rules
at all**, silently, and the loader's one sentence `[gopi] no GOPI.md found` read
like a normal state rather than a rule-less run.

The repository level cannot answer "what this machine's operator always wants",
because that file never travels with the code. The profile is where this product
already keeps its per-machine facts — `internal/envctx` resolves `environment.md`
as `config.GopiDir()` + name the same way — so the system-level rules live beside
it. The path is resolved through `config.GopiDir()` and `filepath.Join` only: no
hardcoded `~/.gopi`, no shell-built path, no new config file, no new environment
switch.

## The merge

`mergeProjectContext(system, repository string) string` is a pure function of two
strings:

- a level with no bytes contributes **nothing** — no heading, no separator, and no
  stray blank line that would leak its absence into the output;
- a present level contributes `heading` + a blank line + its bytes;
- the blocks are joined in the fixed order system → repository.

The two headings are values (`systemProjectContextHeading`,
`repositoryProjectContextHeading`) because an injected block must say **where it
came from**: a reader of the assembled prompt sees two documents, not one blended
one.

## Determinism (the prompt-cache invariant)

Identical inputs produce **identical bytes**: no timestamp, no hostname, no map
iteration and no environment read enters the render, so two calls
(`bytes.Equal`) agree, and the cached prompt prefix stays stable across sessions
and workers. `TestTwoLevelContextIsByteDeterministic` pins it, including the
empty/one-level edge shapes.

## stderr: what a run says about its two levels

One line per level, per outcome, on **stderr** (never a caller's error path):

| outcome | line |
|---|---|
| system loaded | `[gopi] loaded GOPI.md (N bytes) [system] — from <profile path>` |
| repository loaded | `[gopi] loaded GOPI.md (N bytes) [repository] — from <path>` |
| system absent | `[gopi] no system GOPI.md found — the gopi profile carries no rules file` |
| repository absent | `[gopi] no repository GOPI.md found — searched: <bounded list>` |
| profile unresolvable | `[gopi] system GOPI.md unavailable: … (<reason>)` before the absence line |
| present but unreadable | that level's absence line **plus** `[gopi] warning: <path> could not be read (<error>) — continuing without <that level>` |

The two missing cases are **distinct lines with distinct remedies** (place a file
in the checkout vs. write the profile's own). Reporting them as one sentence is
what made a rule-less run look normal — the defect this contract closes.

The loaded line keeps the historical marker and its two fields
(`[gopi] loaded GOPI.md (N bytes)`) and appends the level tag and the source path;
`N` is the file's own length, so the operator's byte count is still the file's
count.

## Fidelity to "optional stays optional"

A level that is absent, unreadable, or whose profile directory cannot be resolved
contributes nothing and reports why. `LoadProjectContext` returns `""` when both
levels are absent — it has no error return, so no caller can turn a missing rules
file into a failed run.

## Evidence

`internal/agent/projectctx_twolevel_test.go` covers both / only-system /
only-repo / neither / determinism / unreadable / resolution-seam, each with real
directories and real bytes, redirecting the profile through `GOPI_HOME` (the
documented seam) so a host that *has* a profile file cannot mask an arm.
