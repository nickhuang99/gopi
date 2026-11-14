<!-- doc-class: normative -->
# Task Report: composition verification of the worker's worktree-cwd release

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Epic: **close the COMPOSITION gap** — prove, against the REAL `gopi worker`
binary, that a round which removes the linked worktree it is standing in ends
with the process OUT of that directory (not pinning the deleted inode).

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md**
>
> The `gopi worker` / `gopi task run` strings below name the SUBJECT of the
> composition probe (the entry point the fixture drives) and the wrapper whose
> tail releases the cwd; they are rows of the verification, not instructions —
> only the operator launches a worker.

Master already carried the fix (a2ccb31f): the ONE leaf action
`internal/worktreerelease.Release()` → `os.Chdir("/")`, reached from the
worker round's close-out (`workerRoundOwnWorktrees`) and from the `gopi task
run` wrapper's tail (`taskRunReleaseOwnWorktrees`). Two guards existed and
neither could see the composition:

- `internal/standards/cwd_release_gate_test.go` — a STATIC go/ast gate: it
  proves the release CALL is present in a worktree-removing producer's source.
- `cmd/agent/cwd_release_rig_test.go` — a UNIT rig: a re-exec'd holder calls
  `Release()` and the parent removes the tree. It proves the ACTION, not the
  COMPOSITION.

## Completed

- **`cmd/agent/cwd_release_composition_test.go` (new, 502 lines)** —
  `TestWorkerRoundReleasesCwdOfTheWorktreeItRemoves` drives the REAL built
  binary as a `gopi worker` whose **cwd is the fixture repo's linked worktree**
  (`cmd.Dir = wt`), with a submitted row for that repo. The round claims the
  row, enters its session container, runs the executor child to completion, and
  its close-out frees the worktree — while the process stands INSIDE it.
- **The fixture's teeth (measured, not assumed)**: the profile root
  (`GOPI_HOME`) is placed INSIDE the worktree, so the session container the
  round runs in (`<wt>/.gopi/sessions/<taskID>`) is a directory OF the tree the
  round removes. Without that, the first green run measured the process standing
  in the worktree's session dir while the WORKTREE was removed — the worktree
  removal was real but the cwd was outside it, so a DELETED release call would
  still have passed. With the profile inside, the cwd at removal IS a directory
  of the removed tree (readings: `wt → wt/.gopi/sessions/<id> →
  wt/.gopi/sessions/<id> (deleted) → /`).
- **The measurement is procfs, because nothing else discriminates**: measured on
  both tmpfs and xfs, `git worktree remove` unlinks the path unconditionally —
  it never consults /proc — so `git worktree list` is clean and `os.Stat(wt)`
  returns ENOENT in BOTH the pinned and the released case. Only
  `/proc/<pid>/cwd` renders the deleted inode (`"<path> (deleted)"`). The test
  therefore asserts on the holder's cwd (`cwdReleaseHolderCwd`, the package's
  ONE procfs reader) and consults NO production diagnostic string.
- **Assertions**: (i) the process was observed AT the release destination
  (`worktreerelease.ProcessRootPath`); (ii) the cwd read back after the round is
  no longer inside the removed tree (the persisting pin = the defect); (iii) the
  worktree is gone in both senses (not git-registered, path absent); (iv) the
  deliverable branch survived; (v) the round really ran (the claim line). The
  transient pinned reading is LOGGED as evidence that the fixture exercised the
  composition, never failed on.
- **Fixture hygiene**: the worktree's `.gitignore` carries the profile dir (else
  `?? .gopi/` makes the tree dirty and finish-cleanup refuses removal — measured
  on the first green run), committed before the profile exists; the worker's
  loop knob (`GOPI_WORKER_IDLE_EXITS=0`) keeps the process alive so its /proc
  entry is observable at rest; the scratch GC throttle is held; the child's temp
  root is pinned to a MEASURED-clean place (Rule 22: devices measured, 28 vs `/`
  2051).

## Not Done (honest)

- **The release call that unpins THIS shape is shared, not worker-only.** The
  red proof below required neutering BOTH call sites: the worker round's own
  (`cli_tasks_worker.go:183`) AND `taskRunReleaseOwnWorktrees`'s
  (`cli_tasks.go:1874`), which the round reaches through `runTaskRunOnce`'s
  close-out. Neutering the worker-side call alone still left the process at `/`
  (measured — the first red attempt PASSED), so this fixture is red iff the
  release as a WHOLE is absent from the round's close-out; it does not
  discriminate "the worker's own call ran" from "the wrapper tail's call ran".
  That is honest coverage of the composition, not of one call site.
- **The round's workdir restore reports the gone origin** (`gopi task: workdir
  origin <wt> is gone — staying in the current directory`) for this shape, by
  construction: the fixture puts the round's ORIGIN inside the very worktree the
  round removes. On that path the release is the ONLY thing that unpins the
  process — which is exactly the composition under test, so the line is logged,
  not failed. What a long-lived process does when its origin is removed and NO
  release follows is a pre-existing behaviour of `task_session_cwd.go`
  (`origin == "" || liveWorkdir(origin) == ""` → refuse), unchanged here and not
  covered by a new test (dispatchable as its own item if wanted).
- No inode-identity probe is shipped beside the /proc reading: the earlier draft
  had one and it was removed as unfalsifiable (see the file's header note) —
  the filesystem state is identical in both shapes, so a second probe would look
  stronger while discriminating nothing.
- Rule 6e cleanup (worktree removal) is deliberately NOT done here: the branch
  is the deliverable, and merge is handled separately after review.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — **OK** (also
  `go vet ./cmd/agent/` clean, `gofmt -l` clean).
- GREEN (tip, quoted):
  ```
  $ go test -count=1 -run 'TestWorkerRoundReleasesCwdOfTheWorktreeItRemoves' -v ./cmd/agent/
      cwd_release_composition_test.go:428: Rule 22 measured: fixture temp root /dev/shm/gopi-test-scratch/suite-cmd-agent-run-2543344663/TestWorkerRoundReleasesCwdOfTheWorktreeItRemoves3472415344/001 device=28, / device=2051
      cwd_release_composition_test.go:463: pin observed: 5 reading(s) inside a REMOVED directory, e.g. ".../001/wt/.gopi/sessions/t-cwd-release-composition (deleted)" — the composition is live (the release is what ends this state)
      cwd_release_composition_test.go:500: composition observed: readings=.../001/wt → .../001/wt/.gopi/sessions/t-cwd-release-composition → .../001/wt/.gopi/sessions/t-cwd-release-composition (deleted) → /; session-inside=true; pinned=5; released=true; exited=false
  --- PASS: TestWorkerRoundReleasesCwdOfTheWorktreeItRemoves (6.23s)
  ok  	github.com/nick/gopi/cmd/agent	10.085s
  ```
- RED (both release calls neutered with `_ = worktreerelease.ReleaseReporting`,
  rebuilt, re-run, restored, quoted):
  ```
  cwd_release_composition_test.go:463: pin observed: 234 reading(s) inside a REMOVED directory, e.g. ".../001/wt/.gopi/sessions/t-cwd-release-composition (deleted)"
  cwd_release_composition_test.go:472: worker pid 1220795 was never observed at / (composition=readings=.../001/wt → .../sessions/t-cwd-release-composition → .../sessions/t-cwd-release-composition (deleted); session-inside=true; pinned=234; released=false; exited=false) — the round's cwd release did not run, so the process is left standing in the directory its own close-out removed
  cwd_release_composition_test.go:476: worker pid 1220795 still stands inside the REMOVED worktree (".../sessions/t-cwd-release-composition (deleted)") after its round — the deleted inode (and every file under it) stays pinned, which is the defect this composition exists to stop
  --- FAIL: TestWorkerRoundReleasesCwdOfTheWorktreeItRemoves (23.20s)
  FAIL	github.com/nick/gopi/cmd/agent	27.653s
  ```
  (First attempt, worker-side call alone neutered: PASSED — evidence that the
  wrapper tail's call also releases; recorded in Not Done.)
- Guards stay green: `go test -count=1 ./internal/standards/` → **ok
  47.553s**; `go test -count=1 -run 'CwdRelease|Release' ./cmd/agent/` → **ok
  94.390s**.
- No leftover fixture state: no `tmp.*` residue under the scratch queue; the two
  driver source files are byte-identical to the tip (`git status --short` lists
  ONLY the new test file).
- Commit: `<filled at commit time>` — `test(cmd/agent): composition test for the
  worker's worktree-cwd release`.
