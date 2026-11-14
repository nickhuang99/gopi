<!-- doc-class: normative -->
# service help is capability-first, not a devdoc (worker / merger -h)

> **WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md** — the operator owns this; an agent must never run it (submit the work instead: `gopi task submit`).

Epic: **"worker help is misleading"** — an operator typing `gopi worker -h`
got a document about how the worker is built, not about what it does.

## The complaint, quoted

1. A whole section answered a question nobody asked — *why the privileged
   escape does NOT work*. If root cannot solve it, why bring it up? A user
   wants to know what a command CAN do.
2. The five-step loop protocol was printed up front, in protocol words
   (`must-succeed push`, `db-sync`, cgroup, CAS races). "It gives too much
   protocol detail … user mostly is interested in what this can do, not how
   it is doing so … the 'how' is part of a devdoc for interested readers,
   not for an impatient commandline user."
3. Why/how may appear LATER in the help — but never as the opening.

Measured against the shipped page (`attachment1`, the pre-fix text): 95 lines
of worker help whose first 10 were a numbered internals walkthrough, and 54
lines of it about privileges/cgroups/detach mechanics.

## What changed (shape)

Both service verbs' heads now follow one shape, in this order:

```
GoPi worker — <one-line capability>            ① capability, not architecture
  gopi worker           …                      ② invocations: what it does
  gopi worker --once    …
Options:                                       ③ the flags
Environment:                                   ④ the knobs
Notes:                                         ⑤ why/how detail, LAST
```

- `workerUsageHead` — 95 → 42 lines. `mergerUsageHead` — 108 → 60 lines.
- **Deleted** (they were the complaint): the *One loop round (the contract)*
  numbered protocol dump, the *Privileges (--daemon)* section (cgroup
  measurements, `setsid`, host service units), the *Detached mode* essay,
  the *Resident daemon management* block that re-explained `--status` et al
  after the Options table had already listed them, the merger's
  *Single instance* flock paragraph, its `SIGTERM`/`SIGUSR2`/durable-file
  stopping protocol (the user-facing fact — a stop takes effect at pass
  boundaries — is kept in one sentence), and the *"top-level because it is a
  service"* CLI-rationale paragraph.
- **Kept, one line each, in `Notes`**: the privilege ANSWER ("needs NO
  privilege, `--daemon` included") and what `--require-scope` does to a
  `--daemon` start. Rule 25 gates the *wording* (never prescribe a
  privilege), and the shape gate below keeps the answer in the tail.
- The loop is still described — one prose sentence in `Notes`: it claims a
  pending task, runs it, records the result, syncs with the other nodes,
  claims again. Facts preserved, vocabulary dropped.

Why the internals were removed and not just moved: the protocol words
(`db-sync`, `must-succeed push`) are *implementation* names for facts a user
needs in user words. The design choice recorded here is **translate, don't
relocate** — the devdoc (`devdoc/lifecycle-resident-tick.md`,
`devdoc/daemon-scope-privilege-hint-20260914.md`) remains the home of the
mechanism.

## Second round: what the rewrite broke, and the fix

The first full `go test ./cmd/agent/` run after the rewrite failed in seven
places — every one a real coupling the focused run had not covered. The
root cause was the same in all of them: **the page had been retargeted while
three things that referenced it were not**.

1. **Mirrored title fingerprints (4 tests).** `cli_contracts_test.go`
   (`cliContractInventory`), `merger_top_level_e2e_test.go` and
   `cli_tasks_worker_unknown_arg_test.go` each held a *copy* of the old
   title line (`"GoPi worker — the task worker loop"`,
   `"GoPi merger — the branch-merge DAEMON"`) and asserted the binary prints
   it. That is Rule 13's exact defect — a literal mirroring a single source
   of truth, drifting the moment the source changes. Fixed by **deriving**
   the fingerprint: `cliContractFingerprint` + `cliUsageTitle(usage)` return
   the first line of the live constant, so the assertion still proves the
   binary prints the page's title (teeth kept) but can no longer pin a dead
   string. The inventory entries for worker/merger now carry no fingerprint
   field at all.
2. **Pinned user-visible FACTS the rewrite dropped (3 tests), each restored
   in user words rather than re-adding the removed section:**
   - `merger_daemon_flag_e2e_test.go:TestMergerDaemon_HelpIsZeroSideEffect`
     wants `serve.lock` — the single-instance section now says the refusal
     names *"the pid serving `<data root>/serve.lock` (that file is the
     record)"*. The fact ("one merger per data root, and here is where that
     is recorded") is user-relevant; the flock measurement prose around it
     was not, and stays out.
   - `merger_graceful_restart_e2e_test.go:TestMergerRestart_HelpDocumentsTheControlFlags`
     wants `SIGTERM` — the Stopping note now says *"SIGTERM starts that
     graceful stop (SIGINT/SIGUSR1 work too)"*. A user who wants to stop a
     daemon needs the signal name; they do not need the TERM_<pid>/FORCE_<pid>
     file protocol.
   - `merger_top_level_e2e_test.go:TestTopLevelMergerHelpDoesNotRun` wants
     `gopi worker` — the Notes tail now names the peer service (*"'gopi
     worker' runs the queued tasks — the merger only merges the branches
     they produced"*), which is the one-sentence version of the deleted
     "top-level because it is a service" paragraph.

Lesson recorded: a help rewrite is a **cross-reference change**, not a text
edit. Before editing a user-facing page, grep for its title line and its
distinctive nouns in the test tree — the tests that pin them are the list of
facts a rewrite must either keep or deliberately retire.

## Third round: the line-number permit the rewrite shifted

`internal/standards` went red after the rewrite for a reason that has nothing
to do with the help text: `baseline.go` pins the `%w`-wrap false positive in
`cmd/agent/main.go` by **file:line** (Rule 16 baselines are exact `file:line`
permits). Shrinking `workerUsageHead`/`mergerUsageHead` by 97 lines moved the
byte-identical site from 1040 to 943, and the gate correctly reported a
*stale* permit plus the "new" violation at the new line.

Fixed by **re-pinning the line, not deleting the permit** — the standard
procedure for a shifted permit (a deleted permit turns a known false positive
into a standing failure). Re-verified the site is the same code with
`grep -n 'escape a literal prompt' cmd/agent/main.go` (one line, 943), and
extended the existing comment with why it moved a second time.

## Verification

### Environment results
- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK.
- Focused: 10/10 PASS (contract + help-shape + capability-first + red proof).
- Full package `go test ./cmd/agent/ -count=1` — PASS except ONE red:
  `TestWorkerDaemon_StatusReportsTheLiveDaemon` (worker_daemon_singleton_e2e_test.go:144).
- `go test ./internal/standards/` — FAIL→PASS after the baseline re-pin; the
  remaining Rule 18 hit is pre-existing on master.
- `go test ./internal/config/` — PASS.
- Real binary: `./bin/gopi worker -h` / `merger -h` open with the invocations,
  `Notes:` last, max 76 display cols (budget `config.HelpWrapCol`).
- `gofmt -l` on touched files: clean. `go vet ./cmd/agent/`: clean.

### The two reds excluded, with the proof (not assumed)

Both were reproduced on a CLEAN master worktree
(`/dev/shm/gopi-test-scratch/ab-master`), so neither is caused by this branch —
and `git diff 16f871c3..master` shows master did not touch any file this
branch edits.

1. `TestWorkerDaemon_StatusReportsTheLiveDaemon` — the status page names a
   `worker.lock` under the sandbox home where the test expects only
   `<dir>/worker.state`. Reproduced on master @c1117fb9 (the branch's base is
   its ancestor). Already fixed independently on branch
   `fix/worker-status-lockdir-assert` (7257a20a) — not adopted here per
   Rule 23 (never continue someone else's branch).
2. `TestStandardGates/TestGate3ElseAfterReturnRedProof` — a Rule 18 `else`
   after a terminating `if` at `cmd/agent/cli_voice_device_policy.go:226`.
   Reproduced on master @c1117fb9; this branch never touches that file
   (`git diff master -- cmd/agent/cli_voice_device_policy.go` is empty).

### Baseline permit shift (this branch's own red, fixed)

Rule 16 baselines are exact `file:line` permits. Shrinking the two usage heads
moved the byte-identical `%w`-wrap false positive from `cmd/agent/main.go:1040`
to `:943`, and the gate reported a stale permit + a "new" violation. Re-pinned
the line (never delete a permit) after re-verifying the site with
`grep -n 'escape a literal prompt' cmd/agent/main.go`.

### Binary staleness trap (worth remembering)

Five e2e tests failed with *"bin/gopi (mtime …) is OLDER than … — this test
drives the BUILT binary"*: any `.go` edit invalidates `bin/gopi`, so a rebuild
must precede the package suite or a green branch reads red.

- `go build ./...` — OK.
- `go test ./cmd/agent/ -run 'TestServiceHelpIsCapabilityFirst|TestServiceHelpDetailStaysAtTheEnd|TestCapabilityFirstPredicateIsNotBlind|TestEveryHelpConstantFitsTheWrapBudget|TestHelpConstantsCarryNoCarelessText|TestWorkerUsageHeadCoversLoopContract|TestDaemonScopeNoPrivilege'`
  — PASS (see the task report for the raw output).
- The REAL binary was run (`gopi worker -h`, `gopi merger -h`) and the
  printed page starts with the invocations.

## The gate (why this cannot silently come back)

`cmd/agent/help_capability_first_test.go`:

- `hcfCapabilityFirstOffenses(text)` — the predicate: (①) an invocation line
  must appear within the first lines; (②) the shipped defect fragments
  (`One loop round (the contract)`, `Privileges (--daemon)`, `cgroup`,
  `TOP-LEVEL BECAUSE IT IS A SERVICE`, `the must-succeed push`, `db-sync`)
  must be absent; (③) after the `Notes:` tail section no new section header
  may start — the page cannot drift back to instructions once the detail
  began.
- `TestServiceHelpIsCapabilityFirst` applies it to the live constants.
- `TestCapabilityFirstPredicateIsNotBlind` is the **red proof**: both
  pre-epic pages are kept byte-exact under `cmd/agent/testdata/`
  (`worker_help_prefix.txt` — the attachment the epic shipped with — and
  `merger_help_prefix.txt`) and must be flagged; the test also asserts each
  forbidden fragment is still covered by a fixture, so a case cannot be
  silently dropped from the proof. A prose-first page with invocations below
  must be flagged, and a page in the shape this epic asks for must pass — a
  green run cannot be a blind one (Rule 10.4). The fixtures are files, not
  hand-retyped strings, because a paraphrase of the shipped page is a proof
  about the paraphrase.
- `TestWorkerUsageHeadCoversLoopContract` was **retargeted**: it used to
  pin the wire vocabulary (`must-succeed push`, `db-sync`), i.e. it was the
  codified version of the complaint. It now pins the same FACTS in user
  words.

Note on the predicate: an early draft flagged the *bare* word `sync`, which
false-positived on legitimate user-facing text ("sync with the other
nodes"). Caught by the red-proof test before commit; the fragments are the
shipped protocol shapes, not the English word.

## Not done

- The `gopi task` group's own help surfaces (e.g. the deprecated
  `gopi task merger` block) were not re-toned — only the two top-level
  service verbs the report names (`worker`, `merger`).
- No attempt to move the deleted internals into a fresh devdoc: they already
  live in `devdoc/daemon-scope-privilege-hint-20260914.md` (privilege/cgroup
  measurements) and `devdoc/lifecycle-resident-tick.md` (the loop/claim
  machinery), so drilling them again would duplicate a single source of
  truth.
