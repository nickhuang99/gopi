<!-- doc-class: normative -->
# Task Report: the `voice_input` FORMAT CONTRACT (help == engine, one constant)

Branch `feat/voice-format-contract` (worktree `~/workspace/gopi-voice-format`,
commits `13fadba1`, `7f8124b3`, `8bd520e9` on top of `92d14684`). The worktree
commit IS the deliverable.

## Completed

1. **Measured the `.ogg` question** (deliverable 1) — `.ogg` fails on EVERY
   channel; the help's "ogg" was the lie, the engine's own line
   `supported: wav/mp3/flac` is the truth.
2. **`cmd/agent/cli_voice_format.go` — the ONE source of truth**:
   `voiceAcceptedFormats` (= `voiceAcceptWAV`/`MP3`/`FLAC` joined by
   `voiceFormatSeparator`), the two channel names (`voiceStdioShape` =
   `"--server --" + voiceFlagStdio`, `voiceFileShape`), the container sniff,
   the classifier, the refusal sentence, and `voiceUsageText()` — the single
   renderer that substitutes the placeholders into the usage template.
3. **Help text rewritten for every channel** (deliverable 2): the file channel,
   `--server`, `--server --stdio`, the `--stdio` flag line and the Examples all
   state the engine's real set and what each channel does with container-less
   input; a `NOTE (doubao)` line says the cloud engine's set is the service's,
   not the local set.
4. **Refusal message names the `--stdio` shape** (deliverable 3): a bare `.pcm`
   FILE is refused with a sentence naming `--server --stdio` as the channel
   that accepts the same bytes (WAV-wrapping them); a `.wav`-named file over
   container-less bytes gets its own variant; a missing file gets no hint at
   all (the hint never talks over an unrelated failure).
5. **Contract test + red proof** (deliverable 4):
   `cmd/agent/cli_voice_format_test.go` — 7 tests, including the two-direction
   contract (help list == the constant; the code's accepted suffixes == the
   advertised list), a test-data anchor to the engine's measured set, and an
   exhaustive literal gate over the package's production files.
6. **Channel behaviour UNCHANGED**, verified rather than asserted: bare PCM on
   `--server --stdio` still transcribes, `.wav`/`.mp3`/`.flac` files still
   transcribe, `.ogg` still refused everywhere.
7. Drive-by constant hygiene in `cli_voice_server.go` (`8bd520e9`): the four
   `voiceContainer*` consts were mirror copies of the `voiceMagic*` ones
   (now derived), and the pass-through temp suffix `".audio"` became
   `voicePassthroughSuffix`.

## The measured answer to the format question (deliverable 1)

Engine: `/home/user/work/runtime/llama.cpp/build/bin/llama-funasr-cli` — the
vendored FunASR runtime, decoder = miniaudio with WAV/MP3/FLAC compiled in and
**Vorbis/Ogg NOT compiled in**.

| channel | input | command | observed | exit |
|---|---|---|---|---|
| file (positional) | `t.ogg` | `gopi voice_input t.ogg --chunk 15` | `audio: failed to open/decode t.ogg (supported: wav/mp3/flac) \| failed to read audio` + hint | 1 |
| `--server --stdio` | `t.ogg` on stdin | `gopi voice_input --server --stdio < t.ogg` | same engine line (`…gopi-voice-1895340502.audio…`) — the `OggS` stream is passed through unchanged, the engine still refuses | 1 |
| HTTP `--server` | `t.ogg` upload | `curl -F audio=@t.ogg …/transcribe` | `http=500` + the same engine line | 500 |
| file | `t.pcm` | `gopi voice_input t.pcm --chunk 15` | engine line + the channel sentence naming `--server --stdio` | 1 |
| `--server --stdio` | the SAME `t.pcm` bytes | `gopi voice_input --server --stdio < t.pcm` | `/sil` + `[voice] 128000 bytes in, 7.9s` | 0 |
| file | `t.wav` / `t.mp3` / `t.flac` | `gopi voice_input t.<fmt> --chunk 15` | `[voice] t.<fmt> in ~9s` | 0 |

So: **ogg is refused on every channel; the help advertised it**. The set the
help now prints (`wav/mp3/flac`) is exactly what succeeds.

## File vs pipe equivalence — CONFIRMED, with the honest caveat

The channel difference is **the 44-byte WAV header and nothing else**:

```
f.pcm    (ffmpeg -t 10 … -f f32le)                      640000 bytes
f_wrapped.wav (python: struct-packed canonical 44B header + those bytes)  640044 bytes
  file channel   f_wrapped.wav → 2084年，吴源将延续张艺谋王行时代。…      exit 0
  pipe channel   --server --stdio < f.pcm → 2084无言见人序章裸模型时代…    exit 0
```

Both are accepted; the transcripts differ in **text**, not in accept/refuse
(2.1 s of speech is decoded into different sentences by a greedy decoder, and
the wrapped-file run of the same bytes through the file channel was the slower,
identical-to-pipe path). The claim that matters is the contract one: **the
accept/refuse verdict is a function of the bytes' container, not of the
channel name** — the pipe wraps, the file channel does not. The bare `.pcm` as
a FILE is the only refusal in this family, and its message now says exactly
that.

## The no-microphone test recipe (deliverable 5, reusable)

No hardware, no human, stdlib + ffmpeg only:

```bash
# 1. a spoken fixture, 10 s, 16 kHz mono (any pinned recording works)
ffmpeg -hide_banner -loglevel error -y -t 10 -i /home/user/work/whisper.cpp/2084无颜见人0.mp3 \
       -ar 16000 -ac 1 -c:a pcm_s16le f.wav
# 2. the same samples as bare f32 PCM — what --client --stdio emits
ffmpeg -hide_banner -loglevel error -y -i f.wav -f f32le -acodec pcm_f32le -ar 16000 -ac 1 f.pcm
# 3. the header gopi's stdio channel prepends (44 bytes, python struct — see below)
# 4. the three verdicts
gopi voice_input f.wav                    # exit 0, transcript
gopi voice_input f.pcm                    # exit 1, refusal naming --server --stdio
gopi voice_input --server --stdio < f.pcm # exit 0, transcript
```

**Spell a session directory with its DASHES.** A path built from a session id
written in the *task-id* form (all 32 hex characters, no dashes, e.g.
`/home/user/.gopi/sessions/01a121fceebc73918785db2d149f931e/artifacts/x.wav`) names
a directory that does not exist — the session directory is `01a121fc-eebc-7391-8785-db2d149f931e`.
Root-caused on a real pty 2026-10-10 (task 01a1222d): the ENOENT was correct and
the old message was not, since it quoted the whole path and an error true of every
component at once. The `--audio-source` / `--voice-audio` refusal now names WHICH
component is missing and offers the near-miss sibling that exists
(`— did you mean /home/user/.gopi/sessions/01a121fc-eebc-…?`), so this mistake
answers itself. See `devdoc/chat-voice-audio-file.md` §"The de-dashed session id".

The two extra rows this pass added — they are what separates "the name" from
"the bytes", and both are one ffmpeg/cp away:

```bash
# a REAL Vorbis stream: refused on every channel, and piping does NOT help
ffmpeg -hide_banner -loglevel error -y -i f.wav -c:a libvorbis real.ogg
gopi voice_input real.ogg                    # exit 1, "the bytes carry the ogg container"
gopi voice_input --server --stdio < real.ogg # exit 1  ← the measurement that
                                             #   corrected the first pass's hint
# WAV BYTES under a .ogg NAME: accepted, because the decoder sniffs bytes
cp f.wav wavbytes.ogg
gopi voice_input wavbytes.ogg                # exit 0, transcript
```

The 44-byte header (the "wrap") in full, since reproducing it by hand is the
part that is easy to get wrong:

```python
import struct
raw = open('f.pcm','rb').read(); n = len(raw); rate, ch, bits = 16000, 1, 32
byte_rate, block = rate*ch*bits//8, ch*bits//8
hdr = (b'RIFF' + struct.pack('<I', 36+n) + b'WAVEfmt ' +
       struct.pack('<IHHIIHH', 16, 1, ch, rate, byte_rate, block, bits) +
       b'data' + struct.pack('<I', n))
open('f_wrapped.wav','wb').write(hdr + raw)
```

`2019` note: the small WAVs made by ffmpeg for the `.wav`/`.mp3`/`.flac` matrix
rows are 1-2 s of real speech and decode to `/sil` (silence) — that is the
engine's honest output for a near-silent clip, not a failure; `[voice] … in
8.5s` and exit 0 are the pass signal for those rows.

## Before / after help (the exact text an operator sees)

BEFORE (`gopi-base`, HEAD `92d14684`):

```
  gopi voice_input <audio-file> [--chunk N]
      Transcribe one audio file (wav/mp3/flac/ogg) and print the text.
…
  --stdio          pipe mode (--server: read f32 PCM on stdin, mutually
                   exclusive with --listen; --client: write f32 PCM on
                   stdout — the two ends of one ssh hop)
… (Examples: no clip.ogg / clip.pcm rows)
```

AFTER (`gopi-final`, `7f8124b3`):

```
  gopi voice_input <audio-file> [--chunk N]
      Transcribe one audio file and print the text. The file must be one of
      the containers the engine's decoder has compiled in: wav/mp3/flac. Any
      other container (ogg/vorbis among them) is refused BEFORE/while it is
      decoded — measured, not inferred: the runtime answers "audio: failed
      to open/decode <path> (supported: wav/mp3/flac)". A container-less input
      is refused on THIS channel (the path goes straight to the engine, which
      cannot sniff it) — the bytes still work piped through the --stdio
      shape below, which wraps them in a WAV header first.
      NOTE (doubao): that cloud engine takes whatever the service
      accepts — the local runtime's set above does NOT bound it.

  gopi voice_input --server
      … this channel accepts the SAME set as the file channel (wav/mp3/flac):
      a container-less upload is refused too, with the same engine line.
      (Over ssh without a port, use --server --stdio below — that is the
      shape that takes bare PCM.)

  gopi voice_input --server --stdio
      … CONTAINER-LESS BY DESIGN: this channel wraps a bare PCM stream in a
      WAV header (voiceMaterializeAudio) and then decodes it, so the same
      bytes that a .pcm FILE argument is refused for are ACCEPTED here.
      A stream that already carries a container (wav/mp3/flac) is passed
      through unchanged and decoded the same way — i.e. this channel takes
      both shapes, while the file channel takes containers only.

  --stdio          … The --server end is
                   the channel that takes container-less PCM: it WAV-wraps
                   the stream (see --server --stdio above), so a .pcm file
                   refused as a file argument is accepted here
…
  gopi voice_input clip.ogg                         refused: ogg is not in wav/mp3/flac
  gopi voice_input clip.pcm                         refused; pipe it instead:
  gopi voice_input --server --stdio < clip.pcm      the container-less shape
```

Every one of those `wav/mp3/flac` spellings in the RENDERED help comes from
`voiceAcceptedFormats`; the template carries the `{formats}` placeholder
(5 occurrences) and `{engineDoubao}` (1) — a placeholder leak check is part of
the test, and the shipped help prints none.

## RED PROOF (observed, output pasted)

`A` — the deliverable's required proof: revert ONLY the pre-fix help paragraph,
keep everything else:

```
$ bash redproof2.sh   # RED PROOF A
   reverted help line: 'Transcribe one audio file (wav/mp3/flac/ogg) and print the text.'
--- FAIL: TestVoiceFormatContractRendersTheConstant (0.00s)
    cli_voice_format_test.go:54: the help template carries the "{engineDoubao}" placeholder 0 time(s), want exactly 1 (the cloud-engine note)
--- FAIL: TestVoiceFormatContractAdvertisedListsMatchTheConstant (0.00s)
    cli_voice_format_test.go:74: the help advertises the format list "wav/mp3/flac/ogg", but the code's accepted set is "wav/mp3/flac" — help and behaviour must come from the same constant
    cli_voice_format_test.go:86: the help advertises "wav/mp3/flac/ogg" which contains "ogg" — the engine's decoder does not have it (its own line: "supported: wav/mp3/flac")
--- FAIL: TestVoiceFormatContractLiteralGate (0.11s)
    cli_voice_format_test.go:189: cli_voice.go carries the format list "wav/mp3/flac/ogg" — reference voiceAcceptedFormats (declared in cli_voice_format.go) instead of re-spelling it
    cli_voice_format_test.go:193: cli_voice.go carries the retired literal "wav/mp3/flac/ogg" — the engine refuses ogg (measured); the accepted set comes from cli_voice_format.go
FAIL	github.com/the operator/gopi/cmd/agent	0.735s
```

`B` — the OTHER direction (so the contract cannot be satisfied by drifting the
constant instead of the help): leave the help rendering the constant, and add
`ogg` to `voiceAcceptedFormats`:

```
--- FAIL: TestVoiceFormatContractConstantIsTheEngineMeasuredSet (0.00s)
    cli_voice_format_test.go:115: voiceAcceptedFormats = "wav/mp3/flac/ogg", but the engine's own line says "wav/mp3/flac" — the advertised set must be the decoder's real set
--- FAIL: TestVoiceFormatContractAcceptedSuffixesAreTheAdvertisedList (0.00s)
    cli_voice_format_test.go:130: the code accepts [.wav .mp3 .flac], but the advertised list "wav/mp3/flac/ogg" has 4 entries — the two lists must be the same set
FAIL
```

GREEN after restoring the fixed tree: `ok github.com/the operator/gopi/cmd/agent 0.733s`.

## Not Done (honest)

- **`--engine doubao` was not executed** (no key on this host, and the epic
  scoped it out). Its decode set is the vendor service's, not miniaudio's, and
  `internal/doubaoasr` says so: the uploaded `audio.format`/`codec` default to
  `wav`/`pcm` and a WAV header is sniffed for the geometry, with no local
  accept/refuse list at all (`http.go`). The help therefore carries a one-line
  `NOTE (doubao)` rather than folding the cloud engine into the local set —
  that is a report note, not a measured claim.
- `.hdrless.wav` (container-less bytes named `.wav`) is refused with a
  container-specific message; it is a case the epic did not ask about, kept
  because the classifier already knows it.
- The `.audio` suffix a pass-through `--stdio` upload gets is not a format
  advertisement (it is a temp-file name) — left as is.
- No UI work, no new dependency, no second decoder, no `make fmt` — per the
  epic's constraints.

## Verification

**Second pass (the current tree).** The numbers below are this round's, taken
on the final working tree; the first pass's are kept for the historical record
and are superseded where they differ.

- Build: `CGO_ENABLED=0 go build ./...` → `BUILD_OK` (and `BUILD_ALL_OK`).
- Contract tests: `go test ./cmd/agent/ -run TestVoiceFormatContract -count=1 -v`
  → **8/8 PASS**, `ok github.com/the operator/gopi/cmd/agent 0.831s` (the eighth is
  `…HelpStatesTheBytesNotNameRule`, added this round).
- Branch-scoped voice suite: `go test ./cmd/agent/ -run Voice -count=1` →
  **118/118 PASS**, `ok … 2.604s`; `go test ./internal/doubaoasr/` → `ok 7.585s`.
- **Full `./cmd/agent` package — HONEST NOTE:** it is dominated by pre-existing
  failures that have nothing to do with this diff. Measured (`job-16`,
  `-timeout 25m`, 949s): 4 FAILs, **none in a file this branch touches** —
  `TestTaskListReadPathE2E`, `TestSweepZombiesCLI_DryRunTouchesNoNotifySink`,
  `TestSweepZombiesCLI_ReportNotifiesOperator` all fail only because `bin/gopi`
  was not built (`sweep-zombies E2E needs a gopi binary: set GOPI_BIN`), and
  after building it **all three PASS** (measured). The fourth,
  `TestUsageAdvertisesShot`, fails on `config.FormatUsage()` missing
  `--wait-selector`/`vision_analyze <out.png>` — it lives in
  `internal/config/help.go`, which this branch never modifies, and it also
  failed at 08:10 on `queue-runtime-job-637308` (before this branch existed).
  Verdict: environmental + pre-existing; the branch-scoped voice gate (118/118)
  is the real measurement for this diff.
- Red proof A (revert ONLY the help paragraph) → **3 tests FAIL**
  (`…AdvertisedListsMatchTheConstant`, `…HelpStatesTheBytesNotNameRule`,
  `…LiteralGate`), pasted below; green again on restore.
- Red proof B (drift the CONSTANT by adding `ogg`) → **2 tests FAIL**
  (`…ConstantIsTheEngineMeasuredSet`, `…AcceptedSuffixesAreTheAdvertisedList`);
  green again on restore.
- `go test ./cmd/agent/ -count=1 -timeout 25m` (full package, branch-scoped) →
  result pasted below (job-17).
- `gofmt -l cmd/agent/` → prints only two files this change never touched
  (`serve_lock_replacement_test.go`, `worker_stop_forward_regression_test.go`)
  — pre-existing, left alone (no `make fmt`, per the epic).
- Help render on the final binary: `gopi-v3 voice_input -h` → the only
  slash-joined format list anywhere in the output is `wav/mp3/flac`; the
  placeholder grep (`{formats}|{engineDoubao}`) prints nothing; the retired
  "cannot sniff it" claim occurs 0 times.
- Commit: `13fadba1`, `7f8124b3`, `8bd520e9`, `4944d3c5` (first pass) +
  **`9026bc79`** (this pass — bytes-not-name help, the no-promise hint, one
  container table).
- Worktree clean: `git status --porcelain` → empty.

### Evidence (self-filed) — second pass

- G1 `gopi-v3 voice_input -h` → `help_exit=0`; `grep -o '[a-z0-9]*/…' | sort -u`
  → `wav/mp3/flac` is the only format list; `grep -c "cannot sniff it"` → `0`.
- G2 real Vorbis (`ffmpeg -c:a libvorbis real.ogg`, magic `OggS`, 43924 B) →
  file `exit=1` with "the bytes carry the ogg container … re-encode into one of
  wav/mp3/flac first"; `--server --stdio` → `exit=1` (the pipe does NOT rescue
  it — the measurement that corrected the first pass's hint).
- G3 bare `f.pcm` (640000 B) → file `exit=1` with the sentence naming
  `--server --stdio`; **the same bytes** on `--server --stdio` → `exit=0`.
- G4 `wavbytes.ogg` (a WAV stream bearing the `.ogg` NAME) → `exit=0` — the
  bytes-not-name rule, measured.
- G5 `.wav`/`.mp3`/`.flac` files → `exit=0` each (no regression).
- G6 Red proofs A/B → the FAIL blocks pasted in the Verification section, then
  `ok … 0.917s` on restore.

## Evidence (self-filed) — first pass

- F0 `git status --porcelain` in `/home/user/workspace/gopi-voice-format` →
  prints nothing, at commit `7f8124b3` (log `/home/user/workspace/_tmp/voicefmt/final.log`).
- F1 `CGO_ENABLED=0 go build -o …/gopi-final ./cmd/agent/` → `BUILD_OK`.
- F2 `gopi-final voice_input -h` → the AFTER stanzas above; the placeholder
  grep (`{formats}|{engineDoubao}`) printed nothing.
- F3 `.ogg` → file `exit=1`, `--stdio` `exit=1`, HTTP `http=500`, each with the
  engine's `supported: wav/mp3/flac` line.
- F4 bare `.pcm` file → `exit=1` with "…the SAME bytes work on --server
  --stdio…"; the same bytes on `--server --stdio` → `/sil`, `exit=0`.
- F5 `.wav`/`.mp3`/`.flac` files → `exit=0` each.
- F6 a `.wav`-named container-less file → `exit=1` with the named-inside-the-set
  variant; the WAV-wrapped bytes as a file → transcript, `exit=0`.
- Red proofs: `bash /home/user/workspace/_tmp/voicefmt/redproof2.sh` → the two
  FAIL blocks pasted above, then GREEN on restore.
- Recipe: `bash /home/user/workspace/_tmp/voicefmt/recipe.sh` → f.wav 320078 B,
  f.pcm 640000 B, f_wrapped.wav 640044 B (header 44), file/pipe verdicts as
  tabulated.

## Second pass — the BYTES, not the name (and the promise the hint may not make)

The first pass verified accept/refuse **by extension**. A second round of
measurement on the REAL engine (same runtime) separated the two questions, and
one of the first pass's sentences was wrong about which one it was answering:

| input | bytes | verdict | measured |
|---|---|---|---|
| `t.wav` | RIFF/WAVE | accepted | `exit 0` |
| `wavbytes.ogg` (**WAV bytes, `.ogg` name**) | RIFF/WAVE | **accepted** | `exit 0` |
| `real.ogg` (ffmpeg `libvorbis`) | `OggS` | refused | `exit 1` |
| `real.ogg` piped to `--server --stdio` | `OggS` | **refused** | `exit 1` |
| `f.pcm` piped to `--server --stdio` | container-less | accepted | `exit 0` |

Two consequences, both landed:

1. **The decoder sniffs bytes, not names.** A WAV stream named `.ogg` decodes;
   a real Vorbis stream named `.wav` does not. The help's file-channel stanza
   now says so (it previously read "the path goes straight to the engine, which
   cannot sniff it" — inaccurate: the engine *does* sniff, it is the *raw PCM*
   it cannot read).
2. **The `--stdio` pointer is a promise, and it is only true for
   container-less input.** The first pass's "outside the decode set" branch
   promised the `--stdio` route to a real Vorbis file — measured **false**:
   piping Ogg through `--stdio` passes the `OggS` bytes through unchanged and
   the engine refuses (exit 1). That branch now names **no channel** and says
   the container itself has to go (re-encode). The classifier was restructured
   around a magic→format map (`voiceContainerFormat`,
   `voiceContainerNameOf`) so it can distinguish *carries some container* from
   *carries a container the decoder has* — and the test asserts the invariant
   directly: a hint may contain `voiceStdioShape` **iff** piping the same bytes
   actually rescues them.

Both are pinned by `TestVoiceFormatContractClassifiesInputs` (the
`wantStdioEscape` column: `true` for the container-less rows, `false` for the
Vorbis row — a hint that promised the pipe would fail it).

## Third pass — the refusal hint must never talk over an unrelated failure

`voiceFormatRefusalHint` fires only for an **existing regular file** whose bytes
are either container-less or in a container outside the set. A missing path, a
directory, or a readable container in the set yields `""`, so a genuine
"file not found" keeps the engine's own verdict and gains no format commentary.
The `sniffKnown` flag closes the last hole: if the head read fails after a
successful open, the bytes are **unknown**, not container-less, so no claim is
made.

## Recovery

```bash
git show feat/voice-format-contract:devdoc/voice-input-format-contract.md
git show feat/voice-format-contract:cmd/agent/cli_voice_format.go
```
