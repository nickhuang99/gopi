<!-- doc-class: historical -->
# Oh My Pi（can1357/oh-my-pi@9b302ef）

<!-- meta -->
repo: can1357/oh-my-pi
sha: 9b302ef
sha_full: 9b302ef6385ca87587f29a29837b4d511da4fe00
default_branch: main
guide: oh-my-pi.md
url_in_guide: https://github.com/can1357/oh-my-pi
license: MIT
open_source: 开源
pinned_sha_verified_at: 2026-11-05T16:10:00Z (api.github.com/repos/can1357/oh-my-pi/commits/9b302ef6385ca87587f29a29837b4d511da4fe00)
source_note: guide 内的 GitHub URL 与本仓库一致
evidence_dir: ~/.gopi/sessions/01a1aa6c-9222-7d72-8a42-ba369dd96ae7/artifacts/stage1a/oh-my-pi/
probe_source: devdoc/competitor-analysis-framework.md §3（17 维 doc 探针正则，唯一真源）
channels: E1 = dsa-docs guide 全文；E2 = 该 sha 下的仓库文件与全树路径。对方不运行（Rule 27/35），E3 结构性不可用，故上限 partial

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | none | E1+E2 | — | guide 全文 0 命中 + 全树 10283 blob 路径 0 命中 + 抽样内容 0 命中 —— 看过、没有（该读数的适用范围是 guide 语料与该 sha 的文件路径，不等于运行时一定不具备） |
| D02 | partial | E1+E2 | dsa-docs/oh-my-pi.md:11 + can1357/oh-my-pi@9b302ef:.cargo/config.toml:1 | E1 原文「`Get an API key from the [DeepSeek Platform](https://platform.deepseek.com/api_keys):`」；E2 源码 原文「`# Cargo environment for the workspace.`」。对方只允许 E1+E2，上限 partial |
| D03 | partial | E1+E2 | dsa-docs/oh-my-pi.md:11 + can1357/oh-my-pi@9b302ef:docs/adding-a-provider.md:30 | E1 原文「`Get an API key from the [DeepSeek Platform](https://platform.deepseek.com/api_keys):`」；E2 文档(非实现) 原文「`For an API-key gateway, start with catalog and auth KDL files; add a discovery`」。对方只允许 E1+E2，上限 partial |
| D04 | partial | E1+E2 | dsa-docs/oh-my-pi.md:19 + can1357/oh-my-pi@9b302ef:docs/config-usage.md:32 | E1 原文「`Create `~/.omp/agent/models.yml`:`」；E2 文档(非实现) 原文「`│ 1) ~/.omp/agent, ~/.claude, ...       │`」。对方只允许 E1+E2，上限 partial |
| D05 | partial | E2 | can1357/oh-my-pi@9b302ef:docs/cli-reference.md:30 | guide 无该维话题；E2 文档(非实现) 原文「`Per-subcommand flags (for example `omp auth-broker --json`) are documented by`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D06 | partial | E2 | can1357/oh-my-pi@9b302ef:docs/session.md:603 | guide 无该维话题；E2 文档(非实现) 原文「`The sessions phase also removes transcript rewrite temps (`.<name>.jsonl.<snowflake>.tmp`, and the same patter`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D07 | partial | E2 | can1357/oh-my-pi@9b302ef:tsconfig.tools.json:7 | guide 无该维话题；E2 源码 原文「`"allowImportingTsExtensions": true`」。E2 单独出现，上限 partial |
| D08 | partial | E2 | can1357/oh-my-pi@9b302ef:docs/approval-mode.md:1 | guide 无该维话题；E2 文档(非实现) 原文「`# Tool approval mode`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D09 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 50 个（`.dockerignore`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/oh-my-pi.md ; https://api.github.com/repos/can1357/oh-my-pi/git/trees/9b302ef6385ca87587f29a29837b4d511da4fe00?recursive=1 ; https://cdn.jsdelivr.net/gh/can1357/oh-my-pi@9b302ef6385ca87587f29a29837b4d511da4fe00/<path> ; https://raw.githubusercontent.com/can1357/oh-my-pi/9b302ef6385ca87587f29a29837b4d511da4fe00/<path> |
| D10 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 157 个（`assets/hero.png`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/oh-my-pi.md ; https://api.github.com/repos/can1357/oh-my-pi/git/trees/9b302ef6385ca87587f29a29837b4d511da4fe00?recursive=1 ; https://cdn.jsdelivr.net/gh/can1357/oh-my-pi@9b302ef6385ca87587f29a29837b4d511da4fe00/<path> ; https://raw.githubusercontent.com/can1357/oh-my-pi/9b302ef6385ca87587f29a29837b4d511da4fe00/<path> |
| D11 | partial | E2 | can1357/oh-my-pi@9b302ef:docs/tools/browser.md:1 | guide 无该维话题；E2 文档(非实现) 原文「`# Browser Eval prelude`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D12 | partial | E1 | dsa-docs/oh-my-pi.md:105 | 仅有 guide 声明「`\| `requiresReasoningContentForToolCalls: true` \| DeepSeek requires `reasoning_content` to be preserved acros`」，全树 72 路径命中但抽样未取到内容 |
| D13 | partial | E1 | dsa-docs/oh-my-pi.md:105 | 仅有 guide 声明「`\| `requiresReasoningContentForToolCalls: true` \| DeepSeek requires `reasoning_content` to be preserved acros`」，全树 145 路径命中但抽样未取到内容 |
| D14 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 17 个（`packages/ai/test/models-cost.test.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/oh-my-pi.md ; https://api.github.com/repos/can1357/oh-my-pi/git/trees/9b302ef6385ca87587f29a29837b4d511da4fe00?recursive=1 ; https://cdn.jsdelivr.net/gh/can1357/oh-my-pi@9b302ef6385ca87587f29a29837b4d511da4fe00/<path> ; https://raw.githubusercontent.com/can1357/oh-my-pi/9b302ef6385ca87587f29a29837b4d511da4fe00/<path> |
| D15 | partial | E2 | can1357/oh-my-pi@9b302ef:docs/lsp-config.md:1 | guide 无该维话题；E2 文档(非实现) 原文「`# LSP configuration in OMP`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D16 | partial | E2 | can1357/oh-my-pi@9b302ef:docs/memory.md:1 | guide 无该维话题；E2 文档(非实现) 原文「`# Autonomous Memory`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D17 | partial | E1+E2 | dsa-docs/oh-my-pi.md:19 + can1357/oh-my-pi@9b302ef:AGENTS.md:1 | E1 原文「`Create `~/.omp/agent/models.yml`:`」；E2 文档(非实现) 原文「`Moved Permanently. Redirecting to https://raw.githubusercontent.com/can1357/oh-my-pi/9b302ef6385ca87587f29a298`」。对方只允许 E1+E2，上限 partial |
