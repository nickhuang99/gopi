<!-- doc-class: historical -->
# Task Report: internal/agent + internal/config D-class reds — the doc-anchor drift, and the session addresses as constant expressions

**Task:** `01a10e03-9bc6-7427-b7e6-90a47f27f96f` — item [3/4] of the verb-retirement re-land.
**Branch:** `task/01a10e039bc67427b7e690a47f27f96f` (worktree
`/home/user/workspace/gopi-01a10e039bc67427b7e690a47f27f96f`), merged with master
`55a72f0c` before the work started.
**Commit:** `abe51505`.

## Completed

### 1. `TestDocAnchorsResolveInNormativeDocs` — 16 new / 10 stale permits → **0 new / 0 stale**

The gate's own tally before: `240 pre-existing drift entries permitted, 16 new, 10
stale permits`. After: `244 pre-existing drift entries permitted, 0 new, 0 stale
permits`.

Every disposition followed the gate's OWN rule (`internal/agent/docanchor_test.go`):
a **reference** doc whose row annotates its anchor with a symbol name is re-keyed
BY SYMBOL; a **dated landed record** whose numbers ARE its evidence is permitted in
`internal/agent/testdata/docanchor-baseline.tsv`. Nothing was re-keyed by
arithmetic shift, and no literal number was rewritten.

- **14 permits added**, one row per (doc, kind, detail) key. The 16 reported
  entries contain **14 distinct keys**: `devdoc/capability-annotation-handoff.md`
  cites `cmd/agent/usage_test.go:153` twice, and `devdoc/competitor-dsh-analysis.md`
  cites `internal/agent/loop.go:5527` in both the explicit and the bare form —
  `docAnchorScan` reports each occurrence and the permit map is keyed, so ONE row
  covers both (verified: the gate prints 16 entries per run and the tally moved by
  14; the baseline hygiene guard asserts no duplicate key).
  Each entry carries its measured reason, naming the tree it measured and the
  symbol's current line:
  - `devdoc/agent-five-organs-ontology.md` + `devdoc/hippocampus.md` — the
    AUTHORED SPEC pair (2026-09-10, anchor base `2516e09`) citing `TakeJobNotices`
    as the "drain once per round" form to copy; `job.go:297` is now blank, the
    function starts at `job.go:312`. Both docs' own headers say line numbers drift
    and the SYMBOL NAME is the authority.
  - `devdoc/capability-annotation-handoff.md` — dated handoff plan whose both
    citations of the help budget are already followed by the sentence naming the
    raised cap (176); the constant declares at `usage_test.go:166`.
  - `devdoc/competitor-dsh-analysis.md` (×2) — the competitive analysis names its
    baseline commit and its own re-measure table records `loop.go:5508 -> :5527`
    for `parallelSafeTools`; the map declares at `loop.go:5943` today.
  - `devdoc/cov-agent-cr.md` — landed coverage report (base `114cf26`, tip
    `3b187fc`) listing `CloseChatRow`'s callers; they are `cli_web.go:1497` /
    `cli_chat.go:3712` today.
  - `devdoc/docanchor-gate-bare-lineno.md` — the report OF the gate fix, whose
    before/after table quotes the tree it measured (`17cec86b`). Its `before`
    cells must keep the old numbers; re-keying the table would falsify it.
  - `devdoc/hippocampus.md` (`loop.go:4471`) — the row names `MemoryHotResident`;
    the production use is `loop.go:5098` and the described marker was rebuilt.
  - `devdoc/reclaim-deterministic-checklist.md` — 2026-09-17 task report recording
    its own checklist table's line map.
  - `devdoc/requirements/cli.md` — reverse analysis declaring `Base read: master
    812f9a60`; the row counts `gopi memory` sub-verbs at `cli_memory.go:417` today.
  - `devdoc/requirements/merge-and-branches.md` — dated module (pass 2026-09-18)
    citing the argv refusal block that has since moved.
  - `devdoc/verify-boundary-audit-20260912.md` / `verify-responsibility-spec-20260912.md`
    — dated audit + spec (2026-09-12, base `8aa1f9b5`); the spec's sibling rows are
    already permitted in this same list.
  - `devdoc/worker-once-vs-task-run-next.md` — carries its own "SUPERSEDED IN
    PART" banner; `runTaskRun` is `runTaskRunOnce` (`cli_tasks.go:1949`) today.
- **10 stale permits deleted** — the gate names each key verbatim and states the
  remedy: "the site was fixed or is no longer gated; delete the entry". They are
  the `blank` rows in `devdoc/assertion-totality-gate-master-red.md`,
  `audit-ab3-peerbox-2026-09-04.md`, `branch-task-linkage.md`,
  `docanchor-gate-bare-lineno.md` (`job.go:89`, the file no longer has 89 lines of
  that shape), `salvage-20260912-0412-incident.md`,
  `task-ledger-hygiene-20260912.md`, `task-repoarent.md` (×3) and
  `verify-responsibility-spec-20260912.md`.

### 2. The session addresses are CONSTANT EXPRESSIONS — `selfKnowledgeRules` stays `const`

**The epic's premise was checked, not assumed, and it is now measurable:** the
`const -> var` downgrade lives on the two sibling branches only and is **not on
master** you were merged with — but the MECHANISM it describes is real and was
reproduced here, which is why the fix is the right shape for it.

**The defect the fix removes** (the reason it is not cosmetic): master's
`selfKnowledgeRules` teaches `log`/`raw`/`stats` as bare top-level verbs, and
argv[0] **REFUSES** all three since the verb-retirement epic:

```
$ ./bin/gopi log -h
gopi: "log" is not a gopi command — this one is spelled `gopi session log` on the command line (`gopi session log -h` for its help).
$ ./bin/gopi raw -h     → this one is spelled `gopi session raw`
$ ./bin/gopi stats -h   → this one is spelled `gopi session stats`
```

So the prompt routed the model into a refusal on its first attempt to read its own
sessions (Rule 11: the prompt is the only channel that teaches a tool). The address
is what the prompt must carry.

- **`internal/cliverbs`**: `Session` (the group word) + `SessionAddresses =
  Session + " " + Log + "/" + Raw + "/" + Stats` — a **constant expression** over
  this package's own leaf constants, folded at compile time. The leaves are named
  identifiers (`Log`/`Raw`/`Stats`), never retyped literals.
- **`internal/agent`**: the section renders `cliverbs.SessionAddresses`;
  `selfKnowledgeRules` remains **`const`** — no `var`, no new tool, no weakening of
  any assertion.
- **Deliberately NO tool-family twin** (measured): `gopi tool capture` dispatches,
  but `gopi tool ui_state` answers "unknown command ui_state", so a uniform
  `Tool + " " + word` join would teach a dead end for `ui_state`/`chat`/`ui` — the
  very verbs this block is built on. `capture`/`act`/`sandbox` keep their
  top-level spellings, which the refusal table still accepts.
- **New anti-regression guard** `TestSessionAddressesIsAConstantExpression`
  (`internal/agent/self_knowledge_address_test.go`) pins BOTH halves the joiner
  function would have silently lost: (1) the section's declaration is a `const`
  (and no `var selfKnowledgeRules` exists); (2) `cliverbs.SessionAddresses` names
  its leaf identifiers and contains no retyped literal and no call. This is the
  regression that the epic was written against, expressed so it cannot come back
  quietly.
- **`TestSenseVerbDeclarationUsesConstants`** keeps its **literal scan** verbatim
  (`"log"`/`"raw"`/`"stats"` inside the declaration are still red) and keeps its
  "must reference the constant" half; the three session leaves now map to
  `cliverbs.SessionAddresses`, so the "never references `cliverbs.Log`" failure
  cannot be satisfied by DELETING anything — the address must still be rendered.

**Budget check (why the address does not repeat the noun):** the assembled kernel
was at **4483 / 4500** chars. Repeating the noun (`session log/session raw/session
stats`) costs +16 chars and would leave 1 char of headroom against
`systemPromptDensityBudget`. The single-noun address costs +8 and the kernel
measures **4491 / 4500** (9 chars headroom) — measured, not assumed.

## Not Done (honest)

- **`internal/config` was ALREADY GREEN** on the merged tree (0 failures). The
  epic's expectation of reds there did not hold, so nothing was changed in it —
  there was no failure to clear. Reported rather than manufactured.
- The **16-vs-14 key difference** is not "two entries left unfixed": the gate
  reports per OCCURRENCE and permits per KEY. Both colliding keys are permitted
  (one row each) and the gate's tally (0 new) is the proof that every occurrence is
  covered.
- **`selfKnowledgeRules` is not taught the tool/session address for `task`,
  `ui_state`, `capture`, `act`, `sandbox`** beyond what was already there: there is
  no `tool <word>` mirror to move them to (see the `ui_state` measurement above).
  The only leg that needed the address is the session family.
- **cmd/agent's own 121-probe migration** (`01a10e8b` / `01a10e23` [2/4] / [4/4])
  is a sibling task, not this one — this run is [3/4] and touches only
  `internal/agent`, `internal/cliverbs` and the anchor permit list.
- No `git gc`/`git prune` was run: the worktree emitted an "unreachable loose
  objects" warning from a PREVIOUS gc run. That is a host/repo-maintenance matter,
  not part of this task, and touching it could disturb a peer's worktree.

## Verification

- **Build:** `CGO_ENABLED=0 go build ./...` → OK; `CGO_ENABLED=0 go build -o
  bin/gopi ./cmd/agent/` → OK (Rule 12's declared build command).
- **Tests:** `go test -count=1 ./internal/agent/ ./internal/config/` → `ok
  github.com/nick/gopi/internal/agent`, `ok github.com/nick/gopi/internal/config`,
  **EXIT=0**; plus `./internal/cliverbs/` → `ok`.
  Gate tally, verbatim: `doc anchors verified: 2952 across 644 normative docs (2450
  explicit + 502 bare continuations; 604 bare out of row/table/heading scope; **244
  pre-existing drift entries permitted, 0 new, 0 stale permits**)`.
  Baseline hygiene: `internal/agent/testdata/docanchor-baseline.tsv: 244 permit
  rows, no duplicate key, no duplicate line`.
- **Ran:** the changed surface is a SYSTEM-PROMPT SECTION and a leaf constant
  package, so it was executed, not merely compiled:
  1. **The real binary's refusal table** (what the prompt must agree with):
     `./bin/gopi log -h` / `raw -h` / `stats -h` / `session log -h` / `session raw -h`
     / `session stats -h` / `ui_state -h` / `tool ui_state -h` / `tool capture -h`
     / `chat -h` / `ui -h` / `task -h`.
  2. **The EMITTED system prompt, captured from the real wire** (Rule 31: the
     surface this change affects is the prompt the binary SENDS). A local
     zero-spend SSE stub recorded the request body —
     `ANTHROPIC_BASE_URL=http://127.0.0.1:8793 ANTHROPIC_AUTH_TOKEN=stub GOPI_HOME=<session>/home
     GOPI_CHAT_DUMP=0 ./bin/gopi chat --once --tools -p 'say ok'` (the `--tools`
     path is the one that sends the full task prompt, which is where
     `promptSections`/`selfKnowledgeRules` are assembled). The captured block reads:
     ```
     [SELF-KNOWLEDGE — you ARE gopi] `gopi <verb> -h` prints usage. … Yours: session log/raw/stats= sessions, cost; task = your ledger row; capture/screen_capture + act = the desktop; sandbox = a private profile.
     ```
- **Result:** the section renders `session log/raw/stats` (the address), the
  declaration is still `const selfKnowledgeRules`, the gate tally is 0 new / 0
  stale, and the three packages are green.

## Evidence (self-filed)

- Refusal table of the three session leaves (pre-fix prompt vocabulary):
  `./bin/gopi log -h` → `gopi: "log" is not a gopi command — this one is spelled
  \`gopi session log\`…`; same shape for `raw`, `stats` (exit 0, one-line refusal).
- `./bin/gopi tool ui_state -h` → `gopi tool: unknown command "ui_state" — this
  group knows: act|annotate|capture|code-search|doccheck|…` — the measurement that
  forbids a uniform `Tool + " " + word` address.
- Gate tally red→green: before `240 permitted, 16 new, 10 stale permits`; after
  `244 permitted, 0 new, 0 stale permits` (same command, same tree).
- Kernel budget: measured **4483** chars pre-change and **4491** post-change
  against `systemPromptDensityBudget = 4500`.
- Emitted-prompt capture: the stub body (91–92 KB) decoded as
  `"\n".join(b['text'] for b in req['system'])` contains the address string
  `session log/raw/stats` and no bare `log/raw/stats`.
- Source-shape guards: `TestSessionAddressesIsAConstantExpression` PASS;
  `TestSenseVerbDeclarationUsesConstants` PASS; `TestSelfKnowledgeSectionIsStatic`
  PASS.

---

## Merge with master `9396a1af` (the delivery-point sync — Rule 30a, LAST step)

Master had moved five commits, and the merge **landed the premise this task was
written against**: its conflicts were exactly `internal/agent/loop.go` and
`internal/cliverbs/cliverbs.go`, and master's side carried
`func ToolAddress(word string) string` / `func SessionAddress(word string) string`
plus `var selfKnowledgeRules` — the joiner-function mechanism, and the `const ->
var` downgrade it forces. So the fix was re-applied as the resolution:

- **Keep master's INTENT, refuse its MECHANISM.** Every leg of the block still
  names its two-word address (the refusal is real: `gopi sandbox -h` →
  `\`sandbox\` is RETIRED — use \`gopi tool sandbox\``), but the addresses are
  **constant expressions** over the group and leaf constants, so
  `selfKnowledgeRules` is a `const` again.
- New in cliverbs: `ToolCaptureAddress`, `ToolScreenCaptureAddress`,
  `ToolActAddress`, `ToolSandboxAddress`, `SessionLogAddress`, `SessionRawAddress`,
  `SessionStatsAddress`, and `SessionAddresses` (the rendered set).
- `TestSessionAddressesIsAConstantExpression` extended: the declaration must NAME
  every per-leaf constant (`SessionLogAddress`/`SessionRawAddress`/
  `SessionStatsAddress`), contain no retyped literal and no call.
- The prompt renders `session log/session raw/session stats`, `tool capture/tool
  screen_capture + tool act`, `tool sandbox` — captured from the wire again after
  the merge (see `## Verification`).

**Both remaining reds on the merged tree were MASTER's own, measured on a control
worktree** (`git worktree add --detach /home/user/workspace/gopi-ctl-01a10e03
9396a1af`), not imported by this branch:

| gate | master @ `9396a1af` | this branch, merged |
|---|---|---|
| `TestSystemPromptDensity` | **4527** / 4500 FAIL | 4498 / 4500 PASS |
| `TestDocAnchorsResolveInNormativeDocs` | 22 new / 11 stale FAIL | **0 new / 0 stale** PASS |
| `TestEnvAuditProbesRunInsideASandbox` + `…GateAgreesWithTheArtifact` | 2 FAIL | PASS |

Dispositions, so a reader can check the direction rather than trust it:

- **`scripts/audit-env-catalog.sh`** carried master's rename (the flat gate →
  `gopi tool`), but its own hand-off still read `gopi sandbox run`. It now reads
  `gopi tool sandbox run` — the spelling **the gate's own green fixture carries**
  (`envAuditSandboxHandoff = "gopi tool sandbox run"`) and the only one argv[0]
  still accepts. This is the "环境审计产物" item of the briefing, and it is a
  script fix only: the gate is unchanged.
- **`TestSystemPromptDensity`**: the gate's own remedy is "compress the blocks".
  Two clauses this branch owns were tightened (`not just characters` — which
  spends 20 chars repeating the `colour+format per cell-range` clause right before
  it — and `That screen is` → `It is`); the per-leaf session spelling that master's
  test wants costs +16 chars and stays in the assembly, so the kernel renders the
  full address set and measures 4498/4500.
- **docanchor on the merged tree**: 18 new + 4 stale → 14 `loop.go` blank-line
  drifts (all master's line moves), 2 evidence anchors (a quoted briefing seed in
  `requirements/01-per-module-briefings.md`, and `verb-retirement-postmerge-realign.md`,
  whose own header states its trigger has NOT fired and quotes `proxyBannerPrefix`
  as the evidence it measured), 1 `cli_completion_test.go` blank line; the 4 stale
  entries are the two `competitor-dsh-analysis.md` rows, `hippocampus.md`
  (`(bare) loop.go:4471`) and `test-env-isolation.md` (`loop.go:2966`), whose sites
  master's own moves fixed — deleted, as the ratchet's direction requires.
  Every new row's reason is in the TSV; keys came from the probe, never typed.
