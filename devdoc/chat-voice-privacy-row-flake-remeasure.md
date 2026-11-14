<!-- doc-class: historical -->
# TestChatVoicePrivacyRowCarriesOnePrefix — re-measured on the current master

**Task**: `01a12d20-d091-756f-92c3-477267ab1c41` · **Branch**:
`task/01a12d20d091756f92c3477267ab1c41` · **Host**: hosta · **Measured**:
2026-10-12, master `6f7acc1e1`

## What this document is

The flake was reported **6/16 red on master tip `0b3c1de29`**. The memory record
(#8624) and `devdoc/chat-voice-privacy-row-flake.md` say a fix exists
(`1b63fb2d3`). This document is the **re-measurement**: is the flake live on
master today, what actually causes it, and is anything left to change?

**Verdict: the flake is NOT live on current master (84 consecutive green,
measured), the report measured a PRE-FIX baseline (ancestry proven below), the
fix in master holds, and NO code change is required.** What this document adds is
an independently measured root cause, and two measured corrections to the
mechanism the fix's own narrative states.

## 1. The report measured a PRE-FIX baseline (ancestry, not assumption)

Two different gopi instances run side by side here, so "master" is not one
thing — the fix must be shown to be *in* the tree that was measured green, and
*absent from* the tree that was measured red:

```
master tip: 6f7acc1e1c4aaea6dfbc26a410a901710cc41410 2026-10-12 06:45:19 +0800
fix:        1b63fb2d3a0c7b0bf69aa7d0d04948bf0badd68c 2026-10-12 06:31:14 +0800
report tip: 0b3c1de292eab16e59ee4d201784ff298fef76aa 2026-10-12 06:16:34 +0800
1b63fb2d3 IS ancestor of master
1b63fb2d3 NOT ancestor of 0b3c1de29 (report tip is PRE-fix)
```

So `0b3c1de29` — the tip the 6/16 report was taken at — predates the fix by 15
minutes and does not contain it. The report was a correct measurement of a
**pre-fix** tree.

## 2. Batch A — current master, the acceptance measurement

Every measurement tree below is a read-only `git archive` materialisation under
`/dev/shm` (tmpfs; `device of /dev/shm: 27`, `device of /: 66308` — off the `/`
partition, Rule 22/29). The package's own test entry point, scoped to the one
case:

```
go test ./cmd/agent/ -run '^TestChatVoicePrivacyRowCarriesOnePrefix$' -count=1
```

| batch | rev | N | GREEN | RED |
|---|---|---|---|---|
| A  (`current-N24.txt`) | `6f7acc1e1` | 24 | 24 | 0 |
| A2 (`current2-N40.txt`) | `6f7acc1e1` | 40 | 40 | 0 |
| A3 (`current3-runs/`) | `6f7acc1e1` | 20 | 20 | 0 |

**84 consecutive green, 0 red**, ~0.7-2.4 s per run. Acceptance asked for N≥20;
this is 4× that. Verbatim tails:

```
current run 23: GREEN
current run 24: GREEN
=== current TestChatVoicePrivacyRowCarriesOnePrefix N=24: GREEN=24 RED=0 ===
current2 run 39: GREEN
current2 run 40: GREEN
=== current2 rev 6f7acc1e1c4aaea6dfbc26a410a901710cc41410 N=40: GREEN=40 RED=0 ===
```

## 3. Batch B — the pre-fix tip, red preserved verbatim

Same command, same host, tree materialised from `0b3c1de29`:

```
prefix run 1: GREEN
prefix run 2: GREEN
prefix run 3: GREEN
prefix run 4: GREEN
prefix run 5: RED
--- FAIL: TestChatVoicePrivacyRowCarriesOnePrefix (3.21s)
    voice_module_defects_test.go:63: timed out waiting for the privacy row on the grid
        --- grid ---
         [gopi] VOICE MODE ON — test mic; transcribing with the recor
         ding engine every 6s; each transcript is APPENDED at the car
         et (`status` or Esc leaves, the line is kept).
         [voice] no text in the last 6s window (silence, or the speec
         h fell outside it) — still listening.
         [voice] no text in the last 6s window (silence, or the speec
         h fell outside it) — still listening.
         [voice] no text in the last 6s window (silence, or the speec
         h fell outside it) — still listening.
         you>
FAIL
=== prefix TestChatVoicePrivacyRowCarriesOnePrefix N=8: GREEN=4 RED=4 ===
```

A second batch on the same pre-fix tree (probe v7, N=10, below) gave
**GREEN=1 RED=9**. The 14-row grid dump is the same in every red: `VOICE MODE ON`
+ silence rows, **no** `PRIVACY`. The flake reproduces here as **50-90 % red**,
not as a rare event — which is why it was caught by a 16-run sweep.

## 4. Root cause, measured

### 4.1 What the case reads, and why that is the defect

The pre-fix wait is a text search over the painted 14-row grid:

```go
h.waitFor(t, "the privacy row on the grid", func(rows string) bool {
    return strings.Contains(rows, "PRIVACY")
})
```

The privacy row is an **eight-wrapped-row paragraph and it is the FIRST row of
the answer band**. Direct measurement of what that grid ever contained, sampling
`h.rows()` every 200 µs for 300 ms right after `h.enter(t)` (probe v6, state
changes printed):

```
=== PRE-FIX (0b3c1de29) ===
[FINE] case_us=135  prefixes=0 privacy=false rounds=0
[ROUND] n=1 case_us=161
[FINE] case_us=1413 prefixes=1 privacy=false rounds=1     <- the silence row's paint
[ROUND] n=2 case_us=1001493
[FINE] case_us=1002780 prefixes=2 privacy=false rounds=2
--- FAIL: TestChatVoicePrivacyRowCarriesOnePrefix (4.90s)
=== MASTER ===
ok  	github.com/nick/gopi/cmd/agent	2.388s
```

and the same instrument over the pre-fix case's own 3.2 s poll loop, N=10 (probe
v7) — **`PRIVACY` was never once visible**, across 1261-1339 polls per run:

```
v7 run 4: RED   | [ORDER] seen=false polls=1323 first_t_ms=0 row_index=-1 rounds_then=-1
v7 run 5: GREEN |                       <- the one green: the paint won the race
=== v7 result: GREEN=1 RED=9 ===
```

**Mechanism (measured, and correctly stated as two parts):**

1. the judgement is taken from a **scrolling window** — the band's height decides
   whether the notice is on screen when the case looks;
2. the notice's paint **races the band's first row** (the entry's own note and the
   first silence row land in the same band inside the first milliseconds), and at
   14 rows the notice phrase is present in **0 of ~1300 samples** in 9 of 10 runs,
   visible in the 1 run that passes.

The mirror experiment makes the capacity half unambiguous — pre-fix tree, **one
variable changed** (pty height 14 → 24), assertion untouched:

```
=== v4 runs (N=10, pre-fix tree, taller pty only) ===
=== v4 result: GREEN=0 RED=10 (baseline at 14 rows was GREEN=4 RED=4) ===
--- FAIL: TestChatVoicePrivacyRowCarriesOnePrefix (0.21s)
    voice_module_defects_test.go:80: the grid shows 2 `[voice] ` prefixes, want exactly 1
        [voice] PRIVACY — engine recording UPLOADS the audio to http ...
        [gopi] VOICE MODE ON — test mic; ...
        [voice] no text in the last 6s window ...
```

At 24 rows the row **is** painted (and fails on the count, because the band still
holds a silence row's own `[voice] `) — so at 14 rows the row is not "never
announced"; it is **lost to the screen**. That is the pre-fix defect, and it is a
**fixture/judgement** defect: production cannot put two rows in the band inside
one screenful, and the assertion (the prefix count) was never the thing that
failed.

### 4.2 Why neither half of the master fix can be removed

The master fix has two halves — the recorder's round gate (`voiceRoundGate`,
`chat_voice.go`) and the announcement tee the wait now reads
(`noticeLog`/`waitAnnounced`). Each was deleted **alone** on the master tree:

| variant | change | result |
|---|---|---|
| A (probe `gauge-*.log`) | loop stops consulting the gate (`voiceRoundGateOf` returns nil) | **RED 3/3** — `the capture loop never asked the recorder for a gate` (rounds run: 3) |
| A2 (probe `probe-halves8.txt`) | gate consult disabled **and** that guard removed — announcement half only | **RED 7/8** — `the grid shows 0 [voice] prefixes` (×4), `no round gate has been handed to the mode` (×3), 1 green |

So the gate is not decorative: it is the half that makes the grid read
**deterministic**, and the announcement is the half that makes the *wait* immune
to band height. The case's own new guard fires when the loop stops honouring the
gate, which is exactly the Rule 15 property the fix's comment claims.

### 4.3 Two corrections to the fix's stated narrative (measured here)

The fix's comment and `devdoc/chat-voice-privacy-row-flake.md` state "the loop cut
**TEN silent windows in 3.2 seconds**" and the row "painted at t=2.4 ms, gone by
t=3.2 ms". On this host neither is what the instrument shows (probe v3, recorder
rounds stamped, pre-fix tree):

```
[ROUND] n=1 t_ms=566
[ROUND] n=2 t_ms=1568
[ROUND] n=3 t_ms=2571
```

**3-5 rounds inside the window, ~1 s apart** — the cadence is the mode's own
`voiceChatWindowPauseSeconds = 1` (the silence pause after an empty round), not a
microsecond burst; and the notice is **never** observed on the 14-row grid, not
"observed at 2.4 ms then scrolled away". The conclusion the fix reached (a
scrolling window, not a product defect) is **independently confirmed** by §4.1-4.2;
the *rate* of the mechanism and the "painted then gone" timeline are not
reproducible here and should not be quoted as this host's measurement. Filed
separately as row `01a12d65-4ece-77b6-89bb-4a299b678102` (tag `bug`, Rule 41) with
the measurement attached, rather than silently corrected, since the text sits in
master comments and a normative devdoc.

## 5. Verdict and what was changed

* **Live on master?** No — 84/84 green.
* **Root cause?** The pre-fix case judged a **scrolling 14-row band** while the
  notice's paint raced the band's first row; a grid read cannot tell "scrolled
  away" from "never written". Not a product defect.
* **Code change required?** **None.** Per this epic's own criterion, when the fix
  already in master holds, the deliverable is the verified verdict + this report,
  not a redundant edit. No file under `cmd/` was touched on this branch.
* **Assertion weakened?** No. The case still counts `[voice] ` prefixes on the
  painted grid at exactly 1 and still fails on the doubled row — §4.2's variant A
  proves the guard still catches a loop that ignores the gate.

## Not Done (honest)

* The **exact order** at 14 rows (notice painted-then-scrolled vs painted after the
  band was already full) is **not** distinguishable from a terminal read — no
  sample of the pre-fix 14-row grid ever showed the phrase (0 of ~1300 samples in
  9/10 runs), and the 24-row mirror proves only that the row reaches the grid when
  there is room. Both readings are the same defect; I did not separate them.
* Batch A ran the single case (`-run`), never the whole `cmd/agent` package — as
  the epic scoped it. A whole-package run was not taken, so this verdict does not
  claim anything about the package's other cases.
* No run was taken on a peer host; every number here is hosta's.

## Verification

* **Build**: `go vet ./cmd/agent/` clean on master (no output) and
  `CGO_ENABLED=0 go build -o /dev/shm/gopi-remeasure-build ./cmd/agent/` → exit 0,
  65,500,322-byte binary. Each probe tree was vetted before its
  batch; the probe A/A2 trees vetted clean too. (Removal of that binary was
  refused with the tmpfs trees — see the disposal paragraph above, which reports
  what was observed rather than what was intended.)
* **Tests**: `go test ./cmd/agent/ -run '^TestChatVoicePrivacyRowCarriesOnePrefix$' -count=1`
  — Batch A N=24 → 24/24 PASS; N=40 → 40/40 PASS; N=20 → 20/20 PASS (84/0).
  Pre-fix `0b3c1de29` → N=8: 4 GREEN / 4 RED, N=10: 1 GREEN / 9 RED.
  Neighbouring cases, master, `go test ./cmd/agent/ -run 'TestChatVoice' -count=1`
  → `ok github.com/nick/gopi/cmd/agent 159.876s` (every `TestChatVoice*` case
  green, not only this one) — evidence `neighbour-cases.txt`.
* **Ran**: the exact loop above, per tree materialised with
  `git archive <rev> | tar -x -C /dev/shm/<dir>` and executed inside it; probes
  v3/v4/v6/v7 and A/A2 are the session-dir scripts named below.
* **Result**: master green 84/84; pre-fix red preserved verbatim (§3); the two
  half-removals red (§4.2); the 24-row mirror red on the count (§4.1).
* **Commit**: this document's commit on
  `task/01a12d20d091756f92c3477267ab1c41`.

## Evidence (self-filed)

Session dir `/home/user/.gopi/sessions/01a12d20-d091-756f-92c3-477267ab1c41/`:

| file | what it holds |
|---|---|
| `ancestry.txt` | the three revs, the two `merge-base --is-ancestor` verdicts, the device ids |
| `current-N24.txt` | Batch A, N=24, `GREEN=24 RED=0` |
| `current-runs/current-run-{1..24}.txt` | Batch A, per-run logs |
| `current2-N40.txt` | Batch A2, N=40, `GREEN=40 RED=0` |
| `current3-runs/run-{1..20}.log` | Batch A3, 20/20 green, per-run logs |
| `prefix-N8.txt` + `prefix-runs/` | Batch B, pre-fix, red text verbatim |
| `probe-prefix3.txt` | ROUND cadence (~1 s apart) — the burst claim correction |
| `probe-prefix4.txt` | the one-variable 14→24 pty mirror experiment |
| `probe-fine6.txt` | 200 µs paint sampling, both trees |
| `probe-order7.txt` | pre-fix ORDER/GREEN correlation, N=10 |
| `gauge-{1,2,3}.log` | probe A — master with the gate consult disabled |
| `probe-halves8.txt` | probe A2 — announcement half only |
| `probe-*.sh` | the probe scripts themselves (read-only `git archive` trees only) |

No file under `/home/user/work/GoPi` was written by this run (`git status
--porcelain` in that repo prints nothing after the run), and no measurement tree
was ever a checkout of it: every tree was a read-only `git archive`
materialisation under `/dev/shm`.

**Disposal, measured rather than asserted.** The removal attempt at close-out
(`rm -r /dev/shm/gopi-verify-current … gopi-remeasure-build`) was **refused by
this host's own guard** — `/dev/shm` is outside every declared root
(`worktree=/home/user/workspace`, `scratch=…/gopi-test-scratch`,
`fixture=…/_tmp`, `fixture=/tmp`), so `rm` reports *"target … is outside every
declared root … refusing"*. The guard was not worked around. The observed
consequence: at close-out the trees of this run were **still present** on tmpfs —
`gopi-verify-{current,current2,prefix}`, `gopi-probe-prefix{,-2,-3,-4,-5}`
(142 MB each, ~1.1 GB total), created 07:37–07:42 and listed by
`ls -ld --time-style=+%H:%M:%S /dev/shm/gopi-*`, plus the removed build binary
`/dev/shm/gopi-remeasure-build`. tmpfs is reclaimed on reboot and 26 GB of the
32 GB were free; a later run that owns a declared root over `/dev/shm` can remove
them. Every `/dev/shm/gopi-*` entry older than this session (05:58–06:23,
including `gopi-base-*`, `gopi-master-1266236`, `gopi-baseline-b7e5eddd6`) is
another run's and was left untouched.
