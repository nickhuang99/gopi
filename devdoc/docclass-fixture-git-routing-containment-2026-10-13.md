<!-- doc-class: historical -->
# The docclass fixture's git children: contain the routing, and witness the containment — 2026-10-13

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

**Status:** one run's record and one repo-state change, on 2026-10-13, on peerbox. This is
the RECORD of a containment landed in `internal/docclass/`; the incident it contains is
narrated in the root-cause row `01a13513-b8d0-7e95-81a8-7ec1137f6307` (still pending) and
in the memory record of the run that measured it. This file adds what that row does not
carry: the containment's exact shape, why the witness is a child process, and what this
run did **not** convert.

## 1. What is contained

`internal/docclass/anchor_number_provenance_test.go::anchorNumberProvenanceRepo` builds a
throwaway repository in `t.TempDir()` with `cmd.Dir = root`. `cmd.Dir` sets the child's
working directory; it does **not** override `GIT_DIR`. Measured in this run (session
`01a13513-2dc0-776c-b253-38d4efa69114`, `repro-gitdir/`):

```
cd <fixture-root>; GIT_DIR=<foreign-repo>/.git git init -q -b main
  → warning: re-init: ignored --initial-branch=main      # it re-initialised the FOREIGN repo
GIT_DIR=<foreign-repo>/.git git rev-parse --show-toplevel
  → <fixture-root>                                       # toplevel follows cwd — the trap
GIT_DIR=<foreign-repo>/.git git add -A && … commit -m "base: the anchored pair is coherent"
  → the FOREIGN repository's HEAD advanced
  → <fixture-root>/.git was never created
```

The containment has two halves, and they answer different questions:

1. **The child environment is BUILT, never inherited** (`anchorNumberGitChildEnv`): the
   parent's environment with `GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`,
   `GIT_OBJECT_DIRECTORY` and `GIT_COMMON_DIR` removed — by exact name, from the one list
   `anchorNumberGitRoutingEnvNames` (Rule 13). They are REMOVED, not set to `""`: `GIT_DIR=`
   is an empty PATH to git, not "unset", and git answers
   `fatal: The empty string is not a valid path` — a loud break instead of isolation.
   `anchorNumberGitCleanEnv` is the identity-free half, for probes that must be unable to
   commit plausibly.
2. **The repository git resolved is asserted to be the fixture's own**
   (`anchorNumberAssertRepoIsOwnRoot`), immediately after `git init`: `rev-parse
   --absolute-git-dir` must be inside `t.TempDir()`. On failure the message names the
   routing variables present in the ambient environment, so a reader is told which
   variable did it instead of being handed a path mismatch to investigate.

## 2. The witness, and why it is a child process

`internal/docclass/anchor_number_fixture_isolation_test.go`:

- `TestAnchorNumberFixtureNeverRoutesGitAtAnotherRepo` builds a **sentinel** repository
  (one commit, branch `main`, author `sentinel`), then re-executes the test binary
  (`os.Args[0] -test.run ^TestAnchorNumberFixtureCHILD$`) twice with `GIT_DIR` pointing at
  the sentinel:
  - **arm (b), the RED witness** — the fixture's three git calls with the strip REMOVED
    (the pre-fix spelling). The sentinel's HEAD moves, its new HEAD is authored
    `fixture@example.invalid`, and its reflog carries the fixture's own two commit
    subjects. This arm is what licenses the green one: a sentinel nobody can reach would
    stay put no matter what the fixture does.
  - **arm (a), the GREEN containment** — the SHIPPED fixture. Exit 0, sentinel HEAD and
    reflog byte-identical to the post-restore baseline, and the fixture's own repository
    built through to `post-freeze: the recorded line moved` (read from the child's own
    report file).
- The red arm's damage is **reset and proved reset** before the green baseline is read
  (HEAD back at the recorded commit, sentinel author restored, reflog expired and checked
  for fixture-authored entries).
- `TestAnchorNumberGitChildEnvStripsEveryRoutingVariable` is the fast in-process arm: it
  poisons the environment with all five names and asserts the strip, naming the variable
  that leaked.

**Why not `t.Setenv` in-process.** The poison is "the PARENT process has `GIT_DIR` set" —
that is the shape that broke master. `t.Setenv` would set it for the whole test binary,
where the fixture's own strip hides it (so nothing is measured) and where a containment
failure would commit into the binary's cwd. So the child carries it, and only the child.

**Why the red arm is a minimal re-implementation rather than the shipped fixture.** The
shipped fixture now refuses to continue the moment it discovers it is not in its own
repository, so running it under the pre-fix environment would end the arm with "the fix
refuses" instead of "the sentinel moved". The call sequence, subjects and identity are
copied verbatim from the incident; the containment is the only thing left out — which is
exactly the delta the witness measures, and why both arms exist.

**Why the child reports instead of the parent walking the tree.** The fixture's repository
lives under a `t.TempDir()`, which the child's exit removes; a parent that walked the
scratch tree afterwards would find nothing and could not tell "the fixture did nothing"
from "the fixture's tree is already cleaned up". The child therefore writes `root/head/
subject/author` into a file in the scratch directory the PARENT owns, and the parent
asserts on it.

## 3. What this run did NOT convert

- **The wider fixture family.** `os.Environ()` + `exec.Command("git", …)` with `cmd.Dir`
  also appears in (at least) `cmd/agent/{cli_archive_unreachable, cli_daemon_cwd,
  cli_resume_cleanup_e2e, cli_resume_worktree, cli_tasks_merged_cleanup,
  cli_tasks_recycle_e2e, cli_tasks_worker_recycle_e2e, cli_worker_resume_e2e,
  merge_lock_e2e, merger_work_lock_e2e, rawprompt_worktree_release_e2e,
  repo_context_sandbox, task_claim_ground, worktree_release_accept}_test.go`,
  `internal/agent/{reframe_durable_repo, task_submit_repo}_test.go`. Each is the same
  shape; none was touched here (Rule 6 scope). The witness in this file protects THIS
  fixture, not that family.
- **`internal/docclass/walk_ignored_test.go`** builds a repository the same way
  (`exec.Command("git", args...)` + `cmd.Dir = root`, `git add -f`). It was NOT converted:
  converting a second fixture inside the same run would have widened the diff past the
  epic's deliverables, and the file has no external author identity in its commits. It is
  named here so the next reader finds it by grep rather than by incident.
- **`internal/docclass/prose_path_line_stability_test.go` / `prose_path_line_one_verdict_test.go`**
  run read-only git commands with no `cmd.Env` at all (inherited environment, cwd = repo
  root). They are read-only (`show`/`rev-parse`) and point at the module's own tree by
  design, so an inherited `GIT_DIR` cannot make them WRITE anywhere; they are out of scope
  for this containment and are named for completeness.
- **The other docclass git child**, `internal/docclass/docclass.go`'s `git check-ignore`
  probe, is inherited-environment and read-only for the same reason.
- **The suite's own scratch placement** could not be measured for the child: the child's
  `GOPI_TEST_TMPDIR` pin is an ordinary subdirectory, and the placement resolver *declines*
  a non-memory filesystem candidate, so the child's `t.TempDir()` lands on the scratch home
  (a block device) rather than `/dev/shm`. That is the honest degradation the placement
  code already reports; the witness does not depend on which one wins, because it reads the
  fixture's report rather than hunting for a repository on disk.

## 4. Evidence

- The raw git-routing reproduction: session `01a13513-2dc0-776c-b253-38d4efa69114`,
  `repro-gitdir/` (`outer` sentinel advanced `00e2fa4d…` → `9ab991f6…`, `tt/.git` never
  created).
- The witness's own runs: `go test ./internal/docclass/ -run
  TestAnchorNumberFixtureNeverRoutesGitAtAnotherRepo -v -count=1`, and the whole package
  `go test ./internal/docclass/ -count=1` (green).
- The red-proof and its restore, plus the master-side acceptance measurements, are in
  `tasks/01a13513-2dc0-776c-b253-38d4efa69114-report.md` (`GOPI_TASK_REPORT_PATH`).

## Overlap (Rule 26/41)

- `01a13513-b8d0-7e95-81a8-7ec1137f6307` (bug, pending) — the GIT_DIR root cause. This run
  contains the same fixture; that row's evidence and hypotheses were READ and not
  duplicated, and the row is left pending (it also carries the wider audit).
- `01a1351b-e8a0-7d63-a247-cd4b7c99750f` (bug, pending) — the branch cut from the wreckage
  (`task/01a134f8401c7489b983e73f85da887b`, still standing at `09f7e0269`). Observed in
  this run's reflog reading; NOT touched (Rule 29 — another run's ground).
- `01a1350f-6fe6-72a9-9133-5cc90c5b1012` (`in_progress`) — docclass anchor drift. Its
  ground and this run's edits are in the same package; this run touched only the fixture
  and added one test file, so the two diffs do not overlap in lines.
