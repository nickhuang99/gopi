<!-- doc-class: historical -->
# CH9329 as gopi's `Actuate` Primitive — Evaluation + Go-Side Integration Design

**Date**: 2026-09-10 · **Task**: evaluate the existing CH9329 USB-HID POC at `/home/user/work/ch9329`
as gopi's hardware Actuate primitive for Computer Use. **Scope: design only — no implementation.**
**Related**: `devdoc/computer-use-second-blue-ocean-4-primitives-20260910.md` (the 4-primitive frame),
`devdoc/shot-design.md` (the `Capture` precedent this design mirrors).

---

## 0. Hardware state of this evaluation (read first)

> ⚠️ **The CH9329 hardware is NOT attached to the box this report was written on.**
> Verified this session: no `/dev/ttyUSB*` exists, no CH9329 (`1a86:e129`) entry in `lsusb`.

Every claim below is therefore tagged:

- **[CODE]** — read out of POC source or the gopi tree, and re-verified by recomputation where possible.
- **[RECORDED]** — evidence captured earlier from the POC's own Raspberry Pi (a recorded device tree),
  i.e. observed once, but not re-observed now.
- **[SPEC]** — from the WCH CH9329 protocol documentation. Network access to the primary docs failed
  this session (CSDN `HTTP 521`, zhihu `HTTP 403`, `wch.cn` is JS-rendered and yielded no body),
  so these are carried from the prior session's reading and **must be re-confirmed against the
  datasheet before implementation**. A third-party implementation was fetched successfully and is
  cited as independent corroboration: [`sunasaji/serial-hid-kvm`](https://github.com/sunasaji/serial-hid-kvm)
  (`src/serial_hid_kvm/hid_protocol.py`, 162 lines, MIT). §14 records what was *measurably* verified.
- **[UNVERIFIED]** — reasoned from the above, needs hardware to settle. Collected in §13.

`/home/user/work/ch9329` was read only; **nothing in it was modified.**

---

## 1. Verdict

**DOABLE, with one hardware add-on and one honest concession.**

The CH9329 gives gopi a **firmware-free, driver-free, OS-agnostic** keyboard/mouse primitive: the chip
enumerates as a standard USB HID composite device, so the target needs no software, no agent, no API —
exactly the "non-invasive, reuse the human I/O channel" property the Computer-Use thesis depends on
(`devdoc/computer-use-second-blue-ocean-4-primitives-20260910.md`, chain step 4: "Computer Use = 复用人类 I/O 通道，非侵入"). Two independent implementations confirm the protocol is small
(§4) and that the whole recipe is ~$15 of commodity parts (§11).

The concession: **arbitrary non-ASCII text entry is not solvable by the HID device alone** — no
keyboard can type what its layout cannot express. §7 gives the honest boundary and the fallbacks.

---

## 2. The POC as it stands

`/home/user/work/ch9329` — not a git repo, 24 `.py` files, 2079 lines total, mode 0664, dated 2026-09-10.

**There is no driver module.** The protocol code is copy-pasted into every script — `def calc_checksum`
is re-declared in **14** files, and `SERIAL_PORT`/`BAUDRATE` are re-typed alongside it. Two lineages are
visible:

| Lineage | Files | Character |
|---|---|---|
| "clean" units | `units/*.py` (7 files, 28–70 lines each, 369 total) | one script = one experiment; readable |
| agent-generated | `doubao_*.py`, `deepseek*.py`, `fixed.py`, `hello.py`, `move_test.py`, `mouse_test.py`, `simple_move.py` | exploratory, duplicated, carries the bugs in §5 |

### 2.1 Coverage matrix

| Capability | Covered? | Where |
|---|---|---|
| Serial open @ 9600 8N1 | yes, every file | `serial.Serial(port, 9600, timeout=…)` |
| Frame build + checksum | yes, ~7 copies | `units/*.py` `calc_checksum` |
| Keyboard general report (0x02) | yes | `units/keyboard_type.py`, `move_test.py` |
| Mouse relative (0x05) | yes | `units/mouse_rel_move.py`, `mouse_calibrate_click.py` |
| Mouse absolute (0x04) | yes | `units/mouse_abs_move.py`, `units/mode_test.py`, `doubao_measure.py` |
| Mode switch (composite/abs) | attempted, **defective** | `units/mode_fix.py` (§5.2) |
| Abs-range calibration probe | yes, manual | `doubao_measure.py:110` sweeps 6 resolution pairs × 3 commands and reads back what the operator types |
| Read the chip's reply (ACK) | **never** | zero occurrences of `ser.read(` in any of the 24 files |
| Full ASCII / shift handling | **no** | `units/keyboard_type.py:10` is a 21-entry hand-picked table |
| Non-ASCII text | X11-only hack | `deepseek_unicode.py`, `fixed.py:97` (Ctrl+Shift+U + hex) |
| Key-release safety on failure | **no** | release is a plain statement after a `sleep` |
| Coordinate calibration closed loop | **no** | scale factor is a hardcoded assumption |

The keyboard table point is worth stating plainly: `units/keyboard_type.py` can type
`"Hello World 256x256:0x04>"` and essentially nothing else — its table holds exactly those 21
characters (recounted from source). There is **no shift-handling table at all**: `LSHIFT` appears only as a
*modifier mask* for the Ctrl+Shift+U hack (`fixed.py:23`, `move_test.py:21`), never to produce an
uppercase letter or a shifted symbol. Uppercase, `!`, `?`, `@`, quotes — all absent. (`fixed.py`'s
`HID_CODE` table has 17 entries — `U` + digits `0-9` + `A-F` — sized for the hexadecimal codepoint
hack, not for text.)

---

## 3. Recorded hardware evidence [RECORDED]

The POC ships `devices` — a captured `evtest`-style input dump from its Raspberry Pi, showing the
device after the chip was switched to **composite (mode 0)**. VID/PID `1a86:e129`, string
`"WWW.WCH.CN WCH UART TO KB-MS_V1.8"`, four interfaces:

| Interface | `Handlers` | Descriptor evidence |
|---|---|---|
| Keyboard | `sysrq kbd leds event4` | full `KEY=` bitmap, `LED=7` |
| System Control | — | power/sleep keys |
| Consumer Control | — | media keys |
| **Mouse** | `mouse0 event7 js0` | `REL=903`, **`ABS=3`** (bit0 ABS_X \| bit1 ABS_Y), `KEY=1f0000` (3 buttons) |

Two facts follow, and they matter:

1. **`ABS=3` proves the absolute-pointer capability is real**, not a datasheet promise — the HID
   descriptor advertises both X and Y absolute axes. This is the single most load-bearing piece of
   evidence for the whole design, and it was captured from working hardware. [RECORDED]
2. The composite device exposes keyboard **and** mouse simultaneously, so gopi never has to switch
   modes to mix typing and clicking. [RECORDED]

The 9600-baud default in every POC script matches the chip's factory default. [CODE/SPEC]

---

## 4. Protocol reference

### 4.1 Frame

```
+------+------+------+-----+-----+------------------+------+
| 0x57 | 0xAB | ADDR | CMD | LEN |  DATA (LEN bytes)| SUM  |
+------+------+------+-----+-----+------------------+------+
SUM = (sum of all preceding bytes) & 0xFF        ADDR = 0x00 (default)
```

Confirmed three ways: recomputed from the POC's own frames [CODE], matched against the fetched
third-party implementation (`hid_protocol.py::_checksum`, `build_packet`) [CODE — independent], and
consistent with the WCH application note [SPEC].

### 4.2 Commands

| CMD | Meaning | DATA layout | Evidence |
|---|---|---|---|
| `0x02` | Keyboard, general | 8 bytes = HID boot report `[mod, 0, k1..k6]` | [CODE] POC ×4 files + `build_keyboard_packet` |
| `0x03` | Keyboard, **media** | media usage codes | [SPEC] — see §5.1, the POC mislabels this |
| `0x04` | Mouse, **absolute** | 7 bytes `[0x02, btn, Xlo, Xhi, Ylo, Yhi, wheel]` | [CODE] `mouse_abs_move.py:29` + `build_mouse_abs_packet` |
| `0x05` | Mouse, **relative** | 5 bytes `[0x01, btn, dx, dy, wheel]` | [CODE] `mouse_rel_move.py:15` + `build_mouse_rel_packet` |
| `0x0F` | Reset | — | [SPEC] |
| `0x01` | **Disputed** (see §5.2) | — | `hid_protocol.py` calls it `CMD_GET_INFO`; `mode_fix.py:8` calls it `CMD_SET_CONFIG` |

The first data byte is the **report ID**, not padding: `0x02` selects the absolute-pointer report,
`0x01` the relative one — that is why `0x04` carries `0x02` and `0x05` carries `0x01`, and why both
immediately follow the LEN byte `0x05`/`0x04`-style values, which is what makes the frames so easy to
misread (§5.3).

### 4.3 Replies — the gap in the POC

Per the WCH protocol each command is answered by a status frame (`0x82` keyboard / `0x84` abs-mouse /
`0x85` rel-mouse, `0x00` = success) [SPEC]. **No POC file ever reads it**, and — notably — the
independent `serial-hid-kvm` library's `send()` does not either; both are fire-and-forget. This is the
single highest-value behavioural upgrade available to us (§4.3 → §10.3): reading the ACK converts "we wrote
bytes" into "the chip accepted the command".

---

## 5. Bugs found in the POC (all [CODE], checksums recomputed)

### 5.1 `0x03` used as a mouse command — 3 files

`mouse_test.py:10` and `simple_move.py:10` define `CH9329_CMD_MOUSE = 0x03 # 鼠标指令` and build
frames with it (`mouse_test.py:35`, `simple_move.py:35`); `move_test.py:13` does the same
(`CMD_MOUSE_GENERAL = 0x03 # 鼠标指令码（正确）` — the comment asserting correctness is wrong).
`0x03` is the **media-keyboard** command [SPEC]; the mouse commands are `0x04`/`0x05`. The mislabel is
self-propagating: `doubao_test_mouse.py:41` calls `send_mouse_abs(960, 540, cmd=0x03)` and only works
because the same file passes `cmd=0x04` elsewhere.

### 5.2 `mode_fix.py` reset frame is invalid

`units/mode_fix.py:49` sends `[0x57, 0xAB, 0x00, 0x02, 0x00, 0x00]` as a "reset" frame.

- It claims `CMD = 0x02` (keyboard with `LEN = 0`) — a keyboard command with an empty report.
- Worse, the trailing byte is **not its own checksum**: `0x57+0xAB+0x00+0x02+0x00 = 0x104`,
  `& 0xFF = 0x04`, so the frame's last byte should be `0x04`, not `0x00`. **The frame is invalid and
  would be rejected.**
- The real RESET is `0x0F` [SPEC].

So the POC's mode-switch recipe ("write config, then reset to apply") **never actually reset the
chip** — the reported success was either a chip-side auto-apply or operator misattribution. Any
implementation must get this right (§4.2, and it is the reason §13.2 asks the datasheet who owns `0x01`).

### 5.3 `serial_test.py` sends a malformed frame

`units/serial_test.py:16` (comment: "你之前跑通过的格式" — the format you ran successfully before):

```python
frame = bytes([0x57,0xAB,0x00,0x05,0x05,0x01,0x00,0x00,0x0A,0x00,0x00])
```

Two defects: the DATA run starts `0x05, 0x01, …` where the relative report must start `0x01` (report
ID) — the fields are shifted by one — and the final byte `0x00` is **not** the checksum
(`0x57+0xAB+0x00+0x05+0x05+0x01+0x00+0x00+0x0A+0x00 = 0x117`, `& 0xFF = 0x17`). The frame is invalid.

### 5.4 Class-level design defects

| # | Defect | Consequence |
|---|---|---|
| D1 | No ACK read (§4.3) | a failed/unplugged chip is indistinguishable from success |
| D2 | No release-on-failure (`mouse_calibrate_click.py:33-36`: press, `sleep(0.1)`, release) | a crash inside that 100 ms window leaves the **left mouse button held** on the target |
| D3 | Fixed magic sleeps everywhere (12 distinct values, 0.01 s–5 s, **158 call sites in 22 of the 24 files**) | unmeasured, unrooted, and they *are* the flakiness catalogue (§9) |
| D4 | Protocol code copy-pasted (`calc_checksum` ×14 files) | the §5.1 mislabel propagated to 3 files; fixes cannot be applied once |
| D5 | Hardware assumptions inlined (`/dev/ttyUSB0`, `1920×1080`, `4096`, `9600`) | any other target requires edits across files |
| D6 | `ch9329Comm` dependency (`deepseek_unicode.py:6`) not present on any box we checked | that script cannot run at all here |

---

## 6. Hard problem 1 — absolute-mouse accuracy, calibration, and how Verify confirms a click

### 6.1 Where the error comes from

The absolute report carries 16-bit X/Y, but the **usable** range is the chip's configured absolute
resolution, hardcoded as `4096` throughout the POC (`mouse_abs_move.py:11-12`,
`mode_test.py:25-26`, `deepseek/test.py:10`). The mapping the POC assumes is a pure scale:

```
chip_x = int(screen_x * 4096 / screen_w)        # mouse_abs_move.py:20
```

Three independent error sources, all real, none measured:

1. **The chip's reference resolution is a configuration value, not a constant.** `doubao_measure.py:110`
   exists precisely because the author discovered this: it sweeps `{(256,256), (512,512), (1000,1000),
   (2048,2048), (4096,4096), (1920,1080)} × cmd ∈ {0x03,0x04,0x05}` and has the operator *read back*
   which config produced motion. That is a human-in-the-loop calibration invented because the
   assumption failed. [CODE — the script is its own evidence]
2. **The desktop is not the screen.** HID absolute coordinates are interpreted against the OS's
   virtual-desktop rectangle. With two monitors the mapping is the union rect, so a hardcoded
   `1920×1080` maps to the wrong physical pixels — an offset *and* a scale error.
3. **OS-side scaling.** A scaled/DPI-adjusted desktop changes the px↔device mapping again.

**Quantization is *not* the limiting factor**, and this is worth stating because it is the objection
people raise first: 4096 steps over 1920 px = **0.47 px/step** (0.94 px/step at 3840 wide), i.e.
sub-pixel on any mainstream display. Whole-pixel targeting is not the problem — the *mapping* is.

A fourth error source that the design must not forget: **relative movement is subject to OS pointer
acceleration** (which is why the POC's relative "draw a square" scripts drift). Absolute mode bypasses
acceleration entirely. So absolute is the primary primitive and relative is only a nudge — and even
the nudge is non-linear. [UNVERIFIED — acceleration behaviour per OS]

### 6.2 Calibration: closed loop, three points

Do not guess the mapping; **measure it**. The procedure is small and needs no hardware modification:

1. Move absolutely to a candidate corner region, capture, and locate the pointer by pixel search for
   the OS cursor glyph — or, more robustly, drive to a *known UI landmark* (a maximised window corner).
2. Repeat at 2–3 well-separated points.
3. Fit an affine map (2×2 + translation) from device units → screen px; store it in config.
4. Re-verify at a fourth point the fit was not trained on.

This is a one-time per-target step, and it is exactly the kind of artifact that belongs in
`~/.gopi/settings.json` (§10.4) rather than in code. The "just scale by 4096" shortcut in the POC is
the thing this replaces.

### 6.3 How Verify confirms a click landed — pixel truth, not OCR

The Computer-Use devdoc already fixes the right instinct (its 4-primitive table, `Wait`/`Verify` rows): Verify is a *second
observation*, not a re-assertion. Concretely, in order of preference:

1. **Crop-and-diff (deterministic, zero LLM cost).** Capture before, capture after, crop the expected
   region, compute the changed-pixel fraction. Above a named threshold → the click landed. This is
   cheap, has no model in the loop, and is immune to hallucination. Pure-Go: `image/png` decode + an
   integer diff loop — no dependency (Rule 10.7 / Rule 9 zero-dep discipline, GOPI.md).
2. **`vision_analyze` only on ambiguity** (diff is small / the region is text). This keeps the token
   cost where the money is — which is the whole economic argument of the second-blue-ocean devdoc.
3. **Never** infer success from "we wrote the bytes". §5.4/D1 is exactly that mistake.

Local tooling for the prototyping stage is available and verified on this box: `compare` and `convert`
(ImageMagick) at `/usr/bin/`, plus PIL 12.2.0 — usable for offline ground truth while the Go
implementation is written.

---

## 7. Hard problem 2 — arbitrary Unicode text entry

**The honest answer: the HID keyboard cannot do it in general, and no design can change that.** The
device types scan codes; what a scan code produces is entirely up to the target's keyboard layout and
input method. The POC's trick —

```python
keyboard.send_data('U', 0x03)     # 0x03 = Ctrl+Shift modifier mask; deepseek_unicode.py:29
```

— is the **IBus/X11 "Unicode input" compose** (`Ctrl+Shift+U`, hex codepoint, `Enter`). Its scope:

| Target | Ctrl+Shift+U works? | Notes |
|---|---|---|
| Linux/X11 + IBus or Fcitx5 | yes | the POC's case (its Pi was X11) |
| Linux/Wayland | **compositor/IME dependent** | plain wlroots/some sessions: no |
| Windows | **no** | the mechanism does not exist |
| macOS | **no** by default | needs the "Unicode Hex Input" source + `Option`+hex |

Per-target alternatives, all with a real cost:

- **Windows**: `Alt` + decimal numpad code — only works for the OEM codepage, dead for CJK and non-BMP,
  and requires a numpad. Not a solution.
- **macOS**: enable "Unicode Hex Input", then `Option`+4 hex digits. Configurable-once, so viable, but
  it is *target-side configuration* — the non-invasiveness assumption is weakened.
- **Any target with an IME**: set the IME to the right mode and type the **romanisation** (pinyin for
  Chinese, romaji for Japanese). Robust, zero target-side hacks, but it moves the problem into "does the
  LLM know the pinyin", and it is IME-state dependent.
- **Clipboard paste**: the obvious workaround — **and it is unavailable to us by construction.** We have
  no software channel to the target; that is the entire point of the HID approach. We can only *press*
  `Ctrl+V` at something that already has the text in its clipboard.

**Where that leaves the design:**

- **ASCII printable: guaranteed**, and only if we ship a real HID usage table with shift handling
  (the POC's 21-char table, with no shift path at all, is not a foundation — §2.1).
- **Non-ASCII: opt-in "input dialect" configuration**, per target, with three documented dialects:
  `unicode-hex` (IBus/macOS-hex), `ime-romanise` (pinyin/romaji), `none` (refuse loudly, never silently
  type mojibake). Refusing loudly is the correct default — a silent wrong character in a form field is
  worse than a hard failure (Rule 15a spirit: never let a required semantic silently vanish).
- Also needed, and currently absent: **keyboard-layout awareness** (US/UK/DE/FR/JP). The independent
  `serial-hid-kvm` implementation ships `hid_layouts.py` for exactly this reason — independent
  evidence that one hardcoded layout is not enough.

**Known-unsolvable, stated plainly:** characters outside the target's installed fonts/IME coverage, and
anything requiring a GUI gesture the HID spec has no scan code for (e.g. touch gestures, three-finger
swipes). Those are out of scope for this primitive, permanently.

---

## 8. Hard problem 3 — key-release safety on every failure path

This is the **highest-severity** issue in the whole design, because the failure mode is not a failed
task — it is *damage on the target*: a stuck `Ctrl` + `c` in a shell, or a held left mouse button that
drag-selects the moment anything moves.

Failure paths, classified:

| Path | What the target sees | Verdict |
|---|---|---|
| `kill -9` / panic mid-chord | last HID report stays latched | **unsafe** |
| serial cable unplugged, USB still attached | last report stays latched | **unsafe — the worst case**: gopi has no channel and the target stays broken |
| controller box powers off | depends on chip latched state | **unsafe** [UNVERIFIED] |
| USB unplugged at the target | OS releases the device's keys on disconnect | generally safe |
| normal exit | nothing sends a release unless we do | **unsafe** |

The POC is unsafe on four of five (D2). **Design mitigations, in layers:**

1. **Atomic press→release transactions.** No API that presses without a paired release on the same code
   path; chording is one call, never two (`build_keyboard_report` + a guaranteed
   `build_keyboard_release_packet`).
2. **`release-all` on every exit path** — `defer`, `defer` inside the *signal handler* too
   (SIGINT/SIGTERM), and a `recover()` for panics. Plus a release-all on `open()` (assume the previous
   run died) and on `close()`.
3. **A janitor goroutine** holding an "open keys/buttons" bitset: if any set bit is older than a
   watchdog interval with no in-flight action, emit release-all. Bounded, self-healing.
4. **Serial write deadlines** on every write, so a wedged port cannot hang the release path.
5. **Refuse to exit with pending presses** — the close path must either succeed at releasing or report
   failure loudly (Rule 15b: never swallow the error).
6. **Chip reset (`0x0F`) as the last resort**, re-applying config afterwards (get §5.2 right).

**Residual risk that no software layer removes:** if the USB link itself is alive but the serial side is
dead (row 2), software is powerless — the latch lives in the chip. The only complete mitigation is
**hardware**: a ~$5 USB relay/switch on the CH9329's upstream port, so gopi can power-cycle it. This
should be recommended, not hidden: *a software-only release guarantee is not achievable.*

---

## 9. Hard problem 4 — timing and flakiness; are the POC's fixed sleeps justified?

**No.** The POC's 158 `time.sleep` calls (12 distinct values, 0.01 s to 5 s, D3) are of three different kinds, and conflating them is
the flakiness bug:

| Kind | Example | Justified? |
|---|---|---|
| Human-observation padding | `move_test.py:80` `sleep(0.1)  # 延长延时，确保移动可见`, `mouse_abs_move.py:47` `sleep(3)` | legitimate for a demo, **meaningless in production** — delete |
| Physical/wire time | frame byte-time at 9600 baud | real but tiny: an 11-byte frame ≈ 11.5 ms; at 115200 ≈ 1 ms |
| App/OS readiness | typing into a window that is not focused yet | **real, and fixed sleeps are the wrong tool** |

Sources of flakiness, ranked by impact:

1. **The target app is not ready** (window not mapped, IME mid-composition, modal not painted). Fixed
   sleeps "work" only because they are generous; under load they fail. The correct primitive is
   **Verify (§6.3)** — capture and confirm the state, don't sleep and hope.
2. **No ACK (D1)** — a dropped frame is invisible; the next frame then compounds the error.
3. **Typing rate vs the target's input stack.** Applications drop or reorder synthetic keystrokes
   (notably IMEs, which need composition time), and the OS HID polling interval (1–10 ms) sets a floor.
   The POC's `keyboard_type.py:31,36` (0.1 s then 0.05 s per char) is guesswork; the independent
   `serial-hid-kvm` uses a 10 ms release delay. Both are unmeasured for our targets. [UNVERIFIED]
4. **Baud saturation** at 9600 if frames are queued — 960 B/s is only ~87 frames/s.

**Design rule:** every delay becomes a **named constant with a documented rationale**, overridable from
config, and *no* sleep substitutes for a Verify. A fixed sleep is acceptable only where it is a floor
enforced by physics or a documented protocol requirement — and then it must say which.

---

## 10. Go-side integration design

### 10.1 Precedent to copy: `Capture` is a CLI verb, not a tool

This is the key structural finding, and it resolves the Rule 3 tension. `gopi` today has **16 tools**
(`GOPI.md:87`, `internal/agent/loop.go` `claudeToolDefs`, names in `internal/toolnames/names.go`,
drift-guarded by `TestGOPIRule3_MatchesClaudeToolDefs` in `internal/agent/gopidoc_sync_test.go:54`).
Screenshot capture — i.e. the `Capture` primitive — is **not** one of them:

```
cmd/agent/cli_verbs.go:67       CliVerbShot = "shot"
cmd/agent/cli_shot.go:64        isShotSubcommand(args)
internal/shot/{cdp,chrome,shot}.go   1013 lines, zero-dependency raw-CDP client
internal/config/help.go:347     help text + the "gopi shot --self && gopi vision_analyze" loop
```

The LLM reaches it **through `bash`**. So the Computer-Use primitive set can be completed
**without adding a single tool name**: `capture` already exists as `gopi shot`; `wait` is a documented
bash sleep; `verify` is `shot` + diff/`vision_analyze`; and `actuate` should be built the same way —
`internal/<engine>` + a `gopi actuate` verb.

> **Recommendation: ship `Actuate` as a CLI verb first. Do NOT add a 17th top-level tool.**
> Promoting it to a first-class tool (`toolnames.ToolActuate` + `claudeToolDefs` + routing prompt) is a
> *separate, later* decision that must be justified by LLM-adoption evidence per Rule 11 — the same bar
> `shot` never had to meet because it rides on `bash`.

This keeps Rule 3 intact, avoids touching `gopidoc_sync_test.go`'s 16-name contract, and is the
lowest-token-cost shape for the model (one bash call, no schema in the prompt).

### 10.2 Package layout (zero new dependencies)

```
internal/ch9329/
    protocol.go     frame encode/decode, checksum, command consts, ACK parsing
    keymap.go       USB HID usage table (full ASCII + shift), layout tables
    dial.go         open /dev/ttyUSB0, termios (raw 8N1), write deadlines
    driver.go       press/release transactions, open-keys bitset, janitor, release-all
    driver_test.go  table-driven frame tests (golden bytes from §4.2) + janitor test
```

- **Serial I/O**: `os.OpenFile` + termios via `golang.org/x/sys/unix` — `Termios`, `IoctlSetTermios`,
  `TCSETS = 0x5402`, `B9600`, `B115200` were all verified present in the **already-vendored module
  cache** (`golang.org/x/sys v0.37.0`, currently an *indirect* require of `modernc.org/sqlite`).
  Promoting it to a direct require is a one-line `go.mod` change with **no new download, no CGO, no
  new module** — the single static binary (Rule 12) is preserved.
  *Alternative considered and rejected:* shelling out to `stty` — an external process on the hot path
  of every keystroke, and a failure mode of its own; the module is already in the graph.
  *Note:* `go.mod` today has exactly `modernc.org/sqlite` + `x/sys`/`x/exp`/4 small indirects — no
  `vendor/` directory, no serial library anywhere in the tree (checked).
- **Constants (Rules 1/13/14)**: protocol bytes, command codes, report IDs, timeouts, limits and the
  default port/baud are **named constants in the owning package**, referenced — never re-typed. Values
  shared *across* packages (the CLI verb verbatim, the report-ID semantics a tool schema might validate)
  go in a small exported-constant package following the `internal/toolnames` precedent (Rule 14). A
  literal-gate test in the style of `TestUnknownNode_MirrorLiteralGate` should pin the wire bytes
  (`0x57`, `0xAB`, `0x0F`) to their single definition site (Rule 13).
- **No silent failure (Rule 15)**: every write's error is propagated; a missing ACK is a loud error, not
  a retry-and-forget; a config key that cannot be applied (e.g. unknown dialect) fails the call.

### 10.3 Proposed CLI surface (verb + sub-actions, not N verbs)

```
gopi actuate key  --chord ctrl+shift+u | --key enter       # one atomic press+release
gopi actuate text --layout us [--dialect ascii|unicode-hex|ime-romanise] "Hello, 世界"
gopi actuate mouse --abs 960,540 [--click left] [--scroll -3]
gopi actuate mouse --rel 20,-10                            # nudge; acceleration-affected
gopi actuate release-all                                   # the emergency primitive
gopi actuate calibrate --points 4                          # §6.2 closed loop
```

**Granularity argument.** One verb + four sub-actions, because: (a) the atomicity boundary must be
visible to the caller — `key`/`text`/`mouse` are *transaction* units, and `release-all` exists
separately so an operator (or a retry path) can always force a safe state; (b) `--chord` is exposed
because `Ctrl+C`-class interruptions are the primitive's most important single use; (c) a single verb
means one dispatch site, one help entry, one completion entry — the `CliVerbShot` pattern — instead of
four near-duplicate predicates. Everything takes `--port`/`--baud` overrides so tests and multi-target
setups never edit code (D5).

### 10.4 `~/.gopi/settings.json` shape

Existing top-level keys: `api_key, base_url, model, reasoning_effort, vision_model, vision_base_url,
compute_nodes, sync_peers, memory_peers, repos, thinking_budget, notify`. The addition follows the same
pattern — a single block, read in exactly one place (Rule 2's discipline applied to config):

```jsonc
"hid": {
  "port": "auto",              // "auto" = probe /dev/ttyUSB* for the CH9329
  "baud": 9600,
  "layout": "us",              // us|uk|de|fr|jp — scancode→layout base
  "dialect": "ascii",          // ascii|unicode-hex|ime-romanise|none
  "abs_calibration": {          // §6.2 — per-target, fitted once
    "ref": [4095, 4095],        // the chip's configured absolute resolution
    "desktop": [1920, 1080],
    "scale": [1.0, 1.0], "offset": [0, 0]
  },
  "timing": { "ack_timeout_ms": 200, "inter_key_ms": 8, "release_delay_ms": 10 },
  "limits":  { "max_keys_per_call": 200, "max_clicks_per_call": 20 },
  "enabled": true               // kill switch (§10.5)
}
```

### 10.5 Safety interlock against runaway typing/clicking

A runaway HID device is qualitatively worse than a runaway shell command: it types into *whatever has
focus on another machine*. Layers, cheapest first:

1. **`enabled` + a sentinel kill switch** — `~/.gopi/hid-disabled` present ⇒ every actuate call refuses
   immediately, no frame written. Filesystem-level, works even if config parsing is broken.
2. **Per-call hard caps** (`limits.*`, named constants): an invocation typing 200+ characters or 20+
   clicks must be split deliberately. This bounds a runaway loop's damage per call.
3. **Rate limiting** in the driver, so a retry storm cannot machine-gun the target.
4. **An audit trail**: every frame logged (command, count, target coords) to the session log — a runaway
   is then reconstructable. Never log the text payload verbatim when it could be a password (log a
   length + a hash).
5. **`release-all` as a documented emergency verb**, plus automatic release on SIGINT/SIGTERM/panic.
6. **Optional human-presence guard** (check the target for recent pointer activity before seizing the
   keyboard) — proposed but *not* recommended for v1: it needs a capture loop and a heuristic, and a
   false negative is as bad as no guard. Defer.

---

## 11. Minimum complete loop from off-the-shelf parts

| Primitive | Part | Cost | Status |
|---|---|---|---|
| **Actuate** | CH9329 module + CH340 USB-serial cable | ~$5 | design here |
| **Capture** | USB HDMI capture dongle + `ffmpeg` → PNG | ~$10 | partial: `gopi shot` (URL) exists; target-screen capture is external |
| **Wait** | named-constant sleeps + Verify | $0 | bash |
| **Verify** | capture + Go pixel-diff; `vision_analyze` on ambiguity | $0 | vision exists; diff is new pure-Go code |

That is **~$15 of commodity hardware** to make any machine — including one with no OS agent, no network,
even a BIOS screen — drivable by gopi. Independent corroboration that this is a real, working recipe:
[`sunasaji/serial-hid-kvm`](https://github.com/sunasaji/serial-hid-kvm) (CH9329 + HDMI capture, ~$15,
JSON-lines API "for AI automation", an `mcp-serial-hid-kvm` MCP server, and a Python client with
auto-reconnect for agent integration). Someone else has already shipped §11 with the same parts and the
same stated purpose — which validates the hardware bill of materials and, equally usefully, gives us a
second implementation of the protocol to test our frames against.

Loop closure check (the 4-primitive table in `devdoc/computer-use-second-blue-ocean-4-primitives-20260910.md:61-64`): **Capture ✓ (shot/capture card)
· Actuate (this design) · Wait ✓ · Verify (diff)** — the primitive set is completable without new
top-level tools, which is exactly the "add the *right few*, not more" conclusion that devdoc reached.

---

## 12. Deliberately NOT designed (keep the primitive small)

- **The `Capture` tool/verb for arbitrary screens** — `shot` covers URLs; a capture-card path is a
  separate task with its own dependency question (v4l2 vs `ffmpeg`).
- **Cursor detection by image search** — needed for fully automatic calibration; v1 calibration can use
  known UI landmarks instead.
- **A `Wait` tool** — bash sleep exists; a tool here would earn its name only with measurement.
- **Custom-HID mode / raw HID passthrough** — not needed for keyboard+mouse.
- **Target-side agent or clipboard bridge** — that would abandon the entire non-invasive premise.
- **Multi-target multiplexing / remote transport** — a `ssh -L`-style tunnel or a TCP protocol
  (`serial-hid-kvm` chose the latter) is a later product decision, not a primitive decision.
- **Any implementation.** Out of scope by the task statement.

---

## 13. Open questions (need hardware; do not guess)

1. **ACK frames** — exact status bytes/semantics for `0x82/0x84/0x85`, and whether they are emitted for
   every command. §7.3 depends on this.
2. **`0x01` semantics** — `hid_protocol.py` says `CMD_GET_INFO`, `mode_fix.py:8` says `CMD_SET_CONFIG`.
   Both cannot be right; the datasheet settles it. (§4.2)
3. **The absolute-resolution config field** — which command writes it, whether it persists to EEPROM,
   and what the factory default is. §6.1/§6.2 depends on this.
4. **Latched state on serial-side death** (§8 row 2) — confirm the chip does hold the last HID report
   with the USB link alive. This determines whether the USB relay is mandatory or merely prudent.
5. **Achievable typing rate** per target app/IME without drops (§9.3) — a measurement, not a guess.
6. **Post-config VID/PID and interfaces** — does the mode change alter enumeration? (The recorded dump
   is post-change; a pre-change dump was not captured.)
7. **Monitor-union and scaling mapping** (§6.1/§6.2) — measure on a 2-monitor and a scaled target.
8. **`x/sys` direct-require policy** — confirm promotable given the offline module-cache constraint.

---

## 14. Acceptance state of this report

### Completed
- Protocol reference (§4) with frame/command tables and a cited, independently-fetched implementation
  as corroboration; POC coverage matrix (§2.1).
- All four hard problems answered with evidence-class labels: absolute-mouse accuracy/calibration/Verify
  (§6), arbitrary-Unicode entry (§7), key-release safety on every failure path (§8), timing/flakiness (§9).
- POC bug list verified by checksum recomputation (§5): `0x03` mislabelled as a mouse command in 3 files;
  `mode_fix.py`'s "reset" frame invalid (wrong command *and* wrong checksum); `serial_test.py`'s frame
  malformed (shifted fields + wrong checksum).
- Repo-grounded zero-dependency check (§10.2) quoting actual files, and the Rule-3 resolution (§10.1).
- Go-side design (§10): package layout, CLI verb + sub-actions, `settings.json` shape, safety interlock.
- Minimum complete Capture/Actuate/Wait/Verify loop from off-the-shelf parts (§11) + deliberately-not-built
  list (§12) + open questions needing hardware (§13).

### Not Done (honestly)
- **No implementation** — out of scope by the task statement.
- **No hardware measurement of any kind** — the device is not attached (§0); every hardware claim is
  tagged, and §13 lists exactly which claims need a bench to settle.
- **Primary WCH docs not read this session** (§14c) — CSDN returned HTTP 521, zhihu HTTP 403, `wch.cn` is
  JS-rendered and returned no body. [SPEC] claims are carried from the prior session and corroborated by
  a third-party implementation; they must be re-confirmed against the datasheet (§13.2/§13.3).
- `mode_fix.py`'s "the mode switch worked" report was **not** re-tested — it cannot be, without hardware;
  §5.2 explains why the frame it sent could not have performed a reset.

### Verification

Three buckets, because a design report's claims are not uniform:

**(a) Checked mechanically this session ✓** — re-run and confirmed, not asserted:

| Claim | How it was checked | Result |
|---|---|---|
| Frame checksum algorithm (§4.1) | recomputed in Python from the POC's own byte arrays | matches (§5.2/§5.3 work shown) |
| `mode_fix.py:49` reset frame is invalid (§5.2) | same recomputation | `0x104 & 0xFF = 0x04 ≠ 0x00` → invalid |
| `serial_test.py:16` frame is malformed (§5.3) | same recomputation | `0x117 & 0xFF = 0x17 ≠ 0x00` → invalid |
| `0x03` is used as a mouse command in 3 files (§5.1) | read the defining and frame-building lines | `mouse_test.py:10,35`, `simple_move.py:10,35`, `move_test.py:13` |
| POC size / duplication (§2) | `find`+`wc`, `grep -rl` | 24 files, 2079 lines; `calc_checksum` in 14 files |
| Sleep count (§5.4/D3, §9) | `grep -c time.sleep` | **158 call sites in 22 of 24 files**, 12 distinct values 0.01–5 s |
| Keyboard tables are 21 / 17 entries (§2.1) | counted from source | `units/keyboard_type.py` 21; `fixed.py` 17 (`U`+0-9+`A-F`) |
| No ACK is ever read (§4.3, §5.4/D1) | `grep -rn '\.read('` over all 24 files | **zero** occurrences |
| gopi still has exactly 16 tools (§10.1) | `GOPI.md:87`, `loop.go` `claudeToolDefs`, `toolnames/names.go`, `gopidoc_sync_test.go:54` | `names.go` exports 26 name constants = the 16 tool names + 10 action values; drift test present |
| No serial library is vendored (§10.2) | `go.mod` + repo-wide grep for `tarm/serial`, `go.bug.st/serial`, `termios` | none; deps are `modernc.org/sqlite` + `golang.org/x/sys v0.37.0 // indirect` |
| `x/sys/unix` has the termios symbols (§10.2) | module cache `golang.org/x/sys@v0.37.0` | `Termios`, `IoctlSetTermios`, `TCSETS`, `B115200` present → zero new deps |
| Device-tree facts (§3) | read from the POC's captured `devices` file | `1a86:e129`, "WCH UART TO KB-MS_V1.8", 4 interfaces, mouse `REL=903 ABS=3 KEY=1f0000` |
| `settings.json` top-level keys (§10.4) | parsed the live file | 12 keys, as listed |
| Prototyping tools exist on this box (§6.3) | `ls /usr/bin/compare`, `import PIL` | ImageMagick `compare`/`convert` present; PIL 12.2.0 |
| POC untouched (§0) | `find /home/user/work/ch9329 -newermt …`, `stat` | newest file 21:12:25, *before* this session's edits — nothing modified |

**(b) Not checkable from this box — needs hardware ✗** (§13): every ACK semantic, the absolute-resolution
config field and its persistence, achievable typing rate, latched-state-on-serial-death, post-config
enumeration. These are *labelled* as such throughout and never presented as measured.

**(c) Not independently re-verified ✗**: the [SPEC] command table. The primary WCH docs were unreachable
(CSDN HTTP 521, zhihu HTTP 403, `wch.cn` JS-rendered). What stands behind [SPEC] is (i) the prior
session's reading and (ii) one fetched independent implementation — `sunasaji/serial-hid-kvm`'s
`hid_protocol.py` (162 lines, MIT), which agrees on the frame, the checksum and the command families.
That is corroboration, **not** proof: §13.2/§13.3 put the two specific disagreements (what `0x01` is,
which command carries the absolute resolution) in front of the datasheet before any implementation.

- Build: no code changed — `go build`/`go test` are genuinely n/a for a design-only report, and are
  *not* being quietly skipped: the worktree has exactly one changed file, the report itself.
- Commits on `feat/ch9329-hid-evaluation` (3, in order): `5b2d977` design-only report → `d0f5b53`
  Rule-5 acceptance sections → final commit *"tighten cross-refs, correct sleep/size counts, add a
  three-bucket verification section"*. The tip hash is intentionally not written down here — a hash
  inside the file it names cannot be self-consistent (amending changes it); read it with
  `git log --oneline -1 feat/ch9329-hid-evaluation`.

### Deliverable declaration (Rule 6d)
- **Branch**: `feat/ch9329-hid-evaluation` (worktree `/home/user/workspace/gopi-ch9329`) — the commit is
  already shared with the main repo at `/home/user/work/GoPi` (worktrees share refs, no push needed);
  verified `git log --oneline -1 feat/ch9329-hid-evaluation` in the main repo returns the **same tip**
  as this worktree (worktrees share refs; the branch is not checked out in the main working tree, so
  no push and no `denyCurrentBranch` refusal is involved).
- **Recovery command**:
  `git show feat/ch9329-hid-evaluation:devdoc/ch9329-hid-actuator-evaluation.md`
- Not merged (merging is the PA's step, after `scripts/safe-merge.sh`).
