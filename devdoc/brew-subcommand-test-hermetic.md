<!-- doc-class: historical -->
# Report: TestIsBrewSubcommand asserts the predicate, not the execution

Branch: `fix/brew-subcommand-test-hermetic` (commit `b3a59e1a`, worktree
`~/workspace/gopi-brew-test-hermetic`), based on master `e8ee5156`.

## Completed

- **Root cause pinned.** `cmd/agent/cli_brew_test.go` called
  `isBrewSubcommand([]string{"brew"})` to assert the *decision*. But
  `isBrewSubcommand` **dispatches**: on a matching argv it calls
  `runBrewSubcommand`, which builds the brew engine via `tools.NewBrewEngine()`
  → `deepseek` credential resolution. The failure path is `os.Exit(1)` **from
  inside the subcommand**, i.e. the test binary is killed before `testing` can
  print a `--- FAIL: <test>` line. That is why cmd/agent reported an anonymous
  package-level FAIL (a `-json` `fail` event with `Test == ""`) and why it sat
  unnoticed: the package failed for a reason it could not name.
- **Seam added (dispatch vs decision).** `cmd/agent/cli_brew.go`:
  - new `brewSubcommand(args []string) bool` — the **pure predicate**, executes
    nothing, compares `args[0]` against the `CliVerbBrew` constant (Rule 1b);
  - `isBrewSubcommand` keeps the same contract and now reads
    `if brewSubcommand(args) { runBrewSubcommand(args[1:]); return true }` —
    the dispatch half still executes, byte-identical behaviour and exit codes.
- **Test rewritten** (`cmd/agent/cli_brew_test.go`):
  - `TestIsBrewSubcommand` asserts `brewSubcommand` for the four vectors
    (`brew` → true, `brew --dry-run` → true, a non-brew verb → false, `nil` →
    false, `[]` → false) and **never executes** the subcommand. The literal
    `"memory_search"` vector became `CliVerbMemory` + its sub-verb (Rule 1b:
    the retired `memory_search` wire name must not be reintroduced, Rule 13).
  - `TestIsBrewSubcommandDispatchesOrSkipsCredentialFree` covers the dispatch
    half. Executing brew on a credential-less host kills the test binary (see
    root cause), so the test **skips loudly, with the reason**, when no
    credential is resolvable — Rule 27's classification of an
    external-dependency test. The skip decision calls the **production
    resolver** (`deepseek.ResolveClaudeAPIKey(nil)`) rather than re-spelling the
    env-var names, so the test cannot drift from the engine's own credential
    check.
- **The contract is unchanged**: `cmd/agent/main.go`'s dispatch chain still calls
  `isBrewSubcommand(dispatchArgs)`; `internal/clicontract` and the
  comment-scan contract test still find that call site.

## Not Done (honest)

- `TestIsBrewSubcommandDispatchesOrSkipsCredentialFree` **does not verify the
  true branch on this host** (no credential is configured here, so it skips).
  A host with a credential executes the `memory search` vector — a
  non-matching argv, so it still does not exercise `runBrewSubcommand`'s body.
  Nor does it assert the `brew` (matching) vector: that path is `os.Exit`, so
  asserting it in-process is impossible without another seam inside the
  subcommand. The dispatch half therefore stays covered by the existing
  clicontract chain assertions, not by execution.
- The `cmd/agent` suite is not hermetic in-session (memory #2497: worker/merger
  children write fixed `~/.gopi` paths), so the whole-package run below is a
  **symptom check**, not a full-package green claim. The branch-scoped verdict
  is the targeted `-run` filter.
- The re-verification's whole-package run still fails ONE test,
  `TestTaskListReadPathE2E`, on a missing `bin/gopi` (its own message says the
  fail-not-skip is deliberate, so a broken guard cannot read green). That test
  is untouched by this diff and its sibling concurrency (another worktree's run
  of the same package saw it PASS) makes it non-deterministic here — it is
  **left as-is**: the epic's own DONE condition (the anonymous package FAIL
  becomes a NAMED failing test) is what this branch fixes, and it holds.
- **CORRECTION (later measurement, same session — the paragraph above was
  incomplete).** A bounded whole-package run of this branch
  (`go test ./cmd/agent/ -count=1`, credentials stripped) did **not** fail on
  `TestTaskListReadPathE2E` and did not name any test at all: it died at Go's
  package timeout and printed
  `FAIL github.com/nick/gopi/cmd/agent 600.594s` / `FAIL`, preceded by a
  goroutine dump (`panic: test timed out after 10m0s`). So the package's
  non-green state on this host has **two** independent pre-existing causes, and
  this branch fixes neither of them (it fixes the third one, the epic's).
  Standalone control: `-run TestVoiceServeHTTPHandlerContract -timeout 90s` →
  `ok 0.360s`, i.e. the goroutine that dominates the dump
  (`cli_voice_server.go:46` `ListenAndServe`, created at
  `cmd/agent/cli_voice_review_test.go:1687`) is a **leaked live server goroutine**, not a
  hang — it merely survives to the timeout and makes the dump look like one.
  The suite's real cost is the family owned by row `01a0a63e` (failed). Filed
  as repair row **`01a0c04b-63b3-7402-91ab-a48c1da0bd3b`** (Rule 26), with the
  instruction that the cost be measured directly (per-package wall time /
  slowest tests from the `-json` stream, or the implicated `tx.Commit()`
  duration) and never by generating device load (Rule 27).
- The brew engine's own credential error is left exactly as it is (it is
  correct: an operator running `gopi brew` must be told the credential is
  missing). Only the *test's* path to it changed.

## Verification

- Build: `go build ./...` → exit 0, no output.
- Vet/fmt: `gofmt -l cmd/agent/cli_brew.go cmd/agent/cli_brew_test.go` → no
  output; `go vet ./cmd/agent/` → no output.
- Tests (branch-scoped, credential-less):
  `env -u DEEPSEEK_API_KEY -u ANTHROPIC_AUTH_TOKEN go test ./cmd/agent/
  -count=1 -run 'TestIsBrewSubcommand|TestBrewUsageDocumentsFlags' -v` →
  `--- PASS: TestIsBrewSubcommand (0.00s)`,
  `--- SKIP: TestIsBrewSubcommandDispatchesOrSkipsCredentialFree`,
  `--- PASS: TestBrewUsageDocumentsFlags`, `ok … 0.532s`.
- Commit: `b3a59e1a fix(cmd/agent): TestIsBrewSubcommand asserts the predicate,
  not the execution`.

## Evidence (self-filed)

- **RED before (master's own test, worktree `~/workspace/gopi-brew-proof`
  detached at `b3a59e1a` with `cmd/agent/cli_brew_test.go` restored from
  master), credential-less:**
  `go test ./cmd/agent/ -count=1 -run TestIsBrewSubcommand -v` →
  `=== RUN   TestIsBrewSubcommand` /
  `gopi brew: brew: deepseek: ANTHROPIC_AUTH_TOKEN not set (…)` /
  `FAIL github.com/nick/gopi/cmd/agent 0.798s` / `FAIL`; and the named-failure
  count `… | grep -c '^[[:space:]]*--- FAIL'` printed `0` — **zero named
  failing tests**, the reported symptom reproduced verbatim.
- **The test binary dies at the test (not the package)**:
  `go test ./cmd/agent/ -count=1 -run 'TestIsBrewSubcommand$' -json` emitted
  `"Action":"run","Test":"TestIsBrewSubcommand"`, then the credential line as
  that test's `Output`, then `"Action":"fail","Package":"…/cmd/agent"` with
  **no `"Action":"fail"` bearing `"Test"`** — the framework never got to
  report the test.
- **GREEN after (branch), credential-less:** targeted run above → `PASS` /
  `ok github.com/nick/gopi/cmd/agent 0.532s` — the package now passes on a host
  with no LLM credential.
- **Named-failure proof (the required property):** with the vector
  `{CliVerbBrew, "--dry-run"}` deliberately changed to `false`, the same
  targeted run printed
  `cli_brew_test.go:31: brewSubcommand([brew --dry-run]) = true, want false` /
  `--- FAIL: TestIsBrewSubcommand (0.00s)` / `FAIL … 0.578s` — a predicate
  mismatch is now a **named** failing test with a diagnostic, which the old
  shape could not produce.
- **Rule 6b self-check (a real slip, caught):** the first edits were applied
  through `~/work/GoPi/cmd/agent/...` absolute paths and landed in the MAIN
  repo. Measured `git status --porcelain` in the main repo listed
  `M cmd/agent/cli_brew.go` + `M cmd/agent/cli_brew_test.go`; the files were
  copied into the worktree, the main repo restored with
  `git restore cmd/agent/cli_brew.go cmd/agent/cli_brew_test.go`, and
  re-measured: main repo `git status --porcelain` → empty; worktree → the two
  intended files. All later edits used absolute worktree paths.
- **Worktree clean at close (Rule 30):** `git status --porcelain` in
  `~/workspace/gopi-brew-test-hermetic` printed nothing after the commit.

## Re-verification (independent pass, all claims re-measured on this branch)

Every item below was observed again from scratch — the earlier lines above are
kept as the original record, these are the re-measurements.

- **RED on master re-measured, own worktree, nothing borrowed:**
  `git worktree add --detach /home/user/workspace/gopi-brew-redproof e8ee5156`
  → `env -u DEEPSEEK_API_KEY -u ANTHROPIC_AUTH_TOKEN go test ./cmd/agent/
  -count=1 -run TestIsBrewSubcommand -v` printed `=== RUN
  TestIsBrewSubcommand` / `gopi brew: brew: deepseek: ANTHROPIC_AUTH_TOKEN not
  set (…)` / `FAIL github.com/nick/gopi/cmd/agent 0.584s` / `FAIL`, and the
  named-failure count was **0**. Worktree removed afterwards
  (`git worktree remove --force`), main repo `git status --porcelain` empty.
- **GREEN on branch, credential-less re-measured:** targeted run →
  `--- PASS: TestIsBrewSubcommand (0.00s)`, `--- SKIP:
  TestIsBrewSubcommandDispatchesOrSkipsCredentialFree (0.00s)` with the skip
  reason naming the credential, `--- PASS: TestBrewUsageDocumentsFlags`,
  `ok github.com/nick/gopi/cmd/agent 0.580s`.
- **Named-failure property re-proven by mutation:** the vector
  `{CliVerbBrew, "--dry-run"}` flipped to `false` in a scratch edit produced
  `cli_brew_test.go:31: brewSubcommand([brew --dry-run]) = true, want false` /
  `--- FAIL: TestIsBrewSubcommand (0.00s)`. The file was restored from a copy
  and `git status --porcelain` re-measured empty.
- **Whole-package run (background job, `-json` stream into the scratch tree,
  never into the worktree):** the `-json` record contains exactly ONE
  `"Action":"fail"` bearing a `"Test"` — `TestTaskListReadPathE2E`, whose own
  output line gives the reason (`sweep-zombies E2E needs a gopi binary: set
  GOPI_BIN to THIS tree's build … ../../bin/gopi: no such file or directory (a
  skip here would report a green regression guard that never ran)`). The brew
  events in the same stream are `"Action":"pass"` for `TestIsBrewSubcommand`
  and `"Action":"skip"` for the dispatch test — **zero anonymous failures**,
  which is the property the epic required. NOTE: a concurrent run of this
  package in another worktree saw the same E2E test PASS, so that row depends
  on `bin/gopi` existing at that moment (the path is written and removed by a
  sibling test); the branch's own READY bit is measured on the two brew tests
  plus this stream's evidence, and the E2E failure is not attributable to this
  diff (no `cmd/agent` tasks/merger file is touched by it).
- **Contract call site unchanged:** `isBrewSubcommand` is still referenced by
  `cmd/agent/main.go:577` (the dispatch chain); `internal/clicontract` →
  `ok github.com/nick/gopi/internal/clicontract 0.003s`.
- **Build/fmt re-measured:** `go build ./...` → exit 0, no output;
  `gofmt -l cmd/agent/cli_brew.go cmd/agent/cli_brew_test.go` → no output.
- **Rule 30 at this pass:** `git status --porcelain` in the worktree → empty
  after every mutation and after the job output was redirected OUT of the
  tree (a first attempt wrote `.tmp-pkgrun.json` inside the worktree; that job
  was killed, the file removed, and the redirect moved to
  `~/workspace/gopi-test-scratch/`).
