<!-- doc-class: normative -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# The docclass manifest gate and the doc-anchor gate were red on master — what was wrong, and the repair

> **Provenance.** `TestDocAnchorsResolveInNormativeDocs` was retired on 2026-10-22 in `5a35f61` (deleted with `internal/docclass`), so a `file.go:NNN` reference in this page is a landing-time record, not an anchor a live gate enforces — nothing resolves one today. The line numbers stay useful to a reader; they are not checked by the suite. Citations below are quoted as they were written — where one of them reads as present tense ("is RED", "resolves", "fails"), read it as the state of THAT run, never as a gate this tree still has.

Two gates the corpus depends on were red on `master` `90e664e0`, re-measured on a
genuinely clean checkout (a fresh `git worktree add --detach` at that commit, not a
working tree that happened to look clean):

- `TestDocManifestCoversEveryDevdocFile` (internal/standards) — `2078 rows cover 2083
  files`; five devdoc files had no row at all.
- `TestDocAnchorsResolveInNormativeDocs` (internal/agent) — `108 new, 30 stale permits`.
  Its own log line claimed `201 pre-existing drift entries permitted, 0 new, 0 stale`
  in `devdoc/doc-truth-classification-report.md` §Evidence, i.e. **the drift arrived
  after the classification landed** (the manifest and the permit list were last touched
  by `208665d8`/`83030f2d`; the corpus has taken many merges since).

Both are the same defect wearing two faces: **the checked-in manifest is generated, and
nothing regenerates it.** A merge lands a doc, or re-keys a Go function, and the
generated artifacts (the class manifest, and by extension the permit list keyed to
today's line numbers) silently fall behind the tree they describe.

## The root cause (measured, not inferred)

1. **The manifest was stale with respect to its OWN generator.** Running the
   checked-in `tools/docclass/classify.py` at the base commit produces a manifest that
   differs from the checked-in `devdoc/DOC-INDEX.tsv` in 127 rows, **24 of them class
   changes**. Checked against all three generator versions that ever shipped the policy
   (`5faf7daa` normative=430, `7e0956c2` normative=432, `83030f2d` normative=431), every
   version agrees with the regenerate and disagrees with the checked-in file on the same
   rows — e.g. `devdoc/ledger-snapshot-ship.md`, `merge-verify-withdraw.md`,
   `sweep-report-not-act.md` are `historical` under all three, and `normative` in the
   checked-in manifest (their three rows were hand-appended by `822e45e6`, a commit whose
   subject is "classify the report in the doc-class manifest"). So the regenerate is the
   generator's own output, not a hand-edit and not a policy change.
2. **Five docs landed with no row** (`pending-queue-obsolescence-review-20260919.md`,
   `proc-identity-single-predicate.md`, `rule13-gate-self-red.md`,
   `task-update-status-message.md`, `web-page-token-fetch-fix.md`) — each merged without
   a manifest regenerate. Until they have a row they are invisible to every class-driven
   gate, which is exactly the "anchor coverage is a lie" hole the manifest exists to close.

**Root cause in one sentence**: the generator is checked in and idempotent, but its output
is committed by hand, so the manifest is only ever as fresh as the last agent who
remembered to run it — and the gates redden whenever someone did not.

## The recipe (checked in, idempotent — no hand-edits)

```bash
# from the repo root of a clean checkout of the branch:
python3 tools/docclass/classify.py .      # rewrites devdoc/DOC-INDEX.tsv (2084 rows)
python3 tools/docclass/banner.py .        # applies/removes the <!-- doc-class: … --> banner
# a SECOND run of both must produce ZERO diff — that is the stability criterion, and it
# is also why the manifest cannot be hand-patched: the next run reverts a hand edit.
git status --porcelain                    # only the files the two generators touched
```

## The five files: class and reason

| file | class | why |
|---|---|---|
| `pending-queue-obsolescence-review-20260919.md` | historical | the title declares a past run's record (`0 standing / 5 record`); no current artifact cites it as authority |
| `proc-identity-single-predicate.md` | normative | a landed ruling: cited by `tasks/01a0b5f5-rework-proc-identity-report.md`, and it names code that resolves (`anchor internal/tools/task_reclaim.go:784`) |
| `rule13-gate-self-red.md` | normative | cited by `tasks/01a0b948-report.md:227`; spot-check `sym TestRule13_NoBareBufferLiteral` in `internal/tools/named_limits_rule1_test.go` |
| `task-update-status-message.md` | normative | cited by `tasks/01a0b948-report.md:228`; spot-check `anchor internal/tools/tasks.go:1436` |
| `web-page-token-fetch-fix.md` | normative | cited by `tasks/01a0b5f5-report.md:13`; spot-check `sym TestWebPageToken_PageLoadThenSendCarriesTheToken` in `cmd/agent/cli_web_webtoken_test.go` |

Four of the five are `normative` because a *current artifact* (a later run's report under
`tasks/`) cites them as authority — the classifier's `cite` tier. That is the policy
working: the doc IS the live statement of the rule it describes.

## The anchor delta, re-measured

`108 new / 30 stale` was measured on the base commit, **before any of this branch's
changes**. The decisive attribution probe: the same gate run in a second clean checkout
of the base commit (`/home/user/workspace/gopi-docgate-base`, normalizing the worktree
path) reports **exactly the same 108 failures**, and that set is a **superset** of the
branch's — the branch's regenerate *reduces* it to 104 (4 failures were evidence-cell
anchors on docs that stopped being `normative`). **Gate 2's red was therefore not caused
by this work; it was already red on master.** The wider manifest scope I ship (413 → 432
normative docs) exposes 6 further anchors, 2 of which are re-keyed, 4 permitted.

Of the 108 pre-existing failures (104 still failing after the regenerate — 4 stopped
failing because their docs left the normative class):

- **16 were re-keyed** by the gate's own rule — *the symbol named in the same table row is
  the authority; the line number is only an accelerator*. Each re-key is provable from the
  doc's own text (the row names the symbol, or quotes the source line), never by guessing:
  `runEngine` (×2), `FeedbackRecompute`, `openSearchDB`, `execSchemaBatchOnce`,
  `ChatRegisterLatch`, `SelfPageProvider`, `ParseProcStartTime`, `finishCleanupServerTarget`,
  `archiveBranchPrefix` (by its quoted fragment), `claudeToolDefs`,
  `runFinishCleanupChainTarget`, `TestMergerSweepPreview_MatchesSweepCandidatePredicate`
  (×2), `TestGate2TestEnvRedProof` and `tools.TakeJobNotices()` (by its quoted fragment);
  four of them are the start of a `:N-N` range. Three further candidate re-keys were
  **rejected**: the detected symbol was a coincidental identifier (`gate`, `main`) that the
  row does not name, and the drift is not provable from the doc — those became permits.
- **88 were permitted** with a reason, because no symbol in the row resolves them: the
  target file was deleted or renamed (`cmd/agent/merger_tick.go`,
  `cmd/agent/merger_takeover*.go`, `merger_loop.go`, `internal/tools/merger_schedule.go`,
  `merger_restart.go`, `GOROOT/src/os/file_unix.go`), or the line drifted onto a blank
  line in a doc that is a landed record with no quoted symbol to re-key against.
- **30 stale permits were DELETED**, not carried (a stale permit is a silent permission):
  their sites had been fixed or their doc had left the normative class.

**Historical docs were not touched** — not one line of a record's anchor was re-keyed.
Every re-key above is in a doc the manifest classes `normative` (a standing claim about
current master), where a drifted line is a live defect rather than a historical fact.
The generator's output for the 24 flips *moves 21 docs out of the gated scope* (records
whose anchors are supposed to describe the tree they measured); 3 docs come back in
(`ledger-snapshot-ship.md`, `merge-verify-withdraw.md`, `sweep-report-not-act.md` — no
longer normative, because no current artifact cites them any more) and their stale permits
left with them.

A caution for the next reader — **where the report lives is load-bearing**: a report under
`tasks/` that names `devdoc/<x>.md` stamps `<x>` as *cited by an authority* and can flip
its class (a `tasks/` report is part of the authority corpus; `devdoc/` is excluded from
it). A repair report must therefore sit at the `devdoc/` root, as this one does.

## Evidence

Branch: `fix/docgate-manifest-anchor-repair`, off `master` = `90e664e0`. Each numeric block below names the tree it was measured on; the delivered tip is a re-measurement of the same commands, and the values are the observation.
Base measurement: a second clean checkout (`git worktree add --detach … 90e664e0`, no
edits) so "pre-existing" is observed rather than argued.

**1. Manifest coverage gate — RED on the base commit:**

```
$ go test ./internal/standards/ -run TestDocManifestCoversEveryDevdocFile -count=1
--- FAIL: TestDocManifestCoversEveryDevdocFile (0.19s)
    docclass_gate_test.go:104: devdoc/pending-queue-obsolescence-review-20260919.md: no row in devdoc/DOC-INDEX.tsv …
    docclass_gate_test.go:104: devdoc/proc-identity-single-predicate.md: no row in devdoc/DOC-INDEX.tsv …
    docclass_gate_test.go:104: devdoc/rule13-gate-self-red.md: no row in devdoc/DOC-INDEX.tsv …
    docclass_gate_test.go:104: devdoc/task-update-status-message.md: no row in devdoc/DOC-INDEX.tsv …
    docclass_gate_test.go:104: devdoc/web-page-token-fetch-fix.md: no row in devdoc/DOC-INDEX.tsv …
    docclass_gate_test.go:130: devdoc/DOC-INDEX.tsv: 2078 rows cover 2083 files under devdoc/ (classes: map[historical:1652 index-meta:1 normative:413 roadmap:12])
FAIL
```

**2. Anchor gate — RED on the base commit, same 108 failures as the branch base:**

```
$ go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1
    docanchor_test.go:214: doc anchors verified: 1970 across 413 normative docs (1595 explicit + 375 bare continuations; 392 bare out of row/table/heading scope; 201 pre-existing drift entries permitted, 108 new, 30 stale permits)
FAIL
```

**3. Both gates GREEN on this branch** (final state, after the report itself was
classified and the generators re-run):

```
$ go test ./internal/standards/ -run TestDocManifestCoversEveryDevdocFile -count=1
ok  	github.com/nick/gopi/internal/standards	0.906s
    docclass_gate_test.go:130: devdoc/DOC-INDEX.tsv: 2084 rows cover 2084 files under devdoc/ (classes: map[historical:1638 index-meta:1 normative:433 roadmap:12])

$ go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1
ok  	github.com/nick/gopi/internal/agent	1.465s
    docanchor_test.go:214: doc anchors verified: 2013 across 433 normative docs (1634 explicit + 379 bare continuations; 393 bare out of row/table/heading scope; 243 pre-existing drift entries permitted, 0 new, 0 stale permits)

$ go test ./internal/standards/ -run TestDocManifestCoversEveryDevdocFile -count=1 >/dev/null 2>&1; echo "gate1 exit=$?"
gate1 exit=0
$ go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1 >/dev/null 2>&1; echo "gate2 exit=$?"
gate2 exit=0
```

The same two, re-measured on a **genuinely clean checkout** of the branch tip
(`git worktree add --detach <fresh> …`) rather than a working tree that
merely looked clean — the acceptance shape:

```
$ git status --porcelain
$ go test ./internal/standards/ -run TestDocManifestCoversEveryDevdocFile -count=1
ok  	github.com/nick/gopi/internal/standards	0.876s        # exit=0
$ go test ./internal/agent/ -run TestDocAnchorsResolveInNormativeDocs -count=1
ok  	github.com/nick/gopi/internal/agent	1.458s            # exit=0
$ python3 tools/docclass/classify.py . && python3 tools/docclass/banner.py .
$ git status --porcelain
$ CGO_ENABLED=0 go build -o /tmp/gopi-docgate ./cmd/agent/
build OK
```

`git status --porcelain` printing nothing both before and after the generator
re-run is the two claims at once: the run's worktree is clean (Rule 30) and the
generators are idempotent.

The delivered tip carries this paragraph, so it is a commit or two past the tip
the block above was pasted from. Every commit after it is a prose edit to this
report, and each was re-measured exactly as the block shows — a fresh detached
worktree of the new tip, `git status --porcelain` empty, both gate commands
exiting 0, `classify.py` + `banner.py` re-run leaving `git status --porcelain`
empty. So the block is a fixed point of the report's own edits: the observation
printed above is not extrapolated from an earlier commit.

The sibling manifest gates stay green (`TestDocManifestRowsAreWellFormed` — 433
normative rows each carrying a spot-check; `TestDocClassBannerMatchesManifest` — 687
markdown docs of a bannered class, 0 mislabelled).

**4. Generator idempotence** — the second run must produce zero diff:

```
$ python3 tools/docclass/classify.py . && cp devdoc/DOC-INDEX.tsv /tmp/i1.tsv
$ python3 tools/docclass/classify.py . && diff -q /tmp/i1.tsv devdoc/DOC-INDEX.tsv
CLASSIFY IDEMPOTENT
$ python3 tools/docclass/banner.py .
banner applied to 0 docs, removed from 0
$ python3 tools/docclass/classify.py . && python3 tools/docclass/banner.py . && diff -q /tmp/i2.tsv devdoc/DOC-INDEX.tsv
SECOND FULL RUN: ZERO DIFF
```

**5. Build + branch-scoped smoke:**

```
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
build OK
$ make test-budget PKGS='./internal/agent ./internal/standards'      # branch-scoped smoke
packages: 2 finished, 0 with a failure
stamp: 2026-09-19T23:31:23+08:00 host=peerbox pkg=./internal/agent ./internal/standards budget=25m elapsed=73s load_start=1.30 1.26 1.75 load_end=2.88 1.78 1.90 cores=4 exit=0 outcome=pass stream=run-1789831883.json
```

The two gate commands above were run **after** this report was written and classified
(the report is itself a devdoc, so the manifest had to absorb it and be re-verified:
2084 rows now, 433 normative).

## Not Done (honest)

- **The 88 permitted anchors are not repaired.** Only anchors a symbol in the same row
  could resolve were re-keyed; the rest name files that no longer exist
  (`cmd/agent/merger_tick.go`, `merger_loop.go`, `internal/tools/merger_schedule.go`, …)
  or drifted lines with no quoted symbol. Repairing those needs the doc's claim to be
  re-derived against current code, which is a corpus rewrite, not a gate repair — the
  permits carry the precise reason so the next pass can rank them.
- **The manifest is still committed by hand.** This branch repairs the *staleness*, not
  its *cause*: nothing runs the generators on merge, so the same red will return the next
  time a doc lands without a regenerate. A follow-up worth filing: a merge-path check that
  `classify.py` leaves `devdoc/DOC-INDEX.tsv` unchanged (the generators are already
  idempotent, so the check is one command).
- **The 8-line drift between this branch's red set and the base's is not attributed further
  than measured**: 4 failures disappear because docs left the normative class, 6 appear
  because the widened scope exposes them, 2 of those 6 are re-keyed and 4 permitted. I did
  not trace each of the four to a specific master commit.
