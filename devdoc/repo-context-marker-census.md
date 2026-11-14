<!-- doc-class: normative -->
# The repo-context gate's marker census: an orphan is either a missing ARM or a stale MARKER

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Class: mechanism** · measured 2026-10-16 (task `01a143b4684f772da77cf3d3394e4080`,
branch `task/01a143b4684f772da77cf3d3394e4080`), ground at merge `1921b4a67`.

## The red, and what it was NOT

`cmd/agent`'s `TestSpawnersPinTheRepoContext` was red on master's line with:

```
the tree carries 4 "repo-context-ok:" marker(s) but this scan COUNTED only 2 escaped spawn site(s)
```

The verdict's own wording blamed the *scan* ("a documented exception stopped being
recognized"), and that reading is the trap: two of the four markers were not
missed by the scan at all — they covered nothing in the first place.

## Item 1 — `worker_control_sigint_e2e_test.go:200`

The marker sat in `newWorkerCtrlSigintFixture`. Its own reason ("the env vector
goes to the REAL binary under test, built by the fixture above from a sandbox
profile") describes a site that **already calls the pin**: the same function has
`SandboxRepoContext(t)` nineteen lines below. The gate never needed the escape —
the site is `PINNED` (measured: `pinned=true callsPin=true` for that function).

The marker was written after the gate reached its present shape (`d1f88b5e5`,
2026-10-16, merged into master by `6c625a3a1` / `18ed99777`; the gate file at
`1921b4a67` is still `c6df765a5`'s, which is where the argv-shape arm below was
frozen). So this is marker drift, not predicate drift: a superseded permission
nothing reads — Rule 13's exact case.

## Item 2 — `worker_control_sigint_e2e_test.go:268`

Same class, one layer deeper. `.runStopWithCtrlC` spawns
`gopi worker --stop --pid …`, and the gate's worker arm reads the argv of a
`worker` invocation: `rcsReadOnlyWorkerControls[argv[i+1]]` matched
`workerControlStop` and `continue`d — out of the verb loop, before `rcsCallSpawnsLifecycleVerb`
could return true. `rcsFunctionSpawnsLifecycleChild` was therefore `spawns=false`
for the function, so its marker was never **counted**, never reached the escape
arm, and never reached the pin arm.

That `continue` is itself the shape of the defect: `continue` moves to the NEXT
argv element, so `{"--status", "--stop"}` still returned `true` (which is what
lets `status_action_conflict_e2e_test.go:321` be classified at all). The waiver
never matched a "read-only worker control" as a class — it matched whichever
control word happened to follow `worker` first. Two call sites were excluded on
that basis by accident of argument order.

## The criterion, and why `--stop` stays waived

`rcsReadOnlyWorkerControls` was renamed `rcsWorkerControlsReadingNoRepo`, and the
criterion it now carries is the one the merger arm already used: **excluded iff
the invocation resolves no repo set**. Per member:

- `--status` / its verbose residue spelling: `workerStatusRequested` →
  `workerStatusReport` walks the state files and `/proc` and returns
  (`cmd/agent/main.go`). No settings, no repos table, no git.
- `--stop`: it *acts* (`workerControlCommand` → `tools.ControlDir(workerStateDirName)`
  → signal the target → `workerAwaitGone`), but `cli_worker_control.go` references
  no settings file, no repos table and no git. The hazard this gate exists for is
  a child whose **cwd-resolved repo set** lets a close-out pass free the
  developer's worktrees; a pid-signalling invocation cannot reach that, whatever
  working directory it inherits.

So a worker-control site is neither pinned nor escaped — which is what makes both
markers orphans rather than missing recognition.

## The second, quieter lie: the census counted MENTIONS

`strings.Count(src, "repo-context-ok:")` counts the marker's spelling ANYWHERE —
including a comment that merely explains that an escape was removed. It was doing
exactly that in this task's own first draft: three directives, four counted hits.
A census that counts mentions reports drift that is not there and hides drift that
is. `rcsMarkerDirectives` now counts lines that ARE the directive (a `//` comment
opening with the marker). At the time the escape arm (`rcsFunctionIsEscaped`)
still accepted a mention inside a function body as a reason; that half is closed
below.

## Item 3 (2026-10-17) — the AST side was the same lie, one level down

The census was tightened by `4d9e7fd96` and the escape arm was left on
`strings.Contains(strings.Join(lines[start:end], "\n"), rcsEscapeMarker)`: any
occurrence of the marker's **spelling** inside the function's body bought that
function an escape. A prose sentence that quoted the token ("the census must
never count a bare `repo-context-ok:` mention as a permit") was therefore a live
permit, while the census — reading directives — counted nothing. The tree stayed
green over a permission nobody wrote, and the mismatch verdict could never see it
because the two sides disagree in `escaped`'s favour.

### The vocabulary — one line, three readings

| reading | shape | census counts it? | escape arm accepts it? |
|---|---|---|---|
| **DIRECTIVE** | a `//` comment whose own text opens with the marker: `// repo-context-ok: <why>` | yes | yes — the ONLY accepted form |
| **MENTION** | the marker's spelling anywhere else: mid-sentence, in a doc, in a refactor note recording a deleted escape | no | no |
| **OUTSIDE THE BODY** | a directive written above the `func` line, or in another function | yes (it is still a directive in the file) | no — it escapes the next function, not this one |

Both halves now read ONE predicate, `rcsLineIsEscapeDirective`, through
`rcsMarkerDirectiveLines`: `rcsMarkerDirectives` is the count over a whole source,
`rcsFunctionIsEscaped` is the count over the function's own line range. There is
no second spelling of "is this a permit" left in the file, so the two figures
cannot drift apart without the predicate itself changing — and
`TestRepoContextEscapeIsADirective` pins the three readings together (each case
asserts the escape arm AND the census on the same source).

An escape covering no counted site still fails the build, unchanged: the census
figure is compared to the number of counted escaped sites, and an orphan
directive reds the mismatch verdict whichever side it is on.

### Proof (2026-10-17, one mutation at a time, this worktree)

| mutation | result |
|---|---|
| a PERMIT turned into a sentence — `wdForeground`'s directive becomes `… — this is the repo-context-ok: case` inside the body | RED after the fix, naming `worker_daemon_singleton_e2e_test.go:81: wdForeground()` |
| the same tree against the PRE-FIX predicate (`strings.Contains` re-installed) | GREEN — the permit nobody wrote was accepted; this is the asymmetry, measured |
| the same tree + one ORPHAN directive (a second `// repo-context-ok:` line inside `mergeE2EStartRun`, which already carries one) | RED on the census arm under BOTH predicates: `carries 3 … COUNTED only 2` — an extra directive on an already-escaped function is over the count, never a cover |
| a real directive DELETED (`mergeE2EStartRun`) | RED: `merge_lock_e2e_test.go:198` reported as an offender (it lost its only permit) |
| an ORPHAN directive ADDED on top of the fixed predicate | RED on the census arm: `the tree carries 3 … but this scan COUNTED only 2`, naming `markers in the tree: merge_lock_e2e_test.go ×2, worker_daemon_singleton_e2e_test.go ×1` |

Green line after the fix: `110 lifecycle spawn site(s) scanned, 89 pinned, 2
escaped, 19 daemon-start (own-dir) site(s), 2 escape marker(s)` — `rcsScanFloor`
still `80`, untouched.


## The verdict now names both sides

The mismatch message printed a count against a count, so the reader had to
re-derive the whole census to find the orphan. It now prints
`markers in the tree: <file> ×N, …` beside `escaped spawn sites this scan counted:
<file:line: fn()>, …`, and states the two legitimate reconciliations (re-recognize
the site, or delete the marker) — with "delete" naming the three reasons a marker
legitimately dies: the site is no longer a recognized lifecycle spawn, the
excluding predicate is evidenced in the product, or the site became PINNED.

## Proof (each pair measured, one mutation at a time)

| mutation | result |
|---|---|
| delete `wdForeground`'s marker (a real cover disappears) | RED, naming `worker_daemon_singleton_e2e_test.go:80: wdForeground()` |
| re-add an orphan marker directive | RED: `3 markers … counted only 2`, both sides listed |
| same tree (2 directives + 1 mention), OLD raw-substring census | RED: `carries 3 … counted only 2` |
| same tree, NEW directives-only census | GREEN |

Green line: `108 lifecycle spawn site(s) scanned, 87 pinned, 2 escaped, 19 daemon-start (own-dir) site(s), 2 escape marker(s)`
with `rcsScanFloor = 80` untouched. A "mention" line in a shell single-quoted
grep cannot be counted, hence the `python3` mutation recipe above (an inline
`grep -c` reproduces the count on the C2 arm).

## Reproduction recipe (a future red of this shape)

```bash
cd <worktree> && go test ./cmd/agent/ -run TestSpawnersPinTheRepoContext -count=1 -v
# read the new verdict: it NAMES the marker files and the counted sites.
# For each marker file, find its function and ask ONE question:
#   is the site PINNED, or is it excluded by an arm whose evidence is "reads no repo"?
# yes -> the marker is the orphan: delete it, and say which reason applies.
# no  -> the arm lost the site: widen recognition (never the floor, rcsScanFloor=80).
```
