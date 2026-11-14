<!-- doc-class: normative -->
# The capability demo — internal edition, and the `--out` fix

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Class: normative. Why: it is the standing description of what
`docs/archive/demo/internal/` delivers, which findings each file carries, and which
claims the external edition must restate because of them — plus the contract of
the `gopi text2img --out` fix landed in the same commit.

Chain link `[3/9]` (row `capdemo-3`, branch `task/capdemo3`). Producer: link
`[2/9]`, the external edition (`docs/archive/demo/external/`, `devdoc/capability-demo-external.md`,
merged to master at `852ff9c0`). Consumer: link `[4/9]`, the annotation layer. The
findings this link's bundle carries come from
`devdoc/capability-demo-decomposition.md` (link `[0/9]`'s investigation) plus
this link's own measurements.

**Files this document is about** (live paths at the commit it lands on — the
classification is checkable, not asserted): `docs/archive/demo/internal/README.md`,
`docs/archive/demo/internal/ANCHORS.md`, `cmd/agent/cli_text2img.go`,
`cmd/agent/cli_text2img_test.go`.

## The bundle, file by file

| Path | What it is | Anchors it carries |
|---|---|---|
| `docs/archive/demo/internal/README.md` | the internal rendition: **10** sections (v2), every finding with its mechanism or its command | the internal→external map (§ table), the defect end to end, the mechanism findings re-verified at the newest head, the annotated walkthrough |
| `docs/archive/demo/internal/ANCHORS.md` | provenance index: generator, base commit, evidence paths, verbatim commands and outputs | the bundle's Rule-31 evidence |
| `docs/archive/demo/internal/stills/` | the internal edition's own stills (**v2**): the live window and the still the `gopi annotate` verb marked up | the walkthrough's pixel evidence |

Static and zero-dependency in the same strict sense the external bundle uses:
Markdown only, no server, no build step, no JavaScript.

### The bundle contract (a criterion, not a list)

- A finding **MUST** resolve to one of **four** anchor kinds: a still with a
  verbatim producing command, a census figure with its generator and base commit,
  a report path, or **a defect** (the command that failed, its exit status and
  stderr, plus the `file:line` that produced the failure). The fourth kind is
  this edition's reason to exist; a finding resolving to none of the four is not
  in the bundle.
- Every section of `README.md` **MUST** name the external section it feeds —
  the map is at the top of the file, so a reader can check the correspondence
  without opening the external bundle.
- Every figure **MUST** cite `bash scripts/gopi-stats.sh` as its generator and
  name the base commit it was measured at (Rule 38). No number is retyped from a
  previous run.
- The bundle **MUST NOT** claim a fidelity measurement it did not make. The
  glyph-fidelity gap is stated as a gap in three places (README §5, §2's
  omission line, and the ANCHORS index) precisely so it cannot be read as met.

## The internal → external section map

Stated in `README.md` as a table; reproduced here because it is the deliverable's
second acceptance criterion:

| Internal § | Feeds external § | What only the internal edition adds |
|---|---|---|
| 1 One binary, in numbers | §1, §7 | figures measured at **this** head; the two stderr lines the instrument still prints |
| 2 The eyes, driven | §2, §5 | first-hand runs; the byte-reproducibility result; the `--out` refusal before and after |
| 3 The surface is derived | §3 | verb/probe counts at this head; why a help-surface probe cannot find an argv/vocabulary defect |
| 4 The eyes are conditional | §4 | per-channel host verdicts re-measured here |
| 5 CJK, end to end | §5 | the chosen font chain; the explicit fidelity gap |
| 6 What refuses on purpose | §6 | the **two refusals this link added**, with their messages |
| 7 Defects and the fix | (corrects §2) | the defect, its mechanism, the fix, the red→green repro test |
| 8 The mechanism findings | (not in external, by construction) | the injected parent measured, the ledger's limits, the resident dispatcher |
| 9 Anchors | §7 | the four-kind anchor index |
| 10 The internal walkthrough | §2, §5 | the prose report and the annotated still side by side, with the pixels the verb changed measured |

The direction of information is one-way and is stated in both bundles: a
capability claim in the external edition is a **selection** from this edition's
measured findings, never an independent composition. When a finding here changes
a claim there, the claim changes there (the publish checkpoint, link `[8/9]`).

## The defect, the fix, and the repro test

### The defect

`gopi text2img <in> --out <out>` was refused:

```
$ gopi text2img /home/user/.gopi/sessions/capdemo-3/t2i-probe.txt --out …/prefix2-out.png
gopi text2img: unknown flag --out
exit=1
```

while `out` **is** the agent tool's own parameter name for that argument
(`internal/agent/loop.go:1659` declares it, `:6296` reads it). Root cause, one
level down (Rule 10.3): the CLI's flag vocabulary was derived from the *numeric*
rendering knobs only (`cmd/agent/cli_text2img.go:17-22`), and the one non-numeric
argument — the output path — was modelled as a positional alone
(`:195-198` `text2imgFlagMember`), so the membership check at `:125-127` refused
the tool's own word. The defect is the translation layer between two argument
models (the CLI's positional-of-two-arguments vs the tool's named parameters).

### The fix (same commit as this document)

- `--out PATH` is a first-class flag name: `text2imgFlagOut = "out"`
  (`cmd/agent/cli_text2img.go:30`), added to the membership predicate at `:264`.
- Both spellings fold into one output through `text2imgResolveOutput`
  (`:196-221`): flag alone → the flag; positional alone → unchanged; `--out`
  with an empty operand → refused naming the flag (`text2imgOutputEmptyMessage`,
  `:38`); both spellings naming the same path → accepted; both naming different
  paths → refused naming both (`text2imgOutputConflictMessage`, `:45`).
- **Help updated in the same commit**: synopsis (`:82`), a worked `--out`
  example (`:94-96`), the two-spellings paragraph (`:100-103`), the flags-table
  row (`:106`).
- Named constants only (Rules 1/13/14); numeric defaults still sourced from
  `internal/text2img`.

**Rejected alternative, recorded so it is not re-proposed:** accepting `--out`
as a silent alias that overwrites the positional. It removes the defect with
less code and introduces exactly one bad state — both spellings given, different
paths — where the command exits 0 and writes a file the user did not name. That
is the same defect class the `--out` gap belongs to (Rule 15).

### The repro test

`cmd/agent/cli_text2img_test.go`: `TestParseText2imgArgsOutFlag` (`:91`) and
`TestParseText2imgArgsOutFlagRefusals` (`:146`). Red on pre-fix behaviour with
the defect's own message, green after:

```
--- FAIL: TestParseText2imgArgsOutFlag (0.00s)
    cli_text2img_test.go:95: `a.txt --out rendered.png`: unexpected error: unknown flag --out
FAIL
```

The red run was produced by restoring the pre-fix **source from `852ff9c0`**
(not a hand-edit, and not `git stash` — this repo bans `git stash` across linked
worktrees, so the pre-fix file was materialised with `git show` and swapped in),
leaving the new test in place, and running it; then restoring the fixed source.

## Findings beyond the fix (each with its evidence)

1. **The `--out` defect was invisible to every help-surface instrument.**
   `gopi text2img -h` rendered correctly (exit 0) before and after; the flag
   simply was not on the page. `grep -c -- '--out'` on the pre-fix page → `0`,
   on the post-fix page → `4`. Generality: the census measures what is
   **printed** and **declared** (`scripts/gopi-stats.sh:758`), never whether the
   page and the parser agree — that disagreement only exists in argv.
2. **The eyes' non-determinism is in the LAUNCH, not the render.** Link `[2/9]`
   observed a first-try Chromium failure; driving `shot --self` twice here
   produced the **identical digest** as the still committed on master
   (`3bdfe2fb…a70b`, 33 764 bytes). Once a browser answers, the page and the PNG
   are the same. This is a stronger statement than the external edition makes,
   and only this edition carries it.
3. **The census prints two `Broken pipe` lines on stderr** (both runs, two each)
   — pre-existing, not introduced by link `[1/9]`'s, `[2/9]`'s or this link's
   edits. Reported rather than fixed: the census is another link's measured
   surface, and the fix would land on a file this link's acceptance does not
   cover.
4. **Mechanism: the chain's edge is real and the gate enforces it.** While this
   link was `in_progress`, its consumer was parked by name
   (`[queue-skip] parent capdemo-3 not released (status=in_progress; …)`) by
   `internal/tools/tasks.go:5816` (`taskParentGateDenied`), within `:5934`
   (`taskRowClaimable`) — so the chain is serial without any dispatcher knowing
   it is a chain.
5. **Mechanism: the ledger cannot express the true assembly shape.**
   `internal/tools/tasks.go:3803` (content immutable — cancel and re-submit) and
   `cmd/agent/cli_tasks.go:1444` (`--parent-task-id` is submit-only) mean a
   one-child-many-producers node is writable only by pre-allocating the
   consumer's id first, or by N single-parent INSERTs; and
   `internal/agent/reframe.go:460` makes a roadmap's auto-enqueue parent every
   item to the decomposer (a fan, never a chain).
6. **Mechanism: the dispatcher is resident only in a live session** —
   `gopi scratch-gc deps` shows resident gopi processes with `detached=no` and
   `scheduled=0`: "always a worker alive" is a property of the session that
   spawned it, not of the machine (Rule 24 — `PPID=1` would not have been
   evidence either).

## v2 — the extension landed by link `[7/9]` (same bundle, later head)

Link `[7/9]` (row `capdemo-7`, branch `task/capdemo7`) extended this bundle rather
than replacing it. Three changes, and one of them is a **correction**:

1. **The mechanism claim was re-verified line by line at that head** — the gate's
   file:line set is unchanged and still resolves (`README.md` §8.1's table), so
   the mechanism stands. What did not survive is one sentence: this document's
   finding 4 and the rendition's §8.1 described the edge as being read
   *backwards* for a decomposer→items tree. **The gate does not read it
   backwards.** `parent_task_id` means "the row that produced this row"; the
   predicate's semantic is "the producer must be released before the consumer may
   be claimed"; and a chain is exactly a producer→consumer order written the
   natural way (`internal/tools/tasks.go:5816`, release set `:5789`). The
   asymmetry is that the harness *auto-generates* only the decomposer→items fan
   (`internal/agent/reframe.go:460`, `:553`), so a chain — or a decomposition
   whose items must run in order — has to be written by hand. `README.md` §8.2
   draws both shapes and names which one this chain is; the phrasing here is
   corrected in the same commit so two documents cannot disagree.
2. **§8.5 is new: the chain's own execution, as ledger evidence.** Nine rows with
   `created_at` four seconds apart ran strictly serially over 2h12m, claimed by
   one resident worker (`peerbox:2211811`), with the two not-yet-released rows
   parked by name at every moment. It also records the **honest gaps** (the
   insertion order is not recoverable; `gopi worker <explicit-id>` was never used;
   one host, one dispatcher, one head).
3. **§10 is new: the internal walkthrough.** A real capture
   (`gopi capture --window`, `sha256_16 1e27ca078310ff84`) annotated by the real
   `gopi annotate` verb (L6) with a rect and an arrow, the prose report and the
   annotated still side by side, and the verb's `ink_pixels` (14538) confirmed by
   an independent PIL diff over the same two PNGs. Committed under
   `docs/archive/demo/internal/stills/` (`ANCHORS.md` §H).

What v2 did **not** do: it did not fix the defect the walkthrough points at. That
defect (`--out` advertised in `text2img`'s help, absent from the derived
inventory surface) is link `[3/9]`'s flag meeting the inventory a layer later, it
is outside this link's surface, and the honest disposition is a new task rather
than a widened diff (Rule 26).

## Not Done (honest)- **No glyph-fidelity (IoU) measurement.** The bundle states the gap in three
  places rather than papering over it: the census probe measures exit status,
  byte count, ink and composition, never accuracy, and the renderer's defaults
  are described as "OCR-validated" — an OCR score cannot see render quality loss
  (Rule 10.2).
- **~~No committed stills in `docs/archive/demo/internal/`.~~ (v2 changed half of this.)**
  §1–§7's three eye runs still land only in the session directory — their commands
  and digests travel in ANCHORS §A–C, not the PNGs. Rationale unchanged for
  those: the external edition owns the still-anchor role for the capability
  claims, and a second copy of a 33 KB PNG is a second place for one artifact to
  drift. **§10's two stills ARE committed** (`stills/10-defect-raw-window.png`,
  `stills/10-defect-annotated-walkthrough.png`) because there the image *is* the
  deliverable: an annotated still that lived only in a session directory could not
  be checked against the prose it annotates by anyone reading this bundle.
- **No pixel-level check that the image bytes are "good".** Digests prove the
  files are identical; nothing here says whether the pixels are correct. Named in
  ANCHORS §B as an explicit omission.
- **The two stderr `Broken pipe` lines are not fixed** (finding 3).
- **The external edition's surface paragraph is not updated here.** Link
  `[5/9]` owns external v2. This link verified that **no committed claim in
  `docs/archive/demo/external/` is falsified** by the fix: still 03 was produced with the
  positional form, which still works identically (four paths, one digest —
  ANCHORS §D), so the claim → anchor map there stays true; only a *description of
  the flag surface* would need the new row.
- **One worktree, no second ground, no reparenting.** Straightforward given Rule
  6a's `[Your worktree — ALREADY CREATED for you]` block, and recorded because
  the chain's own design depends on the parent edge staying as submitted.
