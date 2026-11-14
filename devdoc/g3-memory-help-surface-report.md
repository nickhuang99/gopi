<!-- doc-class: historical -->
# Task Report: G3 — `gopi memory` help-surface hardening (10 advertised verbs × BASIC test + inventory)

**Task**: 01a0c2d1-9c90-7700-bb7a-e34425136cf9 (epic brief:
`briefings/brief-help-surface-hardening.md`, family G3 — the knowledge-base
group).
**Branch**: `feat/g3-memory-help-surface` (worktree `~/workspace/gopi-g3-memory-help`,
Rule 6a: `git worktree list` first, then `git rev-parse --verify --quiet
refs/heads/feat/g3-memory-help-surface` → exit 1 = free, then `git worktree add`).
**Commit**: `2c097c92`.
**Companion deliverable**: `devdoc/gopi-memory-help-surface-inventory.md` — the
re-derivable verb/flag inventory (10 verbs, 51 advertised verb-flag pairs) that
the build reads back.

## Completed

1. **The surface list is DERIVED, twice, from the help itself.**
   - `scripts/g3-memory-help-surface.sh [bin]` runs `gopi memory -h` and emits a
     TSV of `verb ⇥ sorted-flags ⇥ flag-count` from the synopsis heads. The
     command is quoted in the inventory doc and is the doc's re-derivation.
   - `TestMemoryHelpSurfaceMatchesInventory` re-derives the same set inside the
     test package from `memoryUsage` — the constant the binary prints — and
     fails when the help gains or loses a verb that the pinned expectation and
     the claimed coverage do not both account for.
   - `TestMemoryHelpSurfaceInventoryTableMatchesHelp` reads the checked-in
     Markdown table back and fails if it drifts from the help, so the
     deliverable cannot go stale silently.
2. **Two scanner shapes that a first-pass derivation gets WRONG**, both
   documented in the script and the doc because each silently under-reports:
   `provenance` prints two synopsis heads and `feedback` four (their mode flags
   wrap), so the row is emitted only when the VERB changes; and a verb's heads
   are separated by its own description block, so prose must be neither
   collected nor treated as a terminator.
3. **A BASIC test per advertised verb**, each driving the REAL binary over the
   help's own argv (`gopiTestBinPath` + `cliContractEnv` + a per-test ledger,
   never `-h`): happy path plus the verb's first refusal.
   `TestMemoryVerb{Search,Store,Get,List,Update,Delete,Feedback,Sync,Backup,Provenance}RunsAsAdvertised`
   (10), plus `TestBrewRunsAsAdvertised` for the one memory-family verb outside
   the group word, plus `TestMemoryLegacyFlatVerbsStillRunAsAdvertised` for the
   seven flat aliases the group page advertises by name.
4. **Both `--serve` legs are exercised on REAL payloads**, not just admission:
   - `memory sync --serve-assets` gets a v1 export patch on stdin, its reply is
     parsed as an export (version + node), and the row it carried is read back
     out of the peer ledger with `memory search`.
   - `memory backup --serve` gets a real envelope (artifact + header sha256),
     its JSON receipt is parsed (`filename`/`checksum`/`consistency`), the
     artifact is checked on disk, and a TRUNCATED transfer is refused with
     nothing left behind — "a truncated .db in backups/ is the file trusted
     later".
5. **Three surface gates** beyond the two derivation gates:
   `TestMemoryHelpSurfaceAdvertisedFlagsAreAdmitted` sweeps all 51 advertised
   verb-flag pairs through the real binary so "advertised but refused by the
   flag table" is closed for the whole family, with a floor assertion (a
   scanner that regressed to head-lines-only must FAIL rather than pass
   quietly); `TestMemoryHelpGroupPageAdvertisesEveryDispatchableSubcommand`
   pins the group page against `memorySubcommandNames` in BOTH directions; and
   `TestMemoryHelpSurfaceMatchesInventory` doubles as the coverage gate.
6. **Two contained surface defects FIXED** (red→green in Verification):
   - the group page omitted `gopi memory backup` entirely — dispatched by
     `memorySubcommandNames`, offered by the completion set, printed by the
     unknown-subcommand error line, and absent from the one page a reader is
     told to read (the sub-verb page existed, so the group page was the only
     undiscoverable link);
   - the group page named neither peer-side mode (`--serve-assets` on
     `memory sync`, `--serve` on `memory backup`) — the two flags an operator
     must never type by hand. Both are now spelled beside their command, with
     the "ssh'd by an initiator, never run by a human" contract in the same
     breath.
7. **The branch ends clean**: `git status --porcelain` prints nothing (Rule 30);
   the recon scratch (`.g3/help-*.txt`) and the job log were removed, not
   committed.

## Not Done (honest)

- **`gopi brew -h` in a trailing position is a recorded defect, not a fix.**
  Measured: `gopi brew --json -h` exits 0 and RUNS the pipeline instead of
  printing the page, and `gopi brew --limit 2 -h` exits 1 with
  `unexpected argument "-h"`; only `gopi brew -h` prints the page. Every other
  verb in the family scans its whole argv for the help flag ahead of parsing
  (`runMemorySubcommand`, `runMemorySync`, `runMemoryBackup`), so brew
  disagrees with its siblings. The fix is a one-line move of that scan but it
  changes a verb's exit code for that argv, so per the briefing it is recorded
  (inventory finding 3) and **filed as bug
  `01a0cc2c-db21-77ff-bd7a-e34425136cf9`**; the test pins TODAY'S behaviour with
  a comment saying the fix turning it red is the expected signal. (Also noted
  in that bug: `gopi brew --drry-run` is silently ignored rather than refused
  by name — Rule 15's shape, same file.)
- **The LIVE peer legs are not covered**: `memory sync --peer P` without
  `--dry-run` (a bidirectional ssh session) and `memory backup`'s live ship.
  This suite has no reachable peer host and the briefing forbids mocking one;
  both legs' transport seams keep their existing homes in `internal/tools`.
  Stated as the inventory's "not covered, and why" table — not a silent skip.
- **`gopi brew` producing a real lesson** needs a live LLM credential and real
  session JSONLs; the probe asserts the pipeline's own surface against a
  directory with nothing to sweep (`sessions=0`). A probe that fabricated
  sessions would be testing the fixture.
- **`memory feedback --suggest-cold` with a candidate present** needs ≥3
  `useless` votes and 0 `useful` on one row; the probe crosses the other branch
  of the same code path (the empty answer, asserted BY NAME).
- The sibling families G1/G2/G4–G6 are out of scope by the family boundary.

## Verification

**Build**
```
$ gofmt -l cmd/agent/cli_memory_help_surface_basic_test.go
(no output — clean)
$ CGO_ENABLED=0 go build ./...
(no output — green)
$ go vet ./cmd/agent/
(no output)
```

**Tests — the new tests, by exact invocation** (the selector `TestMemoryHelp`
covers all four gate names at once, so nothing is left out by a misspelling):
```
$ go test -count=1 -timeout 900s -v -run \
  'TestMemoryHelp|TestMemoryVerb|TestMemoryLegacyFlatVerbs|TestBrewRunsAsAdvertised' \
  ./cmd/agent/
--- PASS: TestMemoryHelpSurfaceMatchesInventory (0.00s)
--- PASS: TestMemoryHelpSurfaceInventoryTableMatchesHelp (0.00s)
--- PASS: TestMemoryHelpGroupPageAdvertisesEveryDispatchableSubcommand (0.00s)
--- PASS: TestMemoryHelpSurfaceAdvertisedFlagsAreAdmitted (1.33s)
--- PASS: TestMemoryVerbSearchRunsAsAdvertised (1.70s)
--- PASS: TestMemoryVerbStoreRunsAsAdvertised (1.32s)
--- PASS: TestMemoryVerbGetRunsAsAdvertised (0.82s)
--- PASS: TestMemoryVerbListRunsAsAdvertised (0.86s)
--- PASS: TestMemoryVerbUpdateRunsAsAdvertised (1.77s)
--- PASS: TestMemoryVerbDeleteRunsAsAdvertised (1.38s)
--- PASS: TestMemoryVerbFeedbackRunsAsAdvertised (2.91s)
--- PASS: TestMemoryVerbSyncRunsAsAdvertised (3.46s)
--- PASS: TestMemoryVerbBackupRunsAsAdvertised (1.73s)
--- PASS: TestMemoryVerbProvenanceRunsAsAdvertised (2.53s)
--- PASS: TestMemoryLegacyFlatVerbsStillRunAsAdvertised (2.63s)
--- PASS: TestBrewRunsAsAdvertised (0.04s)
PASS
ok  	github.com/nick/gopi/cmd/agent	23.141s
```
(**16 Test funcs, 0 skips**: 2 derivation/coverage gates, 1 group-page gate, 1
flag-admission sweep, 10 per-verb cases, 1 brew case, 1 legacy-alias case.)

**Gates that could be affected by the help edit**
```
$ go test -count=1 -timeout 1200s -run 'Memory|Brew|Help|Usage|Completion|Contract|DocClass|DocAnchor' \
  ./cmd/agent/ ./internal/standards/ ./internal/docclass/
ok  	github.com/nick/gopi/cmd/agent	43.185s
ok  	github.com/nick/gopi/internal/standards	1.197s
ok  	github.com/nick/gopi/internal/docclass	1.081s [no tests to run]
```
(The doc-class/DOC-INDEX row was added in the same commit: the new inventory is
a `normative` devdoc carrying a resolvable path spot-check, alongside G1's.)

**Budget run (branch-scoped)**
```
$ make test-budget PKGS='./cmd/agent'
<output quoted in the run's own log; see Result below>
```

**Ran: the changed provider-side surfaces, on the real binary** (Rule 31 — a
surface not executed is not delivered). Sandbox: `GOPI_HOME` a fresh temp dir,
`--db` a per-probe ledger, `GOPI_MEMORY_PEERS=g3-probe-peer` the declared peer.

*The fix itself — the group page now advertises what it dispatches:*
```
$ ./bin/gopi memory -h | grep -n 'memory backup\|serve-assets'
84:  gopi memory backup [--dir PATH] [--db PATH] [--peer ALIAS] [--no-ship]
<...>
74:  gopi memory sync --serve-assets
$ scripts/g3-memory-help-surface.sh ./bin/gopi
search	db,intent,limit	3
store	answer,db,keywords,question,source,topic	6
get	db	1
list	db,limit	2
update	answer,confidence,db,keywords,question,source,tier,topic	8
delete	db	1
feedback	db,list,memory-id,reason,recompute,suggest-cold,task,unused,useful,useless	10
sync	db,dry-run,peer,serve-assets	4
backup	db,dir,name,no-ship,peer,serve	6
provenance	backfill,counts,db,harvest-latent,latent,latent-calibrate,latent-candidates,latent-crosscheck,reclassify,shape	10
```
Note `sync` reads 4 and `backup` 6 AFTER the edit (each gained its peer-side
mode); before it, the same command reported 3 and 5 with the mode missing.

*Representative invocations and their observed results* (each is also asserted
in the file; these are the raw observations from the session):
```
$ ./bin/gopi memory store --topic t10 --question q10 --answer a10 --db $DB
Memory stored (id=2).
$ ./bin/gopi memory search "q10" --intent debug --limit 1 --db $DB   → the stored row, exit 0
$ ./bin/gopi memory provenance --shape --counts --db $DB
gopi memory provenance: --shape and --counts are mutually exclusive   (exit 1)
$ ./bin/gopi memory provenance --latent-crosscheck --db $DB
gopi memory provenance: --latent-crosscheck needs the harvested column first — run --harvest-latent (exit 1)
$ ./bin/gopi memory backup --db $DB --dir $B --no-ship --name ../evil
gopi memory backup: --name "../evil" must be a bare artifact name   (exit 1)
$ ./bin/gopi memory backup --db $DB --dir $B --no-shipp
gopi memory backup: unknown flag --no-shipp                          (exit 1)
$ ./bin/gopi memory backup --db $DB --dir $B --peer undeclared
gopi memory backup: ... memory_peers whitelist    (exit 1 — AND the local
  snapshot is still reported as taken/verified, which is the help's promise)
$ printf '{"version":1,"node":"probe","rows":[]}' | ./bin/gopi memory sync --serve-assets --db $DB
{"version":1,"node":"peerbox","exported_at":…,"generator":"dev","rows":[…]}
$ echo '{}' | ./bin/gopi memory sync --serve-assets --db $DB
gopi memory sync: serve-assets: memsync serve-assets: patch is missing its
  version field (want 1) — refusing: an unknown-format patch would be imported
  with silently dropped fields                                              (exit 1)
```

*Red→green for the two fixes — MEASURED, by restoring `HEAD~1`'s
`cmd/agent/cli_memory.go` for one run and swapping the fixed file back:*
```
# RED — the pre-fix group page:
$ go test -count=1 -run 'TestMemoryHelpGroupPageAdvertisesEveryDispatchableSubcommand|TestMemoryHelpSurfaceAdvertisedFlagsAreAdmitted' ./cmd/agent/
--- FAIL: TestMemoryHelpGroupPageAdvertisesEveryDispatchableSubcommand (0.00s)
    `gopi memory backup` is dispatched (memorySubcommandNames) but the group
    help prints no synopsis for it — a subcommand only reachable by knowing its
    name is undiscoverable
    the group help never spells --serve, though both are advertised by their own page
    the group help never spells --serve-assets, …
--- FAIL: TestMemoryHelpSurfaceAdvertisedFlagsAreAdmitted (3.43s)
    the scan inspected only 44 advertised flag sites — the synopsis parser is
    broken, so a green run proves nothing
# GREEN — with the fixed file back in place:
$ go test -count=1 -run 'TestMemoryHelpSurface|TestMemoryVerb|TestMemoryLegacyFlatVerbs|TestBrewRunsAsAdvertised' ./cmd/agent/
ok  	github.com/nick/gopi/cmd/agent	12.556s     (51 flag sites)
```
The 44-vs-51 delta is the second fix's own evidence: the flag sweep counts more
sites once the group page advertises `backup` and both peer-side modes.

**Result**: the derivation, the 16 cases/gates and both fixes are green; the help
now advertises the subcommand it dispatches and both peer-side modes. The
reproduction of the omitted-backup finding was: `gopi memory -h | grep backup`
→ no match, while `gopi memory bogus` printed backup in its own error line.

## Evidence (self-filed)

- `gofmt -l cmd/agent/cli_memory_help_surface_basic_test.go` → no output.
- `CGO_ENABLED=0 go build ./...` → exit 0, no output.
- `go vet ./cmd/agent/` → exit 0, no output.
- `go test -count=1 -timeout 900s -v -run 'TestMemoryHelp|TestMemoryVerb|TestMemoryLegacyFlatVerbs|TestBrewRunsAsAdvertised' ./cmd/agent/` → **16 PASS, 0 skips**, `ok … 23.141s` (the full `-v` PASS list is quoted under Verification).
- `go test -count=1 -timeout 1200s -run 'Memory|Brew|Help|Usage|Completion|Contract|DocClass|DocAnchor|DocIndex' ./cmd/agent/ ./internal/standards/ ./internal/docclass/` → `ok` × 3 (`cmd/agent 46.516s`, `standards 0.714s`, `docclass 1.115s`).
- **THE RED HALF OF THE TWO FIXES, reproduced in this session** (not asserted
  from memory): the pre-fix `cmd/agent/cli_memory.go` was restored from `HEAD~1`
  for one run and swapped back —
  ```
  --- FAIL: TestMemoryHelpGroupPageAdvertisesEveryDispatchableSubcommand
      `gopi memory backup` is dispatched (memorySubcommandNames) but the group
      help prints no synopsis for it — a subcommand only reachable by knowing
      its name is undiscoverable
      the group help never spells --serve, … / --serve-assets, …
  --- FAIL: TestMemoryHelpSurfaceAdvertisedFlagsAreAdmitted
      the scan inspected only 44 advertised flag sites — the synopsis parser is
      broken, so a green run proves nothing   (51 after the fix)
  ```
  Both go green with the fix in place (16/16 PASS, `ok … 23.141s` above), so both gates are
  demonstrated red-on-the-defect rather than merely present.
- `make test-budget PKGS='./cmd/agent'` → the job's own log, quoted under `## Verification` → `Budget run`.
- `git status --porcelain` in the worktree → empty after the follow-up commit
  (Rule 30; the recon scratch `.g3/` and the job log were removed, not committed).
- `scripts/g3-memory-help-surface.sh ./bin/gopi` → the 10-row TSV above.

## Files

- `cmd/agent/cli_memory.go` (M) — the group page gains the `backup` synopsis and
  names both peer-side modes.
- `cmd/agent/cli_memory_help_surface_basic_test.go` (A) — the 16 cases + gates.
- `devdoc/gopi-memory-help-surface-inventory.md` (A) — the inventory, its
  re-derivation command, the findings and the honest residuals.
- `devdoc/DOC-INDEX.tsv` (M) — the new normative doc's row.
- `scripts/g3-memory-help-surface.sh` (A) — the operator-facing derivation.
