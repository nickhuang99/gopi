<!-- doc-class: historical -->
> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.
# `TestMergeUnionRowCheckOnTheRealManifest` — which side drifted, and what the check can and cannot see

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Base commit: **0b3c1de29** (`Merge branch 'task/01a12ce5cd81740faa9351bc36c68343'`, master tip at run start).
Task: `01a12cd9-bb7b-735a-9cf2-03cdb6bb6808`. **No code was changed by this task** — the row was
already repaired on master before dispatch; this is the determination, plus the measurements that
say what the check's shape leaves unwatched.

## 1. The finding and the verdict

The dispatched reading: the row for `devdoc/plain-prompt-first-user-message-scope.md` claimed

```
anchor cmd/agent/cli_runpath_boundary_rp1_test.go:246 at t.Errorf("the FIRST user message is not the prompt verbatim:\n got (%d
```

and the check reported that text "no longer carried by any line".

**Verdict: the ANCHOR was the side that drifted, and it is already repaired on master.** Two
commits, both ancestors of this run's base:

| commit | date | what it did |
|---|---|---|
| `cafe04e48` | 2026-10-12 | **the rewriting commit** — `fix(agent): a plain prompt's first request is the prompt alone — the turn stamp is exempted, not glued in`. It rewrote `cli_runpath_boundary_rp1_test.go`'s line, replacing the old sentence with `` `(1) `[EPIC GOAL]\n<token>\n\nBased on this Epic, …[EPIC REFRAME] …` `` |
| `8a0d75d9b` | 2026-10-12 06:02 | **the resolving commit** — `fix(docclass): re-derive the two drifted DOC-INDEX evidence anchors so the floor case passes on the real tree`. Re-derived the row's evidence cell onto the text `cafe04e48` had left. |

Proof of the rewrite (the `-` line is the text the row had recorded):

```
$ git show cafe04e48 -- cmd/agent/cli_runpath_boundary_rp1_test.go | grep -E '^[+-].*did not start the message'
-// did not start the message at all.
+//	    token did not start the message at all (the harness frame on a shell
```

Proof the row was **correct when it was born** (so this is drift, not a row that never matched):
at `dea20b7e1` (2026-10-01, the commit that first added the row) the recorded text stood in the
file — `git show dea20b7e1:cmd/agent/cli_runpath_boundary_rp1_test.go | grep -c 'is not the prompt verbatim'` → `1`.

**The test file was not wrongly changed.** `cafe04e48` changed a diagnostic *message*, and the
row's job is to record the text its anchor stands on; the honest repair is the one taken —
re-anchor the row, leave the code alone. Deleting or rewriting the row's claim to match a
message nobody needed would have been the wrong direction.

## 2. The recorded text is back — because the same sentence exists twice

The re-derivation is honest but it landed on a fragile coincidence, worth naming. Today **both**
sentences stand in the file:

```
$ grep -n 'did not start the message at all\|is not the prompt verbatim' cmd/agent/cli_runpath_boundary_rp1_test.go
247://	    token did not start the message at all (the harness frame on a shell
283:		t.Errorf("the FIRST user message is not the prompt verbatim:\n got (%d chars): %q\nwant (%d chars): %q\n"+
```

That is why the old cell's text (`t.Errorf("the FIRST user message is not the prompt verbatim:…`)
still has a live line today: it is a *prefix of the `t.Errorf` on :283*, not of the anchor's own
line :246. A future reader must not conclude from the current cell that the row's anchor resolves
into its own claim — `:246` carries the marker text, and the recorded text's real home is the
prefix of :283.

## 3. Is the check's text-matching defective? No — measured both ways

The epic asked for a fix *if* the check false-negatives on a text that IS present. It does not.

- **Independent re-implementation.** A scanner mirroring `docclass`'s rule (whitespace-normalised
  substring, drift token `stale=` stripped) over all 230 anchor rows of the shipped manifest:
  **0 stale**. The gate's own verdict on the same 230 rows: `ok`. **The two agree.**
- **The one place the two could disagree was MY artefact, not the gate's.** A first pass stripped
  the drift counter with the wrong token and reported **67** stale rows — every one of them a row
  whose cell ends in `stale=N` (`EvidenceAnchorDriftToken = "stale="`). The gate was right and the
  ad-hoc scan was wrong; the corrected scan reports 0.

A fabricated 67-defect finding would have been filed here as a real bug had the token not been
read. (Rule 41's discipline cuts both ways: an anomaly is a hypothesis until the mechanism is read.)

## 4. What the check cannot see — the measured residual (filed, not fixed)

`mergeUnionCheckRows` resolves **`r.Class == docclass.ClassNormative` rows only**; its own comment
says so (`cmd/agent/merge_union_rows.go`: "only a normative row records a spot-check the gate
re-verifies"). Measured over the repo's own manifest:

| | count |
|---|---|
| rows in `devdoc/DOC-INDEX.tsv` | 2282 |
| class `normative` (checked by the gate) | 726 |
| rows carrying an `anchor` evidence cell | 230 — **all of them normative** |
| anchor rows the gate resolves | 230 |
| anchor rows no gate resolves (non-normative) | **0** |

So the gate's narrow scope costs **nothing for anchor rows today** — the hole the code's shape
suggests is empty on this corpus. The genuinely unwatched class is the **line NUMBER**, and the
gate's own comment names it: `anchoredTextLine` "is preferred, but only to report WHICH line
matched: the verdict is the text's either way" (`internal/docclass/evidence.go`). Measured:

```
ANCHOR ROWS WHOSE path:LINE DOES NOT CARRY THE RECORDED TEXT
  anchor rows with recorded text : 230
  number points elsewhere/blank : 153  (66%)
```

Two thirds of the manifest's anchors carry a line number that no longer points at the text they
recorded — the text kept the row green, the number had drifted and nothing reports it. Named
examples (row | target | recorded text | what actually stands there):

| row | target | recorded | what stands there now |
|---|---|---|---|
| 16 `IMMEDIATE-FEEDBACK-REGRESSION.md` | `internal/agent/loop.go:547` | `if contentReturning {` | `// WITHOUT the tool itself having failed. Ap` |
| 39 `agent-five-organs-ontology.md` | `internal/agent/loop.go:3230` | `if repaired, orphanIDs := …` | `}` |
| 193 `brew-subcommand-test-hermetic.md` | `cmd/agent/cli_voice_server.go:46` | `return srv.ListenAndServe()` | `// voiceServeShutdownCh is armed by a caller` |

**The whole-corpus generator rewrites exactly these numbers**, and that is measured too:
`python3 tools/docclass/classify.py .` is **deterministic** — run twice it produces byte-identical
output (`cmp gen1 gen2` → identical) — yet it rewrites **1320** lines / 660 rows of the shipped
manifest, **265** of them anchor rows of which **162** change the recorded TEXT (0 are
number-only moves), e.g. row 16 to `internal/agent/loop.go:6417 at // executeToolDefault is the
real per-tool dispatch (ex…`.

**Why the numbers drift — measured, and it corrects the first diagnosis of this run.** The
generator does NOT re-anchor by search. In `tools/docclass/classify.py`, the function that builds
the cell (`code_spotcheck`, line 525) iterates `ANCHOR_RE` =
`` `([A-Za-z0-9_][A-Za-z0-9_./-]*\.go):(\d+)` `` over the **DOC's own body** and takes the first
match that still lands on a non-blank line; the recorded text is then `lines[n-1].strip()[:70]`.
The NUMBER is the document's own prose anchor and the TEXT is whatever stood at that line **when
the cell was generated**, so the two are coupled at generation time and drift together as code
moves. Measured:

- **Provenance**: of 6 sampled drifted rows, **4** carry a cell number that appears verbatim in
  their own doc's prose (`` `loop.go:547` ``, `` `task_session.go:517` ``, `` `tasks.go:3976` ``,
  `` `loop.go:3230` ``, each `count == 1`, backticked); 2 do not. The provenance is heterogeneous
  and is **not** fully characterized here — stated as a limit, not a conclusion.
- **Coupling**: a fresh scoped generation of 5 of those docs (`classify.py . --only devdoc/<doc>
  --dry-run`) produced a cell whose own number carries its own text in **5/5** cases.

So the 153/230 figure is an expected steady state of the format, not corruption — and the
correction matters, because it means a whole-corpus re-derivation would **re-couple the number to
the text today and drift again tomorrow**. `01a12d1e`'s own implication that the durable repair is
that 660-row rewrite is therefore not supported; the superseding row
`01a12d21-6193-7cc2-b0e4-d4925cd84d14` says so with the same evidence bundle.

**What is actually unwatched — and the sharper of the two finds.** (a) The number is never checked
at all: it may point thousands of lines away (`loop.go:547` → `:582`; `:3230` → `:3975`;
`tasks.go:3976` → `:5297`). (b) The TEXT tier is looser than its doc comment reads: a recorded text
is satisfied by **any** line of the file containing it, whitespace-normalised, as a substring —
so a claim can be kept green by an unrelated second occurrence. Measured on this task's own doc:
the OLD cell's text (`t.Errorf("the FIRST user message is not the prompt verbatim:…`) is alive at
`cmd/agent/cli_runpath_boundary_rp1_test.go:283`, while the current cell's number `:246` carries the
current cell's text — two occurrences of the same claim, one of them the doc's original, and
nothing distinguishes them. Whether to gate the number (it would go red on 153 healthy rows the
moment it landed) or to tighten the text tier is a **policy decision about a merge/verify gate**,
i.e. system-layer work on a client host (Rule 28) — filed, not taken (Rule 41).

## 5. Reproduction

```bash
cd <worktree>   # task/01a12cd9bb7b735a9cf203cdb6bb6808
go test ./cmd/agent/ -count=1 -run TestMergeUnionRowCheckOnTheRealManifest   # ok, 726 normative rows resolved
python3 tools/docclass/classify.py . --check        # exit 0, writes nothing
```

Raw outputs for every figure above: `artifacts/` of this run's session directory
(`/home/user/.gopi/sessions/01a12cd9-bb7b-735a-9cf2-03cdb6bb6808/artifacts/`).
