<!-- doc-class: historical -->
# The replay traceability check asked for a layout the renderer never produced

**Task** `01a0fa5460557d08a1f03d9924473d6a` · **Surface** `tools/replay/verify.py` (check 1,
traceability) · **Film** `arm-sess-4ca8cd29-20261002`, 209 beats, session
`4ca8cd29-019c-4dda-a000-6dafd782bf91` (550 lines).

## Completed

The checker now accepts that film, and still refuses every corruption of it.

Two false FAILs, byte-exactly root-caused and fixed:

1. **`FAIL: beat 170: text '⚙ task_submit {"payload":"## 现象（操作者原话）\n' is not the
   activity row its source line's call composes`** — a tool row is composed from the
   call's **raw** `input` bytes (`json.RawMessage` → `digestArgs` reads it as a
   string), so Go's escaping of `>` survives into the digest (`\u003e`). The checker
   parsed the call and re-serialized it with `json.dumps`, which writes `>` literally
   — a needle that never matches a correct film. Fixed by composing the needle from
   the recorded line's own raw value (`raw_input_values`, paired to its call by
   offset, never by counting) and reproducing Go's `<`/`>`/`&` escaping
   (`go_json`).

2. **`FAIL: beat 158: text '8cd29` | |---|---|--' is on no row of the grid at its turn's
   end (beat 160)`** — the ledger's rows are fixed-20-rune **chunks** of a source
   line, cut at arbitrary boundaries; the surface received them as one STREAM. Beat
   158 is the tail of a chunk cut mid-token (`…arm-sess-4ca` / `8cd29…`), in the middle
   of a markdown row the surface re-columns and truncates at the turn's row budget.
   Asking each chunk's **probe** (a 4-rune cut of its collapsed text) to sit on the
   grid asks for a layout the renderer never produced. Fixed by making the unit of
   the criterion the **RUN** — maximal stretch of ledger rows sharing
   `(src_line, kind)`, in order (`text_runs` / `run_text`) — and by comparing:

   * **provenance**: the run's text must be the source's own text, decoration set
     aside — contained in it, or a **prefix** of it, which is what the turn's row
     budget creates (`derives_from`);
   * **visibility**: the run's **characters**, in order, must be on the turn's grid —
     on one row, across a wrap, or (for a construct whose row breaks the surface
     owns, i.e. a re-columned table) across the flattened rows (`run_drawn`, the ONE
     predicate the acceptance arms also import).

   Every comparison runs on both sides through the same surface transform
   (`surface`/`surface_line`: a piped line's `|` is layout and is replaced by a
   space; the alignment row is syntax and is dropped; `bare()` strips the gutter and
   emphasis first, in the surface's own order — the gutter stands where a table
   row's empty leading cell was).

**Teeth, not loosening.** The characters other than layout are still compared, in
order and contiguously: `px_per_char` → `px_per_chars` breaks the sequence and is
refused (asserted in the unit test and exercised by the acceptance arms). Whitespace
alone is folded, and only because a re-columned table's spacing is the band's, not
the model's claim.

Also delivered:

* `tools/replay/test_verify.py` — new case 5 (`run-level provenance and raw-byte
  composition`), on the film's OWN measured beats when `REPLAY_SCRATCH` /
  `REPLAY_SESSION` are set, with the synthetic arms running unconditionally.
* `tools/replay/acceptance.sh` — the three corruption arms now **derive** their
  target from the film under test (a beat the checker accepts today) instead of
  hardcoding `172`/`0173.dump`, which named nothing in this film; arm C rewrites a
  row found (by the checker's own skeleton rule) to belong to the target run alone,
  and asserts the FAIL **names** the corrupted beat.
* `devdoc/DOC-INDEX.tsv` — this document's row (class `devdoc-report`).

## Not Done

* **The band's TRANSPOSED form is not reproduced.** A markdown table wider than the
  band is not drawn as columns at all: the band becomes labelled `field: value`
  blocks. No beat of this film exercised that form, so the checker accepts it only
  through the character-order arm; a film that exercised it would need a real
  sample before a rule could be written for it (and the report of such a run should
  say which beats they were).
* **No whole-tree Go run.** The change is Python only (`tools/replay/`), which no
  `go test` package covers; the repo's Go suite is untouched by this diff.

## Verification

- Build: n/a (no Go change). `python3 -m py_compile tools/replay/verify.py` — OK.
- Tests: `REPLAY_SCRATCH=… REPLAY_SESSION=… python3 tools/replay/test_verify.py` —
  **all pass** (case 5's 11 checks, including the real-film arms); the same script
  with no environment set — **all pass** (the real-film arms print a skip note).
- Ran: `python3 tools/replay/verify.py --out <session>/replay-out --scratch
  <session>/replay-scratch --session
  /home/user/.gopi/sessions/4ca8cd29-019c-4dda-a000-6dafd782bf91/session.jsonl
  --allow-sampled-frames`
- Result: exit **0**, `RESULT: all four claims measured true`, with
  `PROVENANCE OK: every beat's text is derivable from the source line it names (133
  exempt as structure-only)` and `VISIBILITY OK: 76 beats' text confirmed on their
  turn's own grid (133 exempt by kind)`. The same artifacts under the PRE-FIX
  checker (`git show HEAD:tools/replay/verify.py`) gave exit **1** with the two
  FAILs in the briefing, reproduced byte-for-byte: `FAIL: beat 170: … is not the
  activity row its source line's call composes` and `FAIL: beat 158: text '8cd29`
  | |---|---|--' is on no row of the grid at its turn's end (beat 160)`.
- Ran: `bash tools/replay/acceptance.sh $PWD <session>/replay-scratch
  <session>/replay-out <the session.jsonl> <session>/acceptance-final`
  (re-run at close-out; ~5 min, so it runs as a background job)
- Result: exit **0**, `ACCEPTANCE OK: clean film passes, every corrupted film fails`
  / `ACCEPT_EXIT=0`, each arm's own line observed — `control exit=0 (want 0) OK`;
  `text-edited exit=1 (want 1) OK` + `FAIL: beat 60: the 4 chunks of source line 204,
  joined (the stream the surface was handed), are not derivable from it; chunk
  'FABRICATED SENTENCE THAT WAS NEVER RECOR' is where the join breaks`;
  `grid-missing exit=1 (want 1) OK` + `FAIL: beat 61: no grid snapshot for the turn's
  last beat`; `grid-rewritten exit=1 (want 1) OK` + `FAIL: beat 54: … is on no row of
  the grid at its turn's end (beat 61)` then `names the corrupted beat 54: OK`.

## Evidence (self-filed)

* Pre-fix red, on the real artifacts: `PRE_FIX_EXIT=1` and the two FAIL lines above,
  printed by `python3 <session>/red/verify-prefix.py` (the `git show
  HEAD:tools/replay/verify.py` copy) over the same `replay-scratch`/`replay-out`.
* Post-fix green: `EXIT=0` / `RESULT: all four claims measured true` (same command,
  current tree).
* Unit test red arm: with the real artifacts, `old PROVENANCE probe of beat 158 is
  the mid-token cut got '8cd2'` was **asserted and failed** while being written —
  recorded here because the assertion was corrected (see below), not because it is
  green now.
* The corrected assertion, and why: the pre-fix provenance rule cut a **4-rune**
  probe from the chunk's collapsed text and required containment in the source — a
  real second failure mode (its collapse dropped `\n`, which markdown's `|---|---|`
  needs), but NOT the mechanism behind beat 158's message, whose text the pre-fix
  rule already matched. Asserting the real mechanism was the only honest option, so
  case 5 asserts provenance and visibility separately on the real run instead.
* Acceptance arm C: `grid-rewritten target: beat 60 in beat-0061.dump` then
  `names the corrupted beat 60: OK` — the arm names the beat it corrupted.

## Files

| file | what |
|---|---|
| `tools/replay/verify.py` | the criterion: run-level provenance + visibility, surface transform, raw-byte tool needles |
| `tools/replay/test_verify.py` | case 5: the two failure modes, on synthetic and real samples |
| `tools/replay/acceptance.sh` | derived targets; arm C names the corrupted beat |
| `devdoc/DOC-INDEX.tsv` | this document's row |
