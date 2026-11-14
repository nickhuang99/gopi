<!-- doc-class: normative -->
# One resolution chain for the run repository — and the withdrawal of the dev_repo deprecation

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Status:** implemented 2026-10-23 (task `01a16541-57c0-7256-8f44-c1a518a968c9`), stage 0.2 of
the worktree retirement.

## The decision this page records

A run needs ONE answer to "which repository do I work in?", and it must be the same answer
on every face. The chain is fixed and ordered, most specific first:

| # | source | when it decides |
|---|---|---|
| 1 | the row's own `deliverable.repo` | the row already says where it works |
| 2 | the current working directory's git repo | the process stands in a repository |
| 3 | settings `dev_repo` (env `GOPI_DEV_REPO`) | the host declares a fallback |
| 4 | none of them | **the ONLY thing that speaks** |

The implementation is `internal/reposource` (one file, zero new dependencies, no new
tool, no new verb). `Resolve(Input)` returns `(Resolution, error)`; the error is a
`*reposource.Refusal` carrying one `Probe` per source, so the text and the data cannot
disagree.

## Why `dev_repo` is optional, and why the deprecation line is gone

The previous revision printed, on every `gopi worker` start of a host whose `dev_repo` was
empty:

```
dev_repo: empty — the CLONE workspace channel is OFF and worker runs still take the
WORKTREE channel; that path is DEPRECATED …
```

That line (with its own mute key, `GOPI_NO_DEV_REPO_NOTICE` / `no_dev_repo_notice`) was
**withdrawn by operator ruling on 2026-10-23**. The reason is the whole point of this
stage: `dev_repo` exists because two parallel workflow channels once forced a host to
*choose* one. With a single channel left, the choice — and therefore the requirement —
disappears, and the key degrades to an **optional fallback**. A host that does not declare
it and starts its runs inside any git repository is on the **normal** path, not a
deprecated one; telling it otherwise once per worker start was a false alarm with a mute
key attached.

So source 2 resolving is silent. Concretely (measured on the shipped binary, this ground):

```
--- CASE 1: cwd = a git repo, dev_repo UNSET (settings {}) ---
exit=0
stdout row:
  run repo               /home/user/workspace/01a16541-57c0-7256-8f44-c1a518a968c9 (from current working directory)
stderr (bytes=0):
```

## The refusal, when all three decline

It fires once, after source 3, and lists each source's **own measured verdict** plus one
remedy per source — so a reader learns whether to `cd`, repair the row, or declare
`dev_repo` without a second experiment:

```
gopi config: no run repository could be resolved — all three sources declined:
  1. row deliverable.repo: the row names no repo
  2. current working directory: /tmp is not inside a git work tree (git rev-parse --show-toplevel: fatal: not a git repository (or any of the parent directories): .git)
  3. settings dev_repo: not configured (optional — a run started inside a git repository does not need it)
fix ONE of them: cd into the repository the run should work in (current working directory), name the row's own repo with `gopi task update <id> --repo <path>` (row deliverable.repo), or declare dev_repo in settings.json / `GOPI_DEV_REPO=host:/path` (settings dev_repo)
```

Two properties of that text are deliberate and are pinned by tests:

- **it never says "DEPRECATED"** — a refusal that calls a source lapsed is the withdrawn
  notice wearing a new label;
- **it names all three** — the shape it replaced reported the last source alone
  ("dev_repo is not configured"), which told a reader standing in a broken cwd exactly one
  third of the truth.

## The operator-visible surface

`gopi config show` gained one row, `run repo`, naming the resolved directory **and the
source that decided it** (`from current working directory`, `from settings dev_repo via …`).
It is read-only, it never changes the exit status, and it carries the refusal on stderr when
nothing resolves. It was added because the resolution otherwise has no surface an operator
can run: the refusal would only ever be seen by starting a task.

`--row-repo` is deliberately **not** a flag on `config show`: that verb reports the HOST,
not a row. The row's own answer stays where it already is (`gopi task show <id>`), and the
precedence is proved at the package level against two real fixture repositories.

## What was removed with the notice

| artifact | state |
|---|---|
| `internal/config/devrepo_notice.go` | deleted |
| `internal/config/devrepo_notice_test.go` | deleted |
| `cmd/agent/devrepo_deprecation_test.go` | deleted |
| `reportDevRepoEmptyDeprecation` + its call site in `main.go` | deleted, with the reason left in place |
| the catalog row for `GOPI_NO_DEV_REPO_NOTICE` / `no_dev_repo_notice` | deleted — a mute key for a line that no longer exists is a knob a future reader has to look up for nothing |
| the catalog row for `GOPI_DEV_REPO` / `dev_repo` | rewritten: it now states the OPTIONAL third source of the chain first, and the clone-channel switch second |

No host has to change anything: an existing `no_dev_repo_notice` key in a settings document
is now simply an unknown key, which the settings reader ignores (the same posture every
removed knob gets).

## Wiring (what stage 2 may still do)

`internal/reposource` is called today from `cmd/agent/cli_config_run_repo.go`
(`gopi config show`). It is deliberately **not** yet called from the worker spawn seam —
`cli_tasks.go`'s `taskRunCloneChannelFor` / `TaskRunGroundFor` path keeps its own
`deliverable.repo`-first resolution, which already agrees with this chain's source 1. The
remaining work is to route that seam through `reposource.Resolve` so the two cannot drift;
it is a separate row by design (this epic is stage 0.2, the shape and the message).

## Verification

- `go build ./...` — OK.
- `go test ./internal/reposource/` — OK (6 arms: order, fallthrough, render callback,
  silent cwd arm, and the three-source refusal).
- `go test ./cmd/agent -run TestRunRepoResolutionFourShapes` — OK (four real runs of the
  shipped binary; the stderr-empty assertions are the acceptance items).
- `go test ./internal/standards/` — the doc-class corpus gate, the Rule 39 hygiene gate and
  the Rule 38 census gate all still read green.
