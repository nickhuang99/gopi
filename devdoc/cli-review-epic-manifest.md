<!-- doc-class: historical -->
# CLI Code-Review Epic — Task Manifest

Date: 2026-09-02 · Planner: epic coordinator (planning-only role)
Goal: systematic code review of every external CLI surface item in `gopi --help`,
before accumulated feature/bugfix churn pushes the source past the edge of collapse.

## Method (each child task follows it)

1. Run the real `gopi --help` / subcommand `-h` — enumerate every item.
2. Read the item's source (cmd/agent/cli_*.go + engine in internal/), its
   implementation, its usage, and its related docs (GOPI.md, devdoc/).
3. Walk git history / git_blame for design reversals, accumulated patches,
   dead flags, undocumented quirks.
4. Cross-check task/bug/development history (devdoc bugNNN reports, tasks/).
5. Issue per-item findings + prioritized suggestions (P0/P1/P2).
6. Deliver one devdoc/review-cli-*.md per task, committed on its own branch.

## Child-task inventory (all submitted via task_submit, pending in ledger)

| # | t-task_id | CLI surface | report file |
|---|-----------|-------------|-------------|
| 1 | t-1788305178 | config/options layer (--settings/--home/env table/flag knobs/input resolution/@payload) | devdoc/review-cli-config-options.md |
| 2 | t-1788305179 | memory_* family (search/store/get/update/delete/feedback/feedback_list) | devdoc/review-cli-memory.md |
| 3 | t-1788305196 | task dispatcher core (list/show/submit/run/serve/update/verify/reopen/archive/search) | devdoc/review-cli-task-core.md |
| 4 | t-1788305197 | task node-sync family (harvest/diff/sync/push + worker loop) | devdoc/review-cli-task-sync.md |
| 5 | t-1788305210 | task merge family (merge/merge-execute/adopt/audit-merges + safe-merge.sh) | devdoc/review-cli-task-merge.md |
| 6 | t-1788305211 | log subcommand (all views) | devdoc/review-cli-log.md |
| 7 | t-1788305224 | resume + checkpoint family | devdoc/review-cli-resume-checkpoint.md |
| 8 | t-1788305225 | chat + web (browser UI) + shot | devdoc/review-cli-chat-web.md |
| 9 | t-1788305238 | brew + memsync | devdoc/review-cli-brew-memsync.md |
| 10 | t-1788305239 | proxy + version | devdoc/review-cli-proxy-version.md |
| 11 | t-1788305253 | web_search + web_fetch + code-search | devdoc/review-cli-search.md |
| 12 | t-1788305254 | vision_analyze + native image input + vision model switching | devdoc/review-cli-vision.md |
| 13 | t-1788305267 | text2img | devdoc/review-cli-text2img.md |
| 14 | t-1788305268 | shot (dedicated task — see also #8 overlap note) | devdoc/review-cli-shot.md |

## Coverage notes

- Every top-level subcommand registered in cmd/agent/main.go's dispatch chain is
  covered: version, worker, memory, web_search, web_fetch, vision, text2img,
  code-search, log, resume, checkpoint, proxy, task, chat, web, brew, memsync, shot.
- The `gopi task` family was split into three tasks (core / node-sync / merge)
  because it is the largest surface (~20 verbs) and the epic's stated hotspot
  (dispatcher P2, verify-loop, merge-backlog, M4.13/M4.20 sync layers).
- The top-level option/environment/flag catalog (config.FormatUsage auto-built
  from internal/config) is task #1 — the single most drift-prone surface.
- Overlap note: task #8 (chat+web+shot) and task #14 (shot) both touch
  cli_shot.go. Task #14 is the dedicated shot review; #8 reviews shot only as
  part of the interactive/UI surface (vision-loop integration). Cross-check
  both reports when consolidating.

## Cross-cutting review lenses (every task applies these)

- Rule 1 (no magic numbers) / Rule 2 (single source of truth for deadlines/knobs).
- Tool-vs-CLI drift: LLM tool description (internal/tools loop.go) vs CLI flag
  surface — one engine, two faces; report divergence.
- --db / --dir / --repo plumbing consistency across subcommands.
- Design reversal detection via git_blame (repeatedly-rewritten lines = bug-prone
  hot spots; e.g. scanTaskRow column-count, child-process flag leakage).
- Docs drift: help text vs actual behavior vs GOPI.md vs devdoc.

## Consolidation (after all 14 land)

- Merge each review branch into master after PA verification (Rule 9:
  safe-merge.sh guard before merge).
- One consolidated epic report: devdoc/cli-review-epic-summary.md — aggregate
  P0/P1/P2 lists, cross-cutting patterns (e.g. repeated column-scan drift),
  and a ranked remediation backlog.
