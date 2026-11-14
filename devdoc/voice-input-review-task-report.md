<!-- doc-class: historical -->
# Task Report: review of `gopi voice_input` (cmd/agent/cli_voice.go + cli_voice_server.go)

**Branch:** `review/voiceinput-2` (worktree `~/workspace/gopi-voicereview2`, off master `0244f3d1`)
**Deliverable report:** `devdoc/voice-input-review.md` (in the branch)
**Bug filed:** `01a095c7-36ef-7d71-86be-a7349097d9cf` (tag `bug`, P1) — the P0 listen default + the dead-code finding

---

## Completed

1. **Salvaged all three test files from the dead task `01a091f1-5947`** and gave each a
   reviewed verdict: `cli_voice_parse_test.go` kept unchanged (already complete),
   `cli_voice_review_test.go` adapted + substantially extended,
   `zz_voice_probe_test.go` deleted (a pure `t.Logf` probe with no assertions).
2. **Finished the promises the salvaged review file left as prose**: the `/transcribe`
   HTTP handler end-to-end, `writeVoiceJSON`, the stdio end-to-end, plus the in-process
   units a subprocess cannot reach (`transcribeFile`, `voiceServeStdio`,
   `voiceServeHTTP`, `discardFile`, `userHomeDirOrDefault`). One file, ~1 000 lines,
   44 tests, **0 fail, 0 skip**.
3. **Measured before/after coverage with real numbers** (`go test -cover` +
   `go tool cover -func`, per file AND per function):
   - `cli_voice.go` 45.3% → **71.6%**
   - `cli_voice_server.go` 38.8% → **81.6%**
   - both 41.9% → **76.8%** (+34.9 pp)
   - `voiceServeHTTP` 0.0% → 73.5%, `voiceServeStdio` 0.0% → 86.7%,
     `transcribeFile` 0.0% → 100%, `writeVoiceJSON` 0.0% → 75%,
     `discardFile` 0.0% → 100%, `voiceDepsReady` 25% → 100%.
4. **Checked the named hazards** and reported each with file:line + evidence:
   - **F1 (P0, real defect):** `voiceDefaultListen = ":7788"` binds **every interface**
     with no authentication, while both sibling listeners default to loopback
     (`cli_web.go:386`, `cli_proxy.go:64`). Filed as a bug with repro + acceptance.
   - **F2 (real defect):** `voiceLooksLikeAudioPath` is **dead code** — its only callers
     are tests; the real decision is container magic. Filed with the same bug.
   - **F3:** no `http.MaxBytesReader` at the HTTP boundary (hardening gap, recorded).
   - **F4:** the salvaged file asserted a **false** tool-mirror contract; corrected
     instead of adopted (adopting it would have invented a production requirement).
   - **F5:** `userHomeDirOrDefault` can return a relative dir (`os.TempDir()` verbatim).
   - **F6:** **no resource leak** in either transport — verified by stat-after-run
     assertions (uploaded + materialised temp files both removed).
   - **F7:** **no swallowed errors** (Rule 15b) — every discard is annotated with a
     reason; `transcribeFile` propagates the engine's stderr tail via `%w`.
5. **Reported the honest "could not cover" list**, headlined by a measured runtime
   limit: `runVoiceSubcommand` stays 0.0% because every one of its paths ends in a
   production `os.Exit`, and **the Go runtime does not flush coverage counters on that
   path** (proved: the instrumented binary run as the CLI with `GOCOVERDIR` set prints
   the correct usage, exits 0, and leaves the coverdir EMPTY). Its behaviour is asserted
   end to end instead (exit codes, exact messages, dependency order). Also listed: real
   ASR quality (stub-engine by design), browser clients, and the temp-dir-error branches.
6. **Fixed nothing that wasn't mine to fix** — the only production change is Rule 1
   constant hoisting (`voiceEncoderName`, `voiceLLMName`, `voiceDefaultListenPort`), same
   values, zero behaviour change.
7. Added supporting harness pieces: `testutil.StartGopiBin` (additive; `StartGopi`
   unchanged) and `CoverBin` (a `go build -cover` sibling of the suite binary, with the
   single-owner cache guard extended to cover it).
8. 7 commits, one file at a time (Rule 8), worktree tree clean.

## Not Done

- **Real-engine ASR quality is not tested** — needs ~910 MB of weights and ~70 s/clip.
  The whole feature *around* the engine is verified through the documented
  `--runtime PATH` seam; recognition quality is the upstream model's property. Stated
  in the report rather than silently skipped.
- **`runVoiceSubcommand` remains at 0.0% statement coverage** — a Go runtime limitation
  (see Completed §5), not missing effort. Its behaviour is asserted end-to-end.
- **The F1/F3 fixes are NOT in this branch** — deliberately. F1 is a behaviour change
  (a LAN user relying on the wildcard default would break) that belongs to the filed
  bug, not to a review branch; the review test pins the current value and holds a
  `fixVoiceListenDefault` switch for the fixing commit.
- **`internal/testutil` does not pass** — two repo-wide gates fail
  (`TestWorkerOwnershipPointerPresent`, `TestScriptsAreBoundedAndOffTmp`) on files from
  other agents' concurrent commits. Confirmed **pre-existing** by reproducing the
  identical failure in the untouched main repo at `543803ab`. My diff there is +18/-1
  with no new test functions, so nothing failing is mine.
- The salvaged `zz_voice_probe_test.go` is deleted rather than converted — its only
  real content is asserted properly by the new tests; the reason is in the report.

## Verification

```
CGO_ENABLED=0 go build ./...                       # exit 0  (BUILD_OK)
gofmt -l cmd/agent/ internal/testutil/             # empty
go vet ./cmd/agent/ ./internal/testutil/           # clean
go test ./cmd/agent/ -run 'TestVoice|TestTranscribeFile|TestWriteVoiceJSON|
    TestDiscardFile|TestUserHomeDir' -count=1      # ok — 44 PASS, 0 FAIL, 0 SKIP
go test ./cmd/agent/ -count=1                      # ok  github.com/nick/gopi/cmd/agent  109.143s
go test ./cmd/agent/ -run '…' -count=1 -cover -coverprofile=…   # profile captured
git status --porcelain                             # empty (clean tree)
```
`internal/testutil`: FAIL from the two pre-existing repo-wide gates above — reproduced
identically in the untouched main repo, not attributable to this branch.

## Deliverable declaration (Rule 6d)

- **Branch:** `review/voiceinput-2` (in the shared repo `/home/user/work/GoPi` — this
  worktree shares the object store and refs, so **no push was needed**).
- **Recovery:** `git show review/voiceinput-2:devdoc/voice-input-review.md` and
  `git show review/voiceinput-2:cmd/agent/cli_voice_review_test.go`.
- **Commits:** `c3f45642`, `bf578642`, `ac138912`, `2a5d4980`, `50b528ec`, `42158cb8`,
  `6d6b7e88`, `104efaab` (head) on top of master `0244f3d1`.
- Merge is **NOT** performed by me (Rule 6e); `scripts/safe-merge.sh review/voiceinput-2`
  currently refuses because my worktree still holds the branch — expected before
  acceptance and cleanup.
