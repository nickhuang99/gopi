<!-- doc-class: historical -->
# The declared test entry point now rides the `bash` and `job` descriptions

Task `01a0f41f6f6870b8b9bce200134ea575` · branch `task/01a0f41f6f6870b8b9bce200134ea575`
Evidence base: `devdoc/overlapping-test-runs-diagnosis.md` (branch
`task/01a0f3f72a0173e4a44548a3a8accee2`) — a diagnosis-only row, no code changed.

---

## 1. The decision, and why the literal route 1 was rejected

The epic asked: **bound** concurrent `go test` runs at the declared entry point
(`scripts/test-budget.sh`), or rely on **scope discipline**?

**Route 2 (scope/entry-point discipline) is the chosen route. Route 1 — a
concurrency bound placed INSIDE `scripts/test-budget.sh` — is rejected, because it
provably cannot see the failure mode it was proposed for.**

The diagnosis measured this (its §0, read back from
`~/.gopi/sessions/231291f0-…/session.jsonl`): the incident's two overlapping test
sets were issued by `bash` (line 239, `timeout_sec: 900`) and `job` action=start
(line 178). Both commands were **BARE `go test`**:

```
go test ./internal/tools/ ./cmd/agent/ ./internal/standards/ -count=1 > …/tests.out 2>&1   # job, 04:10:27
go test ./internal/standards/ ./internal/docindex/ ./internal/testutil/ -count=1 …         # bash, 04:13:59
```

`Makefile:41` (`test-budget: scripts/test-budget.sh …`) is the **declared entry
point**, and it routes through the per-package budget, the heavy/smoke skip
ledger and the per-run `go test -json` stream + scope check. **Neither incident
command entered it.** The diagnosis states it in one line: *"no concurrency bound
exists [anywhere] to have been bypassed"* — and, symmetrically, a bound added
inside a script that the failing commands never invoke is a guard standing in a
doorway nobody used. It would have been dead code on the day it landed: green
tests, unchanged behaviour, the same incident still possible.

The second half of the incident points the same way. The foreground call
(line 239, `timeout_sec: 900`) was killed with
`Bash error: command killed: no output for 2m0s` — **it had printed nothing**.
That is not a timeout configuration problem (the caller had asked for 900 s): a
bare `go test` **buffers per package**, so a multi-package run emits nothing
until a package completes. The declared entry point writes a per-run `go test
-json` stream from its first second. So the entry point addresses *both* observed
symptoms — the overlap (via scoped, one-run-at-a-time discipline) and the
silence — and it does so at the place the model actually chooses a command: the
tool description (GOPI.md Rule 11: the description is a tool's ONLY persuasion
mechanism).

## 2. Rule 35 — the four statements (stated because the answer is "no new dependency")

The chosen route adds **no** persistent dependency: no lock file, no state file,
no daemon, no scheduler/cron line, no new script, no new config key, no new env
var, no new syscall. The statements are made anyway, as the rule requires:

1. **What breaks without a new artifact.** Without a *carrier of the declared
   entry point in the tool descriptions*, the observed failure recurs: a run
   issues two bare `go test` sets whose scopes overlap, both bypass the budget /
   skip ledger / scope check, and a foreground one dies silent at the 120 s
   idle-output net. Observed in `session.jsonl` lines 178 / 239 / 242 of session
   `231291f0-…`; `grep -c 'test-budget'` over that session log = **0**.
2. **Why it cannot be fixed in-process.** It *is* fixed in-process — the carrier
   is a Go string constant folded into `claudeToolDefs` (`internal/agent/loop.go`),
   read from the existing `internal/smokegate` leaf package. No process outside
   the binary is involved; there is nothing to schedule, install or reclaim.
3. **Who owns its lifecycle.** The same owner as every other tool description:
   the `claudeToolDefs` registration site, guarded by
   `TestLaunchSuiteToolsNameTheDeclaredTestPoint` in the same package. Nothing
   survives the process; nothing needs a reclaim path.
4. **What the next reader sees.** A reader who finds the sentence in the two
   descriptions sees the reason inline (`bashJobTestEntryPointNote`'s doc comment
   names the incident, its row and the two halves), and a future editor who drops
   it gets a red test naming the exact loss.

**What was deliberately NOT built** (each is a persistent dependency the
acceptance criteria forbid): a lock file under the scratch queue; a
`flock`-based mutex around `go test`; a pid/state file at the entry point; a
scheduler or daemon that serialises test runs; a wrapper script every caller must
route through. Each would exist after the process exits or on a machine we do not
own the lifecycle of, and none of them can see a command that never invokes the
entry point.

## 3. The change

`internal/agent/loop.go`:

- **`bashJobTestEntryPointNote`** — one shared sentence, built by referencing
  `smokegate.Command` (`make test`) and `smokegate.ScopedCommand`
  (`make test-budget PKGS='…'`) plus `smokegate.Doc`. **No new literal**: the
  command spellings live in `internal/smokegate` (Rules 13/14), so the day the
  smoke command changes, the descriptions change with it.
- The note is appended to the **`bash`** and **`job`** descriptions — the two
  tools that actually launch a suite (the incident used exactly these two).
- **`bashJobToolDef(name string) (deepseek.ClaudeToolDef, bool)`** — returns a
  definition with the note folded in, so the registration site and the gate share
  one mechanism instead of two hand-synchronised strings.

## 4. The anti-regression gate (Rule 10.5)

`internal/agent/test_entry_point_description_gate_test.go`:

- **`TestLaunchSuiteToolsNameTheDeclaredTestEntryPoint`** parses `loop.go`,
  descends from the `claudeToolDefs` **declaration only** (the file has a second
  composite literal with `Name: "job"` — the system-prompt section registry —
  and a gate that wandered into it would judge a section header as a tool
  description), evaluates each `Description` expression **with the `smokegate`
  constants' real values substituted**, and requires both the whole-tree and the
  scoped command in the **rendered** description. It also requires **exactly one**
  reference to `bashJobTestEntryPointNote` per description, so a hand-copied
  sentence (which would then drift) fails even when the rendered text happens to
  be right.
- **`TestEntryPointDescriptionGateRedProof`** proves the predicate: the shared
  note renders both commands, a description with no entry point is reported, and
  the prose/bare-literal shapes a grep-based gate would accept are rejected.
- **RED-then-GREEN, measured** (see the report's `## Verification`): with the
  note line removed from the `bash` description, the gate fails with both
  messages — `references bashJobTestEntryPointNote 0 times, want exactly 1` and
  `does not name the declared test entry point "make test"`; restoring it is
  green.

**Why the AST and not a grep**: a grep for `make test-budget` passes on any file
that merely *mentions* the command — a comment, this devdoc, an unrelated
description. The gate must judge the **rendered description of the two
launch-a-suite tools**, which is the only thing the model ever reads.

## 5. The other half: why the description is the right carrier

`docs`-only persuasion fails by construction (Rule 11's measured finding:
`git_blame` 0 calls / 21435, `code_search` 0.3%, both "documented"). The carrier
had to be the text the model reads **at the moment it composes the command**:
the `bash` and `job` tool descriptions shipped in every request. That is what
this change moves, and §6 of the report carries the adoption measurement (real
binary, real runs, session-log `tool_use` records).
