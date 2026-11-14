<!-- doc-class: normative -->
# cgroupscope: Available must ask the question the MIGRATION asks

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task: `01a12c31def6749ca8a3330469dc0fba` · branch `task/01a12c31def6749ca8a3330469dc0fba`
Host: host-b, kernel 6.8.0-138-generic, cgroup v2 unified.

## The defect

`internal/cgroupscope`'s two verdicts about the same escape disagreed on this
host: `Available(1000)` returned

```
cgroup scope escape unavailable: migrating a pid into cgroup.procs:
/sys/fs/cgroup/user.slice/user-1000.slice is not writable by this user (permission denied)
```

while `Enter(1000, "gopi-probe-agree")` performed the migration and verified it
with a fresh `/proc/self/cgroup` read. `TestAvailable_AgreesWithEnter` — the
package's own agreement test — was RED.

The premise the old `Available` encoded was *"the migration gate is write access
on the per-uid slice, checked unconditionally"*. It is wrong, and it is wrong in
a POSITION-DEPENDENT way.

## The rule, measured (not inferred)

cgroup v2's cross-subtree rule walks the migration's SOURCE and TARGET to their
lowest **common ancestor cgroup** and asks may-write on that one cgroup — then
the low-level vet. On one host, at one instant:

| caller's real position | same target | common ancestor | verdict |
|---|---|---|---|
| this run: `…/user@1000.service/app.slice/…/vte-spawn-….scope` | `…/user@1000.service/<scope>` | `…/user@1000.service` (the operator:the operator 755) | **OK** |
| ssh: `…/user-1000.slice/session-3452.scope` | the same target | `…/user-1000.slice` (root:root 755) | **EACCES** |

The two positions are **siblings** (a session scope and `user@1000.service` are
both children of `user-1000.slice`), which is why one escapes and the other
cannot. A static slice check therefore decides NEITHER case: it reports the
working case as unavailable.

Full 12-row matrix (7 targets × 2 source worlds) plus the veto rows:
`~/.gopi/sessions/01a12c31-def6-749c-a8a3-330469dc0fba/artifacts/migration-gate-evidence.md`.

**The secondary finding — the veto behind the permission gate is the inverse of
its naive reading.** Measured: a target with a *populated* child and an empty
`cgroup.subtree_control` ADMITS a migration (rc=0), while a target with an
*empty* child and `subtree_control = memory` answers **EBUSY**. The predicate is
"does this cgroup have a controller enabled in its own `cgroup.subtree_control`"
(the no-internal-process constraint), not "is it holding processes". Two rows
settle it: the manager itself (`subtree_control = memory pids`) is EBUSY, and a
directory already holding one proc accepts a second (rc=0).

## The fix

`Available` now asks that question:

1. **the migration gate** — `commonAncestor(SelfRel(), target.Rel)`, then
   `Writable()` on it. Computed from where the process really IS and where it is
   really going, so the same code gives the measured answer from either
   position.
2. **the kernel's veto behind it** — `targetMayAdopt()` reads the target's
   `cgroup.subtree_control`; a directory that does not exist yet is admitted (it
   cannot hold a controller, and gate ③ is what creates it empty).
3. **the create gate** — `Writable(manager)`, unchanged in role, now reached
   only when ① and ② pass.

New in the package: `commonAncestor`, `escapeTarget` (one definition of the
target that both `Available`'s prediction and the protocol sentence are spelled
from), `targetMayAdopt`, `MigrationProtocol`. `SubtreeControlFileName` and
`noControllerEnabled` are named constants (Rule 1). `SliceDir` KEPT and
re-documented — it is the real gate for a caller *outside* the manager subtree,
so it is not deleted, just stripped of the claim that it is the gate for
everyone.

**Honest limit, stated in the source (Rule 15):** this is a PREDICTION from the
same inputs the kernel uses, not the kernel's own answer. The authoritative
verdict remains `Enter`'s fresh `/proc/self/cgroup` read; `Available` only
decides whether acting is worth a try.

## Safety of the oracle (a criterion, not a footnote)

A successful migration is irreversible without a second one, so the probe may
only do something safe when it SUCCEEDS. `Available` performs no write and no
mkdir; its reads are `Writable` (`access(2)`), `os.Stat`, the target's
`cgroup.subtree_control`, and `SelfRel`. Pinned by
`TestAvailable_DoesNotMoveTheCaller` (a before/after `/proc/self/cgroup`
comparison plus a filesystem check, on both verdicts). The scripts in this run's
session directory probe with DISPOSABLE children, never with the caller.

## Tests added

- `TestCommonAncestor_IsTheLowestSharedCgroup` — the measured in-manager and
  ssh positions, plus root/sibling/self cases; host-independent.
- `TestTargetMayAdopt_ReadsSubtreeControlNotTheChildTree` — the fixture whose
  INVERTED row (populated child, no controller → admitted) is what makes the
  test discriminating rather than a restatement of a doc.
- `TestTargetMayAdopt_MissingTargetIsAdmitted`, `MigrationProtocol_…`.
- `TestNaiveSliceCheckIsNotTheMigrationGate` — the red proof, kept as a guard;
  skips with a printed reason on a host that cannot discriminate.
- `TestAvailable_DoesNotMoveTheCaller` — the oracle's safety contract.

## Red proof (performed, not asserted)

`Available`'s body patched back to the naive slice check, everything else
untouched:

```
--- FAIL: TestAvailable_AgreesWithEnter
    Available said the escape is unavailable (… user-1000.slice is not writable
    by this user (permission denied)) but Enter succeeded at
    "/user.slice/user-1000.slice/user@1000.service/gopi-probe-agree"
--- FAIL: TestNaiveSliceCheckIsNotTheMigrationGate
```

Restored → `ok github.com/the operator/gopi/internal/cgroupscope`.

## Verification

- Build: `go build ./...` → OK (exit 0, no output).
- Tests: `go test ./internal/cgroupscope/ -count=1` → ok
  github.com/the operator/gopi/internal/cgroupscope; host-dependent cases skipped with a
  printed reason on this host by their own design (`t.Skip` + reason).
- Ran: `go test ./internal/cgroupscope/ -run TestAvailable_AgreesWithEnter -count=1 -v`
  → `--- PASS: TestAvailable_AgreesWithEnter`, ok … 0.0xxs — from the in-manager
  position (`/user.slice/user-1000.slice/user@1000.service/app.slice/
  app-org.gnome.Terminal.slice/vte-spawn-….scope`), where `Available` returns
  nil and `Enter` then really migrates.
- Ran (the same code, the other world): the suite itself run over `ssh localhost`
  inside the SAME worktree, whose real position is
  `…/user-1000.slice/session-3455.scope` — `go test ./internal/cgroupscope/
  -run TestAvailable_AgreesWithEnter -count=1 -v` → `--- PASS`, `ok … 0.561s`.
  Both worlds PASS because the case asserts AGREEMENT (Available nil iff Enter
  succeeds), not a fixed answer: the in-manager verdict is nil and the ssh
  verdict is ErrNoEscape naming the ACTUAL common ancestor (`user-1000.slice`,
  root:root).
- Ran (the seam that consumes the predicate, real host):
  `go test ./cmd/agent/ -run TestDaemonScopeDecide_ChosenMechanismMatchesTheHost
  -count=1 -v` → `--- PASS (0.02s)`; `systemd-run --user --scope --quiet
  /bin/true` → exit 0, which is why `daemonScopeDecide` takes rung ① here (the
  dcEscapeChosenByTheSeam comment's stale premise was corrected in c89c4d358).
- Ran (END TO END, the real surface, two builds of this same tree): with rung ①
  forced impossible (its probe runs /usr/bin/systemd-run by absolute path, so a
  stripped environment defeats it — measured "Failed to connect to bus:
  $DBUS_SESSION_BUS_ADDRESS and $XDG_RUNTIME_DIR not defined"), the command
  `env -i PATH=/usr/bin:/bin HOME=$SB GOPI_HOME=$SB ./bin/gopi worker --daemon
  --require-scope` PRINTED, post-fix, "cgroup escape: cgroupfs-scope
  (/sys/fs/cgroup (uid 1000))" and "worker daemon started: pid 3095196", with
  /proc/3095196/cgroup = …/gopi-daemon-worker-<profile> — the escape really
  happened. With the naive predicate spliced back (nothing else changed) the
  same command printed "refusing to start: … the cgroup escape is unavailable:
  … user-1000.slice is not writable by this user". The probe daemon was killed
  and its death verified; nothing resident was left behind.
- Ran: `make test-budget PKGS='./internal/cgroupscope ./cmd/agent'` → the
  `./internal/cgroupscope` package is GREEN; `./cmd/agent` is RED, and the red is
  NOT this branch's: it is present at this branch's BASE (9a6c99962) and at
  master's tip, and a first-parent `git bisect` of `a3a31da42(ok)..9a6c99962`
  landed on `380151313` ("Merge branch
  'task/01a12bbee6e87ebbbb4d02a4d869f6e1'", the help-slimming/man-page epic,
  `feat(help): move verb detail into man pages`) — an ancestor of this branch
  that contains none of its changes. The red set, identical at base and at
  HEAD: `TestG5InventoryMatchesTheHelp` (the chat inventory lists 45 flags, the
  help advertises 47 — `--target`/`--all` missing),
  `TestG5SurfaceCensusIsDerivedFromConstants`,
  `TestChatDumpArming_TheHelpTextCarriesTheCommandThatArms`,
  `TestChatDumpHelp_NamesTheAutomaticAudit`,
  `TestChatDumpHelp_TheNamedAuditCommandIsTheAutomaticOne`,
  `TestChatCookedFallback_NonTTY`, `TestRunPathPlainPromptIsVerbatim`. Filed as
  its own bug row (`01a12c4c-af81-780a-a6b2-87195aee5583`, Rule 41); evidence +
  bisect transcript in the run's session evidence dir. Everything the DAEMON
  SCOPE seam this epic touches asserts is green
  (`TestDaemonScopeDecide_ChosenMechanismMatchesTheHost`).
- Result: `git status --porcelain` inside the worktree prints nothing;
  `git log --oneline -3` on `task/01a12c31def6749ca8a3330469dc0fba` shows
  `0da6fffd4 docs(cgroupscope): …` and `c4a1050fc fix(cgroupscope): Available
  asks the migration's real gate, not the per-uid slice`.
- Commit: c4a1050fc (code + tests), 0da6fffd4 (this doc).

## Evidence (self-filed)

- `go build ./...` → exit 0, no output.
- `go test ./internal/cgroupscope/ -run TestAvailable_AgreesWithEnter -count=1` →
  `ok github.com/the operator/gopi/internal/cgroupscope` (PASS).
- Red proof observed, not asserted: with the naive access(slice) predicate
  spliced back into `Available`'s body alone, the same command printed
  `--- FAIL: TestAvailable_AgreesWithEnter` (quoting
  `user-1000.slice is not writable by this user (permission denied)` against
  `Enter succeeded at "…/gopi-probe-agree"`) plus
  `--- FAIL: TestNaiveSliceCheckIsNotTheMigrationGate`; reverting the splice
  returned both to PASS in the same session.
- The evidence table's rows come from writing a DISPOSABLE child's pid into each
  target and reading the errno, then killing it — the caller was never migrated:
  `~/.gopi/sessions/01a12c31-def6-749c-a8a3-330469dc0fba/artifacts/
  migration-gate-evidence.md`.

## Completed

- `internal/cgroupscope`: `Available` derived from the operation's real gate
  (common ancestor + the kernel veto), measured in both source worlds.
- Agreement regression + 5 new host-independent unit tests; the old naive check
  red-proved.
- Evidence table in this run's session dir; devdoc (this file).

## Not Done (honest)

- **The veto's reach beyond gopi's own targets.** `Available` vets exactly one
  target (the default name, through `escapeTarget`), because it is a prediction
  for the caller's own migration — a caller that names a different scope
  directory should ask about that one. Exposed rather than papered over by the
  new `escapeTarget(uid, name)`.
- `TestAvailable_ReasonIsNotTheFabricatedEISDIR` SKIPS on this host now, exactly
  as it did before by its own design (it needs an unavailable verdict to
  inspect); it is not silently weakened — with the fix, `Available` legitimately
  returns nil from this position.
- The ssh world is asserted by measurement and by the `commonAncestor` unit
  case, not by a suite run inside an ssh session (the package's tests are run
  from this position; the ssh matrix is the evidence table above).
- **A withdrawn claim, kept visible (Rule 41).** An earlier draft of this report
  asserted that a second daemon of a face cannot re-enter `gopi-daemon` because
  that directory "carries `subtree_control = memory pids`". That was an
  INFERENCE from the wrong rows: the measured `memory pids` targets are
  `user@1000.service` and `app.slice`, and `gopi-daemon` did not even exist when
  the claim was written. Measured instead: a target gopi creates has an EMPTY
  `cgroup.subtree_control`, admits a migration, and admits a SECOND process into
  the same directory (rc=0, both pids listed) — so reuse is fine, and no bug was
  filed, because the risk class ("someone enables a controller in our target")
  has no measured instance here. The inference is recorded rather than deleted
  so the next reader does not re-derive it and act on it.
