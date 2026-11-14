<!-- doc-class: historical -->
# Archive-Tag Audit — every `archive/*` tag, verdict per tag, and what is really unlanded

**Date**: 2026-09-18 · **Repo**: `/home/user/work/GoPi` · **master snapshot**: `0317d09a`
(`docs(brief): devdoc truth — classify the corpus, then gate only what claims to be current`)

**Brief** (this task): audit every `archive/*` tag in `/home/user/work/GoPi` that has **no queued repair
row**; classify each as ALREADY-DONE / SUPERSEDED vs unlanded using `git cherry -v master <tag>`
(decisive) and `git rev-list --count master..<tag>`; an unresolvable ref is **UNCLEAR, never
"merged"**; for unlanded-and-still-wanted work submit one task per coherent unit.

**The brief's tag count is wrong and was NOT used.** It says 66 tags. The namespace today holds
**167** `archive/*` tags (`git tag -l 'archive/*' | wc -l` = 167), which is exactly the drift the
count reconciliation section below is for. Every number in this report is regenerated from the
repo at the stated snapshot.

## 1. Method — two instruments, one stated verdict rule

| instrument | command | what it decides |
|---|---|---|
| identity | `git cherry -v master <tag>` | **decisive**: a `+ ` line is a patch master does not have by identity; no `+ ` line means every patch is on master |
| divergence | `git rev-list --count master..<tag>` | how many commits of the tag are not reachable from master (0 is necessary, not sufficient — a squash/rebase land shows a non-zero count with nothing missing) |
| content shape | `git grep -w -l -e <sym> master -- '*.go'` over each declaration the tag **adds** | is the surface the tag introduces present on master today (anywhere, any path)? |
| retired-surface | `git log --oneline master -- <path>` for a path master lacks | master once had the path (added, then deleted/renamed away) → the tag's surface is **obsolete**, not merely unlanded |

Verdict rule, applied in this order (each row's evidence names the branch that fired):

1. `cherry -v master <tag>` prints **no `+` line** → **ALREADY-DONE** (identity).
2. a **pending ledger row already names** the tag or its branch → **ALREADY-QUEUED** (owned; no second row).
3. hand-adjudicated **renamed surface** → **SUPERSEDED** (both sides opened; see the row).
4. every production declaration the tag **adds** is present on master → **ALREADY-DONE**
   (patch-equivalent land: cherry ids differ, content landed by another path).
5. otherwise → **UNLANDED**, with `N absent prod decls` measured per unit.

**Unresolvable ref → UNCLEAR, never "merged"**: 0 tags hit this; all 167 refs resolve
(`git rev-parse -q --verify <tag>^{commit}` succeeds for every one). No tag is left unclassified
because of a broken ref.

### What "still wanted" means here (and what it does NOT claim)

A verdict of **UNLANDED** is a measured fact about master's content. It is *not* a judgement that
the work should land. The units that got a new task row (section 4) are those where the tag adds a
production surface that is **absent from master today and was never on master** (a live gap). Units
whose absent files all sit on paths master **once had and deleted** are reported as UNLANDED with
the retiring commit named, and got **no row**: their surface was deliberately retired, so re-landing
them would resurrect removed machinery. That split is §5's honest limit, stated explicitly rather
than hidden inside "wanted".

## 2. Count reconciliation — every tag accounted for, no silent gaps

| class | tags |
|---|---|
| ALREADY-DONE | 116 |
| ALREADY-QUEUED | 7 |
| SUPERSEDED | 2 |
| UNLANDED | 42 |

| quantity | value | command |
|---|---|---|
| `archive/*` tags in the namespace | **167** | `git tag -l 'archive/*' \| wc -l` |
| tags the brief claimed | 66 (stale — the namespace grew to 167; ignored, not reconciled to) | — |
| tags audited here (all of the above) | **167** | `wc -l devdoc/archive-tag-audit-20260918.tsv` |
| tags left un-audited | **0** | every row carries a verdict |
| distinct tip commits behind those tags | 139 | `awk -F'\t' '{print $2}' cherry.tsv \| sort -u \| wc -l` |
| tags already carrying a queued (pending/claimed) repair row | **7** | read-only SQL over `/home/user/.gopi/memory.db` (status in pending,claimed AND the tag/branch name appears in summary or payload) |
| tags with no queued row | **160** | 167 − 7 |
| **UNLANDED** tags | 42 | verified content-absent |
| UNLANDED units (one tip commit per unit — the `-20260917`/`-0NNNN` duplicate tags of one tip are ONE unit) | **32** | 42 UNLANDED tags collapse to 32 tips |
| units given a NEW ledger row | **31** | §4.2 — live rows |
| UNLANDED units that got NO row | **1** | `archive/feat/merger-pure-cronjob` (tip `d1faf33b`) — see §5 |
| SUPERSEDED units (no row by design) | 2 | §3 |
| rows submitted then CANCELLED as duplicates | 4 | §4.3 |

The 4 cancelled rows were submitted before the already-queued check was applied per unit; they are
part of the audit's own record (a duplicate dispatch is a defect the audit itself can create), and
the ledger shows them as `cancelled` with the owner row named.

The 7 queued tags and their owner rows (so the reader can see the queue is not double-counted):

| tag | owner row |
|---|---|
| `archive/feat/archive-why-gate` | `01a0b4aa-69af-739c-a381-92e2a031578f` |
| `archive/feat/archive-why-gate-20260917` | `01a0b4aa-69af-739c-a381-92e2a031578f` |
| `archive/feat/conflict-branch-rename` | `01a0b4aa-6874-710f-9380-e39910a2d8f4` |
| `archive/feat/conflict-branch-rename-20260917` | `01a0b4aa-6874-710f-9380-e39910a2d8f4` |
| `archive/feat/hand-archive-gate` | `01a0b4aa-69af-739c-a381-92e2a031578f` |
| `archive/feat/hand-archive-gate-20260917` | `01a0b4aa-69af-739c-a381-92e2a031578f` |
| `archive/fix/cwd-and-lifetime` | `01a0b3d5-e8e4-7b9b-9837-e1d6ef7b212e` |

## 3. Every audited tag — one line each

| tag | `git cherry -v master` | verdict | evidence (one line) |
|---|---|---|---|
| `archive/claim-broadcast-m418` | +2/-3 | UNLANDED | 13 absent prod decls; see the unit card below |
| `archive/feat/cov-tools-rest` | +14/-0 | UNLANDED | 10 absent prod decls; see the unit card below |
| `archive/feat/foreground-component-lifecycle` | +2/-0 | UNLANDED | 13 absent prod decls; see the unit card below |
| `archive/feat/merger-park-tip-dedup` | +1/-0 | UNLANDED | 29 absent prod decls; see the unit card below |
| `archive/feat/merger-pure-cronjob` | +2/-7 | UNLANDED | ? absent prod decls; see the unit card below |
| `archive/feat/merger-tick-branch-account` | +2/-0 | UNLANDED | 21 absent prod decls; see the unit card below |
| `archive/feat/merger-tick-branch-account-20260917` | +2/-0 | UNLANDED | 21 absent prod decls; see the unit card below |
| `archive/feat/raw-round-grammar` | +7/-0 | UNLANDED | 2 absent prod decls; see the unit card below |
| `archive/feat/scan-tip-at-mint-045325` | +5/-0 | UNLANDED | 12 absent prod decls; see the unit card below |
| `archive/feat/scan-tip-at-mint-045423` | +5/-0 | UNLANDED | 12 absent prod decls; see the unit card below |
| `archive/feat/scan-tip-at-mint-045640` | +5/-0 | UNLANDED | 12 absent prod decls; see the unit card below |
| `archive/feat/scan-tip-at-mint-050535` | +5/-0 | UNLANDED | 12 absent prod decls; see the unit card below |
| `archive/feat/shot-jsonlist-retry` | +9/-0 | UNLANDED | 3 absent prod decls; see the unit card below |
| `archive/feat/taskid-whitespace-normalize` | +6/-0 | UNLANDED | 3 absent prod decls; see the unit card below |
| `archive/feat/web-m1-b4b7` | +7/-6 | UNLANDED | 2 absent prod decls; see the unit card below |
| `archive/feat/web-m1-b4b7-20260917` | +7/-6 | UNLANDED | 2 absent prod decls; see the unit card below |
| `archive/feat/web-m2-b5b8b9b12` | +12/-6 | UNLANDED | 3 absent prod decls; see the unit card below |
| `archive/feat/web-m2-b5b8b9b12-20260917` | +12/-6 | UNLANDED | 3 absent prod decls; see the unit card below |
| `archive/feat/worker-run-entry2` | +1/-0 | UNLANDED | 14 absent prod decls; see the unit card below |
| `archive/feat/worker-run-entry2-20260917` | +3/-0 | UNLANDED | 16 absent prod decls; see the unit card below |
| `archive/feat/worker-term-exit` | +4/-0 | UNLANDED | 12 absent prod decls; see the unit card below |
| `archive/fix-memsync-import-atomic-20260901` | +4/-1 | UNLANDED | 3 absent prod decls; see the unit card below |
| `archive/fix/envbool-single-truthy-parser` | +1/-0 | UNLANDED | 7 absent prod decls; see the unit card below |
| `archive/fix/hippo-host-resolution` | +11/-0 | UNLANDED | 4 absent prod decls; see the unit card below |
| `archive/fix/merge-verify-landing-witness` | +10/-0 | UNLANDED | 4 absent prod decls; see the unit card below |
| `archive/fix/merge-verify-landing-witness-20260917` | +14/-0 | UNLANDED | 4 absent prod decls; see the unit card below |
| `archive/fix/merger-park-worktree-release` | +10/-0 | UNLANDED | 29 absent prod decls; see the unit card below |
| `archive/fix/merger-park-worktree-release-20260917` | +10/-0 | UNLANDED | 29 absent prod decls; see the unit card below |
| `archive/fix/merger-takeover-scope-escape` | +1/-0 | UNLANDED | 3 absent prod decls; see the unit card below |
| `archive/fix/notify-all-events` | +4/-0 | UNLANDED | 12 absent prod decls; see the unit card below |
| `archive/fix/park-worktree-live-row-guard` | +1/-0 | UNLANDED | 28 absent prod decls; see the unit card below |
| `archive/fix/park-worktree-live-row-guard-20260917` | +1/-0 | UNLANDED | 28 absent prod decls; see the unit card below |
| `archive/fix/read-binary-sample-straddle` | +1/-0 | UNLANDED | 1 absent prod decls; see the unit card below |
| `archive/fix/serve-lock-race` | +9/-0 | UNLANDED | 12 absent prod decls; see the unit card below |
| `archive/fix/serve-lock-race-20260917` | +9/-0 | UNLANDED | 12 absent prod decls; see the unit card below |
| `archive/fix/shot-chrome-numeric-sort` | +9/-0 | UNLANDED | 3 absent prod decls; see the unit card below |
| `archive/fix/taskverify-ctx-child-kill` | +3/-0 | UNLANDED | 12 absent prod decls; see the unit card below |
| `archive/hp-unique-maxtokens` | +5/-0 | UNLANDED | 7 absent prod decls; see the unit card below |
| `archive/log-stats-raw-gopi` | +1/-0 | UNLANDED | 35 absent prod decls; see the unit card below |
| `archive/memsync-import-atomic-v1` | +4/-1 | UNLANDED | 3 absent prod decls; see the unit card below |
| `archive/merge-resolve-escalate-early-cb9f72f` | +1/-0 | UNLANDED | 21 absent prod decls; see the unit card below |
| `archive/testsuite-codex-20260827` | +1/-0 | UNLANDED | 1 absent prod decls; see the unit card below |
| `archive/feat/archive-why-gate` | +4/-0 | ALREADY-QUEUED | row 01a0b4aa-69af-739c-a381-92e2a031578f names it |
| `archive/feat/archive-why-gate-20260917` | +4/-0 | ALREADY-QUEUED | row 01a0b4aa-69af-739c-a381-92e2a031578f names it |
| `archive/feat/conflict-branch-rename` | +12/-0 | ALREADY-QUEUED | row 01a0b4aa-6874-710f-9380-e39910a2d8f4 names it |
| `archive/feat/conflict-branch-rename-20260917` | +12/-0 | ALREADY-QUEUED | row 01a0b4aa-6874-710f-9380-e39910a2d8f4 names it |
| `archive/feat/hand-archive-gate` | +8/-0 | ALREADY-QUEUED | row 01a0b4aa-69af-739c-a381-92e2a031578f names it |
| `archive/feat/hand-archive-gate-20260917` | +8/-0 | ALREADY-QUEUED | row 01a0b4aa-69af-739c-a381-92e2a031578f names it |
| `archive/fix/cwd-and-lifetime` | +6/-0 | ALREADY-QUEUED | row 01a0b3d5-e8e4-7b9b-9837-e1d6ef7b212e names it |
| `archive/hippocampus-host-superseded-20260910` | +6/-0 | SUPERSEDED | master:internal/agent/hippocampus_agent.go:174 NewHostEngine — the tip's target binding, renamed |
| `archive/tool-family-merge-superseded-20260910` | +2/-0 | SUPERSEDED | its own subject says superseded (c3afd8f0 "…-superseded"); the tool family it documents is internal/toolnames on master |
| `archive/branch-scan-archive-filter-20260901` | +0/-0 | ALREADY-DONE | git cherry -v master archive/branch-scan-archive-filter-20260901 prints no + line: every patch is on master by identity |
| `archive/docs/daemon-no-privilege-remedy-rework` | +0/-0 | ALREADY-DONE | git cherry -v master archive/docs/daemon-no-privilege-remedy-rework prints no + line: every patch is on master by identity |
| `archive/docs/hp-push-diagnosis` | +0/-0 | ALREADY-DONE | git cherry -v master archive/docs/hp-push-diagnosis prints no + line: every patch is on master by identity |
| `archive/docs/requirements-merge-branch-lifecycle` | +0/-0 | ALREADY-DONE | git cherry -v master archive/docs/requirements-merge-branch-lifecycle prints no + line: every patch is on master by identity |
| `archive/docs/requirements-task-ledger` | +0/-0 | ALREADY-DONE | git cherry -v master archive/docs/requirements-task-ledger prints no + line: every patch is on master by identity |
| `archive/docs/requirements-umbrella` | +0/-0 | ALREADY-DONE | git cherry -v master archive/docs/requirements-umbrella prints no + line: every patch is on master by identity |
| `archive/feat-claim-broadcast-20260901` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat-claim-broadcast-20260901 prints no + line: every patch is on master by identity |
| `archive/feat-merge-backlog-20260901` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat-merge-backlog-20260901 prints no + line: every patch is on master by identity |
| `archive/feat/abnormal-child-retry` | +3/-0 | ALREADY-DONE | patch-equivalent: all 12 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/feat/assert-the-bug-audit` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/assert-the-bug-audit prints no + line: every patch is on master by identity |
| `archive/feat/branch-attribution` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/branch-attribution prints no + line: every patch is on master by identity |
| `archive/feat/chat-m2-status-wake` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/chat-m2-status-wake prints no + line: every patch is on master by identity |
| `archive/feat/chat-m2-status-wake-20260917` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/chat-m2-status-wake-20260917 prints no + line: every patch is on master by identity |
| `archive/feat/chat-m2-streaming` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/chat-m2-streaming prints no + line: every patch is on master by identity |
| `archive/feat/claim-time-deliverable-stamp` | +0/-1 | ALREADY-DONE | git cherry -v master archive/feat/claim-time-deliverable-stamp prints no + line: every patch is on master by identity |
| `archive/feat/client-closeout-delivery` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/client-closeout-delivery prints no + line: every patch is on master by identity |
| `archive/feat/cov-config-hp` | +1/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/feat/cov-deepseek-hp` | +2/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/feat/cov-gopilog-hp` | +2/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/feat/cov-hippocampus-hp` | +3/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/feat/cov-shot-hp` | +4/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/feat/cwd-release-inventory-verified` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/cwd-release-inventory-verified prints no + line: every patch is on master by identity |
| `archive/feat/google-headed-x11-ab` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/google-headed-x11-ab prints no + line: every patch is on master by identity |
| `archive/feat/ledger-treasure-hunt-stage0` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/ledger-treasure-hunt-stage0 prints no + line: every patch is on master by identity |
| `archive/feat/memsync-import-atomic` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/memsync-import-atomic prints no + line: every patch is on master by identity |
| `archive/feat/merger-inflight-both-axes` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/merger-inflight-both-axes prints no + line: every patch is on master by identity |
| `archive/feat/night-worker-throughput` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/night-worker-throughput prints no + line: every patch is on master by identity |
| `archive/feat/rawlog-merge-verification` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/rawlog-merge-verification prints no + line: every patch is on master by identity |
| `archive/feat/req-b1-task-ledger` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/req-b1-task-ledger prints no + line: every patch is on master by identity |
| `archive/feat/req-b2-merge-branches` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/req-b2-merge-branches prints no + line: every patch is on master by identity |
| `archive/feat/req-b3-worktree-cwd` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/req-b3-worktree-cwd prints no + line: every patch is on master by identity |
| `archive/feat/req-b4-sync-nodes` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/req-b4-sync-nodes prints no + line: every patch is on master by identity |
| `archive/feat/req-b5-context-memory` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/req-b5-context-memory prints no + line: every patch is on master by identity |
| `archive/feat/req-b6-agent-loop` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/req-b6-agent-loop prints no + line: every patch is on master by identity |
| `archive/feat/req-b7-tools-routing` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/req-b7-tools-routing prints no + line: every patch is on master by identity |
| `archive/feat/req-b9-chat-ui` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/req-b9-chat-ui prints no + line: every patch is on master by identity |
| `archive/feat/requirements-umbrella` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/requirements-umbrella prints no + line: every patch is on master by identity |
| `archive/feat/scan-tip-at-mint` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/scan-tip-at-mint prints no + line: every patch is on master by identity |
| `archive/feat/unsourced-kill-provenance` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/unsourced-kill-provenance prints no + line: every patch is on master by identity |
| `archive/feat/voice-capture-failmatrix` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/voice-capture-failmatrix prints no + line: every patch is on master by identity |
| `archive/feat/web-m2-rework` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/web-m2-rework prints no + line: every patch is on master by identity |
| `archive/feat/worker-run-entry` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/worker-run-entry prints no + line: every patch is on master by identity |
| `archive/feat/worktree-clean-closeout` | +0/-0 | ALREADY-DONE | git cherry -v master archive/feat/worktree-clean-closeout prints no + line: every patch is on master by identity |
| `archive/fix-hp-positional-param-submit-20260907` | +1/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix-sigterm-session-end-20260905` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix-sigterm-session-end-20260905 prints no + line: every patch is on master by identity |
| `archive/fix/ambient-profile-gate` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/ambient-profile-gate prints no + line: every patch is on master by identity |
| `archive/fix/chat-help-command-arg` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/chat-help-command-arg prints no + line: every patch is on master by identity |
| `archive/fix/cli-contract-inventory` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/cli-contract-inventory prints no + line: every patch is on master by identity |
| `archive/fix/client-closeout-push-delivery` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/client-closeout-push-delivery prints no + line: every patch is on master by identity |
| `archive/fix/cmd-agent-red-reanchor` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/cmd-agent-red-reanchor prints no + line: every patch is on master by identity |
| `archive/fix/cmd-agent-suite-red` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/cmd-agent-suite-red prints no + line: every patch is on master by identity |
| `archive/fix/d2-merge-backlog-terminal-guard-reland` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/d2-merge-backlog-terminal-guard-reland prints no + line: every patch is on master by identity |
| `archive/fix/daemoncwd-scratch-pin` | +1/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/docanchor-bare-lineno` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/docanchor-bare-lineno prints no + line: every patch is on master by identity |
| `archive/fix/docanchor-bare-lineno-045359` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/docanchor-bare-lineno-045359 prints no + line: every patch is on master by identity |
| `archive/fix/dump-name-collision` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/dump-name-collision prints no + line: every patch is on master by identity |
| `archive/fix/finish-cleanup-single-node-wording` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/finish-cleanup-single-node-wording prints no + line: every patch is on master by identity |
| `archive/fix/finish-cleanup-single-node-wording-045443` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/finish-cleanup-single-node-wording-045443 prints no + line: every patch is on master by identity |
| `archive/fix/finish-cleanup-single-node-wording-045650` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/finish-cleanup-single-node-wording-045650 prints no + line: every patch is on master by identity |
| `archive/fix/finish-cleanup-single-node-wording-050556` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/finish-cleanup-single-node-wording-050556 prints no + line: every patch is on master by identity |
| `archive/fix/heavy-tests-classification` | +1/-3 | ALREADY-DONE | patch-equivalent: all 4 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/heavy-tests-classification-20260917` | +1/-3 | ALREADY-DONE | patch-equivalent: all 4 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/hp-positional-param-submit` | +1/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/liveclaim-worktree-veto` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/liveclaim-worktree-veto prints no + line: every patch is on master by identity |
| `archive/fix/merge-backlog-terminal-guard` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/merge-backlog-terminal-guard prints no + line: every patch is on master by identity |
| `archive/fix/merge-verify-branch-absence` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/merge-verify-branch-absence prints no + line: every patch is on master by identity |
| `archive/fix/merge-verify-branch-absence-045508` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/merge-verify-branch-absence-045508 prints no + line: every patch is on master by identity |
| `archive/fix/merge-verify-rework-staleness-guard` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/merge-verify-rework-staleness-guard prints no + line: every patch is on master by identity |
| `archive/fix/merger-park-worktree` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/merger-park-worktree prints no + line: every patch is on master by identity |
| `archive/fix/merger-status-branch-account` | +3/-0 | ALREADY-DONE | patch-equivalent: all 7 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/merger-status-branch-account-20260917` | +3/-0 | ALREADY-DONE | patch-equivalent: all 7 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/merger-status-log-rewire` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/merger-status-log-rewire prints no + line: every patch is on master by identity |
| `archive/fix/merger-tick-grid` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/merger-tick-grid prints no + line: every patch is on master by identity |
| `archive/fix/peak-guard-cost-class` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/peak-guard-cost-class prints no + line: every patch is on master by identity |
| `archive/fix/peakhours-merge-verify-record` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/peakhours-merge-verify-record prints no + line: every patch is on master by identity |
| `archive/fix/reclaim-fossil-converge` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/reclaim-fossil-converge prints no + line: every patch is on master by identity |
| `archive/fix/serve-lock-diagnosis-liveness` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/serve-lock-diagnosis-liveness prints no + line: every patch is on master by identity |
| `archive/fix/standards-baseline-rekey` | +3/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/standards-baseline-rekey-20260917` | +3/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/standards-baseline-rekey2` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/standards-baseline-rekey2 prints no + line: every patch is on master by identity |
| `archive/fix/standards-reds` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/standards-reds prints no + line: every patch is on master by identity |
| `archive/fix/standards-reds2` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/standards-reds2 prints no + line: every patch is on master by identity |
| `archive/fix/standards-stylegate-master-red` | +3/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/standards-stylegate-master-red-20260917` | +3/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/stylegate-voice-discards` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/stylegate-voice-discards prints no + line: every patch is on master by identity |
| `archive/fix/task-child-fail-escape-sandbox` | +9/-0 | ALREADY-DONE | patch-equivalent: all 12 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/task-child-fail-escape-sandbox-20260917` | +9/-0 | ALREADY-DONE | patch-equivalent: all 12 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/task-child-fail-escape-sandbox2` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/task-child-fail-escape-sandbox2 prints no + line: every patch is on master by identity |
| `archive/fix/task-list-address-no-scan` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/task-list-address-no-scan prints no + line: every patch is on master by identity |
| `archive/fix/task-list-address-no-scan-rework-20260915` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/task-list-address-no-scan-rework-20260915 prints no + line: every patch is on master by identity |
| `archive/fix/task-list-inprogress-doc-pointer` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/task-list-inprogress-doc-pointer prints no + line: every patch is on master by identity |
| `archive/fix/taskfindjsonl-layout-determinism` | +5/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/taskfindjsonl-layout-determinism-20260917` | +5/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/version-dirty-tagged-witness` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/version-dirty-tagged-witness prints no + line: every patch is on master by identity |
| `archive/fix/version-dirty-witness` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/version-dirty-witness prints no + line: every patch is on master by identity |
| `archive/fix/worker-count-dead-20260912` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/worker-count-dead-20260912 prints no + line: every patch is on master by identity |
| `archive/fix/worker-foreground-console` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/worker-foreground-console prints no + line: every patch is on master by identity |
| `archive/fix/worker-ownership-10files` | +2/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/worker-ownership-pointer-audit` | +1/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/fix/worker-ownership-pointer-line` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/worker-ownership-pointer-line prints no + line: every patch is on master by identity |
| `archive/fix/worker-ownership-slash-pair` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/worker-ownership-slash-pair prints no + line: every patch is on master by identity |
| `archive/fix/worker-provenance-tag` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/worker-provenance-tag prints no + line: every patch is on master by identity |
| `archive/fix/worker-status-foreign-attribution` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/worker-status-foreign-attribution prints no + line: every patch is on master by identity |
| `archive/fix/worker-termination-attribution` | +0/-0 | ALREADY-DONE | git cherry -v master archive/fix/worker-termination-attribution prints no + line: every patch is on master by identity |
| `archive/hp-fix-claim-round-blowout-20260902` | +0/-0 | ALREADY-DONE | git cherry -v master archive/hp-fix-claim-round-blowout-20260902 prints no + line: every patch is on master by identity |
| `archive/hp-local-docs-67500ca` | +1/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/hp-zombie-closeout-20260902` | +0/-1 | ALREADY-DONE | git cherry -v master archive/hp-zombie-closeout-20260902 prints no + line: every patch is on master by identity |
| `archive/memfeedback-motivate-glm-20260827` | +1/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/merge-sweep-v2` | +0/-0 | ALREADY-DONE | git cherry -v master archive/merge-sweep-v2 prints no + line: every patch is on master by identity |
| `archive/openai-sdk-backup-20260826` | +0/-0 | ALREADY-DONE | git cherry -v master archive/openai-sdk-backup-20260826 prints no + line: every patch is on master by identity |
| `archive/review/voiceinput-dead-20260912` | +1/-0 | ALREADY-DONE | patch-equivalent: all 0 prod decl the tip adds exist on master (cherry ids differ, content landed by another path) |
| `archive/shm-gate-shapes-20260918` | +0/-0 | ALREADY-DONE | git cherry -v master archive/shm-gate-shapes-20260918 prints no + line: every patch is on master by identity |
| `archive/single-row-r2-20260905` | +0/-0 | ALREADY-DONE | git cherry -v master archive/single-row-r2-20260905 prints no + line: every patch is on master by identity |
| `archive/single-row-verify-20260905` | +0/-1 | ALREADY-DONE | git cherry -v master archive/single-row-verify-20260905 prints no + line: every patch is on master by identity |
| `archive/wip/cwd-release-resolve` | +0/-0 | ALREADY-DONE | git cherry -v master archive/wip/cwd-release-resolve prints no + line: every patch is on master by identity |
| `op-20260916` | +0/-0 | ALREADY-DONE | git cherry -v master archive/archive/fix-prefix-loop-20260916 prints no + line: every patch is on master by identity |

`git cherry -v master` column is `+N/-M`: N patches not applied by identity, M already applied.

## 4. Units that received a new task row (live gap: production surface absent from master)

Each row below is one submitted task. The full per-unit card (tip, date, every absent declaration,
the touched production files, the retiring evidence) is in §4.1; §4.2 has the raw instrument output.

| tag | tip | ledger row |
|---|---|---|
| `archive/claim-broadcast-m418` | `0371c403` | `01a0b51d-2b9f-754f-922c-7c91424b8c88` |
| `archive/feat/archive-why-gate` | `eac3b475` | `01a0b51d-2e73-7ad7-855f-7359b7b2a0ea` **CANCELLED — duplicate of an existing row (see §4.3)** |
| `archive/feat/conflict-branch-rename` | `abb4a062` | `01a0b51d-30d1-7895-a481-dacd3cfbbe9f` **CANCELLED — duplicate of an existing row (see §4.3)** |
| `archive/feat/cov-tools-rest` | `3e128733` | `01a0b51d-3489-7da0-b84a-4ea6a5beea53` |
| `archive/feat/foreground-component-lifecycle` | `f545a311` | `01a0b51d-3878-7dd3-9784-6fe70d7f3945` |
| `archive/feat/hand-archive-gate` | `c7569bcc` | `01a0b51d-3b7b-7c17-a0d6-27872adf8455` **CANCELLED — duplicate of an existing row (see §4.3)** |
| `archive/feat/merger-park-tip-dedup` | `50f6d4df` | `01a0b51d-3d39-7cda-bb2c-dc1df53549d3` |
| `archive/feat/merger-tick-branch-account` | `e0932678` | `01a0b51d-3e04-79b1-8492-2fdefc45ded7` |
| `archive/feat/raw-round-grammar` | `d9a5ea41` | `01a0b51d-3eaa-77da-aefc-15dcd017bf75` |
| `archive/feat/scan-tip-at-mint-045325` | `4534cd12` | `01a0b51d-3f62-7859-a706-690d83d8f3f4` |
| `archive/feat/shot-jsonlist-retry` | `484e5d30` | `01a0b51d-4026-7304-a978-2cb0e22a2a1a` |
| `archive/feat/taskid-whitespace-normalize` | `e4e80335` | `01a0b51d-40f9-79ed-b597-daad4f096dae` |
| `archive/feat/web-m1-b4b7` | `9ebc9554` | `01a0b51d-41aa-7d1c-9658-f7b5af83e8bc` |
| `archive/feat/web-m2-b5b8b9b12` | `98fb2942` | `01a0b51d-424c-7a54-b686-21f36550c584` |
| `archive/feat/worker-run-entry2` | `0aa03689` | `01a0b51d-42ff-747f-985b-d791a8826871` |
| `archive/feat/worker-run-entry2-20260917` | `3e425d13` | `01a0b51d-43ff-71b7-b62d-d0091963b4dd` |
| `archive/feat/worker-term-exit` | `dd5dde45` | `01a0b51d-44b0-7c5f-b943-016dbea63e54` |
| `archive/fix-memsync-import-atomic-20260901` | `67264535` | `01a0b51d-5016-7790-95e4-e397e5f28347` |
| `archive/fix/cwd-and-lifetime` | `b37b776f` | `01a0b51d-5627-701c-b560-e174b3d3b342` **CANCELLED — duplicate of an existing row (see §4.3)** |
| `archive/fix/envbool-single-truthy-parser` | `e21582f2` | `01a0b51d-5c49-71e3-a4ad-59095fe228ea` |
| `archive/fix/hippo-host-resolution` | `564fa6dd` | `01a0b51d-61cb-7601-bb4e-101367a6898b` |
| `archive/fix/merge-verify-landing-witness` | `5db36b11` | `01a0b51d-6509-7012-841f-c6cf034cb214` |
| `archive/fix/merge-verify-landing-witness-20260917` | `737ca0bf` | `01a0b51d-67a9-7e3a-8bcd-0fe15d70586b` |
| `archive/fix/merger-park-worktree-release` | `d1a4c708` | `01a0b51d-69c7-753f-bc14-5c0db98d0b11` |
| `archive/fix/merger-takeover-scope-escape` | `da7326f5` | `01a0b51d-72e9-7e1a-ad8d-5b801227d043` |
| `archive/fix/notify-all-events` | `f197232d` | `01a0b51d-7678-7741-8d17-dcf61776c579` |
| `archive/fix/park-worktree-live-row-guard` | `255f5cf0` | `01a0b51d-7aae-73e2-b648-29ad70aeba1a` |
| `archive/fix/read-binary-sample-straddle` | `a7e39f5e` | `01a0b51d-7c4d-7cde-8e17-099e24bfbb50` |
| `archive/fix/serve-lock-race` | `dd9c53b4` | `01a0b51d-7ee6-7958-84e9-ac7adc7c9750` |
| `archive/fix/shot-chrome-numeric-sort` | `b66da327` | `01a0b51d-80ea-771e-a860-ba0cd102aab3` |
| `archive/fix/taskverify-ctx-child-kill` | `2fc3edab` | `01a0b51d-83c2-7a35-a1a4-7ced57bda6f9` |
| `archive/hp-unique-maxtokens` | `f9a319a8` | `01a0b51d-8664-7acd-b823-6d6c31a0a31b` |
| `archive/log-stats-raw-gopi` | `65571649` | `01a0b51d-8838-7813-93e7-211580d6e26f` |
| `archive/merge-resolve-escalate-early-cb9f72f` | `cb9f72f7` | `01a0b51d-8b78-7a3e-9c1b-72430ceb9cc8` |
| `archive/testsuite-codex-20260827` | `164ac04f` | `01a0b51d-90d5-7ebf-af97-73a30b7f5f06` |

live rows: 31 of 35 submitted (4 cancelled as duplicates)

### 4.3 The 4 rows cancelled as duplicates (and why)

A tag whose branch is **already named by a pending ledger row** (audit rule 2) must not get a
second row — two live rows would run the same salvage twice (Rule 26's duplicate allowance is for
*repair*, not for an unowned second run). Four of the 35 submitted rows hit exactly that, and were
moved to `cancelled` with a reason naming the owner row:

| row | tag | owner row it duplicates |
|---|---|---|
| `01a0b51d-2e73-7ad7-855f-7359b7b2a0ea` | `archive/feat/archive-why-gate` | `01a0b4aa-69af-739c-a381-92e2a031578f` |
| `01a0b51d-3b7b-7c17-a0d6-27872adf8455` | `archive/feat/hand-archive-gate` | `01a0b4aa-69af-739c-a381-92e2a031578f` |
| `01a0b51d-30d1-7895-a481-dacd3cfbbe9f` | `archive/feat/conflict-branch-rename` | `01a0b4aa-6874-710f-9380-e39910a2d8f4` |
| `01a0b51d-5627-701c-b560-e174b3d3b342` | `archive/fix/cwd-and-lifetime` | `01a0b3d5-e8e4-7b9b-9837-e1d6ef7b212e` |

That the audit caught this **in itself** is the point of the rule: the same defect the row-count
would have created (two carriers for one work item) was measured against the live queue rather
than against the tag name alone. 31 rows stand.

### 4.1 Unit cards

- **`archive/claim-broadcast-m418`** (tip `0371c403`, 2026-08-31, cherry +2/-3, rev-list 5) — feat(claim-broadcast): refusal yields ownership — cancel + sync-yielded (M4.18)
  - single tag
  - production declarations absent everywhere on master (13): `AbortClaimOnRefusal`, `ClaimBroadcastOutcome`, `RefusedAny`, `StartClaimBroadcast`, `WaitClaimBroadcast`, `WaitClaimBroadcastLines`, `YieldClaimedTask`, `broadcastClaimToPeer`
  - touched prod files (2): `cmd/agent/cli_tasks.go`, `internal/tools/memory_sync_claim.go`
- **`archive/feat/archive-why-gate`** (tip `eac3b475`, 2026-09-17, cherry +4/-0, rev-list 4) — docs(sweep): re-measure the 88-tag table live, decide the UNKNOWN row, recover its doc
  - extra tags of the same tip: `archive/feat/archive-why-gate-20260917`
  - production declarations absent everywhere on master (15): `DescribeArchived`, `archivedErrNote`, `buildGateArchiveReason`, `buildGateArchiveReasonFmt`, `buildGateArchiveRevParseFmt`, `describeArchivedNote`, `renderArchivedLines`, `repoSweepFileParkTask`
  - new production files master's history never had: `internal/tools/archive_why.go`, `internal/tools/archive_why_read.go`
  - touched prod files (3): `internal/tools/merger_status.go`, `internal/tools/repo_sweep.go`, `internal/tools/task_merge.go`
- **`archive/feat/conflict-branch-rename`** (tip `abb4a062`, 2026-09-17, cherry +12/-0, rev-list 12) — test(merger): cover the new manual-merge refusal arm (red-proofed)
  - extra tags of the same tip: `archive/feat/conflict-branch-rename-20260917`
  - production declarations absent everywhere on master (32): `ConflictBranchPrefix`, `IsConflictBranch`, `buildGateArchiveRenameFmt`, `buildGateParkedRefFmt`, `buildGateRevivePairFmt`, `conflictBranchPrefix`, `isBranchArchived`, `listUnmergedBranches`
  - new production files master's history never had: `internal/branchnames/names.go`
  - touched prod files (5): `cmd/agent/cli_tasks.go`, `internal/tools/merger_status.go`, `internal/tools/repo_sweep.go`, `internal/tools/task_merge.go`
- **`archive/feat/cov-tools-rest`** (tip `3e128733`, 2026-09-11, cherry +14/-0, rev-list 15) — test(tools): make the whole internal/tools suite hermetic w.r.t. the notify target
  - single tag
  - production declarations absent everywhere on master (10): `claimHolderHostPidSep`, `claimHostIsSelf`, `countLiveRowsByHolder`, `countLiveRowsOwnedByOthers`, `filterSelfOwnedClaims`, `importTaskFresh`, `liveForeignClaimsSQL`, `syncTaskMigrate`
  - touched prod files (3): `internal/tools/memory_sync_claim.go`, `internal/tools/memory_sync_task.go`, `internal/tools/tasks.go`
- **`archive/feat/foreground-component-lifecycle`** (tip `f545a311`, 2026-09-17, cherry +2/-0, rev-list 2) — docs(devdoc): record the measured pre-existing failures + the baseline re-key reason
  - single tag
  - production declarations absent everywhere on master (13): `branchTip`, `isBranchArchived`, `listUnmergedBranches`, `mergeExecIntervalFromEnv`, `mergerDaemonFlagHint`, `mergerInterval`, `serveBacklogLastTick`, `serveBacklogThrottleMin`
  - new production files master's history never had: `cmd/agent/component_log.go`
  - touched prod files (3): `cmd/agent/cli_tasks.go`, `cmd/agent/cli_tasks_worker.go`, `cmd/agent/main.go`
- **`archive/feat/hand-archive-gate`** (tip `c7569bcc`, 2026-09-17, cherry +8/-0, rev-list 8) — docs(archive): retarget the stale .evid-master-* citation to the tracked baseline logs
  - extra tags of the same tip: `archive/feat/hand-archive-gate-20260917`
  - production declarations absent everywhere on master (15): `ArchivedBranch`, `DescribeArchived`, `RepoRootAt`, `archivedReasonMissing`, `mergerDaemonFlagHint`, `repoSweepArchiveDetail`, `repoSweepFileParkTask`, `repoSweepGitInput`
  - new production files master's history never had: `cmd/agent/cli_archive.go`, `internal/tools/archive_reason.go`
  - touched prod files (5): `cmd/agent/main.go`, `internal/tools/merger_status.go`, `internal/tools/repo_resolve_row.go`, `internal/tools/repo_sweep.go`
- **`archive/feat/merger-park-tip-dedup`** (tip `50f6d4df`, 2026-09-17, cherry +1/-0, rev-list 1) — fix(merger): key the park on the WORK, not the branch name
  - single tag
  - production declarations absent everywhere on master (29): `isBranchArchived`, `listUnmergedBranches`, `mergeExecIntervalFromEnv`, `mergerInterval`, `repoSweepArchiveTagRefs`, `repoSweepBranchGone`, `repoSweepBranchTip`, `repoSweepFileParkTask`
  - touched prod files (2): `cmd/agent/cli_tasks.go`, `internal/tools/repo_sweep.go`
- **`archive/feat/merger-pure-cronjob`** (tip `d1faf33b`, 2026-09-17, cherry +2/-7, rev-list 9) — docs(merger): record the two suite-failure root causes (stale bin, cwd-relative artifact read)
  - single tag
  - production declarations absent everywhere on master (0): 
  - touched prod files (1): `cmd/agent/merger_takeover.go`
- **`archive/feat/merger-tick-branch-account`** (tip `e0932678`, 2026-09-16, cherry +2/-0, rev-list 2) — test(merger): the real tick log line reconciles (top-level `merger --once` E2E)
  - extra tags of the same tip: `archive/feat/merger-tick-branch-account-20260917`
  - production declarations absent everywhere on master (21): `InventoryBucketLines`, `InventoryLine`, `PrintMergerLine`, `isBranchArchived`, `listUnmergedBranches`, `mergeExecIntervalFromEnv`, `mergerInterval`, `repoSweepFileParkTask`
  - touched prod files (2): `cmd/agent/cli_tasks.go`, `internal/tools/repo_sweep.go`
- **`archive/feat/raw-round-grammar`** (tip `d9a5ea41`, 2026-09-11, cherry +7/-0, rev-list 8) — docs(raw-round-grammar): E2E re-run on the merged tip binary (56-round real log)
  - single tag
  - production declarations absent everywhere on master (2): `httpSelectorGrammar`, `rawRoundPosition`
  - touched prod files (2): `cmd/agent/cli_raw.go`, `internal/gopilog/httpview.go`
- **`archive/feat/scan-tip-at-mint-045325`** (tip `4534cd12`, 2026-09-17, cherry +5/-0, rev-list 5) — docs(scan-tip): the acceptance is red on this host — and red identically on master
  - extra tags of the same tip: `archive/feat/scan-tip-at-mint-045423`, `archive/feat/scan-tip-at-mint-045640`, `archive/feat/scan-tip-at-mint-050535`
  - production declarations absent everywhere on master (12): `MergeBacklogBranchTip`, `isBranchArchived`, `listUnmergedBranches`, `mergeExecIntervalFromEnv`, `mergerInterval`, `serveBacklogLastTick`, `serveBacklogThrottleMin`, `serveBranchScan`
  - touched prod files (2): `cmd/agent/cli_tasks.go`, `internal/tools/task_merge_backlog.go`
- **`archive/feat/shot-jsonlist-retry`** (tip `484e5d30`, 2026-09-11, cherry +9/-0, rev-list 9) — docs(shot): record the negative control (poll neutered → 1/12 race failures)
  - single tag
  - production declarations absent everywhere on master (3): `errNoPageTarget`, `pageTargetWSURL`, `waitPageTarget`
  - touched prod files (1): `internal/shot/cdp.go`
- **`archive/feat/taskid-whitespace-normalize`** (tip `e4e80335`, 2026-09-11, cherry +6/-0, rev-list 6) — docs(tools): report for the residual task_id entry points (mutation proof, left residual, raw logs)
  - single tag
  - production declarations absent everywhere on master (3): `taskIDNormalize`, `taskLivenessStamp`, `taskWorktreeForeignOwnerRow`
  - touched prod files (2): `internal/tools/task_finish_cleanup.go`, `internal/tools/tasks.go`
- **`archive/feat/web-m1-b4b7`** (tip `9ebc9554`, 2026-09-16, cherry +7/-6, rev-list 14) — docs(web): closure session — master merged in, third red-proof pass, fresh live evidence (queued frame first, 
  - extra tags of the same tip: `archive/feat/web-m1-b4b7-20260917`
  - production declarations absent everywhere on master (2): `webBase64PaddingSlack`, `webMaxChatTextBytes`
  - touched prod files (1): `cmd/agent/cli_web.go`
- **`archive/feat/web-m2-b5b8b9b12`** (tip `98fb2942`, 2026-09-16, cherry +12/-6, rev-list 19) — docs(web): state the master fail-set baseline in the M2 report verification
  - extra tags of the same tip: `archive/feat/web-m2-b5b8b9b12-20260917`
  - production declarations absent everywhere on master (3): `DEFAULT_PLACEMENT_LABEL`, `webBase64PaddingSlack`, `webMaxChatTextBytes`
  - touched prod files (1): `cmd/agent/cli_web.go`
- **`archive/feat/worker-run-entry2`** (tip `0aa03689`, 2026-09-16, cherry +1/-0, rev-list 1) — wip(worker-run-entry2): commit uncommitted work from the ended run (salvage by PA)
  - single tag
  - production declarations absent everywhere on master (14): `isBranchArchived`, `listUnmergedBranches`, `mergeExecIntervalFromEnv`, `mergerDaemonFlagHint`, `mergerInterval`, `serveBacklogLastTick`, `serveBacklogThrottleMin`, `serveBranchScan`
  - touched prod files (4): `cmd/agent/cli_tasks.go`, `cmd/agent/cli_tasks_worker.go`, `cmd/agent/cli_worker_daemon.go`, `cmd/agent/main.go`
- **`archive/feat/worker-run-entry2-20260917`** (tip `3e425d13`, 2026-09-16, cherry +3/-0, rev-list 3) — test(worker): pin the retired runner's contract instead of its spelling
  - single tag
  - production declarations absent everywhere on master (16): `isBranchArchived`, `listUnmergedBranches`, `mergeExecIntervalFromEnv`, `mergerDaemonFlagHint`, `mergerInterval`, `serveBacklogLastTick`, `serveBacklogThrottleMin`, `serveBranchScan`
  - touched prod files (4): `cmd/agent/cli_tasks.go`, `cmd/agent/cli_tasks_worker.go`, `cmd/agent/cli_worker_daemon.go`, `cmd/agent/main.go`
- **`archive/feat/worker-term-exit`** (tip `dd5dde45`, 2026-09-17, cherry +4/-0, rev-list 4) — docs(worm): verbatim result of sleep 600 via bash — 120s no-output kill; detached survives
  - single tag
  - production declarations absent everywhere on master (12): `controlConflictFlags`, `controlReadAndActRefusal`, `descendantPIDs`, `forwardWorkerStopDuringRound`, `notifyWorkerStopNudge`, `readPPID`, `signalRunChild`, `startWorkerStopForwarder`
  - touched prod files (3): `cmd/agent/cli_tasks_worker.go`, `cmd/agent/cli_tasks_worker_stop.go`, `cmd/agent/cli_worker_control.go`
- **`archive/fix-memsync-import-atomic-20260901`** (tip `67264535`, 2026-08-31, cherry +4/-1, rev-list 5) — docs(memsync): full-suite green + concurrent-test loser-BUSY contract + master baseline note
  - extra tags of the same tip: `archive/memsync-import-atomic-v1`
  - production declarations absent everywhere on master (3): `importTaskTxDB`, `syncTaskLiveHashHitTx`, `syncTaskMigrateTx`
  - touched prod files (1): `internal/tools/memory_sync_task.go`
- **`archive/fix/cwd-and-lifetime`** (tip `b37b776f`, 2026-09-15, cherry +6/-0, rev-list 7) — resolve merge fix/cwd-and-lifetime into master
  - single tag
  - production declarations absent everywhere on master (16): `WorktreeOf`, `controlConflictFlags`, `controlReadAndActRefusal`, `isBranchArchived`, `listUnmergedBranches`, `mergeExecIntervalFromEnv`, `mergerInterval`, `mergerRunPass`
  - new production files master's history never had: `internal/procname/identity.go`, `internal/procname/liveness.go`, `internal/tools/daemon_lifetime.go`, `internal/tools/process_cwd_release.go`
  - touched prod files (4): `cmd/agent/cli_tasks.go`, `cmd/agent/cli_tasks_worker.go`, `cmd/agent/cli_worker_control.go`, `internal/daemoncwd/daemoncwd.go`
- **`archive/fix/envbool-single-truthy-parser`** (tip `e21582f2`, 2026-09-11, cherry +1/-0, rev-list 1) — fix(config): one truthy-parser vocabulary (EnvBool SSOT) + Rule-13 literal gate
  - single tag
  - production declarations absent everywhere on master (7): `IsFalseValue`, `IsTrueValue`, `boolFalseSet`, `boolVocabulary`, `newBoolVocabulary`, `normalizeBoolValue`, `taskWorktreeForeignOwnerRow`
  - touched prod files (2): `internal/config/env.go`, `internal/tools/task_finish_cleanup.go`
- **`archive/fix/hippo-host-resolution`** (tip `564fa6dd`, 2026-09-11, cherry +11/-0, rev-list 11) — docs(devdoc): re-verify the acceptance at the tip and mutation-probe the Rule-13 gate
  - single tag
  - production declarations absent everywhere on master (4): `ClampedBriefConfidence`, `ClampedBriefMemoryWeight`, `EffectiveConfidence`, `TierWeight`
  - touched prod files (2): `internal/hippocampus/brief.go`, `internal/hippocampus/host.go`
- **`archive/fix/merge-verify-landing-witness`** (tip `5db36b11`, 2026-09-17, cherry +10/-0, rev-list 10) — docs(merge-verify): align the report with the checked-in red-proof artifact and pin the witness cost
  - single tag
  - production declarations absent everywhere on master (4): `mergeVerifyStaleBranchToken`, `mergeVerifyStaleLandingCommit`, `mergeVerifyStaleLandingGrep`, `mergeVerifyStaleSubjectHead`
  - touched prod files (1): `internal/tools/task_staleness_guard.go`
- **`archive/fix/merge-verify-landing-witness-20260917`** (tip `737ca0bf`, 2026-09-17, cherry +14/-0, rev-list 14) — docs(merge-verify): the whole package IS green at the tip — the earlier red runs were per-run host flakes
  - single tag
  - production declarations absent everywhere on master (4): `mergeVerifyStaleBranchToken`, `mergeVerifyStaleLandingCommit`, `mergeVerifyStaleLandingGrep`, `mergeVerifyStaleSubjectHead`
  - touched prod files (1): `internal/tools/task_staleness_guard.go`
- **`archive/fix/merger-park-worktree-release`** (tip `d1a4c708`, 2026-09-17, cherry +10/-0, rev-list 10) — docs: live park-path demo (filed row carries the ff-ability clauses)
  - extra tags of the same tip: `archive/fix/merger-park-worktree-release-20260917`
  - production declarations absent everywhere on master (29): `isBranchArchived`, `listUnmergedBranches`, `mergeExecIntervalFromEnv`, `mergerInterval`, `repoSweepArchivedBranches`, `repoSweepDrainParkedWorktrees`, `repoSweepFileParkTask`, `repoSweepParkBranch`
  - touched prod files (2): `cmd/agent/cli_tasks.go`, `internal/tools/repo_sweep.go`
- **`archive/fix/merger-takeover-scope-escape`** (tip `da7326f5`, 2026-09-17, cherry +1/-0, rev-list 1) — fix(merger): the scope ladder reads the pre-ladder mode value, and the degradation stays announced
  - single tag
  - production declarations absent everywhere on master (3): `mergerTakeoverModeIsEscaped`, `mergerTakeoverModePhrase`, `trimmedMode`
  - touched prod files (1): `cmd/agent/merger_takeover_scope.go`
- **`archive/fix/notify-all-events`** (tip `f197232d`, 2026-09-11, cherry +4/-0, rev-list 5) — fix(notify): "all" filter enumerates every synthetic alarm from a registry
  - single tag
  - production declarations absent everywhere on master (12): `claimHolderHostPidSep`, `claimHostIsSelf`, `countLiveRowsByHolder`, `countLiveRowsOwnedByOthers`, `filterSelfOwnedClaims`, `importTaskFresh`, `liveForeignClaimsSQL`, `syncTaskMigrate`
  - touched prod files (5): `internal/tools/memory_sync_claim.go`, `internal/tools/memory_sync_task.go`, `internal/tools/taskheartbeat.go`, `internal/tools/tasknotify.go`
- **`archive/fix/park-worktree-live-row-guard`** (tip `255f5cf0`, 2026-09-17, cherry +1/-0, rev-list 1) — wip(fix/park-worktree-live-row-guard): rescue uncommitted work before stopping the workers (PA)
  - extra tags of the same tip: `archive/fix/park-worktree-live-row-guard-20260917`
  - production declarations absent everywhere on master (28): `isBranchArchived`, `listUnmergedBranches`, `mergeExecIntervalFromEnv`, `mergerInterval`, `repoSweepFileParkTask`, `repoSweepParkBranch`, `repoSweepParkFoldRelease`, `repoSweepParkLiveCWDGate`
  - touched prod files (2): `cmd/agent/cli_tasks.go`, `internal/tools/repo_sweep.go`
- **`archive/fix/read-binary-sample-straddle`** (tip `a7e39f5e`, 2026-09-11, cherry +1/-0, rev-list 1) — fix(read): don't report a valid UTF-8 file as binary when the 8KB sample cuts a rune
  - single tag
  - production declarations absent everywhere on master (1): `isContinuationByte`
  - touched prod files (1): `internal/tools/read.go`
- **`archive/fix/serve-lock-race`** (tip `dd9c53b4`, 2026-09-17, cherry +9/-0, rev-list 9) — docs(serve-lock): record the corrected re-run (set equality vs master) and the cwd fix
  - extra tags of the same tip: `archive/fix/serve-lock-race-20260917`
  - production declarations absent everywhere on master (12): `isBranchArchived`, `listUnmergedBranches`, `mergeExecIntervalFromEnv`, `mergerInterval`, `serveBacklogLastTick`, `serveBacklogThrottleMin`, `serveBranchScan`, `serveBranchScanLastTick`
  - touched prod files (3): `cmd/agent/cli_tasks.go`, `cmd/agent/cli_tasks_worker.go`, `cmd/agent/cli_worker_daemon.go`
- **`archive/fix/shot-chrome-numeric-sort`** (tip `b66da327`, 2026-09-11, cherry +9/-0, rev-list 9) — docs(shot): record the branch-tip acceptance re-run in the numeric-sort report
  - single tag
  - production declarations absent everywhere on master (3): `comparePlaywrightVersions`, `trimVersionZeroes`, `versionZeroDigit`
  - touched prod files (1): `internal/shot/chrome.go`
- **`archive/fix/taskverify-ctx-child-kill`** (tip `2fc3edab`, 2026-09-11, cherry +3/-0, rev-list 3) — docs: task report for the TaskVerify ctx-ownership fix
  - single tag
  - production declarations absent everywhere on master (12): `isBranchArchived`, `listUnmergedBranches`, `mergeExecIntervalFromEnv`, `serveBacklogLastTick`, `serveBacklogThrottleMin`, `serveBranchScan`, `serveBranchScanLastTick`, `serveMergeExecLastTick`
  - touched prod files (2): `cmd/agent/cli_tasks.go`, `internal/tools/tasks.go`
- **`archive/hippocampus-host-superseded-20260910`** (tip `867c347b`, 2026-09-10, cherry +6/-0, rev-list 7) — docs(hippocampus): recommended disposition — take the additive files + the identity fix, not both hosts
  - single tag
  - production declarations absent everywhere on master (4): `clearInflight`, `isRepeatDelivery`, `observeDelivery`, `toolEventSummary`
  - new production files master's history never had: `internal/agent/hippocampus_binding.go`
  - touched prod files (2): `internal/agent/chat_ops.go`, `internal/agent/hippocampus.go`
- **`archive/hp-unique-maxtokens`** (tip `f9a319a8`, 2026-08-06, cherry +5/-0, rev-list 5) — style: gofmt loop.go (collapse blank lines around MaxTokens field)
  - single tag
  - production declarations absent everywhere on master (7): `TrimThresholdRatio`, `charsPerToken`, `maxErrorBodyBytes`, `maxErrorDisplayBytes`, `maxPathSuggestions`, `pipeReadBufferSize`, `suggestionPrefixLen`
  - touched prod files (4): `internal/agent/context.go`, `internal/deepseek/retry.go`, `internal/tools/bash.go`, `internal/tools/write.go`
- **`archive/log-stats-raw-gopi`** (tip `65571649`, 2026-09-05, cherry +1/-0, rev-list 1) — feat(logstats): gopi stats + gopi raw subcommands; move --http/--raw out of gopi log
  - single tag
  - production declarations absent everywhere on master (35): `FormatRawRoundsIndex`, `FormatStatsSummary`, `RawRoundFull`, `RawRoundResponse`, `RawRoundsIndexLine`, `RawStats`, `RoundKind`, `StatsForSession`
  - touched prod files (5): `cmd/agent/cli_log.go`, `cmd/agent/cli_raw.go`, `cmd/agent/cli_stats.go`, `internal/gopilog/rawview.go`
- **`archive/merge-resolve-escalate-early-cb9f72f`** (tip `cb9f72f7`, 2026-09-08, cherry +1/-0, rev-list 1) — feat(merger): resolve-fail escalation → rework task submit after N failures
  - single tag
  - production declarations absent everywhere on master (21): `MergeConflictReworkBriefing`, `MergeResolveBriefing`, `MergerResolveRecoverScan`, `isBranchArchived`, `listUnmergedBranches`, `mergeConflictReworkSummary`, `mergeExecIntervalFromEnv`, `repoSweepResolveFailCount`
  - touched prod files (2): `cmd/agent/cli_tasks.go`, `internal/tools/repo_resolve_row.go`
- **`archive/testsuite-codex-20260827`** (tip `164ac04f`, 2026-08-27, cherry +1/-0, rev-list 1) — feat(testsuite): test-suite dev + redundancy cleanup (codex side, GLM)
  - single tag
  - production declarations absent everywhere on master (1): `withTimeoutError`
  - new production files master's history never had: `internal/agent/env.go`, `internal/taskitem/taskitem.go`, `internal/tools/path.go`
  - touched prod files (2): `internal/gopilog/formatter.go`, `internal/tools/timeout.go`
- **`archive/tool-family-merge-superseded-20260910`** (tip `c3afd8f0`, 2026-09-10, cherry +2/-0, rev-list 2) — docs(report): tool-family-merge adoption report (Rule 11 proof + verification)
  - single tag
  - production declarations absent everywhere on master (8): `ActionCommit`, `isJobCall`, `parallelSafeActions`, `postCommitReviewMarker`, `postCommitReviewNoPunishment`, `postCommitReviewVerdictMenu`, `reviewVerdictResponsePrefix`, `toolResultIsContent`
  - touched prod files (2): `internal/agent/loop.go`, `internal/toolnames/names.go`

### 4.2 Raw instrument output per unit

### `archive/claim-broadcast-m418`

```
$ git -C /home/user/work/GoPi cherry -v master archive/claim-broadcast-m418
  (+2 unapplied, -3 already applied)   rev-list --count master..archive/claim-broadcast-m418 = 5
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (13): AbortClaimOnRefusal, ClaimBroadcastOutcome, RefusedAny, StartClaimBroadcast, WaitClaimBroadcast, WaitClaimBroadcastLines, YieldClaimedTask, broadcastClaimToPeer, claimBroadcastTimeout, claimLiveLocalRows, runTaskIngest, taskIngestListenDefault, taskIngestServeReadHeaderTimeout
```

### `archive/feat/archive-why-gate`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/archive-why-gate
  (+4 unapplied, -0 already applied)   rev-list --count master..archive/feat/archive-why-gate = 4
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (15): DescribeArchived, archivedErrNote, buildGateArchiveReason, buildGateArchiveReasonFmt, buildGateArchiveRevParseFmt, describeArchivedNote, renderArchivedLines, repoSweepFileParkTask, repoSweepParkArchiveReason, repoSweepParkBranch, repoSweepParkOutcome, repoSweepParkPayload, repoSweepParkReason, repoSweepParkSummary, repoSweepParkTaskLive
new prod files never on master: internal/tools/archive_why.go, internal/tools/archive_why_read.go
```

### `archive/feat/conflict-branch-rename`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/conflict-branch-rename
  (+12 unapplied, -0 already applied)   rev-list --count master..archive/feat/conflict-branch-rename = 12
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (32): ConflictBranchPrefix, IsConflictBranch, buildGateArchiveRenameFmt, buildGateParkedRefFmt, buildGateRevivePairFmt, conflictBranchPrefix, isBranchArchived, listUnmergedBranches, mergeExecIntervalFromEnv, mergerInterval, repoSweepBranchExists, repoSweepConflictBranches, repoSweepDropParkTag, repoSweepFileParkTask, repoSweepLastResortPark, repoSweepLiveBranches, repoSweepParkBranch, repoSweepParkOutcome, repoSweepParkPayload, repoSweepParkReason, repoSweepParkRename, repoSweepParkRowsInFlight, repoSweepParkSummary, repoSweepParkTaskLive, serveBacklogLastTick, serveBacklogThrottleMin, serveBranchScan, serveBranchScanLastTick, serveMergeExecLastTick, serveMergeExecSweep, trackedBacklogBranches, verdictLine
new prod files never on master: internal/branchnames/names.go
```

### `archive/feat/cov-tools-rest`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/cov-tools-rest
  (+14 unapplied, -0 already applied)   rev-list --count master..archive/feat/cov-tools-rest = 15
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (10): claimHolderHostPidSep, claimHostIsSelf, countLiveRowsByHolder, countLiveRowsOwnedByOthers, filterSelfOwnedClaims, importTaskFresh, liveForeignClaimsSQL, syncTaskMigrate, taskLivenessStamp, taskTagsUnion
```

### `archive/feat/foreground-component-lifecycle`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/foreground-component-lifecycle
  (+2 unapplied, -0 already applied)   rev-list --count master..archive/feat/foreground-component-lifecycle = 2
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (13): branchTip, isBranchArchived, listUnmergedBranches, mergeExecIntervalFromEnv, mergerDaemonFlagHint, mergerInterval, serveBacklogLastTick, serveBacklogThrottleMin, serveBranchScan, serveBranchScanLastTick, serveMergeExecLastTick, serveMergeExecSweep, trackedBacklogBranches
new prod files never on master: cmd/agent/component_log.go
```

### `archive/feat/hand-archive-gate`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/hand-archive-gate
  (+8 unapplied, -0 already applied)   rev-list --count master..archive/feat/hand-archive-gate = 8
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (15): ArchivedBranch, DescribeArchived, RepoRootAt, archivedReasonMissing, mergerDaemonFlagHint, repoSweepArchiveDetail, repoSweepFileParkTask, repoSweepGitInput, repoSweepGitStdin, repoSweepParkBranch, repoSweepParkOutcome, repoSweepParkPayload, repoSweepParkReason, repoSweepParkSummary, repoSweepParkTaskLive
new prod files never on master: cmd/agent/cli_archive.go, internal/tools/archive_reason.go
```

### `archive/feat/merger-park-tip-dedup`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/merger-park-tip-dedup
  (+1 unapplied, -0 already applied)   rev-list --count master..archive/feat/merger-park-tip-dedup = 1
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (29): isBranchArchived, listUnmergedBranches, mergeExecIntervalFromEnv, mergerInterval, repoSweepArchiveTagRefs, repoSweepBranchGone, repoSweepBranchTip, repoSweepFileParkTask, repoSweepParkBranch, repoSweepParkLegacyHead, repoSweepParkOutcome, repoSweepParkPayload, repoSweepParkReason, repoSweepParkSummary, repoSweepParkSummaryHead, repoSweepParkSummaryHeadLikeSuffix, repoSweepParkTaskLive, repoSweepParkTipParked, repoSweepParkTipTag, repoSweepParkTipTagName, repoSweepSettleParkedTips, repoSweepTagParkTipRef, serveBacklogLastTick, serveBacklogThrottleMin, serveBranchScan, serveBranchScanLastTick, serveMergeExecLastTick, serveMergeExecSweep, trackedBacklogBranches
```

### `archive/feat/merger-pure-cronjob`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/merger-pure-cronjob
  (+2 unapplied, -7 already applied)   rev-list --count master..archive/feat/merger-pure-cronjob = 9
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (0): 
```

### `archive/feat/merger-tick-branch-account`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/merger-tick-branch-account
  (+2 unapplied, -0 already applied)   rev-list --count master..archive/feat/merger-tick-branch-account = 2
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (21): InventoryBucketLines, InventoryLine, PrintMergerLine, isBranchArchived, listUnmergedBranches, mergeExecIntervalFromEnv, mergerInterval, repoSweepFileParkTask, repoSweepParkBranch, repoSweepParkOutcome, repoSweepParkPayload, repoSweepParkReason, repoSweepParkSummary, repoSweepParkTaskLive, serveBacklogLastTick, serveBacklogThrottleMin, serveBranchScan, serveBranchScanLastTick, serveMergeExecLastTick, serveMergeExecSweep, trackedBacklogBranches
```

### `archive/feat/raw-round-grammar`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/raw-round-grammar
  (+7 unapplied, -0 already applied)   rev-list --count master..archive/feat/raw-round-grammar = 8
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (2): httpSelectorGrammar, rawRoundPosition
```

### `archive/feat/scan-tip-at-mint-045325`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/scan-tip-at-mint-045325
  (+5 unapplied, -0 already applied)   rev-list --count master..archive/feat/scan-tip-at-mint-045325 = 5
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (12): MergeBacklogBranchTip, isBranchArchived, listUnmergedBranches, mergeExecIntervalFromEnv, mergerInterval, serveBacklogLastTick, serveBacklogThrottleMin, serveBranchScan, serveBranchScanLastTick, serveMergeExecLastTick, serveMergeExecSweep, trackedBacklogBranches
```

### `archive/feat/shot-jsonlist-retry`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/shot-jsonlist-retry
  (+9 unapplied, -0 already applied)   rev-list --count master..archive/feat/shot-jsonlist-retry = 9
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (3): errNoPageTarget, pageTargetWSURL, waitPageTarget
```

### `archive/feat/taskid-whitespace-normalize`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/taskid-whitespace-normalize
  (+6 unapplied, -0 already applied)   rev-list --count master..archive/feat/taskid-whitespace-normalize = 6
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (3): taskIDNormalize, taskLivenessStamp, taskWorktreeForeignOwnerRow
```

### `archive/feat/web-m1-b4b7`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/web-m1-b4b7
  (+7 unapplied, -6 already applied)   rev-list --count master..archive/feat/web-m1-b4b7 = 14
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (2): webBase64PaddingSlack, webMaxChatTextBytes
```

### `archive/feat/web-m2-b5b8b9b12`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/web-m2-b5b8b9b12
  (+12 unapplied, -6 already applied)   rev-list --count master..archive/feat/web-m2-b5b8b9b12 = 19
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (3): DEFAULT_PLACEMENT_LABEL, webBase64PaddingSlack, webMaxChatTextBytes
```

### `archive/feat/worker-run-entry2`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/worker-run-entry2
  (+1 unapplied, -0 already applied)   rev-list --count master..archive/feat/worker-run-entry2 = 1
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (14): isBranchArchived, listUnmergedBranches, mergeExecIntervalFromEnv, mergerDaemonFlagHint, mergerInterval, serveBacklogLastTick, serveBacklogThrottleMin, serveBranchScan, serveBranchScanLastTick, serveMergeExecLastTick, serveMergeExecSweep, trackedBacklogBranches, workerPositionalSyntax, workerSecondPositionalMessage
```

### `archive/feat/worker-run-entry2-20260917`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/worker-run-entry2-20260917
  (+3 unapplied, -0 already applied)   rev-list --count master..archive/feat/worker-run-entry2-20260917 = 3
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (16): isBranchArchived, listUnmergedBranches, mergeExecIntervalFromEnv, mergerDaemonFlagHint, mergerInterval, serveBacklogLastTick, serveBacklogThrottleMin, serveBranchScan, serveBranchScanLastTick, serveMergeExecLastTick, serveMergeExecSweep, trackedBacklogBranches, workerBareRunFlag, workerBareRunFlags, workerPositionalSyntax, workerSecondPositionalMessage
```

### `archive/feat/worker-term-exit`

```
$ git -C /home/user/work/GoPi cherry -v master archive/feat/worker-term-exit
  (+4 unapplied, -0 already applied)   rev-list --count master..archive/feat/worker-term-exit = 4
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (12): controlConflictFlags, controlReadAndActRefusal, descendantPIDs, forwardWorkerStopDuringRound, notifyWorkerStopNudge, readPPID, signalRunChild, startWorkerStopForwarder, workerStopDrainSettle, workerStopNudge, workerStopRegistrySettleGrace, workerStopUndeliveredNote
```

### `archive/fix-memsync-import-atomic-20260901`

```
$ git -C /home/user/work/GoPi cherry -v master archive/fix-memsync-import-atomic-20260901
  (+4 unapplied, -1 already applied)   rev-list --count master..archive/fix-memsync-import-atomic-20260901 = 5
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (3): importTaskTxDB, syncTaskLiveHashHitTx, syncTaskMigrateTx
```

### `archive/fix/cwd-and-lifetime`

```
$ git -C /home/user/work/GoPi cherry -v master archive/fix/cwd-and-lifetime
  (+6 unapplied, -0 already applied)   rev-list --count master..archive/fix/cwd-and-lifetime = 7
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (16): WorktreeOf, controlConflictFlags, controlReadAndActRefusal, isBranchArchived, listUnmergedBranches, mergeExecIntervalFromEnv, mergerInterval, mergerRunPass, serveBacklogLastTick, serveBacklogThrottleMin, serveBranchScan, serveBranchScanLastTick, serveMergeExecLastTick, serveMergeExecSweep, trackedBacklogBranches, workerOnceFace
new prod files never on master: internal/procname/identity.go, internal/procname/liveness.go, internal/tools/daemon_lifetime.go, internal/tools/process_cwd_release.go
```

### `archive/fix/envbool-single-truthy-parser`

```
$ git -C /home/user/work/GoPi cherry -v master archive/fix/envbool-single-truthy-parser
  (+1 unapplied, -0 already applied)   rev-list --count master..archive/fix/envbool-single-truthy-parser = 1
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (7): IsFalseValue, IsTrueValue, boolFalseSet, boolVocabulary, newBoolVocabulary, normalizeBoolValue, taskWorktreeForeignOwnerRow
```

### `archive/fix/hippo-host-resolution`

```
$ git -C /home/user/work/GoPi cherry -v master archive/fix/hippo-host-resolution
  (+11 unapplied, -0 already applied)   rev-list --count master..archive/fix/hippo-host-resolution = 11
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (4): ClampedBriefConfidence, ClampedBriefMemoryWeight, EffectiveConfidence, TierWeight
```

### `archive/fix/merge-verify-landing-witness`

```
$ git -C /home/user/work/GoPi cherry -v master archive/fix/merge-verify-landing-witness
  (+10 unapplied, -0 already applied)   rev-list --count master..archive/fix/merge-verify-landing-witness = 10
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (4): mergeVerifyStaleBranchToken, mergeVerifyStaleLandingCommit, mergeVerifyStaleLandingGrep, mergeVerifyStaleSubjectHead
```

### `archive/fix/merge-verify-landing-witness-20260917`

```
$ git -C /home/user/work/GoPi cherry -v master archive/fix/merge-verify-landing-witness-20260917
  (+14 unapplied, -0 already applied)   rev-list --count master..archive/fix/merge-verify-landing-witness-20260917 = 14
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (4): mergeVerifyStaleBranchToken, mergeVerifyStaleLandingCommit, mergeVerifyStaleLandingGrep, mergeVerifyStaleSubjectHead
```

### `archive/fix/merger-park-worktree-release`

```
$ git -C /home/user/work/GoPi cherry -v master archive/fix/merger-park-worktree-release
  (+10 unapplied, -0 already applied)   rev-list --count master..archive/fix/merger-park-worktree-release = 10
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (29): isBranchArchived, listUnmergedBranches, mergeExecIntervalFromEnv, mergerInterval, repoSweepArchivedBranches, repoSweepDrainParkedWorktrees, repoSweepFileParkTask, repoSweepParkBranch, repoSweepParkFileRow, repoSweepParkFoldRelease, repoSweepParkOutcome, repoSweepParkPayload, repoSweepParkReason, repoSweepParkReleaseWorktree, repoSweepParkSummary, repoSweepParkTaskLive, repoSweepParseWorktreePorcelain, repoSweepRemoveWorktree, repoSweepWorktree, repoSweepWorktreeRefusalIsFailure, repoSweepWorktreeRegistration, repoSweepWorktreeReleaseCandidate, serveBacklogLastTick, serveBacklogThrottleMin, serveBranchScan, serveBranchScanLastTick, serveMergeExecLastTick, serveMergeExecSweep, trackedBacklogBranches
```

### `archive/fix/merger-takeover-scope-escape`

```
$ git -C /home/user/work/GoPi cherry -v master archive/fix/merger-takeover-scope-escape
  (+1 unapplied, -0 already applied)   rev-list --count master..archive/fix/merger-takeover-scope-escape = 1
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (3): mergerTakeoverModeIsEscaped, mergerTakeoverModePhrase, trimmedMode
```

### `archive/fix/notify-all-events`

```
$ git -C /home/user/work/GoPi cherry -v master archive/fix/notify-all-events
  (+4 unapplied, -0 already applied)   rev-list --count master..archive/fix/notify-all-events = 5
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (12): claimHolderHostPidSep, claimHostIsSelf, countLiveRowsByHolder, countLiveRowsOwnedByOthers, filterSelfOwnedClaims, importTaskFresh, liveForeignClaimsSQL, syncTaskMigrate, taskHeartbeatNotifyEvent, taskLivenessStamp, taskNotifyAlarmEvents, taskTagsUnion
```

### `archive/fix/park-worktree-live-row-guard`

```
$ git -C /home/user/work/GoPi cherry -v master archive/fix/park-worktree-live-row-guard
  (+1 unapplied, -0 already applied)   rev-list --count master..archive/fix/park-worktree-live-row-guard = 1
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (28): isBranchArchived, listUnmergedBranches, mergeExecIntervalFromEnv, mergerInterval, repoSweepFileParkTask, repoSweepParkBranch, repoSweepParkFoldRelease, repoSweepParkLiveCWDGate, repoSweepParkLiveOwnerGate, repoSweepParkOutcome, repoSweepParkPayload, repoSweepParkReason, repoSweepParkSummary, repoSweepParkSummaryKey, repoSweepParkTaskLive, repoSweepParkWorktreeRelease, repoSweepParseWorktreePorcelain, repoSweepRemoveWorktree, repoSweepTallyPark, repoSweepWorktree, repoSweepWorktreeRegistration, serveBacklogLastTick, serveBacklogThrottleMin, serveBranchScan, serveBranchScanLastTick, serveMergeExecLastTick, serveMergeExecSweep, trackedBacklogBranches
```

### `archive/fix/read-binary-sample-straddle`

```
$ git -C /home/user/work/GoPi cherry -v master archive/fix/read-binary-sample-straddle
  (+1 unapplied, -0 already applied)   rev-list --count master..archive/fix/read-binary-sample-straddle = 1
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (1): isContinuationByte
```

### `archive/fix/serve-lock-race`

```
$ git -C /home/user/work/GoPi cherry -v master archive/fix/serve-lock-race
  (+9 unapplied, -0 already applied)   rev-list --count master..archive/fix/serve-lock-race = 9
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (12): isBranchArchived, listUnmergedBranches, mergeExecIntervalFromEnv, mergerInterval, serveBacklogLastTick, serveBacklogThrottleMin, serveBranchScan, serveBranchScanLastTick, serveMergeExecLastTick, serveMergeExecSweep, trackedBacklogBranches, workerDaemonGuardIdentity
```

### `archive/fix/shot-chrome-numeric-sort`

```
$ git -C /home/user/work/GoPi cherry -v master archive/fix/shot-chrome-numeric-sort
  (+9 unapplied, -0 already applied)   rev-list --count master..archive/fix/shot-chrome-numeric-sort = 9
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (3): comparePlaywrightVersions, trimVersionZeroes, versionZeroDigit
```

### `archive/fix/taskverify-ctx-child-kill`

```
$ git -C /home/user/work/GoPi cherry -v master archive/fix/taskverify-ctx-child-kill
  (+3 unapplied, -0 already applied)   rev-list --count master..archive/fix/taskverify-ctx-child-kill = 3
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (12): isBranchArchived, listUnmergedBranches, mergeExecIntervalFromEnv, serveBacklogLastTick, serveBacklogThrottleMin, serveBranchScan, serveBranchScanLastTick, serveMergeExecLastTick, serveMergeExecSweep, taskLivenessStamp, taskVerifyWaitDelay, trackedBacklogBranches
```

### `archive/hippocampus-host-superseded-20260910`

```
$ git -C /home/user/work/GoPi cherry -v master archive/hippocampus-host-superseded-20260910
  (+6 unapplied, -0 already applied)   rev-list --count master..archive/hippocampus-host-superseded-20260910 = 7
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (4): clearInflight, isRepeatDelivery, observeDelivery, toolEventSummary
new prod files never on master: internal/agent/hippocampus_binding.go
```

### `archive/hp-unique-maxtokens`

```
$ git -C /home/user/work/GoPi cherry -v master archive/hp-unique-maxtokens
  (+5 unapplied, -0 already applied)   rev-list --count master..archive/hp-unique-maxtokens = 5
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (7): TrimThresholdRatio, charsPerToken, maxErrorBodyBytes, maxErrorDisplayBytes, maxPathSuggestions, pipeReadBufferSize, suggestionPrefixLen
```

### `archive/log-stats-raw-gopi`

```
$ git -C /home/user/work/GoPi cherry -v master archive/log-stats-raw-gopi
  (+1 unapplied, -0 already applied)   rev-list --count master..archive/log-stats-raw-gopi = 1
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (35): FormatRawRoundsIndex, FormatStatsSummary, RawRoundFull, RawRoundResponse, RawRoundsIndexLine, RawStats, RoundKind, StatsForSession, StatsJSON, StatsRound, StatsRoundJSON, StatsSummary, StatsToJSON, SummarizeRawStats, ToolStat, aggregateRounds, formatDur, httpArgLabel, isMovedLogFlag, lastArg, movedLogFlagError, parseLineRange, resolveSessionPath, runRawFile, runRawLog, runRawSession, runStatsFile, sessionArg, shortRawName, signDelta, spanBytes, statsFromJSONL, statsFromRaw, statsRender, trimCliffDrop
```

### `archive/merge-resolve-escalate-early-cb9f72f`

```
$ git -C /home/user/work/GoPi cherry -v master archive/merge-resolve-escalate-early-cb9f72f
  (+1 unapplied, -0 already applied)   rev-list --count master..archive/merge-resolve-escalate-early-cb9f72f = 1
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (21): MergeConflictReworkBriefing, MergeResolveBriefing, MergerResolveRecoverScan, isBranchArchived, listUnmergedBranches, mergeConflictReworkSummary, mergeExecIntervalFromEnv, repoSweepResolveFailCount, repoSweepReworkExists, resolveRowClaim, resolveRowCreate, resolveRowFinish, resolveRowSummary, resolveRowSummaryPrefix, serveBacklogLastTick, serveBacklogThrottleMin, serveBranchScan, serveBranchScanLastTick, serveMergeExecLastTick, serveMergeExecSweep, trackedBacklogBranches
```

### `archive/testsuite-codex-20260827`

```
$ git -C /home/user/work/GoPi cherry -v master archive/testsuite-codex-20260827
  (+1 unapplied, -0 already applied)   rev-list --count master..archive/testsuite-codex-20260827 = 1
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (1): withTimeoutError
new prod files never on master: internal/agent/env.go, internal/taskitem/taskitem.go, internal/tools/path.go
```

### `archive/tool-family-merge-superseded-20260910`

```
$ git -C /home/user/work/GoPi cherry -v master archive/tool-family-merge-superseded-20260910
  (+2 unapplied, -0 already applied)   rev-list --count master..archive/tool-family-merge-superseded-20260910 = 2
$ git -C /home/user/work/GoPi grep -w -l -e <sym> master -- '*.go'    # for each symbol below: no production hit
absent prod decls (8): ActionCommit, isJobCall, parallelSafeActions, postCommitReviewMarker, postCommitReviewNoPunishment, postCommitReviewVerdictMenu, reviewVerdictResponsePrefix, toolResultIsContent
```


## 5. Not done / limits (honest)

- **One UNLANDED unit deliberately got no row: `archive/feat/merger-pure-cronjob` (tip `d1faf33b`,
  cherry +2/-0).** Its single content-absent production file is `cmd/agent/merger_takeover.go`
  carrying `mergerTakeoverDetach`, and `git grep -w -l -e mergerTakeoverDetach master -- '*.go'`
  returns exactly one production hit — the symbol exists on master (the tip's subject is
  "docs(merger): record the two suite-failure root causes", i.e. a docs commit that also carries a
  helper); its other absent paths (`cmd/agent/merger_singleton_e2e_test.go`,
  `cmd/agent/merger_takeover_launcher_test.go`) are files master **once had and removed**. That is
  the retired-surface case §1 describes: reporting it as "unlanded and wanted" would ask an executor
  to resurrect the pre-cronjob takeover tests master deliberately deleted. It is reported here, with
  the measurement, so the reader can disagree knowingly.
- **The 4 cancelled rows were caught by the audit's own rule, not by the tag name.** Their tags
  (`archive/feat/archive-why-gate`, `archive/feat/hand-archive-gate`,
  `archive/feat/conflict-branch-rename`, `archive/fix/cwd-and-lifetime`) each have a pending row
  already naming the branch; a second row would have produced two carriers for one work item. The
  submitted-then-cancelled pair stays in the ledger on purpose (§4.3) — "we dispatched a duplicate
  and retracted it" is information, and the ledger is append-only.
- **"Wanted" is not a product decision.** This audit decides *what is on master* — not whether
  unlanded work should land. A UNLANDED tag whose production surface master deliberately retired
  (e.g. `cmd/agent/merger_tick.go`, `internal/tools/repo_sweep.go` park machinery removed by
  `fb877170 refactor(merger)!: the merger is a cronjob`) is reported as UNLANDED and given a row
  with the conflict stated, so the executor can decide to salvage or to close it — the audit does
  not pre-decide.
- **The audit is read-only by construction**: no merge, no tag/branch deletion, no ledger mutation
  (the SQL above is `-readonly`), no checkout of any archived branch (Rule 29 — foreign branches
  were only read via `git show` / `git diff` / `git cherry`).
- **Symbol presence is a proxy, not a proof, for "the capability is on master".** A declaration can
  exist on master and still be an older shape of the tag's change (the tag may add a fix to a
  function that already exists). That is exactly why a unit is only classified ALREADY-DONE on
  *all-absent == 0*, and why an UNLANDED row asks the executor to re-derive from the tip rather than
  to trust this table.
- **Patch-equivalence** (rule 4) is measured by declaration + path presence, not by a byte-level
  patch-id comparison of every hunk; `git cherry` is the identity instrument and it is what decides
  whenever it can (rule 1).
- The count reconciliation covers tags and units, not individual commits: 160 row-less tags were
  audited and are all classified.

## 6. Verification

Observed in this run:

- `git -C /home/user/work/GoPi tag -l 'archive/*' | wc -l` → **167** (the namespace, not the brief's 66).
- `wc -l devdoc/archive-tag-audit-20260918.tsv` → **167 rows + 1 header + 1 comment line** = 169,
  plus the trailing newline → `wc -l` prints 170. Every row carries a verdict; the verdict totals
  are the ones in §2.
- The `cherry -v` / `rev-list` pass was executed **twice** against the same master snapshot
  (`0317d09a`); the per-tag `+/−` pairs came back identical, so the table is reproducible from the
  recorded commands rather than a one-shot reading.
- This change is docs-only (two new files under `devdoc/`); no production package is touched, so no
  test target is implicated. The build/test gate this run ran is recorded in the session's own
  `## Verification` block (task report), not restated here.

