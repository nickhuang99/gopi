<!-- doc-class: historical -->
# Night Delivery Report — 2026-09-04/05 (nick's autonomous-work window)

Full live log: devdoc/night-progress-20260904.md (committed progressively). Master: ee7a3c9. Both queues empty. HP workers cleaned up.

## 1. Memory-aware experiment — IMPLEMENTED + VERIFIED (request-level evidence) ✅
- **T1 implemented** (master 675b507): write_task_area tool (session_conclusions append-only table) + [WORKING MEMORY LAYOUT — soft] system prompt section + commit-then-trim (epic + commit round + live tail kept, orphan-safe).
- **Independent verification (t-1788524560, 15m24s — a DIFFERENT agent from the implementer, evidence = raw log/--request only):**
  - E1: memLayout section present in the actual request stream (baseline gopi_system_prompt.txt had 0 markers — diff proves it landed).
  - E2: 9 ordered write_task_area calls with full conclusion payloads (quoted from raw log).
  - E3: 8 trim events; per-request message count bounded 3-9 across 19 rounds (analysis rounds dropped, conclusions carried 1→9 as ONE authoritative user message) — COMPRESSION OBSERVABLE.
  - E4: 9 tool-call completions, 0 prose-only "all done".
  - E5: 5/5 defects found (correct line numbers vs ground truth); tests green.
  - **Decisive observation — the design works**: the LLM fabricated item-1 (committed from prior knowledge before reading the file), later caught the discrepancy, and used the loop to self-correct: re-committed 3/4/5 as CORRECTION (supersedes) records + added a RECONCILIATION item against `nl -ba` ground truth. Append-only, authoritative, correctable — exactly the intended semantics.
- **Rollback point**: tag `pre-memory-aware-20260904` (both nodes).
- **Answer to nick's core question**: YES — the LLM cooperates with tasklist/analysis/conclusion memory compression — proven by request-level evidence, not self-report.

## 2. A/B series — honest verdict (feature vs baseline on real tasks)
| Run | Exp (peerbox, feature) | Ctrl (HP) | Verdict |
|-----|---------------------|-----------|---------|
| A/B #1 run_cmd fix | 21m26s / 11.9M tok / deep fix (BootstrapEnvFromArgs — settings never reached child) — merged | 14m50s / 2.5M / surface fix (archived) | Feature tokens 4.76x BUT confounders (Bond depth) dominate; short task gives feature no scenario. |
| A/B #2 auto run_cmd | 19m37s / 6.65M / cache 97.9% / **write_task_area genuinely used 4x** + tasklist 5/5 tool-committed — merged (324b54b) | 12m29s / 2.14M / contaminated (canonical merged first → ctrl turned trace+verify) | Feature is genuinely adopted on real dev work AND cache-friendly (carry-forward didn't break prefix cache). Token delta = confounders. |
| A/B #3 long-chain audit | 30m7s / 26.7M / 357 iters / found 1 P2 real defect (retry Retry-After dead branch) | 57m31s / 42.7M / 551 iters / found **4 deep sync defects** (content-protection family) | **Ctrl contaminated** (HP bin aligned+rebuilt in A/B #2 close-out — feature present; used write_task_area 6x). Both-nodes-have-feature audit. Bond-depth dominates. |

**Honest overall verdict**: feature's independent value CANNOT be isolated by these runs (control contamination + Bond variance + short-task mismatch). The audits' REAL harvest = genuine defects surfaced (see §4). Long-chain A/B with strict control isolation is still the open question — but the mechanism is proven working (E1-E5) and cost-neutral on cache.

## 3. DO Serverless Inference — 协议层支持但 AGENT 运行不可用（结论已推翻，2026-09-05 实测）
- **协议层验证**（2026-09-04）：inference.do-ai.run /v1/messages anthropic 兼容（tool_use round-trip / tool_result 多轮 / 流式事件全通）。Bearer auth。deepseek-v4-flash-0731。settings ~/.gopi/.do_settings.json（messages_path=/v1/messages 必须）。
- **❌ agent 运行层实测失败**（2026-09-05，t-1788551878 doc-review 用 DO 跑）：12 iterations/11 tool_calls 后 263.7s 死于 **429 rate limit exceeded（source=digitalocean），3 次重试全败 → task FAILED**。DO serverless 限流极激进，agent 循环每轮发请求几分钟内必死。全程 cache 0%（153574 tokens 全 miss——无前缀缓存，"便宜"假设对长上下文 agent 任务不成立）。
- **模型行为差**：12 iterations 中 5 次 memory_search + 路径幻觉绕圈（找不到真实存在的文件路径），read 仅 1 次。
- **正确用途**：单次短请求低频文本调用；**不可作 agent provider**（nick 判断正确：太便宜必有代价——专门定制的限流服务）。
- 测试方法教训：单次 smoke + 单任务 ≠ agent 能力验证——必须跑真实多轮工具循环任务到结束。
- ~~⚠️ 429 "queue at capacity" blips (serverless capacity — retry tolerates).~~ ← 错：429 不是偶发 blip，是 agent 循环几分钟内必死的激进限流（2026-09-05 实测推翻）
- ~~Strategic: night batch + post-GLM-free-period (9.20) continuity.~~ ← 取消：DO 不作任何 provider 用途（仅单次短文本调用）
- nick already ran a task on DO (feels faster than GLM) — 短任务通过不代表 agent 可用（限流在长循环才触发）

## 4. Audit-defect fixes (A/B #3 harvest) — BOTH DONE, VERIFIED, MERGED ✅
- **t-1788535171** retry Retry-After dead branch (5c6f5ca regression — 429 dogpile): FIXED 11m35s. sentinelAPIError.retryAfter + shared retryDelay=max(backoff, retryAfter capped). Timed E2E proves: 429+Retry-After:2 @1ms backoff → ~2s (was ~1ms). Regression tests 3/3 + full suite green. Merged (2ec33fc).
- **t-1788535184** sync content-protection unify (4 defects, one root cause: task-id-collision content protection inconsistent across harvest/claim/import): FIXED 40m54s. sameWorkIdentity content gate on ALL 3 write paths; refuse-on-mismatch + ghost-insert for peer's divergent terminal content; 4 audit repros FAIL→PASS; cache 98.9%. Merged (62d0f45).

## 5. HP night CI (GLM free window) — NO real regressions ✅
- tools suite (79 files) PASS / cmd/agent (52) PASS / build+vet gate PASS / E2E smoke PASS.
- small packages: test results ALL GREEN (config/deepseek/checkpoint/gopilog/proxy/shot FAIL=0) — task marked failed only due to an execution-layer artifact (Bond fell into a context_trim no-op loop after tests passed). No real regression.
- Master health on the feature build: all packages + E2E green.

## 6. State & next steps
- Master ee7a3c9; both node queues empty; baseline tag pre-memory-aware-20260904 in place for rollback comparison.
- Open question (for nick): how to run a CLEAN long-chain A/B (strict control isolation) to quantify feature value — or accept mechanism-verified + cost-neutral and proceed to T2 (framework-side hardening: cross-run resume re-injection of conclusions — documented known-not-done in the T1 devdoc).
- Remaining minor: context_trim no-op loop (execution artifact seen in night CI small-pkg run) — candidate small fix (stuck detection for same-arg trim calls).
