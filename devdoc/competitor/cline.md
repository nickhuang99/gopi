<!-- doc-class: historical -->
# Cline（cline/cline@d3ecc48）

<!-- meta -->
repo: cline/cline
sha: d3ecc48
sha_full: d3ecc4890e8f9bcb77182b96c77fa90575e8e145
default_branch: main
guide: cline.md
url_in_guide: absent
license: Apache-2.0
open_source: 开源
pinned_sha_verified_at: 2026-11-05T16:10:00Z (api.github.com/repos/cline/cline/commits/d3ecc4890e8f9bcb77182b96c77fa90575e8e145)
evidence_dir: ~/.gopi/sessions/01a1aa6c-9222-7d72-8a42-ba369dd96ae7/artifacts/stage1a/cline/
probe_source: devdoc/competitor-analysis-framework.md §3（17 维 doc 探针正则，唯一真源）
channels: E1 = dsa-docs guide 全文；E2 = 该 sha 下的仓库文件与全树路径。对方不运行（Rule 27/35），E3 结构性不可用，故上限 partial

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | none | E1+E2 | — | guide 全文 0 命中 + 全树 4458 blob 路径 0 命中 + 抽样内容 0 命中 —— 看过、没有（该读数的适用范围是 guide 语料与该 sha 的文件路径，不等于运行时一定不具备） |
| D02 | partial | E1+E2 | dsa-docs/cline.md:23 + cline/cline@d3ecc48:vitest.config.ts:3 | E1 原文「`#### 3. Choose API Key Mode`」；E2 源码 原文「`export default defineConfig({`」。对方只允许 E1+E2，上限 partial |
| D03 | partial | E1+E2 | dsa-docs/cline.md:23 + cline/cline@d3ecc48:evals/tsconfig.json:4 | E1 原文「`#### 3. Choose API Key Mode`」；E2 源码 原文「`"baseUrl": ".."`」。对方只允许 E1+E2，上限 partial |
| D04 | partial | E2 | cline/cline@d3ecc48:evals/tsconfig.json:2 | guide 无该维话题；E2 源码 原文「`"extends": "../tsconfig.json",`」。E2 单独出现，上限 partial |
| D05 | partial | E2 | cline/cline@d3ecc48:docs/cline-overview.mdx:20 | guide 无该维话题；E2 文档(非实现) 原文「`Run Cline in your terminal with interactive chat or fully headless automation for CI/CD and scripting.`」。E2 单独出现，上限 partial |
| D06 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 2 个（`apps/cli/src/utils/resume.test.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/cline.md ; https://api.github.com/repos/cline/cline/git/trees/d3ecc4890e8f9bcb77182b96c77fa90575e8e145?recursive=1 ; https://cdn.jsdelivr.net/gh/cline/cline@d3ecc4890e8f9bcb77182b96c77fa90575e8e145/<path> ; https://raw.githubusercontent.com/cline/cline/d3ecc4890e8f9bcb77182b96c77fa90575e8e145/<path> |
| D07 | partial | E1+E2 | dsa-docs/cline.md:5 + cline/cline@d3ecc48:.vscode/extensions.json:3 | E1 原文「`Cline is an AI coding assistant that runs as a VS Code extension, supporting multiple API providers and models`」；E2 源码 原文「`// for the documentation about the extensions.json format`」。对方只允许 E1+E2，上限 partial |
| D08 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 34 个（`apps/cli/src/acp/auto-approve.test.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/cline.md ; https://api.github.com/repos/cline/cline/git/trees/d3ecc4890e8f9bcb77182b96c77fa90575e8e145?recursive=1 ; https://cdn.jsdelivr.net/gh/cline/cline@d3ecc4890e8f9bcb77182b96c77fa90575e8e145/<path> ; https://raw.githubusercontent.com/cline/cline/d3ecc4890e8f9bcb77182b96c77fa90575e8e145/<path> |
| D09 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 38 个（`.agents/skills/opentui/references/components/containers.md`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/cline.md ; https://api.github.com/repos/cline/cline/git/trees/d3ecc4890e8f9bcb77182b96c77fa90575e8e145?recursive=1 ; https://cdn.jsdelivr.net/gh/cline/cline@d3ecc4890e8f9bcb77182b96c77fa90575e8e145/<path> ; https://raw.githubusercontent.com/cline/cline/d3ecc4890e8f9bcb77182b96c77fa90575e8e145/<path> |
| D10 | partial | E1 | dsa-docs/cline.md:15 | 仅有 guide 声明「`<img src="./assets/cline_step_1.png" width="250" border="1" />`」，全树 191 路径命中但抽样未取到内容 |
| D11 | partial | E1 | dsa-docs/cline.md:10 | 仅有 guide 声明「`- Click the **Extensions** icon in the activity bar (or press `Ctrl+Shift+X`).`」，全树 38 路径命中但抽样未取到内容 |
| D12 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 118 个（`apps/cline-hub/src/server.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/cline.md ; https://api.github.com/repos/cline/cline/git/trees/d3ecc4890e8f9bcb77182b96c77fa90575e8e145?recursive=1 ; https://cdn.jsdelivr.net/gh/cline/cline@d3ecc4890e8f9bcb77182b96c77fa90575e8e145/<path> ; https://raw.githubusercontent.com/cline/cline/d3ecc4890e8f9bcb77182b96c77fa90575e8e145/<path> |
| D13 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 62 个（`apps/cli/src/commands/history-command.test.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/cline.md ; https://api.github.com/repos/cline/cline/git/trees/d3ecc4890e8f9bcb77182b96c77fa90575e8e145?recursive=1 ; https://cdn.jsdelivr.net/gh/cline/cline@d3ecc4890e8f9bcb77182b96c77fa90575e8e145/<path> ; https://raw.githubusercontent.com/cline/cline/d3ecc4890e8f9bcb77182b96c77fa90575e8e145/<path> |
| D14 | partial | E2 | cline/cline@d3ecc48:docs/usage/acp.mdx:99 | guide 无该维话题；E2 文档(非实现) 原文「`- **Organization switching** — Cline team accounts can switch between personal and organization billing.`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D15 | partial | E2 | cline/cline@d3ecc48:.greptile/rules.md:4 | guide 无该维话题；E2 文档(非实现) 原文「`document explains **why**, so Greptile has the context to avoid false positives.`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D16 | partial | E2 | cline/cline@d3ecc48:.clinerules/general.md:1 | guide 无该维话题；E2 文档(非实现) 原文「`This file is the secret sauce for working effectively in this codebase. It captures tribal knowledge—the nuanc`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D17 | partial | E2 | cline/cline@d3ecc48:sdk/AGENTS.md:23 | guide 无该维话题；E2 文档(非实现) 原文「`- `@cline/agents`: stateless agent loop, tool orchestration, hook/extension runtime, event streaming`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
