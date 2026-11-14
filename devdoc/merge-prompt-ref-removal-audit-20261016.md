<!-- doc-class: historical -->
# The merge child's ref deletion had no audit record — the LLM instruction file was the uncovered hole (2026-10-16)

## What was measured

`cmd/agent/merge_prompt.md` is the instruction file a `gopi merge` run hands to the
LLM child that does the merge. Its §3.6.1 disposition table told that child, in its
**action column**, to run a RAW git ref deletion on a spent branch
(`git branch -d <branch>` for cases ①②③, `git branch -D <branch>` for ④).

The child did exactly that (the run's own transcript carries the action cell and
the command). The consequence, measured:

- merge `41679b36e` (which landed `task/01a132edbfcb…`) was in `master`;
- the branch ref was gone;
- and **no host recorded a `kind=branch-delete`** line for it — the sink held zero
  such records across all three machines.

The contrast that makes it a *record* gap and not a *mechanism* gap: the same
family of rows, going through a production close-out arm, produced two
sha-bearing `branch-delete` records. So the machinery records removals it owns;
this removal went through a channel the machinery could not see.

## Why: the census said so, in a comment

`internal/standards/removal_audit_shell_gate.go` (the removal-audit census, which
fails the build on a raw ref deletion written in a shell script) carried this
paragraph about its own coverage:

> `cmd/agent/merge_prompt.md` teaches the same raw deletion in its Merger
> disposition table (~10 rows) and is NOT a carrier here — that table is a closed,
> separately gated surface … Both are filed as their own work item.

That paragraph was accurate, and it is the whole diagnosis: **the hole was known,
named, and left uncovered.** All nine production Go delete sites were already
audited; the instruction file was the one remaining bare channel.

A prose carrier is exactly as load-bearing as a script one. A command written in
markdown for a spawned process to run is a command that process will run — the
file only *looks* like documentation. The census's own scope rule ("a ref removal
written in a shell script") was keyed on the file's *language*, not on whether a
process executes it, which is why it missed the one carrier that is literally
written to be executed.

## The fix, and the shape it took

**One writer, everywhere.** `gopi task archive <branch> --reason <code>` already
does the three things a spent-branch removal needs: it mints `archive/<branch>`,
writes the companion reason ref (`refs/gopi/archive-reason/<branch>`), and removes
`refs/heads/<branch>` — and that last leg is `ArmArchiveReclaimBranch`, the arm
that emits the `kind=branch-delete` record. So the fix is not a new mechanism and
not a wrapper: it is the prompt's rows pointing at the verb that already existed
and already records.

Two properties of that verb make the rows runnable rather than decorative, and the
prompt now states both:

- it **refuses** a branch whose owning ledger row reads `completed`/`verified`
  without `--override-completed` — hence the flag on every row;
- it **refuses** (never forces) a branch a worktree holds, the integration branch,
  or one whose tag and ref name different commits. A refusal is a report.

**The raw form is an ABSENCE, not a prohibition.** The prompt was rewritten to
describe the forbidden path *without spelling it*. This is deliberate and it is
the same posture `mrgVoidedStateNamespacePrefix` already takes in this file: a
prompt that names a shape in order to forbid it still hands a model a runnable
command to imitate, and imitating it is precisely the measured failure. So the
spellings live only in `cmd/agent/merge_prompt_gate_test.go` (a `_test.go` file,
which is not a census carrier).

### What was NOT done, and why

- **No new script, no new tool, no new state file** (Rule 35). The writer existed;
  the gate existed; both were pointed at the hole.
- **No permit / baseline row.** A permitted silent ref removal is the defect this
  census exists to stop, so the fix is the funnel, never a permit.
- **§3.6.2's ban on `archive/<branch>` tags is not weakened.** §3.6.1's new prose
  states the distinction explicitly, because a reader who saw only the ban would
  read the fix as a contradiction: what §3.6.2 bans is a **hand-minted** tag used
  to *park* a branch ("keep it, decide later") — the shape that leaves the ref
  alive and re-asks who deletes it. The writer's tag is minted in the same call
  that removes the head ref, carries a recorded WHY, and is honoured by every
  merge path as "decided — never merge".

## Two gates, one artifact — why both were needed

The fix is covered from two sides, and neither alone is sufficient:

| gate | what it sees | what it alone would miss |
|---|---|---|
| `internal/standards` removal-audit census (`TestRemovalAuditGateShell…`) | the real tree: is a raw ref deletion written anywhere in a carrier? | it reports the ROUTED form as correct even when the raw one sits beside it in the same file — it is a census, not a contradiction check |
| `cmd/agent` merge-prompt gate (`TestMergePromptRoutesRefRemovalThroughTheOneWriter`) | the instruction itself: the writer verb is STATED **and** every raw spelling is ABSENT | it checks one file, by design; it is not a corpus |

The census's carrier list was widened from `scripts/` + `GOPI.md` + production Go
to include **`cmd/agent/*.md`** — the LLM instruction files — via
`removalShellIsInstruction` (ONE function, so the corpus hook and the red proof
cannot disagree about the file set, Rule 14). The floor was raised accordingly:
`removalShellMinInstructionFiles` counts them, and — because a count cannot
express "the file this half was written for" — `removalShellInstructionSentinel`
names `cmd/agent/merge_prompt.md` and the real-carrier test asserts it was
**actually scanned**.

## Falsification (Rule 11 — shown red before being trusted)

Three independent red arms, all on the REAL files:

1. **The predicate, on the pre-fix prompt text.** The shipped disposition row and
   the row it replaced are spelled once as
   `removalShellPromptRoutedRow` / `removalShellPromptRawRow`; splicing the old
   row back in and re-running the predicate reports it (test output:
   `RED on the real instruction carrier (row reverted): [{rel:cmd/agent/merge_prompt.md line:615 …}]`).
2. **The same splice through the sibling gate.**
   `TestMergePromptRoutesRefRemovalThroughTheOneWriter` fails naming the line when
   the raw cell is restored, and passes on the shipped routed row.
3. **The census, on the real tree.** Before this change the census reported
   `hits=0` while reading 1102 files; the carriers count in the real-carrier test
   now reads `1102 file(s) — 231 under scripts/, 869 production Go, 1 instruction
   ([cmd/agent/merge_prompt.md]), plus GOPI.md`, so the widened scope is visible in
   the output rather than inferred.

The prompt's own section gate (`TestMergePromptCoversEveryRequiredSection`) had
been *requiring* the raw spellings — `{"git branch -d", …}` and
`{"git branch -D", …}` were entries in its required-phrase table. That is the
contradiction stated openly: one test demanded the shape another test now forbids.
The required entries were replaced by the writer's verb, composed from
`CliVerbTask` + `CliVerbArchive` (Rule 14: a rename of either moves the prompt and
the gate together, or the gate fails).

## Evidence

### Ran (Rule 31 — the surface, really invoked)

Fixture repo + a private sandbox profile; the ROUTED verb against a real ref:

```
$ gopi tool sandbox create --label rule31 --task-id 01a142a79…
sandbox=/home/user/workspace/gopi-sandbox/sandbox-rule31-5b154e86 …

$ GOPI_HOME="$SB" gopi task archive task/probe-spent --reason superseded \
      --override-completed --repo /home/user/workspace/gopi-fixture-audit-01a142a7
archived task/probe-spent as archive/task/probe-spent
  reason: unowned
  owner:  (no task row tracked this branch)
  read back: git cat-file blob refs/gopi/archive-reason/task/probe-spent
  deleted          archive/task/probe-spent
```

Result — the ref is gone **and** the record exists:

```
after: GONE
--- sink branch-delete records: 1
{"version":1,"ts":"2026-10-16T09:27:06.215294346Z","kind":"branch-delete",
 "arm":"archive-reclaim-branch","op":"branch:delete","pid":79003,
 "repo":"/home/user/workspace/gopi-fixture-audit-01a142a7",
 "target":"task/probe-spent","branch":"task/probe-spent","evidence":"the archive tag …"}
```

That is the whole fix in one reading: **the routed form removes the ref and writes
exactly one `kind=branch-delete` record; the raw form it replaces writes zero.**
The `arm` field names `archive-reclaim-branch` — the arm the census's plumbing
vocabulary was already taught to classify.

### Gates

```
$ go build ./...                                             # OK
$ go test ./internal/standards/ -run 'RemovalAudit'           # ok  21.8s
$ go test ./cmd/agent/ -run 'MergePrompt'                     # ok   2.5s
$ go test ./internal/standards/ -run 'TestStandardGateDataReport' # ok
  removal-audit (… must route through the ONE archive writer): hits=0 baselined=0 escaped=0 NEW=0
$ go test ./internal/standards/ -run TestRemovalAuditGateShell -v
  carrier corpus: 1102 file(s) — 231 under scripts/, 869 production Go,
    1 instruction ([cmd/agent/merge_prompt.md]), plus GOPI.md
  RED on the real file (funnel reverted): [{rel:scripts/post-merge-cleanup.sh line:159 …}]
  RED on the real instruction carrier (row reverted): [{rel:cmd/agent/merge_prompt.md line:615 …}]
```

## A separate defect found while verifying (Rule 41) — filed, not fixed

`go test ./cmd/agent/ -run 'Merge'` is **red at the base commit**, for a reason
unrelated to this change:

```
--- FAIL: TestMergeEveryLoopWritesNoStateFile (2.74s)
    merge_every_test.go:573: the profile holds [.gopi/logs/removal-audit.log] —
        the command's contract is no state file, no pid file, no progress record.
```

Reproduced on a **detached worktree at `bc8fc75`** (this branch's own base), so it
is pre-existing and not introduced here. The two contracts now contradict each
other: the merge loop's own removal arms (`merge_run.go` 2184 / 2201 / 2305,
`removalAudit.BranchDelete`) append to `<profile>/.gopi/logs/removal-audit.log`,
while the loop's no-state-file contract walks that same profile and refuses any
file but the lock. Which side yields is a design decision on the system layer, not
a fix to make silently here — filed with the reproduction in
`sessions/01a142a7…/artifacts/bug-merge-loop-sink.md`.

`TestMergeEveryLoopWritesNoStateFile` is therefore expected RED in this branch's
run, and it is the only red in the names this change's packages touch.

## Not done

- The `--reason` code chosen for the disposition rows is `superseded`; the
  vocabulary itself is `gopi task archive -h`'s and was not extended. If a future
  case wants a distinct code (e.g. one naming a merge-run cleanup specifically),
  it belongs to that vocabulary, not to the prompt.
- The merge loop's own arms (`ArmMergeRunSpentBranch`) still exist in
  `merge_run.go` and are audited; this change is about the PROMPT's channel, not
  about removing the production arms. Whether the prompt should instruct the child
  to remove refs at all, or leave that to the run's own close-out, is a larger
  design question left open.
- An `eval`-assembled or string-split command remains invisible to any text scan —
  stated in the census's own comment, unchanged by this work.
