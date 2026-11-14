<!-- doc-class: normative -->
# CONFIG.md line references: a resolving anchor is not a useful anchor (convention)

**Class**: normative. **Measured on the MERGED tree** (this branch = `2692e0d` +
master's same-identity gate, merge commit `7fd79f628`). **Generator**: the census
script archived beside this run
(`~/.gopi/sessions/01a1318a-2113-75b0-8f07-58a5f576a7c9/artifacts/config-md-ref-census.py`),
re-runnable as `python3 config-md-ref-census.py .`, exiting non-zero while any
blank-landing reference remains.

## The defect

`TestConfigDocLineRefsResolve` asks whether a `file.go:NNN` reference RESOLVES —
whether the file exists and the line number is inside it. A line number inside a
file proves nothing about what is ON that line, so an anchor pointing at a blank
line passed green while handing the reader who followed it nothing at all. The
document is the configuration reference of record; an anchor that lands on
nothing is a confident misdirection, exactly the failure the gate around it was
written to end.

Measured on master before this change: **212 reference tokens, 8 of them landing
on a blank line.** (The same 8 were present at this branch's base `2692e0d`;
master's own 2026-10-13 change re-pointed anchors whose CLAIMED SYMBOL was wrong
and left all 8 blank-landing ones standing, so this change is complementary to
it rather than a duplicate.)

## The narrow cut, and why it is narrow

Every reference token — both the `path.go:NNN` form and the bare `:NNN`
continuation form (one population, per `configDocContinuationPattern`) — was
classified by what its target line IS:

| class | tokens, master before | tokens, after this change | anchors after | policed? |
|---|---|---|---|---|
| CODE | 129 | 134 | 118 | by the corpus's normal review |
| COMMENT | 68 | 71 | 68 | **NOT policed** — "point at the region" |
| BRACE (`{` / `}`) | 7 | 7 | 7 | **NOT policed** — "point at the block" |
| **BLANK** | **8** | **0** | **0** | **ENFORCED by this change** |
| UNRESOLVED | 0 | 0 | 0 | already enforced (`TestConfigDocLineRefsResolve`) |
| **total** | **212** | **212** | **193** | |

Distinct anchors total 193 (212 tokens − repeats: the same anchor may be cited by
more than one row, and the findings and the census are about ANCHORS, not
occurrences). The CODE/COMMENT counts RISE by 5 because re-pointing a blank
anchor lands it on a comment or a code line, which is the fix moving the number
rather than the document changing shape.

The COMMENT and BRACE classes are the doc's deliberate style: a `:100` pointing
at `// TaskSyncEnabled reports whether AUTOMATIC task-row exchange is ON.` names
the declaration region rather than one statement, and a `:1` pointing at a
package clause is the same intent. Whether a given comment target is the RIGHT
one for a sentence is a **prose judgement** — it needs the reference to carry a
claimed symbol to be checkable at all, and that is precisely the class master's
`TestConfigDocNameCarryingRefsDeclareTheirSymbol` now polices (16 references
carry a claim and are enforced there; the rest are reported as bare).
**Blank is not a judgement**: a line is empty or it is not. That is the whole
reason the enforced class here is blank-only, and the two unpoliced classes stay
visible as a counted, printed, documented gap instead of being quietly widened
into a rule nobody can defend.

## The convention this document states

The gate **fails the build** when a reference lands on a blank line
(`TestConfigDocRefsLandOnContent`), and this is the single source of truth for
how an anchor in CONFIG.md is re-pointed.

1. **Re-point by measurement.** For each blank-landing anchor, read the prose
   beside it, name the symbol it describes, `grep` that symbol in the cited
   file, and point the anchor at the line that CARRIES the claim. All 8 were
   re-pointed in this change (the pre-fix numbers are the ones the census above
   prints for master):

| doc line | reference now (re-pointed) | the line it reaches |
|---|---|---|
| 185 | `internal/agent/loop.go:3452` | `keepThinking := config.EnvBool("GOPI_KEEP_THINKING")` |
| 285 | `cmd/agent/chat_film.go:366` | `// evidenceHint is the one place a report points at the producer's output` |
| 421 | `internal/config/audio_engine_param.go:78` | `// pre-flight check, naming the knob, before any audio is opened.` (above `Endpoint`) |
| 580 | `internal/config/task_sync_switch.go:100` | `// TaskSyncEnabled reports whether AUTOMATIC task-row exchange is ON.` |
| 617 | `internal/tools/memory_sync_delta.go:277` | `self := os.Getenv("GOPI_SYNC_NODE")` |
| 623 | `cmd/agent/cli_tasks_worker.go:127` | `workerIdleExitsEnv = "GOPI_WORKER_IDLE_EXITS"` |
| 657 | `cmd/agent/cli_web.go:790` | `webLocalTrustEnvName  = "GOPI_WEB_LOCAL_TRUST"` |
| 814 | `internal/lineedit/colour.go:497` | `func PaletteSelection() string {` |

Two of the eight are inside the doc's comment-pointing style by design
(`chat_film.go:366`, `audio_engine_param.go:78`): the line the prose describes IS
the doc comment above the field. They are reached deliberately rather than by
accident — the difference between the two is a measurement, not a preference.

2. **Teach the gate the predicate.** `TestConfigDocRefsLandOnContent` runs a pure
   function over `(doc text, repo root)` — the same shape the rest of the file
   uses — with two named-constant read-floors (`configDocMinBlankScanTokens` over
   tokens, `configDocMinBlankScanAnchors` over DISTINCT anchors), because a
   fixture of repeated filler would satisfy a token floor while collapsing to a
   single anchor. `configDocBlankTargetPermits` is the reviewed permit table and
   is **empty** because every violation was re-pointed; an unexercised permit row
   is itself an error (`TestConfigDocNonBlankAnchorPermitsExistAndAreHonoured`),
   so the allow-list can only ratchet down.

3. **Keep the unpoliced classes counted.** `TestConfigDocRefClassCensus` prints
   the class table with the enforced/permitted/unenforced counts on every run,
   over the same token reader the predicate uses, so the unpoliced anchors are a
   number in the suite's own output rather than an omission.

## Red proof

`TestConfigDocGateRedProofBlankTarget` feeds the SAME predicate a poisoned
fixture (a blank-line anchor, DISTINCT content-bearing filler chosen by
measurement over the file's own lines so the read-floors are satisfied without
duplicates) and demands a red verdict; the control (the same anchor pointed at a
content-bearing line) is clean; the census files the poisoned anchor in the BLANK
class; both read-floor arms fire (too few tokens; repeated filler collapsing the
anchor count); and the permit arms show a reviewed row silencing the finding plus
the row becoming itself a finding once the document stops exercising it.

The real-document proof was observed on the merged tree, red then green:
`task_sync_switch.go:100` → `:99` in CONFIG.md made the gate name exactly that
anchor (`CONFIG.md:580 points at a blank line`), and the restore was verified by
sha256 (`sha256sum -c` OK) plus `git hash-object CONFIG.md` =
`git rev-parse HEAD:CONFIG.md`, with `git diff HEAD -- CONFIG.md` empty.

## Honest scope (what stays unpoliced)

- **The predicate reads the BACKTICKED form only.** A widened scan over every
  `path.go:NNN` mention (backticked or not) measures 213 tokens on the pre-merge
  document instead of 208, and the 5 extra were measured individually: all 5
  resolve and all 5 land on CONTENT (4 on a comment, 1 on a code line —
  `internal/agent/loop.go:2400`, `internal/agent/compact.go:386`,
  `cmd/agent/main.go:464`). So the narrow scan hides no blank-landing anchor, and
  the widened form is not obviously the better predicate — it would sweep prose
  citations into a doc-anchor rule.
- **75 comment+brace anchors are NOT policed** (68 + 7 of the 193). A reference
  that lands on the doc comment of the WRONG function still resolves green here;
  where the reference CARRIES a claimed symbol, master's same-identity predicate
  catches that (16 such references, enforced). Where it does not, the anchor's
  standing is existence-only, and this document says so rather than implying
  coverage.
- **The predicate is blank-only by design**, argued above; widening it to the
  comment class would enforce a prose judgement with a mechanical check.
- **A blank line can arrive by itself.** Adding a blank line above an anchor's
  target breaks the anchor; the gate catches it (red proof), and the doc is
  re-pointed in the same change as the code — the existing discipline for every
  anchor in this document.

## Follow-up: the anchor moves when the FILE grows above it (2026-10-13)

A Rule 41 bug row reported the gate red at cut point `0033040` for a **different**
anchor than the table above: `CONFIG.md:174` citing `cmd/agent/main.go:805`. On the
reporting ground that was exact (measured: `git show 0033040:cmd/agent/main.go`,
line 805 empty). By the time the row was executed the tree had moved, and the
defect had **drifted to its other citation**: `CONFIG.md:41` citing
`cmd/agent/main.go:486`, then a blank line. One anchor's point, two citations, and
the drift landed on one of them at a time — so a re-point that fixes only the
reported line leaves the gate red. Measure the BLANK class on the tree in front of
you; never re-point from the bug text.

**The cause is the ordinary one, and it is worth naming**: `CONFIG.md` was written
at `422ec569c`, where `cmd/agent/main.go:486` WAS
`base, sErr := config.FromSettingsFile(settingsPath)` — the anchor was correct
when written. The file then grew above that region (the SIGPIPE block, the
`BootstrapEnvFromArgs` hoist), the read moved to `:706`, and the line the anchor
now named became empty. Nothing re-pointed it, because no surface measures an
anchor that still RESOLVES. That is exactly the blind spot this gate closes, and
the residue it cannot: the drift is silent until the target becomes blank —
a line that moved and left a NON-empty line behind still passes.

Both citations were re-pointed to `:706` (the measured `FromSettingsFile(settingsPath)`
line), census went BLANK 1 → 0 on the real document, and the red proof was
observed on the real doc: `:706` → `:486` in the working tree made the gate name
`CONFIG.md:41 points at a blank line`, and the restore was verified by
`git hash-object CONFIG.md` = `git rev-parse HEAD:CONFIG.md` with
`git diff HEAD -- CONFIG.md` empty.

### The residue this gate cannot see, MEASURED (same day, round five)

The paragraph above names the blind spot in prose ("a line that moved and left a
NON-empty line behind still passes"). It is not hypothetical — the same
`cmd/agent/main.go` region carries **three more anchors in exactly that state**,
all green, all wrong:

| doc line | anchor was | its target line AT THE COMMIT THAT WROTE IT | target line now | re-pointed to |
|---|---|---|---|---|
| 174 | `main.go:805` | `os.Getenv(config.PromptEnvVar)` (code) | `// attached natively (vision model sees…` (comment) | `main.go:1016` |
| 111 | `main.go:478` | `os.Setenv(config.GopiDirEnv, overrides.GopiHomePath)` | `// Top-level \`gopi | head\` semantics…` (comment) | `main.go:698` |
| 911 | `main.go:464-523` | the `FlagOverrides` → `GOPI_SETTINGS` block | `[]` / `//` (blank-ish, off by ~220 lines) | `main.go:684-743` |

The measurement that makes each one a defect rather than a preference is the
**writing commit**: `git show <commit-that-wrote-the-anchor>:cmd/agent/main.go |
sed -n '<N>p'` prints the line the prose describes (`8dd4ce793` for `:805`,
`422ec569c` for `:478` and for the block). All three drifted for the ordinary
reason — the file grew above them — and all three passed every gate, because a
BLANK target is the only drift the enforced class can name.

**Two of these are in a form the gate does not even scan**, and that is worth
stating as a measured limitation rather than a defect: the range spelling
`` `path.go:NNN-MMM` `` does not match `configDocRefPattern`
(`` `([A-Za-z0-9_./-]+\.go):(\d+)` `` — the closing backtick must follow the
digits), so `:464-523` contributes **no token at all** to the census. Probed
directly: rewriting it to `main.go:9999-99999` leaves the gate green. A range is
therefore **unpoliced in both directions** — neither resolvability nor blankness
— and was fixed here by hand, per the doc convention (re-point by measurement),
not by the predicate.

Census after the re-point: **195 anchors, CODE=120 COMMENT=67 BRACE=8 BLANK=0
UNRESOLVED=0**. All four anchors that name `cmd/agent/main.go` in this document
(`:706`, `:698`, `:1016`, `:684-743`) were verified against the line they reach,
one by one, rather than against a count.

