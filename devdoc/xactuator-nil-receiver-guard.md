<!-- doc-class: historical -->
# Task Report: nil-receiver guard for XActuator (shot)

Bug: `XActuator.Display()` guarded its CLIENT field (`a.c == nil`) but
dereferenced the RECEIVER `a` first — a nil `*XActuator` (exactly what
`OpenXActuator` returns alongside its error) panicked with a SIGSEGV instead
of returning the `""` the same method documents.

## Completed
- `internal/shot/xactuate.go`:
  - `Display()` → `if a == nil || a.c == nil { return "" }` (receiver checked
    BEFORE any field is touched).
  - `RootSize()` → same guard, answers `0, 0` (it had NO guard at all: it
    panicked on both a nil receiver and a zero-value actuator).
  - `Close()` → same guard, now an idempotent no-op instead of a crash on nil
    / on an actuator without a client.
  - No new literals or magic numbers introduced (Rule 1); edits surgical (Rule 4).
- `internal/shot/cov_shot_x11_test.go`:
  - `TestCovX11DisplayReportsConnectedName` extended: the stale comment that
    described the nil case as "filed, not fixed" is replaced by the assertion
    `var nilAct *XActuator; nilAct.Display() == ""`.
  - New `TestCovX11NilActuatorDoesNotPanic`: pins Display / RootSize / Close on
    a nil receiver AND on a zero-value actuator.

## Not Done
- No LLM-adoption scenario (Rule 11) was run: this is an internal nil-guard in
  a library accessor, not a tool/prompt/routing change — the LLM path is
  unchanged (`gopi act <verb>` behaviour is identical when a display opens;
  the guard only changes the failure mode when there is no actuator).
- The sibling no-error accessors on the CLIENT (`Pointer`, `Focus`) still
  dereference `a.c`; they are error-returning probes and were left untouched
  (out of the filed scope — the report's root cause names only the three
  accessors that promise a value).
- No live-desktop verification: this session's `$DISPLAY` is
  `localhost:10.0` with no `/tmp/.X11-unix/X10` socket, so `gopi act info`
  cannot reach a server here (checked: it fails loudly with the dial error,
  i.e. the pre-existing behaviour — not a regression of this change). The
  wire-level fake X server in `cov_shot_x11_test.go` is the verification path.

## Verification
- Build: `go build ./...` OK; `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` OK
- Tests (patched): `go test -run TestCovX11 ./internal/shot/` → ok 0.136s;
  `go test ./internal/shot/` → ok 39.379s
- Bug-catching proof (source fix stashed, tests kept):
  `git stash push -- internal/shot/xactuate.go` →
  `go test -run 'TestCovX11NilActuatorDoesNotPanic|TestCovX11DisplayReportsConnectedName'`
  → **FAIL — `panic: runtime error: invalid memory address or nil pointer dereference`**
  (recovered, `panic.go:792`); `git stash pop` restores the fix. The new
  assertion fails on the pre-fix source and passes after it.
- Commit: `84e93e9 fix(shot): guard the XActuator receiver before dereferencing it`
  on branch `fix/xactuator-nil-receiver` (worktree
  `/home/user/workspace/gopi-xactuator-nil`; a worktree shares the main repo's
  refs, so the commit IS in `/home/user/work/GoPi` — no push needed).
  Recover with: `git show fix/xactuator-nil-receiver:internal/shot/xactuate.go`
  (cite the BRANCH, not the hash: the docs commit was amended once, so only
  84e93e9 — the source-changing commit — is stable.)

## Note for the reviewer
My first read of the main repo was misleading and is corrected here: the
`?? devdoc/...` lines in `/home/user/work/GoPi` are unrelated nightshift/tab-order
scratch docs, and `internal/shot/cov_shot_x11_test.go` is in fact **tracked and
clean on master** (`git ls-files --error-unmatch` succeeds; clean `git status`;
it landed in `ae79d8d test(shot): function-level coverage of the X11/XTEST
verbs + fake X server`, contained in `feat/cov-shot-x11` and now master). No
file in the main repo was modified by this task (the only main-repo entries are
the three pre-existing untracked devdoc paths).
