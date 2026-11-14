<!-- doc-class: historical -->
# Task Report: the replay checker's provenance/visibility probes (task 01a0f886)

## Completed

**The defect, restated from the artifacts.** `tools/replay/verify.py` (landed
a7d3749e) reported three FAILs on a film that was correct — reproduced here on
the real run's own scratch (`~/.gopi/sessions/01a0f7a9-…/`), with the pre-fix
checker:

```
FAIL: beat 127: text '⚙ job {"action":"output","job_id":"job-2' not derivable from source line 854
FAIL: beat 172: text 'rt.  ## §3.11 MERGE ' not derivable from source line 929
FAIL: beat 172: text 'rt.  ## §3.11 MERGE ' is on no row of the grid at its turn's end (beat 173)
RESULT: FAILED
```

Three mechanisms, all of them the checker comparing the wrong string:

1. **The probe was cut at an arbitrary boundary.** `normalize(fragment)[:4]` cuts
   at gopi-replay's fixed `answerRunesPerBeat = 20` rune boundary, which is not a
   word boundary. Beat 172's fragment begins `rt.` — the tail of the word
   `report.` — so the four-rune probe is `'rt. '`, ending on a space.
2. **The haystack was the wrong string.** It was `json.dumps(line)`, the JSON
   ENVELOPE, where the text's own newline is the two characters `\n`. Measured on
   the real line 929: the longest matching prefix of the collapsed fragment is
   **3 of 18 runes**, and the rune that does not match is that space
   (`'rt. '` vs `'rt.\n'`). The instrument for this was landing mid-word AND the
   envelope: on a line whose text is not JSON-escaped at the cut, the same probe
   matches — which is why the failure looked arbitrary.
3. **`kind=tool_call` composed its text at draw time.** The ledger's row is
   `toolRowText(name, input)` — `⚙` + name + one argument digest — while the
   source line holds the call as JSON (`"name": "job"`, `"input": {…}`). For beat
   127 the drawn row is `⚙ job {"action":"output","job_id":"job-2"}`: the ledger
   and the JSON share the bytes, but the probe was cut against the envelope, so
   the fragment's own boundary decided whether the check passed.

**The fix — one principle, applied three ways: a fragment is compared against its
SOURCE, normalized the same way on both sides.**

- `probe_of(text)` returns the fragment's WHOLE text in comparable form (no
  fixed-width cut); `normalize()` is still used for containment, but never to cut
  a probe. `bare()` is the whitespace-preserving half, and the comment says why
  the two must not be confused.
- `source_text(obj, raw)` builds the provenance haystack from the line's own
  text-valued leaves (`message.content[*].text`, and `input`'s nested strings),
  not from `json.dumps` of the envelope.
- `tool_row_text(name, input)` / `tool_digest` reproduce `toolRowText`/`digestArgs`
  — the same `ToolGlyph` (read from `internal/lineedit/palette.go`, its single
  definition), the same `digestKeyOrder`, the same 58-rune cut, the same `…`
  marker, and the same redaction applied to BOTH sides. A tool call is therefore
  checked against its INPUTS by the renderer's own rule, rather than exempted:
  the `kind=tool_call` arm of provenance has teeth (an edited call fails).
- Visibility: `locate_on_wrapped_grid` locates the fragment's own lines on the
  grid, starting anywhere. The grid shows the band's WRAPPED rows, so a fragment
  that begins mid-line has its head at the end of one row and its tail at the
  start of the next — no contiguous run carries it, which is why containment and
  visibility are different questions. The chain rule (`_wrapped_chain`) requires
  each row to continue exactly where the previous one ended.
- The ONE named non-acceptance that remains: a fragment whose opening is a word's
  INTERIOR and which is pushed off the band by the row budget. Its source is
  checked (provenance, above) and its TAIL is required on the grid; the checker
  prints the beat, the tail length and the fragment length, so the shape is
  visible rather than silently passed. **On the two runs whose logs this task
  holds, the arm printed NOTHING** (`grep -c 'mid-word' e2e.log e2e2.log` → 0 in
  both): no fragment in that film was *both* mid-word *and* off-band, so the arm
  is a floor, not a measurement of this film. An earlier draft of this report
  credited it with "2 beats (84, 106)" — a figure from another session's scratch
  that no run in this task's logs printed; it is withdrawn.

**What did NOT change.** `cmd/gopi-replay`'s 20-rune fragmenting ledger and the
film are the artifact under test; the fragment list is RE-DERIVED here
(`split_runes`) and pinned against the Go constants by the regression test's
`case_constants`, so a moved constant fails the test rather than silently
loosening the check.

## Not Done

- The checker still tolerates a **mid-word fragment pushed off the band** (its
  tail on the grid is required; its head is not, because the renderer drew that
  word as a single run and the fragment's head is its interior). Closing that
  would need the renderer to record fragment offsets relative to the source text
  — a change to the artifact under test, outside this task's constraints.
- Two probe-shaped acceptances kept their pre-existing looser arms (the leading
  `…`-cut row arm; `STARTED:`/`COMPLETED` row kinds). They are not in this task's
  scope and were not widened.
- `scripts/replay/replay.sh --session` still requires a real `session.jsonl` in
  the directory it is given, while `gopi-replay` accepts a directory holding any
  single `.jsonl`. The acceptance below therefore names the session directory
  that carries `session.jsonl`. Left alone deliberately: replay.sh is outside the
  "verify.py and its tests/scripts" constraint.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi-replay ./cmd/gopi-replay/` → OK;
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK.
- Tests: `REPLAY_SCRATCH=<the scratch> python3 tools/replay/test_verify.py` → **all pass**,
  `exit 0`, 32 checks (`grep -c '^  ok'` = 32; the pre-fix rules are executed
  alongside the new ones, so the RED the fix claims is computed, not asserted).
- Ran: `bash scripts/replay/replay.sh --bin bin --session
  /home/user/.gopi/sessions/7bc9bd75-da04-4de1-9298-c8203376b6ca --scratch
  /home/user/.gopi/sessions/01a0f886-a372-7b7d-af12-031052fa2aea/e2e-scratch
  --out /home/user/.gopi/sessions/01a0f886-a372-7b7d-af12-031052fa2aea/e2e-out`
- Result: **`replay.sh exit=0`** — 201 beats, 181 distinct frames (20 dropped
  as identical), `[verify] RESULT: all four claims measured true`, and the two
  named arms: `PROVENANCE OK: every beat's text is derivable from the source
  line it names (143 exempt as structure-only)`, `VISIBILITY OK: 58 beats' text
  confirmed on their turn's own grid (143 exempt by kind)` — the whole run is
  quoted in the log named below. (The earlier draft of this line read "186
  distinct frames / 56 beats / mid-word fragments (2): beat 84, beat 106"; those
  three figures came from a DIFFERENT replay of the same session, run in another
  session's scratch, and no run whose log I hold printed them together with this
  task's final checker — so they are removed rather than restated. The frame
  count is measured, not narrative: `e2e.log` line `[replay] distinct frames:
  181 (dropped 20 identical)`, and the same figures close
  `replay.json`'s own arithmetic — `201 raw - 181 distinct = 20 dropped`. The
  SECOND end-to-end run of the same session (`e2e2.log`) collapsed differently —
  `192 (dropped 9 identical)` — because the distinct count depends on how many
  consecutive beats drew pixel-identical frames, while `raw_frames` (201) and the
  four claims are the same in both. So neither figure is THE film's; each belongs
  to the run that printed it.)
- Ran (the false-FAIL artifacts, re-derived): the same session replayed into a
  second scratch — `bash scripts/replay/replay.sh --bin bin --session
  /home/user/.gopi/sessions/7bc9bd75-da04-4de1-9298-c8203376b6ca --scratch
  …/e2e2-scratch --out …/e2e2-out` → `replay.sh exit=0`, `[verify] RESULT: all
  four claims measured true` (log: `~/.gopi/sessions/01a0f886-…/e2e2.log`).
- Ran (the checker alone, on that scratch, twice): `python3
  tools/replay/verify.py --out <out> --scratch <scratch> --session
  /home/user/.gopi/sessions/7bc9bd75-…/session.jsonl` → `PROVENANCE OK` and
  `VISIBILITY OK` in both legs (log: `…/legB-fixed-checker.log`). `/tmp`-less,
  `--raw-scratch` omitted, the two FAILs left in the second leg are the
  frame-count floor (11 frames in `frames/`: it is a *sampled* 11-frame
  directory, `--allow-sampled-frames` is the flag that stands it down, and
  replay.sh passes it — hence exit 0 there and 1 here).
- Ran (weapons): `bash tools/replay/acceptance.sh "$PWD" <source-scratch>
  <source-out> <session.jsonl> <workdir>` — the control (untouched artifacts)
  plus three corrupted films, each corrupted in a COPY under the workdir (the
  session's own directory, outside the repo — Rule 22).
- Result: `control exit=0`; `text-edited exit=1` with
  `FAIL: beat 172: text 'FABRICATED SENTENCE THAT WAS NEVER RECOR' not derivable
  from source line 929`; `grid-missing exit=1` with
  `FAIL: beat 173: no grid snapshot for the turn's last beat`; `grid-rewritten
  exit=1` with `FAIL: beat 168/169/170: … is on no row of the grid at its turn's
  end (beat 173)`; the script's own verdict: `ACCEPTANCE OK: clean film passes,
  every corrupted film fails`, `exit=0`.
- Ran (RED proof on the real artifacts): the pre-fix `verify.py`
  (`git show a7d3749e:tools/replay/verify.py`) on the SAME scratch → **exit=1**
  with exactly the three FAILs above; the fixed checker on the same input →
  **exit=0**.
- Commit: see the branch tip (this report lands with it).

## Evidence (self-filed)

- **Where these logs live.** Every path named below is under this run's own
  session directory: `/home/user/.gopi/sessions/01a0f886-a372-7b7d-af12-031052fa2aea/`.
  The false-FAIL repro reused ANOTHER session's scratch
  (`~/.gopi/sessions/01a0f7a9-3281-7276-84a2-ba5b2c54fd26/replay-{scratch,out}`)
  read-only: those artifacts were produced by a replay of the SAME source session
  (`7bc9bd75-da04-4de1-9298-c8203376b6ca` — `replay.json`'s `session_ref` names
  its 1088 records / 844463 bytes, and the hash of that `session.jsonl` differs
  from the scratch session's own jsonl), so pointing the checker at it is
  legitimate evidence, and its `beats.tsv` is the ledger carrying the mid-word
  fragment.

- `git show a7d3749e:tools/replay/verify.py` on the real scratch → exit 1, the
  three FAIL lines quoted above (log:
  `~/.gopi/sessions/01a0f886-a372-7b7d-af12-031052fa2aea/redproof/old-verify.log`).
  The copy that produced that log is that commit's file byte-for-byte —
  `git show a7d3749e:tools/replay/verify.py | cmp - …/redproof/verify.py` → no
  output, exit 0.
- Fixed `tools/replay/verify.py` on the same scratch → exit 0, `RESULT: all four
  claims measured true`.
- `bash scripts/replay/replay.sh …` → `[replay] done: …/e2e-out`, `replay.sh
  exit=0` (log: `…/e2e.log`).
- `bash tools/replay/acceptance.sh …` → `ACCEPTANCE OK: clean film passes, every
  corrupted film fails`, exit 0 (log: `…/accept2.log`; re-run on the mid-word
  ledger with the film's correct session → same four verdicts, `acceptance.sh
  exit=0`, log `…/accept3.log`).
- `REPLAY_SCRATCH=<scratch> python3 tools/replay/test_verify.py` → `all pass`,
  `exit 0` (32 checks, `grep -c '^  ok'` = 32 — three of the arms: `old rule cuts a space-terminated probe`,
  `measured longest matching prefix is 3 of 18`, `the rune that does not match is
  the space before the envelope's \n`).
- The figures this report still states (`3 of 18 runes`, `'rt. '` vs `'rt.\n'`,
  201 beats, 181 distinct / 20 dropped, `143 exempt as structure-only`,
  `58 beats' text confirmed`, `exit 0`, 32 test checks) are read from the
  artifacts and this run's logs, not inferred; every one of them was re-grepped
  out of the log it claims a home in before this line was written.
