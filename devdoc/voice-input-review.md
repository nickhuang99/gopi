<!-- doc-class: normative -->
# Voice Input Review — `gopi voice_input`

**Task:** independent review of the `voice_input` feature (`cmd/agent/cli_voice.go`,
`cmd/agent/cli_voice_server.go`; commits `bd3d34a5`, `79d6fe52`), by finishing the
salvaged review tests from the dead task `01a091f1-5947`, measuring before/after
coverage, and checking the named hazards.
**Branch:** `review/voiceinput-2` (worktree `~/workspace/gopi-voicereview2`, off master `0244f3d1`).
**Review only** — one production change (Rule 1 constants); no behaviour change.
**Supersedes:** nothing. The earlier review attempt `01a091f1-5947` stays failed and is not revived.

---

## 1. Salvaged test files — verdict per file

| Salvaged file | Verdict | Reason |
|---|---|---|
| `cli_voice_parse_test.go` (247 lines) | **KEPT as-is** | Already complete and correct: 12 parse/deps/model tests, all green, no edits needed. |
| `cli_voice_review_test.go` (159 lines) | **ADAPTED + substantially extended** | Header told the truth ("the three main-process funcs exit the process…"), but the `/transcribe` HTTP handler, `writeVoiceJSON` and the stdio end-to-end were *prose promises*, not tests. Also contained a **false assertion** (§4, finding F4). Now ~1 000 lines with the promised tests, the in-process units, and the transport end-to-end. |
| `zz_voice_probe_test.go` (52 lines) | **DELETED** | A pure leftover probe: `t.Logf` diagnostics with no assertions (the `zz_` prefix is this repo's marker for a scratch file). Its only real content — that the tool is reachable — is asserted properly by `TestVoiceSubcommandHelpSmoke` + the contract-mirror tests. |

Salvage archive: `untracked.tgz` (from the dead session's attachments). Baseline was
measured by moving all three files aside (the salvaged file could not compile in
isolation — see §4 F4), so the before/after numbers below are honest.

---

## 2. Coverage, before and after

Measured with `go test ./cmd/agent/ -cover -coverprofile=…` +
`go tool cover -func`. `scripts/func_coverage.py` cannot be used here: it
enumerates **exported** functions by regex, and every voice function in
`package main` is unexported — `go test -cover` is the measurement.

### 2.1 Statement totals

| File | Before | After | Δ |
|---|---|---|---|
| `cli_voice.go` | 43/95 = **45.3%** | 68/95 = **71.6%** | +26.3 pp |
| `cli_voice_server.go` | 40/103 = **38.8%** | 84/103 = **81.6%** | +42.8 pp |
| **Both voice files** | 83/198 = **41.9%** | 152/198 = **76.8%** | **+34.9 pp** |

Package-level: 46.7% → 46.7% (the voice files are ~0.2% of `cmd/agent`, so their
gain is invisible at package granularity — the per-file numbers are the meaningful
signal).

### 2.2 Per-function breakdown

| Function | Before | After |
|---|---|---|
| `cli_voice.go` | | |
| `isVoiceSubcommand` | 0.0% | 66.7% |
| `voiceFlagMember` | 100.0% | 100.0% |
| `parseVoiceArgs` | 90.3% | 100.0% |
| `voiceModelPaths` | 100.0% | 100.0% |
| `voiceDepsReady` | 25.0% | 100.0% |
| `transcribeFile` | 0.0% | 100.0% |
| `lastLines` | 100.0% | 100.0% |
| `runVoiceSubcommand` | 0.0% | 0.0% (see §3) |
| `voiceReadAll` | 83.3% | 83.3% |
| `cli_voice_server.go` | | |
| `voiceServeHTTP` | 0.0% | 73.5% |
| `voiceServeStdio` | 0.0% | 86.7% |
| `voiceMaterializeAudio` | 83.3% | 83.3% |
| `voiceWriteTemp` | 53.8% | 53.8% |
| `voiceWrapPCMAsWAV` | 100.0% | 100.0% |
| `writeVoiceJSON` | 0.0% | 75.0% |
| `discardFile` | 0.0% | 100.0% |
| `userHomeDirOrDefault` | 75.0% | 100.0% |
| `voiceLooksLikeAudioPath` | 100.0% | 100.0% |

Two package-wide numbers were also lifted: the contract-mirror guards
(`TestVoiceInputIsAContractToolMirror`) and the parse boundary table.

---

## 3. "Could not cover" — the honest list

1. **`runVoiceSubcommand` stays 0.0%, and this is a RUNTIME limit, not missing
   effort.** Measured: every path in that function ends in `os.Exit` (`--help`,
   parse error, dependency error, engine error), and **the Go runtime does not
   flush coverage counters on a production `os.Exit`** — `runtime/coverage`
   only helps an exit the test itself owns. Proof: the instrumented test binary
   run as the CLI child with `GOCOVERDIR` set exits 0, prints the correct usage
   on stdout, and leaves the coverdir **EMPTY**. The behaviour is nonetheless
   asserted end to end (exit codes, exact stderr messages, dependency order) by
   `TestVoiceRunExercisingExitPaths` — covered by behaviour, not by counters.
2. **Real ASR output quality** — not attempted. The review drives a **stub
   engine** (argv[0] of a test binary via the documented `--runtime PATH` seam),
   because the real one needs ~910 MB of weights and ~70 s per clip. Everything
   *around* the engine is verified; the recognition quality itself is out of
   scope for a test suite (and is the upstream FunASR model's property).
3. **Browser/`MediaRecorder` clients and real network conditions** — not
   tested; the HTTP contract is asserted with Go's own client.
4. **`voiceWriteTemp` 53.8% / `voiceReadAll` 83.3% / `voiceMaterializeAudio`
   83.3%** — the uncovered remainder is the `os.CreateTemp` error branch, which
   needs an unwritable temp dir mid-call; the oversize-input branch *is* covered
   (`TestVoiceReadAllRejectsOversize`, `TestVoiceMaterialize*`).
5. **`isVoiceSubcommand` 66.7%** — the uncovered line is `return false` for a
   miss. Calling it with a non-matching argv in-process is safe, but the
   negative space is already asserted positively by
   `TestVoiceIsVoiceSubcommandNegativeSpace` (5 subtests) and by the other CLI
   tests that would break if a near-miss verb were swallowed.

---

## 4. Findings

### F1 — P0: the default listen address binds every interface with no authentication
**`cmd/agent/cli_voice.go:60` — `voiceDefaultListen = ":7788"`**

- `net.Listen("tcp", ":7788")` with an **empty host** binds `0.0.0.0` + `::`
  (verified against `$(go env GOROOT)/src/net/http/server.go`
  `ListenAndServe` → `net.Listen("tcp", addr)`).
- The endpoint has **no authentication** and accepts multipart audio uploads
  (`/transcribe`), plus `/health`. Running `gopi voice_input --server` with no
  flags therefore exposes an unauthenticated transcription service to every
  reachable network.
- **It is also an inconsistency bug.** Both sibling listeners in the same
  package default to loopback: `cli_web.go:386 webDefaultAddrValue =
  "127.0.0.1:8090"` and `cli_proxy.go:64 listen: "127.0.0.1:8090"`.
- Evidence in-tree: `TestVoiceHTTPListenDefaultShape` pins the current value and
  the sibling convention, and logs the finding on every run rather than failing
  (the fix is a behaviour change belonging to the filed bug).
- **Verdict: real defect.** File a bug task; the fix is a loopback default with
  `--listen :7788` documented for the LAN case.
- **FIXED** in `fix/voice-listen-default` (devdoc/voice-listen-fix.md): the default is now
  `127.0.0.1:7788`, the LAN opt-in is documented and warns at startup.

### F2 — `voiceLooksLikeAudioPath` is dead code

> **FIXED** in `fix/voice-listen-default` (devdoc/voice-listen-fix.md): the helper and its test are deleted.
**`cmd/agent/cli_voice_server.go:239`**

Its only callers anywhere in the tree are tests (`cli_voice_test.go:234,239`).
No production path consults it: `voiceMaterializeAudio` decides by **container
magic** (`voiceContainerMagic`), not by filename. Either wire it in or delete it
— an exported-behaviour-looking helper that nothing calls is a trap for the next
reader.
**Verdict: real defect (dead code).** Low severity; fold into the same bug task.

### F3 — no upload size limit at the HTTP boundary (`MaxBytesReader` absent)

> **FIXED** in `fix/voice-listen-default` (devdoc/voice-listen-fix.md): declared-length guard +
> `http.MaxBytesReader`, with the multipart spool owned by the handler.
**`cmd/agent/cli_voice_server.go:23-95`**

The handler streams the multipart part into a temp file via
`io.Copy(tmp, io.LimitReader(file, voiceMaxAudioBytes))` — the **copy** is
capped, so RAM is safe, but `r.FormFile` has already spooled the request body
and there is no `http.MaxBytesReader` guarding the request itself (no such call
exists anywhere in the tree). A client can therefore push body data to disk
before the cap is consulted forward. `ReadHeaderTimeout: 10 * time.Second` is
set (good), but there is no read/body timeout.
**Verdict: real but low-severity hardening gap** (needs an untrusted client to
exploit). Record here; no test added — the fix is a one-liner in the handler.

### F4 — the salvaged review file asserted a FALSE contract
**`cli_voice_review_test.go` (salvaged) — `TestVoiceInputIsDeclaredAToolMirror`**

It asserted that `voice_input` appears in the argv tool-mirror table. It does
not: `toolMirrorArgvVerb` (`cmd/agent/cli_toolnames_contract_test.go:39`)
contains only `web_search`, `web_fetch`, `vision_analyze`, `text2img`. Adopting
the file as-is would have **invented a requirement** and forced a production
table entry to satisfy a test.
**Verdict: false premise, corrected.** Replaced with
`TestVoiceInputIsAContractToolMirror`, which asserts what actually exists: an
inventory case for the verb, the usage fingerprint pin, and
`dispatcherOfToken[voice_input] == "VoiceSubcommand"`.

### F5 — `userHomeDirOrDefault` can return a relative directory
**`cmd/agent/cli_voice_server.go:229`**

With `HOME` unset it returns `os.TempDir()` verbatim. `os.TempDir()` is `TMPDIR`
when set, and **`TMPDIR` may be relative** (and `HOME=""` is an error, so the
fallback is reachable). The voice model/runtime roots are joined onto this, so
a relative return silently resolves against the CWD.
**Verdict: minor robustness issue.** Not filed separately; the fallback branch
is now asserted to be non-empty and absolute under a controlled env
(`TestUserHomeDirOrDefaultFallsBack`, using `testutil.SnapshotEnv` so the unset
state is restored exactly).

### F6 — no resource leak found in either transport
Checked and **negative**, recorded because "we looked" is the useful result:
- HTTP: the uploaded temp file is removed (`defer os.Remove(tmp.Name())`) and
  the materialised file is closed; asserted by
  `TestVoiceHTTPServerEndToEnd`, which stats the path the engine was handed and
  requires `os.IsNotExist`.
- stdio: `voiceMaterializeAudio`'s `cleanup` runs via `defer`; asserted by
  `TestVoiceStdioServerEndToEnd` (same stat-after check).
- The server goroutine terminates correctly; `TestVoiceServeHTTPHandlerContract`
  asserts `voiceServeHTTP` does not return early.

### F7 — no swallowed errors found (Rule 15b)
All three `_ =`/ignored discards are deliberate and carry a `stylegate:ignore`
with a reason: `writeVoiceJSON`'s encode error (headers already committed),
`discardFile`'s close error (abandoned temp file), and the `stylegate`-annotated
best-effort writes in the tests. `transcribeFile` **propagates** the engine's
stderr tail via `%w`, asserted by `TestTranscribeFileSuccessAndFailure`.

---

## 5. What the new tests actually establish

Exit-path / argv level (real binary, `TestVoiceSubcommandHelpSmoke`):
`-h` → usage, exit 0; unknown flag → the flag is named, exit 1; the verb never
falls into the "read the argument as an input file" path; a near-miss verb is
not swallowed.

File shape (`TestVoiceFileModeTranscribesViaStubEngine`): stdout IS the
transcript; the banner reports the **effective** chunk; the engine argv carries
`--enc <encoder> -m <llm> -a <user's own path> --chunk N` (a regression here is a
silent wrong-weights bug); file mode never copies the audio.

HTTP shape (`TestVoiceHTTPServerEndToEnd`): `/health` payload (`ok`, `engine`,
`chunk`); multipart POST returns `text`/`engine`/`chunk`/`elapsed_ms`; the
uploaded bytes reach the engine **byte-identical**; the temp file keeps the
client's `.wav` extension and is gone afterwards; GET → 405; missing field →
400 naming `audio`; engine failure → 500 carrying the engine's stderr tail.

stdio shape (`TestVoiceStdioServerEndToEnd`, `…RejectsEmptyInput`,
`…PassesContainerThrough`): raw PCM is wrapped in a 44-byte RIFF header (byte
count **and** payload compared); an existing container is passed through
**unmodified**; empty stdin is an error, not an empty transcript; the
materialised file is removed.

In-process units (`voiceServeStdio`, `voiceServeHTTP`, `transcribeFile`,
`writeVoiceJSON`, `discardFile`, `userHomeDirOrDefault`): the bodies that do not
`os.Exit`, driven directly — this is where the +35 pp comes from.

---

## 6. Harness notes (for the next reviewer)

1. **The full `cmd/agent` suite needs `bin/gopi`** — `TestSweepZombiesCLI_*` FAIL
   (not skip) without it, by design. Build first:
   `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/`.
2. **The full suite takes ~145-170 s** — longer than a single bash call's
   no-output window; run it as a background job.
3. **Child-process coverage does not work the way it looks like it should.**
   Measured: a plain child re-exec contributes nothing; `-test.gocoverdir` on a
   non-instrumented child errors; and (the one that cost the most time) **a
   production `os.Exit` discards the counters entirely**. The only coverage that
   lands from a child is code that RETURNS. Hence §3.1.
4. **A subprocess-driven test must not contort `-test.run` to make the child act
   as the CLI.** The working shape is an `init()` seam gated on an env var
   (`GOPI_VOICE_CLI_CHILD`, `GOPI_VOICE_STUB_MODE` in this file), with the CLI's
   own argv after `--`.
5. **`testutil.Proc.Stop()` is one-shot and already correct** — do not "improve"
   it. An attempted change here (a kill fallback plus a bounded wait because a
   reap looked like a no-op) produced a 10 s hang and a live orphaned server.
   The original `p.mock.Reap(); <-p.done` completes in ~10 ms. Reverted.
6. **The suite pins `TMPDIR`** to a `/dev/shm` scratch root and refuses `/tmp`;
   attachment extraction must go under `~/workspace/`, not `/tmp`.

---

## 7. Production changes in this branch (minimal, Rule 1 only)

- `cmd/agent/cli_voice.go`: hoisted the two weight-file names into constants
  (`voiceEncoderName`, `voiceLLMName`) and used them in `voiceModelPaths`; added
  `voiceDefaultListenPort` so the test pins the port without duplicating the
  literal. **Same string values — no behaviour change.**
- `internal/testutil/run.go`: added `StartGopiBin` (launch an explicit binary
  path with the standard process discipline) and factored the shared
  `startProc`. Purely additive; `StartGopi`'s behaviour is unchanged.
- `cmd/agent/gopibin_test.go`: added `CoverBin` (a `go build -cover` sibling of
  the plain test binary) and extended the single-owner cache guard to cover it.
- `cmd/agent/cli_voice_test.go`: `TestVoiceModelPaths` now uses the new
  constants instead of repeating the literals.

---

## 8. Verification

```
CGO_ENABLED=0 go build ./...                       # exit 0
gofmt -l cmd/agent/ internal/testutil/             # empty
go vet ./cmd/agent/ ./internal/testutil/           # clean
go test ./cmd/agent/ -run 'TestVoice|TestTranscribeFile|TestWriteVoiceJSON|
    TestDiscardFile|TestUserHomeDir' -count=1      # ok — 44 PASS, 0 FAIL, 0 SKIP
go test ./cmd/agent/ -count=1                      # ok  github.com/nick/gopi/cmd/agent  109.143s
go test ./cmd/agent/ -run '…' -count=1 -cover      # coverage profile captured (numbers in §2)
```

**Pre-existing failure, NOT attributable to this branch.**
`go test ./internal/testutil/` FAILS on two repo-wide gates:

- `TestWorkerOwnershipPointerPresent` — a list of `devdoc/*.md` and `scripts/*.sh`
  files (from other, concurrent agents' commits) that lack the canonical
  `WORKER-OWNERSHIP: OPERATOR-ONLY` pointer line.
- `TestScriptsAreBoundedAndOffTmp`

Confirmed pre-existing by running the same package **in the untouched main repo**
(`/home/user/work/GoPi` at `543803ab`): identical failures, same file list. This
branch adds nothing to `internal/testutil` that could fail — the change is
`+18/-1` (one new `StartGopiBin` plus the shared `startProc` body) with **no new
test functions**, so no failing test there is mine. Reported honestly rather
than papered over.
