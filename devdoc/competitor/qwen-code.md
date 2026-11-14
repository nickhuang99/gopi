<!-- doc-class: historical -->
# Qwen Code（QwenLM/qwen-code@5d45b42）

<!-- meta -->
repo: QwenLM/qwen-code
sha: 5d45b42
sha_full: 5d45b42940d6bab286ae34893e195997f23084ef
default_branch: main
guide: qwen_code.md
url_in_guide: https://github.com/QwenLM/qwen-code
license: Apache-2.0
open_source: 开源
pinned_sha_verified_at: 2026-11-05T16:10:00Z (api.github.com/repos/QwenLM/qwen-code/commits/5d45b42940d6bab286ae34893e195997f23084ef)
source_note: guide 内的 GitHub URL 与本仓库一致
evidence_dir: ~/.gopi/sessions/01a1aa6c-9222-7d72-8a42-ba369dd96ae7/artifacts/stage1a/qwen-code/
probe_source: devdoc/competitor-analysis-framework.md §3（17 维 doc 探针正则，唯一真源）
channels: E1 = dsa-docs guide 全文；E2 = 该 sha 下的仓库文件与全树路径。对方不运行（Rule 27/35），E3 结构性不可用，故上限 partial

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | partial | E1+E2 | dsa-docs/qwen_code.md:16 + QwenLM/qwen-code@5d45b42:Makefile:3 | E1 原文「`npm install -g @qwen-code/qwen-code@latest`」；E2 源码 原文「`.PHONY: help install build build-sandbox build-all test lint format preflight clean start debug release run-np`」。对方只允许 E1+E2，上限 partial |
| D02 | partial | E1+E2 | dsa-docs/qwen_code.md:5 + QwenLM/qwen-code@5d45b42:esbuild.config.js:12 | E1 原文「`Qwen Code is an open-source AI agent that lives in your terminal, built by the Qwen team at Alibaba Group. It `」；E2 源码 原文「`import { isStubbedSdkNodeExporterImport } from './scripts/sdk-node-exporter-stub.js';`」。对方只允许 E1+E2，上限 partial |
| D03 | partial | E1+E2 | dsa-docs/qwen_code.md:5 + QwenLM/qwen-code@5d45b42:packages/cli/tsconfig.json:6 | E1 原文「`Qwen Code is an open-source AI agent that lives in your terminal, built by the Qwen team at Alibaba Group. It `」；E2 源码 原文「`"baseUrl": ".",`」。对方只允许 E1+E2，上限 partial |
| D04 | partial | E2 | QwenLM/qwen-code@5d45b42:packages/cli/tsconfig.json:2 | guide 无该维话题；E2 源码 原文「`"extends": "../../tsconfig.json",`」。E2 单独出现，上限 partial |
| D05 | partial | E2 | QwenLM/qwen-code@5d45b42:packages/cli/tsconfig.json:97 | guide 无该维话题；E2 源码 原文「`"src/nonInteractiveCli.test.ts",`」。E2 单独出现，上限 partial |
| D06 | partial | E2 | QwenLM/qwen-code@5d45b42:.qwen/e2e-tests/2026-08-27-webshell-session-issue-binding.md:52 | guide 无该维话题；E2 文档(非实现) 原文「`session list prefers sidecar `issues` over the live entry; the bridge echoes`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D07 | partial | E2 | QwenLM/qwen-code@5d45b42:.qwen/skills/autofix/SKILL.md:10 | guide 无该维话题；E2 文档(非实现) 原文「`Actions supplies an explicit mode when it invokes this skill; in that path the`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D08 | partial | E2 | QwenLM/qwen-code@5d45b42:scripts/build_sandbox.js:17 | guide 无该维话题；E2 源码 原文「`// See the License for the specific language governing permissions and`」。E2 单独出现，上限 partial |
| D09 | partial | E2 | QwenLM/qwen-code@5d45b42:scripts/build_sandbox.js:42 | guide 无该维话题；E2 源码 原文「`alias: 'dockerfile',`」。E2 单独出现，上限 partial |
| D10 | partial | E2 | QwenLM/qwen-code@5d45b42:.qwen/e2e-tests/2026-08-12-daemon-acp-http-pre-attach-bounds.md:1 | guide 无该维话题；E2 文档(非实现) 原文「`# Daemon ACP HTTP pre-attach bounds`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D11 | partial | E2 | QwenLM/qwen-code@5d45b42:.qwen/e2e-tests/2026-08-04-secondary-artifact-workspace-ownership.md:60 | guide 无该维话题；E2 文档(非实现) 原文「`- Browser screenshot of the secondary artifact panel and the fail-closed`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D12 | partial | E2 | QwenLM/qwen-code@5d45b42:.qwen/e2e-tests/2026-07-28-serve-large-text-range-read.md:1 | guide 无该维话题；E2 文档(非实现) 原文「`# Serve large-text range reads`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D13 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 230 个（`.github/scripts/fixtures/serve-ab-session.jsonl`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/qwen_code.md ; https://api.github.com/repos/QwenLM/qwen-code/git/trees/5d45b42940d6bab286ae34893e195997f23084ef?recursive=1 ; https://cdn.jsdelivr.net/gh/QwenLM/qwen-code@5d45b42940d6bab286ae34893e195997f23084ef/<path> ; https://raw.githubusercontent.com/QwenLM/qwen-code/5d45b42940d6bab286ae34893e195997f23084ef/<path> |
| D14 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 10 个（`docs/design/2026-10-25-model-cost-output.md`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/qwen_code.md ; https://api.github.com/repos/QwenLM/qwen-code/git/trees/5d45b42940d6bab286ae34893e195997f23084ef?recursive=1 ; https://cdn.jsdelivr.net/gh/QwenLM/qwen-code@5d45b42940d6bab286ae34893e195997f23084ef/<path> ; https://raw.githubusercontent.com/QwenLM/qwen-code/5d45b42940d6bab286ae34893e195997f23084ef/<path> |
| D15 | partial | E2 | QwenLM/qwen-code@5d45b42:docs/users/features/lsp.md:1 | guide 无该维话题；E2 文档(非实现) 原文「`# Language Server Protocol (LSP) Support`」。E2 单独出现，上限 partial |
| D16 | partial | E2 | QwenLM/qwen-code@5d45b42:.qwen/e2e-tests/2026-08-10-doctor-memory-tool-result-retention.md:1 | guide 无该维话题；E2 文档(非实现) 原文「`Moved Permanently. Redirecting to https://raw.githubusercontent.com/QwenLM/qwen-code/5d45b42940d6bab286ae34893`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D17 | partial | E2 | QwenLM/qwen-code@5d45b42:AGENTS.md:53 | guide 无该维话题；E2 文档(非实现) 原文「`[AGENTS.md](packages/managed-agent-server/AGENTS.md). It supplements`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
