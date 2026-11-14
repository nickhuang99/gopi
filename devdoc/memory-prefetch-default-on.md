<!-- doc-class: historical -->
# The typing-time memory prefetch is ON by default — the flip, the declaration test it forced, and the two real sessions that prove it

Task: `01a10edf-1b62-7590-b101-638447b3937f`. Branch: `task/01a10edf1b627590b101638447b3937f`.
Date: 2026-10-06. Supersedes the "**Default OFF**" state recorded in
`devdoc/memory-prefetch-executor.md` (epic child 3/3) and in the `gopi -h` row,
the `gopi chat -h` section, the settings catalog and `CONFIG.md` §2c.

---

## 0. What changed, in one paragraph

`memory_prefetch` — the read-only background retrieval that runs while you type
in `gopi chat` — resolved to **OFF** when nobody declared it. It now resolves to
**ON**. Silence is still `nil` (nobody asked) and the built-in default answers;
what changed is only *which answer* silence produces. Every way of turning it
off is unchanged and still exact: `--memory-prefetch=off`, `GOPI_MEMORY_PREFETCH=0`
(any falsey spelling), `{"memory_prefetch": false}` in settings.json — and one
of those three is now **the** way to get the pre-knob behaviour, because the
pre-knob behaviour is no longer what you get by saying nothing.

## 1. Why default-on — the measured basis

The gate was built complete (three channels, a read-only DSN, a debounce, a
differential re-fire, per-event JSONL, a segment-attributed give-up) and left
**disarmed**. Measured on this host, 2026-10-06:

```
find ~/.gopi/sessions -maxdepth 2 -name memory-prefetch.jsonl | wc -l   →  14
find ~/.gopi/sessions -maxdepth 1 -mindepth 1 -type d | wc -l           →  83071
```

One session in six thousand, and the long sessions that motivated the gate were
not among them. That is Rule 11's failure mode measured rather than asserted: a
capability whose only problem is that nobody armed it is a capability nobody
has, and its cost — the code every later reader has to understand — is paid
anyway. The decision is the operator's ("使用默认打开，除非用户要关"), and the
flip is the smaller half of this change; the other half is §2, which the flip
forced.

## 2. The declaration test — why the settings field became a pointer

`Config.MemoryPrefetch` was a plain `bool`. That was harmless **only while the
default was false**: an absent key and `{"memory_prefetch": false}` were the same
value and therefore the same answer, so no one could tell — or needed to tell —
whether the file had spoken.

Flipping the default splits those two apart, and the settings channel must now
distinguish them:

| settings.json | plain bool (before) | pointer (now) |
|---|---|---|
| key absent | false → OFF | `nil` → **the built-in default (ON)** |
| `{"memory_prefetch": false}` | false → OFF | `&false` → **OFF (declared)** |
| `{"memory_prefetch": true}` | true → ON | `&true` → ON (declared) |

So the resolution reads the **declaration** (nil vs non-nil), not the value, and
does it in exactly ONE place (`resolveMemoryPrefetch`, `internal/config/memory_prefetch.go`).
The `*bool` shape is the existing `ScratchGC` precedent, not an invention. The
settings channel remains the weakest of the three: env and flag both outrank it
in both directions, which is what keeps "I exported the variable for this run"
meaningful.

One more consequence, and it is the reason the pointer is the right shape rather
than a workaround: with a plain bool and a default-ON knob, `{"memory_prefetch":
false}` would have been **unrepresentable** — the off state would have existed in
the flag and the env and not in the file, i.e. a documented channel that cannot
express one of the two values.

## 3. The change, file by file

- **`internal/config/memory_prefetch.go`** — `memoryPrefetchDefault` is now `true`
  (Rule 1: the default is a named constant at the top of the knob's own file, and
  it is the only place it lives); `resolveMemoryPrefetch` tests the pointer for
  nil instead of the value, and the unreadable-settings message names the default
  it fell back to (`MemoryPrefetchDefaultClaim`) instead of typing "off"; the
  doc block carries the measured basis above and the pointer rationale.
  A new exported constant, **`MemoryPrefetchDefaultClaim`** (`"default ON"`) plus
  its sentence-opening twin **`MemoryPrefetchDefaultClaimOpening`** (`"Default ON"`),
  so the `gopi -h` row, the `gopi chat -h` section and the two tests that assert
  the default are one definition rather than four hand-typed claims (Rules 1b/13/14).
- **`internal/config/config.go`** — `MemoryPrefetch bool` → `*bool`, with the
  comment stating **why** it is a pointer and what nil means.
- **`internal/config/flags.go`** — the `--memory-prefetch` override publishes a
  pointer, so the CLI channel can still say `off` under a default-ON knob.
- **`internal/config/settings.go`** — the catalog row (the `gopi --help-env` /
  `gopi -h` source) says the default, built from the constant.
- **`cmd/agent/chat_prefetch_usage.go`** — the `gopi chat -h` section's precedence
  clause prints the constant.
- **`CONFIG.md` §2c** — the prose paragraph and the `memory_prefetch` row now say
  **default ON**, state the declaration test, and carry the measured basis and the
  `ScratchGC` pointer precedent. Both `read at` anchors were re-measured after the
  edit (`internal/config/memory_prefetch.go:124` / `:132`; the `cmd/agent` anchors
  `:140` / `:589` are unchanged — verified, not assumed).

## 4. The tests the flip reconciled (and why each had to move)

A default flip is exactly the change that turns a green suite into a suite that
**agrees with the old answer**; three of these were asserting the behaviour rather
than the default, and one was asserting the *absence* of a declaration by
accident:

- `TestPrefetchOffWritesNoRecord` asserted "no record is written when nothing is
  declared" with an **empty** settings file — under a default-ON knob that state
  is ON, so the test would have measured the ON path while claiming the OFF one.
  It now declares `{"memory_prefetch": false}`, which is the channel a user
  actually turns it off with. (This is the one test whose *subject* changed, and
  it changed because the previous subject stopped existing.)
- `TestMemoryPrefetchThreeChannelsResolveToOneSwitch` — the "nothing declared" row
  now wants `true`, and two rows were added for the direction that matters under
  the new default: `flag off beats the built-in default`, and the row names were
  rewritten to say what they are deviations **from** ("env off beats the settings
  declaration (on)").
- `chat_prefetch_config_test.go` — the flag-override test now asserts the pointer's
  nil-ness (a nil pointer would silently mean "undeclared", so `--memory-prefetch`
  must publish a **non-nil** true), and the catalog-help assertion takes the
  expected wording from `MemoryPrefetchDefaultClaim` instead of a literal.
- `internal/config/memory_prefetch_help_test.go` — the `gopi -h` wording guard
  takes the same constant; the reason it checks is unchanged ("the row must say
  what it does and which way it defaults").
- `TestPrefetchOffStartsNoGoroutineAndLeavesThePromptByteIdentical`,
  `TestPrefetchOnStartsExactlyOneLoopGoroutineAndStopReclaimsIt`,
  `TestPrefetchAdoptAppendsToMessageNeverToDisplay` and the `segments` suite were
  **untouched and still pass**: the OFF contract (no goroutine, byte-identical
  prompt) is not weakened by where the default sits, and those tests arm or
  disarm the knob explicitly precisely so they never depended on it.

## 5. A master-side collision, resolved by merging rather than duplicating

While this run was in flight, **master landed the help-discoverability half of
the same family** (`f649efd3`, task `01a10f00`) — it added
`cmd/agent/chat_prefetch_usage.go` and four help tests, and its prose carried
"Default OFF" in three places. Two branches writing the same help section is the
Rule 29/26 situation, and the resolution here was the cheap one: **merge master
into this branch** (`git merge master` → fast-forwardable, no conflicts) and
amend the *merged* text so the help half has exactly one author and the wording
cannot drift. Had I typed my own help section first, the merge would have
produced a duplicate surface and a test asserting one of them.

## 6. What a reader should NOT conclude

- This did not add a trigger, a daemon, a scheduler or a store: the mechanism is
  unchanged (in-process, typing-time, debounced), and the record still lives in
  the run's own session directory (Rule 35). Only the default moved.
- This did not change the gates inside `internal/memoryprefetch` (the 24-rune
  minimum, the ≥3-keyword rule, the 600 ms debounce, the differential re-fire).
  They are deliberate and were left alone; a default flip is not a licence to
  retune the thing being defaulted.
- This did not wire the `gopi chat --tools` face. That remains out of scope, for
  the architectural reason its own doc records (the `--tools` path seals its
  prompt through the shared agent loop, a different boundary).

## Completed

- `memoryPrefetchDefault` flipped to ON in the knob's own file; the resolution
  reads the settings **declaration** (nil vs non-nil) via `Config.MemoryPrefetch *bool`.
- `MemoryPrefetchDefaultClaim` / `MemoryPrefetchDefaultClaimOpening` are the single
  definition of the default's wording; the `gopi -h` row, the `gopi chat -h`
  section, the settings catalog and two wording guards derive from them.
- Docs synced: `CONFIG.md` §2c (paragraph + row + re-measured `read at` anchors),
  `internal/config/settings.go` catalog row, `internal/config/memory_prefetch.go`
  doc block, `cmd/agent/chat_prefetch_usage.go`.
- Master merged in (the help-discoverability half landed upstream mid-run, `f649efd3`).
- Tests reconciled as §4, with two new precedence rows for the OFF direction.
- Anti-contamination held: **no second feedback mechanism** — `git diff` touches
  no file under `internal/tools/memory*` or `internal/memory/`, and the record's
  `adopted` field (including its `no` vocabulary, written when a stale section is
  refused) keeps its existing reader; the write side stays the `memory` tool's
  `action=feedback`.

## Not Done

- **`gopi chat --tools` is still not wired** (pre-existing, documented in
  `devdoc/memory-prefetch-executor.md` §Architecture note). Unchanged by this task
  and not attempted.
- **A pre-existing RED in `cmd/agent` was NOT fixed here**:
  `TestEveryHelpConstantFitsTheWrapBudget` fails on unmodified master over three
  help constants that are not mine — measured, with the offenders named in
  §Verification. It is filed as its own task, **`01a10f68-e10d-721c-9a8f-5a12543fc460`**
  (tag `bug`), whose payload carries the byte-for-byte master comparison that
  attributes it away from this branch: the wrap budget is a shared surface, and
  trimming a peer's prose or widening the budget is a decision neither of my two
  choices is mine to make silently.
- **A second pre-existing red family, measured at close-out and also NOT fixed here**:
  `make test-budget PKGS='./internal/config ./cmd/agent ./internal/agent ./internal/tools ./internal/standards'`
  reports `FAIL github.com/nick/gopi/internal/standards` from `docclass_gate_test.go`
  (`devdoc/verb-retirement-phase1.md`: class "historical" but the file does not open with
  its banner) plus `gopi tool doccheck` reporting one gap
  (`devdoc/scripts-retired-verb-sweep.md` — a normative row must record the spot-check
  that holds its claim). Attribution is the same instrument as the wrap-budget red:
  **both files are byte-identical to master on this branch**
  (`git diff master --stat -- <the two files>` → empty), and this branch's only
  `devdoc/` delta is `DOC-INDEX.tsv` (+1 row) plus this new doc. The red belongs to the
  in-flight verb-retirement merge family (rows `01a10ec5`, `01a10e98`), not to the flip.
  Measured on master itself, not inferred: a **detached worktree at master tip `79b29f5f`**
  (`git worktree add --detach … 79b29f5f`), `go test ./cmd/agent/ -count=1` over the ten
  failing names → **all ten FAIL there too**, with master's own messages: `TestCLIContractInventoryMatchesDispatch`
  ("main.go has 20 … contract suite tracks 18", and master's list already contains
  `SessionSubcommand`), `TestG4HelpSurfaceInventoryTableMatchesHelp` ("86 rows … help
  advertises 87", the diff naming `gopi checkpoint …` vs `gopi session checkpoint …`),
  `TestG5InventoryMatchesTheHelp` (no row for `gopi tool proxy`), `TestCheckpointUsageLinesAreDispatchable`
  ("only 0 … inspected"), `TestBashScriptStructure`, `TestProxyBusyPortNeverClaimsListening`,
  `TestSandboxDirFlagDecidesTheRoot`, `TestAnnotateVerbRunsARealArgvEndToEnd`,
  `TestChatStatus_LiveBlockKeepsTheStateWord` (row renders `resp\u00a0` — the very case
  `01a10ec5` flags as "probably NOT this epic"), `TestEveryHelpConstantFitsTheWrapBudget`.
  So they are pre-existing on master; none of the ten mentions `memory-prefetch`, and the
  diff that could have caused them (the verb-retirement test face) is already on master.
- The `gopi ui` surface was not considered: the knob has no UI-side reader, so there is nothing to flip there.

## Verification

- Build: `CGO_ENABLED=0 go build ./...` → OK (exit 0, no output);
  `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK, `./bin/gopi version` → `v0.1.0`.
- Tests: `go test ./internal/config/ ./internal/memoryprefetch/ -count=1` →
  `ok github.com/nick/gopi/internal/config 4.754s`, `ok .../internal/memoryprefetch 0.083s`.
  `go test ./cmd/agent/ -count=1 -run 'Prefetch|MemoryPrefetch|Help' -v` → all
  prefetch/help cases PASS (21 RUN lines, 0 FAIL), including
  `TestMemoryPrefetchThreeChannelsResolveToOneSwitch` (11 rows, 10 PASS subtests
  shown + the fall-through row), `TestPrefetchOffStartsNoGoroutineAndLeavesThePromptByteIdentical`,
  `TestPrefetchOnStartsExactlyOneLoopGoroutineAndStopReclaimsIt`,
  `TestPrefetchOffWritesNoRecord`, `TestChatHelpShowsMemoryPrefetch`,
  `TestChatUsageStaysWithinItsLineBudget`, `TestTopLevelUsageIsAlignedAndTerse`.
  `make test-budget PKGS='./internal/config ./internal/memoryprefetch ./cmd/agent ./internal/standards'`
  → see §Evidence for the recorded verdict, including the wrap-budget red.
- Ran (Rule 31 — the surface this task changes is the DEFAULT, so it was measured
  by running the real binary twice, with nothing declared and with the knob
  explicitly off):
  1. `python3 .../artifacts/pty_chat_probe.py ON2` — a real `bin/gopi chat` on a
     pty (100×30), two typed bursts, each held still past the 600 ms debounce.
  2. `python3 .../artifacts/pty_chat_probe.py OFF GOPI_MEMORY_PREFETCH=0` — the
     same driver, the same text, the control.
  Neither run set any settings key (`grep -c memory_prefetch ~/.gopi/settings.json` → 0;
  `$GOPI_SETTINGS` is that same file) and the ON run was not given
  `GOPI_MEMORY_PREFETCH`, so run 1 measures **the built-in default** and nothing else.
- Also ran (the help surfaces, as pages): `./bin/gopi -h` → the Options row ends
  `… settings `memory_prefetch`; default ON`; `./bin/gopi chat -h` → the section
  ends ``Precedence: `--memory-prefetch=off` > GOPI_MEMORY_PREFETCH > settings
  `memory_prefetch`. Default ON, and OFF means OFF …``.
- Result:
  - **ON (default, nothing declared)** — session `458c0f7e-2c09-4c68-9549-21e27cd5efd9`
    wrote `memory-prefetch.jsonl` with 4 lines, and **every** line carries
    `"adopted":"yes"`, including two `outcome:"adopted"` events with
    `section_bytes` 1605 / 1441 — i.e. the hits did not merely get found, they
    reached the turn's prompt. The raw log shows the injected block
    (`memory-prefetch (experimental, optional; 5 hits; 1200 runes of 1200 budget)`
    followed by `[memory:#4308|verified|warm] …`), which is the adoption observed
    from the other side (the model's input), not merely claimed by the record.
  - **OFF (`GOPI_MEMORY_PREFETCH=0`)** — session `f9dc54c4-1350-4129-b22a-e0a038428826`:
    `ls: cannot access '…/memory-prefetch.jsonl': No such file or directory`, and
    `grep -c memory-prefetch` over its raw log → `0`. Same driver, same typed text
    (both bursts appear in both raw logs), no record and no trace.
  - Both runs' typed text is present in the respective raw logs (the two-line
    burst and the second burst), so the OFF run is a real negative, not a driver
    that never typed.
- Commit: `4c27c8de` — `feat(config): the typing-time memory prefetch defaults ON`
  (11 files, +394/-38). The report doc's own amendment (this paragraph and the
  task id above) lands in a second commit on the same branch.

## Evidence (self-filed)

- `CGO_ENABLED=0 go build ./...` → exit 0; `bin/gopi` rebuilt (30710170 bytes) and
  `./bin/gopi version` → `v0.1.0` / `go1.24.5` / `linux/amd64` — observed.
- `go test ./internal/config/ ./internal/memoryprefetch/ -count=1` → both `ok`
  (4.754s / 0.083s) — observed in the run's own job output.
- `go test ./cmd/agent/ -count=1 -run 'Prefetch|MemoryPrefetch|Help' -v` → `PASS`,
  with the RUN/PASS list quoted in §Verification — observed.
- **Pre-existing red, measured and attributed**: `go test ./cmd/agent/ -count=1
  -run TestEveryHelpConstantFitsTheWrapBudget` →
  `cli_resume.go: resumeUsage line 51 is 96 display cols (> 88)`,
  `cli_session.go: sessionUsage line 28 is 94 display cols (> 88)`,
  `cli_voice.go: voiceInputUsage line 201 is 89 display cols (> 88)` — none of the
  three names `memory-prefetch`, and each of the three lines was compared
  byte-for-byte against `git show master:<file>` at the same line number:
  `IDENTICAL-TO-MASTER` for all three. The gate's file itself is present on master
  (`git cat-file -e master:cmd/agent/help_width_test.go` → present). So the red
  predates this branch and is not caused by this change — it is filed as its own task.
- ON run record, verbatim (session `458c0f7e-2c09-4c68-9549-21e27cd5efd9`, mode 0600):
  `{"query":"explain read only ledger dsn busy timeout policy","hits":6,"ms":56,"outcome":"hits","adopted":"yes","segments":{…,"journal_mode":"wal","query_only":1,"busy_timeout_ms":250,"attempts":1}}`
  `{"query":"explain read only ledger dsn busy timeout policy","hits":0,"ms":0,"outcome":"adopted","adopted":"yes","section_bytes":1605}`
  `{"query":"now compare lineedit published snapshot versus inputpending seam","hits":6,"ms":18,"outcome":"hits","adopted":"yes",…}`
  `{"query":"now compare lineedit published snapshot versus inputpending seam","hits":0,"ms":0,"outcome":"adopted","adopted":"yes","section_bytes":1441}`
- OFF run, verbatim: `ls …/f9dc54c4-1350-4129-b22a-e0a038428826/memory-prefetch.jsonl`
  → `No such file or directory`; the directory holds only the ordinary session
  artifacts (`.raw.log`, `chat-screen.dump{,.json}`, `console.log`, `metadata.json`,
  `session.jsonl`) — observed.
- The adoption seen from the model's own input (session `458c0f7e` raw log):
  `memory-prefetch (experimental, optional; 5 hits; 1200 runes of 1200 budget)\n[memory:#4308|verified|warm] MEASURED PITFALL (2026-09-19, …` — observed by grep.
- An earlier ON run in the same series (`2f660604-d9c0-47da-83a3-e17057e65110`)
  produced the same 4-line shape before the probe set a pty geometry; it is kept
  as a second independent ON session, not as the headline evidence.
- No orphaned chat processes were left behind: `ps -eo pid,etimes,cmd | grep '[g]opi chat'`
  → empty after the runs (one early probe child was found still alive and was killed).
