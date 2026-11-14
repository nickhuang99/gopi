<!-- doc-class: historical -->
# Task Report: the retired-verb sweep reaches `scripts/` subdirectories — and the renderer it had been hiding behind

**Task**: `01a10f9c-04ba-7383-8c06-9a2e4327f42e` — "sweep the retired flat verb
spellings out of `scripts/` subdirectories (depth ≥ 2)".
**Predecessor**: `01a10ec6-…` swept depth-1 `scripts/*.sh` (commits `9abb2644` +
`5105b4d7`, both ancestors of this branch's base). Everything at depth ≥ 2 was
untouched.
**Verdict**: DOABLE, done. Two findings, one of which explains a live failure.

---

## Completed

1. **139 shell invocation sites rewritten** across 19 files under
   `scripts/materials/`, `scripts/human-ui/`, `scripts/replay/`, `scripts/ui/`.
   Each retired word was handed to the family door its OWN refusal message names
   (`gopi <word> -h`), measured rather than guessed:

   | word | door | word | door |
   |---|---|---|---|
   | `act` | `tool act` | `raw` | `session raw` |
   | `annotate` | `tool annotate` | `resume` | `session resume` |
   | `archive` | `task archive` | `sandbox` | `tool sandbox` |
   | `capture` | `tool capture` | `scratch-gc` | `tool scratch-gc` |
   | `checkpoint` | `session checkpoint` | `screen_capture` | `tool screen_capture` |
   | `code-search` | `tool code-search` | `shot` | `tool shot` |
   | `completion` | `config completion` | `stats` | `session stats` |
   | `doccheck` | `tool doccheck` | `text2img` | `tool text2img` |
   | `gif` | `tool gif` | `vision_analyze` | `tool vision_analyze` |
   | `keyword_search` | `tool keyword_search` | `voice_input` | `tool voice_input` |
   | `log` | `session log` | `setup` | `config setup` |
   | `memsync` | `memory memsync` | `proxy` | `tool proxy` |

2. **Prose that TEACHES a retired spelling was updated too** — 21 comment lines
   ("capture the ACTIVE WINDOW with the shipped `gopi capture --window`") now name
   the door. That is the bounded rule: a comment (a header telling the next reader
   how to invoke the verb) and an operator-facing `how_to_replay:` hint are
   teaching; a fixture body recording **what was measured** is history.

3. **A real defect, found by running the surface instead of grepping it.** The
   scan globbed `-name '*.sh'`. The materials renderer is
   `scripts/materials/render.py`, and it invoked the binary through an **argv
   list**: `sh([args.bin, "text2img", …])`. So the sweep reported a clean surface
   while the renderer kept calling the retired spelling at RUNTIME. Measured:
   `gen-topA.sh` ran to completion, every one of the 14 entries' commands executed
   (exit 0 for the ones my rewrite touched), and **every annotated image failed to
   render** with

   ```
   text2img produced no …/03-raw.png (rc=1)
   gopi: `text2img` is RETIRED — use `gopi tool text2img` (`gopi tool text2img -h`
   for its help). To send the word as a prompt instead, say so explicitly:
   `--prompt text2img`.
   ```

   Both halves are now in the sweep: the scan's file glob is
   `\( -name '*.sh' -o -name '*.py' \)` (195 files, was 141), the binary reference
   set includes `args.bin`/`argv[0]`, and the instrument understands the
   `ref, "verb"` argv-list shape — 146 further directives on the control tree that
   were previously invisible.

4. **The instrument's verdict is now the DIRECTIVE count**, not the raw hit
   count. The scan deliberately reports mentions too (its own header says so) — a
   prose citation is history and a human judges it — but it was failing the build
   on mentions, which is a verdict it cannot back. `classify-mentions.py` is that
   reader's rule made repeatable, and the scan prints every mention, counts them,
   and exits 0 only when the directives are zero. A site the classifier cannot
   read is `UNMEASURED` and counts as a directive (Rule 15) — it was a
   `NameError` crash for one round, which is exactly the silent green the rule
   forbids.

## Files

- 22 files under `scripts/` (19 `.sh` + 3 `.py`), 168 lines changed:
  `materials/{gen-taskD,gen-topA,entries-topB,gen-envB,gen-memoryC,gen-flagF,
  gen-interactive-real,gen-topB,gen-taskA,gen-env-E1,materials-gen-env-notify,
  materials-gen-gif}.sh`, `materials/{render,render-entry,materials-render-D}.py`,
  `human-ui/{build,compare-web,compare-terminals,web-only}.sh`,
  `replay/chat_replay_xtest.sh`, `ui/{chat-live-proof,chat-ui-observe}.sh`.
- `devdoc/scripts-subdir-verb-sweep.md` (this file).

The instrument lives in the run's session directory, **not** in the tree:
`~/.gopi/sessions/01a10f9c-…/{scan-old-spellings.sh,classify-mentions.py,
rewrite-doors.py,update-teaching-prose.py}`. It is a one-off measuring tool; Rule 35
says a script is not a deliverable for logic gopi should own, and nothing here is
logic gopi needs to own at runtime.

## Verification

- **Build**: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK, exit 0.
- **Tests**: not run — this diff touches no Go package (`git diff --name-only`
  lists 22 files, all under `scripts/`). The branch-scoped smoke set for the
  touched packages is therefore empty. Stated plainly rather than substituted with
  a whole-tree run (Rule 27: heavy/whole-tree belongs to the off-peak path).
- **Ran** (the exact invocation, verbatim):

  ```
  bash <evidence>/scan-old-spellings.sh /home/user/workspace/gopi-01a10f9c04ba73838c069a2e4327f42e
  ```
  → `exit 0`
  ```
  ### files scanned: 195   (depth 99 under scripts/)
  ### candidates beside a binary reference: 86
  ### of those, words the binary REFUSES as retired: 22
  ### of which DIRECTIVES: 0   (mentions: 107)
  OK: no invocation site under scripts/ spells a retired verb (every remaining site is a mention)
  ```

- **The surface itself was RUN** (Rule 31), not just scanned:
  `bash scripts/materials/gen-topA.sh <out>` → the generator executes the
  rewritten commands inside a sandbox and prints each entry's exit status. The
  rewritten sites are visible in the artefacts it wrote, e.g.
  `materials-topA-out/110-log/01-input.txt`:

  ```
  GOPI_HOME=…/sandbox-materials-topa-… \
    …/materials-evidence/bin/gopi session log 87bb56d8-7128-49f2-8209-57a137e9688d
  ```
  and `02-output.txt` holds that session's log (its own header `Session: 87bb56d8-…`
  and the model line). Entries `110-log`, `111-stats`, `112-raw`, `113-checkpoint`
  all exited 0.

  The run is also what caught the argv-list defect: the FIRST full generator run
  failed every render with `gopi: text2img is RETIRED`, the second with
  `gopi: unknown flag --out` (a two-word door emitted as ONE argv element), and
  after the fix the render advanced past both to

  ```
  gopi tool text2img: 字体不存在: /usr/share/fonts/truetype/wqy/wqy-zenhei.ttc
  ```

  i.e. the door now PARSES and the remaining refusal is `gen-topA.sh:37 FONT=` a
  hardcoded font path this host does not have — pre-existing, unrelated to the
  sweep, and outside its scope. The door itself is proven directly and separately:

  ```
  gopi tool text2img <probe.txt> --out <probe.png> --font /usr/share/fonts/truetype/arphic/ukai.ttc --size 20
  → produced 4199 bytes
  ```

- **Control tree (the "no false clean" proof)** — a detached worktree at
  `master`, driven by master's own freshly built binary, must still show the
  defect. With the FIRST instrument it did (`### of which DIRECTIVES: 140`,
  exit 1); with the WIDENED instrument it shows more (`146`, exit 1) while the
  worktree shows `0`. The instrument is not blind, and the clean is not an
  artefact of a narrower globe:

  ```
  cd /home/user/workspace/gopi-control-master && bash ./scan-old-spellings.sh "$PWD"
  → control scan exit=1   ### of which DIRECTIVES: 146   (mentions: 108)
  ```

- **`git status --porcelain`**: empty (`## Evidence (self-filed)` below).

## Evidence (self-filed)

- Baseline (before any edit): `### of which DIRECTIVES` … the first instrument
  reported `FAIL: 17 retired word(s) invoked` on 24 words / 141 files.
- Diff arithmetic, measured on the diff itself: `removed lines: 162, added: 162`,
  `retired-word sites removed: 171, added: 1` (the single `added` is a
  `::`-separated `--mark-match` spec whose text is genuinely about the door), and
  `comment lines updated: 21`.
- Control tree: `### of which DIRECTIVES: 146   (mentions: 108)`, exit 1, at
  `master` = `79b29f5f1` (the same commit the worktree is based on, so the only
  difference is my diff).
- Worktree: `### of which DIRECTIVES: 0   (mentions: 107)`, exit 0.
- The renderer's runtime refusal is quoted verbatim in `## Completed` item 3 — it
  is the observation that turned a "green" scan into a red product.
- `git status --porcelain` in the worktree: printed nothing.

## Not Done

- **The 107 remaining mentions are deliberately left alone.** They are recorded
  output and explanatory prose: markdown code spans in fixture bodies
  (`` `gopi checkpoint list` ``), finding bodies that explain a past defect, and
  English clauses ("a REAL browser … photographed by gopi capture"). Rewriting
  them would edit a record of what WAS measured into a history that never
  happened. They are enumerated, one per line, in
  `<evidence>/scan-final2.txt` under `### mention (prose/record):`.
- **`scripts/materials/` generators with hardcoded sibling-worktree paths**
  (`ROOT=/home/user/workspace/gopi-01a0ed230…`) cannot run as-is from this
  worktree — `gen-memoryC.sh` and `gen-env-E1.sh` refuse with
  `FATAL: no sandbox profile at …` because that directory does not exist. That is
  a pre-existing property of those fixtures, not something this sweep introduced;
  it is why the Rule 31 run above used `gen-topA.sh`, whose `ROOT` is derived from
  `${BASH_SOURCE[0]}` and therefore resolves to the tree it is run from.
- **The render-failure fix is verified by construction and by the refusal chain,
  not by a green 14/14 run.** Measured, in order: run 1 → `gopi: text2img is
  RETIRED` on every entry (the argv-list defect); run 2 → `gopi: unknown flag
  --out` (the two-word door arriving as one argv element); run 3 → `gopi tool
  text2img: 字体不存在: /usr/share/fonts/truetype/wqy/wqy-zenhei.ttc`, i.e. the
  door parses and the next refusal is the fixture's own hardcoded, absent font.
  The door was then proven directly (`gopi tool text2img …` with a font that
  exists → a 4199-byte PNG). A 14/14 green run needs the fixture's `FONT` path
  repaired on the running host — a fixture fix, not part of this sweep.

- **`scripts/materials/gen-topA.sh:37 FONT=…wqy-zenhei.ttc` does not exist on this
  host** (the CJK fonts here are Noto/arphic), so the generator cannot produce
  images here whatever the verb spelling is. Left alone: repairing a fixture's
  font path is a different change with its own reason, and it would have hidden
  the argv evidence that made this run worth doing.
- **No source-tree gate was added.** A `internal/standards` gate over `scripts/`
  would make this permanent, but it is a separate deliverable on the system layer
  (Rule 28: peerbox's) and inventing it here would be scope creep.
