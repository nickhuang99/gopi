<!-- doc-class: historical -->
# Task Report: one peer-alias validator — closing the ssh option-injection hole

## What the bug was

Every ssh leg of the peer-sync path validated its peer alias inline:

```go
if strings.TrimSpace(peer) == "" || strings.ContainsAny(peer, " \t\n;") { ... }
```

and then passed `peer` as an ssh **argv element**:

```go
exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", peer, query)
```

ssh parses OPTIONS and the HOST from the same argv vector, so an alias that
begins with `-` is an option, not a host. The old guard never looked at the
first character, so `-oProxyCommand=/tmp/payload` passed it and reached ssh.

Verified **pre-fix** (base of this branch, worktree at `040933b`, GoPi):

```
TaskDiffPeer("-oProxyCommand=x") => diff: ssh -oProxyCommand=x: exit status 255
```

— ssh answered, i.e. the option was executed. Because the old guard already
refused whitespace, the injectable shape is a **single token** whose value is
an argument-less command (`-oProxyCommand=sh`, `-oLocalCommand=id`,
`-F/tmp/cfg`), so severity is bounded but real: arbitrary local command
execution whenever the alias arrives from a compromised/shared
`settings.json`, `--peer`, `--report-to`, `--repo`, or a `compute_nodes`
entry. ssh is only contacted from the client process, so the executing host
is the one that runs the task.

## Completed

1. **One validator, one file** — `internal/tools/peer_alias.go`:
   * `ValidateSSHPeerAlias(peer) (string, error)` — trims (long-standing
     padding tolerance kept), refuses blank / leading `-` / the original
     whitespace+`;` set / the git-target separator `:`.
   * `ValidateSSHGitTarget(target) (string, error)` — validates the **alias
     half** of a caller-composed `<alias>:<path>` target (the path half is
     git's business; git quotes it into the remote command, never into ssh's
     option vector).
   * `errInvalidPeerAlias` sentinel wrapping the phrase **`invalid peer`** —
     the pre-existing coverage tests (`memory_sync_reconcile_cov_test.go`,
     `memory_sync_task_gitpushback_cov_test.go`) and the operator-facing
     output match on that phrase, so it must survive.
   * Every literal lives in a named constant (`sshPeerAliasOptionPrefix`,
     `sshPeerAliasForbiddenChars`, `sshPeerAliasTargetSeparator`) — Rule 1b.

2. **Every ssh leg rewired to reference it** (Rule 13/14 — no leg keeps its
   own copy of the rule):

   | leg | file | refusal shape |
   |---|---|---|
   | `TaskDiffPeer` | memory_sync_reconcile.go | `diff: invalid peer alias …` |
   | `reconRemoteRowSets` | memory_sync_reconcile.go | `diff: …` (pre-fix UNGUARDED) |
   | `reconRemotePendingSet` | memory_sync_reconcile.go | `anchor guard: …` (pre-fix UNGUARDED) |
   | `harvestRemoteTasks` | memory_sync_task.go | `harvest: …` |
   | `gitPushbackDefault` / `DefaultGitPushback` | memory_sync_task.go | `harvest git-pushback: …` |
   | `syncSSHExchange` | memory_sync_session.go | `memsync sync: …` (guard BEFORE the test seam) |
   | `syncAssetsSSHExchange` | memory_sync_assets.go | `memsync assets: …` (pre-fix UNGUARDED) |
   | `claimSSHExchange` | memory_sync_claim.go | `claim: …` (pre-fix UNGUARDED) |
   | `GitSyncServerTarget` | task_sync_git.go | option-shaped declared peer ⇒ renders `""` (fail-soft) |
   | `finishCleanupServerTarget` | task_finish_cleanup.go | same, `""` |
   | `GitSyncPushLocalBranches` / `GitSyncClientPass` | task_sync_git.go | refuse/skip when the target carries `:` |
   | `dispatchSSHArgs` + node dispatch (`node.Name`/`ReportTo`/`Repo`) | cmd/agent/dispatch.go | error before any hop |
   | `--peer` / `--report-to` / `--repo` argv | cmd/agent/cli_tasks.go | refused at PARSE time |
   | report-back hop | cmd/agent/cli_tasks.go `writebackOutcome` | refused, local writeback still recorded |

   Two deliberate asymmetries, both documented in code:
   * the guard in `syncSSHExchange`/`syncAssetsSSHExchange` runs **before**
     the `syncSSHExchangeHook` seam, so an in-process test seam can never
     bypass it;
   * the git legs only validate when the target contains `:` — a local
     filesystem target (the fixtures' `<dir>/peer.git`) never reaches ssh and
     stays untouched.
   The stricter set is a **strict superset** of the old one: every alias the
   old guard accepted except option-shaped ones is still accepted, and no
   pre-existing test passes a `:`-carrying or dash-leading alias to a
   peer-level face.

3. **Regression tests** (the fix ships its repro, Rule 10.5):
   * `internal/tools/peer_alias_test.go`
     * `TestValidateSSHPeerAliasRefusalTable` — accept/refuse table
       (`user@host`, padding tolerance) + sentinel identity + the
       `invalid peer` phrase.
     * `TestValidateSSHGitTargetSplitsAliasHalf` — accept/refuse table for
       composed targets.
     * `TestSSHPeerAliasGuardRefusesOptionShapedAliasAtEveryLeg` — the FACE
       test: the epic's `-oProxyCommand=/tmp/payload` alias refused at every
       leg above, including the assertion that the transport seam did NOT
       run (hook-counter).
     * `TestPeerAliasLiteralGate_OldGuardShapeMustNotReappear` — Rule 13
       literal gate: no production file in `internal/tools`/`cmd/agent` may
       spell the old per-leg guard shape again.
     * `TestPeerAliasGuardDocumentedAtEverySSHLeg` — every `exec … "ssh"`
       site must sit in a file that references the validator.
   * `cmd/agent/peer_alias_guard_test.go` — `--peer`/`--report-to`/`--repo`
     parse-time refusal, `dispatchSSHArgs` hardening, node-dispatch refusal.
   * `internal/tools/memory_sync_reconcile_cov_test.go` — the NOTE that
     declared option-shaped aliases out of scope is **replaced**: the shape
     is now asserted refused.

## Not Done

* The `-l`/`-F`-style shapes are refused by the same leading-dash rule; no
  separate test per ssh option (the rule is character-level, not
  option-table-level) — deliberate, not a gap.
* `TaskDiffPeer`'s happy path over a real ssh peer remains unexercised
  (the `reconRemoteRowSets` seam gap is a separate filed ticket,
  `01a08c35-3bb0-...`), unchanged by this work.
* No E2E against a real peer: the refusal is asserted before any transport
  call, so ssh is never needed to prove the injection is closed.

## Verification

* Repro inverted at the same face the epic named:
  `TaskDiffPeer("-oProxyCommand=x")` → `diff: invalid peer alias
  "-oProxyCommand=x": a leading "-" makes ssh parse it as an OPTION (e.g.
  -oProxyCommand=… runs a local command), not a host`
  (pre-fix: `diff: ssh -oProxyCommand=x: exit status 255`).
* `CGO_ENABLED=0 go build ./...` → OK; `go vet ./internal/tools/ ./cmd/agent/` → clean.
* `go test ./internal/tools/ ./cmd/agent/` → see the commit message / raw log
  tail pasted in the report footer (both packages ok).
* Sweeps: the only `ContainsAny(peer, " \t\n;")` left in production is the
  comment that documents the old shape; every `exec … "ssh"`
  (`memory_sync_task.go:1468`, `memory_sync_claim_ssh.go:20`,
  `memory_sync_session.go:194`, `memory_sync_assets.go:220`,
  `memory_sync_reconcile.go:138/371`) is inside a guarded leg.

## Design notes / traps

* **Sentinel text is a compatibility contract.** The old legs printed
  `invalid peer %q (want an ssh alias)` and two coverage tests match on
  `"invalid peer"`; the sentinel keeps that phrase while adding the reason.
* **`:` refusal is the git-target half of the same hole.** The git push-back
  legs take a composed `<alias>:<path>` and hand the host to ssh, so the
  alias boundary must refuse a separator the caller should have composed.
* **Fail-soft at the topology renderers.** A declared peer that is
  option-shaped makes `GitSyncServerTarget`/`finishCleanupServerTarget`
  render no target instead of a broken one — the callers already skip on
  empty, and an injection shape must not become a "best effort" push.
* **Test-side trap I hit:** asserting the offending alias with
  `strings.Contains(err, input)` fails for tab/newline inputs because the
  message quotes with `%q` (which escapes them) — the test must compare the
  quoted form.
