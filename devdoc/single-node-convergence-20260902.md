<!-- doc-class: historical -->
# Single-Node Convergence — Decision Record (2026-09-02)

## Decision (nick, 2026-09-02, after data review)
Converge to **peerbox single-node as the primary execution position**. HP/RPi5 retreat to MANUAL parallel positions (manual submit + manual worker + manual sync — NO automatic protocol). The four-point plan was confirmed verbatim by nick:

1. **peerbox 单节点为主执行位**；HP/RPi5 退为手动并行位（手动 submit + 手动 worker + 手动 sync，不做自动协议）
2. **暂停自动件**：serve tick 的 dispatch/adoption/git-sync/merge-backlog sweep + claim-lock 广播——代码全保留不删
3. **保留最小件**：claim-lock 本地 CAS（防本地双跑）+ post-receive hook（入队）+ 手动 task sync --peer/memsync 命令（已存在，分钟级操作）
4. **恢复条件**：核心稳定后 sync 作为可选增量重新评估——届时复杂度可接受（锦上添花而非生死线）

**First priority: single-node closed loop** — auto-commit → auto-merge → automation running end-to-end on peerbox BEFORE any cross-node work.

## Why (the data that drove it)
Cross-node sync consumed 8/30-9/2's entire dev mainline with protocol accidents:
- HP IP change (DHCP lease expiry) → total link break
- claim-lock self-deadlock ×2 (sync_peers containing self / mirror rows with empty owner)
- HP binary staleness ×2 (protocol side version mismatch)
- anchor blind-spot (single-side rows never sync) + diff json-tag false-positive mass report
- ingest race, serve silent death ×3, HP 17+3 zombie rows
- db-sync design churn: export-batches deemed useless → timestamp-delta → rank model → lock model → worker — four rewrites in four days
- broken-pipe ×2 (worker -h side effect, itself a product of protocol churn)

The problems the protocol was meant to solve (double-run / merge conflict from parallel nodes) DO NOT EXIST in single-node mode.

## What changed today (execution)
- settings.json compute_nodes → [] (serve degrades to pure-local: claims pending, runs locally, no ssh dispatch). Backup: settings.json.bak-singlenode-20260902.
- serve restarted in local mode (`gopi task serve --every 5m --skip-tag pa-queue`, background).
- HP side: 3 zombie in_progress rows cancelled; HP-local tasks submitted (1 debug zombie-closeout + 3 review2 GLM second-opinion re-reviews); HP worker started manually (setsid, GLM) — manual parallel mode in action.
- New blocker discovered and filed: stuck-detection (bug055 toolStreak) false-positives on job_output polling — killed 3 tasks in one morning (t-1788306421/7556/7557) and took the serve daemon down with them (serve os.Exit(1) on failed run — already filed as t-1788308476). Fix filed as t-1788309830.

## Known dependencies for the closed loop
1. t-1788309830 (stuck polling exemption) — tasks keep dying without it.
2. t-1788308476 (serve os.Exit(1) on failed run) — serve keeps dying without it.
3. After those: verify loop = serve claims → Bond runs → commits → branch → adoption sweep merges to master → next task. Machine-verified merge (merge-base --is-ancestor), no LLM self-report.

## Posture going forward
- PA operates: submit tasks, read reports, review/verify, restart serve when it dies (until 8476 lands).
- HP: manual only. Submit there deliberately (like the review2 batch), worker on demand, task sync/memsync by hand when assets need to flow.
- No new cross-node protocol work. Re-evaluate only after the single-node loop is stable and the review backlog is closed.
