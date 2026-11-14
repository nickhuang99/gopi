<!-- doc-class: historical -->
# Voice input — node × device coverage matrix (epic `01a095de` step 6/6)

**Date**: 2026-09-13 · **Host of record**: peerbox (`nick`) · **Harness**:
`scripts/voice-mic-probe.sh` (probe) and `bin/gopi voice_input --client`
(live client), both run keyless over ssh on every node below.
**Branches**: probe/doc on `feat/voice-node-coverage`; the client under test is
`05ab31c0` (tracked tip, an ancestor of master `ce7e127a`).

---

## 1. Which nodes exist, and which are usable at all

A full `/24` sweep (ICMP) found **12 hosts up**; each was then tested for
**keyless** ssh (`ssh -o BatchMode=yes nick@<ip> hostname`), which is the only
entry the harness has.

| Host | ssh keyless | Identity | ALSA capture hardware | Verdict |
|---|---|---|---|---|
| 10.0.0.5 | ✅ `hostb-Laptop` | x86_64 | card 1 `Generic_1` ALC236, `arecord -l` non-empty | **capture-capable** |
| 10.0.0.6 | ✅ (this host) `peerbox` | x86_64 | ALC3228: refused to uid 1000 (13/13 `/dev/snd` DENIED, ∉ group `audio`), **but captures as root (`sudo -n`) and as uid 1000 + group `audio`** — see §2 | client-side **denied** for uid 1000; node **privilege-gated capable** |
| 10.0.0.9 | ✅ `DS918Plus` (NAS) | x86_64 | none — `arecord`/`parecord`/`pactl` **ABSENT** | no ALSA at all |
| 10.0.0.15 | ✅ `rpi5` | aarch64 | only `vc4-hdmi-0/1` **playback** cards; `arecord -l` empty | no capture device |
| 10.0.0.11 | ❌ key refused (peerbox's own wlo1) | — | — | out of scope |
| 10.0.0.13 | ❌ key refused (**hosta**) | — | — | **blocked** (§3) |
| 1, 3, 7, 8, 10, 12 | ❌ port 22 refused/no sshd | — | — | no ssh path |

`~/.ssh/config` carries a **stale** `Host rpi5 → 10.0.0.14` (`.14` is DOWN,
no route); the live rpi5 answers at **10.0.0.15**.

**Node-coverage conclusion**: the keyless node universe is exactly three hosts
(HP, peerbox, NAS) plus rpi5 — and **HP is the only capture-capable one**. No
further node can be added without an operator action (§3).

**CORRECTION (2026-09-13)**: that sentence is wrong. It generalizes uid 1000's
capability into every node's capability. peerbox's ALC3228 capture works
privileged (§2), so **two** nodes are capture-capable: HP for uid 1000, peerbox
for root / group `audio`. The reason no further node can be added is still an
operator action (§3) — but on peerbox the fix is a one-liner the operator can make
now (`usermod -aG audio nick`), not an unreachable login.

---

## 2. The tested matrix — node × device × outcome

Outcomes are the probe vocabulary (`scripts/voice-mic-probe.sh`) with the
client's mode in parentheses where the live client was run.
`OK` = data captured; everything else is a named refusal.

### peerbox (`10.0.0.6`, x86_64) — the privilege-gated node (REWORKED 2026-09-13)

**This section's original conclusion was wrong and is corrected here.** The
32/32 non-`OK` cells below are real *for uid 1000*, and they were reported as the
node's capability — *"Not one cell is OK — honest, and the reason is access
control"* — which is true of the uid and false of the node. The probe never
measured the second privilege level, while passwordless `sudo -n` is available
(`NOPASSWD: ALL`). Re-measured on the SAME node, same hardware:

| device | 16k mono (user) | 16k mono (`sudo -n`) | 48k 2ch (`sudo -n`) |
|---|---|---|---|
| `default` | NO_DEVICE ×2 | NO_DEVICE ×2 | NO_DEVICE ×2 |
| `pulse` | PULSE_REFUSED ×2 | PULSE_REFUSED ×2 | PULSE_REFUSED ×2 |
| `hw:1,0` | NO_DEVICE ×2 | NO_DEVICE ×2 | **OK 192000 B ×2** |
| `plughw:1,0` | NO_DEVICE ×2 | **OK 32000 B ×2** | **OK 192000 B ×2** |
| `plughw:CARD=PCH,DEV=0` | NO_DEVICE ×2 | **OK 32000 B ×2** | **OK 192000 B ×2** |
| `front:CARD=PCH,DEV=0` | HW_PARAM_UNSUPPORTED ×2 | NO_DEVICE ×2 | **OK 192000 B ×2** |
| `sysdefault` | NO_DEVICE ×2 | NO_DEVICE ×2 | NO_DEVICE ×2 |
| `sysdefault:CARD=PCH` | NO_DEVICE ×2 | **OK 32000 B ×2** | **OK 192000 B ×2** |

The controlled single-variable proof (no sudo at all): the same uid 1000 with the
supplementary group `audio` added —
`setpriv --reuid=1000 --regid=1000 --groups=29,1000 arecord -l` → card 1
`PCH [HDA Intel PCH]` listed, and `arecord -D plughw:1,0 …` → **rc 0, 32000 B**.
So the cause is the group/ACL fact (uid 1000 ∉ `audio`; `/dev/snd` is
`660 root:audio` with an ACL for `lightdm` only), and the node **does** have a
working capture path. Consequences for this document's central claim:

- the row *"peerbox: ALC3228 exists but all 13 /dev/snd nodes DENIED → client-side
  denied"* stays as the uid-1000 fact but is no longer a node verdict;
- **"HP is the only capture-capable node" is FALSE** — peerbox is capture-capable,
  privilege-gated; HP is simply the only node where uid 1000 may record;
- the untested-cell entry *"peerbox `permission` mode … root escalation is out of
  scope by design (privilege-free harness)"* was a wrong premise: `sudo -n` is
  fully non-interactive, and root was available the whole time.

`voice_input --list-devices` → rc 1, *"no capture device present (no soundcards
found)"* remains TRUE for uid 1000, and that message is inherited verbatim from
ALSA's access-refusal phrasing — which is exactly why it misleads. A privileged
enumerator reports `card 1: PCH … ALC3228 Analog`.

### peerbox as uid 1000 — the 32/32 refusal table (unchanged, still real)

| device | 16k mono | 48k 2ch | client mode |
|---|---|---|---|
| `default` | NO_DEVICE ×2 | NO_DEVICE ×2 | `no_soundcards` |
| `pulse` | PULSE_REFUSED ×2 | PULSE_REFUSED ×2 | `pulse_refused` |
| `hw:1,0` | NO_DEVICE ×2 | NO_DEVICE ×2 | `no_soundcards` |
| `plughw:1,0` | NO_DEVICE ×2 | NO_DEVICE ×2 | `no_soundcards` |
| `plughw:CARD=PCH,DEV=0` | NO_DEVICE ×2 | NO_DEVICE ×2 | — |
| `front:CARD=PCH,DEV=0` | HW_PARAM_UNSUPPORTED ×2 | HW_PARAM_UNSUPPORTED ×2 | — |
| `sysdefault` | NO_DEVICE ×2 | NO_DEVICE ×2 | — |
| `sysdefault:CARD=PCH` | NO_DEVICE ×2 | NO_DEVICE ×2 | — |

Multiset: **24 NO_DEVICE, 4 PULSE_REFUSED, 4 HW_PARAM_UNSUPPORTED**.
`voice_input --list-devices` → rc 1, *"no capture device present (no
soundcards found)"*. **Not one cell is OK** — honest, and the reason is access
control (`/dev/snd` DENIED, Pulse socket unlinked), not missing silicon.

### HP (`10.0.0.5`, x86_64) — 32/32 probe cells

| device | 16k mono | 48k 2ch | client mode |
|---|---|---|---|
| `default` | **OK** 32000 B ×2 | **OK** 192000 B ×2 | `ok` (32000 B) |
| `pulse` | **OK** 32000 B ×2 | **OK** 192000 B ×2 | `ok` (32000 B) |
| `pulse:<source>` (by name) | — | — | `ok` (32000 B) |
| `hw:1,0` | BUSY ×2 | BUSY ×2 | `busy` |
| `plughw:1,0` | BUSY ×2 | BUSY ×2 | `busy` |
| `plughw:CARD=Generic_1,DEV=0` | BUSY ×2 | BUSY ×2 | — |
| `front:CARD=Generic_1,DEV=0` | BUSY ×2 | BUSY ×2 | — |
| `sysdefault:CARD=Generic_1` | BUSY ×2 | BUSY ×2 | — |
| `sysdefault` | NO_DEVICE ×2 | NO_DEVICE ×2 | — |
| `gopi_e2e.monitor` (as an ALSA string) | — | — | `no_device` |

Multiset: **20 BUSY, 4 NO_DEVICE, 8 OK**. `--list-devices` → rc 0,
`card 1: Generic_1 … ALC236 Analog`.

Two facts the client contract depends on, both measured here:
1. **Only logical devices capture** — `default` and `pulse`. Every raw
   `hw:`/`plughw:`/`front:`/`sysdefault:` spelling of the *same* card is
   `BUSY`, because PulseAudio owns the PCM. `hw:1,0` cannot even express the
   contract: its hw-params are `CHANNELS: 2`, `RATE: [44100 192000]`.
2. **A PulseAudio source is only addressable by name** — `pulse:<source>`
   (translated to `PULSE_SOURCE`) captures `alsa_input.pci-0000_05_00.6.analog-stereo`
   at 32000 B/1 s, while the ALSA spelling `gopi_e2e.monitor` is `no_device`.

### rpi5 (`10.0.0.15`, aarch64) — 12/12 probe cells + client

| device | 16k mono | 48k 2ch | client mode |
|---|---|---|---|
| `default` | HW_PARAM_UNSUPPORTED ×2 | HW_PARAM_UNSUPPORTED ×2 | `unsupported_params` |
| `sysdefault` | HW_PARAM_UNSUPPORTED ×2 | HW_PARAM_UNSUPPORTED ×2 | — |
| `pulse` | NO_DEVICE ×2 | NO_DEVICE ×2 | — |
| `plughw:0,0` / `hw:0,0` / `vc4hdmi0` | — | — | `no_device` |

Multiset: **8 HW_PARAM_UNSUPPORTED, 4 NO_DEVICE**. Mechanism:
`ALSA lib pcm_asym.c:105 … capture slave is not defined` — the two cards are
HDMI **playback-only**; `arecord -l` prints an empty capture list and
`--list-devices` exits 0 with that same empty list. Cross-arch proof for the
client: the arm64 binary runs, enumerates, and names its refusals on aarch64.

### NAS DS918Plus (`10.0.0.9`, x86_64) — 1/1 probe cells + client

No ALSA stack at all: `arecord`, `parecord`, `pactl` all ABSENT; no
`/proc/asound/*`. Probe emits one honest row
`DS918Plus (none) - - - 0 127 0 ERROR arecord absent`; the client exits 1 with
*"arecord not found in PATH"*, and `--device default` → `no_device` (rc 1).
**A node with no audio stack is a result, not a crash** — and `/tmp` on DSM is
`tmpfs noexec`, so a copied binary must live in `$HOME` (`/var/services/homes/<u>`).

---

## 3. The ssh blocker — hosta (10.0.0.13)

Re-verified 2026-09-13 06:25 from peerbox, still blocked:

```
$ ping -c4 10.0.0.13       → 4 received, 0% loss, rtt avg 0.82 ms
$ curl -m5 -o /dev/null -w '%{http_code}' http://10.0.0.13/   → 200
$ ssh -o BatchMode=yes nick@10.0.0.13 hostname
nick@10.0.0.13: Permission denied (publickey,password).
$ ssh -vv … | grep -iE 'offering|can continue'
debug1: Offering public key: /home/user/.ssh/id_ed25519 ED25519 SHA256:WB/Gtn+…
debug1: Authentications that can continue: publickey,password
```

**Root cause (not a symptom)**: the offered key is not in that account's
`authorized_keys`, and `BatchMode` forbids the password fallback. The decisive
finding is that the trust is **one-way hosta → peerbox**: peerbox's
`authorized_keys` holds a hosta key whose private half lives **on hosta**, and
the only private key peerbox owns (`id_ed25519`) is refused here — as it is on
`.11` and on `.6` reached over the network. The fix (add peerbox's
`id_ed25519.pub` to hosta) needs a write on the one host the agent cannot log
into, so no agent-side work can unblock it.

**Filed as its own task**: `01a097ac-f9b2-7deb-94a3-cf96b4ea7b20`
(tags `bug,infra,ssh`) — **status `verified`**, with the deliberate-decision
record `devdoc/hosta-unreachable-decision-20260913.md` merged to master
(`d066b059`, merge `ce7e127a`): *hosta is not a managed node; re-entry requires
one operator action.*

---

## 4. Explicitly UNTESTED cells, each with its reason

| Untested cell | Reason |
|---|---|
| **hosta × any device** | ssh key refused — non-interactive login impossible (§3); bug task verified, decision recorded |
| `.1 .3 .7 .8 .10 .12 × any device` | no reachable sshd (port 22 refused) — cannot be a keyless test node |
| `.11 × any device` | peerbox's own wlo1 interface; key refused over the network path |
| **HP `gopi_e2e.monitor` via a raw ALSA string** | measured `no_device` (ALSA cannot name a Pulse source); the working route is `pulse` + `PULSE_SOURCE`, which **is** tested (`ok` 32000 B) |
| **peerbox `permission` mode** | **MEASURED 2026-09-13** (was listed untested): as root and as uid 1000 + group `audio` the capture succeeds — `plughw:1,0` 32000 B, `hw:1,0` @48k/2ch 192000 B. The earlier entry called root escalation "out of scope by design (privilege-free harness)"; that premise was wrong, `sudo -n` is non-interactive and NOPASSWD. The client's `no_soundcards`/`permission` reporting for the uid stays correct — what was wrong was reading it as a node verdict |
| **USB microphone × any node** | **no USB audio device is physically attached anywhere** — `lsusb` on HP/peerbox/rpi5 shows no UAC device (HP: camera, Bluetooth, hub, card reader only). The "second device" cell of the epic is satisfied by the by-name Pulse source instead |
| **Any client cell on branch tips `da80e41a` / `7da0ac99`** | those tips **do not compile** (`opts.device/seconds/recorder/out undefined` — they carry `cli_voice_capture.go` without the `voiceOptions` fields); the tracked tip `05ab31c0` builds and **is an ancestor of master**, so live cells run on master's code |
| **NAS capture beyond `no_device`** | no ALSA stack exists — nothing to capture from |
| **`hw:2,0`+ (third+ card) on any node** | no node has more than one capture-capable card |

---

## 5. What was actually run — commands and observed output

Every claim above is reproducible with these commands (all keyless, all
non-interactive); the raw artifacts are committed next to this file.

```bash
# (1) node universe + keyless reachability
for i in $(seq 1 254); do (ping -c1 -W1 10.0.0.$i >/dev/null 2>&1 && echo 10.0.0.$i) & done; wait
for ip in 1 3 5 6 7 8 9 10 11 12 13 15; do timeout 6 ssh -o BatchMode=yes -o ConnectTimeout=3 \
    nick@10.0.0.$ip 'hostname; uname -m'; done

# (2) the probe, per node (exit 0 whenever the probe ran)
scp scripts/voice-mic-probe.sh nick@<host>:/tmp/vmp.sh
ssh -o BatchMode=yes nick@<host> '/bin/bash /tmp/vmp.sh > /tmp/probe-<node>.stdout 2>&1; echo EXIT=$?'

# (3) the live client cells (master build)
scp /tmp/gopi-master nick@<host>:/tmp/ && ssh -o BatchMode=yes nick@<host> \
  '/tmp/gopi-master voice_input --list-devices; /tmp/gopi-master voice_input --client --device <D> --seconds 1 --stdio'

# (4) determinism: two probe runs, compare the verdict tuples
diff <(sort run1.tsv) <(sort run2.tsv)      # empty  → set-equal

# (5) the human-free end-to-end (HP capture → peerbox engine → text)
ssh nick@10.0.0.5 'PULSE_SOURCE=gopi_e2e.monitor /tmp/gopi-master voice_input --client \
  --device pulse --seconds 3 --out /tmp/hp-e2e.pcm' &
ssh nick@10.0.0.5 'sleep 1.2; paplay --device=gopi_e2e /usr/share/sounds/alsa/Front_Center.wav'
#   → [voice-capture] device="pulse" mode=ok bytes=96000 want=96000   peak 15183
curl -F audio=@/tmp/hp-e2e.wav http://10.0.0.6:7788/transcribe
#   → {"chunk":15,"elapsed_ms":59610,"engine":"funasr-nano","text":"Front, center."}
python3 scripts/asr_score.py hyp.txt ref.txt   # → CER 0.00%, accuracy 100.00%
```

**Observed and recorded**:
- probe exit 0 on **all four** nodes (a capture-less node is a result);
- peerbox `table.tsv` **md5-identical** across two runs (`d5acaad7…`);
- HP two runs **set-equal** on all 32 verdict tuples (only row order and the
  capture out-path differ) — `hp-determinism.txt`;
- end-to-end transcript **exactly** the ground truth (`Front, center.`), CER 0.00%;
- the real microphone path (HP `alsa_input.pci-0000_05_00.6.analog-stereo`,
  peak 365) transcribed to `/sil` — an honest "no speech present" result, not a
  client defect.

## 6. Side finding from this node sweep (fixed on its own branch)

Running the live cells on HP — whose user locale is `zh_CN.UTF-8` — exposed a
**locale-dependent failure classifier**: arecord localizes its stderr via
gettext, so the busy case arrived as `音乐打开错误： 设备或资源忙`, matched none
of the English markers, and degraded from `busy` to the generic `error`, losing
the actionable advice. `LC_ALL=C` → `mode=busy`; `LC_ALL=zh_CN.UTF-8` →
`mode=error`, same device, same second. Fixed by pinning the **child's** locale
(`LC_ALL=C`) in `runVoiceCapture`, with two regression tests — branch
`fix/voice-capture-locale-classify` (`10e4f5bd`),
report `tasks/voice-capture-locale-report.md`.
