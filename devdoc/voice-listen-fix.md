<!-- doc-class: normative -->
# Task Report: voice_input listen default → loopback + upload body cap (P0/F1, F2, F3)

**Branch:** `fix/voice-listen-default` (worktree `~/workspace/gopi-voice-listen-fix`, off master `d79e1982`).
**Closes:** `devdoc/voice-input-review.md` §4 **F1** (P0), **F2** (dead code), **F3** (missing upload cap).
**Recovery:** `git show fix/voice-listen-default:devdoc/voice-listen-fix.md`

---

## Acceptance mapping (epic → evidence, each re-derived on the final tree)

| # | Acceptance line | Where it is proven |
|---|---|---|
| 1 | `grep -n 'voiceDefaultListen' cmd/agent/cli_voice.go` shows `127.0.0.1:7788` | line 58: `voiceDefaultListen = "127.0.0.1:7788"` |
| 2 | a real bind probe asserts `IP.IsLoopback()`, and FAILS on the old `":7788"` | `TestVoiceDefaultListenBindsLoopbackOnly`; red-proof: revert the const → `FAIL … bound :: — NOT loopback` |
| 3 | `TestVoiceHTTPListenDefaultShape` green with `fixVoiceListenDefault=true` | `-run TestVoiceHTTPListenDefaultShape` → PASS (`fixVoiceListenDefault = true`, line 946) |
| 4 | usage text documents the explicit LAN opt-in (`--listen :7788`) | `cli_voice.go` usage lines 42/56/96/117; also printed by the frozen binary's `voice_input -h` |
| 5 | `MaxBytesReader` on `/transcribe` + an over-limit body rejected before spooling | `cli_voice_server.go:111-124` (declared-length guard) + `:286` (`MaxBytesReader`); `TestVoiceTranscribeRejectsOverLimitUpload` (zero-read counter, 4 KiB cap probe) + `TestVoiceMaxUploadBytesIsTheDocumentedBudget`; red-proofs in the Verification section |
| 6 | `voiceLooksLikeAudioPath` + its test deleted, `grep -rn` empty | `grep -rn voiceLooksLikeAudioPath --include=*.go .` → 0 hits; deletion diff in `git diff d79e1982..HEAD -- cmd/agent/cli_voice_test.go` |
| 7 | `CGO_ENABLED=0 go build` OK + `go test ./cmd/agent/` PASS | Verification section (build OK; package suite green) |
| 8 | devdoc report + memory store + branch named | this file; memory records #2160/#2158/#2166/#2167; `fix/voice-listen-default` |

---

## Completed

### F1 (P0) — the default listen address is now loopback

- `cmd/agent/cli_voice.go`: `voiceDefaultListen = "127.0.0.1:7788"` (was `":7788"`, an
  **empty host** that `net.Listen` resolves to `0.0.0.0` + `::`). The constant now carries the
  reasoning (unauthenticated upload endpoint; the wildcard default is the finding; LAN is an
  explicit opt-in) so the decision is not re-litigated from the diff alone. Matches the sibling
  convention (`cli_web.go webDefaultAddrValue`, `cli_proxy.go parseProxyArgs` — `127.0.0.1:8090`).
- `cmd/agent/cli_voice.go` — **usage text** (`voiceInputUsage`) now documents the posture and the
  LAN opt-in explicitly:
  - `gopi voice_input --server` → "HTTP server on 127.0.0.1:7788 (loopback only) … Any client on
    THIS machine can talk to it."
  - `gopi voice_input --server --listen :7788` → "Same server, bound to every interface — the
    explicit LAN opt-in, for the 'server on the big machine, client on the laptop' case. The
    endpoint has NO authentication and accepts audio uploads of up to 512 MiB per request …
    Unreachable from other hosts unless this is passed."
  - `--listen ADDR` flag text: "Default 127.0.0.1:7788 (loopback). Pass "--listen :7788" to serve
    the LAN, or an explicit address to pick one interface."
- `cmd/agent/cli_voice_server.go`: new `voiceListenIsWildcard(addr)` predicate (`net.SplitHostPort`;
  empty host / `0.0.0.0` / `::` / `[::]` ⇒ wildcard; an unparseable address is also treated as
  unsafe) drives a **startup warning** on the LAN opt-in, and the banner is now the pure function
  `voiceListenBanner(listen, chunk)` so the warning is assertable without binding a wildcard.

**LAN serving stays possible** — verified live, not just declared (below).

### F2 — dead code removed

`voiceLooksLikeAudioPath` (and `TestVoiceLooksLikeAudioPath`) deleted: its only callers were tests;
the production path decides by container magic (`voiceContainerMagic`). `grep -rn
voiceLooksLikeAudioPath --include=*.go` over the tree is empty — the only remaining hits are the
prose in `devdoc/voice-input-review.md` (the finding itself, kept as the record) and an untracked
`salvage/voicefix/tracked.diff` artifact.

### F3 — a request-body cap applied before the multipart parse

The review's gap was real and worse than "no `MaxBytesReader`": `r.FormFile` parses with a
**zero** `maxMemory`, which routes every part through multipart's own temp-file spool — unbounded,
client-controlled, and (because a returned `*os.File` part is already unlinked) unusable for the
engine-by-path contract. The handler now:

1. refuses a declared `ContentLength > voiceMaxUploadBytes` outright (413) — no byte read;
2. otherwise reads through `http.MaxBytesReader` and spools the part into a file **this process
   owns** (`voiceSpoolAudioPart`), so the cap is on the read itself and the temp file survives to
   be handed to the engine;
3. classifies an over-limit read as 413 (`voiceUploadTooLarge`: `errors.As(*http.MaxBytesError)`,
   with the stdlib's "request body too large" wording as the fallback) instead of reporting it as
   a malformed request (400) — the misclassification the red-proof B1 below demonstrates.

Named constants (Rule 1): `voiceMaxUploadBytes = voiceMaxAudioBytes + voiceMultipartFramingSlack`
(the audio budget plus the multipart envelope a client wraps around the part).

### Tests (`cmd/agent/cli_voice_review_test.go`)

| Test | What it proves |
|---|---|
| `TestVoiceHTTPListenDefaultShape` (restructured, `fixVoiceListenDefault = true`) | Port pin, concrete **loopback** host, "not loopback ⇒ regression", the sibling convention. |
| `TestVoiceDefaultListenBindsLoopbackOnly` (new) | **Real bind** of the default: bound IP `IsLoopback()`; a wildcard `:<port>` bind is refused (EADDRINUSE ⇒ the loopback socket owns the wildcard space); every non-loopback IPv4 on this host refuses a connection. |
| `TestVoiceListenIsWildcard` (new) | The predicate: `:7788`, `0.0.0.0:7788`, `[::]:7788`, `::7788`, unparseable ⇒ unsafe; `127.0.0.1:7788`, `[::1]:7788`, `192.0.2.6:7788`, `localhost:7788` ⇒ safe. |
| `TestVoiceListenBannerWarnsOnWildcard` (new) | Loopback default is quiet; the LAN opt-in warns, names "EVERY interface", the missing authentication, and the way back to loopback. |
| `TestVoiceTranscribeRejectsOverLimitUpload` (new) | Declared over-limit ⇒ 413 with **zero bytes read** from the body (a counting `io.Reader` proves the guard decided from the declaration) and nothing spooled; an undeclared-length over-limit body is stopped by the reader cap (`voiceSpoolAudioPart` at a 4 KiB limit — minimal sample) and classified as over-limit; an under-cap body still transcribes end to end. |
| `TestVoiceMaxUploadBytesIsTheDocumentedBudget` (new) | The cap arithmetic cannot drift from the two constants it is built from. |
| `TestVoiceLooksLikeAudioPath` | **DELETED** with the helper (F2). |

---

## Verification (re-derived by the verification session on the FINAL tree)

Everything below was RE-RUN on the branch tip after the last edit (the verification
session re-derived each acceptance line rather than trusting this report): `git status
--porcelain` empty, `CGO_ENABLED=0 go build ./cmd/agent/` OK, `go test ./cmd/agent/`
green, and every red-proof reverted-and-restored below.

**Build** — `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (also `go vet ./cmd/agent/` clean).

**Voice suite** — `go test ./cmd/agent/ -run Voice` → `ok … 14.1s` (includes the pre-existing
subprocess end-to-end tests, which run against the refactored handler).

**Full package** — `go test ./cmd/agent/` → `ok  github.com/nick/gopi/cmd/agent  209.224s`
(re-run by the verification session on the FINAL tree — after all red-proof reverts were restored
and the branch tip was `cd83b46c`; the earlier same-day run on `39ffa706` read `192.722s`. The
voice subset alone is `ok  1.567s`).

**Acceptance grep** — `grep -n 'voiceDefaultListen' cmd/agent/cli_voice.go` shows
`voiceDefaultListen = "127.0.0.1:7788"`; `grep -rn voiceLooksLikeAudioPath --include=*.go` → no hits
(doc prose only, see F2).

**Red-proof (Rule 10.4, each by reverting the fix, then restoring):**

| Reverted | Result |
|---|---|
| `voiceDefaultListen` → `":7788"` | `TestVoiceHTTPListenDefaultShape` FAIL ("binds EVERY interface again"), `TestVoiceDefaultListenBindsLoopbackOnly` FAIL ("the default ":7788" bound :: — NOT loopback"). Re-derived by the verification session on the final tree: same two failures, restored, tree clean. |
| both caps removed | `TestVoiceTranscribeRejectsOverLimitUpload` FAIL ("a 8340-byte body was spooled under a 4096-byte cap — the reader cap did not apply"). |
| declared-length guard only | `TestVoiceTranscribeRejectsOverLimitUpload` FAIL ("declared over-limit upload status = 400, want 413 … missing multipart field") — proves the over-limit body used to be misreported as malformed. Re-derived by the verification session. |
| banner warning disabled | `TestVoiceListenBannerWarnsOnWildcard` FAIL (no warning emitted on a wildcard listen). |
| the counting body in case 1 | With the declared-length guard removed, the body is read (`n > 0`) — the zero-read assertion is load-bearing, not decorative. |

All four revert/restore cycles were re-run by the verification session on the final tree
(`fc88d7c6`): the constant revert failed both listen tests, removing the declared-length guard
turned 413 into 400, removing the reader cap re-spooled an 8340-byte body under a 4096-byte limit,
and disabling the wildcard warning produced a banner with no WARNING — each restored, `git status
--porcelain` empty afterwards.

**Live evidence (the REAL binary, not the test)** —
`bin/gopi voice_input --server &` then `ss -ltn`:

```
LISTEN 0  4096  127.0.0.1:7788  0.0.0.0:*
curl http://127.0.0.1:7788/health → {"chunk":15,"engine":"funasr-nano","ok":true}
curl http://192.0.2.6:7788/health  → connection refused
curl http://192.0.2.11:7788/health → connection refused
```

and the banner for each mode: the default prints the three plain lines; `--listen :7788` prints
the WARNING ("binds EVERY interface … NO authentication … Drop --listen (default 127.0.0.1:7788)").

**Live evidence re-derived by the verification session on the final tree** (the real binary,
`/tmp/gopi-voicebin voice_input --server`, every probe a raw socket or curl):

```
ss -ltn | grep 7788                       → LISTEN 0 4096 127.0.0.1:7788 0.0.0.0:*
curl http://127.0.0.1:7788/health         → {"chunk":15,"engine":"funasr-nano","ok":true}
curl http://192.0.2.6:7788/health       → 000 (refused)
curl http://192.0.2.11:7788/health      → 000 (refused)
curl "http://[240e:…acd6]:7788/health"    → 000 (refused; both host IPv6 addrs probed)
# declared over-limit POST /transcribe, raw socket, Content-Length 600000000 (> 545259520 cap):
HTTP/1.1 413 Request Entity Too Large
{"error":"upload exceeds the 545259520 byte limit for /transcribe"}
# a small well-formed multipart upload (Content-Length declared):
HTTP/1.1 500 … {"error":"engine failed: exit status 1 — audio: failed to open/decode
        /tmp/gopi-voice-893459656.wav …"}   → the body WAS read, spooled to a file the
        handler owns under the cap, and handed to the engine: the 413 above is a decision,
        not an empty-body parse error.
# --listen :7788 opt-in:
ss -ltn | grep 7788                       → LISTEN 0 4096 *:7788 *:*   (LAN serving stays possible)
```

**Merge cleanliness** — master advanced past the branch base (`d79e1982` → `bdf0a875`, incl.
`572622d6 fix(help): keep the one-line voice_input index row`) and also touched
`cmd/agent/cli_voice.go`. A scratch worktree at master with `git merge fix/voice-listen-default`
merged with **no conflict** (`git merge-tree` marker count 0, `git status --porcelain` empty), and
the merged tree is green on the listening/help/usage pins
(`go test ./cmd/agent/ -run 'Voice|Help|Usage|Index|LineCount|Width' -count=1` → `ok 28.618s`).

---

## Not Done (honest)

- **`go test ./internal/standards/` fails on this branch — PRE-EXISTING on master, not introduced
  here.** `TestGate2TestEnvRedProof` reports a Rule 17 NEW unannotated `os.Unsetenv` at
  `cmd/agent/cli_voice_review_test.go` (line 1301 at master `bdf0a875`, line 1598 on this branch —
  this branch only adds lines ABOVE it). The site is `TestUserHomeDirOrDefaultFallsBack`, which
  deliberately unsets `HOME` and restores it through `testutil.SnapshotEnv` (the reviewed
  presence-preserving form), i.e. the gate's predicate is narrower than its own documented
  rationale. Verified as pre-existing by running the gate in a scratch worktree at master: same
  violation, same three escapes (`internal/agent/crashdump.go recoverRunPanic`,
  `internal/shot/x11.go getRootImage`). Master already records it and filed the bug
  (`tasks/01a0959b-cf65-report.md`, commit `bdf0a875`: bug task
  `01a0960d-9edf-7ed1-848c-739e47d6aa48`). Out of this epic's scope and deliberately untouched: the
  fix belongs to the filed bug (either an `// env-leak-ok:` escape or widening the predicate), not
  to a listen-address change.

- **No authentication on `/transcribe`.** Out of scope for this task (the review files it as a
  posture note, not a finding): the fix is to stop being reachable by default plus an explicit,
  warned LAN opt-in. A token/`--auth` flag would be the next step if the endpoint is ever exposed
  beyond a trusted LAN.
- **No body/total request timeout.** Only `ReadHeaderTimeout` is set. A slow-body client can still
  hold a connection (the cap bounds the bytes, not the time). Not filed by the review; recorded
  here as the next hardening item.
- **`userHomeDirOrDefault` relative-`TMPDIR` robustness (review F5)** — untouched, as the review
  deliberately did not file it (its branch is asserted by a test, not fixed).
- **`cli_web.go:435` still spells the same wildcard-host set inline** (`host != "" && host !=
  "0.0.0.0" && host != "::"`) while this change named the equivalent set inside
  `voiceListenIsWildcard`. If a third site appears, lift the predicate into one package-level
  helper (Rule 14) — deliberately not done here, since re-pointing the web banner's host check is
  a behaviour-adjacent edit outside this task's findings and would need its own red-proof.
- **The 512 MiB audio budget is unchanged** (a large WAV is legitimate for a 220 s+ clip);
  `voiceMaxAudioBytes` remains the read budget for both transports.
