<!-- doc-class: normative -->
# Task Report (REWORK): the voice probe measured ONE privilege level and reported it as the node's capability

**Failure being reworked**: `84d05693-0d8a-4d22-85cf-4ced6e632765` (epic ledger
`01a095de-0b80-78dd-8c6d-a86dd00a64e4`, *"gopi new voice input client should be
tested in more nodes"*) — failed by a superficial survey: its capability probe
ran **privilege-free** and concluded peerbox had no capture path.
**Branch**: `fix/voice-mic-probe-privileged` (worktree `~/workspace/gopi-probe-priv`,
from `master` `d45d90e2`).
**Verdict of the rework**: the epic's central hardware premise was **false**.

---

## Completed

1. **Root cause, proven by a controlled experiment** (not inferred):
   `/dev/snd/*` is `crw-rw---- root:audio` with a POSIX ACL granting `rw` to
   `lightdm` only, and **uid 1000 is not in group `audio`** (`id`: adm, cdrom,
   sudo, dip, video, plugdev, lxd, ollama — no `audio`). ALSA cannot resolve the
   card registry for that uid and renders the refusal as
   `arecord: device_list:274: no soundcards found...` — phrasing that reads as
   *missing hardware*. Single-variable proof
   (`devdoc/assets/voice-hw-probe-priv/experiment-audio-group.sh`, same uid, one
   variable changed):

   | run | `arecord -l` | `plughw:1,0` capture |
   |---|---|---|
   | A. uid 1000 as configured | `no soundcards found` | rc 1, **0 B** |
   | B. uid 1000 **+ group `audio`** | card 1 `PCH` / `ALC3228 Analog` | rc 0, **32000 B** |
   | C. root (`sudo -n`) | card 1 `PCH` / `ALC3228 Analog` | rc 0, **32044 B** valid WAV |

2. **The probe now measures the privilege dimension** (`scripts/voice-mic-probe.sh`,
   revision 2):
   - a step-0 privilege probe decides once whether passwordless `sudo -n` works;
   - UNPRIVILEGED **and** PRIVILEGED `arecord -l` / `-L` inventory;
   - every capture cell probed at **both** levels, root capture routed through
     `-t wav -` so the artifact stays **user-owned** (a direct
     `sudo arecord … file` left a `root:root` file in the caller's directory —
     measured, so it is not used);
   - new outcome vocabulary `SUDO_REQUIRED` / `SUDO_UNAVAILABLE` / `TIMEOUT`,
     new `mode` column (`user`/`root`) appended **after** the original ten, so
     existing parsers keep working;
   - a new `CAPTURE_TRUTH` verdict line
     (`CAPTURE_OK` / `CAPTURE_SUDO_REQUIRED` / `UNMEASURED` /
     `NO_CAPTURE_HARDWARE` / `NO_USABLE_DEVICE`) — the elevation-free probe could
     only ever say "UNUSABLE";
   - the misleading ALSA phrasing became a named constant
     (`ARECORD_NO_SOUNDCARDS_MARKER`) instead of an inline grep.

3. **Real peerbox run, verified** (v2, `--trials 2`): `exit 0`;
   `sudo_required_cells=16 ok_cells_root=16`; verdict
   **`CAPTURE_SUDO_REQUIRED`**. Working privileged paths, with byte counts and
   `file(1)`-validated WAVs: `hw:1,0` @48k/2ch 192000 B; `plughw:1,0` 32000 B
   @16k/mono and 192000 B @48k/2ch; `plughw:CARD=PCH,DEV=0`; `sysdefault:CARD=PCH`;
   `front:CARD=PCH,DEV=0`. Genuinely dead at **both** levels (not a privilege
   artifact): `default` (dsnoop slave fails), `sysdefault` (bare), `pulse`
   (no user PulseAudio daemon).

4. **Corrections to the artifacts that carried the false conclusion**:
   `devdoc/voice-hw-matrix.md` (headline table, new §3.1-a refutation, §3.2 "out
   of scope" claim retracted, cross-node table, selection algorithm, outcome
   vocabulary) and `devdoc/voice-node-coverage.md` (node table, "HP is the only
   capture-capable node" retracted, "peerbox permission mode" moved out of the
   untested cells).

5. **Regression guard** (`internal/testutil/voice_probe_privilege_test.go`):
   static half (the privilege vocabulary must be declared and *referenced*; sudo
   only inside the funnel; the misleading phrasing only via its constant) and
   behavioural half (a fake `arecord`/fake `sudo` PATH harness drives both
   privilege branches hermetically — no hardware, no root). **Red-proof on the
   base script**: against `git show HEAD:scripts/voice-mic-probe.sh` the same
   harness yields 0 `SUDO_REQUIRED` rows, no `mode` column and no
   `CAPTURE_TRUTH` line — i.e. the guard fails on the pre-fix artifact.

6. **Memory corrected**: record #2276 updated from *"peerbox cannot capture / the
   refusal is PERMISSION so no device string helps"* to the measured truth plus
   the `use_pty` pitfall; record #2187 voted useless (same false premise).

## Two defects found while verifying (both fixed, both measured)

- **`sudo` outlives `timeout`'s SIGTERM when sudoers carries `use_pty`.**
  A privileged `arecord --dump-hw-params` on the busy PCM survived `timeout 6`
  for **3 m 41 s** and had to be killed by pid. Worse, killing *sudo* leaves the
  **grandchild** alive: two killed probe runs left orphaned privileged `arecord`
  processes holding `pcmC1D0c`, which made every later privileged cell read
  `BUSY`. Fix: `run_sudo` now nests `timeout -k` on **both** sides — outer
  bounds sudo (a prompting sudo fails fast), inner runs the command **as root**,
  which is the only way to bound the actual device opener.
- **The live probe took ~8 minutes** at the original per-command cap, long enough
  to look hung. `CMD_TIMEOUT_SEC` 8→6 and `SUDO_TIMEOUT_SEC` 5 make a full
  64-cell, two-mode run ≈2–3 minutes (still ≥5× the 1 s capture it bounds).

## Not Done

- **No product-code change**: `voice_input --list-devices` still reports
  `no capture device present (no soundcards found)` for a uid that cannot open
  `/dev/snd`, and still does not privilege-escalate. That was a deliberate
  boundary for this rework (the failed task is the *probe*), and the consequence
  is now honest rather than silent: the probe's `CAPTURE_TRUTH` names the
  privilege gate and the fix, so a reader can no longer conclude the node has no
  hardware. **Proposed follow-up (not submitted)**: have the client distinguish
  "no card at all" from "refused by access control" and mention
  `usermod -aG audio <user>` / the privileged path.
- **HP was not re-probed** with revision 2 (the privileged branch is unreachable
  for uid 1000 there and unnecessary, since uid 1000 holds the ACL). HP's numbers
  in the matrix are unchanged from revision 1 and are labelled as such.
- **The operator action itself** (adding uid 1000 to group `audio`) was **not**
  applied — changing the host's group membership is the operator's call, and the
  rework's job was to measure and report, not to reconfigure the machine.
- `scripts/voice-capture-failmatrix.sh` was left untouched (it probes the client,
  not node capability; out of this rework's scope).

## Verification

- `bash -n scripts/voice-mic-probe.sh` and `bash -n
  devdoc/assets/voice-hw-probe-priv/experiment-audio-group.sh`: **OK**
- `go test -run 'TestVoiceProbe|TestScriptsAreBoundedAndOffTmp' ./internal/testutil/`:
  **ok** (includes the repo's /tmp-ban + `bash -n` gate over every script,
  `devdoc/` included)
- Probe, real peerbox run **twice**: `EXIT=0` both times; verdict tuples
  (`device, rate, channels, outcome, mode`) **identical across the two runs**
  (`diff` empty) — determinism preserved
- Red-proof: the guard + fake harness against the **base** script → no
  `SUDO_REQUIRED`, no `mode` column, no `CAPTURE_TRUTH` (fails as intended)
- Cross-check against an independent measurement taken before the probe existed:
  privileged `plughw:1,0` → 32044 B, `hw:1,0` @48k/2ch → 192044 B; the probe
  reports 32000 B / 192000 B PCM (**same** payload after the 44-byte RIFF header)
- Artifacts staged from the verified runs: 110 files (`table.tsv`, per-cell
  `.err`, `dev-node-access.tsv`, both listings unprivileged+privileged,
  16 non-empty capture WAVs) under `devdoc/assets/voice-hw-probe-priv/peerbox/`

## Rule 22 / scratch note

All scratch lived in `/dev/shm/gopi-probe-priv/` (device 28 ≠ `/` device 2051 —
`gopi_assert_off_root_fs` semantics); the new `devdoc/` experiment script takes
its workspace from `scripts/lib/tmp-ban.sh` (`gopi_scratch_mkdir`), so the
`devdoc/` scan in `TestScriptsAreBoundedAndOffTmp` stays green.

## Deliverable declaration

- **Branch**: `fix/voice-mic-probe-privileged` (in the shared repo; worktree
  `~/workspace/gopi-probe-priv`, so the commits are already visible to the main
  repo — no push needed).
- **Recovery**: `git show fix/voice-mic-probe-privileged:devdoc/voice-mic-probe-privileged.md`
- **Commits**: see `git log --oneline master..fix/voice-mic-probe-privileged` in the worktree.
