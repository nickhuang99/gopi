<!-- doc-class: historical -->
# Codex（openai/codex@49f0c60）

<!-- meta -->
repo: openai/codex
sha: 49f0c60
sha_full: 49f0c6022596c361fe676a5c4738fb2be34e3a93
default_branch: main
guide: codex.md
url_in_guide: https://github.com/ZhiYi-R/moon-bridge
license: Apache-2.0
open_source: 开源
pinned_sha_verified_at: 2026-11-05T16:10:00Z (api.github.com/repos/openai/codex/commits/49f0c6022596c361fe676a5c4738fb2be34e3a93)
source_note: guide 内唯一的 GitHub URL 指向第三方 `ZhiYi-R/moon-bridge`，不是本仓库；仓库地址由阶段 0 §6 核对得出
evidence_dir: ~/.gopi/sessions/01a1aa6c-9222-7d72-8a42-ba369dd96ae7/artifacts/stage1a/codex/
probe_source: devdoc/competitor-analysis-framework.md §3（17 维 doc 探针正则，唯一真源）
channels: E1 = dsa-docs guide 全文；E2 = 该 sha 下的仓库文件与全树路径。对方不运行（Rule 27/35），E3 结构性不可用，故上限 partial

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | partial | E1+E2 | dsa-docs/codex.md:14 + openai/codex@49f0c60:docs/install.md:28 | E1 原文「`npm install -g @openai/codex`」；E2 文档(非实现) 原文「`cargo install --locked just`」。对方只允许 E1+E2，上限 partial |
| D02 | partial | E1+E2 | dsa-docs/codex.md:24 + openai/codex@49f0c60:.cargo/config.toml:1 | E1 原文「`#### 2. Get a DeepSeek API Key`」；E2 源码 原文「`[env]`」。对方只允许 E1+E2，上限 partial |
| D03 | partial | E1 | dsa-docs/codex.md:24 | 仅有 guide 声明「`#### 2. Get a DeepSeek API Key`」，全树 10 路径命中但抽样未取到内容 |
| D04 | partial | E1+E2 | dsa-docs/codex.md:37 + openai/codex@49f0c60:.vscode/settings.json:23 | E1 原文「`Create `config.yml` and set your DeepSeek API key:`」；E2 源码 原文「`// Array order for options in ~/.codex/config.toml such as `notify` and the`」。对方只允许 E1+E2，上限 partial |
| D05 | partial | E1+E2 | dsa-docs/codex.md:123 + openai/codex@49f0c60:codex-rs/cli/src/doctor.rs:7 | E1 原文「`MODEL="$(go run ./cmd/moonbridge --config config.yml --print-codex-model)"`」；E2 源码 原文「`//! summary and `--json` support report. PATH entries are untrusted data: checks`」。对方只允许 E1+E2，上限 partial |
| D06 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 26 个（`codex-rs/core/src/agent/control/resume.rs`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/codex.md ; https://api.github.com/repos/openai/codex/git/trees/49f0c6022596c361fe676a5c4738fb2be34e3a93?recursive=1 ; https://cdn.jsdelivr.net/gh/openai/codex@49f0c6022596c361fe676a5c4738fb2be34e3a93/<path> ; https://raw.githubusercontent.com/openai/codex/49f0c6022596c361fe676a5c4738fb2be34e3a93/<path> |
| D07 | partial | E1+E2 | dsa-docs/codex.md:57 + openai/codex@49f0c60:docs/skills.md:1 | E1 原文「`extensions:`」；E2 文档(非实现) 原文「`# Skills`」。对方只允许 E1+E2，上限 partial |
| D08 | partial | E2 | openai/codex@49f0c60:docs/sandbox.md:1 | guide 无该维话题；E2 文档(非实现) 原文「`## Sandbox & approvals`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D09 | partial | E2 | openai/codex@49f0c60:docs/sandbox.md:1 | guide 无该维话题；E2 文档(非实现) 原文「`## Sandbox & approvals`」（命中落在文档而非源码，按未验证声明计）。E2 单独出现，上限 partial |
| D10 | partial | E1 | dsa-docs/codex.md:93 | 仅有 guide 声明「`This minimal config uses the current Moon Bridge configuration structure and enables DeepSeek V4 Pro / Flash, `」，全树 142 路径命中但抽样未取到内容 |
| D11 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 66 个（`codex-rs/app-server-protocol/schema/typescript/v2/BrowserUseAccessApprovalLifetime.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/codex.md ; https://api.github.com/repos/openai/codex/git/trees/49f0c6022596c361fe676a5c4738fb2be34e3a93?recursive=1 ; https://cdn.jsdelivr.net/gh/openai/codex@49f0c6022596c361fe676a5c4738fb2be34e3a93/<path> ; https://raw.githubusercontent.com/openai/codex/49f0c6022596c361fe676a5c4738fb2be34e3a93/<path> |
| D12 | partial | E1+E2 | dsa-docs/codex.md:42 + openai/codex@49f0c60:codex-rs/app-server-client/README.md:1 | E1 原文「`server:`」；E2 文档(非实现) 原文「`# codex-app-server-client`」。对方只允许 E1+E2，上限 partial |
| D13 | partial | E2 | openai/codex@49f0c60:codex-rs/cli/src/login.rs:91 | guide 无该维话题；E2 源码 原文「`"Warning: failed to open login log file {}: {err}",`」。E2 单独出现，上限 partial |
| D14 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 26 个（`codex-rs/app-server-protocol/schema/json/v2/SendAddCreditsNudgeEmailParams.json`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/codex.md ; https://api.github.com/repos/openai/codex/git/trees/49f0c6022596c361fe676a5c4738fb2be34e3a93?recursive=1 ; https://cdn.jsdelivr.net/gh/openai/codex@49f0c6022596c361fe676a5c4738fb2be34e3a93/<path> ; https://raw.githubusercontent.com/openai/codex/49f0c6022596c361fe676a5c4738fb2be34e3a93/<path> |
| D15 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 7 个（`.github/scripts/archive-release-symbols-and-strip-binaries.sh`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/codex.md ; https://api.github.com/repos/openai/codex/git/trees/49f0c6022596c361fe676a5c4738fb2be34e3a93?recursive=1 ; https://cdn.jsdelivr.net/gh/openai/codex@49f0c6022596c361fe676a5c4738fb2be34e3a93/<path> ; https://raw.githubusercontent.com/openai/codex/49f0c6022596c361fe676a5c4738fb2be34e3a93/<path> |
| D16 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 116 个（`codex-rs/app-server-protocol/schema/typescript/ThreadMemoryMode.ts`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/codex.md ; https://api.github.com/repos/openai/codex/git/trees/49f0c6022596c361fe676a5c4738fb2be34e3a93?recursive=1 ; https://cdn.jsdelivr.net/gh/openai/codex@49f0c6022596c361fe676a5c4738fb2be34e3a93/<path> ; https://raw.githubusercontent.com/openai/codex/49f0c6022596c361fe676a5c4738fb2be34e3a93/<path> |
| D17 | partial | E2 | openai/codex@49f0c60:docs/agents_md.md:3 | guide 无该维话题；E2 文档(非实现) 原文「`For information about AGENTS.md, see [this documentation](https://developers.openai.com/codex/guides/agents-md`」。E2 单独出现，上限 partial |
