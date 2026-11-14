<!-- doc-class: historical -->
<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

# Task Report: `gopi shot --url <unreachable>` — a failed navigation is visible on the control channel

**Task:** EUAUDIT slice4 TOOLS-4 (F2+F4)
**Branch:** `task/01a0dde8-8572-7eda-8f82-94ed020d3838`
**Base:** `933c9a8c` (master)

## Completed

### 1. The defect, restated as a measurement

`gopi shot --url file:///definitely/not/here.html` **exited 0** and wrote a PNG of
Chromium's error page. The only channel distinguishing success from failure was
the pixels. Root fact: `captureOverCDP` **discarded the result of
`Page.navigate`** — which carries CDP's `errorText` — and then byte-checked only
the PNG magic, so an error document and the requested page were identical on
rc / stdout / PNG-shape.

Measured with the pre-fix binary (`bin/gopi-red`, built from `HEAD` before any
edit):

| | rc | stdout | stderr | PNG sha256 |
|---|---|---|---|---|
| `--url file:///definitely/not/here.html` | **0** | the PNG path | *(empty)* | `ac685916…` |
| `--url file://<existing>.html` | 0 | the PNG path | *(empty)* | `4ccce013…` |

### 2. `internal/shot` — navigation-failure detection

`NavigationFailure` (typed error: `URL`, `Reason`, **`PNG`**) is returned when the
navigation produced an error document. Detection has two measured probes:

- **`Page.navigate`'s own `errorText`, read from the FIRST call.** It is the free
  half: no extra round trip. Measured answer for an unreachable `file://` URL:
  `net::ERR_FILE_NOT_FOUND`, with **no CDP error at all** — which is exactly why
  discarding the result hid this failure class.
- **A post-navigation error-document check** (`location.protocol` via
  `Runtime.evaluate`), catching an error page that appeared but never reached the
  navigate result.

The error page is still **captured**: `captureErrorPage` takes the PNG and folds
Chromium's own wording into the failure, so the pixel evidence survives.

### 3. `cmd/agent/cli_shot.go` — the chosen contract

- Failed navigation → **rc=1**, one stderr line naming the URL and the error-page
  fact; the PNG is **written anyway** and its path named on stderr; **stdout
  carries no bare path**.
- Success → unchanged: rc=0, the path on stdout.
- `shotUsage` states the contract.
- `reportShotFailure` (new, testable without `os.Exit`) + `writeShotPNG` (ONE
  writer shared by both exit paths).

### 4. Tests

- `TestCaptureOverCDPNavigationFailureIsTyped` — fake-CDP, **both failure shapes**:
  (A) `errorText` present while the DOM probe sees a healthy document, (B) no
  `errorText` and only the error document reveals it. Asserts the **contract**
  (`errors.As` → `*NavigationFailure`, the URL named, the reason carrying the
  fact, the PNG still returned) — never "a PNG was written", which passed BEFORE
  the fix and is precisely what hid the defect.
- `TestHealthyPageCostsOneNavigationAndOneProbe` — the cost contract: one
  navigate, one probe to end the wait.
- `TestCaptureErrorPageDetailIsQuoted`, `TestExpectedNetErrorPrefixIsDerived`,
  `TestNavigationFailureErrorShape`.
- `TestReportShotFailure*` / `TestShotUsageStatesTheExitContract` (cmd/agent) —
  drives the real renderer (`writeShotPNG` + `reportShotFailure`).
- `TestCaptureRealChromeUnreachableURL` (**real-browser leg**, `CriterionDeviceExec`,
  inventoried in `testdata/heavy-tests.tsv` so it stays off the merge/verify path
  per Rule 27): unreachable URL → typed failure carrying the PNG; control page →
  normal capture.

### 5. The probe that changed the design (and corrected two of my own assumptions)

Three throwaway probes against real chromium-1228. Two corrected mistakes that
would have shipped silently:

1. **My first expression was inert.** It read `url.protocol`; in an evaluate
   context `url` is **not a global binding** —
   `ReferenceError: url is not defined`. The probe would have answered nothing
   forever and the detection would have degraded to `errorText` alone while
   *looking* like it worked. Rewritten to read `location.protocol`.
2. **A shadowing defence I had written was unnecessary, and a risk I had
   missed.** Measured on a real page: `Object.defineProperty(window,'location',…)`
   **throws TypeError**; `window.location = {protocol:'chrome-error://…'}` does not
   forge a reading — it makes the browser **navigate** there, so the page really
   becomes an error document. `location` cannot be shadowed into a false report.
   Separately, the naive "hold `Page.navigate`'s result" approach would have
   reported Chromium's `Transaction::SetError` **without the code** (the real
   document carries it) — and the second `Page.navigate` I first designed would
   have **aborted a still-loading navigation**, reporting its own artifact
   (`net::ERR_ABORTED`) as the page's verdict. The repeat navigate was dropped:
   the first result's `errorText` is read instead.
3. **`loaded` is not health.** `performance.getEntriesByType('navigation')[0]` is
   present (true, within 150 ms) on the error document **too** — so `loaded`
   only ends the wait; the **protocol** decides. This is what keeps the ordinary
   healthy capture free: one probe, no waiting.

## Not Done (honest)

- **The real-browser leg is not executed by this run.** `heavytest` refuses the
  heavy set inside a work context (inherited `GOPI_EXECUTOR_SESSION_ID` /
  `GOPI_AGENT_SESSION` / `GOPI_TASK_RUN_ID`), and `GOPI_HUMAN_TERMINAL=1` would be
  a false declaration of a terminal I am not sitting at — so I did not use it. The
  evidence for the same behaviour comes from the **real binary**, which is a
  stronger instrument than the test: the acceptance runs below drive a real
  chromium through the escape path (transient systemd user unit). The leg is
  committed and green-by-construction for
  `GOPI_HUMAN_TERMINAL=1 go test -run TestCaptureRealChromeUnreachableURL -v ./internal/shot/`.
- **`cmd/agent`'s FULL package suite does not finish in one go on this host right
  now.** `go test ./cmd/agent/ -count=1` hit Go's default 10-minute ceiling, and a
  bounded re-run (`-timeout 90s`) named the hanging test:
  `TestTaskListInProgressViewResolvesByLedgerHint` — **ledger/task-list machinery
  this diff does not touch**. Run alone it **passes in 2.6s**
  (`go test ./cmd/agent/ -run TestTaskListInProgressViewResolvesByLedgerHint
  -count=1 -timeout 60s` → `ok … 2.579s`), so the hang is package-level
  contention, not a defect in this change — another agent was running the same
  package concurrently (`ps` showed a second `agent.test` in a sibling worktree).
  What IS green for this change is scoped to the surface it touched, below.
- Two further `cmd/agent` failures in that run, both attributed rather than waved
  off:
  - `TestBareRunKeepsAllItsArtifactsInOneSessionDir` — **caused by this run** (the
    red-proof revert rewrote `internal/shot/shot.go`'s mtime AFTER `bin/gopi` was
    built, and the test refuses to verify a stale binary). **Fixed by rebuilding
    `bin/gopi`**; it is a staleness guard working as designed, not a code fault.
  - `TestChatDump_GridFollowsTheLayoutGeometryOnARealPty` — `lineedit: not a
    terminal` opening the pty slave: an environment failure in a test that needs a
    real terminal, untouched by this diff (no `lineedit`, pty or chat-dump code is
    in it).
- **`--surface chat` / `--surface screen` were not exercised.** They never reach
  `captureOverCDP` (`captureTextSurface` / `captureScreenSurface` are separate
  engines), so this change cannot affect them; that is a by-construction argument,
  not a run — stated as such.
- The **`shot: shot:` double prefix** is visible on the new failure line (and was
  already there: `GOPI_SHOT_CHROME=/nonexistent ./bin/gopi shot …` prints
  `gopi shot: shot: GOPI_SHOT_CHROME=… is not an executable file`). Pre-existing
  shape, deliberately untouched by this surgical change; noting it so a later
  reader does not read it as new.

## Verification

- Build: `go build ./...` → **OK**; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → **OK**;
  `go vet ./internal/shot/ ./cmd/agent/` → clean; `gofmt -l` → clean on every changed file.
- Tests: `go test ./internal/shot/ -count=1 -timeout 600s` → **`ok github.com/nick/gopi/internal/shot 62.402s`** (the FULL
  package, running alone); `go test ./cmd/agent/ -count=1 -run 'Shot|Navigation'` →
  **`ok github.com/nick/gopi/cmd/agent 1.316s`** (the surface this diff touches);
  `go build ./...` → OK; `go vet ./internal/shot/ ./cmd/agent/` → clean;
  `gofmt -l` → clean on every changed file. The whole `cmd/agent` package does not
  finish in one round on this host right now — see *Not Done* for the attribution
  (a hanging ledger test this diff does not touch, green alone in 2.6s, while
  another agent ran the same package concurrently).
- Ran (**the surface the task names, verbatim, against a real chromium**):
  ```
  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/
  # pre-fix binary for the baseline: bin/gopi-red @ 933c9a8c
  ./bin/gopi shot --url file:///definitely/not/here.html --out <EV>/fixed-error.png
  ./bin/gopi shot --url file://<EV>/control-ok.html --out <EV>/fixed-ok.png
  ./bin/gopi shot -h
  ```
- Result (**measured**):
  - Failure: **rc=1**, stdout **empty**, stderr exactly two lines —
    `gopi shot: error-page PNG: <EV>/fixed-error.png` and
    `gopi shot: shot: navigation to file:///definitely/not/here.html failed: net::ERR_FILE_NOT_FOUND: 无法访问您的文件 该文件可能已被移至别处、修改或删除。 ERR_FILE_NOT_FOUND — captured Chromium's error page, not the requested page`
  - Failure PNG sha256 `ac685916aa30e7c601aedfb5017d6ff3f7b4290cba65cb208e7c467526c77662`
    — **byte-identical to the pre-fix baseline**, so no pixel evidence was lost.
  - Control: **rc=0**, path on stdout, stderr empty, sha256
    `4ccce013df6157d395fb2fe75705c8bdb8a956531eb90610e91accc872796d40` —
    **byte-identical between `bin/gopi-red` and `bin/gopi`** on the same URL
    (`cmp` clean), which is the "same PNG bytes as before" acceptance.
- Commit: `4254e30b fix(shot): a failed navigation exits non-zero and names the error page`
  (`git status --porcelain` in this worktree prints nothing).

Re-run of the SAME acceptance against the committed HEAD (binary rebuilt from
`4254e30b`, evidence in `final-error.*` / `final-ok.*`):

| case | rc | stdout | stderr | PNG sha256 |
|---|---|---|---|---|
| `--url file:///definitely/not/here.html` | **1** | *empty* | two lines (PNG path + reason) | `ac685916…` = **baseline** |
| `--url file://<EV>/control-ok.html` | **0** | the PNG path | *empty* | `4ccce013…` = **pre-fix binary** |

## Evidence (self-filed)

Recorded under `/home/user/.gopi/sessions/01a0dde8-8572-7eda-8f82-94ed020d3838/euaudit-tools4/`:

| file | observation |
|---|---|
| `baseline-error.png` / `baseline-error.stdout` / `baseline-error.stderr` | pre-fix: rc=0, path on stdout, **stderr empty**, sha `ac685916…` |
| `fixed-error.png` / `.stdout` / `.stderr` | post-fix: rc=1, stdout empty, two stderr lines, sha `ac685916…` |
| `baseline-ok.png` / `fixed-ok.png` | control captures |
| `ctl-gopi-red.png` / `ctl-gopi.png` | same URL through both binaries: sha `4ccce013…` **both**, `cmp` → identical |
| `baseline-sha256.txt` | the sha256 lines as captured |

Probe outputs (chromium-1228; the probes were deleted before commit, their
conclusions live in the code comments):

- `Page.navigate` result on `file:///definitely/not/here.html`:
  `{"frameId":"…","loaderId":"…","errorText":"net::ERR_FILE_NOT_FOUND","isDownload":false}`, `err=<nil>`
- `location.protocol` on the error page: `chrome-error:` (URL `chrome-error://chromewebdata/`)
- `url.protocol` in an evaluate context: `ReferenceError: url is not defined`
- `Object.defineProperty(window,'location',…)`: `TypeError`
- `window.location = {protocol:'chrome-error://…'}`: assigns, then the page **navigates** there
- the loaded flag: `{"loaded":true,"protocol":"chrome:error…"}` within 150 ms on the **error** page, and `{"loaded":true,"protocol":"file:"}` on a healthy one

Red-then-green proof (the regression test can fail): with BOTH detection signals
temporarily disabled (`navigationErrorText` returning `""`, `errorPageURLPrefix`
renamed to a string that never matches), `go test ./internal/shot/ -run
TestCaptureOverCDPNavigationFailureIsTyped` → **FAIL**, both failure shapes:
`failure returned as <nil> (<nil>), want *NavigationFailure — an error document
must be distinguishable on the control channel, not only in the pixels`. With the
detection restored → **PASS**. The disable was reverted before commit
(`git diff internal/shot/shot.go` still shows only the intended change).
