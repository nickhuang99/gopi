<!-- doc-class: historical -->
# Task Report: the sync group is GREPABLE on `gopi task -h` (and the gate that only looked one way)

<!-- WORKER-OWNERSHIP: OPERATOR-ONLY — devdoc/worker-ownership.md -->

Task id: `01a11dd4-d986-7b59-84fd-aa35c870e5eb` (branch `task/01a11dd4-d986-7b59-84fd-aa35c870e5eb`).

## The defect, measured

`gopi task -h` is the page an operator greps (`gopi task -h | grep -i sync`).
Before this change that grep returned ONE folded sentence:

```
Not ledger verbs — this page only points at their manuals:
  sync/diff/harvest/push carry rows to and from a PEER (settings.json
  "sync_peers"); their flags live in 'gopi task sync -h'. The patch-FILE channel
  is the separate top-level 'gopi memory memsync'. The git-infrastructure atoms
  (merge / merge-execute / merged-cleanup / adopt / audit-merges) and the merge
  machinery are documented by 'gopi merge -h'; the task-executing service by
  'gopi worker -h'. The RESIDENT MERGER DAEMON is RETIRED (2026-09-19,
  devdoc/merger-retired.md): 'gopi merger' and its deprecated alias 'gopi task
  merger' / 'gopi task serve' refuse and exit 1. Merging itself is NOT retired:
  a merge pass is the one-shot 'gopi merge'.
```

Four of the eight sync verbs were named only as a slash-joined blob
(`sync/diff/harvest/push`), so `gopi task send`, `gopi task pull`,
`gopi task receive`, `gopi task sync-pass` had **no spelling on the page at
all**; and not one line carried syntax. The capability read as removed while the
verbs dispatched the whole time.

The gate was HALF A GATE. `usage_test.go` forbade the sync group from growing a
ledger index ROW (`TestTopLevelUsageShowsTaskLedger`'s SyncVerbs/MergerVerbs
loop), and `cli_tasks_advert_gate_test.go` pinned the single home — but nothing
asserted the other direction ("the verb must still be FINDABLE here"), so a
change that deletes the pointer entirely stays green. A gate that only forbids a
shape is satisfied by the shape's absence.

## Completed

- `cmd/agent/cli_tasks.go` — `tasksUsage`'s pointer section now SPELLS every
  verb of the group as the command you type, one synopsis row each:
  `sync` (×2: the delta session and `--peers`/`--status`), `diff`, `harvest`,
  `push`, `send`, `pull`, `receive`, `sync-pass`. The section head now says why
  the spellings are here ("this page SPELLS them so the one you need is findable
  here") and re-states the single-home boundary in the same breath: the flag
  **SEMANTICS** stay in `syncUsageHead` and *there only* — `gopi task sync -h`
  remains the page that EXPLAINS a flag, this page prints the spelling for
  recognition. The git-infrastructure paragraph (merge atoms, `gopi merge -h`,
  `gopi worker -h`, the retired merger daemon) is unchanged and now follows as
  its own paragraph.
- `cmd/agent/usage_test.go` — the MISSING direction, in the existing file (no
  second gate file, per the briefing):
  - `TestEverySyncVerbIsGrepableFromTheFamilyHelp` — for every verb in
    `taskverbs.SyncVerbs`: ① `gopi task <verb>` is spelled on the page, ② its
    synopsis (spelling + syntax placeholders) is present, ③ the pointer-section
    heading is LOCATED and the section naming the group is measured (a reworded
    heading fails loudly instead of measuring an empty region as clean), ④ and
    no sync FLAG crept back into the family page — so this discoverability fix
    cannot become a licence to copy the manual back up. The table
    (`syncVerbSynopsis`, keyed by the DERIVED verb names) is asserted to cover
    `taskverbs.SyncVerbs` exactly, so a verb added to the group with no declared
    synopsis is a failure, not a silent omission.
  - `TestSyncVerbSynopsisGateIsNotBlind` — the in-repo RED PROOF (Rule 10.4):
    the same predicate runs over the **verbatim pre-fix pointer sentence** and
    must go red on it, with the specific missing spellings named; and the SHIPPED
    page must be clean, so the predicate is not simply "always red".

### What the pointer row may and may not carry (the constraint that shaped it)

The briefing asked for the sync pointer to carry `--push-only --peer/--peers
--status --full`. Two STANDING gates — `TestSyncVerbsHaveExactlyOneHeadHome`
(`cmd/agent/cli_tasks_advert_gate_test.go:324`) and `TestTasksSyncFamilyHelp`
(`cmd/agent/cli_tasks_test.go:178`) — refuse `--push-only`, `--full`,
`--serve-remote`, `--claim-remote` inside `tasksUsage`, on a plain
`strings.Contains`. That is the 2026-09-15 restructure's surviving invariant:
the sync flags' SEMANTICS have exactly one home (`syncUsageHead`), and the family
page carries the pointer.

The briefing also says the grouping decision must not be overturned, so the two
requirements meet head-on. Resolution taken: **the grouping decision wins** — it
is the stated constraint, and the two flags it excludes are excluded as
*semantics*, not as capabilities. The page carries every sync verb's spelling
plus the syntax it takes (`--peer <alias>`, `--peers [a,b]`, `--status`,
`--tags TAG`, `--db PATH`, `--since UNIX`, `--no-push`, `--cancel-source`,
`--task-id T`, `--skip-tag a,b`, `--once`), and `--push-only` / `--full` remain
at their one home — where the same grep already lands
(`gopi task sync -h`). ④ of the new gate makes that boundary explicit and
enforced rather than a happy accident.

## Before / after, verbatim (`gopi task -h`)

Before, `gopi task -h | grep -i sync --line-number` — three lines, no syntax:

```
136:  sync/diff/harvest/push carry rows to and from a PEER (settings.json
137:  "sync_peers"); their flags live in 'gopi task sync -h'. The patch-FILE channel
138:  is the separate top-level 'gopi memory memsync'. The git-infrastructure atoms
```

After, the same command — a synopsis row per verb, each one a command you type:

```
140:recognition, and 'gopi task sync -h' is where each one is explained:
141:  sync       gopi task sync --peer <alias> [--tags TAG] [--db PATH]
142:  sync       gopi task sync --peers [a,b] | --status
149:  sync-pass  gopi task sync-pass --once
150:      Cross-node sync (settings.json "sync_peers"): the sync surface has ONE
151:      home and it is 'gopi task sync -h' (printed by sync/diff/harvest/push -h
153:      channel is the separate top-level 'gopi memory memsync'.
```

The full shipped block (`sed -n '134,160p'` of the after-artifact):

```
Not ledger verbs — this page SPELLS them so the one you need is findable here.
Each row is the command as you type it, so grepping this page answers "what
moves a row to a peer?" without opening a second page; the flag SEMANTICS
(peer resolution, conflict rules, exit codes) live on the page the group names,
and THERE only — the flags below are the spellings this page prints for
recognition, and 'gopi task sync -h' is where each one is explained:
  sync       gopi task sync --peer <alias> [--tags TAG] [--db PATH]
  sync       gopi task sync --peers [a,b] | --status
  diff       gopi task diff --peer <alias> [--db PATH]
  harvest    gopi task harvest --peer ALIAS [--since UNIX] [--no-push]
  push       gopi task push
  send       gopi task send --task-id T [--peer <alias>] [--cancel-source]
  pull       gopi task pull --task-id T[,T2…] --peer ALIAS
  receive    gopi task receive --task-id T --peer ALIAS [--skip-tag a,b]
  sync-pass  gopi task sync-pass --once
      Cross-node sync (settings.json "sync_peers"): the sync surface has ONE
      home and it is 'gopi task sync -h' (printed by sync/diff/harvest/push -h
      too, which is where the flags above are explained in full). The patch-FILE
      channel is the separate top-level 'gopi memory memsync'.
```

The rest of the page (the ledger index, the git-infrastructure paragraph, the
shared `--db` row) is byte-identical apart from the pointer section — the two
help dumps differ on exactly lines 135–160 (`diff` of the two artifacts is in
`## Verification`).

## Not Done (honest)

- **`make test-budget PKGS='./cmd/agent'` is RED on this host, on four
  `TestToolsMaintenanceHelpSurfaceFormsAreTyped` sub-cases that are NOT mine.**
  Reported as measured, not glossed:

```
--- failures (assertion/build; the budget did NOT trip) ---
  --- FAIL: TestToolsMaintenanceHelpSurfaceFormsAreTyped/gopi_tool_voice_input (0.01s)
  --- FAIL: TestToolsMaintenanceHelpSurfaceFormsAreTyped/gopi_tool_voice_input_--seconds_5 (0.01s)
  --- FAIL: TestToolsMaintenanceHelpSurfaceFormsAreTyped/gopi_tool_voice_input_clip.ogg (0.01s)
  --- FAIL: TestToolsMaintenanceHelpSurfaceFormsAreTyped/gopi_tool_voice_input_clip.pcm (0.01s)
  --- FAIL: TestToolsMaintenanceHelpSurfaceFormsAreTyped (44.66s)
stamp: 2026-10-09T07:58:13+08:00 host=hostb-Laptop pkg=./cmd/agent budget=25m elapsed=1067s exit=1 outcome=fail
```

  The four cases assert the "first reachable LOCAL state" of the voice capture
  path (the stubbed `arecord` card refused by ID, or the engine's decode-set
  refusal). On this host the FIRST reachable state is a different one — the
  default engine is the cloud engine and there is no key file:

```
$ ls -l ~/.doubao/apikey.txt
ls: cannot access '/home/user/.doubao/apikey.txt': No such file or directory
$ GOPI_HOME=<sandbox> ./bin/gopi tool voice_input <dir>/clip.ogg
gopi tool voice_input: doubao engine needs an API key: /home/user/.doubao/apikey.txt (key file
"/home/user/.doubao/apikey.txt" (set --engine doubao's key file with GOPI_DOUBAO_KEY_FILE or
settings "doubao_key_file")). to transcribe ON this machine instead: --engine local, …
```

  **It is not this change.** Proof, not argument: the same commands were run
  against the pre-fix binary built from this worktree's HEAD~2 `cli_tasks.go`,
  and the outputs are **byte-identical** (`diff` of the two runs, all four
  forms: `clip.ogg: IDENTICAL`, `clip.pcm: IDENTICAL`, and both bare forms
  identical under the stubbed `arecord` PATH) — the help text of `gopi task -h`
  cannot reach the voice engine. `git diff master --name-only` for this branch
  lists exactly the five paths in `## Files`; none of them is voice code, and
  the case file's own last touch is `9a0450584` (2026-10-08, already an ancestor
  of both HEAD and master). Evidence that the second failure mode is a marker
  near-miss rather than a behaviour change: the case accepts `"doubao engine:"`
  (the error form) while the key-missing refusal spells it `"doubao engine needs
  an API key"` (cli_voice_engine.go:90 vs :554).
  **Dispatched as its own bug row rather than fixed here** (Rule 26 + Rule 3:
  it is a different surface — the G6 case table's engine assumption — and this
  epic's scope is the help page):
  **`01a11e05-3525-781c-bed3-53940b76e972`** (tags=bug, pending; the harness
  chained it under this row, so it releases when this one goes terminal).
- `--push-only` and `--full` are **not** on the family page (see the section
  above): two standing gates forbid them there, and the briefing's
  "do not overturn the grouping decision" is the stronger constraint. If the
  operator wants those two spellings on `gopi task -h`, that is a change to the
  single-home rulings themselves — a separate decision, not a help-text edit.
- The new gate measures the verb SPELLING and the syntax placeholders, not that
  each flag named is accepted by the verb. A synopsis that names a flag the verb
  rejects would still pass — the parser's own tests are the gate for that
  (e.g. `taskPullClaimFlag`, the `--peers` grammar refusals). Noted, not closed.
- The `88`-column budget is satisfied by construction (`awk 'length>88'` over the
  changed region prints nothing) and by the standing
  `TestEveryHelpConstantFitsTheWrapBudget`; no separate width assertion was added.

## Files

- `cmd/agent/cli_tasks.go` — `tasksUsage` pointer section (spelling + synopsis rows).
- `cmd/agent/usage_test.go` — `syncVerbSynopsis`, `missingSyncSynopses`,
  `TestEverySyncVerbIsGrepableFromTheFamilyHelp`, `TestSyncVerbSynopsisGateIsNotBlind`.
- `COMMANDS.md` — regenerated anchor line number for `gopi task`
  (`cmd/agent/cli_tasks.go:475` → `:491`), via its own generator, never by hand.
- `devdoc/task-help-sync-entry-discoverable.md` — this report.

## Verification

- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` → OK (exit 0).
- Tests: `make test-budget PKGS='./cmd/agent'` → **FAIL, 1 package, 1067s**
  (`stamp: 2026-10-09T07:58:13 host=hostb-Laptop exit=1 outcome=fail`) — the
  only failing test is `TestToolsMaintenanceHelpSurfaceFormsAreTyped`'s four
  `voice_input` sub-cases, which this change cannot reach; the evidence that it
  is not mine, and the dispatch of the bug row, are in `## Not Done` below.
  <br>**Every gate this change touches is green**, run as a focused set
  (see `## Evidence`): the two new tests, both single-home gates, the
  no-duplication gate and the ledger-group gate → `ok … 3.592s`.
  <br>The first full-package run of this worktree (before the report existed)
  was red for a different and now-resolved reason — the built binary predated
  `cli_tasks.go`, so the e2e freshness guards (`assertGopiBinIsFresh`) refused
  it; re-run after the rebuild they pass (`TestRawPromptRunReleasesItsWorktree`
  and the nine sibling worktree/worker e2e cases → `ok … 41.981s`).
- Ran (the surface changed IS a help page, so it was printed twice — once by the
  pre-fix binary, once by the shipped one):

```
$ SESS=~/.gopi/sessions/01a11dd4-d986-7b59-84fd-aa35c870e5eb
$ git show HEAD:cmd/agent/cli_tasks.go > /tmp/cli_tasks_head.go
$ cp /tmp/cli_tasks_head.go cmd/agent/cli_tasks.go
$ CGO_ENABLED=0 go build -o $SESS/artifacts/gopi-prefix ./cmd/agent/     # the BEFORE binary
$ cp $SESS/artifacts/cli_tasks.go.fixed cmd/agent/cli_tasks.go
$ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/                        # the AFTER binary
$ $SESS/artifacts/gopi-prefix task -h > $SESS/artifacts/task-h-before.txt   # exit=0
$ ./bin/gopi task -h > $SESS/artifacts/task-h-after.txt                     # exit=0
$ ./bin/gopi task sync -h > $SESS/artifacts/task-sync-h.txt                 # exit=0
$ diff $SESS/artifacts/task-h-before.txt $SESS/artifacts/task-h-after.txt
135,144c135,160     # 10 lines out, 26 lines in — the pointer section only
```

- Result:

```
$ grep -i -n sync $SESS/artifacts/task-h-after.txt
140:recognition, and 'gopi task sync -h' is where each one is explained:
141:  sync       gopi task sync --peer <alias> [--tags TAG] [--db PATH]
142:  sync       gopi task sync --peers [a,b] | --status
149:  sync-pass  gopi task sync-pass --once
150:      Cross-node sync (settings.json "sync_peers"): the sync surface has ONE
151:      home and it is 'gopi task sync -h' (printed by sync/diff/harvest/push -h
153:      channel is the separate top-level 'gopi memory memsync'.
```

  `gopi task sync -h` still prints `syncUsageHead` alone, flag semantics intact
  (`--push-only` ×2, `--full` ×3, `--serve-remote` ×3, `--claim-remote` ×4,
  `--takeover` ×1 — counted in the artifact), and `gopi task -h` is 10139 bytes
  against the pre-fix 9135.

  Re-generation of the moved anchor was verified rather than assumed:

```
$ go run internal/standards/testdata/commands-census.go . COMMANDS.md --check
commands-census: COMMANDS.md is in step with the source (44 verbs)   # exit 0
```

## Evidence (self-filed)

- RED (new gate vs the pre-fix `cli_tasks.go`, everything else untouched):
  `go test ./cmd/agent/ -run 'TestEverySyncVerbIsGrepableFromTheFamilyHelp|TestSyncVerbSynopsisGateIsNotBlind' -count=1`
  → `FAIL github.com/nick/gopi/cmd/agent 2.017s`, exit 1, **20 findings**:
  7 × "never spells `gopi task <verb>`" (diff/harvest/pull/push/receive/send/
  sync-pass), 8 × "does not carry the synopsis …", 4 × "the pointer section does
  not name …" (pull/receive/send/sync-pass), plus the not-blind proof failing
  because the shipped page was the pre-fix one. Artifact:
  `~/.gopi/sessions/01a11dd4-d986-7b59-84fd-aa35c870e5eb/artifacts/red-run.txt`.
- GREEN (same gate + the two single-home gates + the duplication gate +
  the ledger-group gate, after the help change):
  `go test ./cmd/agent/ -run 'TestEverySyncVerbIsGrepableFromTheFamilyHelp|TestSyncVerbSynopsisGateIsNotBlind|TestSyncVerbsHaveExactlyOneHeadHome|TestTasksSyncFamilyHelp|TestNoHelpTextIsDuplicatedAcrossPages|TestTopLevelUsageShowsTaskLedger' -count=1`
  → `ok github.com/nick/gopi/cmd/agent 3.592s`, exit 0. Artifact:
  `.../artifacts/green-run.txt`.
- The e2e family the freshness guard had blocked, re-run after the rebuild:
  `go test ./cmd/agent/ -run 'TestRawPromptRunReleasesItsWorktree|TestWorker*|TestTaskRun*|TestFrontRun*' -count=1`
  → `ok github.com/nick/gopi/cmd/agent 41.981s`, exit 0.
- The ratchet on `cmd/agent`'s changed files:
  `go test ./cmd/agent/ -run TestSyncBarePeersTestCoversEveryChangedFile -count=1`
  → `ok … 1.381s`, exit 0 (my two files are in its coverage lists by identity —
  `usage_test.go` in `syncPeersBareTestFiles`, `cli_tasks.go` in
  `syncPeersBareSourceFiles` — so no list edit was needed).
- Width budget: `awk 'NR>=440 && NR<=478 && length>88' cmd/agent/cli_tasks.go`
  printed nothing (longest new line: 80).
- Rule 30: `git status --porcelain` in the worktree at close-out → **printed
  nothing**.

## Commit

`3e8e3aab9 feat(cli): spell the sync group on 'gopi task -h' and gate the direction that was missing`

(The change commit. This report rides one commit later, so the hash it quotes is
the parent's and stays correct without the quote moving every time the message is
touched.)

Rule 30 at close-out: `git status --porcelain` → empty (the five paths above are
in those commits; nothing untracked was left in the worktree).
