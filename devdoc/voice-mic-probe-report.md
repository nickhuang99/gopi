<!-- doc-class: historical -->
# Task Report: Voice input device inventory + capability probe (epic 1/6)

## Completed
- `scripts/voice-mic-probe.sh` — deterministic, **non-interactive**, exit-0 probe.
  Sections: IDENTITY, PROC_ASOUND_CARDS, PROC_ASOUND_PCM, ARECORD_L,
  ARECORD_L_PCMS (`arecord -L`), LSUSB, PACTL_INFO/SOURCES/SINKS, PERMISSIONS,
  DEVICE_NODE_ACCESS, CAPTURE_CAPABLE_CARDS, CANDIDATE_DEVICE_STRINGS,
  HW_PARAMS, CAPTURE_PROBES, `VOICE_MIC_TABLE_BEGIN/END` (machine-readable TSV),
  DEVICE_VERDICT.
- Per-device 1s capture matrix over `hw:<n>,0`, `plughw:<n>,0`,
  `plughw:CARD=<id>,DEV=0`, `front:CARD=<id>,DEV=0`, `default`, `pulse`,
  `sysdefault`, `sysdefault:CARD=<id>` at **16000 Hz/mono/S16_LE** and
  **48000 Hz/2ch/S16_LE**, with an explicit outcome vocabulary
  (`OK|EMPTY|BUSY|PERMISSION|NO_DEVICE|NO_SOUNDCARDS|HW_PARAM_UNSUPPORTED|PULSE_REFUSED|ERROR`).
- Ran on **peerbox** (capture-less) and **HP** (capture hardware), artefacts committed.
- `devdoc/voice-hw-matrix.md` — the measured matrix, the exact working device
  strings, the identifiers the client must accept/reject, and the selection algorithm.
- Two `outcome`-classification defects found by reviewing against raw stderr and
  fixed (see "Not Done"/findings below).

### Findings worth carrying into the next epic step
- **HP succeeds only through logical devices**: `default` and `pulse`, both
  `32000 B` @16k/mono (4/4 trials each). Every raw `hw:`/`plughw:` spelling of the
  same card is `BUSY` because PulseAudio owns the PCM — raw-hardware defaults are
  wrong for the client.
- **HP `hw:1,0` cannot express the voice contract at all**: hw-params are
  `CHANNELS: 2`, `RATE: [44100 192000]` — the plug layer is mandatory.
- **peerbox's failure is client-side access control, not missing hardware**: the
  ALC3228 capture device exists (`/proc/asound/pcm` `01-00 ... capture 1`,
  `/dev/snd/pcmC1D0c` present) but all 13 `/dev/snd` nodes are `DENIED` to uid
  1000 (errno 13; mode `660 root:audio`, uid 1000 not in `audio`), and the Pulse
  socket is unlinked though a listener remains (`Connection refused`).

## Files
| File | Purpose |
|---|---|
| `scripts/voice-mic-probe.sh` | the probe (chmod +x, no external deps beyond ALSA/Pulse userland) |
| `devdoc/voice-hw-matrix.md` | measured matrix + client contract |
| `devdoc/assets/voice-hw-probe/{peerbox,hp}/` | per-node raw dumps, `table.tsv`, one `.err` per probed cell, contract-format `.wav` proof files |
| `devdoc/assets/voice-hw-probe/{peerbox,hp}-stdout.txt` | full labelled probe stdout per node |
| `devdoc/voice-mic-probe-report.md` | this report |

## Verification
- `bash -n scripts/voice-mic-probe.sh` → clean; `shellcheck -S warning` → **clean**.
- **exit 0 on peerbox** (capture-less) — `RUN1_OK`, `RUN2_OK`; the node's
  `no soundcards found` / `Connection refused` are table outcomes, not errors.
- **exit 0 on HP** over ssh, no interactive input, no root — `HP_EXIT=0`,
  32/32 table rows emitted.
- **Determinism**: two consecutive peerbox runs → `diff` of `table.tsv` is empty
  ("byte-identical"), which is what makes the table reviewable in CI.
- Contract-format WAV proof: `file hp/capture-default_r16000_c1_t1.wav` →
  `WAVE audio, Microsoft PCM, 16 bit, mono 16000 Hz`; sizes exactly
  32000 B (16k/mono) and 192000 B (48k/2ch) per 1 s.
- Every "verbatim" quote in the doc was re-checked against its artifact file by
  exact-string match (10/10 checks matched; one citation was corrected because
  its artifact is not in the final candidate set).
- Build/test gate: `CGO_ENABLED=0 go build ./...` OK;
  `go test ./internal/agent/ -run TestGOPIRule3_MatchesClaudeToolDefs` → ok
  (this change adds no Go code and no tool surface).
- Commit: `89d0e5fd feat(voice): deterministic cross-node mic capability probe + measured hw matrix (epic 1/6)`

## Not Done
- **No root escalation tested.** Whether a privileged open of `pcmC1D0c` succeeds
  on peerbox is *unmeasured* — deliberately: the probe must be non-interactive and
  privilege-free. peerbox's verdict is therefore "no capture path for uid 1000",
  not "the hardware is broken".
- **No audio-content/level verification** — byte counts and WAV headers only;
  the probe does not prove a human is audible, nor measure gain.
- **No Pulse null-sink rig** — the human-free deterministic *audio* rig from the
  epic's earlier measurement (null-sink + `parecord -d <sink>.monitor`) is not
  automated here; this step is the device-capability layer only.
- **No peerbox remediation** (e.g. adding uid 1000 to group `audio`) — that is an
  operator/ops change, out of scope, and deliberately not "fixed" to make a table
  look green.
- **`plughw:1,0` on HP is time-varying**: it returned 32044 B in a manual attempt
  early in the session and `Channels count non available`, then `BUSY`, in later
  runs. The committed table is one deterministic run; the variability is recorded
  in the doc's caveats rather than hidden.
- One classification nuance retained as-is: `plughw:CARD=<id>,DEV=0` on peerbox
  reports `NO_DEVICE` while `arecord -l` says `no soundcards found`, because the
  script exits early on `NO_SOUNDCARDS` only when arecord itself emits that text
  (an honest per-attempt report beats a blanket override).
