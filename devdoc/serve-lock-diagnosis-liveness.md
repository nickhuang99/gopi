<!-- doc-class: normative -->
# Task Report: the serve-lock refusal diagnosis — liveness and identity, not a pid

**Branch:** `fix/serve-lock-diagnosis-liveness` (worktree `~/workspace/gopi-lock-diag-live`, base `17cec86b`)
**Recovery:** `git show fix/serve-lock-diagnosis-liveness:devdoc/serve-lock-diagnosis-liveness.md`

---

## Completed

### 1. The refusal now answers "is the holder ALIVE?" and "what does the KERNEL say
about the inode I was refused on?" — `cmd/agent/cli_tasks.go`

The CONTENTION arm of `lockAndVerifyIdentity` (the only arm that may say
"another merge daemon is already running") now appends two diagnostic lines to
the refusal, built **from the acquired descriptor before it is closed**:

```
lock-stray-holder: pid 1234 on an unlinked copy of /path/serve.lock (ALIVE)
lock-kernel-records: 1 record(s) on the refused inode 29125061: 3: FLOCK ADVISORY WRITE 2368392 103:05:29125061 0 EOF
```

- **`lock-stray-holder:`** = `tools.OrphanLockHolder(lockPath)` **with its
  liveness** from `tools.PidAlive` (`ALIVE` / `DEAD`). The pid alone was the
  defect: an inherited/duplicated descriptor keeps a flock alive past its
  owner's death, so a printed pid can name a corpse. "No stray holder" is
  printed explicitly (`none (no process holds an unlinked copy of …)`), never a
  blank.
- **`lock-kernel-records:`** = the `/proc/locks` records matched on the inode
  **`fstat` of the acquired fd** — the inode the flock was actually refused on,
  *not* what the PATH currently names. That distinction is the epic: after a
  replacement the two are different files, and a path join reports "no records"
  for a lock that is very much held. The line also prints the inode it joined
  on, so an operator can see the identity the refusal rested on.
- **Rule 15 — no silent failure:** an **unreadable** `/proc/locks` renders as
  `UNREADABLE (<err>) — no record evidence either way`, and an unresolvable
  descriptor inode as `UNKNOWN inode (<why>)`; neither can be printed as
  `none`, which would read as evidence of a free lock.

Every label, key and answer word is a named constant in the file's const block
(`serveLockStrayHolderPrefix`, `serveLockKernelRecordsPrefix`,
`serveLockStrayHolderFormat`, `serveLockHolderLive`/`serveLockHolderDead`,
`serveLockKernelNoRecords`, `serveLockKernelUnreadableFormat`,
`serveLockKernelNoInodeFormat`, `serveLockLineEnd`) and the line is composed by
one constructor (`serveLockDiagnosisLine`) — Rules 1b/13/14: referenced, never
re-typed.

### 2. The table reader is single-sourced — `internal/tools/merger_status.go`

Two small EXPORTED accessors over machinery that already existed, so the
diagnosis cannot drift from the status/stop surfaces (Rules 1b/14):

- `tools.KernelFlockRecords(ino) ([]string, error)` — the FLOCK rows for an
  inode, routed through the package's ONE `/proc/locks` parser
  (`procLockTableRead` + the same field layout as `lockTableHolds`). The
  **error return is what makes Rule 15 mechanical**: nil-error + empty slice =
  "the kernel says nobody holds it" (evidence); non-nil = "no evidence", which
  is a different sentence.
- `tools.InodeOfFileInfo(fi) uint64` — exposes the existing private `inodeOf`,
  so the join key has one definition.

A second `/proc/locks` reader in `cmd/agent` would have been a second answer to
"who holds it?" — hence the export rather than a local parse.

### 3. What was RE-IMPLEMENTED FRESH vs. cherry-picked (the briefing's question)

The two helper names the briefing names exist **only** on `fix/serve-lock-race`:

| name | this task | why |
|---|---|---|
| `serveLockErrnoIsContention` | **NOT included** | It guards the NON-contention arm (a flock failure that is not `EWOULDBLOCK` must not be reported as a holder). That is a different defect from this epic's (a pid with no liveness evidence), and it is a live deliverable still on `fix/serve-lock-race` — noting it here keeps it visible, and merging it here would be a Rule-23 style second copy of another branch's work. |
| `serveLockHeldError` | **NOT included** | Same reason: it is the constructor the errno fix introduced. |

Consequently the CONTENTION arm keeps master's own shape
(`fmt.Errorf("%w: %w", errServeLockHeld, fmt.Errorf(serveLockHeldRefusal, …))`)
and the diagnosis is appended to it. **No code was cherry-picked**: the
diagnostic is written fresh against master's production path, so this branch
carries no part of that branch's history (Rule 23).

**A fact the briefing's own wording assumed, corrected:** master's
`lockAndVerifyIdentity` has **no "existing fd-holder dump"** to keep — no
descriptor scan exists on this path on master or on `fix/serve-lock-race` (the
fd/`(deleted)`-target scan lives in `internal/tools.OrphanLockHolder`, which the
new line now surfaces here). Nothing was removed; the refusal text plus the two
new lines is the whole surface.

### 4. Tests (new, `cmd/agent/serve_lock_diagnosis_liveness_test.go`)

- `TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness` — the epic's
  acceptance test, driving the **production** `acquireServeLock` with a
  **REPLACED** lock file, refused by the CONTENTION arm, asserting the refusal
  names the stray holder (with `ALIVE`) and the kernel records for the refused
  inode. The rig is two re-exec'd helper processes (owner → ExtraFiles leak →
  owner dies `os.Exit(0)`), and it **proves its own premises** with bounded
  polls before asserting: the kernel really records the flock on this host, the
  recreated path really got a different inode, `OrphanLockHolder` really sees a
  live stray, and the flock's owner is really dead. A host where the mechanism
  differs reports it **unproven**, never passes on an empty rig.
- `TestServeLockRefusalDiagnosisReadsTheDescriptorNotThePath` — pins the
  IDENTITY half (`fstat` vs path) independently of the rig: it holds a real
  flock on a descriptor, replaces the path, and requires the join on the
  descriptor's inode (the two coincide end-to-end in `acquireServeLock`, so
  this is the test that can see the difference).
- `TestServeLockLivenessNoteReportsADeadHolder` — the `DEAD` word is measured on
  a genuinely reaped pid, not a constant that happens to be printed.

No test was deleted, skipped or weakened.

### 5. The executed red proofs (mutant → FAIL → revert → PASS)

Each mutation was applied to `cmd/agent/cli_tasks.go`, built, run, then
reverted; the transcripts are committed under `.evid/`.

**Mutation A — the diagnosis is computed and discarded** (pre-change wiring):

```go
-		diagnosis := serveLockRefusalDiagnosis(f, path)
-		return nil, fmt.Errorf("%w: %w\n%s", errServeLockHeld, …Holder(path)), diagnosis)
+		_ = serveLockRefusalDiagnosis(f, path)
+		return nil, fmt.Errorf("%w: %w", errServeLockHeld, …Holder(path)))
```

```
$ go test ./cmd/agent/ -run TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness -count=1 -v
    serve_lock_diagnosis_liveness_test.go:339: the refusal carries no "lock-stray-holder:" line:
--- FAIL: TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness (1.46s)
FAIL	github.com/the operator/gopi/cmd/agent	2.100s
```

**Mutation B — the pid is listed, the liveness is dropped**:

```go
+		_ = serveLockLivenessNote(stray) // MUTANT B
-		…serveLockStrayHolderFormat, stray, path, serveLockLivenessNote(stray)))
+		…serveLockStrayHolderFormat, stray, path, ""))
```

```
    serve_lock_diagnosis_liveness_test.go:348: the stray-holder line must report the holder's LIVENESS ("ALIVE") — a bare pid is the artifact this diagnostic removes
--- FAIL: TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness (1.55s)
```

**Mutation C — the table is joined on the PATH instead of the descriptor**:

```go
-	fi, err := f.Stat()
+	fi, err := os.Stat(f.Name()) // MUTANT C
```

```
    serve_lock_diagnosis_liveness_test.go:411: the kernel records must be joined on the DESCRIPTOR's inode 29381012
    serve_lock_diagnosis_liveness_test.go:417: the kernel records were joined on the PATH's inode 29381502 — a replaced lock file makes those two different files and the refusal happened on the descriptor's (29381012)
--- FAIL: TestServeLockRefusalDiagnosisReadsTheDescriptorNotThePath (0.04s)
```

**Revert → PASS** (`.evid/green-after-revert.txt`):

```
--- PASS: TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness (1.48s)
--- PASS: TestServeLockRefusalDiagnosisReadsTheDescriptorNotThePath (0.05s)
--- PASS: TestServeLockLivenessNoteReportsADeadHolder (0.64s)
ok  	github.com/the operator/gopi/cmd/agent	2.976s
```

---

## Verification

- Build: `go build ./...` — OK. `gofmt -l cmd/agent internal/tools` lists exactly the
  files pristine master lists (`cli_tasks_advert_gate_test.go`,
  `daemon_scope_unit_test.go`, `usage_test.go`, `gitblame.go`, `symbol_test.go`,
  `task_deliverable_worktree.go`) — none of this change's files appear, i.e. no
  new unformatted file.
- Targeted (the epic's own surface): 3 PASS —
  `TestServeLockRefusalDiagnosisNamesTheStrayHolderAndItsLiveness`,
  `TestServeLockRefusalDiagnosisReadsTheDescriptorNotThePath`,
  `TestServeLockLivenessNoteReportsADeadHolder` (transcripts in
  `devdoc/assets/serve-lock-diagnosis-liveness/`).
- `internal/tools` (the package that gained the two exports), new guards:
  `TestKernelFlockRecordsSelectsOnlyTheFlockOnThatInode`,
  `TestKernelFlockRecordsDistinguishesUnreadableFromEmpty`,
  `TestInodeOfFileInfoExposesTheStatKey` — PASS.
- Standards gates (Rules 16–21): `go test ./internal/standards/ -count=1` —
  `ok github.com/the operator/gopi/internal/standards 14.333s`.

### `go test ./cmd/agent/ -count=1` — NOT green, and NOT green on pristine master

The acceptance asked for green "where green on master `17cec86b`", so the
package was run on BOTH trees in the same window (a loaded host: load average
~10–12, other agents' suites running):

| tree | result | failing tests |
|---|---|---|
| pristine master `17cec86b` (`~/workspace/gopi-lock-diag-base`) | **FAIL** | **13** |
| this branch | **FAIL** | **10** |

The branch set is a **strict SUBSET** of the master set — the set difference
(branch-only failures, i.e. reds this change could have introduced) is **EMPTY**.
Three of master's failures went green on the branch run, which is the signature
of the flakiness rather than of a fix: they are session/cgroup/daemon-lifetime
dependent (`TestMergerDaemon_*`, `TestWorkerDaemon_*`,
`TestMergerFlock_DaemonLockStopsWithTheDaemon`, `TestUserBusSeam_*`,
`TestMergerSingleton_TwoConcurrentInvocations`), and each fails on master with
exactly the same message.

Evidence committed: `base-cmd-agent-fails.txt` / `branch-cmd-agent-fails.txt`
(the raw `--- FAIL:` lines), `base-cmd-agent-fail-names.txt` /
`branch-cmd-agent-fail-names.txt` (durations stripped, the form `comm` compares
— this is how the subset claim is checkable), and `base-cmd-agent-full.txt`.

`internal/tools` has the same story, re-measured on pristine master in the same
window (`job-7`): `TestSuiteScratchRootIsMemoryBacked`,
`TestScratchRootFilesystemClassification` (this host's scratch root is on ext4,
not tmpfs, so these suite-policy pins cannot pass here) and
`TestMemoryBaseOutOfPolicyRegistrationNamesTheRemedy` all fail on master too —
described in the report's own text as "pre-existing reds, attributed".

### Rule 27 compliance

No load generation anywhere in this change: the rig starts at most three short
helper processes that sleep or exit, every wait is a bounded poll on a condition
the test itself creates, and no `dd`/`fsync`/parallel-writer/pressure threshold
appears (`grep -nE "oflag=direct|iopress|pressure|conv=fsync" cmd/agent/serve_lock_diagnosis_liveness_test.go internal/tools/merger_status_kernel_records_test.go` — empty).

## Not Done

- The errno classifier (`serveLockErrnoIsContention` / `serveLockHeldError`)
  remains on `fix/serve-lock-race`; this branch re-implements the diagnosis
  fresh and deliberately does not carry that branch's work (Rule 23). If both
  branches merge, the CONTENTION arm's return statement needs one merge
  resolution (that branch restructures it into `serveLockHeldError`); the
  diagnosis is additive to either shape.
- The non-contention arm (a flock failure that is not `EWOULDBLOCK`) carries no
  diagnosis here — it is a syscall failure and is reported as one by the other
  branch's fix. This task's lines fire on the arm that claims a live holder,
  which is where a pid was being presented as evidence.
- **`go test ./cmd/agent/ -count=1` is not green on this host** — it is not
  green on pristine master either (13 failures there, 10 here, branch ⊂ master,
  reds in both cases from session/cgroup-dependent daemon tests). This is a
  host/suite condition, reported honestly rather than papered over; the served
  acceptance ("green where green on master") holds, and no new red is
  attributable to this change.
- **A pre-existing style-gate red, attributed, not fixed here:**
  `go test ./internal/stylegate/ -count=1` fails on pristine master `17cec86b`
  too — `stylegate ignored-error: 201 test hit(s), ceiling 182` and
  `stylegate errorf-verb: 9 production hit(s), ceiling 8`. After the change the
  branch reports **exactly the same 201/9 counts** (evidence:
  `devdoc/assets/serve-lock-diagnosis-liveness/stylegate-base.txt` vs
  `…-branch-after-ignore-reason.txt`), i.e. the new test files add **zero**
  violations. It did initially add one — the gate flagged
  `_ = syscall.Kill(pid, syscall.SIGKILL)` — and that site was corrected in the
  same change by carrying an explicit `//stylegate:ignore` reason rather than by
  loosening the gate.

## Commit

`9e5e94bc` on `fix/serve-lock-diagnosis-liveness` — `fix(cmd/agent): the serve-lock
refusal names the stray holder's LIVENESS, not just a pid` (4 files + 12 evidence
files; base `17cec86b`).
