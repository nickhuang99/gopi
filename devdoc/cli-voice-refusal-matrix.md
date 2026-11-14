<!-- doc-class: historical -->
# CLI voice_input — the contradictory-combination matrix is refused at parse time

Task: `01a113e468467a54978c7baeb86270dd` (branch `task/01a113e468467a54978c7baeb86270dd`).
Session evidence: `/home/user/.gopi/sessions/01a113e4-6846-7a54-978c-7baeb86270dd/artifacts/`.

## Completed

### 1. The matrix — one table, both halves (`cmd/agent/cli_voice_refusal_test.go`, new)

`voiceContradictoryCombinations()` is the single place a contradictory pair is
enumerated, and two tests drive it from both directions:

- `TestVoiceParseRefusesContradictoryCombinations` — 28 rows, each asserting the
  message by TEXT (the flag name **and** the reason), not `err != nil`. A bare
  error check would pass on "invalid arguments" while leaving the operator exactly
  as stuck as before; the value of a refusal is that it is actionable.
- `TestVoiceParseDefaultsAreTheCaptureShapeAndItsFlags` — 11 rows of combinations
  that must stay **accepted**. A refusal layer that over-triggers is the same
  defect in the opposite coat, and the default shape (bare invocation = capture)
  is the one it would break first.
- `TestVoiceParseRefusalDoesNotCreateTheNamedFile` — the parser names `--record` /
  `--out` files; a refused invocation must leave both the named outputs and the
  named input untouched.

### 2. The audit — three real gaps found, all in this change

The matrix was written first and driven against the unmodified parser, so its
failures were findings rather than confirmations. Three were real:

1. **`voiceRunShape` never named `--client`.** It fell through to
   `file-transcription`, so a capture-only flag refused on a client run told the
   operator they were transcribing a file when they were recording from a
   microphone. Fixed by adding the `voiceClientShapeName` case (named constant,
   Rule 1/13) — the callers that reach it are the capture-only refusal arms.
2. **Two refusal arms were DEAD CODE.** `--out` + `--list-devices` and `--record`
   + `--list-devices` were shadowed by their general clauses, which matched first
   and said only "it records nothing" — the specific, correct sentence never
   printed. Fixed by collapsing the two flags into one loop whose `switch` tests
   `--list-devices` FIRST; the arm order is now the point, and both messages now
   name the listing.
3. **Three combinations had no refusal at all**, each a silent no-op of the kind
   Rule 15 forbids:
   - `--chunk N` with `--client` or `--list-devices` — it windows a file
     transcribed **here**; the client's audio is windowed on the far side and the
     listing transcribes nothing. Accepted, it reported a chunked pass that never
     happened.
   - `--models DIR` / `--runtime PATH` with `--client` or `--list-devices` — these
     are the local weights. A client run opens none (the host it posts to owns its
     own), and a listing opens no model; an operator who redirected the weights
     and got the defaults in use had been told something false.
   - `--display DISPLAY` without `--self` — the display is the screen `--self`
     types into, so on a run that never types it is read by nothing and the
     operator's chosen display is silently abandoned.

### 3. What was found already correct (the honest "no gap" half)

These pairs already had a named, parse-time refusal and were left untouched:
`--client`+`--server`, `--stdio`+`--listen`, an audio file on a `--server` /
`--list-devices` run, `--client` with an audio file, `--seconds` / `--device` /
`--record` on a shape that records nothing, `--server-url` on a shape that never
posts, `--self` on `--server` / `--stdio` / `--list-devices`, `--chunk` on the bare
capture, and the valueless-flag guards.

**One row was moved OUT of the matrix rather than fixed, and it is a finding:**
`--client --list-devices` is **legal** — it prints the device table and exits before
any capture, and the help page advertises exactly that spelling. My first draft
listed it as contradictory; the test reddened on the unmodified parser and the
parser was right, so the row became an ACCEPTED assertion. A refusal there would
have broken a documented shape.

### 4. Documentation updated in the same change

`cmd/agent/cli_voice.go`'s usage text now carries the reach of `--chunk`,
`--models`/`--runtime` and `--display` (refused where they mean nothing);
`CONFIG.md`'s Voice-input section states the parse-time contract, names the matrix
test as its check, and lists the refused pairs.

**A usage-EXAMPLE was added and then removed — the reason is worth keeping.**
The first revision also put one executed refusal example in the `Examples:`
block (`gopi tool voice_input --chunk 15`). The g6 help-surface group derives its
form inventory from every `gopi ` line on a page, so that example created a NEW
advertised form the inventory did not list and **widened a pre-existing red by one
row** (pre-existing: commit `0407b4eff` records "voice_input advertised but
unregistered"; the two rows it names, the bare form and `--seconds 5`, exist in
master's usage). Two fixes were possible — register the form in the inventory
(a SYSTEM-layer gate, Rule 28, plus an inventory-doc row) or drop the example.
Dropping won: the flag's own description prose already states the reach
("refused on the bare capture … and with `--client` / `--list-devices`"), and
**prose creates no form**, so the example was redundant as well as expensive.
Verified after removal: the gate no longer names `--chunk 15`, and the remaining
rows are exactly master's own. Lesson: on a page whose forms are machine-derived,
an example is not free documentation — it is a new claim on the inventory.

## Not Done

- **Not a whole-tree suite run.** `go test ./cmd/agent/` is the branch-scoped
  package this diff touches; the whole tree belongs to the off-peak heavy path
  (Rule 27 / smokegate). No other package's files were modified.
- **`--client --list-devices --seconds 5` is accepted and unchecked.** `--seconds`
  is legal on a client run, and the listing returns before recording, so the value
  is unused — but whether a duration *should* be refused on a listing is a design
  question about an advertised shape, not a contradiction I can settle from the
  help text alone. Recorded here rather than guessed at.
- **No real capture was run** — except the ONE deliberate control above. This host
  is HP (client), and the task's own constraint is that the refusal path must not
  touch the microphone; the bare-invocation control existed to prove the refusal
  sensor fires, it did reach a real capture, and it is disclosed here rather than
  left out: it ran under `GOPI_HOME` pointed at a scratch profile, recorded 10s of
  PCM, exec'd `parecord`, and was terminated by `timeout 20`. Its trace is
  `artifacts/proof/bare-control.log`.
- **No push / no merge.** The deliverable is the commit on this branch.

## Verification

- Build: `go build ./...` → OK; `CGO_ENABLED=0 go build -o
  <session>/artifacts/gopi-final ./cmd/agent/` → OK (this is the binary every
  measurement below used).
- Tests: `go test ./cmd/agent/ -run Voice` → `ok github.com/nick/gopi/cmd/agent` (green,
  after the three fixes; the same command was RED before them, naming exactly the
  rows above). Full package: see `## Evidence`.
- Ran: the real binary, 28 times — **one per row of the matrix, the row list
  parsed out of the test file itself** (`artifacts/proof/sweep_matrix.py`), so the
  evidence cannot drift from the table. Verbatim invocation text, exit code and
  refusal message per row: `artifacts/proof/matrix-full.tsv`. Example, verbatim:

  ```
  $ /home/user/.gopi/sessions/01a113e4-6846-7a54-978c-7baeb86270dd/artifacts/gopi-final \
        tool voice_input --client --chunk 15
  gopi tool voice_input: --chunk windows a file that is transcribed HERE, but this is
  a --client run — it transcribes no file: the window belongs to `gopi tool
  voice_input <audio-file> --chunk 15`, or to the server the client posts to
  exit=1
  ```

  (That binary was built `CGO_ENABLED=0 go build -o …/gopi-final ./cmd/agent/` from
  the branch tip `63aa370b9`, whose only difference from `a89c579f5` is the
  devdoc's own text — the code is identical.)

- Result: **28/28 exit 1 with a message naming the flag and the shape**
  (`matrix-full.tsv`, column `named_refusal` — 0 rows are "(NO NAMED REFUSAL)").
  Each row ran in a FRESH EMPTY working directory and every one of those
  directories is still empty afterwards (`cwd_after=EMPTY`, 28/28) — so the two
  paths the refused invocations NAMED (`keep.wav`, `kept.wav`) were never created,
  and no other recording artifact appeared either.
- **Non-touch proof (machine-level):** each of the 28 ran under
  `strace -f -qq -e trace=execve,openat -o trace.log`, and across all 28 traces the
  child-exec count of `arecord`/`parecord` is **0** (`matrix-full.tsv` column
  `recorder_execs`).
  **The sensor that discriminates is `execve`, and it was shown to fire on a
  capture that really happened:** a bare `gopi tool voice_input` (the capture
  default) execs `/usr/bin/pactl` (`list short sources`, `get-default-source`),
  `/usr/bin/arecord -l`, `/usr/bin/pactl info`, then `/usr/bin/parecord
  --device=@DEFAULT_SOURCE@ --raw --rate=16000 …` and captures 320000 bytes
  (`artifacts/proof/bare-control.log`) — **2** lines match the recorder regex, the
  same regex that reads 0 on all 28 refusal rows. So "0 recorder execs" means the
  refusal happened BEFORE the recorder was ever constructed, not that the trace
  missed it.
  **The `/dev/snd` column is reported but is NOT doing discriminating work on this
  host, and saying so is part of the evidence:** that same successful capture shows
  **0** `/dev/snd` opens too, because `parecord` is a PulseAudio client — it reaches
  the sound server over a socket, not the device nodes. (`arecord -l` does open
  `/dev/snd`: **36** opens, measured.) A column that reads 0 on both the refused and
  the capturing path distinguishes nothing; the execve count is the measurement of
  record here.
  (Both recorder binaries are installed: `/usr/bin/arecord`, `/usr/bin/parecord`.)
- **One correction this sweep produced, recorded rather than hidden:** the first
  sweep was hand-listed and covered **27** rows while the matrix holds **28** —
  `--list-devices --seconds 5` had no binary evidence. The row list is now PARSED
  OUT OF THE TEST (`sweep_matrix.py` reads `voiceContradictoryCombinations()` and
  resolves each `voiceFlag*` constant to its literal, following the two that alias
  another verb's constant), so the evidence cannot drift from the table again. The
  same check corrected the counts stated above (`25`/`10` → measured `28`/`11`).
- Commit: `a89c579f5` (the code), `63aa370b9` (the branch tip — devdoc text only).

## Evidence (self-filed)

| claim | instrument | reading |
|---|---|---|
| 28/28 refused, named message, exit 1 | real binary `artifacts/gopi-final` (built from `63aa370b9`), row list parsed from the test, `artifacts/proof/matrix-full.tsv` | 28 rows, every one `exit=1` + a `gopi tool voice_input: …` line; 0 rows without one |
| matrix row count (28) and accepted-row count (11) | `go test ./cmd/agent/ -run TestVoiceParse…Refuses… -v` / `…Defaults… -v`, counting PASS lines | `28` and `11` subtests PASS |
| no recording spent | each row in a fresh empty cwd, `ls` after | `cwd_after=EMPTY` for 28/28 |
| capture never started | `strace -f -e trace=execve,openat` per row, 28 traces on disk | `recorder_execs=0` for 28/28 |
| the recorder sensor FIRES on a capture that really happens | bare `gopi tool voice_input` under the same trace (the capture default) | `2` lines match the recorder regex; it execs `/usr/bin/pactl`, `/usr/bin/arecord -l`, `/usr/bin/parecord …`; stderr says `captured 320000 bytes (10.0s of 16000Hz mono PCM16)` |
| `/dev/snd` does NOT discriminate on this host — stated, not hidden | same control trace | `/dev/snd` opens = **0** on the successful capture too (`parecord` is a Pulse client); `arecord -l` alone opens 36 |
| matrix red before the fix, green after | `go test ./cmd/agent/ -run Voice` at both revisions | red named 6 failing rows (2 dead arms, `--client` shape name, 3 missing refusals); green after — observed EARLIER in this same run (the red half cannot be re-run now: the test file imports `voiceClientShapeName`/`voiceListDevicesShapeName`, which the pre-fix parser does not define, so a pre-fix compile of THIS test is impossible by construction) |
| the fixes are confined to the voice surface | `git diff --stat 3255973cc a89c579f5` | `CONFIG.md`, `cmd/agent/cli_voice.go`, `cmd/agent/cli_voice_refusal_test.go`, `devdoc/DOC-INDEX.tsv`, `devdoc/cli-voice-refusal-matrix.md` |
| the whole Voice surface (not just the new file) is green | `make test-budget PKGS='./cmd/agent'` stream `run-1791346098-647908.json` | 133 top-level `TestVoice*` tests (228 names including subtests) appear in the stream and **0** carry `"Action":"fail"`; the run then continued past them and was stopped (whole-package = off-peak path, Rule 27) |
