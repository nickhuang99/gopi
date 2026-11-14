<!-- doc-class: historical -->
# Task Report: the replay classifies the terminal it is holding — one entry point, two callers

Task `01a0fa57-4367-7836-b26a-fe7c2195ccc9` (worker, branch
`task/01a0fa5743677836b26afe7c2195ccc9`).

## Completed

**The defect, stated exactly.** `cmd/gopi-replay` draws the product's own chat
surface onto a real tty. It had the terminal **open** (`lineedit.Open(os.Stdin,
out)`) and then classified its colours with the env-only resolver
`lineedit.PaletteForEnvCapability(false, out)` — a function that reads `TERM` and
`COLORTERM` and nothing else, and **never probes**. `gopi chat` does the
opposite: `CapabilityForEnv` → `term.ProbeCapability()` → `WithMeasurement` →
`PaletteForCapability(measured)`. The film's launcher
(`scripts/replay/replay.sh`) exports `LC_ALL`/`LANG` and no `TERM`/`COLORTERM`, so
the declaration landed on `DepthBasic`, the table fell to `BasicPalette`, and a
24-bit terminal received an 8-colour film.

1. **`internal/lineedit` gained the ONE terminal-facing entry point** —
   `CapabilityForOpenTerminal(declared Capability, term *Terminal)
   (Capability, CapabilityMeasurement)` (`colour.go`). It owns: the gate (ask only
   when `ColourOn`, because with colour off no query byte may reach the operator's
   screen — the same condition `CapabilityForEnv` uses to skip its declaration
   reads), the probe call, and the fold (`WithMeasurement`, the ONE place
   measurement meets declaration — Rule 13). The declaration is the **caller's**,
   so a run still resolves the environment once (Rule 2: a second resolution would
   print the palette-downgrade notice twice, into the terminal the operator is
   reading).
2. **The console converges onto it** — `cmd/agent/lineedit_wire.go` no longer
   spells `term.ProbeCapability()` / `declared.WithMeasurement(...)`; it calls
   `lineedit.CapabilityForOpenTerminal(declared, term)`. The chat surface's
   behaviour is unchanged (same sequence, same notice, same one resolution).
3. **The replay converges onto it** — `cmd/gopi-replay/main.go` opens the terminal
   **first**, then classifies through the named function
   `replayCapability(sink, term)`, and prints `lineedit.CapabilityNotice` when the
   terminal overruled the environment (the console's own sentence, so the film's
   stderr says what the console's would).
4. **The film's record now carries the decision** —
   `recordReplayCapability(dumper, capability)` writes the same four fields
   (`capability=`/`depth=`/`declared_depth=`/`palette=`, plus the probe summary in
   the JSON twin) that `gopi chat --dump-screen` writes. Without it the acceptance
   ("observe that the capability came from measurement") had no artifact to be read
   from: the replay armed its sink and never reported to it.
5. **Two guards, both red-proved.**
   - `cmd/gopi-replay/capability_parity_test.go` — the epic's acceptance test: on
     **one** real pty, with `TERM`/`COLORTERM` **unset**, the replay's
     classification equals the console's (depth, evidence source, and the table
     drawn), and the replay's depth source is `measured` (arm A: colors=256 + RGB;
     arm B: colors=256 + RGB refused; arm C: silent — the declaration stands, so a
     fix that invented a measurement would fail here).
   - `cmd/agent/terminal_capability_wiring_test.go` — a source guard on the
     console's call site: it must call the entry point, and must NOT contain
     `.ProbeCapability()` or `.WithMeasurement(`. A private copy of the sequence is
     exactly what the replay had copied, and a behaviour test cannot see it.

## Not Done (honest)

- **A NEW program that holds a tty and calls `CapabilityForEnv` is not caught by
  any gate.** The guards pin the two known call sites (the console and the
  replay); a third surface would have to be added to the parity test by hand.
  What the type system gives for free is weaker than a gate: a capability from
  `CapabilityForEnv` carries **no** measurement, so a report built from it can
  never claim `measured` — it will say `declared`/`default`, which is the
  observable that made this defect findable.
- **`internal/lineedit` has two pre-existing reds on this host, unrelated to this
  change, and one of them is a flake.** `TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow`
  fails deterministically on unmodified `HEAD` (verified by reverting only
  `colour.go`: still FAIL, 4/4 runs); it is now **filed, not left for the next
  reader** — task `01a0fade-c42a-7d72-bafd-27a3c99fb34b` (tag `bug`, priority 1)
  carries the verbatim repro, the determinism measurement, and the isolation
  evidence, i.e. it goes to its own worktree instead of being parked here
  (Rule 26). `TestProbeMeasuresDepthFromRealAnswers` fails 3/10 on `count=10`
  with `RGB=unknown` and a lost DA1 answer — the fake responder races the
  probe's bounded read window; also fails on unmodified `HEAD`, 3/10. Neither is
  in `testdata/flakes.tsv`, so neither is a *known* flake by the registry's own
  standard — that classification is a different task and it is **not** this one.
  `cmd/agent`'s `TestPtyMarkerColumnDump` is red on unmodified `HEAD` too.
  Nothing in this change touches any of the three.
- `cmd/gopi-replay/colour_sink_test.go` (the previous task's guard) still asserts
  `PaletteForEnvCapability` on a pty — that is now **not** the path the replay
  takes. It was left untouched on purpose: it pins the *sink* decision
  (`ColourWanted`'s "not a terminal is never coloured"), which is a different
  question from this one, and its assertion is about the resolver's contract, not
  about the replay's call site.

## Verification

- **Build**: `go build ./...` → OK (exit 0).
- **Tests** (final, after the fixture correction below): `go test -count=1 ./cmd/gopi-replay/`
  → **ok 0.353s**; `go test -count=1 -run 'TestCapabilityForOpenTerminal|TestProbeMeasuresDepth|TestProbe' ./internal/lineedit/`
  → **ok 3.627s**; `go test -count=1 -run 'TestChatCapabilityWiringAsksThroughTheOneEntryPoint' ./cmd/agent/`
  → **ok 5.617s**; `go build ./...` → BUILD_OK.
  Budget run: `make test-budget PKGS='./internal/lineedit ./cmd/agent ./cmd/gopi-replay'`
  → `cmd/gopi-replay` PASS; `internal/lineedit` and `cmd/agent` carry the
  pre-existing reds named under **Not Done** (`TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow`;
  `TestChatCLIVocabularyIsClosedEveryAcceptedFlagParses/--resume` — `close |1: bad file
  descriptor`; `TestCodeSearchBackendFindsDefinition` — the ctags index). None of the
  three is touched by this change, and all three reproduce on unmodified `HEAD`.
- **The red half of the regression test** (pre-fix body at the same call site —
  the env-only classification, `_ = term`), re-proved **with the corrected
  fixture**, `9 runs → 9 red`:
  ```
  go test -count=1 -run TestReplayCapabilityEqualsChatCapabilityOnTheSameTerminal ./cmd/gopi-replay/
  --- FAIL: TestReplayCapabilityEqualsChatCapabilityOnTheSameTerminal (0.16s)
      --- FAIL: …/a_terminal_that_answers_256_colours_and_24-bit (0.02s)
          capability_parity_test.go:309: the two surfaces disagree about the depth: replay=basic chat=truecolor
          capability_parity_test.go:313: the two surfaces disagree about WHICH EVIDENCE settled the depth: replay="default" chat="measured"
          capability_parity_test.go:319: the two surfaces draw different tables on the same terminal: replay="basic" chat="hermes"
          capability_parity_test.go:325: the replay's depth came from "default", want "measured" …
          capability_parity_test.go:330: the replay drew "basic", want "hermes" …
          capability_parity_test.go:342: the depth was measured but the capability carries no measurement: probe=none … bytes=0
      --- FAIL: …/a_terminal_that_answers_256_colours_and_refuses_24-bit (0.01s)  (same six lines, chat=256 / chat="palette256")
  FAIL   (exit 1)
  ```
  Full quote: `…/evidence/r31-prefix-red.txt`. With the fix in place the same
  command is **PASS** (arms A, B, C green), **31 consecutive runs, 0 red**.
- **The red half of the wiring guard** (console body replaced by the private
  probe): `--- FAIL: TestChatCapabilityWiringAsksThroughTheOneEntryPoint` naming
  `.ProbeCapability()` and `.WithMeasurement(`; restored → PASS.

### The acceptance test was FLAKY, and the flake was the FIXTURE's (cf. commit 91413cd9)

Measured after the first commit (`dee606f3`): `6 runs → run 1 FAIL, runs 2..6 ok`,
red at `capability_parity_test.go:247` as `replay=256 chat=basic` — with nothing
wrong in the product. The mechanism was read, not guessed, with a temporary
diagnostic that printed **both** arms' measurements over 12 iterations:

```
iter  8 replay: depth=basic src=default | probe=partial cpr=yes da1=\e[?1;2c osc11=- colors=0   rgb=no bytes=28
        chat  : depth=256   src=measured| probe=full    cpr=yes da1=\e[?1;2c osc11=- colors=256 rgb=no bytes=82
```

The fixture opened two `lineedit.Terminal` on **one** pty: both probes wrote into
and read from one input queue, consumed each other's answers, and a reply arrived
split — so one probe read a truncated `colors` reply as `colors=0`. Two
corrections, each the shape a real emulator has:

1. **one answer table, TWO descriptors** (`newParityTerminal`) — which is also
   the production shape: `gopi chat` and `gopi-replay` are two processes on the
   same terminal hardware, never two readers of one queue;
2. **the responder answers a COMPLETE question batch in ONE write**
   (`parityResponder.batchComplete`, `parityBatchBound`) — necessary because the
   probe ends its window the moment the answers are in, so a reply still in
   flight simply was not there when the window closed.

Measured: before the correction `20 runs → PASS=18 FAIL=2`; after it
`31 runs → 0 red`. A test whose red comes from its own race asserts nothing
about the fix, so this correction is part of the deliverable rather than
housekeeping. **Summary: `…/evidence/r31-fixture-race.txt`.**


### Ran: the real binary on a real pty (Rule 31)

Driver: `/home/user/.gopi/sessions/01a0fa57-4367-7836-b26a-fe7c2195ccc9/live_replay2.py`
(python `pty.openpty()`, a fake terminal that **answers** the probe, and a
background acker playing the film driver's role). `TERM` and `COLORTERM` are
**popped from the child's environment** — the launcher's own condition.

```
python3 …/live_replay2.py …/live-answer …/live-session answer
  {"exit": 0, "mode": "answer"}
```

**Result — post-fix, terminal answers** (`…/live-answer/`):

- stimulus on the wire proves the terminal was asked:
  `b'\x1b[?2004h\x1b[6n\x1b[c\x1b]11;?\x1b\\\x1bP+q636F6C6F7273\x1b\\\x1bP+q524742\x1b\\…'`
- stderr: `[gopi] terminal colour measured as truecolor (TERM/COLORTERM declare
  basic) — the terminal's own answers win: probe=full cpr=yes da1=\e[?1;2c
  osc11=rgb:0000/0000/0000 colors=256 rgb=unknown bytes=67 unread=0 elapsed_ms=0`
- artifact record line: `… frames=… capability=measured depth=truecolor
  declared_depth=basic palette=hermes`
- **the pixels the product drew**: `\x1b[38;2;255;248;220myou>` and
  `\x1b[38;2;255;215;0msay` — 24-bit chocolate/gold, i.e. `HermesDarkPalette`, not
  the 8-colour table.

**Result — post-fix, terminal silent** (`…/live-silent/`): `capability=default
depth=basic declared_depth=basic palette=basic` and the stream carries `\x1b[32m`
/ `\x1b[33m`. The fallback is the declaration's floor, and it is *reported* as
default — silence never becomes a measurement.

**Result — pre-fix binary (same driver, same answering terminal)**
(`/tmp/pre-bin/gopi-replay`, built from `HEAD`):
the record line has **no capability fields at all**, stderr carries **no notice**,
the master sees **no query** (`b'\x1b[?2004h\x1b[1;24r\x1b[1;1H…'` — the probe
queries are absent), and the payload is 8-colour only (`\x1b[32m`/`\x1b[33m`),
while the post-fix run on the same terminal drew `\x1b[38;2;…`. That is the defect
and the repair, side by side, on one machine.

## Evidence (self-filed)

- `go build ./...` → exit 0 (run after every edit; last at the post-fix restore).
- `go test -count=1 ./cmd/gopi-replay/` → **ok 0.353s** (includes the parity test).
- **Re-run on the FINAL code (after the fixture correction, commit 91413cd9)**,
  same driver, same answering fake terminal, `TERM`/`COLORTERM` unset
  (`…/live-r31-post2/`):
  ```
  python3 …/live_replay3.py /tmp/rp-bin/gopi-replay.v3 …/live-r31-post2 live-r31-post2/session answer
    {"exit": 0, "mode": "answer", …}
  surface.dump line 1: … capability=measured depth=truecolor declared_depth=basic palette=hermes
  master-bytes.log   : {\x1b[6n: 1, \x1b[c: 1, \x1b]11;?: 1, \x1bP+q636F6C6F7273: 1, \x1bP+q524742: 1}
                       24-bit runs: 38 | 8-colour (\x1b[32m/\x1b[33m): 0
  stderr             : probe=full cpr=yes da1=\e[?1;2c osc11=rgb:0000/0000/0000 colors=256 rgb=yes bytes=84
  ```
  The terminal was **asked** (five queries on the wire), the depth source is
  `measured`, and the product drew 24-bit SGR and no 8-colour SGR at all.
- `go test -run 'TestReplayCapability' -count=1 ./cmd/gopi-replay/` with the
  pre-fix body → `FAIL` (exit 1), six assertions per measured arm, quoted above;
  `9 runs → 9 red` with the corrected fixture.
- `go test -run 'TestChatCapabilityWiringAsksThroughTheOneEntryPoint' -count=1 ./cmd/agent/`
  with the private probe reinstated → `FAIL`, naming both banned shapes; restored → `ok`.
- Pre-existing redness, measured on unmodified `HEAD` by reverting only the
  changed file: `git checkout HEAD -- internal/lineedit/colour.go` →
  `TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow` FAIL, and
  `TestProbeMeasuresDepthFromRealAnswers` 3/10 FAIL on `-count=10`.
- Live runs: `…/live-answer/surface.dump` line 1 carries `capability=measured
  depth=truecolor declared_depth=basic palette=hermes`; `…/live-r31-post2/surface.dump`
  carries the same on the final code; `…/live-prefix/surface.dump`
  carries no capability field; `…/live-silent/surface.dump` carries
  `capability=default depth=basic … palette=basic`.

## Commit

`<hash> feat(replay): classify the terminal the film is drawn on, through the one entry point the console uses`

---

## The operator's design questions

**Why do two paths exist?** Because the *sequence* never had an owner. The
declaration half was owned from the start (`CapabilityForEnv`, one place, with
`PaletteForEnvCapability` as the "startup, one call" convenience). The measurement
half was added later by the chat epic and was written **where it was needed** —
inside `newChatInputWithPalette`, next to the `lineedit.Open` it follows —
because that is the only place a probe could run before the first frame, and the
chat surface was the only caller that existed then. Nothing was wrong in either
file: `lineedit_wire.go` had the terminal and used it; `cmd/gopi-replay` had the
terminal, had no probe to call, and reached for the one resolver the package
*offered* for a startup decision. **The package offered the wrong one to the
caller it did not know about** — that is the whole shape of the defect, and it is
Rule 13/14's exact failure mode: a sequence that lives at a call site is a
sequence the next caller will not find.

**Should the two be merged?** **Yes at the level of the sequence; no at the level
of the callers.** What must be one thing is the *decision*: gate, probe, fold,
precedence — now `CapabilityForOpenTerminal`, called by both. What must stay two
things is the *surface*: `gopi chat` has a REPL, subscribes signals, loads history,
installs the injector and the dump tee, and needs the notice in its own voice;
`gopi-replay` has a film driver, a ledger and a beat protocol. Merging the
callers would mean one function with two sets of unrelated wiring behind a flag —
the shape this repository bans. The test that pins the merge is the equality
assertion, not a shared call site.

**What does it cost?**

- **Latency at startup**: the probe is bounded by `probeWindowMS` (120 ms) and
  ends EARLY once the answers are in — measured `elapsed_ms=0` in both live arms,
  in a 1.5 s run. On a terminal that answers nothing the whole window is spent
  (arm C / `live-silent`), which is the price of the honest fallback.
- **Bytes on the operator's screen and answers off their input path**: four
  queries, before the first frame, with everything else prefetched and replayed in
  order (`probe.go`'s rule 2). The gate keeps that at zero when colour is off.
- **One thing a caller can no longer do quietly**: choose its own evidence. A
  caller that wants the env-only answer must now *name* it (`CapabilityForEnv`),
  which is exactly the sentence a reviewer needs to see.
- **A new risk this introduces**: a surface with a tty whose reader is not the
  operator will now emit query bytes. Its remedies are named (`--no-color`, or
  `NO_COLOR`), and the silent arm shows what happens when the peer does not
  answer: nothing is invented, the floor is drawn and `default` is reported.

**The guard against this happening again** is three-layered, and layered on
purpose:

1. **Equality + provenance** (`cmd/gopi-replay/capability_parity_test.go`, real
   pty, `TERM`/`COLORTERM` unset, red-proved) — the property, not the call.
2. **The console's wiring guard** (`cmd/agent/terminal_capability_wiring_test.go`,
   red-proved) — the source-level half: the sequence may not be re-inlined.
3. **The report field** (`capability=measured|declared|default` on both surfaces'
   artifacts) — the observable an operator or an agent can read off a finished
   run, which is what turned this from an argument into a measurement.
