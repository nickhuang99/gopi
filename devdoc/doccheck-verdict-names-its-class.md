<!-- doc-class: historical -->
# devdoc: a doccheck red names its cause — the stored-cell verdict line

**Status:** landed on `task/01a13b7145417746b1e274d5d2d700b2`, class `historical`
(a past run's record); **re-derived on `task/01a1422c297f764397c4624dd7720530` on
top of master `942b3ac`**, which had independently grown a machine-readable exit
vocabulary in the same region. **Tree it was written at:**
`4747f09fc2c4113f34913dbb1aa29f630f4f2ede`; **tree it was re-derived at:**
`942b3ac6ad27dd551080e726896d227875565e59`.

## The defect row, and what the measurement said about it

The row was filed from a screen on another branch's worktree
(`01a136cff3cf7c8b959bd1440fb22c0f`):

```
doccheck_rc=1
not_permitted=0        # grep -c 'NOT PERMITTED'
stale=0                # grep -c 'STALE PERMIT'
doccheck: 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
doccheck: this run exits 1 on stored-cell drift (the manifest's stored text is not what
          tools/docclass/classify.py renders today), independently of the 0 gap(s)
```

The epic asked for one of two verdicts: **(a)** the stored-cell leg is a third gap
class that is merely not counted or printed (an output defect), or **(b)** the
class IS printed and counted and the exit is correctly attributed, in which case
the "every counter reads zero" premise is stale.

**The measurement says (b), and the number that decides it is `2531`.** The verb
was executed on this run's own ground, verbatim, with a binary built from that
ground's HEAD:

```
$ <ground>/gopi-doccheck tool doccheck --repo <ground>     # binary built from HEAD 4747f09fc
doccheck: 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
doccheck: stored-cell drift (the manifest's stored text vs what tools/docclass/classify.py renders today):
  doccheck: stored-cell drift: 0 of 2531 rendered row(s) whose stored text is not what the classifier renders today
  doccheck: stored-cell drift: none — every rendered row matches the manifest byte for byte
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
RC=0
```

Three independent facts, each one measured rather than read:

1. **There is no unattributed red at this ground: the verb exits 0.** The
   `exit 1` in the defect row belonged to a tree whose stored cells genuinely
   drifted (the leg's own count line said so). Between that tree and this one the
   corpus was re-derived (`classify.py --only …`, Rule 36's ONE writer), so the
   third question is now answered `0 of 2531`.
2. **The class IS printed and IS counted.** The failing leg prints a section
   header, a count line carrying the population (`N of 2531`), a bounded echo of
   the drifted rows, and a closing verdict line naming the class. The row's
   "every counter reads zero" is a statement about the counters a `grep` was
   pointed at (`NOT PERMITTED`, `STALE PERMIT`, `gap(s)`) — those are the FIRST
   leg's counters, and the third leg's counter is not one of them. A reader who
   greps only the first leg's spelling concludes "no counter fired" from a screen
   that carries a different leg's counter three lines later.
3. **The exit was correctly attributed**, because the closing line names the
   class verbatim — the same string the leg RETURNS.

So the bounded repair is not "make the class appear" (it appears), it is the
one the epic named for this branch: **make the property machine-checked, so a
future leg cannot reintroduce an unattributed red.**

## The hole the guard closes

The report arm named its failing legs by writing one `if` block and one print
beside each of them:

```go
if driftClass != "" {
    fmt.Printf(docCheckReportDriftVerdictFormat + "\n", driftClass, len(gaps))
}
if storedClass != "" {
    fmt.Printf(docCheckStoredDriftVerdictFormat + "\n", storedClass, len(gaps))
}
```

Two hand-written blocks, over two hand-written literals with the same text. The
arm asks its questions in sequence and returns `1` if ANY leg answered
(`gaps || drift || stored`) — so a leg can return a failing class, make the exit
non-zero, and be named by nothing, unless whoever added the leg also remembered
to add the block. That is precisely the shape the row was filed about, and no
test in the suite could see it: each leg's predicate was correct, and each leg's
own test asserted the RETURN VALUE, not the screen.

## What landed

`cmd/agent/cli_doccheck.go` (the verb, not its predicates):

- `docCheckVerdictLineFormat` — ONE spelling of the closing sentence for EVERY
  leg, **and BOTH facts are ARGUMENTS**: the CODE (`%d`, first) and the CLASS
  (`%s`, second), then the gap figure. The code argument is what lets the same
  sentence report master's own exit vocabulary — `docclass.DocclassVerdictForClass`
  maps an abstention to `DocclassExitNotMeasured` (4) and a finding to
  `DocclassExitFinding` (1), so a typed `1` in the format could state a status
  the run did not reach (task 01a136d7). The per-leg constants remain as NAMES
  (`docCheckReportDriftVerdictFormat` and `docCheckStoredDriftVerdictFormat` are
  both aliases of it) so this file keeps its vocabulary and no caller re-spells
  the sentence (Rules 13/14).
- `docCheckVerdictLines(classes ...string) []string` — a pure function answering
  the ORDERED classes a run exits 1 on, empty entries dropped. The report arm
  prints one verdict line per element, so **naming a failing leg is a property of
  the loop rather than of remembering to write a block**: a fourth question added
  later is named by adding its class to that call, next to the arms.
- The fix arm's third-leg close now prints the SAME shared format (it previously
  carried the second spelling).

### What the merge with master `942b3ac` had to keep from BOTH sides

The rework was re-derived on top of a master that had grown, in the same region:

| master's channel | what it is | why it survives |
|---|---|---|
| `docclass.DocclassVerdictLine(name)` | the machine-readable `verdict: <name>` line | a script greps it; the exit status and this line must not disagree (Rule 15) |
| `docclass.DocclassExitFinding` / `…ExitNotMeasured` | the ONE exit vocabulary (1 / 4) | the census generator reports the same question through the same mapping |
| `docCheckVerdictForClass` / `docCheckVerdict(class) int` | the verb's one seam onto that mapping | it is what turns "which class" into "which exit" |

The re-derivation keeps all three AND the branch's intent: the shared format
takes the code as an ARGUMENT (`docclass`'s mapping, never a typed literal), the
report arm prints ONE line per failed question through the loop, and every arm
still closes with `docCheckVerdict(...)` so the `verdict:` line and `$?` stay two
channels for one verdict. Master's `if/return` chain after the loop is kept for
the exit VALUE (a drift class abstains with 4, a stored-drift class is always a
finding, the gap arm is a finding) — the loop names the causes, the chain decides
the status, and they cannot disagree because both read the same classes.

**The exit CODE did not move for any arm**: measured on eleven arms (clean, gap,
anchor drift, `--anchors` alone, `--check --anchors`, stored-cell drift, three
`--fix` arms, no-corpus) with master's binary and the reworked binary on identical
fixtures — identical `rc` everywhere. The reworked screen is a SUPERSET of
master's: where master's arm returned after the first failing question it
reported, the loop also names the second (`anchor-check` prints the anchor class
AND the stored-cell class). That is the epic's intent — a red names EVERY cause
it stands on — and it is an added line, never a dropped one.

`cmd/agent/doccheck_verdict_class_guard_test.go` (new) drives the **real arm**
(`docCheckReport`) and asserts on what an operator sees — the exit code and the
printed lines:

- `TestDocCheckVerdictNamesTheStoredCellClass` — a gap-free corpus whose ONLY
  failing question is the third one (the leg's report is stubbed through the
  seam `docCheckStoredDriftReport`, which exists for this and nothing else): the
  exit is non-zero AND the screen carries
  `fmt.Sprintf(docCheckVerdictLineFormat, <the class the leg returned>, 0)`.
- `TestDocCheckVerdictNamesTheAnchorClass` — the other door into the same
  status, on the arms fixture (0 gaps, one live anchor row, one permit covering
  nothing) with the third leg stubbed CLEAN, so the class on the line can only
  be the anchor leg's.
- `TestDocCheckVerdictLinesAreTheArmsAnswer` — pins the one definition (the fix
  arm's spelling IS the report arm's) and the ordering/emptying of the list.

**The assertion is the rendered LINE, not the class name**, and that is
load-bearing: `stored-cell drift` also appears on the section header, so a
substring test on the class alone would pass over a verb that printed the
section, exited 1, and named nothing. The rendered line carries the class, the
gap figure and the phrase a reader skims.

## The red proof

The guarded production line is `docCheckVerdictLines`'s filter. Mutating it to
drop every class (`_ = class`, returning an empty slice) leaves every leg's
predicate untouched and every leg's own test green — and turns the guard RED,
because the arm then exits 1 with no verdict line. Restore is verified against a
**revision**, not an index-relative diff:

```bash
test "$(git hash-object cmd/agent/cli_doccheck.go)" = "$(git rev-parse HEAD:cmd/agent/cli_doccheck.go)"
```

## Why the guard is not where the earlier proofs are

The third leg already carries an arm-level red proof
(`doccheck_stored_drift_verdict_test.go`) — for the leg printing a measured
count and then RETURNING the clean value. This guard is about the property one
level up: the ARM's exit versus the ARM's lines, across ALL legs. Both live in
`cmd/agent` because that is where the arm lives; a predicate test in
`internal/docindex` cannot see either.
