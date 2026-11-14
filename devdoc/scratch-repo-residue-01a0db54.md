<!-- doc-class: normative -->
# What a run writes when nobody gave it a destination — the `<repo>/scratch/` residue of 2026-09-26

**Class:** normative — the landed ruling (option 1: a repo-relative `scratch/` is
refused by `internal/standards/scratch_gate.go`, which cites this file) plus the
historical record below of the run that diagnosed it and the repair on the HP main
checkout.

**Reclassified 2026-09-26, by the manifest's own machinery** (not by hand): while
this file was only a record its row read `historical` and it opened with the
`<!-- doc-class: historical -->` banner. Once the gate class landed and began
citing it, the row was recomputed as `normative` — a standing claim about master —
and the banner was removed, because `docclass.ClassBannered` is true for the
historical class only. The history below is unchanged.
**Reporter:** task `01a0dbe4-ac93-74f6-8afe-e740669c9532` (operator: nick, HP-Laptop).
**Point-in-time record:** this doc is the *reference half* of the repair. The product
fix is dispatched as a child task (see "Where the product fix goes"); this file is what
a future agent reads so the diagnosis does not have to be re-derived.

## The complaint

`git status` in the HP main checkout `/home/user/work/GoPi` printed three untracked
directories and nothing else:

```
?? scratch/replay-01a0db54-audit/
?? scratch/t2i-verify-01a0db54/
?? scratch/text2img-ink-01a0db54/
```

91 porcelain entries, 161 files, **135 MB**, `10:01–10:19` on 2026-09-26 — written into
the one tree every other run reads: the `repos` table's merge target
(`/home/user/work/GoPi`, `internal/...` — CONFIG.md "repos"), whose branch
`master` is what `git pull peer1` updates.

## Attribution (measured, not inferred)

The commit that introduced all three paths is `995ea0e7`, whose author is the run of
task **`01a0db54-72c9-75f0-918c-91f81da83757`** ("fix gopi text2img so it either
renders the text or FAILS LOUDLY"). Proven three ways:

1. **The ledger.** That row's `deliverable` is `{"repo":"/home/user/work/GoPi"}` — the
   main checkout, recorded verbatim.
2. **The transcript.** `~/.gopi/sessions/01a0db54-…/68d910be-….jsonl` carries
   `mkdir -p /home/user/work/GoPi/scratch/t2i-verify-01a0db54`, plus every build,
   `--out`, patch and report path under the same root.
3. **The run's own reasoning, quoted.** It knew the tree was shared and wrote down why
   it went there anyway:

   > "`/dev workspace` in this repo (devdoc/tmp-ban-scratch-root.md), so all evidence
   > lives under /home/user/work/GoPi/scratch/text2img-ink-01a0db54/"

   and, three lines later, the self-doubt:

   > "here's a task about worktree runs polluting main checkout. Best: move my evidence
   > out of /home/user/work/GoPi/scratch into the task worktree? But the devdoc says /tmp is …"

The three sibling directories have the same shape and the same author row: `text2img-ink-`
(authoring evidence), `t2i-verify-` (independent verifier), `replay-` (audit).
**Three runs, one invented root** — because the root is not a rule they could follow,
it is a directory each of them had to guess.

## Root cause — the chain, in order

1. **`deliverable.repo` was set to the shared checkout.** `gopi task update --repo
   /home/user/work/GoPi` (the row's own transcript records it). The task's *work* was a
   branch in `~/workspace/gopi-t2i-01a0db54`; the *repo field* said main checkout. Two
   different things were spelled with one field.
2. **The briefing rendered that field as the run's repo.** A later session's briefing
   carried the literal line `Repo: /home/user/work/GoPi`. A line that names the shared
   checkout is an invitation: the run is told the repo is *there*.
3. **No destination for evidence exists.** Rule 22 says evidence is not written to `/`
   and not into a source tree a `git add` can capture; the operator's standing ruling
   (2026-09-23) is *no reaper, no clock* (`briefings/brief-scratch-lifetime-owned-by-writers.md`).
   The suite scratch root is `gopi scratch-gc`-managed and is walked by the merge/verify
   path; `gopi sandbox create` is a private *profile*, not an evidence directory. So the
   run had: a prohibition, no destination, and 135 MB to write.
4. **So it invented `<repo>/scratch/`** — a name that does not exist in the repo's
   vocabulary (`git ls-files` has no `scratch/` tree; the only `scratch/` is that
   residue, plus three tracked files from 09-01). The ground announcement
   (`internal/tools/task_run_ground.go`, `task_ground_advisory.go`) teaches a **fence**
   ("your worktree is the ONLY tree this run writes") and never teaches where evidence
   *does* go, so invention is the only available move.
5. **The existing guard stayed silent — for a reason worth keeping.** The main-checkout
   arm (`internal/tools/task_main_checkout_deposit.go`, landed `76601678` 09:06 that
   same morning) runs `git status --porcelain` on the shared checkout **only for a row
   whose briefing NAMES it**, and this row's *own* text does not: measured,
   `payload LIKE '%work/GoPi%'` = **NO** (3238 B payload). It names `/tmp/t2i-test` and
   asks for "the two PNG artifacts". Its own prohibition, "goes there, under a path
   relative to it", is in the ground announcement; the main checkout is named in a
   *different* run's briefing. So the arm's precondition was false by construction — it
   was built for a row that points the run at the shared tree, not for a row whose
   **`deliverable.repo`** is the shared tree while its briefing is silent.

**The falsified hypothesis, recorded so nobody re-walks it:** I first assumed the row's
briefing named the main checkout (the arm's docstring describes exactly that shape). It
does not, and that is the finding — the same defect now has a second, unguarded spelling:
*`deliverable.repo` == the shared checkout*, which no arm measures at all.

## The repair performed (this run, HP main checkout)

Order: **archive → delete → prove the pull's real blocker** (never delete first).

1. **Archived** all three trees to the owner row's own session directory, outside every
   checkout (Rule 22's criterion is off-`/`; `/` and `/home` share device `66307`, so
   *within* `/home` the binding constraint is "outside a source tree", which the session
   dir satisfies — it is also where the row's own `task-report.md` already lives):
   `~/.gopi/sessions/01a0db54-72c9-75f0-918c-91f81da83757/residue-archive-scratch-20260926/`
   — 88 files, 57 MB, plus `EXCLUDED-BINARIES.sha256` for the seven rebuildable
   binaries (their provenance — `PREFIX_COMMIT_master.txt`, `POSTFIX_COMMIT.txt` — is
   *included* in the archive).
2. **Deleted** the three trees from the shared checkout.
3. **Measured** the result: `git status --porcelain` in `/home/user/work/GoPi` is now
   **empty**.

### What the cleanup did NOT fix, stated plainly

**Superseded — the second blocker was then removed too** (same run, 12:17). The
paragraph below is kept as the diagnosis that led there; the resolution follows it.

The leftover abort was `git pull peer1` → `fatal: Not possible to fast-forward,
aborting.` — `pull.ff only` with `master` diverged **13 local-only / 11 remote-only,
0 shared patch-ids**. Not residue (tree clean), not a conflict (`git merge-tree` → 0
conflict markers), but four files both sides touched (`cmd/agent/chat_once.go`,
`chat_stream.go`, `cli_chat.go`, `devdoc/DOC-INDEX.tsv`) — a content decision, plus
`devdoc/DOC-INDEX.tsv` is a generated manifest two merges in a row left malformed
(`0585a823`, `c4ee769b`).

### The resolution: the divergence was duplicate landing, and it was already recoverable

`git branch -r --contains` on each local-only commit was the decisive measurement:
**14 of the 16 are already on peer1 as `peer1/feat/chat-status-row-degradation-test`**
(`995ea0e7` — the text2img fix — first among them). Realigning `master` onto the peer
therefore loses nothing from those 14; the remaining two (`031edce0` "the
non-interactive face reads REPL lines", `40a01655` its report — the whole
`chat-once-scripted-repl-lines` work) are **preserved on `wip/master-local-only-20260926`**,
a local-only branch whose tip equals the old `master` tip (`40a01655`, reflog
`branch: Created from 40a01655`, `12:15:41`). That is the Rule-9 archive shape: the
path walked is kept, nothing is force-recovered.

```bash
git reset --hard peer1/master   # after proving the above; HEAD<->master b4a5f5b1 preflight
git pull peer1                  # → fast-forward f0db74ba..779761f4, rc 0; second run: "Already up to date."
```

End state, measured: `master` = `peer1/master` = **`779761f4`**, `git rev-list
--left-right --count master...peer1/master` = **0 0**, `git status --porcelain` **0
lines**, and `go build ./...` rc 0 (the merged tree compiles — no `DOC-INDEX.tsv`
conflict markers leaked into a `.go` file). The `wip/` ref still resolves to
`40a01655`.

**What the realignment deliberately did not decide:** whether the two
`wip/`-only commits *should* land on peer1 — that is a content decision for the
operator/peer1 (Rule 28: the chat surface is feature work, HP's shape; landing it is a
review step, not a merge step). What is settled is the machine's own complaint: the
shared checkout is aligned, clean, and pullable.

## Where evidence belongs TODAY (so the next run does not invent a root again)

The diagnosis above explains *why* a root was invented; this is the answer it lacked.
Until the ground announcement renders a destination (the product fix, above), a run that
needs an evidence directory has these two, both already in the machine's vocabulary —
**neither is `<repo>/scratch/`**:

1. **This run's own session directory** — `~/.gopi/sessions/<task-id>/` (the `run_dir`
   field on the row). It is outside every checkout, so no `git add` can capture it and no
   `git status` sees it, and it is the directory the row's own `task-report.md` already
   lives in. Measured this run: the 135 MB residue archived there (88 files, 57 MB)
   changed nothing in any worktree.
2. **A sandbox** (Rule 34) — `gopi sandbox run --label <name> --task-id <id> -- CMD` when
   the evidence is a *program that must execute* in an isolated profile; it is created,
   owned and reclaimed by the machine instead of by a guess.

The test that distinguishes a legal root from an invented one is **not** its spelling:
it is whether the path is inside a git checkout (`git rev-parse --show-toplevel` from
inside it fails ⇒ outside ⇒ safe) — the same criterion Rule 22 applies to `/`.

## Where the product fix goes (dispatched, not done here)

The remaining gap is system-layer (Rule 28 → peer1: merger/worktree/ledger paths), so
this run **filed** it rather than editing those paths on HP. The child task carries:
which arm is silent and why, the two candidate signals (`deliverable.repo` == the main
checkout; directory-name match against the repo's basename, since `scratch/` is nobody's
to create), the missing half of the teaching (render the *allowed evidence root* in the
ground announcement — a destination, not another fence), the measured incidence
(01a0db25's four probe rows; 01a0db45's arm record: `completed-unclean-main-checkout-deposit`;
this 135 MB recurrence, all on the same day the arm landed), the explicit
**non-goals** (do not touch `.gitignore` — operator ruling "ignore rules hide writers";
add no reaper or timer; do not reuse the sandbox as an evidence directory), and the
acceptance: a row shaped like `01a0db54` makes the arm speak, red-then-green.

## Follow-up, same day (2026-09-26): option 1 landed — the invented root is untracked and the gate now refuses the class

The dispatch above named two candidate fixes. **Option 1 landed** — untrack the
three indexed paths that made `<repo>/scratch/` read as an existing convention,
rewrite the seed that produced them, and teach the repo-root junk gate the class.
Option 2 (leave the index alone, teach the *ground announcement* a destination)
was **not** taken: it is the product fix dispatched in the section above, it is
still needed, and it alone would never have removed the precondition. The two are
not alternatives for the same slot — option 1 removes the *look* of a convention,
the announcement fix removes the *need* for one.

**Why option 1, in one line**: the residue's causal chain starts at "a run saw
`scratch/` in the index and read it as the destination this repo uses"; a fence
that only tells a run where NOT to write leaves the false licence in place, while
untracking removes it at the source — and the gate makes its return impossible.

What landed, in this one commit:

1. **The three paths are out of the index**, one `git rm --cached` per path
   (Rule 8): `scratch/payload.md`, `scratch/expected.md`, `scratch/run-clean.log`
   — the round-trip probe artefacts of commit `6cf7d1ef` (2026-09-01). `git
   ls-files scratch/` is now empty. **The files stay on disk** (untracked, not
   ignored, not deleted): they are the recorded evidence of that probe, and
   deleting tracked content is not what untracking means. The one with a live
   producer — `payload.md` — is regenerated by the seed, whose path moved out of
   the checkout.
2. **No `.gitignore` pattern was added, deliberately** (the operator's standing
   ruling in the dispatch above: *"do not touch `.gitignore` — ignore rules hide
   writers"*). The new gate class is **index-only**, so an untracked `scratch/`
   keeps showing as `??` in `git status` — which is exactly the half Rule 30
   reads (`completed-unclean`), and the diagnosing signal this record was built
   from. An ignore rule would have silenced it.
3. **`testdata/gopi-special-chars-input.md` rewired**: the prompt now resolves
   gopi's machine-owned temp root (`$TMPDIR`, default `<home>/workspace/_tmp`,
   off `/` per Rule 22 — the knob's single source of truth is `CONFIG.md`'s
   `tmpdir` row / `config.ResolveTmpDirConfigured`) and names
   `$TMPDIR/gopi-special-chars/payload.md`, saying in the prompt not to create a
   `scratch/` inside the checkout. The special-character content lines are
   byte-identical: the assertion (exact content, read back) is unchanged; only
   the destination is different. Note the seed had **no** in-repo consumer
   (`git grep gopi-special-chars` → no production hit); it is an operator-driven
   probe prompt.
4. **`internal/standards/scratch_gate.go` learned the class** (7th entry in
   `junkPathChecks`, path-component anchored via `hasPathPrefix(names,
   repoScratchDirName)`; the name is one const, not a literal — Rule 13/14) with
   the red-proof extended in `scratch_gate_test.go` (+4 poisoned, +5 legit
   lookalikes) and a **live red→green run on the real index**, observed in both
   directions:

```
$ printf 'red-proof\n' > scratch/redproof-probe.md && git add scratch/redproof-probe.md
$ go test ./internal/standards/ -run TestRepoRootHasNoTrackedJunk
--- FAIL: TestRepoRootHasNoTrackedJunk
    tracked scratch/junk paths in the repo index (1):
      scratch/redproof-probe.md → a repo-relative `scratch/` tree. gopi's scratch root is `tmpdir` …
$ git rm --cached scratch/redproof-probe.md && rm scratch/redproof-probe.md
$ go test ./internal/standards/ -run TestRepoRootHasNoTrackedJunk
ok  	github.com/nick/gopi/internal/standards
```

The same probe caught a **fixture bug of my own**: the first "legitimate
lookalike" list carried `scratchpad-notes.md`, which the *pre-existing* root
parking-lot class refuses (a root `.md` that is not GOPI.md/CHANGELOG.md/CONFIG.md)
— the gate was right and the fixture was wrong, so the lookalike is now
`scratchpad-notes.txt`. Recorded because it is the same lesson as the residue
itself: a plausible-looking path is judged by its class, not by its spelling.

**What option 1 does NOT fix** (honest, Rule 26): a run can still *create* an
untracked `scratch/` inside a checkout — the gate only refuses it into the index.
The announcement fix above remains the other half.

## Evidence (self-filed)

- Attribution: `sqlite3 ~/.gopi/memory.db "select deliverable from tasks where task_id
  like '01a0db54%'"` → `{"repo":"/home/user/work/GoPi"}`; `git log -1 --format=%s 995ea0e7`
  → `fix(text2img): ordered glyph-fallback font chain + empty-render guard; ink-count tests`.
- The arm's silence: payload does **not** contain `work/GoPi` (`does NOT name it`,
  `length 3238`); the only pre-existing arm record in the ledger is
  `01a0db45-… completed-unclean-main-checkout-deposit` (a *different*, probe-shaped row).
- Cleanup: archive 88 files / 57 MB at the path above; `EXCLUDED-BINARIES.sha256`
  written; `git status --porcelain` → 0 lines after deletion.
- The pull: `git pull` → `fatal: Not possible to fast-forward, aborting.` (rc 128);
  `git cherry -v peer1/master master` → 13; `git cherry -v master peer1/master` → 11;
  `comm -12` of the two patch-id sets → 0; `git merge-tree` → 0 conflict markers.

### Second follow-up, same day (12:3x): the manifest gate was RED on master, and this branch closes it

Re-anchoring the record onto master surfaced a defect **master itself carries**,
measured with master's own test and master's own file (extracted with
`git archive master | tar -x -C <session-dir>`, so no checkout was touched):

```
$ go test -count=1 ./internal/standards/ -run TestDocManifestCoversEveryDevdocFile   # tree = master 779761f4
--- FAIL: TestDocManifestCoversEveryDevdocFile
    devdoc/gopi-chat-pipe-back-driver.md: no row in devdoc/DOC-INDEX.tsv — every file under devdoc/ must be classified
    docclass_gate_test.go:130: devdoc/DOC-INDEX.tsv: 2419 rows cover 2420 files under devdoc/ (classes: map[historical:1863 index-meta:1 normative:543 roadmap:12])
```

**Root cause**: `d1e6422a` (the `--turns` pipe-back driver) added
`devdoc/gopi-chat-pipe-back-driver.md` **without its row and without its class
banner**; the merge that landed it
(`f0db74ba Merge orphan task/01a0db61 …`) carried neither, and nothing on the merge
path runs this gate — so master has been red on its own truth-class manifest since
that merge, with two independent failures (coverage, and the banner gate that
follows from it).

Repaired here, because the record cannot land on a tree whose gate is red and
because both halves are the same one-file defect:

- `devdoc/gopi-chat-pipe-back-driver.md` — the `<!-- doc-class: historical -->`
  banner as its first line (nothing else in the file changed: Rule 4, a
  one-string edit).
- `devdoc/DOC-INDEX.tsv` — **two** rows: that doc (historical, placed after
  `devdoc/gopi-chat-mode-effort.md`) and this record (historical, after
  `devdoc/scratch-lifetime-owned-by-writers.md`).

Green, measured on this branch:

```
$ go test -count=1 ./internal/standards/ -run 'TestDocManifestCoversEveryDevdocFile|TestDocManifestRowsAreWellFormed|TestDocClassBannerMatchesManifest'
ok  	github.com/nick/gopi/internal/standards	5.653s
```

The red→green transition was observed in both directions: with the row removed the
coverage gate fails naming the file (the state master is in), and with the row
present it passes — the same two tests, the same tree, one line different.

**Note for the merger**: this branch is now *exactly* `master` + these four file
changes (`devdoc/gopi-chat-pipe-back-driver.md`, `devdoc/DOC-INDEX.tsv`,
`devdoc/scratch-repo-residue-01a0db54.md`), one commit, no code. It cannot conflict
with the pre-master chat lineage the old branch carried, and it is the fix that
makes master's own `internal/standards` gate green again.
