<!-- doc-class: historical -->
# Task Report: the installed operator launcher was stale (attribution silently OFF)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->
> The launcher audited here starts workers, and a worker is the **operator's**
> process: an agent must never run it (the guard refuses with `exit 3`). An
> agent's verb is a submission — `gopi task submit`.

## Completed

- **Diagnosed the drift with byte identity, both sides recorded.** The installed
  `~/.local/bin/gopi-worker-up.sh` (reached as `~/bin/gopi-worker-up.sh`, a
  symlink to it — that is the copy an operator's shell runs) was
  `sha256 b2cd1ef0…f9d12` (4232 bytes) while the repo's
  `scripts/gopi-worker-up.sh` was `sha256 4b2ad5a2…2d128f`.
  `git show 776cb789^:scripts/gopi-worker-up.sh | sha256sum` →
  `b2cd1ef0…f9d12`: **the installed bytes were exactly the pre-rename git blob**,
  i.e. the copy predates the `--worker-log-root` / `GOPI_WORKER_LOG_ROOT`
  contract. This is a measured drift, not an inference from a diff.
- **Re-installed from the repo** (`--install`), and proved byte identity on all
  three sides:

  | path | before | after |
  |---|---|---|
  | `~/work/GoPi/scripts/gopi-worker-up.sh` | `4b2ad5a2…2d128f` | `4b2ad5a2…2d128f` |
  | `~/.local/bin/gopi-worker-up.sh` | `b2cd1ef0…f9d12` | `4b2ad5a2…2d128f` |
  | `~/bin/gopi-worker-up.sh` (symlink) | → target, stale | → target, identical |

  `diff -q` clean; `readlink -f ~/bin/gopi-worker-up.sh` →
  `/home/user/.local/bin/gopi-worker-up.sh`.
- **Proved a REAL launch through the installed copy carries the attribution
  root** (stub binary in a temp PATH, so no worker claimed anything): the
  launched process received BOTH faces —
  `ARGV: worker --worker-log-root <root>` and `GOPI_WORKER_LOG_ROOT=<root>` —
  and the pre-claim record landed at `<gopi-dir>/logs/worker-startup.log`.
- **Added `--check` to the launcher** — the drift was loud-free and needed a
  loud answer in the thing that owns the start path. `--check` audits the
  installed copy against the running script with sha256 and exits
  **0 identical / 1 STALE (naming both digests and the one repair command) /
  2 no installed copy** (a distinct status: "never installed" is not "stale").
- **Regression gates** in `internal/testutil/worker_up_install_test.go`:
  `TestWorkerUpInstallIsByteIdenticalToTheRepoScript` (installed == repo bytes,
  executable, `~/bin` is a symlink to the stable copy),
  `TestWorkerUpCheckReportsStaleInstalledCopy` (the defect test: the fixture
  rewrites the installed copy in the incident's own shape — the attribution
  flag dropped — and the check must go red, name both digests, and be repaired
  by `--install`), and
  `TestWorkerUpCarriesNoRetiredLauncherForms` (the retired knob
  `GOPI_WORKER_LOG_DIR` may appear nowhere; the retired positional `[logdir]`
  may not be advertised in the usage line).
- **RED-proof that is the incident itself, not a formality**: replacing
  `scripts/gopi-worker-up.sh` with the pre-fix blob `b2cd1ef0…` turns
  `TestWorkerUpCheckReportsStaleInstalledCopy` **red**; restoring returns the
  tree to `4b2ad5a2…`. A separate mutation (usage line re-advertising
  `[logdir]`) reddens the retired-form gate. Both restored:
  `git status --porcelain scripts/` empty.
- **glm-night-watch contract text checked, and it was already correct.**
  `~/.gopi/glm-night-watch.sh` is byte-identical to `scripts/glm-night-watch.sh`
  (`sha256 b2fa238c…5f351`) and contains **no** reference to the retired
  `GOPI_WORKER_LOG_DIR` (the only `worker-night-` occurrence is inside the
  quoted RETIRED argv it documents as the defect it closed). Its contract is
  argv-shaped: `gopi-worker-up.sh [HHMM] [N]` + "The operator's own plain-shell
  launch is a single launcher call: `gopi-worker-up.sh N`". Its delegated
  dry-run prints `ssh hp 'bin/gopi-worker-up.sh 2'` and
  "this script starts no worker itself". **No repo edit was needed here** — the
  premise that the header still cites the env-var form did not hold on this
  revision; asserting it were "fixed" would have been a fabricated change.
- **Swept the other installed launchers for completeness**: `gopi-home-gc.sh`
  (`94e82381…`), `gopi-scratch-gc-watchdog.sh` (`f6f189df…`) and
  `glm-night-watch.sh` (`b2fa238c…`) are each byte-identical to their repo
  counterpart; `night-progress.sh` contains no worker start at all. The
  launcher was the ONLY stale installed copy on this host.

## Not Done

- **The live host copy is the MERGED copy, not this branch's copy.** Immediately
  before finishing I re-ran `--install` against this branch; my own red-proof
  then reverted the uncommitted `--check` edits, so I restored the deliverable
  and re-installed **master's** `4b2ad5a2…` bytes — deliberately. Running a
  further `--install` from the worktree now would install an unmerged branch
  over the host's stable copy (the same objection devdoc/worker-count.md
  records). **After the merge, run
  `~/work/GoPi/scripts/gopi-worker-up.sh --install`, then
  `~/bin/gopi-worker-up.sh --check` (which prints `identical: …`).** The live
  host is presently correct-but-without-the-audit-seam; the seam arrives with
  the merge.
- **The end-to-end "task id → output.log" acceptance was already green**
  (`cmd/agent` `TestWorkerLogsAreReachableFromTheTaskIDAlone` PASS, run against
  a binary built at this branch's tip, mock LLM), so it is cited rather than
  re-implemented. I did **not** launch a real worker inside this agent session —
  the agent-spawn guard would refuse it with exit 3, and that refusal is
  correct. The launch evidence above is the isolated-stub form.
- **Found, filed as out of scope (not fixed here)**: on this host
  `~/.gopi/sessions/*/output.log` does not exist and the four live
  `gopi worker` processes carry no `GOPI_WORKER_LOG_ROOT` — attribution is
  absent in practice even after the byte fix. That is a separate, deeper gap
  (why those workers run unattributed) and deserves its own task; it is NOT
  repaired by refreshing the installed copy.
- The 20 legacy `worker-<ts>-<i>.log` files under `~/.gopi/` remain (retained
  and indexed by the attribution epic, cited by merged docs — untouched).

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (exit 0).
- Launcher syntax: `bash -n scripts/gopi-worker-up.sh` → OK.
- `go vet ./internal/testutil/` → clean; `gofmt -l` → empty.
- Gate tests: `go test ./internal/testutil/ -run 'TestWorkerUp' -count=1` →
  **PASS** (incl. the three new/related gates; the 3 pre-existing procname
  gates still pass: shape count 3/6, no `pgrep`/`pkill`, values named).
- Defect e2e (binary at this tip + mock LLM):
  `go test ./cmd/agent/ -run TestWorkerLogsAreReachableFromTheTaskIDAlone` →
  **PASS** (1.98s).
- RED-proofs: pre-fix blob → drift gate **FAIL**; usage-line mutation →
  retired-form gate **FAIL**; both restored (`git status --porcelain scripts/`
  empty, `sha256 4b2ad5a2…2d128f` back).
- `--check` behaviour matrix (sandbox HOME): none installed → exit 2; after
  `--install` → exit 0 `identical: …`; installed copy replaced by the incident
  blob → exit 1 `STALE` + both digests + the repair line.
- Commit: see the commit on `fix/launcher-install-refresh`.

## Branch + recovery (Rule 6d)

- Branch: **`fix/launcher-install-refresh`** (worktree
  `~/workspace/gopi-launcher-stale`, in the shared repo `~/work/GoPi` — the
  commits are already visible from the main repo, no push needed).
- Recover the report:
  `git show fix/launcher-install-refresh:devdoc/worker-up-installed-copy-drift-20260912.md`
- Live host after this task: installed launcher == master's copy
  (`4b2ad5a2…2d128f`), `~/bin` symlinked to it.
