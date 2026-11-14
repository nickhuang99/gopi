<!-- doc-class: historical -->
# devdoc: the worker's PROGRAM identity is its IMAGE, not its comm

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator
> owns launching workers; an agent must never run `scripts/gopi-worker-up.sh` to
> start one (submit the work instead). Read-only use — `--count` — is what this
> document's evidence used.

## The defect (bug 01a131fcb8337c1b, measured 2026-10-13)

Four judgement sites asked **"is this process a gopi worker?"** and answered it
with `comm == "gopi"`. `comm` is the basename of the executable **file**, so a
gopi binary built under another file name — `bin/gopi-fix`, `/tmp/gopi-test`, a
per-task build — made its worker invisible:

| site | what it said about a live `gopi-fix worker` |
|---|---|
| `scripts/gopi-worker-up.sh --count` | `live gopi workers: 2` (the worker absent) |
| `gopi worker --status` (fallback scan) | not listed |
| `cli_worker_status.go` residue classification | `pid … is alive but is not gopi (comm "gopi-fix")` |
| `internal/config` `parentIsWorkerGopi` | parent not recognized as a worker |
| `cli_worker_foreground.go` `workerGopiImage` | (already fixed in `b50f70157`, image-based) |

This is the **proxy metric reported as the measurement** (Rule 15): the file's
name is cheap to read and is not the fact the caller asked about. Same family as
bug 01a131c8-0500, one level down.

## The judgement (one definition, `internal/procname`)

```go
func ImageName(procRoot, pid, argv0 string) string   // /proc/<pid>/exe → argv[0]; "" = unmeasured
func IsGopiImage(procRoot, pid, argv0, comm string) (isGopi, fromComm bool)
func WorkerImageIsGopi(procRoot, pid, argv0 string) bool   // the CLI adapter
func WorkerShape(procRoot, pid, comm string, argv []string) bool
```

- **Channel order**: `/proc/<pid>/exe` (the kernel's answer, `" (deleted)"` marker
  stripped), then the process's **own `argv[0]`** basename, then `comm` as the
  third and weakest channel — `comm` is the kernel's truncation of the same image
  file, so it is evidence, and the second return value says when the verdict rests
  on it.
- **Prefix, not equality**: `ImageGopiPrefix = "gopi"`. `gopi-fix` **is** this
  program. An image that merely contains the word is not (`nginx` is refused).
- **A judgement, not a guess**: every site reads the *same* function; the shell
  reaches the same value through `GOPI_PROC_IMAGE_PREFIX` (env constant
  `EnvImagePrefix`), aliased in the script to `$PROC_NAME` so the value has one
  definition per language and `internal/testutil/procname_gate_test.go` asserts
  they agree.

**Find widely, signal strictly.** The identity judgement decided above is
*inclusive* (it answers "is this the gopi program?"). `workerSameBinary` — the
test that decides whether a **signal** may be sent — stays strict (device+inode
of `/proc/<pid>/exe` vs `/proc/self/exe`) and is unchanged. Widening the
identification never widens the blast radius.

## The five sites

1. `internal/procname` — the definition above (`WorkerShape` rewritten).
2. `cmd/agent/cli_worker_status.go` — `workerProcessShape` (fallback scan) and
   the state-file residue classification; `processComm` → `processImageName`,
   `ProcessComm` → `ProcessImage`, reason text now names the **image**.
3. `cmd/agent/cli_worker_foreground.go` — `workerGopiImage` delegates to the leaf
   judgement; `workerProgramPrefix` / `procExeName` are aliases of the shared
   constants instead of re-typed literals.
4. `internal/config/agentguard.go` — `parentIsWorkerGopi` (the fourth site, found
   by measurement, not named in the original report).
5. `scripts/gopi-worker-up.sh` — `count_gopi_processes` reads `<pid>/exe`
   (then argv[0]) against `$IMAGE_PREFIX`.

## Pitfalls found while doing it

- **`filepath.Base("")` returns `"."`** — a naive `ImageName` fallback turns "no
  argv[0]" into the plausible-looking name `.`. A directory spelling is not an
  image name, so it is refused explicitly.
- **A fixture that omits `<pid>/exe` silently tests the fallback** for every case
  at once. The launcher fixture and the `cmd/agent` `procFixture` now write an
  image link; `procFixtureExe` (the explicit-override variant) removes the
  default link first, or the symlink quietly *replaces* it.
- **The residue test's `comm`-based reason** pinned the old channel's words. The
  reason vocabulary is now per-channel, so the test had to be re-pointed — a good
  example of a test that encoded the defect.
- **The fixture's EXPECTED classification is part of the criterion.** Once the
  judgement is the image, an entry whose comm is `gopi runner` but whose image is
  a foreign program is a *decoy*, and an entry whose comm holds a space while its
  image is gopi is a *worker*. Both directions had to be re-stated in the fixture
  rather than adjusted in the assertion.
- **`gofmt -l` reports many pre-existing files** in this tree; run it on the
  touched files, not repo-wide, when deciding what to format.

## Rule 36 / report

Classification row for this document lives in `devdoc/DOC-INDEX.tsv` (written by
the close-out sync or `gopi tool doccheck --fix`).
