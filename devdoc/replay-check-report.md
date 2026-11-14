<!-- doc-class: historical -->
# Task Report: `scripts/replay-check.sh` — a claim checkpoint for the replay/film devdocs

## Completed

- **`scripts/replay-check.sh`** (new, ~1000 lines): 34 anchors over the
  replay/film family. Each anchor parses `recorded` **out of the doc's own
  line** and compares it with `measured`, taken from the artifact the sentence
  describes. Read-only by construction — it writes nothing, creates no
  directory, starts no film, generates no load (Rule 27).
- **The genuine drift it found in the tree it lands in, now fixed:**
  1. `devdoc/chat-film-replay-acceptance.md:76` — the **verbatim** `gopi chat`
     usage line still read `[--film DIR]`; `--film` has since taken
     `[--out FILE.gif]`. A quoted-verbatim line that is not verbatim is the
     defect this check exists for.
  2. `:84` — the doc's own `chat` flag row read **14** flags
     (`chat-ideal-geometry, film, max-seconds, …`); the script the same sentence
     names derives **16** (`film-terminal` and `out` have landed).
  3. `:86` — the derived group total read **40** rows; it is now **42**.
  4. `devdoc/film-assembly-and-verification.md:36` — the `verify.py` **1949**
     lines figure is **unattributable in this repository** and is now stated as
     such and pinned with `REPLAY-CHECK-HISTORICAL` (see *Not Done*).
- **Per-line bypass, counted and printed.** `REPLAY-CHECK-HISTORICAL` on the
  anchor line clamps it; the CLAMP line still prints both values, so a pinned
  drift cannot hide behind the counter. A claim the doc **stopped publishing**
  is a red (`need()`), not a silent pass.

## Deliverables and their evidence

| what | where |
|---|---|
| the checker | `scripts/replay-check.sh` (commit `e089ec38`) |
| the corrections | `devdoc/chat-film-replay-acceptance.md`, `devdoc/film-assembly-and-verification.md` |
| green run (34 rows) | `~/.gopi/sessions/01a1038933817ac9a00aa41973ca67e7/evidence-final-green.txt` |
| the red proofs | `evidence-1…5-*.txt` in the same directory |
| the doc fix as a patch | `stage-doc-fix.patch` in the same directory |

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK (binary
  pre-built and reused; the checker itself is bash, `bash -n` clean).
- Tests: `make test-budget PKGS='./internal/testutil ./internal/standards'` —
  branch-scoped to the packages this diff touches (a whole-tree suite is the
  off-peak path). **`exit=1`, `outcome=fail`, and I did NOT clear it**: the run
  reported 15 failures, of which **exactly one was mine** on its first pass —
  `TestDocManifestCoversEveryDevdocFile: devdoc/replay-check-report.md: no row in
  devdoc/DOC-INDEX.tsv`, which Rule 36 owns and which is now fixed (row added,
  `gopi doccheck` → *the devdoc corpus is consistent*, exit 0). The other 14 name
  files this branch does not touch (`scripts/evidence-code-delivery-axis.sh`,
  `TestStandardGates`, `TestPinCoverageTable`, `TestWorkerOwnership*`,
  `TestScratchPlacementSaturationIsMeasuredOnTheHost`, …) and are pre-existing on
  this base's shared worktree — **measured, not inherited from the load**: the
  method is in `## Evidence (self-filed)`, and the per-package rerun of the two
  gates this branch could plausibly move is `job-3` in the run's own log.
- Ran (**the surface itself, verbatim**):
  ```
  bash scripts/replay-check.sh
  bash scripts/replay-check.sh ./bin/gopi
  bash scripts/replay-check.sh ./bin/gopi DIR
  bash scripts/replay-check.sh --list
  ```
- Result: `34 check row(s) measured, 1 clamped line(s) …, 1 note(s)` →
  `replay-check: OK — every replay/film anchor resolves (0 stale)`, **exit 0**.
  At the doc's pre-fix state the same command printed
  `3 STALE anchor(s)` and **exit 1**; with the clamp token removed from
  `film-assembly-and-verification.md:36` it prints `1 STALE` (exit 1) again —
  the bypass is load-bearing, not decorative. All four red proofs and the
  restored green run are captured in the session's evidence directory.

## Evidence (self-filed)

- **Red, then green, on the real tree** — the same command, the same tree:
  - before the corrections: `replay-check: 3 STALE anchor(s) — …`, exit 1
    (`evidence-5-bypass-removed-red.txt` is the one-anchor form; the 3-anchor
    form is in the PROOF 4 transcript: usage line + flags 14→16 + total 40→42).
  - after: `replay-check: OK — every replay/film anchor resolves (0 stale)`,
    exit 0.
- **Injection, doc side** (`PROOF 1`): editing the doc's `**6 of 6**` to
  `**9 of 9**` on its own line yields
  `STALE the film frame AND distinct-frame counts: recorded=9 of 9 measured=6 of 6
  (published in devdoc/chat-film-replay-acceptance.md:119)`, exit 1.
- **Injection, artifact side** (`PROOF 2`): rewriting one byte count inside a
  scratch **copy** of `film/frames.tsv` (never the evidence itself) yields
  `STALE … recorded=6 rows, bytes 9897/…/8618 measured=6 rows, bytes
  12345/…/8618`, exit 1. The scratch copy under
  `/home/user/workspace/gopi-rcheck-ev` was removed afterwards (verified
  `exists after : False`).
- **The bypass is load-bearing** (`PROOF 3`): removing
  `<!-- REPLAY-CHECK-HISTORICAL -->` from `film-assembly-and-verification.md:36`
  turns the clamped line into
  `STALE the tools/replay/verify.py line count … recorded=1949 measured=1609`.
- **A claim the doc stopped publishing is a red, not a pass** (`PROOF 5`):
  deleting the numbers from the `distinct md5` row yields
  `STALE C2a the doc no longer publishes a parseable frame count and
  distinct-frame count`.
- **The gate run's 15 failures are NOT mine, measured three ways** — not asserted
  from a clean feeling:
  1. the run's own JSON stream names my artifacts **exactly once**
     (`grep -o 'replay-check[a-z.-]*' | sort | uniq -c` → `1 replay-check-report.md`
     — the DOC-INDEX row, fixed), and never names the two devdocs I corrected
     (`grep -o 'chat-film-replay-acceptance\|film-assembly-and-verification'` → 0);
  2. the failing gates name other files: `scripts/evidence-code-delivery-axis.sh`
     carries the `/tmp` literal `TestScriptsAreBoundedAndOffTmp` reports, and
     `git show HEAD~2:<that file>` reproduces the same line at a revision this
     branch never touched;
  3. my script has no `/tmp` literal and no unbounded loop:
     `grep -n '/tmp' scripts/replay-check.sh` → no match.
- **Proofs 2's premise, and the earlier false reds, are recorded here rather
  than smoothed over:** the first three versions of the checker produced
  ~12 STALEs that were **my own bugs**, not drift (see *Not Done*). The numbers
  reported above are from the version that ships.

## Not Done (honest)

- **`1949` is NOT rewritten — it is pinned and explained.** The
  `film-assembly-and-verification.md` sentence publishes `tools/replay/verify.py`
  as 1949 lines. Measured: **no revision of this repository holds a 1949-line
  `verify.py`** — the ablation commit's pre-image is **1609**, and every other
  revision of that path is 1176/1182/1424/1574/1609. So the figure is
  attributable only to the port's own frozen copy, which this repository does
  not contain. I judged rewriting another author's number to a value I could not
  attribute to be worse than naming the gap, so the line states what the 1949
  IS and carries the token. **Owner: whoever holds the port's frozen
  `verify.py`** — the figure needs that artifact, or removal.
- **Four checks MEASURE and REPORT rather than judge**, because their `recorded`
  is not reproducible at any single revision, or is a defect tracked elsewhere:
  - `C9c` the `c8e71550 → master` shortstat: the doc's `85 / +6691 / −8779` IS
    reproduced (at `08968a8e`), and today's `master` gives `175 / 23021 / 9200`
    (a census figure — Rule 38; re-pin it or name its head).
  - `C12b` the §7/B1 "is not on master" wording: true at the doc's base
    `429ac774` (0 importers, 0 entries — that half IS judged), false today
    (3 importers, 10 files).
  - `C14` `gopi chat -h` never mentions `--scratch`, `--cols`, `--rows` although
    the parser accepts them and the docs' own examples use them — the same
    `-h` discoverability defect the doc's §2 records.
  - `C5` `gopi chat --replay -h` still answers a usage error instead of the help
    page.
  **Owner of C5 and C14: task `01a10351-6fae-7905-8255-7a76b5d2c0b0`** — the
  FULL id, named in the checker's own output. (I first wrote the short form
  `01a10351`, which `gopi task show` refuses: *no task with task_id="01a10351"*.
  A row id that does not resolve is not an owner.)
  **And that row reads `completed`, while both defects still measure live**: a
  closed row whose defect outlived it is precisely what the checker's owner line
  is there to make visible. Turning another row's live defect into this gate's
  red would make the gate wrong, not stricter — so C5/C14 report, and the
  re-dispatch decision (Rule 26) belongs to whoever reads this, with the row id
  and the measured state now in hand.
  **DISPATCHED: task `01a103e4-858f-7878-81ca-7ddad063d3a9`** (tag `bug`), whose
  acceptance is C5/C14 going green in `bash scripts/replay-check.sh` — the row
  carries the exact commands and outputs above, so the executor starts from
  measurement rather than from a description. Dispatch, not a note: this report
  is not a channel anyone is obliged to read.

## What is safe to generalise here

The upstream goal of this epic (as briefed) was that the devdoc numbers stop
rotting silently. What this run establishes, and what it does not:

- **Establishes**: the instrument exists, is read-only, and its red is real — a
  doc edit, an artifact edit, and a removed bypass token each turn it red, and
  the tree it lands in went from 3 stale anchors to 0. Its `recorded` is parsed
  from the doc, so the document cannot drift away from the gate that guards it.
- **Does NOT establish**: that any check RUNS AUTOMATICALLY. Nothing invokes
  `scripts/replay-check.sh` — it is operator/agent-invoked, exactly like
  `scripts/capdemo-external-check.sh`, the precedent it copies. Wiring it into a
  gate is a separate decision with its own cost (Rule 35: a new thing that ticks
  is a dependency), and I did not make it.
- **Does NOT establish**: coverage of `devdoc/chat-film-replay-driver.md`. It is
  read only for its existence (the script fails loudly if it disappears); none of
  its numbers are anchored yet. That is the next increment if the family is worth
  it.
- **The earlier count of "~8 genuine doc-drift reds" is NOT what I found.** The
  recon's list was produced by reading; the mechanical run confirms **3 fixable
  drifts + 1 unattributable figure + 4 report-only items**. The remainder of the
  initial red list was **my own script's bugs**, each of which is worth recording
  because each one produced a confident false accusation:
  - `printf … | grep -q PAT` under `set -o pipefail` returns 141 (SIGPIPE) on a
    **successful** match, so `|| fallback` fired and the value read as absent.
  - `tr` is byte-wise: `tr '×' 'x'` turned `706×342` into `706xx342`.
  - `grep -F "$(basename $f)"` matched `replay.go` against `scripts/replay/replay.sh`,
    reading a neighbour's number; two paths sharing one doc line needed each
    bold count bound to the path **preceding** it.
  - A regex can re-capture its own anchors (`2/0 × 52 frames` → `2 0 52`).
  - An anchor phrase that silently matches nothing pinned the claim to line 0.
  The lesson is in the script's header, and the general one is worth keeping:
  **a claim checker's false red is as expensive as a false green** — it spends
  the reader's trust on a bug of its own.
- **No `devdoc/<feature>.md` beyond this report.** The design decisions live in
  the script's own header (why a script, why recorded is parsed from the doc,
  the pipefail pitfall, the per-line bypass).
