<!-- doc-class: historical -->
# Night Progress 2026-09-04 (nick's autonomous-work window) — live log

## Completed (verified)
1. **memory-aware T1 implemented + INDEPENDENTLY VERIFIED (request-level)**
   - write_task_area tool + [WORKING MEMORY LAYOUT — soft] system prompt section — master 675b507
   - Independent acceptor (t-1788524560, 15m24s): E1-E5 ALL PASS with raw-log quotes —
     memLayout in request stream (baseline had 0 markers); 9 ordered write_task_area calls with conclusions;
     8 trim events; per-request message count bounded 3-9 across 19 rounds (analysis dropped, conclusions carried 1→9);
     tool-call completions 9 vs 0 prose; 5/5 defects found; tests green.
   - Decisive observation: LLM fabricated item-1 (committed before reading the file) → later read caught it →
     re-committed CORRECTION records (append-only) + reconciliation item vs ground truth — design's self-correction works.
   - Baseline tag for rollback: pre-memory-aware-20260904 (both nodes).

2. **A/B #1: run_cmd --settings fix (same task, same DS model, feature vs baseline)**
   - Exp (peerbox, +feature) t-1788526647: 1281s / 11.88M tokens / 153 rounds — DEEP fix (BootstrapEnvFromArgs: settings never reached child — root cause) — merged 8510649+75bb579
   - Ctrl (HP, baseline acbf958) t-1788527319: 890s / 2.50M tokens / 66 rounds — surface fix (runMethodFor echoes os.Args) — archived ab-control-runcmd-ae39ded
   - Honest attribution: confounders (independent Bonds; fix-depth difference) + SHORT-task mismatch (feature has no scenario on single-bug fixes). 4.76x tokens NOT attributable to the feature. Recorded devdoc/ab-test-runcmd-20260904.md.

3. **DO Serverless Inference — 协议层 OK / agent 运行不可用（2026-09-05 实测推翻——见 night-delivery）**：agent 循环几分钟内 429 rate-limit 必死（t-1788551878 263.7s/12iter FAIL 3 重试全败）；cache 0%（153K 全 miss）——仅单次短文本调用可用

## In progress
4. **A/B #2: auto-task run_cmd (t-1788526902 / HP clone t-1788528730)** — same design as A/B #1 (peerbox feature build cbe6e921f2 vs HP baseline bin acbf958; same DS model). Both Bonds running. Metric: tokens/time/quality.
5. **HP night GLM-free queue** — 6 NIGHT-TEST suite tasks (tools/agent/cmd/small/build/E2E) + canceled the redundant write_task_area mirror (already implemented on peerbox). Auto-starter armed: 4 GLM workers launch at 23:00 (proc_c4db9dfecdb9, sleep 5400).

## Queue state (peerbox): empty pending (4554 duplicate verify canceled — 4560 covered it)
## Master: 75bb579 (+ab report doc) — bin cbe6e921f2 rebuilt
## Tags: pre-memory-aware-20260904 (rollback point, both nodes)

## A/B #2 update (21:50)
- Exp (peerbox, feature, t-1788526902): DONE 19m37s (1177s) — cache 97.9% (6.43M/6.57M) — write_task_area called 4x, tasklist 5/5 subtasks tool-committed — fix 324b54b (auto mirror inherits wrapper run_cmd/run_method via maybeRegisterSessionTask; verified live t-1788529742 run_method=child).
- Ctrl (HP, baseline, t-1788528730): first run STUCK (repeated thinking block, CPU ~0 — killed); row reset to pending; clean rerun in flight.

## A/B #2 complete (22:15)
- Exp (peerbox feature) t-1788526902: 1176s / 6.65M tok / 149 iters / cache 97.9% — write_task_area called 4x, tasklist 5/5 subtasks tool-committed (feature genuinely adopted on a real dev task; carry-forward did NOT break prefix cache) — fix 324b54b merged.
- Ctrl (HP baseline) t-1788528730 rerun: 748s / 2.14M tok / 51 iters — contaminated: canonical fix merged by exp node first, ctrl Bond turned trace+verify (stashed its own parallel fix, ff-aligned HP, live-verified). Not a full independent implementation.
- Pattern: feature-node tokens 3.1x in both A/Bs — dominated by confounders (Bond depth, ctrl contamination), NOT the feature. Feature is genuinely used and cache-friendly. Honest verdict: short-bug A/Bs cannot isolate feature value; long-chain A/B still needed for the real question.

## A/B #3 (23:30) — long-chain audit, both nodes, CONTROLLED CONTAMINATED
- Exp (peerbox): 30m7s / 26.7M tok / 357 iters — found D1 P2 (retry.go Retry-After dead branch — 429 dogpile; introduced 5c6f5ca) + verified prior #1237 fixes; honest, no padding.
- Ctrl (HP): 57m31s / 42.7M tok / 551 iters — found 4 DEEP sync defects (same root: task-id-collision content protection inconsistent — import protected / harvest partial (strictly-newer terminal path lacks gate) / claim none): harvest terminal-overwrites-live-different-content (silent loss); M4.21 claim verb-switch regression (PushTerminalTaskSync refused every retry); claim acquire zero content gate (hijacks divergent pending); harvest terminal-over-PENDING fabricates completion. Each with FAILING repro test. Commit 637c4f3 (audit-ab3-baseline).
- CONTAMINATION: HP bin was aligned+rebuilt (22:10, A/B #2 close-out) and CONTAINS the feature — ctrl used write_task_area 6x. So A/B #3 = both-nodes-have-feature audit comparison, NOT feature-vs-none. Bond-depth dominates, as in A/B #1/2.
- Overall A/B verdict: feature's independent value cannot be isolated by these runs (ctrl contamination + Bond variance). REAL harvest: the audits surfaced genuine defects (retry Retry-After dead branch; harvest/claim content-protection family) — filed as follow-up fixes.

## HP night CI results (00:10) — NO real regressions
- NIGHT-TEST tools suite (79 files): PASS ✅ (384s+)
- NIGHT-TEST cmd/agent (52 files): PASS ✅
- NIGHT-TEST full build+vet gate: PASS ✅
- NIGHT-TEST E2E smoke (m418-yield + m420-rank): PASS ✅
- NIGHT-TEST small packages: TEST RESULTS ALL GREEN (config/deepseek/checkpoint/gopilog/proxy/shot FAIL=0) — task marked failed due to EXECUTION issue: Bond fell into a context_trim no-op loop (repeatedly trimming a non-existent call id — wasted rounds until timeout). Test conclusion valid; no real regression.
- Master health: full night CI = NO real regressions across all packages + E2E on the feature build.

## Audit-defect fixes (both DONE, verified, merged)
- t-1788535171 retry Retry-After dead branch (A/B #3 peerbox D1): FIXED 11m35s — sentinelAPIError.retryAfter + retryDelay=max(backoff,retryAfter capped); timed E2E proves honor (429+Retry-After:2 @1ms → ~2s vs ~1ms pre-fix); merged master (2ec33fc).
- t-1788535184 sync content-protection unify (A/B #3 HP D1-4, one root cause): FIXED 40m54s — sameWorkIdentity content gate on all 3 write paths (harvest/claim/import); refuse + ghost-insert; 4 audit repros FAIL→PASS; cache 98.9%; merged master (62d0f45).
