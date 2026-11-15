<!-- doc-class: historical -->
# Task Report: Process Independence Judgment Rule (GOPI.md Rule 24)

Branch: `docs/detach-judgment-rule` (worktree `~/workspace/gopi-detach-rule`) — **not merged**.
Deliverable: one new rule in `GOPI.md` + this devdoc. No mechanical gate implemented (proposal only, §6).

## 1. Why this rule exists — the two incidents

Both mass-kill incidents had the same counter-intuitive root: **a process that
*looks* fully detached (own session, own process group, PPID=1) still sits in
the login session's cgroup, and systemd tears the whole scope down as a unit.**

**Incident 1 — 2026-09-12 worker cohort kill** (`devdoc/worker-cohort-kill-20260912.md` §2.1):
four workers died together between 04:12:19 and 04:13:30. The launcher *did*
isolate session and process group, and it still did not matter:

```
$ ps -o pid,ppid,pgid,sid,stat,args -p 380024,380057
    PID    PPID    PGID     SID STAT COMMAND
 380024       1  380024  380024 Ssl  /home/user/work/GoPi/bin/gopi worker
 380057       1  380057  380057 Ssl  /home/user/work/GoPi/bin/gopi worker
$ cat /proc/380024/cgroup -> 0::/user.slice/user-1000.slice/session-18793.scope
$ cat /proc/380057/cgroup -> 0::/user.slice/user-1000.slice/session-18793.scope
$ # setsid child: PID == PGID == SID (own session), cgroup unchanged
wrapper_pid=518953 pgid_of_wrapper=518953
```

`PPID=1`, `SID == own pid`, and the same `session-*.scope` — every member of that
scope is signalled at once when the session ends.

**Incident 2 — 2026-09-13 fleet kill** (`devdoc/signal-provenance-20260913.md`):
four workers + the merger died; the SIGTERM victims' own dumps recorded
`"cgroup":"0::/user.slice/user-1000.slice/session-18200.scope"`, and the same
task proved by exhaustive sweep that **no fleet-scoped kill exists in gopi** —
the kill came from session teardown. That incident's conclusion is the rule's
核心: the honest verdict is a fresh read of `/proc/<pid>/cgroup`, never a
mechanism assertion.

The same trap had been *noticed and accepted* in an earlier devdoc
(`devdoc/worker-daemon-flag-20260912.md`: "the daemon's cgroup/session is not
changed … does not survive a kill of the user's whole session") — "we knew and
accepted it" is what the rule now forecloses for the next author.

**Sibling trap (same family: taking "it looks independent" as evidence)** —
`devdoc/night-report-20260910.md` 失误 2: a fallback script used
`pgrep -f "gopi worker"` to detect workers and matched **itself** (its own
cmdline carries the pattern) → reported a live worker when none existed; the fix
was `pgrep -x gopi`, verified experimentally.

## 2. Mechanism — why PPID / PGID / SID / setsid() are NOT criteria

`setsid(2)` changes exactly two things: it makes the caller a **session leader**
and gives it a **new process group**. Its one security-relevant side effect is
that Linux clears the SIGHUP *disposition* — so a terminal-hangup SIGHUP no
longer reaches the process. All three of these are **session/process-group**
facts.

systemd's per-login resource container is a **cgroup** (`session-N.scope`), and
`setsid` never migrates a process between cgroups — cgroup membership only
changes by writing `cgroup.procs` (or `cgroup.threads`) or by a systemd-managed
spawn. Therefore:

- `PPID=1` — only says the parent exited (or that the process was re-parented);
- `PGID == SID == pid` — only says "own session", a session concept;
- `setsid()` returning success — only says the above two were achieved;
- and (the trap that produced the earlier cgroup-escape epic) **a `cgroup.procs`
  write can return success while `/proc/<pid>/cgroup` is unchanged** — the write
  return value is evidence of nothing.

The **only** observable that answers "did this process leave the login session?"
is `/proc/<pid>/cgroup` **read after the fact** — hence the rule's one-line
check, and hence the correct-practice pointer: a resident process that must
survive session teardown has to *acquire its own cgroup* (`systemd-run --scope`,
measured ~36 ms, `devdoc/cgroup-limiting-not-cleanup-20260911.md:184`; `--user
--scope` only when a user bus actually answers, per the daemon-cgroup-detach
measurements), and if only setsid is available the degradation must be
**announced** — never presented as "detached".

## 3. Landing spot — what was read first, and why Rule 24 (new, at the end)

Read all 23 existing rules (757 lines) before writing. Findings:

- **No existing rule covers process independence.** `grep -in "cgroup|setsid|pgrep|PPID|SIGTERM"` over
  `GOPI.md` before the change returned **zero** hits. So this is a genuinely new
  topic, not a restatement.
- **Not folded into Rule 3** (Tools Are Minimal): that rule is about the tool
  surface (`claudeToolDefs`), asserted by `TestGOPIRule3_MatchesClaudeToolDefs`;
  a cgroup criterion there would arm a tool-inventory test with unrelated text.
- **Not folded into Rule 6** (Isolated Work Directory): Rule 6 is about *where
  code is written* (worktree/clone/branch lifecycle), not about process lifetime.
- **Not folded into Rule 22** (temp workspaces): Rule 22 is the closest *shape*
  precedent — a criterion rule, landed at the file's end, with its own
  criterion test — but its subject (the `/` partition / temp roots) is different;
  merging would make one rule carry two unrelated criteria and would falsify
  `TestGOPIRule22...`'s ownership.
- **Placement after Rule 23**: nearest live neighbours are the operational-rules
  end of the file (Rule 22 criterion, Rule 23 branch discipline). Appending as
  **Rule 24** keeps both existing criterion tests' split-at-heading logic
  (`splitRuleFileAt`) valid and adds no risk to any rule body.

Rejected alternative landing spots (checked, not chosen): Rule 1b/13-adjacent
constant discipline (this is not a literal/constant issue), Rule 10 habits (the
habits list is scoped to the text2img case study), the "Machine-Checkable Gates"
section (that section documents *existing* gates; adding prose for a gate we did
not implement would be a false claim).

## 4. Position fork — GOPI.md vs kernel prompt (operator's call, not made here)

Rule written in **`GOPI.md`**, i.e. the project-level second system message read
from the cwd at runtime (`internal/agent/context.go` `ProjectContext`, loaded in
`internal/agent/loop.go`). Reasoning for choosing GOPI.md:

1. `session-*.scope` is a **host/systemd** fact, not a universal law of
   programming; `internal/agent/prompt_test.go:126-142` asserts the kernel
   prompt stays free of project wording ("a future project without GOPI.md must
   still get a self-sufficient prompt") — the criterion is exactly the kind of
   host-specific claim that test's rationale pushes out of the kernel.
2. The rule's audience is *this repo's* process-spawning code
   (`cmd/agent/cli_daemon_spawn.go`, `internal/tools/job.go`), and GOPI.md is
   where the other repo-operational rules already live (6, 9, 12, 22, 23).
3. Cost: GOPI.md is injected every round, so the rule is written at minimum
   length (~15 lines: ban + counterexample + one-line check + pointer + sibling
   trap + one-line why), with mechanism/detail left here in this devdoc.

**If the operator disagrees** — e.g. because the sibling `pgrep -f` trap is
host-independent and worth a kernel line — the split would be: kernel gets the
generic "never infer liveness/independence from cmdline patterns or session
ids alone" line; GOPI.md keeps the host-specific cgroup criterion. Not done
here; recorded so the decision is visible rather than assumed.

## 5. Rule content as landed

`GOPI.md` Rule 24 — "Process Independence Is Judged by cgroup — Never by PPID or
setsid": criterion (`/proc/<pid>/cgroup` must not match `session-*.scope`);
counterexample (`SysProcAttr{Setsid: true}` / `setsid nohup … &` do not change the
cgroup, still killed with the session); one-line check
(`grep -q 'session-.*\.scope' /proc/<pid>/cgroup && echo "NOT detached" || echo "detached"`);
correct practice (acquire an own cgroup — `systemd-run --scope`, ~36 ms; announce
a setsid-only degradation); sibling trap (`pgrep -f` self-match → `pgrep -x` /
`ps -p`); one-line why with the two incident devdocs referenced.

## 6. Proposed gate (NOT implemented — out of scope for this task)

Proposal only, for a future task. Shape: a criterion-style test alongside
`internal/testutil/tmproot_assert_test.go` (Rule 22 precedent), i.e.
`TestGOPIHasTheDetachCriterion` + `TestGOPIDetachCriterionIsNotADuplicate`
(`splitRuleFileAt`/`ruleSentences` helpers are already generic).

The rule's *prose* half is easy to gate statically; the *code* half is the
valuable one and needs care not to repeat the `%v`-gate mistake (a red-on-day-one
gate gets disabled):

1. **Rule presence (green immediately)**: Rule 24 body mentions
   `/proc/<pid>/cgroup`, `session-*.scope`, `setsid`, the literal `pgrep -f`
   trap and a `grep -q` verification line; its sentences are not verbatim
   duplicates of Rules 1–23.
2. **Code half (needs a reviewed baseline, NOT day-one red)**:
   `SysProcAttr{Setsid: true}` sites whose stated intent is *survival past the
   session* must (a) be accompanied by a real cgroup escape, or (b) carry an
   announced-degradation path, or (c) be annotated as deliberately
   session-scoped. Today's three production sites:
   - `cmd/agent/cli_daemon_spawn.go:295` — already correct (setsid only on the
     non-systemd path, with a documented degradation ladder) → would be an
     annotated/baselined site.
   - `internal/tools/job.go:138` — deliberately session-scoped (a child job
     bounded by `JobKill`); the intent is the *opposite* of survival, so it must
     be annotatable, otherwise the gate would demand a cgroup escape for a
     short-lived child.
   - `internal/testutil/mockproc.go:140` — test fixture.
   Trap to encode in the gate's help text: intent cannot be read from the AST,
   so a pure "Setsid ⇒ must detach" predicate is a false-positive machine. The
   honest gate is "**a resident/daemon spawn path must not rely on setsid for
   survival**" — i.e. scoped to the daemon/worker/merger spawn seam, not to
   every `Setsid`. This needs a design decision (annotation grammar vs seam
   allowlist) → separate task.

## 7. Verification

## 7. Verification (all run in the worktree `~/workspace/gopi-detach-rule`)

- `go build ./...` → `BUILD_OK` (exit 0).
- Injection-contract tests, `go test ./internal/agent/ -run 'GOPI|Kernel|Prompt|Resume|System' -count=1`:
  `ok github.com/the operator/gopi/internal/agent 0.299s` — includes the resume test
  asserting exactly two system messages `[kernel, GOPI.md]` and the kernel-purity
  test (project wording must NOT be in the kernel). Both still pass.
- Existing criterion tests that scan the file this change edited,
  `go test ./internal/testutil/ -run 'TestGOPIHasTheTempRootCriterion|TestGOPITempRootCriterionIsNotADuplicate' -count=1`:
  `--- PASS: TestGOPIHasTheTempRootCriterion` and
  `Rule 22: 74 sentences checked, none duplicated from Rules 1–21` → the new
  Rule 24 did not disturb the existing split-at-heading logic (`splitRuleFileAt`)
  and Rule 22 stays unique.
- **Real short session (the "not just reading it and believing it" check)**:
  built `bin/gopi` from this worktree and ran a live session with cwd = the
  worktree:

  ```
  $ CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/ && echo BUILT
  BUILT
  $ echo "Reply with just: OK" | timeout 180 ./bin/gopi
  [gopi] loaded GOPI.md (39965 bytes)
  [gopi] session: /home/user/.gopi/sessions/cce5391b-.../session.jsonl
  ```

  `39965` bytes = `wc -c GOPI.md` in the worktree exactly, i.e. the edited file
  is the one loaded. And the raw payload log for that session contains the rule
  verbatim (3 occurrences), including its criterion, counterexample, one-line
  check and sibling trap:

  ```
  $ f=~/.gopi/sessions/69a75926-.../69a75926-...raw.log
  $ grep -c "Process Independence Is Judged by cgroup" "$f"   → 3
  $ grep -o "session-\*\.scope" "$f" | head -2               → session-*.scope
  $ grep -o 'pgrep -f' "$f" | head -2                        → pgrep -f
  ```

  Session `cce5391b-d2a9-42c1-a677-e0090ce0c730`, raw log
  `69a75926-4095-4b1b-9c7f-6525357ff683.raw.log`. Note `ProjectContext` is part
  of the request payload (the raw log), while the on-disk `session.jsonl`
  records the `session_start` metadata — so the raw log is the right artifact to
  quote.
- Diff scope: `git diff --stat` → `GOPI.md | 29 +++++` (1 file, 29 insertions,
  0 deletions) + one new devdoc. File grew 757 → 786 lines.
- `grep -in 'cgroup|setsid|pgrep|PPID|SIGTERM' GOPI.md` before the change: 0 hits
  → no duplication of an existing rule.


## 8. Not done / deferred

- No mechanical gate implemented (explicitly out of scope; §6 is a proposal).
- No kernel-prompt change (§4 records the fork for the operator).
- No merge — branch `docs/detach-judgment-rule` is the deliverable.
- No `make fmt` (per task constraint); `gofmt` on Go files not needed — only
  `GOPI.md` (Markdown) and this devdoc changed.
