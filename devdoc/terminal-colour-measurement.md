<!-- doc-class: historical -->
# Task Report: terminal colour capability is MEASURED, not declared

Task `01a0f720-97e8-75a8-a9d2-1995030571ba` (branch `task/01a0f72097e875a8a9d21995030571ba`).

## Completed

The colour decision used to be read out of the ENVIRONMENT: `TERM` carrying
`256color`, `COLORTERM` saying `truecolor`. A string in the environment is not a
fact about the terminal — it survives ssh, it is inherited through multiplexers,
and it can be wrong in both directions. The terminal is now ASKED.

- **`internal/lineedit/probe.go` (new)** — the bounded capability probe. Writes
  four queries and reads the answers inside a bounded window
  (`probeWindowMS`/`probeSliceMS`/`probeMaxBytes`/`probePartialBytes`), on the
  same descriptor the editor reads. `CapabilityMeasurement` reports each answer
  verbatim beside the verdict drawn from it; `Summary()` renders one
  `name=value` line of evidence.
- **The queries** (composed in `internal/sgr`): DSR cursor-position (liveness),
  DA1 (identity; `22` = "ANSI colour exists"), OSC 11 (a positive truecolour
  signal), and **XTGETTCAP** `DCS + q` for terminfo `colors`/`RGB` — the only
  query that answers DEPTH with a number.
- **`internal/sgr/sgr.go`** — the vocabulary: XTGETTCAP composer (hex-encoding
  the capability name), the private-prefix set, the query-half constants. Also
  the ERASED `DCSSeparator`: see the bug below.
- **`internal/lineedit/colour.go`** — `Capability` now holds the DECLARATION
  (`declaredTruecolor`/`declaredPalette256`/`declaredDepth`) and the MEASUREMENT
  (`measured`), and `WithMeasurement` is the ONE place they meet:
  measurement-decided when the probe read something a depth can rest on
  (`CapabilityMeasurement.Measured()`), declaration otherwise. `PaletteFor`-side
  table selection, `DepthName`, `CapabilityReport`, `CapabilityNotice`.
- **The keystroke question, answered by construction** — `probe.go` rule 2: every
  byte the probe read that is not an answer is PREFETCHED and REPLAYED in order
  by `ReadEvent` (`Terminal.prefetch`/`takePrefetch`/`addPrefetch`,
  `Terminal.Prefetched()`), so a key the operator pressed during the probe
  reaches the decoder exactly as typed. `Editor.Run` calls `continueProbe()`
  first, and the probe is a `sync.Once` per terminal.
- **Wiring** — `lineedit_wire.go` probes at `Open` (before the first paint) and
  re-derives the table; `cli_chat.go` takes the MEASURED table for the surfaces
  the REPL owns itself (banner, band rows), so the run has ONE colour decision;
  `chat_dump.go` records it.
- **The artifact** — `uiclient.CapabilityReport` + `SetCapabilityReport`: the
  record line gains `capability=` (which evidence decided), `depth=`,
  `declared_depth=` (the claim that lost), `palette=`; the JSON twin carries the
  probe's own evidence, which the line cannot (it is a space-separated field
  list). `DumpVersion` is **unchanged**: the capability is a property of the RUN,
  not of any cell, so `ScreenDump` needed no change.

## Not Done (honest)

- **`TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow` is red — and was red
  BEFORE this work.** Measured on a pristine `git archive HEAD` tree
  (`/dev/shm/probe-baseline-2067278`): `BeginTurn committed 8 row(s), want 5`.
  Not mine, not touched, not fixed here (it belongs to the turn-header family,
  not to capabilities).
- The other red tests observed in the first `make test-budget` were artifacts of
  the run order (a stale suite binary: `TestEditToolAnnotatesTeachingDocsInAuditedTrees`
  named the binary as older than a source file I had just edited — it passes
  after `make build`), or the coverage ratchet
  (`TestSyncBarePeersTestCoversEveryChangedFile`), which I extended for the two
  cmd/agent files this branch changed.
- The probe does NOT ask when colour is off (`lineedit_wire.go`): four queries
  would put bytes on a screen and answers on an input path for a decision already
  made.
- `gopi ui`'s surface still resolves through `CapabilityForEnv` (declarations
  only): it has no terminal descriptor it owns, and giving it one is a separate
  change.

## Verification

- **Build**: `go build ./...` → clean; `go vet ./internal/lineedit/ ./internal/uiclient/ ./cmd/agent/` → clean.
- **Tests**: `go test ./internal/lineedit/ -run 'TestProbe' -count=1` (pty arms,
  see Evidence); `go test ./internal/uiclient/ -run 'TestCapability' -count=1` →
  ok; `go test ./internal/lineedit/ -run 'TestXTGETTCAP|TestProbeAnswersText|TestProbePartialReplies|TestProbeQueryBytes'`
  → ok.
- **Ran** (Rule 31, the surface itself, against a real pty):
  ```
  $D=/home/user/.gopi/sessions/01a0f720-97e8-75a8-a9d2-1995030571ba/realrun
  python3 $D/terminal-responder.py $PWD/bin/gopi $D/measured.dump $PWD
  python3 $D/terminal-responder.py $PWD/bin/gopi $D/disagree2.dump $PWD --colors16
  ```
  `terminal-responder.py` forks a pty, execs the REAL `bin/gopi chat
  --dump-screen=…` on the slave, reads the probe's queries off the master and
  writes back the answers a real terminal gives.
- **Result**:
  - arm 1 (terminal answers `colors=256`, `RGB=1`, under `COLORTERM=truecolor`):
    record line → `capability=measured depth=truecolor declared_depth=truecolor palette=hermes`.
  - arm 2 (terminal answers `colors=16`, refuses `RGB`, under
    `COLORTERM=truecolor`): record line →
    **`capability=measured depth=basic declared_depth=truecolor palette=basic`**
    — the measurement overruled the declaration and the record says the
    declaration lost.
  - the answering bytes were logged by the responder, e.g.
    `answered b'\x1bP+q636F6C6F7273' with b'\x1bP1+r636F6C6F72733D3136\x1b\\'`.
- **Commit**: see below (`git log -1 --stat`).

## Evidence (self-filed)

- **Two real defects were found by RUNNING, not by reading** (both are now tests):
  1. `\x1bP+q;636F6C6F7273` — the XTGETTCAP request carried a `;` after `q`,
     which no emulator accepts (measured: a real responder answered the DA1/CPR
     queries and stayed silent on both XTGETTCAP ones until the separator was
     removed). The constant was erased and the absence documented in `sgr.go`.
  2. The reply spelling — `applyDCS` insisted on the SPACED `1 + r` that
     ctlseqs(ms) documents, while a real responder (and xterm.js) writes the
     DENSE `1+r`. The live run's record read `capability=declared` while the
     terminal HAD answered `colors=16` — the measurement silently became nothing
     and the declaration won, i.e. the exact defect this probe exists to remove.
     Pinned by `TestXTGETTCAPReplySpellingsBothParse`.
- **The pty arms run against a real pty** (`probe_pty_test.go`): the master side
  plays the terminal and answers the queries; a responder that stays SILENT is
  the arm the fallback rests on (`TestProbeOnASilentTerminalMeasuresNothing`),
  and `TestProbeReplaysAKeystrokeTypedDuringIt` types `hello` DURING the probe
  and asserts the decoder receives exactly `hello` — the epic's keystroke
  question, measured. The whole `TestProbe` set runs in **3.0s** (12/12 PASS).
- **A third defect was found by running the suite, and it was the TESTS'**: the
  responder's teardown joined a goroutine parked in a blocking read on a pty
  master, which does NOT unblock when the master is closed while the slave is
  open — the suite hung until its own 150s timeout. The harness no longer joins
  it (the reason is recorded on `probeResponder.done`), and the same file's
  first version had a worse form of the bug (a `timeout` bounding a single
  `ReadEvent` took the whole test down with it).
- **Baseline proof for the pre-existing red test** (so the claim is checkable):
  ```
  D=/dev/shm/probe-baseline-2067278; cd $D   # git archive HEAD | tar -x
  go test ./internal/lineedit/ -run TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow
  → turn_header_block_test.go:364: BeginTurn committed 8 row(s), want 5
  ```
- **What the probe asks** (the whole question on one line, from the run's own
  frame stream): `\e[6n\e[c\e]11;?\e\\eP+q636F6C6F7273\e\\eP+q524742\e\`.
