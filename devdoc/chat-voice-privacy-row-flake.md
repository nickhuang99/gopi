<!-- doc-class: historical -->
# The chat voice privacy row: a flaky grid wait that was reading a scrolling window

**Task**: `01a12cd9-a9f0-7792-b660-2113928a3349` · **Branch**:
`task/01a12cd9a9f07792b6602113928a3349` · **Host**: hosta

## What the case was doing wrong

`TestChatVoicePrivacyRowCarriesOnePrefix` (`cmd/agent/voice_module_defects_test.go`)
judges that the chat cloud-engine privacy row carries ONE `[voice] ` prefix —
the defect was a call site re-typing the prefix `voiceDoubaoPrivacyNotice`
already owns (`Rule 13`).

It was **flaky, and badly**: measured **12 of 20 runs red** on 2026-10-11 with

```
--- FAIL: TestChatVoicePrivacyRowCarriesOnePrefix (3.20s)
    voice_module_defects_test.go:63: timed out waiting for the privacy row on the grid
```

The briefing called it "抖动用例，超时约 3.20s" and asked whether the timeout was a
hard wall-clock assumption or a dump/repaint race. **The answer is neither of the
two offered:** the row arrives in ~2 ms, and the case loses it because it reads a
**scrolling window** and calls its height a property of the notice.

## Root cause (measured, not inferred)

The failing grid is the whole diagnosis: it carries `VOICE MODE ON` and **three**
silence rows, and `PRIVACY` is **absent** (`count([voice] )=3`), while the case's
own bound is 3 s.

Two mechanisms compound, and both were measured with a throwaway probe
(the probe's own timeline is quoted below; it was deleted before commit):

1. **The scripted recorder computes a window in microseconds; production waits
   `voiceChatWindowSeconds` (6 s).** `scriptedRecorder.Round` returns `("", nil)`
   when its text script is spent — i.e. the moment it is called. So the mode's
   capture loop answers a silent window the instant it is called, and the only
   clock between two silent windows is the pacing gap itself
   (`voiceChatWindowPauseSeconds`, 1 s). Each silent window writes its own row
   (`chatVoiceSilentNote`), and the rows land in the answer band.

   **CORRECTED 2026-10-12 (row `01a12d65`).** This paragraph first read *"the
   mode's own loop cut TEN silent windows in 3.2 s"* — a number this host cannot
   produce and which no measurement in this document ever showed. Re-measured on
   **this very tree** (`0b3c1de29`, the tree the claim was taken from), 4 runs,
   with the recorder's own `Round` stamped: the loop answered windows at
   `t = 0/1001/2003 ms`, `0/1002/2003`, `0/1000/2001`, `0/1002/2004` — **THREE
   silent windows inside the case's 3 s bound (`voicePtyWait`; a red run ends at
   ~3.58 s), one second apart.** The number was wrong on its own tree; the
   mechanism sentence in front of it ("the recorder computes a window in
   microseconds") is right and is what the fix's pacing claim rests on.

2. **The privacy row is an eight-row paragraph on a 14-row pty, and the notice is
   the FIRST row in the band** — so it is the first thing the band scrolls off.
   Measured: with only `voice=1` (no silence rows, no burst), the row's wrapped
   paragraph plus the entry note already filled **all 14 rows**.

**ON THE PRESENCE OF THE ROW, THE TWO INSTRUMENTS AGREE; ON ITS STAY, NEITHER
CAN NAME A DEADLINE.** The table below is the FILING run's probe (a pre-fix
`waitFor` poll instrumented with a `2 ms` step, so its samples are ~2 ms apart and
`t=3.756 ms` is a POLL reading — the row's departure happened somewhere in the
preceding 2 ms, not at that instant). This row's own re-measurement, sampling the
painted grid every 200 µs for the case's whole bound (`probe-c-announce.txt`,
3 runs on `0b3c1de29`), saw the phrase on the grid for ~2-3 ms in 2 of 3 runs:

```
[ANNOUNCE] t_us=68 privacy=true                                  (3 runs: 55, 68, 55 µs)
[WATCH] t_ms=0 privacy=false rows=14 sample=1
[WATCH] t_ms=1 privacy=true  rows=14 sample=3     ← on the grid here
[WATCH] t_ms=3 privacy=false rows=14 sample=4     ← and gone here (run 2)
```

and in the third run it was **never observed on the grid at all** (`privacy_seen=false`,
`0 of 2380 samples`), although the announcement fired at `t=55 µs`. So "announced
within ~0.1 ms" is reproducible; "on the grid at 2.4 ms / gone by 3.2 ms" is a
**race whose outcome the runs disagree on**, and the fixing task's own summary
("painted at t=2.4 ms and gone by t=3.2 ms") is one run of that race, not a
timeline the case can be described by.

The filing run's instrument (pre-fix `waitFor`, 2 ms poll step):

```
t=  2.420ms voice=1 privacy=true  sil=0 rows=PRIV,?s://openspee,PRIV,?machine (cho,?g, env or se,?--engine loc,env,env,enter,enter,enter,prompt
t=  3.756ms voice=1 privacy=false sil=1 rows=PRIV,?machine (cho,?g, env or se,?--engine loc,env,env,enter,enter,enter,sil,sil,prompt
...
(steady state to the end of the window: privacy=false, sil,sil)
```

and the verbatim row at the instant it was painted (~1.6 ms after `enter`), when
the grid still had room for it:

```
=== AT PAINT (t=1.590817ms) ===
    [voice] PRIVACY — engine recording UPLOADS the audio to http
    s://openspeech.bytedance.com/api/v3/auc/bigmodel/submit (X-A
    pi-Resource-Id volc.seedasr.auc); the recording leaves this
    machine (chosen explicitly on one of the three channels (fla
    g, env or settings)). to transcribe ON this machine instead:
     --engine local, GOPI_AUDIO_ENGINE=local, or "audio_engine":
     "local" in /dev/shm/suite-cmd-agent-run-936441493/suite-env
    /settings.json
    [gopi] VOICE MODE ON — test mic; transcribing with the recor
    ...
```

**The row IS painted** — the announcement is reproducible at `t=55-68 µs` — and its
text leaves the 14-row grid within a few milliseconds (observed at `t=3-4 ms` in 2
of 3 runs, never observed at all in the third). The case's `waitFor(… "PRIVACY")`
was *correct about the paint* and *wrong about the screen*: a band row that is
announced, drawn, and then scrolled off is indistinguishable, from a grid read, of
a row that was never written.

Why this is a **fixture** defect and not a product defect: production cannot
produce a silence row one second apart inside a 3 s window (each costs a real 6 s
window), and no product
behaviour is asserted wrongly here — the prefix judgement itself is sound. The
case was racing its own fixture.

## The two halves of the fix

Both halves are about not racing the fixture; **no product behaviour was changed**
to make the case pass.

### (1) Event-driven wait on the assertion's subject

The console already owns the right record. `chatInput.WriteNotice` tees every
notice it announces to `in.announce` — a seam the layer documents as *"the ONE
place a notice is counted as an event; nothing in the editor's repaint path
reaches it"* and which changes **no byte** of the console (the row is still
drawn). The harness now installs its own `noticeLog` there, and the case waits on
the **announcement**:

```go
h.waitAnnounced(t, "the privacy row to be announced", rowOpening)
```

The subject of this wait is *"did the console say this sentence?"* — a fact that
cannot be un-said by a later repaint. The **prefix judgement still reads the
painted grid** (unchanged: `strings.Count(rows, "[voice] ")` must be exactly 1 and
a doubled row is still fatal), because that judgement is about how the row was
*composed*, and the cells the operator sees are what show a doubled prefix. The
two readings are complementary: the grid says the operator can see it, the
announcement says the console said it whatever the screen did afterwards.

**A trap worth recording**: the notice's full text embeds **this run's own
settings path** (its back-to-local hint ends `"audio_engine": "local" in
/dev/shm/suite-cmd-agent-run-…/settings.json`), so waiting for the whole string
waits for a temporary directory's name — it failed 6/6 as a *deterministic* red
before being narrowed. The subject is derived from the production sentence
(`strings.Join(strings.Fields(notice)[:2], " ")`), never re-typed (Rule 13).

### (2) A recorder-published round gate

The wall-clock assumption that WAS present is the fixture's: "the rounds keep
coming while the case is still reading the screen the last one produced". The fix
is a new **optional** half of the recorder seam
(`voiceRoundGate`, no production recorder implements it — the same pattern as the
existing `voiceWindowReporter` / `voiceChatFinisher`), so *the pacing belongs to
the thing that has a clock*:

```go
// cmd/agent/chat_voice.go:1479 — the loop's one obligation
if g := voiceRoundGateOf(m.recorder); g != nil {
    select {
    case <-stop:
        return
    case <-g:
    }
}
```

The loop watches `stop` on the **same** select, so a mode left on a parked gate
still leaves at once rather than waiting for a round that will never be released,
and the generation is re-checked after the wait like every other await in the
loop. `scriptedRecorder` publishes one release per round
(`RoundGate`/`armRoundGate`/`gateAsksRun`), so the case reads the screen a step
left, then releases the next step.

**The parked state is WAITED ON, not slept at.** `RoundGate` counts the ask the
mode makes immediately before it waits (`gateAsks`), and the case waits on that
counter — so a mode that stopped consulting the seam is *caught*, rather than
silently raced.

The case also runs on a taller terminal for this judgement
(`newVoicePtyGridTall`, 24 rows; the shared 14-row default stays for the layout
cases). Taller is not a louder assertion — it is the same assertion with the
screen no longer deciding it.

## Red proofs (Rule 10.5) — both sides of the fix

| red proof | change | result |
|---|---|---|
| **1. the assertion still catches the defect** | restored the doubled prefix `m.notice("[voice] %s\n", …)` in `chat_voice.go` | **3/3 FAIL** — `the privacy row is prefixed TWICE — the call site re-typed the prefix the notice already carries (Rule 13)` (in 0.21 s, i.e. not a timeout) |
| **2. the gate seam is load-bearing** | deleted the gate consult from the loop (the mode stops asking) | **3/3 FAIL** — `the capture loop never asked the recorder for a gate … (rounds run: 3)` |

Both were reverted; green was re-confirmed 3/3 afterwards.

## Before / after

| | before | after |
|---|---|---|
| judgement of "did the console say it?" | `waitFor(grid contains "PRIVACY")` — a scrolling band row | `waitAnnounced` on the layer's own announcement tee |
| round pacing | fixture's microsecond round vs the case's 3 s bound (3 rounds / 3.2 s, REMEASURED — this cell said "10 rounds / 3.2 s" until 2026-10-12) | each round released by the case; the screen is still when read |
| terminal | 14 rows (an 8-row paragraph + entry note already fills it) | 24 rows for a notice judgement; 14 kept for layout cases |
| failure rate (20 runs) | **12 red** | 0 red (verified below) |
| failure mode | `timed out waiting for the privacy row on the grid` (3.20 s) | defect-shaped failures only (proven by red proof 1) |
| assertion strength | count on grid == 1, doubled row fatal | **unchanged**, plus "the announced row is on the grid" |

## Re-measurement (2026-10-12, row `01a12d65`) — the two numbers, re-taken on the tree they were written from

Two sentences in this document (and in the source comments of the fix) did not
reproduce on this host. They were re-taken here rather than argued about; the fix's
**conclusion** is untouched and was independently confirmed by the filing run
(one-variable 14→24 row mirror turns the absence into a prefix-count failure) as
well as by `git merge-base --is-ancestor 1b63fb2d3 0b3c1de29` → **NO** (the claimed
baseline is genuinely pre-fix) and by master's own 84-run green batch.

**Tree, and why it is the right one.** `0b3c1de29` — the pre-fix tip this document
names — materialised read-only with `git archive` under `/dev/shm` (tmpfs, a
different device from `/`: `stat -c %d` → `/dev/shm`=27 vs `/`=66308, Rule 22).

**Instrument 1 — the round cadence** (`scriptedRecorder.Round` stamped with the
case's clock, `probe-b-repro.txt`, 4 runs):

```
[ROUND] n=1 t_ms=0
[ROUND] n=2 t_ms=1001
[ROUND] n=3 t_ms=2003        (runs 2-4: 0/1002/2003, 0/1000/2001, 0/1002/2004)
```

→ **THREE rounds inside the case's bound, one second apart.** One second is
`voiceChatWindowPauseSeconds`, the gap the loop pays after a window that produced
no usable text — a silent round is exactly that, so the scripted recorder is paced
by that constant and not by its own microseconds.

**Instrument 2 — the grid, and the announcement** (`probe-c-announce.txt`, 3 runs,
200 µs sampling of the painted grid for the whole bound, plus the `WriteNotice`
tee):

```
[ANNOUNCE] t_us=68 privacy=true
[WATCH] t_ms=0 privacy=false rows=14 sample=1
[WATCH] t_ms=1 privacy=true  rows=14 sample=3
[WATCH] t_ms=3 privacy=false rows=14 sample=4
```

→ the notice is **announced at 55-68 µs in every run**, and its text was on the
grid for ~2-3 ms in 2 of 3 runs — **and never observed in the third** (0 of 2380
samples, while its announcement still fired at 55 µs). All 3 runs ended red with
`timed out waiting for the privacy row on the grid`.

**What was changed on the strength of this, and what was not.** The rate claim
(`TEN windows / 3.2 s`) was replaced by the measured cadence in all three places it
appeared; the timeline claim was replaced by the announcement latency that IS
reproducible, with the grid stay stated as a raced interval. The fix's own
conclusion — *the case read a scrolling 14-row band, not a product defect* — is
cited, unchanged, in every one of those places.

**Reproduction (verbatim; scripts and raw output live in this row's session dir).**

```bash
SD=/home/user/.gopi/sessions/01a12d65-4ece-77b6-89bb-4a299b678102
bash $SD/probe-b-repro.sh     # cadence:  [ROUND] n=1 t_ms=0 / n=2 t_ms=1001 / n=3 t_ms=2003
bash $SD/probe-c-announce.sh  # grid+announce: [ANNOUNCE] t_us=55..68 ; [WATCH] t_ms=1 privacy=true / t_ms=3 privacy=false
```

Both scripts materialise `0b3c1de29` with `git archive`, instrument only
observability (a timestamp in `Round`, a 200 µs sampler, a print in `WriteNotice`),
run the case unchanged with `go test ./cmd/agent/ -run
'^TestChatVoicePrivacyRowCarriesOnePrefix$' -count=1`, and leave their raw output in
`$SD/probe-b-repro.txt` / `$SD/probe-c-announce.txt`.

## Not done (honest)

- **The scripted recorder is still un-paced by wall clock**, deliberately: it is
  the *reason* the gate exists. Its rounds are still instantaneous when nothing is
  armed, so **any other case built on it can still produce a burst** — the gate is
  opt-in per case (`armRoundGate`). No other case in `cmd/agent` was audited or
  changed by this task.
- The **band scroll behaviour itself** (a notice pushed off a short screen by the
  rows that follow it) is a property of the reserved layout, is asserted by its own
  cases at a fixed height, and was **not** touched. This task judges it in no way.
- The package run was executed while **two other `scripts/test-budget.sh ./cmd/agent`
  instances were live on this host** (measured: pids 1144425, 1151661). Memory
  #8537 records that concurrent same-package suite runs on this machine pollute each
  other's `-tested-bin` mtime guard and global session directory assertions — so a
  red result for an *unrelated* case in that run is attributable to the ambient
  condition, and the per-case `-run` evidence above is the arm that this task
  controls.
- The flake rate of the **whole-package** path was measured only through the
  package run recorded in `## Verification`; a 20× loop of `./cmd/agent/` was not
  run (time budget) and is not claimed.

## Evidence (self-filed)

- **Raw red run (verbatim, this task's own observation)**: 12/20 red with the
  binary built from the unmodified tree —
  `for i in $(seq 1 20); do ./gopi-ct-privacy.test -test.run TestChatVoicePrivacyRowCarriesOnePrefix -test.count=1 …
  done` → `PASS=8 FAIL=12`; each red emitted `voice_module_defects_test.go:63: timed
  out waiting for the privacy row on the grid` with a grid carrying three silence
  rows and no `PRIVACY`. Two raw logs are preserved at
  `~/.gopi/sessions/01a12cd9-a9f0-7792-b660-2113928a3349/artifacts/red-run-3.raw.txt`
  and `…/red-run-8.raw.txt`.
- **Probe timeline** (the displacement, quoted above) was produced by a throwaway
  `cmd/agent/zz_privacy_probe_test.go`, deleted before commit; the probe source is
  preserved at `…/artifacts/probe-test.go.txt`.
- **Acceptance loop**: the literal command repeated 6× → `PASS=6 FAIL=0`.
- **Red proofs 1 and 2**: 3/3 FAIL each, with the quoted sentences, reverted
  afterwards (green re-confirmed 3/3).
