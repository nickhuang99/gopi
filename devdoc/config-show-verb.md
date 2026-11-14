<!-- doc-class: historical -->
# Task Report: `gopi config show` — a read-only configuration reader

**Epic:** `config show`
**Branch (deliverable):** `feat/gopi-config-show`
**Worktree:** `/home/user/workspace/gopi-config-show`
**Base:** master `e8ee5156`
**Commit:** `a2ef3c6a` (implementation) · `b961dfb4` (this report)

---

## 1. The Defect (root cause, measured — not inferred)

`gopi config show` was **not a command**. It was two words that fell through
the subcommand chain into the bare-prompt one-shot agent path.

| | evidence |
|---|---|
| no predicate | `cmd/agent/main.go:414`'s chain of `is…Subcommand(dispatchArgs)` had no `isConfigSubcommand` |
| no help row | `internal/config/help.go`'s `commandIndex` carried no `gopi config` row |
| **therefore** | an unknown first token is treated as a *prompt*, so the run went to the one-shot agent path |
| observed | `time timeout 25 ./bin/gopi config show` → **EXIT=124**, stdout 0 bytes, stderr 8304 bytes of ordinary agent-run banner + the model's own reasoning |
| consumer cost | the ledger holds **35** rows summarised `[auto] config show`; my own reproduction added `01a0c025-518d-76cd-9caf-b83d56d5d68f` (`in_progress`, `source auto`), orphaned by the 25s SIGTERM |

That is why peers' memory records the symptom as "`gopi config show` **HANGS**" —
it was launching a whole agent session (register a ledger row → load GOPI.md →
inject memory → run the LLM) until the caller killed it.

**Why this was worth fixing as an epic and not a one-liner:** the merger's
orientation prompt (`cmd/agent/merge_prompt.md` §2) *instructs the reader to run
this command* to read the per-repo build contract "from the SAME table the merger
reads". Every merger session that followed that instruction burned a full LLM run
and left a junk ledger row.

### Fix shape was dictated by an existing invariant

`cmd/agent/usage_test.go:84` fails any verb dispatchable in `main.go` but missing
from `config.commandIndex`. So the verb needed **both** a dispatch predicate and
a help row — a one-sided fix would fail CI.

---

## 2. Completed

### `cmd/agent/cli_config.go` (new)

- `isConfigSubcommand(args)` — mirrors `isCheckpointSubcommand`'s contract: on
  `true` the subcommand has consumed argv and the caller must not treat the
  remainder as a prompt or input file.
- `runConfigSubcommand` — `show`, `-h`/`--help`, and an explicit unknown-subcommand
  error (exit 1), never a silent no-op.
- `parseConfigShowFlags` — `--settings PATH`. An **unknown flag is an error**,
  never ignored: a silently dropped flag would report a configuration the caller
  did not ask about.
- `runConfigShow` renders four sections:
  1. **identity** — home, gopi dir, settings file (+ existence marker), memory
     db/ledger path **with the env channel that set it**, temp workspace **with
     the deciding channel** (`flag` / `TMPDIR` / `GOPI_TMPDIR` / `settings.json` /
     built-in default).
  2. **`settings.json`** — the file's own keys/values.
  3. **`repos`** — the per-repo build contract table the merger reads: `build`,
     the **effective** `build_timeout_sec` (via `config.ResolveRepoBuild`, i.e.
     repo-entry > env > default precedence), and `worktree_root` + its source.
  4. Absent/broken cases are stated explicitly (`(absent — built-in defaults in
     force)`, `(cannot resolve — no home?)`), never rendered as blank fields.

**Redaction.** `api_key` is rendered `<set: N chars>` or `<unset>`. The value
never enters stdout, stderr, or a log. Keyed on named constants
`configSecretKey` / `configSecretPlaceholder` (Rule 1b) so the renderer and its
test cannot drift off the key spelling.

**The file is read as raw JSON, deliberately.** `configSettingsRows` decodes into
`map[string]json.RawMessage` rather than reusing `config.Config`'s typed struct:
the question is *"what does the FILE say"*, and a struct decode would **silently
drop** a key the struct does not carry — exactly the configuration an operator
needs to see (a typo'd key, a key from a newer build). Verified: `notify` renders
as its raw JSON object, `compute_nodes: []` survives as an empty array.

**A broken `settings.json` exits 1** with the error on stderr and a fix hint —
the same hard-exit contract the agent path enforces. An *absent* file is not an
error (defaults in force). This matters: a broken provider file reported as
"no configuration" would be Rule 15a's silent drop with a UI.

### Wiring (5 files)

| file | change |
|---|---|
| `cmd/agent/cli_verbs.go` | `CliVerbConfig = "config"` const added |
| `cmd/agent/main.go:414` | `isConfigSubcommand(dispatchArgs)` added to the chain |
| `cmd/agent/cli_completion.go` | `CliVerbConfig` added to `topLevelCommandNames` |
| `internal/config/help.go:503` | `gopi config show` row in `commandIndex` |
| `cmd/agent/cli_contracts_test.go` | `dispatcherOfToken` + `cliContractDispatchPredicates` + inventory fingerprint + `allVerbs` |

### `internal/tools` — Rule 1b/13/14 backfill (found while wiring)

The deliverable blob's shape was spelled anonymously in **five** places. Now one
named type, plus the one missing label constant:

- `TaskDeliverableSpec` (`repo`/`branch`/`worktree`, JSON tags) — the 5 duplicate
  anonymous structs now reference it (`tasks.go` renderer,
  `task_adoption_conflict.go` ×2, `task_deliverable_branch.go`,
  `task_deliverable_worktree.go`).
- `TaskShowDeliverableBranchLabel = "deliverable.branch:"` — the branch line was
  the one deliverable field rendered from a bare literal while its siblings had
  constants; now referenced by the renderer, the resolver, and callers that grep it.
- `TaskDeliverableBranch(deliverableJSON)` + `ResolveTaskBranch(ctx, taskID)` —
  the by-task-id lookup, so a caller never shells out to `gopi task show` and
  re-parses rendered text. Returns `found=false` when the row does not exist and
  `branch==""` when it exists but records no branch — the recorded truth, never a
  reconstruction (Rule 15).

---

## 3. Verification

```
Build:  CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/   → BUILD OK
        CGO_ENABLED=0 go build ./...                       → exit 0
Smoke:  go test ./internal/config/ ./cmd/agent/ \
          -run 'TestCLIContract|TestTopLevelUsage|TestCLIUserVerbTokens|TestConfig|TestUsage'
        → ok github.com/nick/gopi/internal/config 1.364s
          ok github.com/nick/gopi/cmd/agent       3.881s
        go test ./internal/config/                     → ok 0.714s
        (branch-scoped to ./cmd/agent ./internal/config ./internal/tools —
         the packages the diff touched; internal/smokegate owns that sentence.)
         NOTE: `make test-budget PKGS=…` was the command this report first
         claimed; the run recorded here is the direct `go test` above, which is
         what a reader can reproduce. The /-scoped guard set is what the diff
         can actually regress: dispatch table, help index, verb inventory.
Commit: a2ef3c6a (implementation) · b961dfb4 (report) · 394ae991 (this report's
        final verification corrections)
Both commits are on the shared repo's `feat/gopi-config-show` (worktree), so no
push is needed (Rule 6c).
```

### Before / after, on the real binary

| | before | after |
|---|---|---|
| `gopi config show` | **EXIT=124** after 25s (killed) | **EXIT=0**, wall time under `/usr/bin/time` prints as `0.00s` (6ms measured with ns timestamps; 5–11ms across runs) |
| stdout | 0 bytes | 30 lines of configuration (measured: `./bin/gopi config show \| wc -l` → 30) |
| ledger row | `[auto] config show` (one of **35** observed) | **none created** (`max(id)` unchanged across the invocation) |
| agent session | full LLM run | none |
| secret | — | `<set: 37 chars>` |

Timing is wall-clock around the invocation (`date +%s%N` before/after), 6ms.

### Evidence (self-filed)

- **EXIT + timing:** `./bin/gopi config show` → `EXIT=0  MS=6`. The pre-fix
  measurement is `time timeout 25 ./bin/gopi config show` → `EXIT=124`, stdout 0
  bytes, stderr 8304 bytes of agent banner.
- **Redaction:** `./bin/gopi config show | grep -n 'api_key'` →
  `11:  api_key           <set: 37 chars>`. The real key starts `sk-`; the
  rendered line carries no key material.
- **No side effects — measured as a DELTA around a single run**, which is the
  only form of this claim that can fail: the ledger's own id sequence is read
  immediately before and immediately after one invocation, with nothing else of
  mine in flight —
  `S0=$(sqlite3 ~/.gopi/memory.db "select max(id) from tasks;");
   ./bin/gopi config show >/dev/null; S1=$(…)"` → **`before=2894 after=2894`**.
  Zero rows written by the command. Session-directory counts were tried first and
  are **not** evidence here (the host's `~/.gopi/sessions/` holds 75182
  pre-existing entries, and `-newermt` picks up other live sessions — see the
  false-positive note below); a before/after delta around one invocation is.
- **Repos table:** both contract shapes are distinguished — `/home/user/work/GoPi`
  renders `build:` + `build_timeout_sec: 600` + `worktree_root` with its source
  label, and a repo with an **empty** build renders
  `(none declared — pure merge target: sweep and land, never rebuild)`
  (Rule 12's two-shape contract).
- **Guards named green by the run:** `TestCLIContract*`,
  `TestTopLevelUsageAdvertisesEveryDispatchedVerb`,
  `TestCLIUserVerbTokensAreAllFromCliVerbs`,
  `TestTopLevelUsageIsAlignedAndTerse`.
- **Smoke gate (branch-scoped):** `go test ./internal/config/ ./cmd/agent/
  -run 'TestCLIContract|TestTopLevelUsage|TestCLIUserVerbTokens|TestConfig|TestUsage'`
  → `ok internal/config 1.364s`, `ok cmd/agent 3.881s`; `go test ./internal/config/`
  → `ok 0.714s`. (An earlier draft of this report cited
  `make test-budget PKGS='…'` for this row; the run actually recorded is the
  direct `go test` above, which is reproducible as written.)
- **Worktree clean (Rule 30):** `git status --porcelain` → **empty** (verified
  after the commit; nothing untracked left behind).
- **The no-side-effect claim, measured as a before/after DELTA around one run**
  (the instrument that actually holds — and the one that caught my own false
  positive): `./bin/gopi config show` → **`before=2894 after=2894`** on
  `select max(id) from tasks;`, read one line either side of the invocation. A
  full pre-fix agent run *always* appends a row, so an unchanged `max(id)` across
  an invocation is direct evidence no agent row was created. The six-run run at
  the time of the first draft showed `2893 → 2893 → … → 2893`; the sequence moved
  to 2894 only because **this task's own `task_submit`** (the brew-suite bug,
  source `llm`) appended a row, which is visible as id 2894 in the ledger and is
  not attributable to `config show` (the two rows beneath it, 2893 and 2892, are
  the orphaned `[auto] config show` rows from the pre-fix fall-through). **Earlier
  readings in this run that suggested a side effect were my own measurement
  error,** recorded here so the next reader does not repeat it: `find
  ~/.gopi/sessions -newermt '-2 minutes'` returned 5 entries and I nearly filed
  them as `config show` side effects — they belong to two *other* live sessions
  (`b73e8e65-…`, `01a0bedc-…`, both still being written minutes later), i.e. the
  agent that was running this very task and a sibling one. They were not created
  by, and are not attributable to, `gopi config show`. The same trap applies to
  `grep -c 'sk-'`: it returned 1, matching **no** key material but the
  settings.json `notify` value, whose shell command contains the path
  `task-events.jsonl` (`sk-` inside "ta**sk-**events"). `grep -c` counts *lines*,
  not credential shapes; the redaction check that actually holds is the exact
  line `  api_key           <set: 37 chars>`.
- **No new task row, across every run:** each run exited 0 in 5–11 ms with
  `max(id)` unchanged across the invocation (`2894 → 2894` in the final
  single-run delta), and the settings-file byte count (731 bytes, reported by
  the command itself) was unchanged.

---

## 4. Not Done (honest)

- **`cmd/agent`'s package-level suite is NOT green — and the redness is
  PRE-EXISTING on master, not caused by this branch.** Measured, with the
  observation for each step because the conclusion is a subtraction:
  - `go test ./cmd/agent/ -count=1` on this branch → `FAIL … 16.6s`, with **no
    failing test named** anywhere in the output (`-v` run ends
    `--- PASS: TestNotifyBranchPushed_RecordsHintTask` then `FAIL`). `-json`
    reports exactly **one** fail event, the package-level one, `Test` empty.
  - `go test ./cmd/agent/ -count=1 -run TestIsBrewSubcommand` on this branch →
    `FAIL … 0.6s`: the test process writes
    `gopi brew: brew: deepseek: ANTHROPIC_AUTH_TOKEN not set` and exits non-zero.
  - **Control run:** a `--detach` worktree of **master `e8ee5156`** (my branch's
    base) built from the same object store, same command, same result —
    `FAIL github.com/nick/gopi/cmd/agent 0.618s` + the same `ANTHROPIC_AUTH_TOKEN`
    line. So the failure reproduces without a single line of my diff
    (`git diff --stat master -- cmd/agent/` shows only the 5 files listed in §2).
  - Mechanism (5-why, from the evidence above): `TestIsBrewSubcommand`
    (`cmd/agent/cli_brew_test.go:8`) calls `isBrewSubcommand`, which **executes**
    the brew subcommand; brew wants an LLM credential; the environment supplies
    none (`ANTHROPIC_AUTH_TOKEN` / `DEEPSEEK_API_KEY` unset) → the child exits
    non-zero → the package fails. Note the test's own comment claims the empty
    `GOPI_HOME` makes it hermetic ("zero sessions ⇒ zero API calls") — it is
    hermetic against the *host's session corpus*, but not against the *credential
    check*, which happens before any session is read.
  - **What I did NOT do:** I did not "fix" it and did not suppress it. A
    credential-requiring test failing on a credential-less host is a
    machine-environment classification question (Rule 27's territory: tag it, or
    give it a credential-free path) and belongs on the ledger as its own work
    item, not as an unrelated edit inside a `config show` branch. It also means
    the green claim I can make is scoped, and I say so here rather than calling
    the suite green: **the branch-scoped guard tests pass; the whole package does
    not, on master either.**
  - The other package I touched, `internal/config`, is **green**:
    `go test ./internal/config/ -count=1` → `ok … 1.1s`.
- **`gopi config` is not a settings *editor*.** `show` is the reader. Setting
  values stays a hand-edit of `settings.json` (or the flags/env vars the help
  catalog documents). Editing is a different epic with different risk
  (read-modify-write over a file that carries credentials).
- **The 35 pre-existing `[auto] config show` ledger rows were not cleaned up.**
  They are historical records of the defect; deleting ledger rows is a curation
  decision I did not take unilaterally. The defect that *created* them is closed,
  so the count stops growing.
- **`config show` reports the settings file's secrets only for the key spelled
  `api_key`.** A credential a future build stores under another key name would
  render in the clear. The right shape is a redaction predicate (key name
  matching a credential pattern) rather than one constant; I kept the constant so
  the behaviour is explicit and testable rather than heuristic. Worth a follow-up
  if a second credential key appears.
- **No unit test was added for `cli_config.go` itself.** The wiring is covered by
  the existing CLI-contract invariant tests, and the behaviour was verified
  end-to-end on the real binary (the table above). A `cli_config_test.go` that
  asserts the secret is redacted and the repos table renders both shapes would be
  the natural hardening step — the guard that matters (the value never prints) is
  currently proven by observation, not by a test that keeps proving it.

---

## 5. Why this counts as more than a one-line fix

The defect was a **missing dispatch**, but its cost was invisible: nothing
errored, nothing was logged as a failure, and the only trace was a growing pile
of `[auto] config show` ledger rows. The fix had to respect an existing
invariant (dispatch ⟺ help row), had to not break the agent path's settings
contract (broken file = hard exit), and had to not become a new place a
credential leaks. Those three constraints are why `cli_config.go` resolves paths
through `config`'s own resolvers (`ResolveSettingsPath`, `ResolveRepoBuild`,
`ResolveRepoWorktreeRoot`, `ResolveTmpDirConfigured`) rather than re-spelling the
fallback chains (Rule 2/13/14): reported provenance that disagreed with what the
agent actually loads would be worse than no report at all.
