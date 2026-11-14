<!-- doc-class: historical -->
# Pi（earendil-works/pi@55a5c69）

<!-- meta -->
repo: earendil-works/pi
sha: 55a5c69
sha_full: 55a5c6914570e2ae31ca36297654483bbbf46883
default_branch: main
guide: pi_mono.md
url_in_guide: https://github.com/badlogic/pi-mono
license: MIT
open_source: 开源
pinned_sha_verified_at: 2026-11-05T16:10:00Z (api.github.com/repos/earendil-works/pi/commits/55a5c6914570e2ae31ca36297654483bbbf46883)
source_note: guide 写的 URL 是 `badlogic/pi-mono`，阶段 0 §6 核对后真实仓库已更名为 `earendil-works/pi`（重定向后地址）
evidence_dir: ~/.gopi/sessions/01a1aa6c-9222-7d72-8a42-ba369dd96ae7/artifacts/stage1a/pi/
probe_source: devdoc/competitor-analysis-framework.md §3（17 维 doc 探针正则，唯一真源）
channels: E1 = dsa-docs guide 全文；E2 = 该 sha 下的仓库文件与全树路径。对方不运行（Rule 27/35），E3 结构性不可用，故上限 partial

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | unknown | E1 | dsa-docs/pi_mono.md:13 | 未运行（成本/权限）；E1/E2 三通道均未取得该维输出；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/pi_mono.md ; https://api.github.com/repos/earendil-works/pi/git/trees/55a5c6914570e2ae31ca36297654483bbbf46883?recursive=1 ; https://cdn.jsdelivr.net/gh/earendil-works/pi@55a5c6914570e2ae31ca36297654483bbbf46883/<path> ; https://raw.githubusercontent.com/earendil-works/pi/55a5c6914570e2ae31ca36297654483bbbf46883/<path> |
| D02 | partial | E1+E2 | dsa-docs/pi_mono.md:40 + earendil-works/pi@55a5c69:tsconfig.json:60 | E1 原文「`"apiKey": "$DEEPSEEK_API_KEY",`」；E2 源码 原文「`"@earendil-works/pi-env": ["./packages/env/src/index.ts"],`」。对方只允许 E1+E2，上限 partial |
| D03 | partial | E1+E2 | dsa-docs/pi_mono.md:38 + earendil-works/pi@55a5c69:.pi/skills/add-llm-provider.md:34 | E1 原文「`"baseUrl": "https://api.deepseek.com",`」；E2 文档(非实现) 原文「`- Create `src/providers/<id>.ts` exporting `<id>Provider()`, which calls `createProvider({ id, name, baseUrl, `」。对方只允许 E1+E2，上限 partial |
| D04 | partial | E1+E2 | dsa-docs/pi_mono.md:31 + earendil-works/pi@55a5c69:apps/p2/tsconfig.json:2 | E1 原文「`- **Linux / macOS**: `~/.pi/agent/models.json``」；E2 源码 原文「`"extends": "../../tsconfig.json",`」。对方只允许 E1+E2，上限 partial |
| D05 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 1 个（`packages/chord/test/examples/56-web/headless.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/pi_mono.md ; https://api.github.com/repos/earendil-works/pi/git/trees/55a5c6914570e2ae31ca36297654483bbbf46883?recursive=1 ; https://cdn.jsdelivr.net/gh/earendil-works/pi@55a5c6914570e2ae31ca36297654483bbbf46883/<path> ; https://raw.githubusercontent.com/earendil-works/pi/55a5c6914570e2ae31ca36297654483bbbf46883/<path> |
| D06 | none | E1+E2 | — | guide 全文 0 命中 + 全树 3983 blob 路径 0 命中 + 抽样内容 0 命中 —— 看过、没有（该读数的适用范围是 guide 语料与该 sha 的文件路径，不等于运行时一定不具备） |
| D07 | partial | E1+E2 | dsa-docs/pi_mono.md:5 + earendil-works/pi@55a5c69:scripts/tool-stats.ts:201 | E1 原文「`Pi (pi-mono) is a minimal, aggressively extensible terminal coding harness. It adapts to your workflows throug`」；E2 源码 原文「`rows.forEach((r,i)=>new Chart(document.getElementById(el+'Hist'+i),{type:'bar',data:{labels:data.bucketLabels,`」。对方只允许 E1+E2，上限 partial |
| D08 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 4 个（`.github/APPROVED_CONTRIBUTORS`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/pi_mono.md ; https://api.github.com/repos/earendil-works/pi/git/trees/55a5c6914570e2ae31ca36297654483bbbf46883?recursive=1 ; https://cdn.jsdelivr.net/gh/earendil-works/pi@55a5c6914570e2ae31ca36297654483bbbf46883/<path> ; https://raw.githubusercontent.com/earendil-works/pi/55a5c6914570e2ae31ca36297654483bbbf46883/<path> |
| D09 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 19 个（`apps/p2/src/hosts/tui-sandbox.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/pi_mono.md ; https://api.github.com/repos/earendil-works/pi/git/trees/55a5c6914570e2ae31ca36297654483bbbf46883?recursive=1 ; https://cdn.jsdelivr.net/gh/earendil-works/pi@55a5c6914570e2ae31ca36297654483bbbf46883/<path> ; https://raw.githubusercontent.com/earendil-works/pi/55a5c6914570e2ae31ca36297654483bbbf46883/<path> |
| D10 | partial | E2 | earendil-works/pi@55a5c69:packages/ai/src/images.ts:1 | guide 无该维话题；E2 源码 原文「`import "./providers/images/register-builtins.ts";`」。E2 单独出现，上限 partial |
| D11 | partial | E2 | earendil-works/pi@55a5c69:apps/p2/test/p2/browser.ts:7 | guide 无该维话题；E2 源码 原文「`import { buildBrowserScript } from "../../src/hosts/bundles.ts";`」。E2 单独出现，上限 partial |
| D12 | partial | E2 | earendil-works/pi@55a5c69:apps/p2/plugins/mcp/web.tsx:1 | guide 无该维话题；E2 源码 原文「`// The MCP sheet (owners): the running servers with their status, errors and tools, restart, and signing in an`」。E2 单独出现，上限 partial |
| D13 | partial | E2 | earendil-works/pi@55a5c69:apps/p2/src/runtime/logs.ts:361 | guide 无该维话题；E2 源码 原文「`/** Detached hosts publish an atomic, bounded, already-redacted snapshot; no unbounded raw log file. */`」。E2 单独出现，上限 partial |
| D14 | partial | E1+E2 | dsa-docs/pi_mono.md:50 + earendil-works/pi@55a5c69:scripts/cost.ts:17 | E1 原文「`"cost": {`」；E2 源码 原文「`console.log(`Usage: cost.ts -d <path> -n <days>`」。对方只允许 E1+E2，上限 partial |
| D15 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 2 个（`packages/coding-agent/src/core/tools/grep.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/pi_mono.md ; https://api.github.com/repos/earendil-works/pi/git/trees/55a5c6914570e2ae31ca36297654483bbbf46883?recursive=1 ; https://cdn.jsdelivr.net/gh/earendil-works/pi@55a5c6914570e2ae31ca36297654483bbbf46883/<path> ; https://raw.githubusercontent.com/earendil-works/pi/55a5c6914570e2ae31ca36297654483bbbf46883/<path> |
| D16 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 14 个（`AGENTS.md`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/pi_mono.md ; https://api.github.com/repos/earendil-works/pi/git/trees/55a5c6914570e2ae31ca36297654483bbbf46883?recursive=1 ; https://cdn.jsdelivr.net/gh/earendil-works/pi@55a5c6914570e2ae31ca36297654483bbbf46883/<path> ; https://raw.githubusercontent.com/earendil-works/pi/55a5c6914570e2ae31ca36297654483bbbf46883/<path> |
| D17 | partial | E1+E2 | dsa-docs/pi_mono.md:31 + earendil-works/pi@55a5c69:AGENTS.md:49 | E1 原文「`- **Linux / macOS**: `~/.pi/agent/models.json``」；E2 文档(非实现) 原文「`- Run `npm run build` whenever something needs built output (`dist/`): tests or benchmarks that import built p`」。对方只允许 E1+E2，上限 partial |
