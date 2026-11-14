<!-- doc-class: normative -->
# Per-module briefings [1/N]..[14/N] — dispatch-ready

Self-contained briefings reconstructed by the umbrella ([0/N], `docs/requirements-umbrella`).
Each is written to be pasted into a fresh task payload with **no other context**:
it carries the module's boundary and reason, the evidence seeds the umbrella
already found, the measurements the worker must make itself, and the findings it
must deliver.

**Every briefing implicitly includes `00-method-and-standards.md` §1–§6** (the
five-step method, the evidence classes E1–E6/I/O, the why-standard with good/bad
examples, the three user roles, the eight-section shape with sections 6 and 8
carrying the weight, the six operator-vision constraints as filters, and the
honesty rule). The dispatcher must paste that section text into the payload —
these briefings assume it and do not repeat it.

**Standing constraints for every module worker** (from the epic):

- Worktree/branch first, created by you (Rule 6a):
  `git worktree add ~/workspace/gopi-<task> -b docs/requirements-<module>`.
- Write **only** `devdoc/requirements/<module>.md`. No code, no tests, no
  `make fmt`. `git status --short` must show only your document.
- Do **not** invent requirements. Mark inference `[INFERENCE]` and name its
  weakest link. An honest "why this exists could not be determined" beats a
  confident invention.
- Deliver **at least one finding new to the operator** (gap, over-build, or an
  intent never decided) and say which sentence it is.
- Cite as `file:line`, commit SHA, or ledger task id. Uncited = `[OPINION]`.
- End with **"What I could not determine"**.

**Already-established facts the umbrella verified (reusable, with their commands):**

- 143,015 L production Go / 179,917 L test Go — `find internal cmd -name '*.go' ! -name '*_test.go' | xargs wc -l`
- 40 `internal/` packages, 47 `cmd/agent/cli_*.go` files, 932 `devdoc/` files, `GOPI.md` = 1,085 lines / 29 rules.
- Ledger: 1,607 rows; 557 `source=human`, 686 `auto`, 348 `llm`; 3,251 distilled memory rows; 73,559 session directories.
  `sqlite3 ~/.gopi/memory.db "select source,count(*) from tasks group by source;"`
- A **prior requirements series existed and produced nothing** — rows `01a0aac3`, `01a0aac4` (×2), all `cancelled`; zero documents ever added
  (`git log --all --oneline --diff-filter=A -- 'devdoc/requirements/*'` → empty).
- **Tool adoption has moved since GOPI.md's 2026-08-27 audit.** In the 300 newest session logs (a *sample*, not an audit):
  bash 15,665 | job 3,292 | task 1,942 | read 1,930 | edit 1,874 | memory 995 | write 552 | code_search 142 | context_trim 123 |
  keyword_search 111 | task_submit 59 | vision_analyze 20 | git_blame 6 | **text2img 0, screen_capture 0, web_search 0, web_fetch 0**.
  Reproduce: `ls -t ~/.gopi/sessions/*/*.jsonl | head -300 | xargs cat 2>/dev/null | grep -oE '"name": *"[a-z_]+"' | sort | uniq -c | sort -rn`.
  `GOPI.md:309`'s older figure (git_blame 0/21,435, code_search 0.3%) is **stale and must not be restated as current**.

---

## [1/14] M1 — The task ledger: what the operator asked for, and what happened to it

**Goal.** Reconstruct the user need behind the durable work record, then
gap-analyse it. Reuse the earlier framing the operator himself used on the
cancelled row: *"what a task row is FOR (queue vs memory)"* `[E1: task 01a0aac4]`.

**Boundary + reason.** One module although it spans `internal/tools` (ledger
DSN, claim/reclaim, salvage, control sort, branch attribution), `internal/tasktags`,
`internal/taskverbs`, `internal/taskverdict`, `internal/taskreport`, the
`tasks`/`tasks_*` CLI family, and `internal/tools/ledger_dsn.go`. Reason: "what
happened to my task?" is one user question. Use is not ownership — the CLI, the
merger, the workers and the client sync all *use* the ledger without owning it.
Name each `internal/tools` file you cover, so no file is described twice.

**Evidence seeds.** Schema is in `~/.gopi/memory.db`
(`sqlite3 ~/.gopi/memory.db ".schema tasks"`): status vocabulary
`pending|claimed|in_progress|completed|failed|cancelled|verified|reopened` plus
`priority`, `source`, `deliverable`, `reason`, `proc_pid`/`proc_start_ticks`.
Operator-filed asks that describe ledger behaviour: `01a0aad7` ("merger tick past
due"), `01a0aabb` ("the tests ASSERT the bug"), `01a0aab8` ("client push must not
be gated by the worktree dirty guard").

**Must measure yourself.** (a) How many rows are in a *terminal-but-not-verified*
state right now, and what that means for the operator's trust in the ledger
(re-runnable SQL + the number in the doc). (b) Whether a row can be silently
orphaned (claimed by a dead process with no reclaim evidence) — trace the claim
holder freshness logic. (c) Whether "source" (human/auto/llm) is used anywhere
as a decision input, or is only ever written.

**Required findings.** One of: a status the operator cannot act on; a claim path
that can strand a row; or a ledger field nobody reads. Also state explicitly
whether the ledger serves the operator's *control* need (§5.2 of the standards)
or has drifted into a scheduler abstraction.

**Deliverable.** `devdoc/requirements/task-ledger.md`, 8 sections + constraints +
could-not-determine. Section 7 must name specific existing tests whose verdict
the requirement changes (look in `internal/tools/*_test.go`: claim holder
freshness, live-claim worktree veto, lock probes, ledger DSN).

---

## [2/14] M2 — Delivering finished work to the trunk (merger + branch lifecycle)

**Goal.** Reconstruct the need behind unattended delivery: the operator files
work before sleeping and its result must reach the shared trunk with nobody
present — therefore the merge path must be mechanical (pure git, no LLM
judgement, no waiting on a human) and a branch that cannot land must be *parked*
with its reason preserved rather than allowed to block.

**Boundary + reason.** Merge, park, branch-rename/archive, worktree release,
conflict resolution and the safe-merge gate are one outcome ("did my work
land?") with several verbs; splitting them would separate the merger from the
conflict resolver while they share one state machine.

**Evidence seeds.** `GOPI.md:255` Rule 9 (merge into master → archive the loser
→ re-check; merge only through `scripts/safe-merge.sh`, exit 0 required);
`GOPI.md:311` Rule 12 (merge → cleanup → **rebuild** close loop, build-failed
event); `GOPI.md:755` Rule 23 (never continue on someone else's failed branch —
open a fresh one; but *read* the old branch). ~~Operator asks: `01a0ab04` (parked
branches must be **renamed** `conflict/<name>` — retire the archive-tag carrier)~~
⚠️ **WITHDRAWN 2026-09-17** (`devdoc/branch-lifecycle-ruling.md` §1, task
`01a0af2f`): the rename is VOID — the lifecycle's endpoint is deletion (in
`refs/heads/` or deleted; no state namespace, no `archive/` tag). Kept
struck-through as the record of the reversed decision.
`01a0aaab` ×3 ("resolve merge conflict: … parked at archive/…"). Packages:
`internal/workerownership`, `internal/worktreerelease`, `internal/worktreeroots`,
`cmd/agent/cli_tasks_merge.go`, `cmd/agent/cli_merger*`, `internal/daemonlifetime`.

**Must measure yourself.** (a) How many branches carry a state ref right now and
how long the oldest has been waiting (`git for-each-ref refs/heads`, and any
`refs/tags/archive/*` legacies — the merge sweep's own candidate list is the
authoritative input).
(b) Which steps the merger skips, and which it records as skipped, when a
worktree is dirty — the "no steps on a dirty worktree" family — and state the
correct behaviour in user terms (the safe step runs; only the destructive step
defers) `[E1: task 01a0aab8]`. (c) Whether a parked branch's *reason* survives as
data (a row) or only as a tag name.

**Required findings.** One of: a parked branch the operator can never learn about
from the ledger; a delivery step that is silently skipped; or an over-build in
the recycle/park machinery. Section 7 must name the dirty-worktree tests whose
verdict the requirement changes.

**Deliverable.** `devdoc/requirements/merge-and-branches.md`.

---

## [3/14] M3 — The same state on two machines (sync + node identity)

**Goal.** Reconstruct what must converge between the operator's two hosts, what
must never cross, and what he needs to be true to trust it.

**Boundary + reason.** The ssh client/server legs, the role split, node identity
and the sync leg form one user-visible property ("is my other machine up to
date?"). The role split is a **requirement, not deployment trivia**: peerbox owns
the system layer, HP owns features `[E2: GOPI.md:1002 Rule 28]`, and everything
bad in that incident came from both hosts working one layer at once.

**Evidence seeds.** `GOPI.md:1002` Rule 28 (division, migration = export +
submit + **cancel** the original row, briefing must carry the reason and the
evidence); `GOPI.md:855` Rule 26 (incomplete fix → dispatch again; duplicates
allowed; the briefing carries the reason so the executor judges). Operator asks:
`01a0aab1` ("diagnose: why did HP never push its finished branches to peerbox?
client leg silently skipped"), `01a0aab8` (client push blocked by a worktree
dirty guard). Packages/surfaces: `cmd/agent/cli_tasks_sync*.go`,
`cli_tasks_bridge.go`, `cli_tasks_adopt.go`, `cli_memory_sync.go`/`memsync`,
`internal/sessionevent`, node identity under `~/.gopi/`.

**Must measure yourself.** (a) Where the sync leg can return success while doing
nothing (the silently-skipped shape) — trace and cite. (b) How node identity is
established and what happens on a fresh host. (c) What, concretely, syncs
(ledger rows? events? memory?) — list it; the answer is the requirement.

**Required findings.** One of: a sync outcome the operator cannot distinguish
from success; a role-boundary the code does not enforce (Rule 28 in code vs in
prose); an unstated "who wins" rule on concurrent edits.

**Deliverable.** `devdoc/requirements/sync-and-nodes.md`.

---

## [4/14] M4 — Keeping a long run inside its attention budget (context + memory)

**Goal.** Reconstruct the need: finite attention per run + experience that must
survive the run. Then gap-analyse hard — this is the module most likely to serve
the *operator's wish to watch learning* rather than the *agent's need to not
re-derive*.

**Boundary + reason.** Compaction, the distilled store, context_trim and the
memory brew/feedback/update loop are one need. The store is SQLite; its consumer
is the agent. `internal/agencyscan` — the improvement-proposal pass over past
sessions `[E4: internal/agencyscan/agencyscan.go:2]` — is **proposed as belonging
here**; the worker must state whether it claims it (if it declines, M14 claims it,
and the map says so).

**Evidence seeds.** The `memory` tool's own contract in the tool description
(search/store/update/feedback; "feedback says WHAT is wrong, update does the
FIX"); `.schema tasks` is in `memory.db` alongside `distilled_qa*`,
`procedural_tri*`, `session_conclusions`, `chats*` — the schema itself is
evidence of intended use. 3,251 distilled rows exist. GOPI.md's memory-related
rules (Rules 1/13/14 on constants, Rule 15 on silent failure) constrain how the
store may be written.

**Must measure yourself.** (a) Retrieval reality, not row count: how many
distilled rows have ever been *returned by a search* — approximate honestly from
`chats`/session logs or say it could not be measured and why. (b) What fraction of
`distilled_qa` rows are cold/never-matched (the tier column). (c) Whether
`context_trim` is used to *protect* context or merely to tidy (it had 123 calls in
the 300-log sample — small but non-zero).

**Required findings.** At least one of: stored-but-never-read knowledge;
read-but-never-stored experience; a feedback loop whose vote does not change
behaviour; an over-build relative to "the agent must not re-derive".

**Deliverable.** `devdoc/requirements/context-and-memory.md`.

---

## [5/14] M5 — Working a goal to a verified deliverable (the agent loop)

**Goal.** Reconstruct what the operator buys when he runs gopi: take a
natural-language goal, work it to something a human can check, without a human
in the loop, showing its working.

**Boundary + reason.** The turn loop, the task list it maintains, the in-session
working area, the review verdicts, the "no report = task not finished" rule and
the crash-resume behaviour are one contract between operator and worker.

**Evidence seeds.** `GOPI.md:114` Rule 5 (build → test → commit → **write the
report**; no document = task not finished); the tool description of `task`
(commit/review/reframe semantics, "an honest IMPOSSIBLE beats a fabricated
result"); `internal/checkpoint/parse.go:1` ("extracts resume checkpoints from
gopi session raw logs"). 14,565 L in `internal/agent` — the largest package, itself a
finding candidate. Ledger rows where the loop misbehaved: `01a0aabb`,
`01a0aab8`.

**Must measure yourself.** (a) What actually prevents a run from ending without
a deliverable, and whether it holds for every exit path (including crash, kill,
budget exhaustion) — cite the code. (b) How much of the loop's behaviour exists
to inform the *harness* (tasklist bookkeeping) versus to serve the *operator*.
(c) Whether the review verdicts (A–E) change anything observable.

**Required findings.** One of: an exit path that can end a run with no artifact
and no ledger reason; loop machinery that serves itself; a requirement the loop
enforces that the operator never asked for (a candidate over-build).

**Deliverable.** `devdoc/requirements/agent-loop.md`.

---

## [6/14] M6 — The toolbox and the rules that make the model reach for it

**Goal.** Reconstruct the need: the agent must be able to act on a real machine,
and must *actually reach for* the tools.

**Boundary + reason.** Tool set and routing share one acceptance standard:
a tool the model does not call does not exist `[E2: GOPI.md:298 Rule 11]` —
"any tool development must be accepted by the LLM really calling it; code + unit
tests = half-finished".

**Evidence seeds.** The umbrella's measured adoption table (above) — use it as a
starting point and **re-measure on a larger sample or state the sample size**;
`GOPI.md:309` is stale. `internal/toolnames` (the wire-name single source of
truth, 109 L); PROJECT.md Rules 1b/13/14 (never bypass a constant; shared values in
a leaf package). Tool descriptions live in `internal/agent/loop.go`'s
`claudeToolDefs` with `TestGOPIRule3_MatchesClaudeToolDefs` pinning the list.

**Must measure yourself.** (a) Which tools have **zero** calls in the newest
N session logs (N ≥ 300, state N) — the umbrella found text2img,
screen_capture, web_search, web_fetch at zero in a 300-log sample: confirm or
refute on a bigger sample. (b) For any zero-call tool, whether the routing text
persuades or fails — quote the description's first sentence. (c) Whether the
17-tool count stated at `GOPI.md:87` still matches `claudeToolDefs`
(`internal/agent/loop.go:1098`) — the pinning test is
`internal/agent/gopidoc_sync_test.go:54`.

**Required findings.** At least one: a tool that exists and is never reached for
(with the number that proves it); a routing description that contradicts its own
routing rule; an over-build in tool surface. **Do not restate the stale 2026-08-27
figure as current.**

**Deliverable.** `devdoc/requirements/tools-and-routing.md`.

---

## [7/14] M7 — The operator's cockpit (CLI surface)

**Goal.** Reconstruct the need: one person must control and inspect the whole
system from one place he understands — the operator's own stated preference for
controlling everything over integrating best-of-breed silos (standards §5.2).

**Boundary + reason.** Verb families are the operator's map of the system, so
"is this still holdable by one person?" is asked once, here.

**Evidence seeds.** `cmd/agent/cli_verbs.go:7` ("`cliVerbs` is the single source
<!-- docanchor drift permitted 2026-09-27 (task 01a0e2ea91f37e9185e4f99317484247):
     the doc cites :7, the `cliVerbs` doc comment now opens at :9 (the verb
     registry grew above it); a re-key would move the citation of a measurement
     already taken, and the row's symbol names the site, so the line is listed
     rather than rewritten — repair = re-key by symbol. -->
of truth for the TOP-LEVEL gopi CLI verb") with its own drift-guard note about
the ssh-passthrough lexical scan in `isTasksSubcommand`; 47 `cli_*.go` files;
`internal/clicontract` (330 L — contract-pinning tests); `internal/lineedit`
(4,327 L).

**Must measure yourself.** (a) The **exact** top-level verb count (read
`cliVerbs`) and the subcommand count — these numbers are the answer to "one
person can hold it". (b) Which verbs no workflow uses: cross-reference each verb
against the operator's 557 human ledger rows and the 300-log sample's bash
commands. (c) How many distinct CLI families the *documentation* mentions versus
how many exist (a documentation-drift finding).

**Required findings.** One of: a verb nobody invokes or needs; a family whose size
exceeds what one person can hold (with the number); an undocumented or
double-spelled verb.

**Deliverable.** `devdoc/requirements/cli.md`.

---

## [8/14] M8 — Talking to the agent, live (chat / streaming UI)

**Goal.** Reconstruct the need for a durable, steerable conversation instead of
one-shot piping — or find that the need was never stated, which is the more
likely and more valuable answer.

**Boundary + reason.** Chat/wake/status/web share an interaction need; the web
surface is included only if it serves **that same** need. **The worker must
decide the boundary and state the decision** (merging or separating `web/` are
both acceptable outcomes; an undecided doc is not).

**Evidence seeds.** `cmd/agent/chat_stream.go`, `cli_chat.go`,
`internal/tools/chat.go`, `cli_web.go`; a live worktree `gopi-web-m2-rework`
exists in the main repo (`git worktree list`) — evidence of active work whose
requirement should be findable. Search the human ledger rows for chat/web asks
before concluding none exist: `sqlite3 ~/.gopi/memory.db "select substr(task_id,1,8), substr(summary,1,110) from tasks where source='human' and (summary like '%chat%' or summary like '%web%' or summary like '%ui%');"`

**Must measure yourself.** The count of human asks mentioning chat/web (the
command above) — **this number is the finding**: if it is near zero, the module
is an over-build relative to the stated constraints, and the doc must say so
plainly (standards §5.5).

**Deliverable.** `devdoc/requirements/chat-ui.md`.

---

## [9/14] M9 — Prose rules that did not hold, turned into checks (gates & test infra)

**Goal.** Reconstruct the need: the standards the operator cares most about must
survive the next agent's forgetfulness, so they are computed rather than written.
This is where the operator's own diagnosis about tests most directly applies —
state, in product-owner terms, **why a gate (not a written rule) is the right
instrument**.

**Boundary + reason.** The gate mechanism (baseline + inline escape + data
report), the heavy-test classification and `internal/testutil` are one
responsibility: making a decided standard stick.

**Evidence seeds.** `GOPI.md:433` Rule 15 (no silent failure — `omitempty`
dropping a required field, swallowed errors); Rules 16–21 each pair a checklist
row with a machine check, a red-proof test and a **deliberately rejected**
alternative (see the GOPI.md gate section, `GOPI.md:492`–`695`, and
`devdoc/standards-gate.md`); `GOPI.md:884` Rule 27 (**never** generate device
load to satisfy a test — three incidents, and the reasoning that produced it is
itself the defect); the heavy-test inventory `testdata/heavy-tests.tsv` with
`internal/standards/heavy_inventory_gate_test.go`; `internal/testutil/rule27_gate_test.go`.

**Must measure yourself.** (a) The gate backlog: how many of the 29 rules are
mechanically enforced vs prose-only (count them; cite the gate list) — **this
number is a headline finding for the summary**. (b) The baseline's direction of
travel: rows added vs removed recently (`git log -p internal/standards/baseline.go
| grep -c '^+.*:'`) — a ratchet that is not shrinking is a gate that is not
working. (c) Whether any gate would forbid **correct** behaviour (the
"tests assert the bug" family, `[E1: task 01a0aabb]`).

**Required findings.** One of: a rule with prose but no check (name it); a gate
whose verdict conflicts with a rule's intent; a check policing style rather than
user need.

**Deliverable.** `devdoc/requirements/test-infrastructure.md`.

---

## [10/14] M10 — Not stranding anything (worktree, cwd, cgroup, scratch, temp root)

**Goal.** Reconstruct the promise: two tasks never stand in one directory, a
crash strands nothing, temp work never fills the boot disk, and no process is
judged independent by a proxy signal.

**Boundary + reason.** Rules 22/24/25/29 are four faces of "clean up after
yourself; do not touch what is not yours", and they share their incidents.

**Evidence seeds.** `GOPI.md:695` Rule 22 (temp workspaces never on `/`;
**criterion, not value** — measure the device id; the 2026-09-11 exhaustion from
19G to 1.5G free is the incident; the explicit tmpdir parameter and its
precedence chain live in `internal/config`); `GOPI.md:774` Rule 24 (independence
is judged by cgroup, never PPID/setsid; the `pgrep -f` self-match trap; two
machine-wide kill incidents); `GOPI.md:803` Rule 25 (unprivileged daemons; a
privilege is never the remedy; the anti-regression test
`cmd/agent/daemon_scope_no_privilege_test.go` gates the *text*); `GOPI.md:1045`
Rule 29 (never use another task's worktree as scratch — and the attribution
classifier that makes a borrow *visible*). Operator asks: `01a0aae0` ("worker
stdout should go to console").

**Must measure yourself.** (a) The live residue: stranded worktrees, scratch
directories, or leaked cgroups right now, with the command
(`git worktree list`, `ls <scratch root>`, `systemctl --user list-units '*.scope'`).
(b) Whether the tmpdir default is still off `/` on this host — **measure the
device id**, never trust the spelling. (c) How many places independently decide
"is this process mine" (a duplicated-judgement finding).

**Required findings.** One of: residue that nothing will ever collect; a cleanup
only a lucky exit path performs; the "simple daemon" constraint strained by a
fallback chain (standards §5.3) — say which branch could be deleted.

**Deliverable.** `devdoc/requirements/worktree-and-cwd.md`.

---

## [11/14] M11 — Telling the operator without being asked (notifications & observability)

**Goal.** Reconstruct the need: an absent operator is told when something needs
him, and can afterwards reconstruct what happened.

**Boundary + reason.** "Must reach me" + "I must be able to look back" are one
signal-versus-noise responsibility.

**Evidence seeds.** `GOPI.md:311` Rule 12's `build-failed` notify event
(subscribe via `GOPI_TASK_NOTIFY_EVENTS`); `~/.gopi/notify`; `internal/gopilog`
(4,956 L); `cmd/agent/cli_log.go` + `cli_logstats`; crash dumps
(`internal/agent/crashdump.go`, whose re-panic is the one annotated deliberate
panic per Rule 19); `internal/taskreport` (the report the operator reads);
session layout `~/.gopi/sessions/<task-id>/<session-id>.jsonl` (+ `.raw.log`,
`attachments/`, `output.log`). Operator asks: `01a0aad7` ("merger tick past due"
— the operator noticed a *missing* notification and filed it).

**Must measure yourself.** (a) Notification **volume**: how many notifications
the system emits per day for a week of history (`ls -la ~/.gopi/notify`,
count files/entries) and how many are actionable — this ratio is the finding.
(b) Whether the operator can answer "why did task X fail?" from logs alone:
pick one recent failed row and try, citing what you needed. (c) What is not
reported at all (name one failure mode that leaves no trace).

**Deliverable.** `devdoc/requirements/notifications.md`.

---

## [12/14] M12 — New code is running code (build, rebuild, repo declaration)

**Goal.** Reconstruct the need: after work lands, the machine runs it — the
recurring stale-binary incident (`GOPI.md:311` Rule 12) is the user-visible
consequence.

**Boundary + reason.** The repos table, the declared build, the rebuild after a
merge round, the build-failed signal and the binary's own version identity form
one property: master tip == running binary.

**Evidence seeds.** `GOPI.md:311` Rule 12 in full: declaration (not discovery) in
`settings.json`; **no entry = no build, silently skipped**; the repos table is
also the merger's sweep list; an entry with an empty build is a valid pure merge
target; merge → cleanup → **rebuild**; build failure is *not* a rollback — it
fires `build-failed`. Packages: `internal/version`, the `repos` keys in
`settings.json` (`grep -n '"repos"' ~/.gopi/settings.json` and the repo's own),
`cmd/agent/cli_tasks_rebuild.go`.

**Must measure yourself.** (a) The repos table as it actually is on this host,
and whether GoPi's own build entry is present (Rule 12 *requires* it). (b)
Whether the running binary's version equals master tip **right now** — run the
version comparison; a mismatch is a live finding. (c) Whether the silent
skip on a missing entry conflicts with Rule 15's "no silent failure" (name the
conflict honestly if it does — it is a real finding, not a gotcha).

**Deliverable.** `devdoc/requirements/build-and-repos.md`.

---

## [13/14] M13 — Living in the operator's environment (voice, local models, image I/O, the web)

**Goal.** Reconstruct the need for each environment adapter, or declare that no
requirement was ever decided — this module is the most likely home of over-builds
relative to standards §5.1 (zero-dep) and §5.2 (control over integration).

**Boundary + reason.** Each owns one *interface to the operator's world*
(spoken input, an API shape his other tools already speak, a screenshot of the
live desktop, an image rendered on demand, a model endpoint). Grouped because
they share one failure mode: each pulls the zero-dependency binary toward
external capability, and each must justify itself or be named an over-build.
**Not** grouped with M6 (the agent's hands) or M8 (a chosen channel) — different
user, different acceptance.

**Evidence seeds.** `internal/proxy` (1,208 L: "convertRequest translates an
Anthropic /v1/messages request into the OpenAI Chat Completions request shape",
`internal/proxy/*.go`); `internal/doubaoasr` (802 L) + the `voice*` CLI family
with its own phase-2 spec (`cmd/agent/cli_voice.go` cites
`devdoc/SPEC-voice-input-phase23.md`, and lists modes file/HTTP-server — note it
is exactly the kind of feature that a *spec* exists for, unlike most modules);
`internal/shot` (1,953 L) + `cli_shot.go`/`screencapture`; `internal/text2img`
(928 L) + `cmd/text2img-verify` + `GOPI.md:271` Rule 10 (the text2img case study:
pixel-level truth, constraint dual-path, dependency preflight) — **Rule 10 is the
strongest existing evidence for this module's requirement**, since it is a rule
written *from* these features.

**Must measure yourself.** (a) Usage: calls/mentions in the 300-newest-log sample
for each adapter (voice, shot, text2img, screen_capture, proxy, brew) — the
umbrella's sample shows text2img and screen_capture at **zero** in 300 logs.
(b) Whether `doubaoasr`/`proxy` require credentials or endpoints that make the
"one binary, no dependencies" promise conditional — cite the config keys.
(c) Whether `devdoc/SPEC-voice-input-phase23.md` states a user need at all
(quote it or say it states only implementation).

**Required findings.** At least one of: an adapter with no measurable use;
an adapter that makes the zero-dep promise conditional; the ONE adapter with a
real operator ask behind it (name it opposite the others).

**Deliverable.** `devdoc/requirements/environment-adapters.md`.

---

## [14/14] M14 — Cross-cutting obligations of the running system (identity, config, lifecycle)

**Goal.** Reconstruct the promises every module must keep, so no feature document
has to restate them: one identity for a process, one place a setting comes from,
one truth about a node's role, one event vocabulary.

**Boundary + reason.** Deliberately not owned by any feature; grouping them makes
the *tension between them* visible (e.g. "a daemon must be simple" §5.3 versus
Rule 25's duty to announce a degradation).

**Evidence seeds.** `GOPI.md:69` Rule 2 (single source of truth for deadlines:
constant is the default, exactly ONE place reads the env var — check the code
honours the "exactly one" claim); `GOPI.md:6` Rule 1 (`GOPI_TIMEOUT_SEC` style
env knobs must be named constants; `boolEnvTrue`-style funnels are the approved
shape per Rule 21); Rule 21's env-reader gate (a declared env constant with no
reader is a defect — `grep -rn "GOPI_" internal/config` and cross-check); Rule 12's
tmpdir precedence (`config.ResolveTmpDirConfigured`, documented in
`devdoc/tmpdir-param-not-banner.md`); `internal/procname` (357 L, process
identity), `internal/sessionevent` (43 L — a very thin event vocabulary; is that
enough?), `internal/sessionlayout`, `internal/clicontract`, `internal/runcmd`,
`internal/daemonlifetime`.

**Must measure yourself.** (a) How many distinct env vars gopi reads, and how
many have a real reader (Rule 21's own test
`TestGate6EnvReaderFindsTheRealTree` covers constants — check the *inline*
literals the rule deliberately excludes, and report the count). (b) Whether
`ResolveTmpDirConfigured` is genuinely the only resolution point (grep for
`TMPDIR` across production code; a second reader is a Rule 2 violation and a
finding). (c) What determines a node's role at runtime, and whether an
ambiguous/misconfigured role fails loudly.

**Required findings.** One of: a setting resolved in two places; an env knob the
help text claims works but nothing reads (the exact defect Rule 21 was written
for — check whether it has recurred); a role decided by something other than a
declared setting.

**Deliverable.** `devdoc/requirements/cross-cutting.md`.

---

## [15/15] M15 — The whole-project summary

**Goal.** Read every produced module document (none of them is optional; cite
them) and write `devdoc/requirements/99-summary.md` answering, at whole-project
level:

1. **What gopi is FOR** — product-owner language, no code identifiers.
2. **Which requirements are well served** — with the evidence that they are.
3. **Where the biggest gaps are** — ranked, each traced to a module doc's cited
   evidence.
4. **Where the operator's model and the code's model disagree** — concrete
   instances, with the single sharpest disagreement stated plainly.
5. The consolidated **"what we could not determine"** list across all modules.
6. **What the test-writing phase should pin first** — named requirements, not
   tests written.
7. **The series' own open question:** the earlier `REQUIREMENTS [0/11]` umbrella
   (`01a0aac3`) was `cancelled` with no document produced — nobody recorded *why*
   a requirements effort over the same system stopped. If the operator's reason
   is recoverable from the session log of that run, say what it was; if not, list
   it under could-not-determine. `[E1: task 01a0aac3]`.

**Deliverable.** `devdoc/requirements/99-summary.md`. Do not write tests, do not
change code.
