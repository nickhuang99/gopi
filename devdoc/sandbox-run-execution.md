<!-- doc-class: historical -->
# `gopi sandbox run` — the sandbox as an EXECUTION ENVIRONMENT

Task `01a0d99a-a55f-76d2-9fa5-33f3afab194b` · branch `task/01a0d99aa55f76d29fa533f3afab194b`

## The defect (why a PATH is not enough)

`gopi sandbox create` returns a **location**. Nothing bound subsequent execution to
it, so a child launched by absolute path inherited the **caller's** working
directory — and inside a task run the caller stands in the run's own git worktree.
Kill the launcher and the orphan keeps `cwd=<worktree>`; `finishCleanupWorktreeInUse`
then refuses that worktree **forever**, and it is *right* to: a live process is
standing in the tree (the all-or-nothing holder verdict, memory #5224). The guard is
not the bug — the producer is.

Measured RED, this run (unmodified shape, a linked worktree at HEAD):

```
RED  launcher pid=2268614  orphan pid=2268616
RED  /proc/2268616/cwd = …/wt/ab-red-2268603          ← inherited the WORKTREE
RED  orphan alive after the launcher was killed: yes
RED  orphan ppid: 1 (reparented to init = the orphan shape)
RED  guard  -> reason="worktree in use by live process pid 2268616 (sleep): cwd …/wt/ab-red-2268603" refused=true
```

## Shape chosen (A) and why

Shape A from the epic: **one allocation+execution entry point**

```
gopi sandbox run [create flags] -- CMD ARGS...
```

Alternatives weighed: (B) a shell wrapper (`scripts/sandbox-run.sh`) — rejected
outright by the briefing (the capability belongs in gopi's own Go code, and a script
is a second, unversioned copy of the resolver); (C) teaching `finish-cleanup` about
"Inherited cwd" — forbidden (it would weaken the guard that exists for a reason, and
the orphan is real); (D) an env var the launcher honours — forbidden and useless (an
env var cannot move a cwd).

Shape A is the only one that fixes the producer where the mistake is made: the moment
an agent asks for a sandbox it now gets **one call** that allocates *and* executes,
with the child's directory set to the thing it asked for.

## Caller inventory (who invokes what, and the residue)

| caller | shape | cwd of the child | risk it carries | this change |
|---|---|---|---|---|
| agent shell / briefing recipe (`SB=$(gopi sandbox create …)` then `<cmd>` in `$SB`) | absolute path, caller's cwd | **the caller's dir** — a worktree during a task run | an orphan pins the worktree forever | recipe now leads with `gopi sandbox run … -- CMD` (GOPI.md Rule 34); the create+export form is kept and its missing half named |
| `gopi sandbox create` alone | allocation only | n/a (no child) | none by itself | unchanged (a doc-commented choice: allocation is a legitimate standalone need — show/inspect/script) |
| `devdoc/evidence/ui-state/ui_state_e2e.sh`, `ambiguity_probe.sh` | caller-supplied `GOPI_UI_SANDBOX`, absolute path, `GOPI_HOME=$SB` | the script's dir | a long-lived `gopi ui` started this way stands in the caller's dir | out of scope here: these are devdoc evidence drivers run by hand, not task-run producers; they are the natural follow-up (`gopi sandbox run -- … ui`) — see `## Not Done` |
| `internal/sandbox` tests, `gopi` daemons | Go `cmd.Dir` already | their own tree | none | unchanged; the new seam is the same `cmd.Dir` idea, named and reusable |
| `gopi scratch-gc sandboxes` reclaimer | reads `/proc/<pid>/cwd` | n/a | — | unchanged: a live process inside a sandbox still vetoes its removal (`every unknown is a KEEP`) |

## What landed

`internal/sandbox/run.go` — the exec seam, **no new allocation path** (it takes the
path the ONE initializer produced):

* `RunIn(RunSpec{Dir, Argv, Env})` — child runs with `cmd.Dir = <sandbox>`, stdio
  wired straight through, **gopi itself never chdir's** (a launcher that chdir'd
  moves every other thing it does for the rest of its life — the shape
  `disownProcessCwd` existed to undo).
* Exit propagation: a child that ran and failed returns `*ExitError` carrying its own
  status (`128+N` for a signal death, the shell convention); a child that could not
  be **started** is a plain error — there is no status to propagate, and inventing
  one is a lie (Rule 15a). Refusals (empty argv, no/absent/non-directory dir) are
  plain errors and start nothing.
* `ProfileEnv(env, dir)` — the execution half of Rule 34's "private profile": sets
  `GOPI_HOME`/`GOPI_SETTINGS` into the sandbox (the two names already in the tree:
  `config.GopiDirEnv`, `config.SettingsEnv`) and lets them WIN over an inherited
  value; no new env var invented, nothing else touched.

`cmd/agent/cli_sandbox.go` — `gopi sandbox run`, a **thin face**: `sandboxParseFlags`
(shared by create and run, so the two cannot drift), `sandboxMaterialise` (the ONE
path: `config.ResolveSandboxRoot` → `sandbox.Create`), `reportMaterialised` (stderr,
keeping stdout to the path alone). `--` is required and everything after it is the
child's argv verbatim. Exit status: child's own; `2` usage; `1` materialisation
failure.

`GOPI.md` Rule 34 — extended with the EXECUTION contract: the recipe line, the
mechanism (a child launched by absolute path inherits the caller's cwd; an orphan
pins the worktree and the guard correctly refuses it forever), and the explicit note
that `GOPI_HOME=…` binds the **profile**, never the **CWD** — and it is the CWD that
pins the worktree. `CONFIG.md` is untouched: no knob was added or changed (the root
resolver is the same one, referenced not copied).

## Verification

See the task report (`## Verification`) for the verbatim commands and outputs. The
short version:

* **RED**, measured on the unmodified shape (quoted above).
* **GREEN**, the same orphan shape through `gopi sandbox run`:

```
GREEN child /proc/2268655/cwd = /home/user/workspace/gopi-sandbox/sandbox-abgreen-2268603-0e1318c6
GREEN child reported FROM INSIDE: cwd=/home/user/workspace/gopi-sandbox/sandbox-abgreen-…  (readlink -f /proc/self/cwd, run by the child)
GREEN the launcher (gopi) cwd never moved: …/wt/ab-green-2268603      ← the parent did NOT chdir
GREEN child alive after the launcher was killed: yes ; ppid: 1        ← the same orphan shape as RED
GREEN guard -> who="" reason="" refused=false                         ← the worktree is NOT refused
```

The A/B is on two **linked worktrees at the same commit**, the guard is the REAL
`finishCleanupWorktreeInUse` (driven through a temporary instrument, deleted before
commit), and the RED/GREEN difference is only the invocation shape.

* `go build ./...` clean; `gofmt -l` clean; `go vet` clean.
* `./internal/sandbox/` suite green, including the new `run_test.go` (child's
  `/proc/self/cwd` == sandbox; the launcher's cwd unmoved; exit-status and
  signal-status propagation; the four malformed-argv refusals; `ProfileEnv` purity;
  an outside reader on a live pid sees the sandbox as cwd).

## Not Done

* `devdoc/evidence/ui-state/*.sh` (and any other hand-run evidence driver) still
  starts its child by absolute path with `GOPI_HOME=$SB`; they are not task-run
  producers, and they are not this branch's to change — the natural follow-up is
  `gopi sandbox run --label … -- <cmd>`.
* The Go-level seam is not wired into any *other* producer (merger/worker daemon
  spawns already set `cmd.Dir` on their own, via `internal/daemoncwd`); this branch
  adds the entry point for the **agent's** shape and does not refactor theirs.
* `gopi sandbox run` does not set rlimits/systemd scope (memory #2267's hardening
  ranking) — it binds a cwd, nothing more.
* No `sandbox run` verb in the shell-completion tables beyond what already covers the
  `sandbox` verb (`cmd/agent` has no per-sub-verb completion entry for create/show
  either).
