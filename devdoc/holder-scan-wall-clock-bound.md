<!-- doc-class: historical -->
# Task Report: `scratch.LiveHolders` — a wall-clock bound, and truncation that cannot be read as "not held"

Task: `01a12ee4-64a9-7059-aa82-f22c3ff59e9d` · branch `task/01a12ee464a97059aa82f22c3ff59e9d`

## Completed

- **`internal/scratch/holder.go` — the bound, the result type, and the ONE reader.**
  - `HolderScanDefaultBudget = 8 * time.Second` (named constant, Rule 1/2) is the default
    wall-clock bound of ONE holder pass.
  - `HolderScanDeadlineEnvName = "GOPI_HOLDER_SCAN_DEADLINE_SEC"` with exactly ONE reader,
    `HolderScanDeadline()`: unset ⇒ the default; `0` (or negative) ⇒ the deliberate
    **unbounded** form; a positive integer ⇒ that many seconds; **unparsable ⇒ reported
    on stderr and the default**, never a silent fall-through to unbounded.
  - `type HolderScan struct { Paths []string; Truncated bool; Elapsed time.Duration }` plus
    `Complete()`, `Disclosure()`, and `HeldByProcesses(dir) (held, ok bool)`.
  - `ScanHolders()` (production entry: bound from the single env reader) and
    `ScanHoldersWithin(budget)` (the probe/test entry: an explicit deadline, `0` = none).
  - `LiveHolders()` is kept as a deprecated one-line wrapper for non-destructive readers;
    every production caller was moved to `ScanHolders()`.
- **Truncation is carried in the TYPE, so a caller cannot skip it silently.**
  - `HeldByProcesses(dir, holders []string) bool` — the package-level function that a
    delete arm called — is now unexported as `pathHeldByProcesses`: it still answers about
    paths only, but a caller can no longer reach it without having been asked whether the
    scan finished (Rule 15). The delete arms call `scratch.HolderVeto(dir, scan) string`,
    which renders **two different sentences for two different facts**:
    `HolderVetoReason` ("held by a live process (cwd/fd/exe/maps)") versus
    `HolderScanWithheldReason()` ("the holder scan was TRUNCATED at its wall-clock bound
    before it finished the process table — an unscanned process could hold it, so it is
    kept; nothing was observed holding it").
- **Fail-closed at every delete predicate.** `unheldBatch` (the shared delete predicate
  reached by the base, queue, residue, stub and profile arms), the sandbox arm, the
  worktree-sweep arms and the internal/tools arms all keep the candidate when the scan is
  truncated. `unheldBatch` checks `Truncated` **first**, so no later rung can authorise a
  delete on a scan that never finished.
- **No second /proc probe under truncation.** `worktreeStubHeld` returns the disclosure
  instead of running its per-directory fallback probe — that fallback would pay a second
  full process-table scan per candidate, which is the cost the bound exists to cap.
- **Report lines tell the truth.** `baseResidueKeepReason`, `profileResidueReason`,
  `entryVerdictInNamespace` (via the arm's own `holderEvidence`) and the chat arm's
  `worktreeSweepChatHolderPhrase` now print the truncation sentence instead of claiming a
  holder that was never observed. `worktreeSweepChatHolderPhrase` was added exactly so the
  chat rung cannot dress the truncation in the session-liveness sentence.
- **Configuration documented in the same commit** (Rule: `CONFIG.md` + the gate):
  a `CONFIG.md` §2c row and an `internal/config/settings.go` catalog row, so the knob has
  a `gopi --help-env` face rather than being an undocumented env var.
  `configDocFlagRowCount` 71 → 72 with its reason recorded in the constant's comment.
- **Regression tests** (`internal/scratch/holder_scan_test.go`): the default/`0`/positive/
  malformed contract of the env reader; the expired-deadline truncation; and the
  delete-path pair — the SAME fixture judged with a complete scan (collectable, the
  non-vacuous control) and with a truncated one (kept, reason asserted to disclose the
  truncation and to NOT contain `EntryKeepLiveHolder`).

## Not Done (honest)

- **The `ProcMapsReadLimit` (4 MiB) per-file cap does not set `Truncated`.** A process whose
  maps file exceeds the cap has mappings left unread, and the disclosure does not say so.
  This is stated in `ScanHoldersWithin`'s doc comment rather than fixed: the direction is
  fail-CLOSED for nothing (the cap can only *lose* holder evidence, never invent it), so it
  cannot cause a live directory to be deleted; the cost is a directory whose only holder
  evidence lies past the cap being collected. Widening `Truncated` to cover it would make
  the flag fire on hosts that are entirely healthy and drown the signal this epic adds.
- **`LiveHolders()` still exists** (deprecated) and still performs a bounded scan. It is the
  one remaining path where a caller receives paths with no completeness information — kept
  so a read-only caller is not forced to change shape. Every production delete caller is
  off it; no test pins "nobody calls it for a decision", so a future delete caller could
  still reach for it.
- **No gate pins the type change.** The compiler is the gate (a `[]string` will not satisfy
  `HolderScan`), which is exactly why it was done as a type change — but nothing prevents a
  future author from adding a new caller that ignores `Truncated` after the fact.
- **The env knob cannot express a sub-second bound, so an operator cannot force the
  truncation arm through the CLI on a warm host.** The knob is whole seconds (by design: the
  default is 8 s), and a warm-cache scan here costs ~0.6 s, so `GOPI_HOLDER_SCAN_DEADLINE_SEC=1`
  completes rather than truncating — the smallest positive value is above the measured cost.
  The truncation PATH is therefore proven by the unit tests and RED proof, not by a CLI run;
  the CLI was exercised for the deadline values it can express (`0`, `1`, `bogus`).
- **No measurement of the 8 s bound under real contention at the truncation edge.** Rule 27
  forbids generating load to reach it, so the truncation arm is driven by an
  already-expired deadline (the same branch a loaded host takes). The honest statement is
  that the bound was measured *not* to fire on a healthy host, not that it was measured
  firing at 8 s of real scanning.

## Verification

- **Build**: `CGO_ENABLED=0 go build -o .evid/gopi ./cmd/agent/` → OK (no output).
- **Tests** (branch-scoped, via the declared entry point):
  `make test-budget PKGS='./internal/scratch ./internal/tools ./internal/config ./internal/testutil ./internal/standards'`
  and the focused set
  `go test -run 'Holder|LiveHolders|ProfileRoot|ScratchGC|Sandbox' ./internal/scratch/ ./internal/tools/`
  → `ok github.com/nick/gopi/internal/scratch 247.064s` · `ok github.com/nick/gopi/internal/tools 359.165s`
  (the final branch-scoped run, on a clean tree and after the master sync; the earlier
  focused runs gave 23.1s / 61.1s for the same packages). `go test -run TestConfigDoc
  ./internal/standards/` → `ok`.
- **Ran** — the surface actually executed, verbatim:
  1. `go run ./internal/scratch/zzprobe` (a scratch-main probe that calls the production
     entry points directly; removed after measuring) — output in
     `~/.gopi/sessions/01a12ee4-64a9-7059-aa82-f22c3ff59e9d/evidence/probe-baseline-unbounded.txt`
     and `.../probe-bounded-vs-unbounded.txt`.
  2. `./.evid/gopi tool scratch-gc sandboxes --dry-run`
  3. `./.evid/gopi tool scratch-gc profile --root ~/.gopi --explain`
  4. `GOPI_HOLDER_SCAN_DEADLINE_SEC={0,1,bogus} ./.evid/gopi tool scratch-gc sandboxes --dry-run`
  5. `./.evid/gopi --help-env | grep HOLDER_SCAN`
- **Result**:
  - **Direct measurement on this host** (8 cores, load average 0.99–7.92, 478 `/proc` PIDs):
    unbounded scan **1455–1636 ms**, **56 208–56 263 paths** per call, `truncated=false`;
    default-bounded scan **1353–1435 ms**, **56 259–56 263 paths**, `truncated=false` — the
    8 s default does **not** truncate a healthy scan (nor one at load ~7.9), which is the
    property that keeps this from turning every sweep on the machine into a silent
    keep-everything pass.
  - `--help-env` renders the new row (`GOPI_HOLDER_SCAN_DEADLINE_SEC` with its description).
  - `GOPI_HOLDER_SCAN_DEADLINE_SEC=bogus` prints
    `scratch: GOPI_HOLDER_SCAN_DEADLINE_SEC="bogus" is not a whole number of seconds — using the default 8s`
    and the command exits 0 — a malformed value is *not* an off switch.
  - `scratch-gc profile --root ~/.gopi --explain` (the verb whose `ProfileRootReport` was
    in the original timeout stack) completes in **0.59 s** wall clock end to end.
- **RED proof** (the guard removed, the new tests must fail):
  `.../evidence/redproof-truncation-removed.txt` —
  `--- FAIL: TestTruncatedHolderScanNeverAuthorisesADelete … a TRUNCATED holder scan
  authorised a delete — 'not scanned' was read as 'not held'` and
  `--- FAIL: TestHolderScanTruncationKeepsTheProfileArm … a truncated scan made a profile
  residue child collectable`. Restored → green (`.../evidence/greenproof-after-restore.txt`).

## Evidence (self-filed)

- Baseline (unbounded) probe:
  `proc_entries=478` · `scan=1 elapsed_ms=1529.3 paths=56257` · `scan=2 1513.4/56257` ·
  `scan=3 1455.4/56251` — the three calls the acceptance clause asks for.
- Bounded-vs-unbounded probe: `DEFAULT scan=1 elapsed_ms=1353 paths=56263 truncated=false`
  … `UNBOUNDED scan=1 elapsed_ms=1628 paths=56210 truncated=false`.
- `go test -count=1 -run 'Holder|LiveHolders|ProfileRoot|ScratchGC|Sandbox' ./internal/scratch/ ./internal/tools/`
  → exit 0, both packages `ok`.
- `go test -run TestConfigDoc ./internal/standards/` → `ok` after the `CONFIG.md` row and the
  catalog row landed (it was red between the two: the gate demanded a catalog row for any
  §2c row, so the doc row alone was refused — the gate working as designed).
- `git status --porcelain` in this worktree: the commit below lands every file this branch
  touches, and the tree is then clean (no untracked evidence directory is left behind —
  the probe's measurements live in the run's session directory, not in the repo).

## Deliberate design decisions (why it is shaped this way)

1. **A type change, not a parameter.** The epic asked for "a carrier that makes truncation
   mandatory at compile time". Changing `holders []string` to `holders HolderScan` across
   `internal/scratch`, `internal/tools` and `internal/testutil` (~30 signatures, 24 call
   sites) means every existing caller had to be *edited*, and the only two call shapes the
   compiler accepts are "answer the holder question through `HeldByProcesses`/`HolderVeto`,
   which handle truncation" or "handle `Truncated` explicitly". Nothing was renamed to
   hide the migration: the compiler reported each site.
2. **One liveness verdict (memory #2632).** No new probe was added. `HolderScan` wraps the
   SAME `/proc` pass; the truncation flag rides the existing verdict instead of introducing
   a second opinion about liveness.
3. **FAIL-CLOSED is about disk, not about correctness of the answer.** With truncation the
   sweeps keep what they could not check, so a host whose scan is truncated reclaims
   nothing rather than reclaiming something it never looked at. The disclosure states the
   fact so an operator sees the degradation instead of inferring it from a falling
   `removed=` count.
