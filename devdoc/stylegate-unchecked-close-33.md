<!-- doc-class: historical -->
# Task Report: stylegate `unchecked-close` 33 vs ceiling 32 — the 33rd site named and resolved

Branch: `fix/stylegate-unchecked-close-33` · base: master `fa20029` (the failure was
reported on `2cb44bc`; `fa20029` is the same tree for this rule)

## Completed

- **Reproduced** the red deterministically on master: `CGO_ENABLED=0 go test
  ./internal/stylegate/ -count=1 -run TestStyleGate -v` → `33 test hit(s),
  ceiling 32` on 3/3 runs (so, unlike the sweep's single observation, this is not
  a flake — the sweep's `33 vs 32` in `devdoc/style-sweep-followup.md` §3 and
  this failure are the same event).
- **Identified the 33rd site by measurement, not by guessing.** The sweep's own
  merged tip `5479b35` is the last revision where the count was 32 and its
  `reasons/unchecked-close.json` + 361-site `-annotate` pass is the 32-site
  annotation set. Scanned both trees with the CLI and diffed:

  ```
  git worktree add /tmp/sg32 --detach 5479b35
  (cd /tmp/sg32 && go run ./internal/stylegate/cmd/stylegate -rule unchecked-close -list) > A
  (cd master  && go run ./internal/stylegate/cmd/stylegate -rule unchecked-close -list) > B
  diff A B   →   3a4
                 > cmd/agent/cli_web_align_test.go:542:
  ```

  **The 33rd site is `cmd/agent/cli_web_align_test.go:542` (`resp.Body.Close()`),
  introduced by `f6694fc`** ("fix(test): stop the web-signal test from logging
  into the real /home/user/.gopi", 2026-09-11 09:47:48 +0800). The sweep
  integrated master `805b313` at 09:31:47 and merged at 09:41:48 — 6 minutes
  **before** `f6694fc` existed, and it is an ancestor of master, never of
  `5479b35` (`git merge-base --is-ancestor f6694fc master` → yes). So the sweep
  never saw the site; `32` was a correct measurement of an older master.
- **Resolved it with the rule's own documented escape** (the alternative the
  briefing's acceptance criterion 1 allows), at `cmd/agent/cli_web_align_test.go:542`:

  ```go
  body, readErr := io.ReadAll(resp.Body)
  resp.Body.Close() //stylegate:ignore the body was drained to EOF by the read above and its error is checked on the next line; the close error of a fully-read response carries no state
  ```

  Why annotate and not "check the Close error": the very next statement already
  fails the test if the read failed (`t.Fatalf("reading the turn stream failed: %v", readErr)`),
  so the annotation records a fact about the site rather than skipping a check.
  Both candidate mechanical fixes were considered and rejected on correctness
  grounds — see "Not Done" below.
- **Ceiling unchanged at 32** (`gateCeilings[RuleUncheckedClose][true]`), and the
  count is measured back to **32**, so the ratchet has no headroom and needs no
  movement in either direction.

## Not Done

- **The alternative fix (make the Close error a checked error) was rejected, not
  skipped.** Neither variant is correct here:
  - `if err := resp.Body.Close(); err != nil { t.Fatalf(...) }` after a full
    `io.ReadAll` makes the test flaky by construction — for HTTP/1.1 the body
    close on a fully-drained body is a **no-op that returns nil**, but if the
    response is chunked/keep-alive-adjacent and a keep-alive read races, a test
    that asserts nil there fails for a reason unrelated to the behaviour under
    test (session-end on SIGTERM). The site's own read error is already checked,
    which is the error that carries meaning.
  - Folding it into the existing condition (`if readErr != nil || closeErr != nil`)
    would attribute a *transport teardown* failure to "reading the turn stream
    failed", i.e. a misleading diagnosis in exchange for no new coverage.
  The rule's escape is exactly the "documented, reviewable exception" the gate
  text asks for; the reason names the check that already happened.
- **No regression test added for the *annotation*** (there is nothing mechanical
  to assert that the existing suite does not already cover): `internal/stylegate/stylegate_test.go`
  `TestScanFixtureRules` already pins the marker contract (`bareClose`: the bare
  close is a finding, the marker and the deferred close are not), and
  `TestStyleGate` itself is the regression guard for the count — it went red on
  this site and is green now, which is the evidence that would go red again if
  the annotation were dropped.
- **`TestHippocampusDocAnchorsResolve`** and any other master-side red is out of
  scope; the standards gate (Rule 16–21) was re-run and is green on this tree.

## Verification

- Gate suite: `CGO_ENABLED=0 go test ./internal/stylegate/ -count=1` → **ok**
  (`2.126s`); `-run TestStyleGate -v` → **PASS** (previously FAIL).
- Count, before → after (whole repo, same scanner): `unchecked-close 33` →
  **32**; `errorf-verb 9` and `ignored-error 180` unchanged; `defer-close 363`
  and `undecidable 87` unchanged; **TOTAL 222 → 221**. The only delta is the
  annotated site.
- Standards gates untouched: `go test ./internal/standards/ -count=1` → **ok**
  (`3.318s`).
- The touched test itself, both with and without the annotation, passes with a
  real binary: `go build -o bin/gopi ./cmd/agent/` then
  `go test ./cmd/agent/ -count=1 -run TestWebToolsSignalShutdownWritesSessionEnd -v`
  → **PASS (1.27s)**. (Without the built binary the test self-skips —
  `bin/gopi not built — run the live verification separately` — which is why the
  binary was built first.)
- `gofmt -l cmd/agent/cli_web_align_test.go` → empty; `go vet ./cmd/agent/` →
  clean; `CGO_ENABLED=0 go build ./...` → OK.
- Diff: **1 file, 1 line** (`git diff --stat`).

## The general lesson (why this recurred)

A count ceiling has no headroom by construction (already recorded in
`devdoc/style-sweep-followup.md` §3): on a rule whose ceiling equals its current
count, *any* new site merged from master is a red build. `5479b35` merged master
`805b313`; `f6694fc` landed on master 6 minutes later and its new
`resp.Body.Close()` was one site over an already-saturated ceiling. The
self-diagnosing site list added in `ae683ec` is what made this a 2-minute
identification instead of the "one unreproduced run" it was last time — the fix
for the *process* is that the site list exists, and the fix for the *instance*
is this annotation.
