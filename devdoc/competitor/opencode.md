<!-- doc-class: historical -->
# OpenCode（anomalyco/opencode@48f88b4）

<!-- meta -->
repo: anomalyco/opencode
sha: 48f88b4
sha_full: 48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3
default_branch: dev
guide: opencode.md
url_in_guide: absent
license: MIT
open_source: 开源
pinned_sha_verified_at: 2026-11-05T16:10:00Z (api.github.com/repos/anomalyco/opencode/commits/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3)
source_note: 默认分支是 `dev`（不是 `main`），sha 取自 `refs/heads/dev`
evidence_dir: ~/.gopi/sessions/01a1aa6c-9222-7d72-8a42-ba369dd96ae7/artifacts/stage1a/opencode/
probe_source: devdoc/competitor-analysis-framework.md §3（17 维 doc 探针正则，唯一真源）
channels: E1 = dsa-docs guide 全文；E2 = 该 sha 下的仓库文件与全树路径。对方不运行（Rule 27/35），E3 结构性不可用，故上限 partial

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | none | E1+E2 | — | guide 全文 0 命中 + 全树 9862 blob 路径 0 命中 + 抽样内容 0 命中 —— 看过、没有（该读数的适用范围是 guide 语料与该 sha 的文件路径，不等于运行时一定不具备） |
| D02 | partial | E1+E2 | dsa-docs/opencode.md:17 + anomalyco/opencode@48f88b4:sst.config.ts:3 | E1 原文「`- Enter your [DeepSeek API Key](https://platform.deepseek.com/api_keys)<br/>`」；E2 源码 原文「`export default $config({`」。对方只允许 E1+E2，上限 partial |
| D03 | partial | E1+E2 | dsa-docs/opencode.md:17 + anomalyco/opencode@48f88b4:sst.config.ts:27 | E1 原文「`- Enter your [DeepSeek API Key](https://platform.deepseek.com/api_keys)<br/>`」；E2 源码 原文「`apiKey: process.env.STRIPE_SECRET_KEY!,`」。对方只允许 E1+E2，上限 partial |
| D04 | partial | E2 | anomalyco/opencode@48f88b4:tsconfig.json:3 | guide 无该维话题；E2 源码 原文「`"extends": "@tsconfig/bun/tsconfig.json",`」。E2 单独出现，上限 partial |
| D05 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 2 个（`packages/cli/src/run/noninteractive.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/opencode.md ; https://api.github.com/repos/anomalyco/opencode/git/trees/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3?recursive=1 ; https://cdn.jsdelivr.net/gh/anomalyco/opencode@48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> ; https://raw.githubusercontent.com/anomalyco/opencode/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> |
| D06 | none | E1+E2 | — | guide 全文 0 命中 + 全树 9862 blob 路径 0 命中 + 抽样内容 0 命中 —— 看过、没有（该读数的适用范围是 guide 语料与该 sha 的文件路径，不等于运行时一定不具备） |
| D07 | partial | E2 | anomalyco/opencode@48f88b4:specs/v2/tools.md:3 | guide 无该维话题；E2 文档(非实现) 原文「`Status: **Current semantic overview.** The Plugin package owns the public tool type; Core owns registration, e`」。E2 单独出现，上限 partial |
| D08 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 54 个（`packages/app/src/session/requests/auto-approve-machine.test.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/opencode.md ; https://api.github.com/repos/anomalyco/opencode/git/trees/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3?recursive=1 ; https://cdn.jsdelivr.net/gh/anomalyco/opencode@48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> ; https://raw.githubusercontent.com/anomalyco/opencode/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> |
| D09 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 35 个（`.dockerignore`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/opencode.md ; https://api.github.com/repos/anomalyco/opencode/git/trees/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3?recursive=1 ; https://cdn.jsdelivr.net/gh/anomalyco/opencode@48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> ; https://raw.githubusercontent.com/anomalyco/opencode/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> |
| D10 | partial | E2 | anomalyco/opencode@48f88b4:packages/ai/src/image.ts:14 | guide 无该维话题；E2 源码 原文「`import { ImageClient, Service } from "./image-client.js"`」。E2 单独出现，上限 partial |
| D11 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 236 个（`packages/app/component-tests/browser-pane-restore.spec.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/opencode.md ; https://api.github.com/repos/anomalyco/opencode/git/trees/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3?recursive=1 ; https://cdn.jsdelivr.net/gh/anomalyco/opencode@48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> ; https://raw.githubusercontent.com/anomalyco/opencode/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> |
| D12 | partial | E2 | anomalyco/opencode@48f88b4:infra/webapp.ts:11 | guide 无该维话题；E2 源码 原文「`VITE_OPENCODE_SERVER_MODE: "none",`」。E2 单独出现，上限 partial |
| D13 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 65 个（`packages/ai/src/protocols/assemblyai-transcription.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/opencode.md ; https://api.github.com/repos/anomalyco/opencode/git/trees/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3?recursive=1 ; https://cdn.jsdelivr.net/gh/anomalyco/opencode@48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> ; https://raw.githubusercontent.com/anomalyco/opencode/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> |
| D14 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 4 个（`packages/ai/script/recording-cost-report.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/opencode.md ; https://api.github.com/repos/anomalyco/opencode/git/trees/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3?recursive=1 ; https://cdn.jsdelivr.net/gh/anomalyco/opencode@48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> ; https://raw.githubusercontent.com/anomalyco/opencode/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> |
| D15 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 84 个（`packages/app/src/settings/workspaces/project-lsp.test.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/opencode.md ; https://api.github.com/repos/anomalyco/opencode/git/trees/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3?recursive=1 ; https://cdn.jsdelivr.net/gh/anomalyco/opencode@48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> ; https://raw.githubusercontent.com/anomalyco/opencode/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> |
| D16 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 47 个（`.opencode/command/learn.md`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/opencode.md ; https://api.github.com/repos/anomalyco/opencode/git/trees/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3?recursive=1 ; https://cdn.jsdelivr.net/gh/anomalyco/opencode@48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> ; https://raw.githubusercontent.com/anomalyco/opencode/48f88b4c7d7bf5b8e1575a10469aaf7c7812d4e3/<path> |
| D17 | partial | E2 | anomalyco/opencode@48f88b4:AGENTS.md:62 | guide 无该维话题；E2 文档(非实现) 原文「`- In `src/config`, follow the existing self-export pattern at the top of the file (for example `export * as Co`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
