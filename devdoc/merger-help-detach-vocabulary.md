<!-- doc-class: historical -->
# Task Report: merger help names no detach mechanism — the class closed, not one token

Rework of `fix/merger-help-setsid-vocab` (sha `5c476ade`), which could not merge
into master (CONFLICT in `cmd/agent/main.go`). This report states **what of the
original change is still real work**, what master had already superseded, and
how the conflict was resolved.

## The red (original)

Master `9ce8e159` was RED on a single test:

```
--- FAIL: TestServiceHelpIsCapabilityFirst (0.00s)
    help_capability_first_test.go:115: mergerUsageHead: carries process-mechanics
    vocabulary (session leadership) in user help (setsid)
FAIL	github.com/nick/gopi/cmd/agent	1.662s
```

The offender was one clause in the Notes tail of `mergerUsageHead`
(`cmd/agent/main.go`), added by ancestor `f234658c` (merger stop/restart control
surface): *"The loop is the operator's OWN foreground process (or their own
`setsid nohup`)"*. `setsid` was already in the gate's `hcfForbiddenFragments`.

## Why the branch conflicted, and what master had already done

The conflict is **the same fix, made twice, differently**:

| | the clause after "No flag detaches anything. The loop is the operator's OWN foreground process" |
|---|---|
| base (`f234658c` … `9ce8e159`) | `(or their own 'setsid nohup'), it holds …` |
| master today (`06438272`, "feat(merge): add the foreground one-shot `gopi merge` command") | `(or their own 'nohup'), it holds …` |
| the reworked branch | `(or a launcher of the operator's own, if it must outlive the shell), it holds …` |

`06438272` dropped the **token** `setsid` — enough to make
`TestServiceHelpIsCapabilityFirst` green on master (measured: the focused trio is
`ok` on master `a9a96cd7`). It kept the detaching **program** `nohup`, which was
never on the gate's list, so the page still **chooses a launcher for the reader**
— the same devdoc-shape defect, one token over.

So the rework is NOT a replay: the branch's text is taken as the better fix, and
its decision record is the part that keeps the class closed.

## The decision (recorded where each future agent looks)

**Reword the shipped page; never exempt the token.**

- `cmd/agent/main.go` — a comment block immediately above `mergerUsageHead`
  ("THIS PAGE NAMES NO DETACH MECHANISM") states the rule: the user-facing fact
  is *"a flag detaches nothing, and the loop is your own foreground process"*;
  HOW a caller outlives its shell belongs to the caller and to the
  implementation's devdoc, never to `-h`. It also names `06438272` as the
  **partial** predecessor (token dropped, program kept) so the next reader sees
  why the launcher went too.
- `cmd/agent/help_capability_first_test.go` — the `setsid` entry of
  `hcfForbiddenFragments` now carries that decision (the refused exemption, the
  partial-fix history) and says **"never widen this list"**.

### Why not the exemption (the part a future reader will re-ask)

Rule 25's *"mention is not prescription"* is the tempting argument — and it is
**the wrong rule here**. Rule 25 is about **PRIVILEGE** text: the help pages must
be able to name a privilege in order to *deny* it. Nothing on these pages needs
to name a detach *call* to state that no flag detaches: the fact is expressible
without the identifier, and it now is. An exemption would have bought a green
gate by teaching the page that process mechanics are allowed in `-h`.

### Why `nohup` is not simply re-listed as a forbidden fragment

Because that would make the gate a name list again — the next launcher spelling
(`disown`, `screen`, `tmux`, `&`) repeats the argument. The **page** is where the
class is closed (no launcher named at all); the gate entry stays the record of
the decision. The invariant the gate enforces is unchanged.

## Superseded by the retirement (measured, and it changes the verification)

Master **retired the resident merger after this branch was cut**:
`cmd/agent/merger_retired.go` (2026-09-19) closes BOTH live entry points —
`isMergerSubcommand` (top-level `gopi merger`) and the `gopi task merger` /
`gopi task serve` aliases — and `TestRetiredMergerRefusalIsNotTheOldMessage`
asserts `gopi merger -h` must NOT print `mergerUsageHead`.

Measured on the reworked tree:

```
$ GOPI_HOME=<tmp> ./bin/gopi merger -h
merger: retired — the resident merger daemon was retired for re-development: `--stop` is only answered at a TICK BOUNDARY, …
  merging itself is NOT retired: use the one-shot `gopi merge` face for a merge pass
exit=1
grep -c setsid  → 0      (but the page is not printed AT ALL, so this number proves nothing)
```

Consequences, stated plainly:

- **The original branch's shipped-page verification is no longer reproducible.**
  Its report claimed `./bin/gopi merger -h` → `grep -c setsid` = 0 as the
  decisive evidence. That command now proves nothing about the page: the page is
  gone from the output. This rework does **not** re-assert it.
- **The constant is dormant, not dead.** The retirement keeps the implementation
  verbatim for re-development ("to revive the feature, delete the retireMergerFace
  call at each entry point; nothing else is required"), and FOUR gates still run
  unconditionally over `mergerUsageHead`: `help_capability_first_test.go:112`,
  `help_width_test.go:261`, `daemon_scope_no_privilege_test.go:255`,
  `cli_tasks_advert_gate_test.go:362/439`. So keeping the page clean is not
  cosmetic — it is what a revival starts from, and the gate would go red the
  moment the face is revived if the class were left open.
- **What is superseded is the SHIPPED-TEXT HALF of the original intent**: master
  made the gate green by dropping the token (`06438272`) before the retirement,
  so no live user-facing page is improved by this diff. The half that is NOT
  superseded, and the reason this lands, is the **decision record** (comment +
  gate vocabulary entry + this devdoc).

## Files

- `cmd/agent/main.go` — rewrapped the Notes clause (no `setsid`, no launcher) +
  the decision comment above the page's declaration.
- `cmd/agent/help_capability_first_test.go` — the `setsid` vocabulary entry now
  carries the decision ("never widen this list").
- `devdoc/merger-help-detach-vocabulary.md` — this report.

## Not Done (honest)

- **Not a `git am` of the original patch.** The patch's `cmd/agent/main.go` hunk
  is superseded by master's own rewording (above); the hunk was re-derived
  against current master instead of replayed, which is what the conflict
  demanded. The result is a strict improvement on both predecessors, not a
  byte-identical reland.
- **Not verifiable on the shipped page** (see the retirement section): the
  original `gopi merger -h` grep is retired with the verb. Verification here is
  at the CONSTANT level (`mergerUsageHead`), which is what the four gates read.
- **No `--daemon`/resident-loop e2e**: the merger's resident-loop suite spawns
  real resident processes (the slow path), and the retirement means there is no
  live face to walk.
- The four child-retry fixture failures from that same master run are a SEPARATE
  task — untouched here (this diff is help text + one gate comment + a devdoc).

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → `BUILD_OK` (exit 0).
- Vet: `go vet ./...` → exit 0, no output.
- Focused gate + help family:
  `go test ./cmd/agent/ -count=1 -run 'Help|CapabilityFirst|hcf|RetiredFlag|ControlFlag|Privilege'`
  → `ok github.com/nick/gopi/cmd/agent 35.688s` (covers
  `TestServiceHelpIsCapabilityFirst`, `TestCapabilityFirstPredicateIsNotBlind`,
  `TestEveryHelpConstantFitsTheWrapBudget`, `TestHelpWidthPredicateIsNotBlind`,
  `TestServiceHelpDetailStaysAtTheEnd`, `TestHelpConstantsCarryNoCarelessText`).
- Scoped smoke (touched package): `go test ./cmd/agent/ -count=1` — recorded in
  `## Evidence (self-filed)` below.
- Worktree clean: `git status --porcelain` prints nothing after the commit.

## Evidence (self-filed)

- Before edit (master worktree, page-level red is already gone):
  `go test ./cmd/agent/ -count=1 -run 'TestServiceHelpIsCapabilityFirst|TestCapabilityFirstPredicateIsNotBlind|TestNoHelpTextIsDuplicatedAcrossPages'`
  → `ok github.com/nick/gopi/cmd/agent 2.347s` — master is GREEN, so this rework
  is not a red-to-green fix; the value is the class closure + decision record.
- Conflicting hunk, master vs branch (the whole conflict):
  `sed -n '693,695p' cmd/agent/main.go` on master →
  `No flag detaches anything. The loop is the operator's OWN foreground` /
  `process (or their own 'nohup'), it holds the single-instance flock,`;
  the branch's side of the same hunk is in
  `~/workspace/merge-run-diffs/fix_merger-help-setsid-vocab.patch`.
- `git log -L 690,700:cmd/agent/main.go master` → the clause's last change is
  `06438272 feat(merge): add the foreground one-shot gopi merge command`
  (`'setsid nohup'` → `'nohup'`); the clause itself came in from `f234658c`.
- After edit, widths of the rewritten lines (help-wrap budget):
  `awk` → `70|  No flag detaches anything. The loop is the operator's OWN foreground`,
  `70|  process (or a launcher of the operator's own, if it must outlive the`,
  `73|  shell), it holds the single-instance flock, it is reported by --status,`,
  `75|  and it is ended by the signal it documents — so there is nothing hidden`,
  `53|  for a flag to reach. The two control flags (%s) are` — all ≤ 79.
- Retired face (the reason the shipped-page grep is not evidence):
  `GOPI_HOME=<tmp> ./bin/gopi merger -h` → exit **1**, prints
  `merger: retired — the resident merger daemon was retired for re-development…`
  and does NOT print the page; pinned by
  `TestRetiredMergerRefusalIsNotTheOldMessage` (`merger_retired_face_test.go:105`).
- Gate coverage of the dormant constant (it is still read):
  `grep -rn 'mergerUsageHead' cmd/agent/*_test.go` →
  `cli_tasks_advert_gate_test.go:362,439`,
  `daemon_scope_no_privilege_test.go:255`, `help_capability_first_test.go:112`,
  `help_width_test.go:261`.

