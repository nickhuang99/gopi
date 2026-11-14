<!-- doc-class: historical -->
# When `GOPI_HOME` Names a Code Checkout — the throwaway-profile guard

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Task:** `01a14a53-b5cf-7a9a-aa07-72eabc872571`
**Epic:** a gopi run's child processes must never resolve a THROWAWAY profile.
**Date:** 2026-10-17 (host `hosta`, client role — Rule 28).

---

## 1. The defect, stated exactly

`GOPI_HOME` is documented as a **HOME**, and the profile root is
`<GOPI_HOME>/<ProfileDirName>` — `GopiDir()` in `internal/config/gopihome.go`,
`ProfileDirIn`. So a `GOPI_HOME` whose value is a **git work tree** (a code
checkout) makes every process that reads it resolve `<worktree>/.gopi` as its
profile. Three consequences, all silent:

1. **the profile is throwaway** — nothing sweeps it (`SweepSandboxes` sweeps a
   sandbox ROOT's children; this is not a sandbox), nothing dispatches from it,
   nothing reads it back;
2. **notify deliveries vanish** — `scripts/notify/notify.sh` appends the anchor
   to `<profile>/notify/task-events.jsonl`, so the event lands in a file no chat
   console, no peer cursor and no operator ever reads;
3. **the checkout goes DIRTY** — the profile is written *inside* the worktree,
   and `gopi merge` then parks the queue on "tracked files modified — left alone
   for a human".

None of the three announces itself. That is the defect: not the inconvenience,
the silence (Rule 15).

## 2. The measured propagation chain

### 2.1 What this run's environment actually carried

Measured at run start (`env | grep ^GOPI` in the run's own shell, plus
`/proc/<pid>/environ` of every process above it):

```
$ for p in 2210764 2407439; do tr '\0' '\n' </proc/$p/environ | grep -E '^(GOPI_HOME|HOME)='; done
2210764  gopi worker            → HOME=/home/user            (no GOPI_HOME)
2407439  bin/gopi (executor)    → HOME=/home/user            (no GOPI_HOME)
```

**This run did not leak it.** The wrapper (`gopi worker`, pid 2210764) was
started from the operator's login shell and never exported `GOPI_HOME`; the
executor child (pid 2407439) inherited only `HOME`. So the guard shipped here is
*not* a fix for an observed leak in this session — it closes a channel that is
**open by construction**, and §2.2 shows the channel end to end.

### 2.2 The channel, driven for real

Every writer of `GOPI_HOME` for a CHILD, found by `grep -rn 'GOPI_HOME='`:

| site | what it does |
|---|---|
| `internal/sandbox/run.go:165` | `GOPI_HOME=<sandbox>` for `gopi tool sandbox run` — **legitimate** (a real dir holding `.gopi`) |
| `internal/testutil/run.go:331` | the suite's own profile pin — legitimate |
| `internal/tools/memory_sync_row.go:73` | `GOPI_HOME=<dir>` on an ssh/path REMOTE argv — legitimate (peer's `--remote-home`) |
| `cmd/agent/main.go:717` | `--home PATH` → `os.Setenv` — legitimate |
| `internal/config/env.go:148` | settings.json `gopi_home` → `os.Setenv` — legitimate |
| **the operator's own shell** (`export GOPI_HOME=$PWD`, a script that does it, a checkout that IS a sandbox) | **this is the leak** — and it is the one no Go site can see |

`os.Environ()` is the carrier: `bashChildEnv` (`internal/tools/bash.go:141`) and
`taskChildExecEnv` (`cmd/agent/task_child_env.go:185`) both start from the
parent's whole environment and SUBTRACT. Whatever the launcher exported passes
through untouched.

**Driven (pre-fix binary, `git worktree add --detach HEAD`), `GOPI_HOME=<a fake
worktree carrying `.git`>`:**

```
$ ./bin/gopi "Use the bash tool to run exactly: env | grep '^GOPI_HOME=' || echo NO_GOPI_HOME."
NO_GOPI_HOME          ← pre-fix the child PRINTED the value

$ find <worktree> -maxdepth 2 -name .gopi
<worktree>/.gopi                    ← the run's own profile, INSIDE the checkout
<worktree>/.gopi/memory.db          ← a throwaway ledger
<worktree>/.gopi/sessions/<sid>     ← throwaway sessions
```

The pre-fix control that shows what a child *would* have resolved:

```
$ GOPI_HOME=$WT bash -c 'echo "$GOPI_HOME/.gopi"'
/home/user/workspace/_tmp/fakewt-tAIN/.gopi
```

**Post-fix, same invocation** (`real-run-2.log`, this tree's binary):

```
[gopi] environment: the spawned shell does NOT inherit GOPI_HOME=/home/user/workspace/_tmp/fakewt-tAIN:
that value names a code checkout (it carries .git), so its profile root would be a throwaway inside the
checkout itself. The shell resolves the operator's own profile instead; set GOPI_HOME to a real home to
give it a private one.
NO_GOPI_HOME
```

The strip is **announced with the value and the reason** (Rule 15), and the shell
falls back to the operator's own profile instead of a throwaway.

## 3. The change

### 3.1 ONE predicate + its constants (`internal/config/gopihome.go`)

```go
const CheckoutMarkerName = ".git"          // Rule 1/14: one spelling
func GopiHomeNamesCodeCheckout(home string) bool
```

**The test is the marker, not the path shape.** `$GOPI_HOME/.git` *existing* is a
FACT about the value; "it must be a gitdir POINTER file" would be an inference
about how worktrees are laid out (Rule 41: inference never stands in as a
criterion). Existence covers all three checkout shapes — a work tree (marker is a
directory), a `git init`-ed dir, a bare repo, and a worktree (marker is a FILE).

**The keep arm is the load-bearing half.** `GOPI_HOME=<sandbox>` — a real
directory holding `.gopi`, what `gopi tool sandbox create` materialises and what
`gopi tool sandbox -h` instructs — carries **no** marker, so it is never refused
and keeps inheriting. Pinned by test, both directions.

Relative and empty values are not checkout claims (an empty value means "nothing
was published"); `strings.TrimSpace` + `filepath.IsAbs` are the guards.

### 3.2 The seams a run actually uses

| seam | code | arms pinned |
|---|---|---|
| **the bash tool child** — every shell the harness spawns | `stripWrapperIdentity` (`internal/tools/childenv.go`), reached by `bashChildEnv` (`bash.go`) and `JobStart` (`job.go`) | red: `TestBashChildEnv_DropsACheckoutGOPIHome` · keep: `TestBashChildEnv_KeepsASandboxGOPIHome` |
| **the executor spawn** — the `task run` child | `stripCheckoutHomeEnv` in `taskChildExecEnv` (`cmd/agent/task_child_env.go`), announced by `taskCheckoutHomeStripLine` at the spawn seam (`cli_tasks.go:3073`) | red: `TestStripCheckoutHomeEnv_DropsACheckout`, `TestTaskChildExecEnv_DropsACheckoutGOPIHome` · keep: `TestStripCheckoutHomeEnv_KeepsASandbox` |

The predicate is referenced through the aliases `tools.GopiHomeEnvName` /
`tools.GopiHomeNamesCodeCheckout` (Rule 13/14: one definition, no mirror).
`internal/tools` already imports `internal/config` in 144 files, so the alias adds
no dependency edge.

Announcement surfaces: the bash seam prints `checkoutHomeStripRecord(...)` to
stderr; the spawn seam writes `taskCheckoutHomeStripLine(...)` into
`execRunFacts`. **Both render nothing when the value was kept** — inheritance is
the normal case and a normal run's stderr is byte-identical to before.

### 3.3 The consumer-side half: `scripts/notify/notify.sh`

The briefing claimed a worktree refusal was already shipped here. **Measurement
says it was not:** `git log --all -S'GOPI_HOME/.git'` → no commit, and the script
resolved `PROFILE="${GOPI_HOME}/${PROFILE_DIR_NAME}"` unconditionally. It now
refuses (§0 of the script), and it **still anchors the event** — in the
operator's own profile (`$HOME/.gopi`), stated in the refusal line:

```
notify.sh: REFUSING GOPI_HOME=<worktree> — it names a CODE CHECKOUT (it carries .git), so the profile
root would be <worktree>/.gopi: a throwaway profile inside the checkout that nothing sweeps, nothing
dispatches from and nothing reads back. The event is anchored in /home/<op>/.gopi/notify/task-events.jsonl
instead, and NOT inside the checkout. Point GOPI_HOME at a real home (e.g. /home/<op>) or at a sandbox.
```

Refusing to anchor at all would lose the delivery — the very failure the guard
exists to stop. Nothing is written inside the checkout (asserted).

## 4. Red proofs (mutation, run and observed)

The predicate body was mutated to `return false` (a probe, then restored):

```
RED   internal/config  TestGopiHomeNamesCodeCheckout_RecognisesACheckout
      → = false … a directory carrying ".git" IS a code checkout
RED   internal/tools   TestBashChildEnv_DropsACheckoutGOPIHome
      → bashChildEnv handed the shell GOPI_HOME=<worktree> …
RED   cmd/agent        TestStripCheckoutHomeEnv_DropsACheckout / TestTaskChildExecEnv_DropsACheckoutGOPIHome
GREEN (after restore)  all three packages ok
```

The restore was verified by **content identity against a pre-mutation copy**, and
`grep -c 'MUTATION PROBE'` → 0 (Rule 5's restore criterion).

The shell half is a case in the existing contract test:
`bash scripts/notify/notify_slot_contract_test.sh` → `all assertions passed (6
cases)`, case 6 being the checkout refusal; cases 1–5 double as the **keep arm**
(every one of them runs with a sandbox-shaped `GOPI_HOME` and must keep working).

## 5. Honest residual gaps

- **The run's OWN startup still writes the throwaway profile.** Measured above:
  with `GOPI_HOME=<checkout>`, the run itself creates `<checkout>/.gopi/memory.db`
  + `sessions/` at startup, before any seam this task touched. Those two seams
  govern what CHILDREN resolve; the run's own profile resolution is a separate
  door (a startup refusal next to `cli_sandbox_profile_guard.go`), and it is
  filed as its own row rather than widened into this change (Rule 28: narrow).
- **The predicate is per-value, not per-process.** A child that inherits an
  ordinary home and then `cd`s into a checkout is fine (its profile is not in the
  checkout). A launcher that exports a checkout during the run, after the strip,
  is out of reach of a seam that runs once.
- **`internal/tools/childenv.go`'s strip is shared** with the wrapper identity and
  report-contract strips; the checkout arm was added to that ONE list rather than
  a second list, so all three consumers (`bash`, `JobStart`, and — through the
  cmd/agent function — the executor spawn) get it by construction.

## 6. Verification (as run)

```
go build ./...                                        → OK
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/       → OK
go test ./internal/config/ -run TestGopiHomeNamesCodeCheckout        → ok
go test ./internal/tools/  -run 'TestBashChildEnv_|TestCheckoutHomeStripRecord' → ok
go test ./cmd/agent/       -run 'TestStripCheckoutHomeEnv|TestTaskCheckoutHomeStripLine|TestTaskChildExecEnv_DropsACheckout' → ok
bash scripts/notify/notify_slot_contract_test.sh      → all assertions passed (6 cases)
<real binary> "… env | grep '^GOPI_HOME=' …"          → record + NO_GOPI_HOME
```

See `~/.gopi/sessions/01a14a53-b5cf-7a9a-aa07-72eabc872571/task-report.md` for the
`Ran:` lines verbatim.
