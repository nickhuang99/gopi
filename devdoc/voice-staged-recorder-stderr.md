<!-- doc-class: normative -->
# The staged recorder's stderr: read it, and pin the locale that makes it readable

Task `01a141bf-dbac-7c81-be44-7875d5af0278` (bug, filed per Rule 41 by the run that
pinned the one-shot recorder's locale, `01a141a0-…`, commit `d808a8745`).

## The finding, as filed

`cmd/agent/cli_voice_audio_source.go`'s `voiceStagedRecorderSource.Open` spawned the
SAME recorder argv the one-shot capture path spawns (`voiceLiveRecorderCommand`), but

1. set no `cmd.Env`, so the child inherited this process's message catalogues, and
2. captured `cmd.Stderr` into `s.stderr`, a field no reader in the repo ever touched.

Both halves are one question: **does the staged source READ its recorder's stderr?**

## The decision: it READS it

A recorder that refuses to open its device is the single most common way a capture
fails, and the child's exit status plus its stderr is the ONLY thing that names the
cause. The staged path was throwing both away and reporting:

```
parecord from "pulse:@DEFAULT_SOURCE@" produced no audio — the source is reachable
but silent: check the capture level is not muted
```

That sentence is the right *verdict* and a useless *diagnosis*: the device was not
silent, the recorder had refused to open it, and it said so on stderr. Dropping the
capture (the other branch of the decision) would have made that permanent — the
operator would have to re-run the one-shot `voice_input` to learn why `/voice` had
produced nothing. Rule 15 settles it: an error that was collected and then ignored
is a silent failure, and the field existed precisely because somebody meant to read
it.

### So the pin is not optional — it is the other half of the same decision

The moment that text is READ, the pin becomes load-bearing, and the reason is that
**the strings are parsed, not just printed**. `voiceRecorderHint` matches

- `voiceErrBusyMarker = "Device or resource busy"` (a glibc `strerror` message), and
- `voiceCaptureBrokenPipeMarker = "Broken pipe"` (the same),

so a translated child loses the busy/missing-device advice as well as the operator's
comprehension. Measured on this host, 2026-10-12, against the REAL recorder, same
absent device:

```
$ arecord -D plughw:99,0 -f S16_LE -r 16000 -c 1 -q 2>&1 >/dev/null      # LANG=C
arecord: main:831: audio open error: Invalid argument
$ env -u LC_ALL LANG=zh_CN.UTF-8 arecord -D plughw:99,0 … -q 2>&1 >/dev/null
arecord: main:831: 音乐打开错误： 无效的参数
```

The pin is `internal/tools.ChildLocaleEnv` — the ONE spelling of strip-then-append
(Rule 14). `d808a8745` had exported it (as `ChildLocaleEnv` plus
`ChildLocaleEnvName` / `ChildLocaleLocalePinnedNames` / `ChildLocaleSelectorNames` /
`ChildLocaleEnglish`) for exactly this kind of second caller; this change is that
caller arriving. Strip-then-append is the load-bearing half — and the reason is
NOT the one `internal/tools/git_child_locale.go`'s comment gives; see the
measurement immediately below, which inverts it.

**The measurement.** Measured on this host 2026-10-16 (generator + raw output under this run's
session directory, `evidence/duporder/main.go` → `evidence/duplicate-env-order.txt`):
a duplicated exec-env key resolves to its **LAST** entry — glibc's gettext view
(case B: `C` first, `zh` last → the message comes out translated) **and** the
child shell's `getenv` view (case D: `C` first, `zh` last → the shell reads
`LANG=zh_CN.UTF-8`). Filed, not fixed here, as task
`01a14212-e0d5-7aa5-8db3-0eca2b4e0969` (Rule 41): the wrong comment lives in
`internal/tools/git_child_locale.go`, whose owner decides its wording.

So the honest reason for the **strip** half is narrower than the comment above
claims: with the two consumers measured here, `append(os.Environ(), …)` alone
would in fact decide the language. The strip is kept because it removes the
*dependence on that ordering at all* — this call site does not control which
loader reads the environment (a shell, a C library, a Python process, a future
`exec` shim), and the ordering result above is a measurement of two of them, not
a property of `execve`. Removing the inherited entries is the half that holds
whatever the loader does. State it as that, never as "first wins".

## What was built

**1. The pin, at the single spawn seam** (`cli_voice_audio_source.go`, `Open`): one
line, `cmd.Env = tools.ChildLocaleEnv()`. No second locale list, no bare `LC_ALL` /
`LANG` literal in `cmd/agent` non-test code.

**2. The read, as a source-level fact rather than a type assertion.**
`voiceAudioSource` gains

```go
Diagnostic() string
```

The refusal is built by the WINDOW BUILDER (`voiceStagedStream`), which holds a
`voiceAudioSource` and nothing else; a type assertion there would be a second, silent
definition of which sources can explain themselves. `voiceStagedRecorderSource`
implements it, `voiceFileSource` answers `""` (a file has no process behind it), and
`voiceMicStreamSink` delegates, as it already does for `Health` and `Origin`.

**3. The refusal carries the child's own sentence** (`cli_voice_recorder_staged.go`,
`sourceSilenceNote`), on both silence refusals (`flush`'s "produced no audio" and
`noFirstSample`'s "within Ns").

### The restraint IS the decision

The note is attached under exactly two conditions, both necessary:

- **the reader has stopped** (`readEnded`, a new field the reader's `defer` sets) —
  a recorder whose stdout reached EOF is over, and its stderr is a finished document.
  A recorder still running has not failed yet, and one whose window WE closed prints
  the ordinary notes of a working microphone, which repeating at every paused round
  would turn into noise;
- **the child actually said something** — a silent exit adds nothing to a sentence
  that already says no audio arrived.

A recorder that was closed under us, or one whose read failed (the stream's own
health already reports that), gets no note: one failure must not acquire a second,
louder verdict.

`voiceStderrBuffer` replaces the bare `bytes.Buffer`: `os/exec`'s copy goroutine
writes it for the child's whole life while the caller's goroutine reads it for a
refusal — an unsynchronised `bytes.Buffer` there is a data race and a torn sentence.

### One hint builder, not two

`voiceRecorderHint` is factored out of `voiceRecorderFailure`, so the one-shot
refusal and the staged diagnostic print the same advice for the same cause
(`voiceStagedRecorderNote` quotes the last three lines, the count the one-shot path
already used).

## Not done

- The **staged `parecord` argv carries no duration flag** (`voiceLiveRecorderCommand`
  strips `-d`), so the stand-in in the regression test is a stub, not the real
  recorder: the real recorder path is exercised by the one-shot guard
  `cmd/agent/voice_recorder_locale_test.go` (on `task/01a141a0-…`), and the staged
  guard here pins the SPAWN (its `cmd.Env`) and the READ (the surfaced sentence).
- No load was generated and no device was opened: the stub never touches a
  sound card (Rule 27).

## Verification

See the task report at `$GOPI_TASK_REPORT_PATH` for the run's own Build/Tests/Ran/
Result, the red proof and the explicit-revision restore check.

```bash
env -u LC_ALL LANG=zh_CN.UTF-8 go test ./cmd/agent -run 'TestVoiceStagedRecorder' -count=1 -v
```

## A flake the new arm found in the FIX ITSELF — the child's stderr is a SECOND pipe

The behavioural arm went red intermittently while the shape arm stayed green. That
is a defect in the CODE, not in the clock, and it was measured rather than guessed:
`-count=200` of `TestVoiceStagedRecorderDiagnosticSpeaksThePinnedLocale` failed
**53 times** with

```
the staged source surfaced no diagnostic at all for a recorder that exited 1 with
stderr … child stderr was ""
```

(`evidence/flake-count200.txt`).

**Mechanism (5-why).** The source had `cmd.Stderr = &s.stderr`, i.e. the copy was
done by `os/exec`'s own goroutine, which this struct cannot observe. The gate on
the read was `readEnded` — set when the SAMPLE pipe (stdout) hits EOF. Those are two
different pipes: the child can close stdout before the kernel hands the copy
goroutine its last stderr bytes, so `Diagnostic()` read an empty buffer for a
recorder that had already exited 1 *having said why*. Why it looked fine in a
single run: the window is microseconds, and it only opens under a scheduler that
preempts at the wrong instant — which is what 200 iterations arrange.

**Fix, at the RIGHT level.** The source now owns that pipe: `cmd.Stderr` stays nil
and `cmd.StderrPipe()` + a `drainStderr` goroutine read it into `s.stderr`, setting
`stderrEnded` and closing `stderrDrained` at EOF. `Diagnostic()` **waits** on that
channel (`voiceStagedStderrDrainWait`, 2 s, Rule 1) instead of sampling a flag, and
`readerStopped` requires BOTH halves (`readEnded && stderrEnded`). `Close` closes
the read end so a drain parked on a dead writer cannot outlive the source.

The wait is bounded on purpose: a refusal is synchronous and must never hang on a
recorder that keeps its stderr open. Past the bound the buffer is quoted as it
stands — the honest answer, not an invented one.

**Re-measured after the fix:** `-count=200` green (0 failures), the same 200 that
had 53.
