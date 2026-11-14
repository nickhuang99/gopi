<!-- doc-class: historical -->
# Task Report: the four voice-module defects of task `01a11da05a507db986684d21e79a7640`

Branch `task/01a11da05a507db986684d21e79a7640` (a linked worktree of the main
checkout — Rule 6c, no push involved).

## Completed

Four defects fixed in `cmd/agent/`, each with a red→green case in the new
`cmd/agent/voice_module_defects_test.go`, plus one independent report investigated
and answered honestly (not fixed).

### A — the chat privacy row carried TWO `[voice] ` prefixes

`chat_voice.go` composed the cloud engine's row as `m.notice("[voice] %s\n",
voiceDoubaoPrivacyNotice(opts))`, and that notice **already** begins with
`[voice] ` (`cli_voice_engine.go:131`, the same one function the CLI prints
through, and the CLI prints it bare). Every chat entry on the doubao arm therefore
showed `[voice] [voice] PRIVACY — engine doubao UPLOADS …` — Rule 13: the value's
spelling lives with the value, and the caller re-typed it.

Fix: the call site prints the notice **bare** (`m.notice("%s\n", …)`), matching the
sibling rows (`chatVoiceStreamEngineLabel` / `chatVoiceEngineLabel` carry no prefix
either). The task brief also asked whether the device-note loop
(`chat_voice.go`, `m.notice("[voice] %s\n", n)`) had the same defect: it does
**not** — `voiceListDevices` / `voiceResolveSelection` return notes with no
prefix (`cli_voice_client.go:412,436`), and the CLI prints those through the very
same `"[voice] %s\n"` shape (`cli_voice_client.go:1116,1122,1132`). Changing it
would have REMOVED a prefix the operator sees today, so it is left alone.

### B — `errVoiceStreamClosed` was reported as a failed round

`errVoiceStreamClosed` is the SENTINEL for "the capture was closed under this
round" — the mode's own exit. `voiceChatRecorderStaged.readerFailure` already
answers `nil` for it, but the loop (`chat_voice.go`, the OTHER reader of the same
sentinel) took the generic arm: the screen read
`voice mode FAILED — the voice capture stream was closed. The mode is OFF`, and the
exit was attributed to `OFF (a failed round)`, on a round nothing had failed.

Fix: the loop recognises the sentinel with `errors.Is`. The half that is **not**
just "be quiet" is stated in the brief and kept: a sentinel round that reaches a
**still-owned** mode (nothing else closed the capture) must not leave the mode ON
over a microphone nobody is reading — that is the half-open state Rule 15 forbids.
So it leaves, with a reason of its own (`chatVoiceLeftByCaptureEnded = "the capture
ended"`), not the failure note and not "a failed round". In the production flow the
operator's own exit already made `stale` true and the arm is a plain return.

### C — `gopi tool voice_input -s 5` was read as a FILE named `-s`

The shared flag parser (`memoryFlagSet`) only knows the **double**-dash spelling,
so `-s` was filed as a POSITIONAL; the invocation then took the file shape, and the
run died inside the engine with
`read audio "-s": open -s: no such file or directory` — a refusal naming a file the
operator never wrote, never mentioning the flag they typed (Rule 15). The
membership loop that existed checked `fs.order`, which by construction can only
hold flags the parser had already declared, so it could never see the shape that
was broken.

Fix: a new `voiceRefuseUndeclaredFlags` runs **before** the positionals are
interpreted and answers BOTH spellings; a short spelling with an obvious long
equivalent names it (`did you mean --seconds ?`). It refuses only words that are
*spelled like flags*: a lone `-` (the stdin convention) and a numeric token such as
`-3` stay positionals, and that boundary is pinned by its own negative-control case.

### D' — `transcribed locally` meant two things at once

`runVoiceCaptureLocal`'s closing line said `transcribed locally`, where "locally"
carried both meanings the voice module uses it for — "**this machine** vs the far
end of a `--client` hop" and "**the local funasr engine** vs the cloud one". On a
run whose OPENING row had just announced that the audio leaves this machine, the
closing row read as its own contradiction; two independent readers hit it.

**The privacy disclosure was correct and is untouched** (`--seconds 5` with no
`--engine` really does upload to doubao; the default engine is the cloud one). What
changed is the closing line, which now **names the engine it used**, through the
SAME `voiceEngineLabel` the announcing rows print:

| | before | after |
|---|---|---|
| local arm | `[voice] transcribed locally in 53.0s (chunk=15s)` | `[voice] transcribed by funasr-nano in 33.4s (chunk=15s)` |
| cloud arm | `[voice] transcribed by doubao in …` (engine named, so the sentence is TRUE where it uploads) | same |

The empty-transcript half of the same function was rewritten the same way (its old
text said "the local engine" on a run that may have uploaded). Two new named
constants hold the rendered lines (`voiceTranscribedByNote`,
`voiceEngineNoTextNote`), so the wording has one definition (Rule 1b/13) and the
closing line cannot name an engine different from the one the opening line named.
`runVoiceCaptureLocal` is *not* renamed: it is called from `transcribeFile`'s
dispatch and its name reads "the in-process capture arm", which is what the
`--client` split means by "local"; the ambiguity that cost us was in the printed
sentence, which is what was fixed. (Recorded here as a considered-and-rejected
alternative rather than silently omitted.)

### The independent problem (hosta, `/voice` failing on the first round) — NOT REPRODUCED

The brief asked for reproduction first, and honest reporting if it does not
reproduce. **It did not reproduce on this host (peerbox)**, on either engine, in three
different shapes, all driven through the REAL pty (`devdoc/evidence/sgr-grid/termgrid.py`,
60×30, the operator's own keystrokes):

| shape | settings | entry | first round | later |
|---|---|---|---|---|
| doubao (default) | `{"voice_engine":"doubao","doubao_api_key":"sk-…fake"}` | `VOICE MODE ON … transcribing with the doubao engine every 6s` | no text (the key is fake, as expected) | after 90s the cut-inside-speech row, `Still listening` — the mode never left |
| local | `{"voice_engine":"local"}`, rung-2 deployment `/home/user/work/funasr-models` | `VOICE MODE ON … transcribing with the local engine every 6s` | mode stayed ON over 50s of shots | — |
| leave + re-enter | doubao | OFF row on the second `/voice`; the third `/voice` re-entered and the grid showed `rec · ready` | re-entry works | — |

What the three runs DO show is that the reported screen
(`VOICE MODE ON` immediately followed by `voice mode FAILED — the voice capture
stream was closed.`) is exactly what the pre-fix loop printed for the sentinel —
i.e. **the reported symptom's second half IS defect B**, and B is now fixed and
pinned by a case that drives the sentinel through the production loop. The
remaining half of the report — "the rec state was not shown" — did not reproduce
either: every one of the runs above painted `rec · ready` on the status row while
the mode was on (the dump is in this run's artifacts; the row's own field was
added by `01bfa6611`, 2026-10-08, after the operator's report).

What I could NOT do, stated so it is not read as covered: hosta is not reachable
from here (`ssh hosta` → `Could not resolve hostname`, and **hosta is not an
alias in this host's ssh config**), so its own settings, its engine resolution and
its capture device were never measured — those are the three host-dependent inputs
this failure is most likely to hang on, and peerbox's values of all three differ from
hosta's by construction. **A reproduction on the host that produced the report is
still owed**, and the reproducing recipe is the three termgrid invocations above
verbatim. If it does reproduce there, the most likely remaining root is upstream of
this diff: the first two rounds are the only ones whose `startReader` can fail
(`errVoiceStagedBoundChanged` is returned when a second Round asks for a different
bound, and a Round that reaches a recorder whose `Select` never ran gets
`errVoiceStagedNotOpen` — which **is** `errVoiceStreamClosed`, i.e. it reports as a
quiet exit under the new loop and NOT as FAILED, so that road is now visibly
distinct on the screen rather than silent).

## Not Done

- **The hosta reproduction.** Explained above; the host is unreachable from peerbox
  and is not in this host's ssh config. Not a silent skip: the exact commands are
  given, and the missing measurements are named.
- **`runVoiceCaptureLocal` was not renamed**, deliberately (see D').
- **The device-note loop was left as it is**, deliberately (see A) — it does not
  carry the doubled prefix.
- Deliberately NOT widened: the cloud arm's own closing line (the file shape,
  `cli_voice.go` `"[voice] %s in %.1fs (chunk=%ds)"`) already names the file rather
  than the engine; changing it would be a second wording decision the brief did not
  ask for, and it makes no claim about where the audio went.

## Verification

- **Build**: `CGO_ENABLED=0 go build ./cmd/agent/` → `BUILD_OK` (also the full
  `make test-budget` run below, which builds the tree).
- **Tests**: `make test-budget PKGS='./cmd/agent ./internal/config ./internal/doubaostream'`
  — log at `<session>/artifacts/final-test.log`, stream
  `…/queue/test-budget/run-1791501752-3445346.json`. Verdicts, read out of that
  stream: `internal/config` **pass**, `internal/doubaostream` **pass**, and
  `cmd/agent` **fail on exactly ONE test, unrelated to this diff** —
  `TestCodeSearchBackendNoSymbol`, whose own output is the witness:
  `code_search: …/.gopi-tags: tags index incomplete (rebuilt 3s ago and the index
  on disk is still unfinished)`. That is the tags-index rebuild racing the case,
  not a voice behaviour; `go test ./cmd/agent/ -run 'TestCodeSearchBackendNoSymbol' -count=1`
  re-run against the same tree → `ok github.com/nick/gopi/cmd/agent 1.037s`.
  Every case of this diff is **pass** in the same stream
  (`TestChatVoicePrivacyRowCarriesOnePrefix`, `TestChatVoice_CaptureClosedExitsQuietly`,
  `TestVoiceUndeclaredShortFlagIsRefused` + its three sub-cases,
  `TestVoicePositionalShapesSurviveTheShortFlagRefusal`,
  `TestVoiceClosingLineNamesTheEngine`). Focused, verbatim:
  - `go test ./cmd/agent/ -run 'TestChatVoicePrivacyRowCarriesOnePrefix|TestChatVoice_CaptureClosedExitsQuietly|TestVoiceUndeclaredShortFlagIsRefused|TestVoicePositionalShapesSurviveTheShortFlagRefusal|TestVoiceClosingLineNamesTheEngine' -count=1 -v`
    → all five `PASS`
  - `go test ./cmd/agent/ -run 'Voice|voice' -count=1` → `ok github.com/nick/gopi/cmd/agent 68.835s`
- **Ran** (the surfaces this diff changes, verbatim, on the REAL binary):

  ```bash
  # C — the operator's own typo, before and after
  /tmp/gopi-v1 tool voice_input -s 5
  #   -> gopi tool voice_input: doubao engine: doubaoasr: read audio "-s": open -s: no such file or directory   (exit 1)
  /tmp/gopi-v2 tool voice_input -s 5
  #   -> gopi tool voice_input: unknown flag -s (gopi tool voice_input takes long flags, not short ones — did you mean --seconds ?)
  #                                                                                                        (exit 1)
  /tmp/gopi-v2 tool voice_input -x
  #   -> gopi tool voice_input: unknown flag -x (gopi tool voice_input takes long flags, not short ones)
  /tmp/gopi-v2 tool voice_input -3     # a NUMERIC positional is not a flag: still the file shape
  #   -> … read audio "-3": open -3: no such file or directory
  echo "" | /tmp/gopi-v2 tool voice_input -   # the stdin convention survives
  #   -> the same file-shape read (a lone `-` is not refused as a flag)

  # D' — the closing line, on a real 2s capture, before and after
  GOPI_VOICE_ENGINE=local /tmp/gopi-v1 tool voice_input --seconds 2
  #   -> [voice] transcribed locally in 53.0s (chunk=15s)      + the transcript on stdout
  GOPI_VOICE_ENGINE=local /tmp/gopi-v2 tool voice_input --seconds 2
  #   -> [voice] transcribed by funasr-nano in 33.4s (chunk=15s) + the transcript on stdout

  # A + B — the chat surface, under a REAL pty (Rule 31), termgrid.py 120x30
  GOPI_HOME=<sandbox> python3 devdoc/evidence/sgr-grid/termgrid.py /tmp/gopi-v1 120 30 script-redA.txt
  #   -> grid row: "[voice] [voice] PRIVACY — engine doubao UPLOADS the audio to …"      (the defect, on the real screen)
  GOPI_HOME=<sandbox> python3 devdoc/evidence/sgr-grid/termgrid.py /tmp/gopi-v2 120 30 script-doubao.txt
  #   -> grid row: "[voice] PRIVACY — engine doubao UPLOADS the audio to …"              (one prefix)
  #      and the mode stayed ON across 90s of rounds (no FAILED row)
  GOPI_HOME=<sandbox> python3 devdoc/evidence/sgr-grid/termgrid.py /tmp/gopi-v2 120 30 script-reenter-real.txt
  #   -> OFF row on the 2nd /voice, and the 3rd /voice re-entered with "rec · ready" on the status row
  ```

- **Result**: each `Ran:` line's output is quoted beside it above; the pty grids
  (`01-entered`, `02-round2`, `10-t30`, `11-t60`, `12-t90`, `20-local-t25`,
  `21-local-t55`, `30-first-entry`, `31-left`, `32-second-entry`, `40-prefix-before`,
  `50-localA`, `51-localB`, `60-nokey`, `70-on1`, `71-off1`, `72-on2-firstround`,
  `.grid.txt` + `.attr.txt` + `.raw.txt` per shot) are in this run's evidence
  directory: `/home/user/.gopi/sessions/01a11da0-5a50-7db9-8668-4d21e79a7640/artifacts/pty/`.
- **Commit**: `3dc17d3bd fix(voice): four voice-module defects — doubled prefix, sentinel reported as FAILED, short flag read as a file, ambiguous closing line`
  on `task/01a11da05a507db986684d21e79a7640` (a linked worktree — the commit is already on the
  branch in the shared repo, Rule 6c).

## Evidence (self-filed)

- A: `termgrid.py /tmp/gopi-v1 …` → `40-prefix-before.grid.txt` line 19 begins
  `[voice] [voice] PRIVACY`; the same shot against `/tmp/gopi-v2`
  (`01-entered.grid.txt` / `10-t30.grid.txt`) reads `[voice] PRIVACY`, and
  `TestChatVoicePrivacyRowCarriesOnePrefix` asserts `strings.Count(rows, "[voice] ") == 1`
  on the production grid.
- B: `TestChatVoice_CaptureClosedExitsQuietly` PASS, with the case asserting the
  three facts at once (no `voice mode FAILED`, no `chatVoiceLeftByFailed`,
  `!h.mode.active()` and exactly one round run). Green now; the pre-fix screen is
  quoted in the case's own comment and was observed red by the pre-fix loop's
  code path (`m.fail(err)` with no sentinel check).
- C: the three `-s 5` / `-x` / `audio -s 5` refusals above, each quoted with its
  exit status, on `/tmp/gopi-v2`; the negative control
  `TestVoicePositionalShapesSurviveTheShortFlagRefusal` PASS.
- D': `GOPI_VOICE_ENGINE=local /tmp/gopi-v2 tool voice_input --seconds 2` printed
  `transcribed by funasr-nano` where `/tmp/gopi-v1` printed `transcribed locally`
  — the two lines are both quoted in `Ran:` above.
- Suite: `make test-budget PKGS='./cmd/agent ./internal/config ./internal/doubaostream'`
  → see `artifacts/final-test.log` (+ `scripts/test-budget.sh`'s stamp line, which
  carries the exit status and the stream file for that run).
- Worktree: `git status --porcelain` prints nothing after the commit (Rule 30).
