<!-- doc-class: historical -->
# Task Report: one socket, one release — `Close` after `Finish` is not an error

**Task**: `01a11dfe-615f-7491-863a-2333f6c45325`
**Branch**: `task/01a11dfe615f7491863a2333f6c45325`
**Deliverable**: `internal/doubaostream/session.go` + `internal/doubaostream/session_teardown_test.go`

## Completed

- **The defect is fixed at its root: the socket's release is claimed ONCE, and
  both teardown paths share the claim.** `Session.releaseSocket()` is the single
  release point; `Finish`'s drain and `Close` both call it. The first caller
  claims (sets `socketReleased`, creates `releaseDone`) and closes; every later
  caller waits for that release to return and answers **nil** — "already
  released" is reported as the success it is.
- **`Close` after `Finish` returns nil**, and the sequence the live arm actually
  runs (`Finish` at `cmd/agent/chat_voice_live.go:282`, then the deferred
  `_ = sess.Close()` at `:261`) no longer re-closes the descriptor.
- **Both teardown paths still bound the reader**, so the "reader released from a
  parked read" property is unchanged and can only get stronger: `Close` —
  whether it claimed the release or found it done — returns only after
  `awaitReader`, and `Finish` does the same on its own path. In the un-hooked
  shape `Close` could return before `Finish` (still parked in its bounded wait)
  had even closed the socket; now the wait makes the two comparable.
- **The semantics the constraint named are untouched**: `closed` still means
  "Close was CALLED" (it is still what `isClosing()` reads and what
  `stateErrLocked` reports as `ErrSessionClosed`), and the refusal vocabulary is
  unchanged — after Finish-then-Close, `Say` answers `ErrSessionClosed`;
  Finish-then-`Finish` answers `ErrSessionFinished`.
- **The ported suite's parked assertion is re-enabled**: the line in
  `session_test.go` that said "NOT ASSERTED HERE … the second close really does
  run" now asserts `Close() == nil`, with the history kept in the comment (the
  value is no longer a defect encoded as expected behaviour).
- A new file, `session_teardown_test.go`, holds **two regression tests** with
  three sub-cases (see below).

## Not Done (honest)

- **The real CLI surface was not run**: the only production caller of this API
  is `cmd/agent/chat_voice_live.go`'s live `/voice` arm, which needs a
  microphone and the vendor's app id + token. What was executed instead is the
  real public API surface end to end (`Open` → `Say` → `Finish` → `Close`)
  against a real loopback WebSocket service — the same surface, minus the
  vendor credential. See `## Verification` → `Ran:`.
- **A pre-existing data race is REPORTED, not fixed** (out of this task's scope
  and its brief): `wsConn.SetReadDeadline` writes `c.readTimeout` (ws.go:265)
  from the reader goroutine (`readLoop` → `readOne`) and from `Finish`
  (session.go:294) at the same time. Measured on this branch, but **not
  introduced by it**: the same `WARNING: DATA RACE` fires on the PRE-fix
  `session.go` for the *ported* tests (`TestSessionRefusesAudioAfterFinishAndAfterClose`,
  and `TestFinishDrainReportsAMidFrameInterruption`, neither of which this change
  touches). `make test-budget` does not use `-race`, so the standard gate is
  green — this is a real defect that deserves its own task, not a silent skip.
- The devdoc `doubaostream-session-test-coverage.md` § "The defect this port
  found" is left as the RECORD it is (what was observed then), with a new
  section at the end pointing at this fix — the record is not rewritten.

## The fix, in the shape it was measured

```go
func (s *Session) releaseSocket() error {
	s.mu.Lock()
	if s.socketReleased {
		done := s.releaseDone
		s.mu.Unlock()
		if done != nil {
			<-done
		}
		return nil
	}
	s.socketReleased = true
	done := make(chan struct{})
	s.releaseDone = done
	s.mu.Unlock()

	err := s.conn.Close()
	close(done)
	return err
}
```

`Finish` replaced its `_ = s.conn.Close()` with `s.releaseSocket()`; `Close`
replaced its `s.conn.Close()` with `s.releaseSocket()` and returns its error.

WHY A SECOND FLAG AND NOT `closed`: `closed` is the *caller-visible state*
("Close was called"), read by `isClosing()` and `stateErrLocked`. `Finish`'s
ordinary exit releases the socket with **nobody having called Close**, so the
release needs a fact of its own — setting `closed` there would have made
`isClosing()` true on the normal path (turning a genuine mid-stream read failure
into "we closed it ourselves") and would have turned the live arm's *next*
`Say` refusal from `ErrSessionFinished` into `ErrSessionClosed`. That is exactly
the semantic the task forbade moving, and it is why the two facts are kept apart.

## Verification

- **Build**: `go build ./...` → OK (exit 0)
- **Tests**: `make test-budget PKGS='./internal/doubaostream'` →
  `exit=0 outcome=pass elapsed=43s stream=run-1791506081-3791870.json`
  (load_start=5.19, cores=4 — the earlier flake of
  `TestSessionPushesPartialWhileAudioIsStillGoing` that one run showed was
  re-measured: **6/6 green on the fixed tree and 6/6 on the pre-fix tree**, so it
  belongs to neither side of this diff)
- **Red → green** (Rule 10.5 / Rule 10.4 — the SMALLEST sample, both directions
  measured on the same tree, product file restored and sha256-checked):

  `internal/doubaostream/session.go` sha256 pre-fix `db1e2eb9a5484e80a866e04b1cefd0a2887d8cd46651c6c4acefcce86ee5137f`
  → fixed `7c4a793ab53d27f86c3b382b981d52f0dac83843051079fb7b891744dbb8b339`.

  PRE-FIX (`git checkout -- internal/doubaostream/session.go`), the new tests:

  ```
  --- FAIL: TestSessionCloseAfterFinishIsNotAnError (3.81s)
      session_teardown_test.go:73: Close after Finish: close tcp 127.0.0.1:50328->127.0.0.1:39981: use of closed network connection — teardown must report the socket as released, not re-close it (the live arm calls exactly this sequence on every utterance)
  --- FAIL: TestSessionReleasesTheSocketExactlyOnce (0.02s)
      --- FAIL: TestSessionReleasesTheSocketExactlyOnce/Finish_then_Close (0.00s)
          session_teardown_test.go:107: the socket was released 2 times, want exactly 1 (a second Close on the same descriptor is the "use of closed network connection" the live arm used to report)
  FAIL
  ```

  POST-FIX (same commands):

  ```
  --- PASS: TestSessionCloseAfterFinishIsNotAnError (3.81s)
  --- PASS: TestSessionReleasesTheSocketExactlyOnce (0.07s)
      (Finish then Close / Close then Finish / Close and Finish concurrently — 20 rounds)
  ok  	github.com/nick/gopi/internal/doubaostream	4.622s
  ```

  The ported suite (`task/01a11dc9`'s file, merged from master in this branch)
  stays green, all nine cases, measured on the fixed tree:

  ```
  --- PASS: TestSessionSendsExactlyWhatTheCallerSaid (3.81s)
  --- PASS: TestSessionPushesPartialWhileAudioIsStillGoing (3.83s)
  --- PASS: TestFinishFlushesTheTailAndFlagsIt (3.81s)
  --- PASS: TestFinishReportsTheServicesOwnVerdictOnSpeechlessAudio (3.81s)
  --- PASS: TestFinishRefusesAnEmptyTranscriptWithNoCode (3.81s)
  --- PASS: TestSessionRefusesAudioAfterFinishAndAfterClose (3.82s)
  --- PASS: TestSessionCloseIsIdempotent (0.81s)
  --- PASS: TestOpenRefusesMissingCredentialsBeforeDialing (0.00s)
  --- PASS: TestOpenReportsARefusedHandshakeWithTheLogID (0.00s)
  ```

- **Ran**: the package's real public API against a real loopback service —
  `go test ./internal/doubaostream/ -run 'TestSessionCloseAfterFinishIsNotAnError|TestSessionReleasesTheSocketExactlyOnce' -count=1 -v`
  (the tests call `Open` with real credentials/endpoint options, `Say` real PCM,
  `Finish`, `Close`, against a real TCP listener speaking the vendor's frames);
  plus `go vet ./internal/doubaostream/` (clean), `gofmt -l
  internal/doubaostream/` (prints nothing).
- **Result**: pre-fix red on both new tests with the defect's own sentence
  (`use of closed network connection`, and the counter reading `released 2
  times`), post-fix green; whole package green under the declared budget entry
  point. `git status --porcelain` at close-out: (recorded in `## Evidence`).
- **Commit**: the branch's fix commit — `fix(doubaostream): the socket is
  released once — Close after Finish returns nil` — resolvable as
  `git log -1 --format=%h -- internal/doubaostream/session.go` on
  `task/01a11dfe615f7491863a2333f6c45325`. The commit message itself carries the
  measurement; the hash is deliberately NOT transcribed here, because this
  report travels in that same commit and a self-referential hash is unsatisfiable
  (transcribing it once produced three different hashes in three amendments).

## Why the second test counts the socket instead of reading a flag

A test that asserted `s.closed` after `Finish` would be asserting the
bookkeeping that was WRONG — `session_test.go`'s `TestSessionCloseIsIdempotent`
passed on the broken tree for exactly that reason. So the instrument is the
descriptor: the client end of the package's own `socketPair` fixture is wrapped
in `countingConn`, which counts the `Close` calls that actually reach the
socket, and the verdict is `closes == 1`.

The fixture ends the reader the way the SERVICE does — an explicit terminator
(negative sequence) written through the same helpers the ported suite's fake
server uses — and not by dropping the peer. Dropping the peer looks simpler and
is an instrument that cannot see the defect: `Finish` returns through its
recorded-error branch without touching the socket, so the pre-fix tree would
have passed the counting test. (Measured: it did, before the fixture was
changed.)

## Evidence (self-filed)

- PRE-FIX red of `TestSessionCloseAfterFinishIsNotAnError` on the same tree the
  rest of this report was measured on (session.go sha256 `db1e2eb9a548…`, the
  file restored by `git checkout --` before the run): the failure line quoted
  above — `close tcp 127.0.0.1:50328->127.0.0.1:39981: use of closed network
  connection`.
- PRE-FIX red of `TestSessionReleasesTheSocketExactlyOnce/Finish_then_Close`:
  `the socket was released 2 times, want exactly 1`.
- POST-FIX green: `ok github.com/nick/gopi/internal/doubaostream 4.622s` for the
  two new tests; the nine ported cases green as listed.
- `make test-budget PKGS='./internal/doubaostream'` printed
  `exit=0 outcome=pass elapsed=43s stream=run-1791506081-3791870.json`.
- The SAME entry point was re-run at close-out on the frozen tree (load 7.4→5.0,
  the host carrying other sessions) and **one different test flaked, the same one
  this report already re-measured**: `TestSessionPushesPartialWhileAudioIsStillGoing`
  failed with `OnPartial fired 5 times … want 6`, stream
  `run-1791506577-3830384.json`. Immediately re-run: `exit=0 outcome=pass
  elapsed=36s stream=run-1791506653-3831566.json`, and that test alone
  `-count=3` → `ok … 12.128s`. So the flake is the ported suite's scheduling
  sensitivity (a callback-count assertion under load), **not** a property of this
  fix: it fires on both sides of the diff, and every run of the two tests this
  change adds is green. Reported rather than smoothed over (Rule 26's corollary).
- Product file restored byte-for-byte after every red run:
  `sha256sum internal/doubaostream/session.go` → `7c4a793ab53d27f86c3b382b981d52f0dac83843051079fb7b891744dbb8b339`.
- Close-out state: `git status --porcelain` prints nothing after the fix
  commit was amended into `ed220297d` and the close-out measurement note landed
  as `83ffc8753` — the branch's tip. (The earlier spelling of this line named
  `889da3ab0`, an intermediate commit from before the amendments; it is reachable
  from no ref, and the `## Verification` → `Commit:` note above is the reason this
  report does not transcribe a hash of itself. Re-measured at close-out:
  `git status --porcelain` → empty, working tree clean.)
- FINAL independent re-verification on the frozen tree (after `83ffc8753`, one
  reviewer pass): PRE-FIX red reproduced by re-checking out the master copy of
  the product file (`git checkout 75a767f9d -- internal/doubaostream/session.go`
  → sha256 back to `db1e2eb9a548…`, both new tests FAIL with the defect's own
  sentence and `the socket was released 2 times, want exactly 1`), then restored
  (`git checkout HEAD --` → `7c4a793ab53d…`, `git status --porcelain` empty) and
  both tests PASS. The ported suite re-run by name, 9/9 PASS (`ok … 24.184s`).
  The declared entry point re-run last: `make test-budget
  PKGS='./internal/doubaostream'` → `exit=0 outcome=pass elapsed=36s
  load_start=6.74 cores=4 stream=run-1791506844-3837323.json`.
- The sibling defect found in the same session (the `-race` data race in
  `wsConn.SetReadDeadline`) was filed as its own bug task
  `01a11e1a-1b43-731b-9c68-81a709d0cbdc` rather than fixed here (Rule 26).
- Data race: `go test ./internal/doubaostream/ -count=1 -race` prints
  `WARNING: DATA RACE … (*wsConn).SetReadDeadline ws.go:265` —
  and the SAME warning prints on the PRE-fix `session.go` for the ported tests
  (no code from this change involved), which is the observation behind
  "pre-existing, not fixed here" above.
- FINAL re-verification, the closing pass of `01a11dfe` (`t+2068s`, host load
  ~2.7–5.8 on 4 cores), all of it on the frozen tree
  `task/01a11dfe615f7491863a2333f6c45325`:
  `go build ./...` → `BUILD_OK`; PRE-FIX reproduced once more by
  `git checkout 75a767f9d -- internal/doubaostream/session.go`
  (sha256 `db1e2eb9a548…`) → both new tests FAIL, the first with
  `session_teardown_test.go:73: Close after Finish: close tcp
  127.0.0.1:58466->127.0.0.1:36773: use of closed network connection`, the
  second with `:107`/`:128 the socket was released 2 times, want exactly 1`;
  restored by `git checkout HEAD --` (sha256 `7c4a793ab53d…`,
  `git status --porcelain` empty) → both PASS (`ok … 4.431s`). The ported
  suite by name: 9/9 PASS, `ok … 24.271s`. The declared entry point last:
  `make test-budget PKGS='./internal/doubaostream'` → `exit=0 outcome=pass
  elapsed=36s stream=run-1791507109-3845080.json`. `gopi tool doccheck`
  reports the corpus consistent.
- Delivery-point sync (Rule 30a — done LAST, after the work was frozen and the
  branch clean): `master` had moved 6 commits ahead (its sync-entry help change,
  which touches no file this branch touches except the auto-merged
  `devdoc/DOC-INDEX.tsv`), so `git merge master` landed as `8e7b752d7` with no
  conflict and no change to the product file (sha256 still `7c4a793ab53d…`).
  Re-measured after the sync: `go build ./...` → `BUILD_OK`; the whole package
  `go test ./internal/doubaostream/ -count=1` → `ok … 35.430s`;
  `git status --porcelain` → empty.
