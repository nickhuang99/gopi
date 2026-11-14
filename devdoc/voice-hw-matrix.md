<!-- doc-class: normative -->
# Voice Input Hardware Matrix — Measured Node Capabilities (epic 1/6)

**Status:** measured, not inferred. Every value below comes from
`scripts/voice-mic-probe.sh` run on the node in question; the raw artifacts are
committed next to this file and each claim cites its artifact.

| | |
|---|---|
| Probe | `scripts/voice-mic-probe.sh` (deterministic, non-interactive, exit 0) |
| Probe revision | the commit that added `scripts/voice-mic-probe.sh` (see `git log --oneline -- scripts/voice-mic-probe.sh`) |
| peer1 run | 2026-09-13T05:21:06+08:00, kernel `Linux 5.15.0-163-generic x86_64` |
| HP run | 2026-09-13T05:20:52+08:00, kernel `Linux 5.15.0-185-generic x86_64` |
| peer1 artifacts | `devdoc/assets/voice-hw-probe/peer1/` (+ full stdout `peer1-stdout.txt`) |
| HP artifacts | `devdoc/assets/voice-hw-probe/hp/` (+ full stdout `hp-stdout.txt`) |
| Contract probed | `S16_LE`, `16000 Hz`, `1` channel (voice-client contract) **and** the card's native `48000 Hz / 2ch` |

Host identity, verbatim from `identity.txt` on each node:

```
peer1: hostname=peer1              uname=Linux 5.15.0-163-generic x86_64
hp:    hostname=hostb-Laptop     uname=Linux 5.15.0-185-generic x86_64
```

`hostb-Laptop` is the node the epic calls **HP** (`ssh hp` → 192.0.2.5); the
probe's `node` column carries the real hostname, so the table is self-describing.

---

## 1. The headline result

**CORRECTED 2026-09-13 (probe revision 2, branch `fix/voice-mic-probe-privileged`).**
Revision 1 measured ONE privilege level (the calling uid) and read its refusals as
hardware facts. On peer1 that produced a false conclusion: uid 1000 is outside
group `audio`, ALSA renders that refusal as `no soundcards found`, and this
document — plus the epic built on it — concluded "peer1 has no usable capture
path". It has one; it is privilege-gated. §3.1-a is the refutation;
`devdoc/voice-mic-probe-privileged.md` is the rework report.

| Node | Capture as the calling user (uid 1000) | Capture as root (`sudo -n`) | Evidence |
|---|---|---|---|
| **HP** (`hostb-Laptop`) | **YES** — logical `default` and `pulse`, backed by card1 / ALC236 Analog / device0 | not needed (uid 1000 holds the `/dev/snd` ACL on HP) | `hp/table.tsv`, 4/4 `OK` cells each |
| **peer1** | **NO** for uid 1000 *as configured* — not in group `audio`, no ACL entry | **YES** — `hw:1,0` @48k/2ch, `plughw:1,0`, `plughw:CARD=PCH,DEV=0`, `sysdefault:CARD=PCH`, `front:CARD=PCH,DEV=0` all captured (32000 B / 192000 B) | `peer1/table.tsv` revision 2 (`mode` column) + `devdoc/assets/voice-hw-probe-priv/peer1/` |

Both conclusions are exit-0 results of the same script — a refused or
device-less node is a recorded fact (`outcome=NO_DEVICE` / `PULSE_REFUSED` /
`SUDO_REQUIRED`) and never a probe error. Revision 2 measures every cell twice
(user, then root), so "this user cannot open it" can no longer be reported as
"this node cannot capture".

---

## 2. HP — the capture-capable node

`hp/proc-asound-cards.txt`:

```
 0 [Generic        ]: HDA-Intel - HD-Audio Generic
 1 [Generic_1      ]: HDA-Intel - HD-Audio Generic
```

`hp/proc-asound-pcm.txt` (card `01`, device `00`, **capture 1**):

```
00-03: HDMI 0 : HDMI 0 : playback 1
01-00: ALC236 Analog : ALC236 Analog : playback 1 : capture 1
```

`hp/arecord-l.txt`:

```
**** List of CAPTURE Hardware Devices ****
card 1: Generic_1 [HD-Audio Generic], device 0: ALC236 Analog [ALC236 Analog]
  Subdevices: 1/1
  Subdevice #0: subdevice #0
```

### 2.1 What worked, and the exact strings

Both successful strings are **logical ALSA/Pulse names that resolve to card 1
(ALC236 Analog), device 0**. Each measured `2/2` trials `OK` at **both** probed
parameter sets, with byte-exact expected sizes:

| Device string | 16000 Hz mono S16_LE | 48000 Hz 2ch S16_LE | Trials |
|---|---|---|---|
| `default` | `rc=0`, **32000 B** PCM | `rc=0`, **192000 B** PCM | 2/2 and 2/2 OK |
| `pulse`   | `rc=0`, **32000 B** PCM | `rc=0`, **192000 B** PCM | 2/2 and 2/2 OK |

`32000 B = 16000 Hz × 1 ch × 2 B × 1 s` and `192000 B = 48000 × 2 × 2 × 1 s` — the
captures are exactly the contract length, so these are real 1-second recordings,
not a spun-up device that returned immediately. The contract-format WAV headers
were confirmed independently:

```
$ file hp/capture-default_r16000_c1_t1.wav
RIFF (little-endian) data, WAVE audio, Microsoft PCM, 16 bit, mono 16000 Hz
```

### 2.2 The raw `hw:`/`plughw:` strings are **BUSY** on HP, and that is the norm

Every raw-hardware spelling of the same card failed with
`Device or resource busy`, 8/8 trials:

| Device string | Outcome | Trials |
|---|---|---|
| `hw:1,0` | `BUSY` | 4/4 |
| `plughw:1,0` | `BUSY` | 4/4 |
| `plughw:CARD=Generic_1,DEV=0` | `BUSY` | 4/4 |
| `front:CARD=Generic_1,DEV=0` | `BUSY` | 4/4 |
| `sysdefault:CARD=Generic_1` | `BUSY` | 4/4 |
| `sysdefault` | `NO_DEVICE` | 4/4 |

Verbatim stderr (`hp/capture-hw_1_0_r16000_c1_t1.err`):

```
arecord: main:831: audio open error: Device or resource busy
```

**Root cause (measured).** HP's PulseAudio holds the capture PCM:

```
$ pactl list short sources
1  alsa_output.pci-0000_05_00.6.analog-stereo.monitor   ...  IDLE
2  alsa_input.pci-0000_05_00.6.analog-stereo            ...  SUSPENDED
$ pactl list short sinks
1  alsa_output.pci-0000_05_00.6.analog-stereo           ...  RUNNING
```

The server owns the card as `alsa_input.pci-0000_05_00.6.analog-stereo`, so a
second opener of the raw hardware is refused. This is why `default` (which
negotiates through the ALSA configuration and ends at the Pulse/dmix path) and
`pulse` (which talks to the server directly) succeed while `hw:1,0` does not.
`capture_nodes=1 denied_nodes=0` (`hp-stdout.txt`) — **permissions on HP are
fine**; the only obstacle is the server holding the raw PCM.

### 2.3 HP hw-params (why `hw:` is the wrong default for the client)

`hp/hwparams-hw_1_0.txt` — the raw hardware demands far more than the voice
contract:

```
HW Params of device "hw:1,0":
ACCESS:  MMAP_INTERLEAVED RW_INTERLEAVED
FORMAT:  S16_LE S32_LE
SAMPLE_BITS: [16 32]
CHANNELS: 2
RATE: [44100 192000]
```

`CHANNELS: 2` and `RATE: [44100 192000]` — at `16000 Hz / mono` the raw device
would have to *convert*, which only `plughw:`/`default` (the `plug` layer) can
do. A client that opens `hw:` with the voice contract can never work on this
hardware; the plug layer is mandatory.

---

## 3. peer1 — the capture-less node (reported honestly, verbatim)

The epic's expected reporting is reproduced **exactly** by the probe:

`peer1/arecord-l.txt` — verbatim, entire file:

```
arecord: device_list:274: no soundcards found...
```

`peer1/pactl-info.txt` — verbatim, entire file:

```
Connection failure: Connection refused
pa_context_connect() failed: Connection refused
```

Neither is an error path in the probe: both are parsed into the machine-readable
table as `NO_SOUNDCARDS` / `PULSE_REFUSED`, and the script still exits 0.

### 3.1 The subtle part: peer1 *advertises* a capture device it cannot open

`peer1/proc-asound-pcm.txt` ends with the same `capture 1` marker HP has:

```
01-00: ALC3228 Analog : ALC3228 Analog : playback 1 : capture 1
```

and `/dev/snd/pcmC1D0c` exists. Yet **every** device string failed (`peer1/table.tsv`,
32/32 non-OK rows):

| Device string | Outcome | Trials |
|---|---|---|
| `hw:1,0`, `plughw:1,0` | `NO_DEVICE` | 4/4 each |
| `plughw:CARD=PCH,DEV=0` | `NO_DEVICE` | 4/4 |
| `default`, `sysdefault`, `sysdefault:CARD=PCH` | `NO_DEVICE` | 4/4 each |
| `front:CARD=PCH,DEV=0` | `HW_PARAM_UNSUPPORTED` | 4/4 |
| `pulse` | `PULSE_REFUSED` | 4/4 |

Verbatim (`peer1/capture-hw_1_0_r16000_c1_t1.err`):

```
ALSA lib confmisc.c:165:(snd_config_get_card) Cannot get card index for 1
arecord: main:831: audio open error: No such file or directory
```

Verbatim (`peer1/capture-pulse_r16000_c1_t1.err`):

```
ALSA lib pulse.c:242:(pulse_connect) PulseAudio: Unable to connect: Connection refused
arecord: main:831: audio open error: Connection refused
```

A reader may wrongly conclude "the card is missing". It is not — the **card
registry is empty for this user**. Measured causes:

1. **The client uid may not open any `/dev/snd` node.** `peer1-stdout.txt`
   `DEVICE_NODE_ACCESS` → `capture_nodes=1 denied_nodes=13`: all 13 nodes,
   including `pcmC1D0c`, report `DENIED` to uid 1000. The nodes are mode `660
   root:audio` and `id the operator` shows uid 1000 is **not** in group `audio`
   (`groups=...44(video),46(plugdev),110(lxd),998(ollama)` — no `audio`), while
   the POSIX ACL grants `rw` to `lightdm` only. Direct syscall confirmation:

   ```
   /dev/snd/controlC1 -> errno 13 Permission denied
   /dev/snd/pcmC1D0c  -> errno 13 Permission denied
   ```

   This is the exact "permission" outcome class the probe is required to report.

2. **ALSA's card registry does not offer card 1 to this user** — hence
   `arecord -l` → `no soundcards found` even though `/proc/asound/cards` lists
   two cards. The probe records both faces of the truth side by side
   (`PROC_ASOUND_CARDS` lists them; `ARECORD_L` reports none).

3. **PulseAudio is running but its client socket is gone.**
   `ss -xl` shows a listener at `/run/user/1000/pulse/native`, yet the directory
   is **empty** (`ls -la /run/user/1000/pulse/` → only `.` and `..`, inode
   `105146`, mtime 2026-08-27) and every client path is unlinked
   (`/tmp/pulse-*/native`, `/var/run/pulse/native` → absent). A dangling listener
   with no reachable node produces exactly the observed
   `pa_context_connect() failed: Connection refused`.

Root-cause summary: **on peer1 the capture path is closed by client-side access
control (uid 1000 ∉ `audio`, node mode 660) plus a dead Pulse socket — not by
missing hardware.** The hardware (`ALC3228`, card1/device0) is present. Note the
peer1 codec is `ALC3228`, HP's is `ALC236` — different codecs, same ALSA shape.

### 3.1-a CORRECTION (2026-09-13): peer1 CAN capture — the refusal is access control

The measurement above is real, and the reasoning drawn from it was wrong in one
step: the unprivileged outcome was treated as the node's capability. It is the
*calling user's* capability. Measured at BOTH privilege levels on peer1:

| Probe | unprivileged (uid 1000) | privileged (`sudo -n`) | uid 1000 **+ group `audio`** (via `setpriv`) |
|---|---|---|---|
| `arecord -l` | `no soundcards found` | card 1 `PCH` / `ALC3228 Analog` | card 1 `PCH` / `ALC3228 Analog` |
| `hw:1,0` @48k/2ch | `NO_DEVICE` | **OK — 192000 B** | — |
| `plughw:1,0` @16k/mono | `NO_DEVICE` | **OK — 32000 B** | **OK — 32000 B** |
| `plughw:CARD=PCH,DEV=0`, `sysdefault:CARD=PCH` | `NO_DEVICE` | **OK** | — |
| `front:CARD=PCH,DEV=0` @48k/2ch | `HW_PARAM_UNSUPPORTED` | **OK** | — |
| `default`, `sysdefault` (bare) | `NO_DEVICE` | **NO_DEVICE** — dsnoop slave open fails (a genuine defect, not access) | — |
| `pulse` | `PULSE_REFUSED` | `PULSE_REFUSED` — no user PulseAudio daemon (genuine) | — |

The controlled single-variable experiment is decisive: **uid 1000 + supplementary
group `audio` (29) + no other change turns `no soundcards found` into a listing
and `plughw:1,0` into a 32000-byte capture.** So the cause is precisely the one
§3.1 point 1 names (`/dev/snd` is `660 root:audio`, `id the operator` lacks `audio`, the
POSIX ACL grants `rw` to `lightdm` only) — and the correct conclusion is *"the
fix is one operator action (add uid 1000 to group `audio`, or run the probe with
`sudo -n`)"*, NOT *"peer1 is not a capture node"*. The epic's node plan (peer1 =
engine only, HP = the only client) rested on the wrong reading.

What is still true and still a real limit: the uid-1000 path is refused on peer1
**as configured**, and the bare `default`/`sysdefault` aliases fail at both
privilege levels. The privilege dimension was simply never measured — which is
what revision 2 of the probe fixes: every cell now carries a `mode` column
(`user`/`root`) and a device that only records privileged is reported
`SUDO_REQUIRED`, never `NO_DEVICE`/`NO_SOUNDCARDS`.

Artifacts: `devdoc/assets/voice-hw-probe-priv/peer1/` (privileged run, full raw
dumps + per-cell WAVs) and `devdoc/assets/voice-hw-probe/peer1/` (revision-2 run
with both modes in one table).

### 3.2 What would (and would not) change the verdict

Not tested in revision 1 because it needs root — and revision 2 tested it: a
privileged open of `pcmC1D0c` **does** succeed (§3.1-a). Root was reachable all
along through passwordless `sudo -n` (`NOPASSWD: ALL` on peer1), so "out of scope
for a non-interactive probe" was a wrong premise: `sudo -n` is exactly as
non-interactive as the rest of the harness. Revision 2 records the unprivileged
truth AND the privileged one side by side, and names the difference as
`SUDO_REQUIRED` instead of letting it read as absent hardware.

---

## 4. Cross-node comparison

| Dimension | HP (`hostb-Laptop`) | peer1 |
|---|---|---|
| `/proc/asound/cards` | 2 (Generic, Generic_1) | 2 (HDMI, PCH) |
| Capture stream in `/proc/asound/pcm` | `01-00 ... capture 1` | `01-00 ... capture 1` |
| `arecord -l` (user) | lists card1 ALC236 Analog | **`no soundcards found...`** |
| `arecord -l` (root, `sudo -n`) | — | **lists card1 PCH / ALC3228 Analog** (see §3.1-a) |
| `/dev/snd` nodes openable by the probe uid | **all** (`denied_nodes=0`) | **none** (`denied_nodes=13`, errno 13; ACL grants `lightdm` only) |
| PulseAudio | up, socket `/run/user/1000/pulse/native` | socket unlinked; `Connection refused` |
| `default` @16k/mono S16_LE | **OK, 32000 B** | `NO_DEVICE` (both modes — dsnoop slave fails) |
| `pulse` @16k/mono S16_LE | **OK, 32000 B** | `PULSE_REFUSED` (both modes — no user daemon) |
| `hw:`/`plughw:` raw card | `BUSY` (server holds PCM) | `NO_DEVICE` as user; **`OK` as root** (32000 B / 192000 B) |
| Verdict | **capture-capable client node** | **capture node, privilege-gated**: records as root, refused for uid 1000 as configured |

---

## 5. Per-device identifiers the voice client MUST accept

Derived strictly from the table above. The client must accept these strings, and
must treat the listed outcome as a *normal* result (surface it), never as an
unhandled crash.

**Must accept and prefer (measured working):**

| Identifier | Meaning | Measured |
|---|---|---|
| `default` | ALSA default; resolves through the plug layer — the only form that satisfies `16k/mono/S16_LE` on HP's raw `CHANNELS: 2` / `RATE: [44100 192000]` hardware | HP `OK` 4/4 |
| `pulse` | PulseAudio ALSA plugin; multiplexes with the running desktop audio server | HP `OK` 4/4 |

**Must accept and handle gracefully (measured failing — a clear error, not a hang):**

| Identifier | Meaning | Measured outcome |
|---|---|---|
| `hw:<card>,0` | raw ALSA, **no conversion** — cannot express 16k/mono on this codec | HP `BUSY`; peer1 `NO_DEVICE` |
| `plughw:<card>,0` | raw ALSA + plug conversion | HP `BUSY`; peer1 `NO_DEVICE` |
| `plughw:CARD=<id>,DEV=0` | index-independent spelling of the above (**prefer this over `plughw:1,0`** when a raw device is wanted) | HP `BUSY`; peer1 `NO_DEVICE` |
| `sysdefault` / `sysdefault:CARD=<id>` | ALSA "sane first device" | HP `NO_DEVICE`/`BUSY`; peer1 `NO_DEVICE` |

**Must NOT be generated (measured invalid):**

| Identifier | Why |
|---|---|
| `front:<card>` / `front:CARD=…,DEV=…` | `front` accepts no `DEV=` parameter (`Unknown parameters CARD=PCH,DEV=0`, then `Unknown PCM front:CARD=PCH,DEV=0`) |

**Pulse-side identifiers present on HP** (for a `-d <source>` style selector):
`alsa_input.pci-0000_05_00.6.analog-stereo` (the microphone) and
`alsa_output.pci-0000_05_00.6.analog-stereo.monitor` (the loopback — the
human-free test rig from the epic's earlier measurement).

### 5.1 Selection algorithm the measurements justify

1. Open `/dev/snd/pcmC*c` for the client uid (`access(2)`). **Denied** → the
   client cannot record as this uid, and no ALSA device string helps *at this
   privilege level* (peer1's case, §3.1-a). But do **not** conclude the node has
   no capture path: on peer1 the same hardware records with `sudo -n` and as
   uid 1000 + group `audio`. Report `PERMISSION` for the uid and name the fix
   (add the uid to group `audio`, or run privileged) — a client that reports
   only "no device" here strands a node that can record.
2. A user audio server is reachable → **prefer `pulse`** (no collision with the
   server; measured OK on HP).
3. No server → **use `default`**.
4. Use `plughw:CARD=<id>,DEV=0` **only** when no audio server is running;
   otherwise expect `BUSY`.
5. Never default to `sysdefault` or `front` (measured unusable/invalid), and
   never retry a `BUSY` device in a tight loop — it means another owner holds
   the PCM.

### 5.2 Outcome vocabulary (stable, machine-readable)

The table's `outcome` column is the client's contract. `OK` = data captured;
`EMPTY` = opened but 0 bytes; `BUSY` = another owner holds the PCM;
`PERMISSION` = node unopenable by this uid; `NO_DEVICE` = card/device not in this
user's registry; `NO_SOUNDCARDS` = ALSA advertises no card at all;
`HW_PARAM_UNSUPPORTED` = the device cannot express the requested parameters;
`PULSE_REFUSED` = Pulse socket unreachable; `ERROR` = none of the above.

Revision 2 adds two values and one column:

- `SUDO_REQUIRED` = the cell captured **privileged** while the unprivileged
  attempt at the same device/rate/channels failed — an access-control fact, and
  the value that must never be collapsed into `NO_DEVICE`/`NO_SOUNDCARDS`.
- `SUDO_UNAVAILABLE` = no passwordless `sudo -n` on the node, so the privileged
  level was NOT measured (recorded, never silently absent).
- `mode` = the eleventh column (`user` / `root`), appended after
  `stderr_first_line` so a parser of the original ten columns keeps working.

---

## 6. Reproducing this matrix

```bash
# capture-less / local node — always exit 0
bash scripts/voice-mic-probe.sh --out devdoc/assets/voice-hw-probe/peer1

# capture node, over ssh (no interactive input, no root)
scp scripts/voice-mic-probe.sh hp:/tmp/ && ssh hp 'bash /tmp/voice-mic-probe.sh --out /tmp/probe-hp'
```

Verified properties (see `devdoc/voice-mic-probe-report.md` for the raw output):
`exit 0` on **both** nodes; `bash -n` clean; `shellcheck -S warning` clean;
**two consecutive runs on an unchanged node produce a byte-identical
`table.tsv`** (determinism), which is what makes the table diffable in CI.

The machine-readable table is emitted between the
`VOICE_MIC_TABLE_BEGIN`/`VOICE_MIC_TABLE_END` sentinels with the fixed columns
`node, device, rate, channels, format, trial, rc, data_bytes, outcome,
stderr_first_line`; `table.tsv` in each artifact directory is the same data as a
file, next to one `.err` per probed cell.

---

## 7. Caveats — what this matrix does NOT claim

- **One HP sample, one instant.** `plughw:1,0` on HP returned data in an *earlier
  manual* attempt (32044 B) and `Channels count non available` in another, then
  `BUSY` in the final probe run — because a desktop audio server's hold on the
  PCM is inherently time-varying. The committed table is a single deterministic
  run; that variability is the reason the client must treat `BUSY` as a
  first-class outcome rather than a fatal error.
- **No measurement of audio *content*** beyond byte counts and WAV headers; the
  probe proves a path records the contract number of bytes, not that a human is
  audible.
- **peer1's privileged path is untested** (§3.2) — the probe never escalates.
- **No level/gain measurement** — out of scope for this step.
