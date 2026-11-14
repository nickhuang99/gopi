<!-- doc-class: historical -->
# The session-directory ENOENT: a spelling, not a sweeper

> **Status**: measured verdict, 2026-10-16, task `01a142630844745eb09a754876bcec9a`,
> baseline `61b0846`, host `hosta`.
> **Outcome**: the incident container was **never removed** — the two ENOENTs came
> from a command spelling that dropped a dash (below, measured). The fix premise
> the briefing carried (a sweeper deleting a live run's directory) was **falsified,
> twice**. The investigation that established that turned up a *different*,
> measured defect one arm over — the **session-attachments** arm collecting a live
> run's empty archive — which is **fixed, red-proved and pinned** in this branch.

## The incident

A run's own session directory `~/.gopi/sessions/01a14204-7022-7746-a450-e53ed95502f9`
returned `ENOENT` twice in a live run, then "reappeared untouched".

## What was measured, and what each measurement decided

### 1. The transcript's own spelling census — the ENOENT calls used a SHORT uuid

Every `01a14204…` literal in that session's transcript, by `(length, dash positions)`:

| length | dash positions | count | where |
|---|---|---|---|
| 36 | 9,14,19,24 | 209 | `session_start` … `assistant` |
| 32 | — (no dashes) | 229 | `session_start` … `assistant` |
| **35** | **9,14,19** | **31** | **`assistant` only, L399–L572** |
| 8 / 9 / 18 | — | 12 | truncated fragments |

The ONLY spelling the agent produced itself is the 35-character, four-dash form
(`01a14204-7022-7746-a450e53ed95502f9` — the 8-hex `e53ed95502f9` group is
concatenated with no separating dash). The canonical 36-character uuid appears
only in `session_start` and in the session's own log filenames — i.e. copied out
of a tool result, never typed by the model.

### 2. Every occurrence of the 4-dash literal in a tool RESULT is an echo of a failing call

Attribution over the whole transcript: **6** occurrences in tool results, **6** of
them inside the same result's own `No such file or directory` / `cannot statx`
text; **0** occurrences that are an independent source (a directory listing, a
`readdir`). So nothing in the transcript ever shows that spelling as an EXISTING
path — it exists only as the error text of the call that used it.

### 3. The container's own artifacts prove it was never destroyed

| artifact | birth time (`stat %w`) | inode |
|---|---|---|
| the container | 2026-10-16 **08:02:29** | 4756580 |
| `<session>.raw.log` (63 MB) | 2026-10-16 09:27:48 | 4756536 |
| `output.log` (376 KB) | 2026-10-16 09:27:46 | 4756526 |

`raw.log` (**63 MB**, growing continuously) lives INSIDE the container, and its
first line is its creation banner: `created 2026-10-16T01:27:48Z` = 09:27:48
+08:00. The ENOENTs are the run's ONLY signed observations and they sit inside
that 09:27:48 → 10:23:50 window. **A container that was removed and recreated at
09:47 (the moment the path "came back") would carry a 09:47 birth time and a
log starting at that instant.** It carries neither, and the log is continuous
across the window. The container was never gone.

### 4. The live discriminator, one command, two spellings

```
$ ls -d /home/user/.gopi/sessions/01a14204-7022-7746-a450-e53ed95502f9
/home/user/.gopi/sessions/01a14204-7022-7746-a450-e53ed95502f9          # canonical, 36
$ ls -d /home/user/.gopi/sessions/01a14204-7022-7746-a450e53ed95502f9
ls: cannot access '…01a14204-7022-7746-a450e53ed95502f9': No such file or directory   # the spelling used, 35
```

Exactly one entry matching `01a14204*` exists in `~/.gopi/sessions`, and only one
spelling resolves to it. The two commands above reproduce the incident's ENOENT
and its "untouched" path side by side, today.

## Candidates the measurement killed

**Candidate (a) — "two containers for one uuid with different dash groupings; a
read-spelling illusion".** Refined rather than killed, and this is the incident's
mechanism: there is ONE container, and the *transcript* carries both spellings
because the model produced the wrong one. Prior art (`memory #10865/#10870`, task
`01a13680`) had observed the same shape and called it an illusion — the
measurement here says the illusion IS the defect: the wrong spelling is authored
by the agent, not by a duplicated directory, which is why nothing was deleted and
everything "came back".

**Candidate (b) — the sessions-tree scratch GC arm (`internal/scratch/sessions_scratch_dir.go`,
`tree=session-raw`) emptying a live run's container.** **KILLED**, twice over:

1. The **real sweep** was run against the LIVE profile
   (`gopi tool scratch-gc trees --dry-run`, plan captured verbatim): the incident
   container does not appear in the plan at all — `0` occurrences. It is not a
   candidate.
2. The **live-veto hypothesis** behind the briefing — "the ledger veto asks only
   about the candidate path, so a live container's child is collectable" — was
   tested with a controlled minimal sample in a sandbox profile
   (`gopi tool sandbox create`, Rule 34), four containers, everything aged 40 days
   past every window in play, and the real sweep verb as the instrument:

   | fixture | holder | real sweep verdict |
   |---|---|---|
   | A: cwd == `<A>/artifacts` | live `sleep` (pid recorded) | **absent** from the plan |
   | D: cwd == `<D>` itself | live `sleep` (pid recorded) | **absent** from the plan |
   | C: carries `metadata.json` | none | **absent** (ledger veto) |
   | **B: nothing holding it** | **none** | **`would-remove … tree=session-raw`** ← the control |

   The one-sided veto the briefing posited does not exist in the running code:
   `heldSet` indexes each holder's path **and every ancestor of it up to the tree
   root**, so a holder standing in the container already protects the directories
   beneath it. The `heldIn(candidate)` call reads as candidate-only in isolation,
   and reading it that way is what made this look like a defect (Rule 9/31:
   a symbol hit is not implementation, and this is the same failure one step
   further in — a *code read* is not a *measurement*). Had the control not been in
   the fixture, "nothing was collected" would have looked like a bug.

   *Honest scope of this probe*: the phase-1/phase-2 pair (identical fixture, holders
   alive vs. dead) was intended as a second control; the phase-2 leg captured no
   candidate lines in the time it was given, so the discrimination rests on the
   control container **B being listed in the SAME run with identical age and no
   holder** — which is the stronger evidence anyway: one sweep, one fixture, one
   variable.

## The residual finding: the ATTACHMENTS arm, and the fix that lands

Chasing candidate (b) to its end turned up a defect that is real, measured, and
NOT the one the briefing described — in a **different arm of the same tree**.

### What was measured

`attachmentsArchiveCollectable` decides whether `<container>/attachments` may go.
Its vetoes were `heldIn(<container>/attachments)` (does a live process hold **the
archive**) and `unheldRegistered(<container>/attachments)`. **The container is
never asked.** A live run does not stand in its archive — it stands in its
CONTAINER (`cd $S`), so the question about the archive cannot see it.

Run against the real sweep verb in a sandbox profile, one fixture, one variable:

| fixture | holder | real sweep verdict |
|---|---|---|
| K: `metadata.json` + `*.jsonl`, empty aged `attachments/` | **none** | **`would-remove … tree=session-attachments`** |
| W: same, non-empty `attachments/` | none | absent (refused: it holds bytes) |
| H: record-less container, empty aged `attachments/` | none | `would-remove` (the intended case) |

Then the decisive pair — the SAME fixture, differing only in the holder:

| fixture | holder (cwd == the container) | real sweep verdict |
|---|---|---|
| G: `metadata.json` + `*.jsonl`, **empty** aged `attachments/` | no holder | `would-remove … tree=session-attachments` |
| W: same shape | **live process, cwd == the container** | `would-remove … tree=session-attachments` ← **the defect** |

A container carrying `metadata.json` **and** a `*.jsonl` — a live run's own record,
exactly what the session-raw arm treats as "the ledger describes it, KEEP" — had
its empty archive listed for removal while a process stood in it.

### Scope, stated honestly

The exposure is **the empty archive only**: an archive holding anything is refused
by `attachmentsArchiveHoldsNoEvidence` before the holder question is asked
(measured, probe 6: non-empty → absent in every configuration). That is still the
case this arm exists for — **75,602 of 75,851 archives are empty** (measured
2026-10-02, recorded in the arm's own test). And an empty archive that disappears
costs a directory with real consumers: `gopi task attachments` resolves
`<GOPI_TASK_SESSION_DIR>/attachments`, which a task's attachment lands in when the
source file is already on disk (the no-op COPY in
`internal/tools/task_attachments.go`).

### The fix

`AttachmentsArchivePlan` now asks the **container** question before the candidate
one, one line at the decision point:

```go
if heldByAncestor(dir, filepath.Join(root, container.Name()), held) { continue }
```

`heldByAncestor` (new, `internal/scratch/sessions_scratch_dir.go`) walks from the
candidate up to and including the container and asks the tree's ONE holder index
(`heldSet`, the single `/proc` pass) at each level. It is **bounded at the
container**: a holder standing in the sessions root is sweeping the tree, not
working in one container, and must not freeze every container under it.

**Why the candidate-only question cannot be fixed in `attachmentsArchiveCollectable`:**
that function is given a path, not a container, and the container is the fact the
decision was missing. The veto therefore lives where the container is known — the
walk — and the arm's existing predicate is left alone, so the "holds bytes" and
"probe leaves no trace" contracts are untouched.

The tree's `/proc` pass moved behind `attachmentsArchiveHeldIndex` (a var), the
same seam this package already uses for the evidence verdict
(`attachmentsArchiveHoldsNoEvidence`), because a holder is a LIVE PROCESS a test
cannot schedule deterministically.

## The guard that now exists — and its red direction

`internal/scratch/attachments_archive_live_container_test.go`:

- **KEEP** — an archive whose container a live process stands in is not planned.
- **CONTROL** — the identical fixture with **no holder** IS planned, so "nothing
  was collected" cannot be satisfied by a sweep that lists nothing.
- **RED PROOF** — what the real `/proc` pass yields for a container-cwd holder is
  `{container, sessions-root}` and **nothing about the archive**: on that index the
  pre-fix question is blind while the container question is not; and with an index
  that cannot see the holder at all, the archive **is** planned. So the KEEP is the
  container veto at work, not the age rule, the contents rule or a sweep that never
  lists anything.
- **SCOPE** — an archive holding bytes is still refused, and an empty unheld
  archive is still collected.

**Recorded red→green, both directions measured on this branch:**

```
# RED — ONLY the container-veto block removed from the production file:
--- FAIL: TestAttachmentsArchiveKeepsALiveRunsArchive
    the archive …/0a000000-…-0000000000a1/attachments was planned while a live
    process's cwd IS its container: the run's own attachments were judged by a
    question about the archive, not about the run
# RESTORED (verified against HEAD, not the index: `git diff HEAD --stat` → 34 insertions)
# GREEN — 3/3 PASS
```

`internal/scratch/sessions_scratch_dir_live_container_test.go` pins the
session-raw arm's behaviour that made candidate (b) false — a holder whose cwd IS
the container keeps the directory inside it, a holder with an **open fd on a file
inside** the container keeps it too, and the no-holder control is still planned —
so the falsification cannot silently become true. It drives the REAL code path
(`containerScratchDirEntries`, and the walk's own `containerEntries`) with the
holder index shaped exactly as the single `/proc` pass shapes it. **No production
change was needed there**: that arm's `heldIn(candidate)` is already
ancestor-aware, because `heldSet` indexes each holder's path AND its ancestors.

## Filed separately (Rule 41)

`gopi tool scratch-gc trees --explain` prints a line for every candidate it would
**remove** and **nothing at all** for what it keeps — so "kept, and why" is
unanswerable from the surface whose job that is. This cost this run two
instrumented probes whose empty output could have meant "protected" or "never a
candidate". Filed as a bug row with the evidence beside this document.

## Verdict in one line

The incident was a **spelling** (the agent's own 35-char uuid), not a sweeper —
but the investigation that had to be done to establish that turned up a real
defect one arm over, in the empty-archive case, and that defect is fixed, red-proved
and pinned.
