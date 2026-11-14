<!-- doc-class: historical -->
# The demo tree carries recipes, not renders — `docs/archive/demo` untracked above 256 KiB

**Task**: `01a0fd2e-eb81-7497-9275-5610c4c731cc` (epic "git 存代码不存 binary", step `[2/5]`)
**Branch**: `task/01a0fd2eeb81749792755610c4c731cc`
**Measured at**: merge-base `56e17d98` (master) → this branch's commit
**Scope**: `docs/archive/demo` only. The rest of the epic's families (`sandbox/**`, the repo-root
run sites, history) are other rows' ground — see *Not Done (honest)*.

## What was wrong

Measured against master's tip (`56e17d98`), `docs/archive/demo` held **1295 tracked paths /
147.08 MB**, of which **166 blobs exceeded 256 KiB** — and **156 of those 166 were
already counted as violations in the tracked-blob hygiene baseline**
(`internal/standards/testdata/repo-hygiene-baseline.tsv`, GOPI.md Rule 39). They were
not "assets". Every one is **run output of a tracked generator**, produced from a
tracked text input:

| class | count | produced by | from |
|---|---|---|---|
| `materials/*/03-raw.png` | 61 | `scripts/materials/*.sh` (`render-entry.py`, `render.py`, `gen-*.sh`) | that entry's own `01-input.txt` / `02-output.txt` |
| `materials/*/04-annotated.png` | 62 | same, plus `gopi annotate` box geometry via `scripts/materials/render.py` | the `03-raw.png` it re-annotates |
| `loop/*/03-raw.png`, `04-annotated.png` | 7 | `scripts/demo-memory-loop-figs.sh <loop-dir> <out-dir>` | `loop/*/00-entry.md` + `02-output.txt` |
| `human/frames/f0NN.png` | 25 of 43 | `tools/human-ui/assemble.py` → `<out>/frames/f%03d.png` (`scripts/human-ui/build.sh`) | a live chat session recorded by the build |
| `human/{chat-loop.gif,hero.png,chat.png,clean/*,raw/*}` | 9 | same build (`gopi gif` / `gopi capture` / masking) | the same session |
| `final/render-proof.png` | 1 | `docs/archive/demo/final/build.sh --shot` → `gopi shot --url file://…/index.html` | the page `build.sh` itself generates |

The generators are all **tracked and unchanged** by this commit; the recipe is intact.
A clone was paying ~84 MB for pictures it can rebuild.

## The reference proof (per class)

A path is only removable if **no machine reads it**. The check is path-EXACT, not
name-level: a same-named file in another directory (`t.TempDir()`, a sandbox, a
`gopi_scratch_mkdir` root) is not a reference, and a prose sentence narrating a past
run is not a reference either.

```bash
# every >256K path under docs/archive/demo, with the object-store length the gate reads
git ls-files -s -z -- docs/archive/demo | tr '\0' '\n'   # <mode> <sha> <stage>\t<path>
# … then git cat-file --batch-check='%(objectsize)' on the object ids
# and, for each path in the removal set, an exact-path citation search:
git grep -n -F '<that exact path>' -- . ':!docs/archive/demo'
```

Result over the removed paths — **four citations, all prose or comment, zero machine
readers**:

| hit | verdict |
|---|---|
| `devdoc/demo-thesis.md:386` — a record-table cell naming `docs/archive/demo/final/render-proof.png` | **PROSE.** Saying the artifact exists; `demo-thesis.md` is classed **historical** in `devdoc/DOC-INDEX.tsv`, so no anchor gate reads it. |
| `devdoc/demo-thesis.md:323` — a bullet listing `env-04-tmpdir-loud-refusal/04-annotated.png` as 配图 | **PROSE.** Same doc, same class. |
| `devdoc/human-ui-gif-legibility.md:8` — "`docs/archive/demo/human/chat-loop.gif` **was** 960x456" | **PROSE, past tense.** That normatively-classed doc's manifest spot-check names a *different* path (`docs/archive/chat-loop-960x456-20260930.gif`, still tracked), so the deletion does not touch its row. |
| `tools/human-ui/assemble.py:674` — a `#` comment: "who opens `docs/archive/demo/human/hero.png` gets the artifact…" | **COMMENT.** The generator writes that path; it never reads it. |

Everything else that matched was **the class name, not the path**
(`03-raw.png` / `04-annotated.png` / `render-proof.png` appear in ~200 files as the
six-file-shape vocabulary and inside the generators' own write logic) — never as a
read of a repository path. The two runnable surfaces that touch the tree at all are:

- `scripts/capdemo-external-check.sh` — its A1 arm walks `git grep` for
  `docs/archive/demo/external/**/*.png` citations. It never sees `materials/`, `human/`,
  `loop/` or `final/`.
- `docs/archive/demo/final/build.sh:552` — `git log -1 --format=%h -- docs/archive/demo/materials/01-list/04-annotated.png`.
  A `git log` over the path, **not** a read of the file: it still resolves after the
  untrack (git history is untouched by `git rm --cached`).

## What was done

1. **Untracked every enumerated path, one per line** — `xargs -a <list> git rm --cached --quiet --`
   (never `git add -A` / `git add .`, GOPI.md Rule 8). 156 paths / 80.41 MB de-tracked;
   with master's own replay-arm untrack riding the same merge, `docs/archive/demo` goes
   **147.08 MB → 62.77 MB** and its >256 KiB count **166 → 10**.
2. **Root-anchored `.gitignore`** so the files, which stay on disk, do not come back
   as `??` (Rule 30). Nine patterns, one per class:
   `/docs/archive/demo/materials/*/03-raw.png`, `/docs/archive/demo/materials/*/04-annotated.png`,
   `/docs/archive/demo/loop/*/03-raw.png`, `/docs/archive/demo/loop/*/04-annotated.png`,
   `/docs/archive/demo/human/{frames,raw,clean}/`, `/docs/archive/demo/human/chat-loop.gif`,
   `/docs/archive/demo/human/hero.png`, `/docs/archive/demo/human/chat.png`,
   `/docs/archive/demo/final/render-proof.png`.
   Masthead's own replay block (`/docs/archive/demo/replay/arm-*/` + six bare-product
   names) lands beside them from master; the merge kept **both** blocks
   (`git merge master` → one `.gitignore` conflict, resolved by keeping both, each
   carrying its own reasoning). Pre-existing rules survive:
   `grep -c '^bin/$' .gitignore` = 1, `grep -c '^!/GOPI.md$' .gitignore` = 1.
3. **No new gate — the ceiling is master's, and this is deliberate.**
   Master already carries the epic's `[1/5]` deliverable: **GOPI.md Rule 39** +
   `internal/standards/repo_hygiene_gate.go` + baseline
   `internal/standards/testdata/repo-hygiene-baseline.tsv` + the re-runnable census
   `testdata/repo-hygiene-census.go`. That gate is a **strict superset** of anything
   this row would have written, and comparing the two shapes is the whole argument:

   | property | Rule 39's gate (master) | this row's first draft |
   |---|---|---|
   | measurement | `git ls-files -z` index + `git cat-file` object store | the same |
   | ceiling | `oversizeLimitBytes` = 256 KiB, plus a 1 MiB font arm | 256 KiB only |
   | classes | oversize, font, binary magic, NUL-in-first-8KiB | oversize only |
   | ratchet | `(path, class)` permits with reasons, **stale row = error**, class-trade = error, malformed row = error | path permits with a stale check |
   | red proof | a real throwaway repo with one blob per class | a planted index fixture |

   Two gates over one ceiling is exactly the anti-pattern Rule 13/14 names (one
   value, one definition): a second ceiling is a second thing to drift. So the draft
   gate was **dropped** and this row's removal was folded into Rule 39's own ratchet:
   the baseline was **regenerated**, not hand-edited —
   `go run internal/standards/testdata/repo-hygiene-census.go . internal/standards/testdata/repo-hygiene-baseline.tsv`
   — which removed **325 rows (161 `oversize` + 164 `magic`)** and left the table at
   **682 rows** (589 magic, 56 nul, 37 oversize), `--check` re-deriving it byte for
   byte. Every removed row named a path that is no longer tracked: `git diff HEAD --
   …baseline.tsv | grep '^-' | cut -f1 | grep -vc '^docs/archive/demo/'` → `0`.
   **This is the ratchet working as designed**: the removal is only *complete* once
   the permits it made stale are gone in the same commit, which is why the two halves
   are one change and not two.

## Permits: what still sits above the ceiling, and why it is not a hole

Rule 39's permit is an allow-list entry in the exact sense GOPI.md Rule 16 gives that
word: a **stale** permit (its path no longer fires) is an error, a class **trade** is
an error, and an unknown class is an error — so the list can only ratchet down.

After this change, **10** tracked blobs under `docs/archive/demo` exceed 256 KiB, and every
one carries a baseline row with a printed reason:

| path | bytes | why it stays |
|---|---|---|
| `docs/archive/demo/replay/replay-film/chat-replay.gif` | 15 416 176 | The **curated film** — repo content, not a run's residue (see `docs/archive/demo/replay/README.md`). Owned by task `01a0fd20`. |
| `docs/archive/demo/replay/appendix/img-old-text2img/*.png` (4) | 331–349 KB | The appendix's archived text2img renders — a record of an earlier round, owned by `01a0fd20`. |
| `docs/archive/demo/replay/img/{raw,clean}/05-05-root-cause.png` (2) | 308–318 KB | The replay figure pair; owned by `01a0fd20`. |
| `docs/archive/demo/external/stills/02-live-desktop-window.png` | 329 234 | A still of the **external capability bundle**, gated by its own checkpoint: `scripts/capdemo-external-check.sh` compares each still's recorded sha256 and byte count against the file (A3) and refuses a still no caption attributes (A2); `internal/standards/capdemo_external_gate_test.go` runs it from the suite. Removing it is a **bundle edit** (re-shoot + re-caption `CAPTIONS.md` + repoint three devdocs), not a `git rm`. |
| `docs/archive/demo/external/stills/06-annotate-live-window.png` | 331 104 | Same class as above. |
| `docs/archive/demo/materials/env-07-debug-channel-line/02-output.txt` | 381 683 | The **evidence half** of a materials entry. `devdoc/materials-D-annotation-criterion.md:97` states the split: `03-raw.png`/`04-annotated.png` are *regenerated*, `01-input.txt`/`02-output.txt` are *evidence* and are never touched. Re-running the observed command would produce a **different** file, so deleting it deletes the claim rather than re-deriving it. It is text, not a binary. |

The four `replay/**` classes are **deliberately not touched here**: task `01a0fd20`
is pending and explicitly owns that directory, and `01a0fd14` is `in_progress` moving
the replay verb's output out of the source tree. Doing it here would collide with
both. Growth of the class still fails — the ceiling is a size check on **every**
tracked blob, not a directory exemption.

## Verification

See `## Evidence (self-filed)` for the command/observation pairs, and the task report
for the full Build/Tests/Ran/Result block.

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → rc 0.
- **Tests**: `go test ./internal/standards/ -run TestRepoHygiene -count=1` → `ok` —
  the gate this change *adopts* is green against the regenerated baseline; plus the
  fuller `make test-budget PKGS='./internal/standards/ ./internal/agent/'` in the
  task report, including the two **pre-existing** master reds it discloses.
- **Ran**: the removal's acceptance surface —
  `git ls-files -z -- docs/archive/demo | xargs -0 du -b | awk '$1>262144'` → **10 rows**, all
  of them a permitted one; and the census's own `--check` → matches the tree.
- **Result**: `docs/archive/demo` is **1139 tracked paths / 62.77 MB** (from 1295 / 147.08 MB);
  `git status --porcelain` prints nothing.

## Regenerating any class

```bash
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/          # every generator needs it

# materials/<entry>/{03-raw,04-annotated}.png — the batch generators, per family
bash scripts/materials/gen-topA.sh <out>     # or gen-topB / gen-flagF / gen-envB / gen-taskD / gen-memoryC
python3 scripts/materials/render-entry.py    # single entry, from its 01/02 text
python3 scripts/materials/render.py --raw <dir>/03-raw.png --ann <dir>/04-annotated.png …

# loop/*/ — the closed-loop figures
bash scripts/demo-memory-loop-figs.sh <loop-evidence-dir> <out-dir> <render-evidence-dir>

# human/ — the whole showcase (needs Xvfb + the host terminal + xdotool)
bash scripts/human-ui/build.sh --bin bin/gopi --scratch "$HOME/.gopi/sessions/<id>/human-ui-scratch" --out docs/archive/demo/human

# final/render-proof.png — needs the page first
bash docs/archive/demo/final/build.sh && bash docs/archive/demo/final/build.sh --shot
```

## Not Done (honest)

1. **The four `replay/**` residual classes (≈17.1 MB) are not removed here.** They are
   another row's ground: `01a0fd20` is pending and explicitly owns that directory;
   `01a0fd14` is `in_progress` moving the replay verb's output out of the source tree.
   They stay in the baseline with their reasons above.
2. **The two `external/stills` stills are not removed.** They are gated by a live
   checkpoint and named by three devdocs; that is the capdemo bundle's own edit.
3. **`docs/archive/demo/final/index.html` was left tracked** even though `build.sh` regenerates
   it (1.7 MB tree, under the ceiling): it is what the bundle *publishes*, and untracking
   it is a bundle-level decision, not a size sweep. The `.gitignore` pattern for its
   `render-proof.png` is scoped to the PNG, so the page still comes back as tracked.
4. **History is not rewritten.** The ~84 MB stays in the object store; this commit
   stops the tree from *carrying* it. `01a0fd26` (the sibling `[1/5]` row) measured the
   history problem — the plan for it is `[4/5]`, and a rewrite of the shared `.git` is
   the operator's decision, not this row's.
5. **The untracked copies stay on disk** (~80 MB in this worktree, ignored). Deleting
   them is an operator call: they are still readable evidence until the merged tree is
   regenerated once.
6. **A first-draft gate of this row's own was written and then dropped** (`demo_artifact_gate.go`,
   `demo_artifact_gate_test.go`) once the merge showed master's Rule 39 gate already
   covers the same ceiling with more classes and a stronger ratchet — see *What was
   done* §3. It is removed from the tree, not merely disabled.

## Evidence (self-filed)

- removal, before → after: `git ls-tree -r -l 56e17d98 -- docs/archive/demo | awk '$4>262144'|wc -l` → `166`;
  `git ls-files -z -- docs/archive/demo | xargs -0 du -b | awk '$1>262144'|wc -l` → `10`.
- bytes: `git ls-tree -r -l 56e17d98 -- docs/archive/demo` → `1295 paths, 147079714 bytes`;
  `git ls-files -z -- docs/archive/demo | xargs -0 du -bc` → `62768350`.
- `.gitignore` in force: `git check-ignore -v docs/archive/demo/human/frames/f043.png` →
  the `/docs/archive/demo/human/frames/` rule (and the reason a live red proof needed `git add -f`).
- baseline ratchet: `go run internal/standards/testdata/repo-hygiene-census.go . … --check`
  → `matches the tracked tree (682 rows)`.
- stale-row cleanliness: `git diff HEAD -- …baseline.tsv | grep '^-' | cut -f1 | grep -vc '^docs/archive/demo/'` → `0`.
- gate green: `go test ./internal/standards/ -run TestRepoHygiene -count=1` → `ok`.
- corpus: `gopi doccheck` → `the devdoc corpus is consistent`, rc 0.

## Two pre-existing reds (measured, NOT introduced here)

`make test-budget PKGS='./internal/standards/ ./internal/agent/'` reports two
failures. Both are **master's own**; the sibling `[1/5]` row repaired the second one
on master, which is one of the reasons this branch had to sync before it could claim
anything:

1. **`internal/agent`: `TestDocAnchorsResolveInNormativeDocs`** — 14 new drift
   entries, 3 stale permits, all master's own landed records
   (`devdoc/requirements/merge-and-branches.md`, `devdoc/docanchor-gate-bare-lineno.md`,
   `devdoc/assert-the-bug-audit-20260917.md`, …). **Zero** failure lines name this
   row's document (`go test … | grep -c demo-artifacts` → `0`).
2. **`internal/standards`: `TestDocClassBannerMatchesManifest`** — master's own
   `devdoc/chat-repl-pty-teardown-order.md` (`historical` banner vs a `normative`
   manifest row), repaired on master by `ac547d1a`'s lineage before this merge; the
   post-merge run of `./internal/standards` is the evidence below.

## Ran (Rule 31) — the generator that produced the removed classes, re-run for real

A build and a green suite do not show that the classes this commit untracked are
really regenerable. So the materials generator was run end to end, against a REAL
tracked input, with real arguments:

```bash
E=$HOME/workspace/gopi-test-scratch/rule31-regen-<pid>
mkdir -p "$E/x" && cp -r docs/archive/demo/materials/004-checkpoint-resume "$E/x/004"
CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
python3 scripts/materials/render-entry.py --dir "$E/x/004" \
  --needle 'completion_tokens=221' --label 'P1-round-usage' --caption 'usage line' \
  --font /usr/share/fonts/truetype/wqy/wqy-zenhei.ttc --bin "$PWD/bin/gopi" --scratch "$E/x/scratch"
```

**Result**: exit 0, and the two classes this commit untracked were produced from
`02-output.txt` (which STAYS tracked): `03-raw.png` 597971 B, `04-annotated.png`
599966 B, with the script's own self-proofs `ink_under_box 763 / changed_pixels 2093 /
label_covered_ink 0`.

**Why the two renders are not byte-identical to the tracked ones — and why that is
the point.** The needle, the label and the scratch path are per-run arguments, so a
rebuild produces a *different* PNG. That is exactly the asymmetry this commit acts
on: the **render** is derived (throw it away, re-make it in a minute), while the
**evidence text** it illustrates is not (keep it — see the `02-output.txt` permit).

**What was NOT run.** A byte-identical reproduction of a removed render is not
possible by construction (per-run label/scratch), and `scripts/human-ui/build.sh`
and `docs/archive/demo/final/build.sh --shot` were not executed: they need Xvfb, the host
terminal, `xdotool` and a Chromium shot, i.e. a GUI session this run does not have.
Their classes were untracked on the reference proof alone, and their regeneration
commands are recorded above.
