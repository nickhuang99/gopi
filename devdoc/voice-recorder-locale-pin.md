<!-- doc-class: historical -->
# The recorder's message locale is part of the contract

Task `01a141a0-1d5a-7e4d-872a-6bb5e13da9e0`. Measured on `hostb-Laptop`, 2026-10-12.

## The defect

`cmd/agent/cli_voice_client.go` reads its recorder child's **stderr** and classifies
it by English literals. Four of the signatures it matches are in that list, and
**two of them are glibc `strerror` messages**:

| constant | literal | errno |
|---|---|---|
| `voiceCaptureBrokenPipeMarker` | `Broken pipe` | EPIPE |
| `voiceErrBusyMarker` | `Device or resource busy` | EBUSY |

gettext translates both. The recorder child was spawned with **no `cmd.Env`**:

```go
cmd := exec.CommandContext(ctx, binary, args...)   // cmd.Env == nil → inherits this process's env
```

`cmd.Env == nil` means the child inherits the parent's environment, message
catalogues included. Under a translating locale the classification then breaks in
two distinct places:

- `voiceCaptureContextSensitiveError` stops recognising a read side **we closed
  ourselves** (`stopVoiceRecorder` closes the pipe to unwind a full recorder), so
  an ordinary window close is reclassified as "the recorder refused" — the wrong
  CAUSE reported for the right observation. This is a Rule 15 silent failure.
- `voiceRecorderFailure`'s busy hint — and `voiceMissingDeviceHint`'s "no such
  device" hint — are **lost on the refusal path**, i.e. exactly where the operator
  needs the advice.

## What is MEASURED, and what is not

Everything below is raw output from this host. Nothing here is inferred.

### (a) `strerror(EPIPE)` is translated — minimal sample

`/usr/share/locale-langpack/zh_CN/LC_MESSAGES/libc.mo` (425 catalogues in that
directory; note the **`locale-langpack`** path — `/usr/share/locale/zh_CN/LC_MESSAGES`
holds 110 files and **no `libc.mo`, `coreutils.mo`, `alsa-utils.mo` or
`pulseaudio.mo`**, so a search that stops at the first path concludes the evidence
is unobtainable. It is not.).

```c
/* strerror_probe.c — the smallest sample that answers the question */
#include <stdio.h>
#include <string.h>
#include <errno.h>
#include <locale.h>
int main(void){ setlocale(LC_ALL, ""); printf("EPIPE  =[%s]\n", strerror(EPIPE)); return 0; }
```

```
$ env LC_ALL=C               ./strerror_probe
EPIPE  =[Broken pipe]
$ env -u LC_ALL LANG=zh_CN.UTF-8 ./strerror_probe
EPIPE  =[断开的管道]
```

The catalogue confirms the source rather than the outcome:

```
$ msgunfmt /usr/share/locale-langpack/zh_CN/LC_MESSAGES/libc.mo | grep -B1 -A1 "Broken pipe"
msgid "Broken pipe"
msgstr "断开的管道"
```

### (b) The REAL recorder, translated, printing a SHIPPED marker

`arecord` is installed and the capture device exists (`card 1: ALC236 Analog`), but
the sound server holds it — `plughw:1,0` gives `Device or resource busy`, and that
is EBUSY, i.e. **a marker the shipped code matches**. Both of the two live paths
were therefore reproduced with the real binary:

```
$ timeout 6 arecord -D plughw:1,0 -f S16_LE -r 16000 -c 1 -d 1 /dev/null    # LC_ALL=C
arecord: main:831: audio open error: Device or resource busy

$ env -u LC_ALL LANG=zh_CN.UTF-8 timeout 6 arecord -D plughw:1,0 … -d 1 /dev/null
arecord: main:831: 音乐打开错误： 设备或资源忙
```

The marker `Device or resource busy` is present in the first and **absent** in the
second. Note that `音乐` is alsa-utils' own mistranslation of "audio" (it means
*music*), which is beside the point — the point is that the string a marker reads
is gone.

`-D pulse` **does** open (the sound server answers), so the true read-side shape was
reproduced too:

```
$ env LC_ALL=C arecord -D pulse -f S16_LE -r 16000 -c 1 2>err | head -c 3200 >/dev/null
Recording WAVE 'stdin' : Signed 16 bit Little Endian, Rate 16000 Hz, Mono
stdout: Broken pipe

$ env -u LC_ALL LANG=zh_CN.UTF-8 arecord -D pulse … 2>err | head -c 3200 >/dev/null
正在录音 WAVE 'stdin' : Signed 16 bit Little Endian, 频率16000Hz， Mono
stdout: 断开的管道
```

**`stdout: Broken pipe` is exactly a shipped marker, and `arecord` really prints
it, and it really is translated.** That is the defect measured end to end against
the product's own dependency, not against a stand-in.

**SUPERSEDED — an earlier revision of this document said `parecord`'s EPIPE text
was not measured here. It is measured, and it is the cleanest arm of the whole
result.** The earlier run had only produced `parecord`'s refusal
(`Failed to open audio file.` → `打开声音文件失败。`); re-running with the read end
closed reaches the write path:

```
$ env LC_ALL=C parecord --raw 2>parec-epipe-C.err | head -c 3200 >/dev/null
write() failed: Broken pipe

$ env -u LC_ALL LANG=zh_CN.UTF-8 parecord --raw 2>parec-epipe-zh.err | head -c 3200 >/dev/null
write() 失败：断开的管道
```

The shipped marker `Broken pipe` is **present** in the first and **absent** in the
second, and — unlike `arecord` — `parecord` reports this as a **`write() failed`
diagnostic with an exit status, not as a signal death**, so here the marker really
is the deciding branch of `voiceCaptureContextSensitiveError`. `parecord` is
gopi's Pulse backend (the device list is built from it), so this is measured
against the product's own recorder, on the exact path the code classifies.

### (c) A consequence worth stating, because it is NOT locale-fragile

`arecord` **dies of SIGPIPE** when its read side disappears (`os/exec` reports
`signal: broken pipe`), and `voiceCaptureContextSensitiveError` answers TRUE from
its `signal:` branch **before** it ever consults a marker. `yes` behaves the same
way. So for `arecord`, the read-side case is caught by the signal branch and the
translated marker is **not** what decides that verdict.

**`parecord` is the counter-example that keeps this from being a hypothetical**:
it does *not* die of `SIGPIPE` on the same condition — it prints
`write() failed: Broken pipe` and exits — so the signal branch never fires and the
marker loop is the only thing that can recognise the read side. For gopi's Pulse
backend the translated marker is therefore not a redundancy behind the signal
branch, it is the decision. The same holds for the EBUSY/missing-device family in
(b), and for any recorder that ignores `SIGPIPE`. The marker loop is nonetheless the branch that is *meant*
to recognise the read side, and "the branch that exists for this case matches
nothing" is the defect regardless of which branch happens to win the race today.

Not measured in this run, and therefore not claimed: whether the strings in
`voiceErrMissingMarkers` are translated by ALSA/Pulse (they are the same
`strerror` family, but no such condition was produced here). The pin covers them
either way, because it covers the child rather than the strings.

### (d) Scope: one spawn site was reported, TWO exist

The briefing named `cli_voice_client.go:720`. Grepping for sibling recorder spawns
— the first lesson of the earlier locale incident is to pin *every* child whose
output is read, not only the one in the report — found a second:

`cmd/agent/cli_voice_audio_source.go:202` (`voiceStagedRecorderSource.Open`) spawns
the same `voiceRecorderCommand` argv and sets **no `cmd.Env`**, so it carries the
identical unpinned shape. It is NOT fixed here, and the reason is a decision this
change is not entitled to make: **its `stderr` buffer has no reader anywhere**
(declared at 168, assigned at 208, referenced nowhere else — not in `Health()`,
`recordReadErr` or `Close()`). So the staged path either must read its recorder's
stderr — and then needs this same pin, at the same seam — or it should stop
capturing a pipe it never drains. Pinning a buffer nobody reads would look like a
fix without being one.

Filed as a bug row on its own evidence: task `01a141bf-dbac-7c81-be44-7875d5af0278`
(briefing at `.../artifacts/bug-staged-recorder-stderr.md`), with the measured
greps and the inferred half labelled as inference.

## The fix

One call site, one seam, no second list:

```go
cmd.Env = voiceRecorderEnv()          // cli_voice_client.go
func voiceRecorderEnv() []string { return tools.ChildLocaleEnv() }
```

`tools.ChildLocaleEnv()` is the **existing** helper in
`internal/tools/git_child_locale.go` (added by the git-child incident, epic
`01a1306b`, commit `b42a1f731`), now **exported** together with the three values
that describe the policy — `ChildLocaleLocalePinnedNames` (`LANG`, `LC_ALL`),
`ChildLocaleSelectorNames` (`LC_MESSAGES`, `LANGUAGE`) and `ChildLocaleEnglish`
(`"C"`). It was unexported while git was the only caller; the recorder is the
second, and an unexported name would have forced `cmd/agent` to **re-type** the
four selectors — a bare literal in disguise (Rule 14) that drifts the day a fifth
selector is added, with neither caller noticing.

**Strip-then-append is the load-bearing half.** A duplicate key in an exec
environment resolves to the **FIRST** entry, so

```go
append(os.Environ(), "LC_ALL=C")      // NOT a pin
```

leaves the inherited translation in force while looking like a pin. The helper
strips the four names out of the environment and *then* appends the pinned pair.
The red arm below measures exactly this trap.

## Why pin, rather than delete the assertion

Deleting (or widening) the marker would be cheaper and would be wrong: the four
signatures are how the code distinguishes *"I closed this recorder's window"* from
*"this recorder refused to work"*, and that distinction is user-visible — it is the
difference between a normal end-of-window and a reported device failure, and it is
what attaches the busy/no-device hint. A classifier that answers "recorder refused"
on every ordinary window close is not a smaller assertion, it is a broken product.
The defect is that the child's message catalogue was an **uncontrolled input** to a
parser that reads English; the fix is to control the input, at the one seam where
the child is spawned.

## Red proof

Production mutation, at the single seam `voiceRecorderEnv`, then the new tests under
the hostile locale (`env -u LC_ALL LANG=zh_CN.UTF-8 go test ./cmd/agent -run TestVoiceRecorder`) :

| mutation | what it is | result |
|---|---|---|
| **A** | `return append(os.Environ(), "LANG="+…, "LC_ALL="+…)` — the append-only trap | **RED**, 4 assertions (each of `LANG`/`LC_ALL` appears 2×, each selector 1×) |
| **B** | the exact pre-fix production shape (no `cmd.Env` at all) | **RED**, 5 assertions — including the behavioural arm: *"the PINNED recorder child no longer reports `Broken pipe`"* |

A first attempt at mutation A used `return os.Environ()`, which failed as
`[build failed]` — an **unused-import** error from my mutation, not the assertion.
That is recorded because a `[build failed]` arm proves nothing, and it is why
mutation A was re-applied in the append-only form (which keeps the import live) and
mutation B carries `var _ = tools.ChildLocaleEnglish` while mutated.

The restore was verified against an **explicit revision**, not an index-relative
diff: `git hash-object cmd/agent/cli_voice_client.go` equals the pre-mutation copy,
and `git diff HEAD -- cmd/agent/cli_voice_client.go` shows only the pin.

## The tests

`cmd/agent/voice_recorder_locale_test.go`:

- `TestVoiceRecorderEnvPinsTheLocale` — deterministic, asserts the **shape**:
  each pinned name appears exactly once carrying `"C"`, each selector zero times.
  It asserts the mechanism because a naive "is `C` in there" check passes under the
  append-only trap.
- `TestVoiceRecorderReadSideMarkerSurvivesATranslatingLocale` — behavioural, and
  the arm that goes red when the pin is removed. It runs a **real child that really
  meets EPIPE** (read end closed before the write; a 6.4 MB corpus so the write
  exceeds the pipe buffer), with production's own environment, and asserts both
  halves:
  - **RED CONTROL**: with the ambient environment the marker is gone *and* the
    classifier misses the read side. If this host stopped translating, the control
    would be a lie and the test says so instead of passing.
  - **GREEN**: with `voiceRecorderEnv()` the child still reports `Broken pipe` and
    the classifier recognises the read side **from the marker, explicitly not from
    the signal branch** — it fatals if the child was killed or exited 0.

The stand-in is `sh -c 'trap "" PIPE; exec sort "$1"'`, deliberately shaped like
the measured `arecord`: **a diagnostic plus exit status 2, not a signal**. Without
`trap "" PIPE` the child dies of SIGPIPE (measured `rc=-13`), the classifier
answers TRUE through its `signal:` branch, and the marker under test would never be
the thing deciding the verdict. `sort`'s own EPIPE text is `strerror` +
coreutils' `write failed:` wrapper, so it is translated by the same mechanism:

```
$ sort probe-4m.txt | head -c 1 >/dev/null            # LC_ALL=C
sort: write failed: 'standard output': Broken pipe
$ env -u LC_ALL LANG=zh_CN.UTF-8 sort probe-4m.txt | head -c 1 >/dev/null
sort: 写入失败: 标准输出: 断开的管道
```

(`arecord` itself was considered for the test and rejected: it needs a capture
device, its EPIPE verdict arrives through the signal branch so it would pass with
the marker deleted, and Rule 27 forbids manufacturing device load to satisfy a
test. The real recorder is exercised as **forensics**, which is what the artefact
above is.)

Host skip is honest: the behavioural test skips **loudly**, naming `locale-gen`,
when `locale charmap` shows the locale cannot be set; the deterministic shape test
still runs.

## Evidence

`~/.gopi/sessions/01a141a01d5a7e4d872a6bb5e13da9e0/artifacts/` — `strerror_probe.c`
+ its three locale runs, `arec-busy-{C,zh}.txt`, `arec-epipe-{C,zh}.err`,
`parec-{c,zh}.err`, `sort-{c,zh}.err`, `probe-4m.txt`,
`redproof-A-append-only-trap.txt`, `redproof-B-pre-fix-shape.txt`,
`run-voice-locale-green.txt`, `restore-diff-HEAD.txt`.
