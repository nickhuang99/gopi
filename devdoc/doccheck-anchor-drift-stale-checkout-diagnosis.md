<!-- doc-class: historical -->
# Task Report: doccheck anchor-NUMBER drift — a STALE CHECKOUT, not a live master defect

**Task**: `01a13320-4981-795e-86c9-30adb8b271a8` (bug) ·
**Ground**: `/home/user/workspace/gopi-01a133204981795e86c930adb8b271a8` ·
**Branch**: `task/01a133204981795e86c930adb8b271a8` · **Base (cut point)**: `afbf9b572`

## Verdict

The filed claim — "`gopi tool doccheck` is RED on unmodified master for two
anchor-NUMBER drifts" — **is false as stated against the integration branch, and
true against the reporter's checkout.** Measured, three trees, two surfaces:

| tree | doccheck | census `--check` |
|---|---|---|
| **this ground** (`afbf9b572`, = `refs/remotes/gitPushcheck/master`) | **exit 0** — "163 of 163 are frozen … the devdoc corpus is consistent" | **exit 0** — "table matches the tree — the freeze is exact" |
| **same tree exported** (`git archive` of `afbf9b572`, no `.git`) | exit 0, **but one `CLAIM NOT TRUE` line** (separate defect, below) | exit 1 — the provenance probe cannot run without a git dir |
| **the reporter's checkout** `/home/user/work/GoPi` (`master` = `64b8a316a`, built binary from 10:09) | **exit 1**, first line `main.go:982` | exit 1 — "1 unpermitted … the table is not the tree" |

The two rows the bug names are **already fixed, on the integration branch, by an
existing commit: `944c71660`** (`fix(docclass): regenerate the anchor-NUMBER
freeze table for two unpermitted master drifts`, 2026-10-13 11:49:33).

## Root cause

Two independent drifts of the same shape, both created by commits that landed
**after** the freeze commit `0b3c1de29` and **before** the reporter's reading:

1. `devdoc/doubaostream-session-test-coverage.md` → `cmd/agent/chat_voice_live.go:249`.
   `a9f971496 feat(voice): chat /voice runs the v3 sauc engine on the bidirectional
   endpoint` inserted lines above the anchored site, so the recorded text
   `run := runVoiceLiveSession(stream, voiceLiveRunConfig{` moved `:249 → :254`
   (+5). The row's number was a true code location at its birth and at the freeze.
2. `devdoc/merge-resolve-peak-hour-guard.md` → `cmd/agent/main.go:982` at `}`.
   The number is **not a code location at all**: it is a line of
   `internal/standards/baseline.go` (a Rule-16 permit string) quoted by the doc's
   own `git show master:… | grep -n` transcript (doc line 300). It was coherent at
   row birth only by coincidence (`main.go` then had 984 lines, so `:982` was a
   bare `}`); the text is now at `:80`. A bare `}` occurs throughout the file, so
   "the recorded text is at :N" measures the nearest scan, not provenance.

Neither row is **re-pointed**: `AnchorFinding.Key()` is `doc|path|text` — the
number is not part of a permit's identity — and Rule 36 makes the table DATA
written only by its census generator. `944c71660` therefore **froze** the two
findings as permits (table 194 → 196 rows), which is exactly what the ratchet-down
allow-list is for. Its own message records the base-attribution: `git archive` of
`b99fc60f2` + census `--check` on that copy produced the identical finding set and
a byte-identical table, so neither drift was branch-caused.

**Why the reporter saw red while `afbf9b572` is green** — a measured clock skew,
not a code difference:

- `/home/user/work/GoPi`'s `master` is `64b8a316a` (reflog: 10:22:49, last entry
  `pull: Fast-forward`) — **65 commits behind** `refs/remotes/gitPushcheck/master`
  (= `afbf9b572`). `git merge-base --is-ancestor` says `a9f971496`, `b99fc60f2`
  and `944c71660` are **all absent** from that tree.
- The checkout's own remote-tracking ref is **already at `afbf9b572`** — so the
  tree is stale by a **non-fast-forwarded local `master` ref**, not by a missing
  fetch. Its `bin/gopi` (10:09) is likewise older than the 11:49 repair.
- The bug row was created 10:38:36 — i.e. it was filed **71 minutes before the
  repair landed**, against a tree that never contained it.

So: **the red belongs to the checkout, not to master.** The remaining question is
whether anything should have caught the staleness; it is a plain local branch that
no gopi gate reads, and the standard route (pull/ff before reading) is the fix.

## Reproduction (commands + raw output)

```bash
cd /home/user/workspace/gopi-01a133204981795e86c930adb8b271a8     # afbf9b572
$ ./bin/gopi tool doccheck
doccheck: anchor NUMBER coherence (recorded text still present, line number stale):
doccheck:   163 of 163 are frozen in internal/docclass/testdata/anchor-number-baseline.tsv (each permit states whether its drift predates the freeze or was first frozen here; the gate fails only on drift NOT listed there)
doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row
→ exit 0

$ go run internal/docclass/testdata/anchor-number-census.go . \
    internal/docclass/testdata/anchor-number-baseline.tsv --check
anchor-number-census: scanned 244 anchor row(s), 163 incoherent; table carries 163 row(s)
anchor-number-census: table matches the tree — the freeze is exact
→ exit 0

$ ./bin/gopi tool doccheck --repo /home/user/work/GoPi           # the reporter's tree
doccheck: anchor NUMBER coherence (recorded text still present, line number stale):
doccheck:   NOT PERMITTED (drifts, absent from the baseline): devdoc/merge-resolve-peak-hour-guard.md:1221: anchor cmd/agent/main.go:982 at } — number-drifted (recorded text is at :80)
doccheck: regenerate the table with:
  go run internal/docclass/testdata/anchor-number-census.go . internal/docclass/testdata/anchor-number-baseline.tsv
doccheck: 0 gap(s) between devdoc/ and devdoc/DOC-INDEX.tsv
doccheck: this run exits 1 on anchor NUMBER drift (see the section above), independently of the 0 gap(s)
→ exit 1        ← the reported symptom, reproduced, on the STALE tree only

$ go run internal/docclass/testdata/anchor-number-census.go /home/user/work/GoPi \
    /home/user/work/GoPi/internal/docclass/testdata/anchor-number-baseline.tsv --check
anchor-number-census: scanned 244 anchor row(s), 160 incoherent; table carries 159 row(s)
  NOT PERMITTED (drifts, absent from the table): devdoc/merge-resolve-peak-hour-guard.md	cmd/agent/main.go	}	pre-existing at the 0b3c1de29 freeze; …
anchor-number-census: 1 unpermitted, 0 stale permit(s), 0 unreproducible reason(s), 0 unreadable and 0 untrue claim(s) — the table is not the tree
→ exit 1        ← and it fails on its OWN table too: this is the pre-repair state
```

The **same tree exported at the repair's parent** (`git archive 944c71660~1`) is
red on doccheck with **both** rows (`:715` and `:1224`) — i.e. the repair is what
turns those two lines green, and nothing else did.

## Evidence (self-filed)

- `./bin/gopi tool doccheck` on this ground → **exit 0**; raw output in
  `~/.gopi/sessions/01a13320-4981-795e-86c9-30adb8b271a8/evidence/01-worktree-doccheck.txt`.
- `go run …anchor-number-census.go . <table> --check` on this ground → **exit 0**
  ("table matches the tree — the freeze is exact"), `…/02-worktree-census-check.txt`.
- `./bin/gopi tool doccheck --repo /home/user/work/GoPi` → **exit 1**, first line
  `main.go:982`, `…/06-stale-checkout-doccheck.txt`; its own-table census → exit 1,
  `…/08-stale-checkout-census-check.txt`.
- Export of `refs/remotes/gitPushcheck/master` (`git archive | tar -x`), with
  `GIT_DIR=/home/user/work/GoPi/.git` so the provenance probe can resolve
  `0b3c1de29`: doccheck **exit 0**, census **exit 0** —
  `…/07-export-doccheck-with-gitdir.txt`, `…/05-export-census-check.txt`.
- Pre-repair export (`944c71660~1`): doccheck **exit 1** with both rows at
  `:715`/`:1224` — `…/master-at-report-doccheck.txt` (that artifact is the
  `b99fc60f2` export) and the tree at `…/peer-before/`.
- `make test-budget PKGS='./internal/docclass'` → `1 finished, 0 with a failure`,
  `exit=0 outcome=pass` (stream `run-1791868538-2574802.json`).
- Staleness readings: `git merge-base --is-ancestor {a9f971496,b99fc60f2,944c71660}
  64b8a316a` → all **NO**; `git rev-list --count master..refs/remotes/gitPushcheck/master`
  → **65**; the whole 65-commit gap is a fast-forward (`--is-ancestor master
  refs/remotes/gitPushcheck/master` → OK).

## Separate defect found on the way (filed per Rule 41)

On the **git-less export** of the very tree this report declares green:

```
doccheck:   CLAIM NOT TRUE (regenerate the table): provenance not checkable here — provenance
probe: resolve 0b3c1de29: git rev-parse … exit status 128 (fatal: not a git repository)
→ exit 0
```

`docCheckReportProvenance` reuses the **failure arm's name**
(`docCheckDriftClaimFalse`, "CLAIM NOT TRUE") for an **abstention** it deliberately
does not fail on — while the census generator **throws** on the same tree (exit 1,
"provenance probe: …"). Two faces of one corpus therefore both print a
provenance verdict and disagree; and the exit-0 arm's first word is an assertion
about a claim that was never judged. Under Rule 15 the abstention needs its own
name; the header in `internal/docclass/anchor_number_baseline.go` claims the gate
and the verb "are the same verdict". Repro + raw output:
`…/evidence/10-provenance-uncheckable-mismatch.txt`; filed as **`bug` row
`01a133b2-f6aa-78b4-a0a1-94d6fef89465`** (`gopi task submit … --tags bug`, status
pending, source agent). A second, smaller observation rides in that row as a note
rather than its own finding: `docCheckDriftLine` is the one drift-preface
constant without a trailing newline (`cli_doccheck.go:328`), so that line and the
next run together.

## Not Done (honest)

- **`/home/user/work/GoPi` was not updated.** It is read-only reference for this
  run; fast-forwarding another checkout is not this branch's business. The
  staleness is reported, not repaired.
- **The provenance defect is filed, not fixed** — it is a different seam
  (`cmd/agent/cli_doccheck.go` naming) from this epic's question, and Rule 41 asks
  for evidence + a row, not for a fix in this branch.
- **No sandbox/peer-host reproduction**: the peer-master leg was taken from
  `refs/remotes/gitPushcheck/master` in the shared object store, which is the same
  commit the export is cut from (`afbf9b572`); a live `ssh` read was not needed and
  was not done (Rule 22/34 unrelated).
