<!-- doc-class: normative -->
# Module Boundary Map

Umbrella document for the `devdoc/requirements/` series. Read with
`00-method-and-standards.md` (the standards every per-module document obeys).

> **Provenance (added 2026-09-17, branch `docs/req-umbrella-verify`, master `9ce8e159`).**
> This file's first version is commit `12e1ac74` (323 lines); the text below is the
> later rebuild. Numbers are re-measured in this pass and every one that moved is
> corrected in place with the previous value kept visible, because **a boundary map
> that is not re-enumerated at its own revision is a claim about a revision, not a map**:
> `find internal cmd -name '*.go' ! -name '*_test.go' | xargs wc -l` →
> **160,428** production lines (first version: 143,015), **203,191** test lines;
> `ls -d internal/*/` → **43** packages (first version: 40); `ls cmd/agent/cli_*.go |
> grep -v _test` → **45** surfaces (first version: 47); plus **33** non-CLI production
> files in `cmd/agent` (**78** total) and **3** `cmd/` directories. The three packages
> and two `cmd/` dirs added since the first version are assigned in §2 — see the note
> there.

---

## 0. The boundary principle, and the corrections it forced

A boundary is drawn **by user-visible responsibility**, never by directory
(`00-method-and-standards.md` §7). Measured against the code
(`find internal cmd -name '*.go' ! -name '*_test.go' | xargs wc -l` → 160,428
lines across **43** `internal/` packages and **45** `cmd/agent/cli_*.go` files at
master `9ce8e159`), this principle produced **14** modules, not the 12 first proposed
in this epic's roadmap and **not** the 11 of the earlier attempt.

Three corrections are worth recording because they are themselves findings:

**Correction 1 — an eleventh-hour addition.** The set proposed in the roadmap
(`services/voice/`, `services/proxy/` absent from every proposed boundary) left
`internal/proxy` (1,208 L), `internal/doubaoasr` (802 L), the `voice*`,
`voice_input`, `brew`, `shot`, `screencapture` CLI families and
`internal/text2img`/`internal/shot` (2,889 L) unassigned. Modules 13 and 14
close that hole.

**Correction 2 — an earlier series already existed and stopped; a LATER one then
succeeded.** The ledger holds three rows filed 2026-09-16 15:09–15:10, all `cancelled`:

| task | module it claimed | status |
|---|---|---|
| `01a0aac3` | `REQUIREMENTS [0/11] umbrella` | cancelled |
| `01a0aac4` | `[1/11] merge, branch lifecycle & the merger daemon` | cancelled |
| `01a0aac4` | `[2/11] the task ledger — what a task row is FOR (queue vs memory)` | cancelled |

`[E1: tasks 01a0aac3, 01a0aac4 — re-run: sqlite3 ~/.gopi/memory.db "select
substr(task_id,1,8),status,summary from tasks where summary like '%REQUIREMENT%'"]`

**This entry's original text is now wrong and is corrected here rather than
deleted.** It said "no document was ever produced:
`git log --all --oneline --diff-filter=A -- 'devdoc/requirements/*'` returns empty,
and `git branch -a --list '*requirement*'` returns only this branch." Both commands
return something else today:

- `git log --all --oneline --diff-filter=A -- 'devdoc/requirements/*'` lists **13**
  commits — the first is `12e1ac74` (this file + the method doc + the briefings),
  then `8e2fa780` (the method-doc rebuild), then one `--diff-filter=A` commit per
  module document;
- `git branch -a --list '*requirement*'` returns **nothing** — and that is not
  evidence of absence either: four merge rows are `cancelled`
  (`01a0ab1c`, `01a0ab49`, `01a0ab57`, `01a0ab70`), i.e. the umbrella branch was
  merged by hand/rebuild and the branch retired. **A missing branch name is not a
  missing document** — the lesson the first version's command could not express.

The earlier attempt died at the planning stage (that part stands). What the series
did afterwards: the `01a0ab11-*` module rows executed and produced
`01-task-ledger.md` … `11-build-and-repos.md` (11 of 12 `completed`, `B12` pending),
and the `01a0ab12-*` series — the one this document belongs to — produced these two
umbrella documents. The continuity is deliberate, so the operator's own vocabulary
("the task ledger", "the merger") is preserved. **The open question the first version
raised — "the umbrella of a cancelled series was itself cancelled, and nothing says
why" — is now partly answerable: the cancellation was of the *first* attempt only,
and its rows were superseded, not abandoned; the reason is still not written down
anywhere, which is the finding `99-summary.md` should carry.**

---

## 1. The fourteen boundaries

Each entry states **the responsibility in user words**, the **reason this is one
boundary** (why it is not merged with a neighbour nor split), and the **packages
and surfaces it owns**. `M0` is this umbrella; `M15` is the summary.

### M1 — The task ledger: what the operator asked for, and what happened to it
- **Responsibility (user words):** the durable record — every unit of work the
  operator asked for, its state, who is doing it, and how it ended.
- **Boundary reason:** the operator's question "what happened to my task?" is a
  single question answered by a single store, even though the store is SQLite
  (`internal/tools/ledger_dsn.go`), its schema and status vocabulary live in
  `internal/tools` + `internal/tasktags` + `internal/taskverbs` +
  `internal/taskverdict`, and it is *used* by the CLI, the workers, the merger
  and the client sync. Use is not ownership: the ledger is one module, many
  consumers.
- **Owns:** `internal/tools` — `ledger_dsn.go`, claim/reclaim paths
  (`claim_holder_freshness_test.go` names them), `task_*` files,
  `control_sort.go`, `dirty_holder_salvage.go`, `branch_attribution.go`;
  `internal/tasktags`, `internal/taskverbs`, `internal/taskverdict`,
  `internal/taskreport` (the report *contract* the operator reads); the
  `tasks`/`tasks_*` CLI family (`cmd/agent/cli_tasks*.go`); `internal/ledgerwindow`
  (the one wall-clock→epoch window definition the ledger's time questions resolve
  through, `epochs.go`'s `Bounds`) with its verb `cmd/ledger-window/`.
  **One owner per package:** the re-enumeration of 2026-09-17 put
  `internal/taskreport` under **both** M1 and M11. M1 owns it here —
  `internal/tools/task_report.go`, `childenv.go` and the `task_report*_test.go`
  files are the ledger's own report contract, and `internal/tools` is a package
  M1 already owns in part. **M11 reads `task_report.go` as the text the operator
  is shown** and owns the *delivery* of it (`GOPI_TASK_NOTIFY_EVENTS`, the notify
  bus); the split is stated rather than left to two workers to guess.
  [inferred, weakest link: the two packages are genuinely two-faced — a reader
  could reasonably hand `taskreport` to M11 instead, and the choice is recorded
  rather than derived from a citation.]
- **Note for the worker:** the earlier series framed this as "queue vs memory"
  (`[E1: task 01a0aac4]`) — that framing is directly reused.

### M2 — Delivering finished work to the trunk (merger + branch lifecycle)
- **Responsibility:** a branch that is finished lands on master without a human
  present, and a branch that cannot land is parked instead of blocked.
- **Boundary reason:** merge, park, archive-tag/rename, worktree release,
  conflict resolution and the safe-merge gate are one *outcome* ("did my work
  land?") with several verbs. Splitting them would put the merger and the
  conflict resolver in different documents while they share one state machine.
- **Owns:** merger daemon + its spawn/stop (`cmd/agent/cli_merger*`,
  `internal/daemonlifetime`), `cmd/agent/cli_tasks_merge.go`,
  `internal/workerownership`, `worktreerelease`, `worktreeroots`,
  `scripts/safe-merge.sh`, the archive-tag branch namespace.
- **Operator's own words already on the record:** a parked branch must be
  **renamed** `conflict/<name>` rather than carried by an archive tag
  `[E1: task 01a0ab04]`.
  ⚠️ **WITHDRAWN 2026-09-17** (`devdoc/branch-lifecycle-ruling.md` §1, task
  `01a0af2f`): the rename is VOID — the lifecycle's endpoint is deletion, so a
  branch is in `refs/heads/` or it is deleted, with no state-carrying namespace
  and no `archive/` tag. The `archive/` namespace named above is likewise on its
  way out (the park lane that minted it is removed). The requirement text is
  kept as the record of a decision that was reversed.

### M3 — The same state on two machines (sync + node identity)
- **Responsibility:** work done on either host is visible and usable on the
  other, and the two hosts do not fight over the same layer.
- **Boundary reason:** the ssh client/server legs, the role split, node identity
  and the sync ledger form one user-visible property ("is my other machine up to
  date?"). The *policy* that the two hosts own different layers is Rule 28's —
  a requirement, not a deployment note `[E2: GOPI.md:1002]`.
- **Owns:** `cmd/agent/cli_tasks_sync*.go`, `cli_tasks_bridge.go`,
  `cli_tasks_adopt.go`, `cli_memory_sync.go`/`memsync`, `internal/sessionevent`
  (the sync event stream), the node-identity state under `~/.gopi/`.

### M4 — Keeping a long run inside its attention budget (context + memory)
- **Responsibility:** an agent does not re-derive what a previous session
  learned, and a long run does not die of its own transcript.
- **Boundary reason:** compaction, the distilled memory store, `context_trim`,
  and the memory brew/feedback loop are one need (finite attention + retained
  experience). The *store* is SQLite, but its consumer is the agent.
- **Owns:** `internal/hippocampus` (1,154 L), `internal/tools/memory*.go`
  (including brew/procedural/provenance), `internal/agent` compaction paths,
  `internal/tools` `context_trim`, the `memory` CLI family.
- **Explicitly in scope for the worker:** `internal/agencyscan` — the
  improvement-proposal pass over past sessions — is a *consumer* of this store,
  and the worker must decide whether it belongs here (proposal: yes, and say so).

### M5 — Working a goal to a verified deliverable (the agent loop)
- **Responsibility:** hand it a goal in plain language; it works, shows its
  working, and stops with something a human can check.
- **Boundary reason:** the turn loop, the task list it maintains, the in-session
  working area, the review verdicts and the refuse-to-finish-without-a-report
  behaviour are one contract between the operator and "the thing that works".
- **Owns:** `internal/agent` (14,565 L — the largest single package),
  `internal/checkpoint` (resume-from-crash), `internal/thinkmode`,
  `internal/pricing`, `internal/clicontract` (only where it pins loop behaviour).

### M6 — The toolbox and the rules that make the model reach for it
- **Responsibility:** the agent can actually act on a real machine, and it
  actually does.
- **Boundary reason:** the tool set and the *routing* that makes it used are one
  responsibility because they share one acceptance standard: a tool the model
  does not call is a tool that does not exist `[E2: GOPI.md:298 Rule 11]`. The
  worker must measure current adoption, not restate the 2026-08-27 audit
  (`GOPI.md:309` cites git_blame 0/21,435 and code_search 0.3%) — see §2 below.
- **Owns:** `internal/tools` tool implementations (read/write/edit/bash/
  code_search/keyword_search/web_*/git_blame/job/task/task_submit/memory/
  vision/text2img/screen_capture), `internal/toolnames`, the tool section of
  `internal/agent`'s prompt, `internal/stylegate` + `internal/standards` only
  where they gate *tool* behaviour.

### M7 — The operator's cockpit (CLI surface)
- **Responsibility:** one person can dispatch, watch, inspect and repair the
  whole system from places he understands.
- **Boundary reason:** verb families are the user's map of the system; they
  belong in one document so "is this surface still holdable by one person?" can
  be asked once. **45** `cmd/agent/cli_*.go` files (re-measured; the first version
  said 47) plus **33** non-CLI `cmd/agent/*.go` production files is the measured
  size of that question. The "one person" number itself does not exist: the first
  version's "65 verbs" (via `grep -rhoE '"[a-z-]+"'` over the CLI files) **no longer
  reproduces** — the same regex over all 78 files returns 517 distinct literals,
  mostly flags and values, not verbs — so the doc must count from the registry
  (`cmd/agent/cli_verbs.go:6`, "cliVerbs is the single source of truth for the
  TOP-LEVEL gopi CLI verb words", 164 lines, backed by `internal/taskverbs`).
- **Owns:** `cmd/agent/cli_verbs.go` (the verb source of truth),
  `cli_completion.go`, the dispatch surface, `cmd/agent/main.go` +
  `cmd/agent/commands.go` (the dispatcher), `internal/lineedit` (5,421 L — the
  interactive editing surface the operator types into; the first version said
  4,327 L), the prompt/input half (`inputtext.go`, `lineedit_wire.go`), and
  `cmd/ledger-window/` (a CLI verb whose numbers come from `internal/ledgerwindow`,
  §2). **`internal/clicontract` is M14's package** (it is the contract of a
  cross-cutting surface); M7 is a *reader* of it and says so rather than claiming
  it — the first version listed it under both M7's and M14's owns-lists without
  saying which was the claim.

### M8 — Talking to the agent, live (chat / streaming UI)
- **Responsibility:** steer work in progress over a durable conversation rather
  than a one-shot pipe.
- **Boundary reason:** chat/wake/status/web share the *interaction* need; the
  web surface is included only if it serves that same need — the worker must
  decide and state the decision (it may legitimately conclude the web surface is
  a **separate** module if it serves a different user need; either way the doc
  must say which and why).
- **Owns:** `cmd/agent/chat_stream.go`, `cli_chat.go`, `internal/tools/chat.go`.
  **`web/` does not exist as a directory in this tree (re-measured 2026-09-17:
  `ls web/` → no such directory; `grep -rln '"web"\|cli_web' cmd/agent` → the
  surface is `cmd/agent/cli_web.go`, a Go file with the page embedded in it), so the
  M8 worker's "decide `web/`" instruction resolves trivially: there is no separate
  web tree to assign, and the web surface's need is judged from `cli_web.go` alone.
  The first version's reference to "`cmd/agent/cli_web.go` + `web/`" presumed a
  directory that is not in the repo.**

### M9 — Prose rules that did not hold, turned into checks (gates & test infra)
- **Responsibility:** the rules the operator cares most about are enforced by
  machines so they survive the next agent's forgetfulness.
- **Boundary reason:** the gate mechanism (baseline + inline escape + data
  report), the heavy-test classification, and `internal/testutil` are one
  responsibility: making a decided standard *stick*. **Re-measured 2026-09-17:
  `internal/standards` is 3,015 L and `internal/stylegate` 1,954 L** (the first
  version said "`internal/standards` alone is >6 files and 2,625 L"), and this
  module also owns the three packages that arrived *after* the first version drew
  the map — `internal/smoke`, `internal/smokegate`, `internal/ledgerwindow`'s gate
  half (§2) — because all three exist to make the merge/verify path's smoke
  command one definition with one shape.
- **Owns:** `internal/standards`, `internal/stylegate`, `internal/heavytest`,
  `internal/testutil`, `internal/toolnames` (as the constant single-source),
  `internal/scratch`'s assert helpers, `internal/smoke`, `internal/smokegate`,
  the tests that gate gates. **`internal/scratch` itself is M10's** — M9 owns only
  its assert helpers (`scratch.AssertTempRootOffRootFS`), and the task-vs-body
  matter assignment is stated so the two modules do not both claim the package.

### M10 — Not stranding anything (worktree, cwd, cgroup, scratch, temp root)
- **Responsibility:** two tasks never stand in one directory, a crash does not
  strand a checkout or leak a process, and temp work never fills the boot disk.
- **Boundary reason:** Rule 22 (temp off `/`), Rule 24 (cgroup-judged
  independence), Rule 25 (unprivileged daemons), Rule 29 (never borrow another
  task's worktree) are four faces of one promise: *clean up after yourself, and
  do not touch what is not yours*. The incidents that produced them
  (`GOPI.md:695,774,803,1045`) are shared.
- **Owns:** `internal/scratch`, `internal/daemoncwd`, `internal/cgroupscope`,
  `internal/procname`, `internal/procstat`, `internal/signalrec`,
  `internal/workerownership` (release side), `internal/worktreeroots`,
  `internal/worktreerelease`, `cmd/agent/cli_scratch_gc.go`,
  `cmd/agent/cli_daemon_*.go` (scope/lifetime half).

### M11 — Telling the operator without being asked (notifications & observability)
- **Responsibility:** an absent operator is told when something needs him, and
  can reconstruct afterwards what happened.
- **Boundary reason:** "the system must reach me" and "I must be able to look
  back" are one responsibility (signal vs noise) served by notification events,
  the session log, crash dumps and `gopilog`.
- **Owns:** `~/.gopi/notify`, `internal/gopilog` (4,956 L),
  `cmd/agent/cli_log.go`, `cli_logstats`, crash dumps
  (`internal/agent/crashdump.go`), `internal/taskreport` (the report the
  operator reads), `GOPI_TASK_NOTIFY_EVENTS`.

### M12 — New code is running code (build, rebuild, repo declaration)
- **Responsibility:** after work lands, the machine is actually running it.
- **Boundary reason:** the repos table, the declared build command, the rebuild
  after merge, the build-failed signal, and `internal/version` (the binary's
  identity) form one property: *master tip == running binary*.
- **Owns:** the `repos` settings table (`settings.json`), `cmd/agent/cli_tasks_rebuild.go`,
  build-timeout config, `internal/version`, `internal/config` (build/repo half).
  `[E2: GOPI.md:311 Rule 12]`

### M13 — Living in the operator's environment (voice, local models, image I/O, the web)
- **Responsibility:** gopi is reachable and useful from where the operator
  actually is — spoken input, a proxy the operator's other tools already speak, a
  screenshot of the live desktop, an image rendered on demand.
- **Boundary reason:** these are **environment adapters** — they exist because
  the operator's world is not a plain terminal. They are grouped because they
  share a failure mode (each drags external capability toward the zero-dep
  binary) and each is a candidate over-build the worker must judge against the
  standing constraints §5.1/§5.2 (`00-method-and-standards.md`).
- **Owns:** `internal/proxy` (1,208 L — Anthropic-API-shaped requests
  translated to OpenAI shape), `internal/doubaoasr` (802 L) + the `voice*` CLI
  family, `internal/shot` (1,953 L) + `cli_shot.go`/`screencapture`,
  `internal/text2img` (928 L) + `cmd/text2img-verify`, the `brew` CLI family,
  `internal/config` only where it carries their credentials/endpoints.
- **Why not with M6 (tools):** the *tools* are the agent's hands; these are the
  operator's environment. A voice-input defect and a `vision_analyze` defect
  have different users and different acceptance.
- **Why not with M8 (chat):** chat is a channel the operator chooses; these are
  capabilities the host must provide.

### M14 — Cross-cutting obligations of the running system (identity, config, lifecycle)
- **Responsibility:** the promises every module must keep: one identity for a
  process, one place a setting comes from, one truth about the node's role.
- **Boundary reason:** these are deliberately **not** owned by a feature — if
  they were, every other document would restate them. Grouping them makes the
  *tension between them* visible (e.g. "a daemon must be simple" vs. Rule 25's
  requirement to announce a degradation).
- **Owns:** `internal/procname` (process identity), `internal/config` (the
  settings/env/precedence catalog — the module where Rule 2's single-source-of-
  truth lives), `internal/daemonlifetime`, `internal/sessionevent`,
  `internal/sessionlayout`, `internal/clicontract`, `internal/runcmd`,
  `internal/agencyscan` (if M4 does not claim it — the two workers must not both
  claim it or both skip it; M4 proposes to claim it).

### M15 — The whole picture (summary)
- Reads all module docs, answers what gopi is for, which requirements are well
  served, the ranked biggest gaps, where the operator's model and the code's
  model disagree, and what the test phase should pin first.

#### M1–M15 → the document that delivers each (added 2026-09-17)

A reader needs to know **which file to open**, and which of these boundaries a
worker has already discharged. Status re-measured from the ledger
(`sqlite3 ~/.gopi/memory.db "select substr(task_id,1,8),status,summary from tasks
where summary like '%REQUIREMENT%'"`, and the module docs on master):

| module | document on master | task row | status |
|---|---|---|---|
| M1 task ledger | `01-task-ledger.md` (920 L) | `01a0ab3d-1e94…` | completed |
| M2 merge & branch lifecycle | `02-merge-and-branches.md` (1,002 L) + `merge-branch-lifecycle.md` (836 L) | `01a0ab3d-4c03…` | completed |
| M3 sync & node identity | `04-sync-and-nodes.md` (1,079 L) | `01a0ab3d-d541…` | completed |
| M4 context & memory | `05-context-and-memory.md` (523 L) | `01a0ab3e-02ef…` | completed |
| M5 agent loop | `06-agent-loop.md` (581 L) | `01a0ab3e-3057…` | completed |
| M6 tools & routing | `07-tools-and-routing.md` (1,116 L) | `01a0ab3e-652f…` | completed |
| M7 CLI surface | **none** — the 12-boundary lineage had no B-row for the CLI surface as such | — | **open** |
| M8 chat & interactive UI | `09-chat-ui.md` (937 L) | `01a0ab3e-c46a…` | completed |
| M9 gates & test infra | **none** | `01a0ab3f-7069…` | **pending** |
| M10 worktree & cwd | `03-worktree-and-cwd.md` (752 L) | `01a0ab3d-7e16…` | completed |
| M11 notifications & observability | **none** | — | **open** |
| M12 build & repo declaration | `11-build-and-repos.md` (727 L) | `01a0ab3f-2c2c…` | completed |
| M13 environment adapters | **none** | — | **open** |
| M14 cross-cutting obligations | **none** | — | **open** |
| M15 summary | `99-summary.md` (does not exist yet) | the `[15/15]` row | pending |

**Read this table as the map's honest scorecard: 9 of 15 have a document, and the
five missing ones are named rather than silently counted as done.** The numbering
gap is itself the record — `01`, `02`, `03`, `04`, `05`, `06`, `07`, `09`, `11`
exist; `08`, `10`, `12`, `13`, `14`, `99` do not. The 12-boundary lineage never
filed a row for the CLI surface (M7), the gates (M9, still `pending`), notifications
(M11), the environment adapters (M13) or the cross-cutting obligations (M14) — which
is exactly the shortfall the 15-row `01a0ab12` series was created to close, and why
its module children are still entitled to run.

---

## 2. Coverage proof (nothing unassigned) — re-enumerated 2026-09-17 at `9ce8e159`

The claim in §1 is that every production package and surface belongs to exactly
one module. **The check is the enumeration, run in both directions**: every name the
first command prints must appear in the table below, and every name in the table must
appear in the first command's output. A missing name is a found gap; an invented name
is a fabricated module.

```bash
ls -d internal/*/                        # 43 packages  (first version: 40)
ls cmd/agent/cli_*.go | grep -v _test    # 45 CLI surfaces (first version: 47)
ls cmd/agent/*.go | grep -v _test | grep -v '/cli_'   # 33 non-CLI surfaces = 78 total
ls -d cmd/*/                             # 3: cmd/agent, cmd/ledger-window, cmd/text2img-verify
```

**All 43 `internal/` packages** (each name taken from the command above, not typed
from memory; the deltas from the first version are marked ★):

| package | module | | package | module |
|---|---|---|---|---|
| `agent` | M5 | | `ledgerwindow` ★ | **M1** (surface) / **M9** (the gate pinning it) |
| `agencyscan` | M4 (proposed; M14 if M4 declines) | | `lineedit` | M7 |
| `cgroupscope` | M10 | | `pricing` | M5 |
| `checkpoint` | M5 | | `procname` | M14 (M10 reads it) |
| `clicontract` | M14 (**M7 reads it** — one owner) | | `procstat` | M10 |
| `config` | M14 | | `proxy` | **M13** |
| `daemoncwd` | M10 | | `runcmd` | M14 |
| `daemonlifetime` | M14 (M2 reads it) | | `scratch` | M10 |
| `deepseek` | M5 (model transport) | | `sessionevent` | M14 |
| `doubaoasr` | **M13** | | `sessionlayout` | M14 |
| `gopilog` | M11 | | `shot` | **M13** |
| `heavytest` | M9 | | `signalrec` | M10 |
| `hippocampus` | M4 | | `smoke` ★ | **M9** |
| `pricing`* | M5 | | `smokegate` ★ | **M9** |
| `standards` | M9 | | `stylegate` | M9 |
| `taskreport` | M11 | | `tasktags` | M1 |
| `taskverbs` | M1 | | `taskverdict` | M1 |
| `testutil` | M9 | | `text2img` | **M13** |
| `textguard` | M13 | | `thinkmode` | M5 |
| `toolnames` | M9 (constant source; M6 consumes) | | `tools` | **split** M1/M4/M6/M7/M8 |
| `version` | M12 | | `workerownership` | M2 (+M10 release side) |
| `worktreerelease` | M2 | | `worktreeroots` | M2 |

*(`pricing` appears twice in the left column of the original table; the duplicate row
above is left visible rather than tidied, because a coverage table with a repeated
entry is exactly how a reader concludes a package was "already covered". The entry is
M5.)*

**The three packages and the two `cmd/` entries that had no home before this pass**
— this is the gap the re-enumeration found, and it is the reason the coverage claim
must be re-run rather than trusted:

| new since the first version | lines | module | reason |
|---|---|---|---|
| `internal/ledgerwindow` | 335 | **M1** (consumer), **M9** (gate owner) | one definition of a wall-clock→epoch window against the ledger (`internal/ledgerwindow/epochs.go` `Bounds`/`ErrInvertedWindow`: an inverted window "reads exactly like a quiet period"); the go vet-style gate that keeps it single-definition belongs to M9 |
| `internal/smoke` | 375 | **M9** | the EXECUTED half of the merge/verify smoke gate (`internal/smoke/smoke.go`): a branch touching one package runs only that package |
| `internal/smokegate` | 82 | **M9** | "names the ONE command the merge/verify path runs, and the ONE shape that path must never prescribe again" (`internal/smokegate/smokegate.go:1-2`) |
| `cmd/ledger-window/` | 1 pkg | **M7** (verb), M1 owns the numbers it prints | "Command ledger-window answers the ONE question a script surface needs about a wall-clock window" (`cmd/ledger-window/main.go:1-4`) |
| `internal/textguard` | — | **M13** | it was in the first version's table but in **no** §1 boundary's owns-list; M13 is where it belongs (a text-handling adapter), and it is named here so the assignment is not implicit |

**A reader can verify the whole table with one command pair** (both directions, which
is the acceptance this map is held to). The extraction must take the package name from
**every** `| \`name\` | M<n> |` cell in §2, not just the first cell of a line — the
table is two column-pairs wide, and a line-anchored grep silently misses half of it
(this is the recipe's own bug, found by running it):

```bash
# forward: every package the enumeration prints must appear in the table above
ls -d internal/*/ | sed 's#internal/##;s#/##' | sort > /tmp/actual.txt        # 43
sed -n '/^## 2\./,/^## 3\./p' devdoc/requirements/00-module-map.md \
  | grep -oE '\| `[a-z0-9]+`( ★)?' | grep -oE '`[a-z0-9]+`' \
  | tr -d '`' | sort -u > /tmp/claimed.txt
comm -23 /tmp/actual.txt /tmp/claimed.txt   # must be EMPTY: no package missing
comm -13 /tmp/actual.txt /tmp/claimed.txt   # must be EMPTY: nothing invented
```

Run at `9ce8e159`, this pair is **empty in both directions** — 43 packages enumerated,
43 claimed (verified 2026-09-17). The `cmd/` entries are checked the same way against
`ls -d cmd/*/` (3 names: `agent`, `ledger-window`, `text2img-verify`).

`internal/tools` is the one package that legitimately appears in several modules —
**106** production files, **61,646** lines (`find internal/tools -maxdepth 1 -name
'*.go' ! -name '*_test.go' | wc -l`; re-measured; the first version said 53,901 lines
and 291 files, the latter counting tests). **Whoever owns a file inside `internal/tools`
for their module is responsible for naming the file explicitly**, so two workers cannot
describe the same file twice. The file set is enumerable and finite; a module whose
boundary touches it should list its files in its own document and the union of those
lists is the check.

**`cmd/agent` surfaces = 78 production files = 45 `cli_*.go` + 33 others.** The
non-CLI files are *not* "the CLI by default" — each is assigned, because they are a
different kind of surface (long-running daemons, child management, takeover, timers):

| `cmd/agent` non-CLI files (33) | module |
|---|---|
| `merger_loop.go`, `merger_tick.go`, `merger_restart.go`, `merger_work_lock.go`, `merger_takeover*.go` (the glob expands to **5**: `merger_takeover.go`, `_gate.go`, `_lock.go`, `_progress.go`, `_scope.go` — 9 `merger_*.go` files in all; re-measured 2026-09-18 at `713b2f7a`) | **M2** (the merger's own process surface) |
| `task_lifecycle_tick.go`, `scratch_gc_tick.go`, `tasklistsweep_gate.go`, `task_claim_ground.go`, `task_session_cwd.go`, `task_child_env.go`, `task_child_fail_guard.go`, `child_registry.go`, `child_stop.go`, `agent_spawn_guard.go`, `tmpdir_inject.go` | **M10** (worktree/cwd/cgroup/scratch lifecycle) — `task_claim_ground.go` is M1's claim path, named there |
| `dispatch.go`, `commands.go`, `cmd/agent/main.go` | **M7** (the dispatcher) |
| `chat_stream.go`, `inputtext.go`, `lineedit_wire.go` | **M8** (chat) / **M7** for the input/edit wire — split stated, not left to the reader |
| `daemon_scope_profile.go`, `daemon_userbus_probe.go`, `host_lifecycle_log.go`, `signal_verdict.go` | **M14** (daemon identity/lifecycle) |
| `report_contract_child.go`, `worker_display_preflight.go` | **M11** (what the operator is told) |
| `session_console_log.go` | **M11** |
| `merger_takeover_lock.go`, `merger_takeover_scope.go` | **M2** (already counted above) |

---

## 3. Ordering, and what each worker must carry

Suggested reading order (each document is independent, but M1/M2 are the ones
the operator speaks about most and the ones the previous series started with):

1. **M1, M2** — the ledger and delivery. Highest operator-visibility.
2. **M5, M6, M4** — the agent, its toolbox, its memory. The core loop.
3. **M10, M9** — cleanup and enforcement: where the rules already exist.
4. **M3, M12, M14** — two machines, running binary, shared obligations.
5. **M11, M7, M8, M13** — being told, the cockpit, the conversation, the
   environment adapters (most likely to contain over-builds).
6. **M15** — after all of the above produce a document.

**What a worker needs is one document, not this branch.** The
dispatch-ready briefings are in `01-per-module-briefings.md`; the *template* that
makes them self-contained — the 8-section shape, the method, the why-standard with
worked good/bad pairs, the six constraints C1–C6, the mining query, the honesty
rules — is `00-method-and-standards.md` §4.2 (**authoritative text; paste it whole**).
A worker handed §4.2 plus its own module entry from §1 can produce a conforming
document **without opening this map's siblings** (§3 of the acceptance). If the two
ever disagree, §1 of this map governs *what a module owns* and §4.2 of the method
governs *how the document is written*.

**The assembling instruction, in this file, so the map is usable alone** (the shapes
below are the same as §4.2's; read §4.2 for the worked examples, the mining SQL and
the acceptance test):

```
BRIEFING FOR MODULE <M-n>, "<responsibility in user words>"
  deliverable : devdoc/requirements/<nn>-<module>.md — and nothing else
  step 0      : git worktree add ~/workspace/gopi-<task> -b docs/requirements-<module>
                (never continue another attempt's branch — PROJECT.md Rule 23);
                record the master SHA you read, top of the document
  owns        : <the module's Owns line from this map, §1 — copy it verbatim>
  does not own : <the neighbour modules' names for the packages it merely READS,
                taken from §1 — so two workers cannot claim one package>
  method      : 00-method-and-standards.md §4.2 — paste it whole
  shape       : 8 sections (1 what it is for / 2 who is served / 3 the workflow /
                4 why it exists, cited / 5 acceptance in user terms / 6 GAP ANALYSIS /
                7 implications for existing tests / 8 confidence and limits)
  constraints : C1 one static binary, zero deps · C2 operator controls the whole
                thing · C3 a daemon must be simple · C4 the agent cleans up after
                itself · C5 small team, limited budget · C6 why over what
                → a need that violates one is a §6 gap, not a requirement
  acceptance  : §6 and §8 load-bearing; every claim cited or [opinion]/[inferred];
                ≥1 finding new to the operator, and you name the sentence;
                closing "What I could not determine"; git status shows only your file
```

---

## 4. What this map itself could not determine

- **Who owns `internal/taskreport` — resolved, with the reasoning left visible.**
  The first version listed it in M1's owns-line *and* in M11's owns-line, which is a
  contradiction a worker cannot resolve. **Resolved here: M1 owns the package
  (`internal/tools/task_report.go` is the report the ledger produces); M11 owns its
  delivery (the notify events and the operator-facing surface).** It is left in this
  list because the reasoning is weak: `[inferred]`, and the weakest link is that a
  reader could hand the whole package to M11 instead.
- **Whether `internal/agencyscan` is M4's or M14's.** It scans past sessions to
  propose improvements; that is arguably "memory being used" (M4) and arguably
  "system-level self-improvement" (M14). Background: it is 920 L, 4 files, and
  its own doc comment says it is the "agency-LEVEL-3 improvement-proposal pass"
  `[E4: internal/agencyscan/agencyscan.go:2]`. Proposal: M4 claims it, and the
  M4 worker must state that it did; if it declines, M14 must claim it.
- **Whether `web/` belongs to M8 — resolved by measurement.** There is no `web/`
  directory in this tree (re-measured 2026-09-17: `ls web/` → no such directory).
  The browser surface is `cmd/agent/cli_web.go`, a Go file with the page embedded.
  The M8 worker still judges **whether that surface serves the chat need or a
  different one** — that is the open question — but there is nothing to assign.
- **Whether `deepseek` (3,223 L) is a module.** It is the model transport — the
  API client for the model the agent runs on. It is placed in M5 because the loop
  is meaningless without it; but if the operator regards "which model I run on"
  as a decision he owns, it is a boundary of its own. Flagged, not resolved.
- **The right number of modules.** 14 + umbrella + summary is what the
  user-visible-responsibility test produced from **43** packages. There is no
  external reference for the number — it is `[INFERENCE]`, and its weakest link
  is that an operator may merge two of these in his head (most plausibly M14 with
  M10, or M13 with M6) as soon as he reads them.
- **`internal/lineedit` is 5,421 L, not 4,327 L** (re-measured). It is listed under
  M7 because it is the surface the operator types into, but it is a *library* the
  chat surface (M8) also uses — the same use-vs-ownership distinction M1 makes.
  A reader may reasonably redraw that line; it is named here so they know it was
  drawn deliberately.

---

## 5. Verification of this map (self-filed, per Rule 5)

- **Branch/worktree:** `docs/req-umbrella-verify` at
  `~/workspace/gopi-req-umbrella-verify`, created from master `9ce8e159`
  (`git worktree add … -b docs/req-umbrella-verify`). No other attempt's branch was
  used (Rule 23).
- **Files changed:** `git diff --name-only 9ce8e159..HEAD` → the two
  `devdoc/requirements/00-*.md` files only; `git status --porcelain` → empty (Rule 30).
  Measured against the branch's **own base** `9ce8e159`, not `master`, which advanced
  to `a9c20fa3` during the session and would otherwise contribute a sibling's 15
  files to the answer (see the method doc's Verification note).
- **Coverage, both directions:** the §2 commands were run in this session. Forward
  (`ls -d internal/*/` → 43 names, every one present in the table) and reverse
  (every package name in the table resolves under `internal/`). The three packages
  the first version missed (`ledgerwindow`, `smoke`, `smokegate`) and the two `cmd/`
  entries (`cmd/ledger-window/`, plus `web/` shown to be nonexistent) are named in §2.
- **Measured in this session at `9ce8e159`:** 43 `internal/` packages; 45
  `cmd/agent/cli_*.go`; 78 `cmd/agent` production files (45 + 33); 3 `cmd/` dirs;
  160,428 production LOC / 203,191 test LOC; `internal/tools` 106 files / 61,646 L;
  `internal/standards` 3,015 L; `internal/stylegate` 1,954 L; `internal/lineedit`
  5,421 L; `internal/hippocampus` 1,154 L; `internal/agent` 14,611 L;
  `internal/gopilog` 4,956 L; `internal/proxy` 1,208 L; `internal/doubaoasr` 802 L;
  `internal/shot` 1,961 L; `internal/text2img` 928 L; `internal/ledgerwindow` 335 L;
  `internal/smoke` 375 L; `internal/smokegate` 82 L.
- **Not done by this document:** no module document was edited, no requirement was
  invented to fill a cell, and no code or test was touched (docs-only, Rule 5/7;
  Rule 27 trivially satisfied — no device load was generated).
