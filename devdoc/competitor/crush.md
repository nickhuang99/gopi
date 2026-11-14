<!-- doc-class: historical -->
# Crush（charmbracelet/crush@b18531d）

<!-- meta -->
repo: charmbracelet/crush
sha: b18531d
sha_full: b18531d6b0971f36d443d2569fb453cd61cfd090
default_branch: main
guide: crush.md
url_in_guide: absent
license: FSL-1.1-MIT (Functional Source License, MIT Future License)
open_source: 源可用（非 OSI 开源）
pinned_sha_verified_at: 2026-11-05T16:10:00Z (api.github.com/repos/charmbracelet/crush/commits/b18531d6b0971f36d443d2569fb453cd61cfd090)
source_note: `LICENSE.md` 标题为 `Functional Source License, Version 1.1, MIT Future License`，缩写 `FSL-1.1-MIT`：源码可见，但许可不是 OSI 认可的开放源码
evidence_dir: ~/.gopi/sessions/01a1aa6c-9222-7d72-8a42-ba369dd96ae7/artifacts/stage1a/crush/
probe_source: devdoc/competitor-analysis-framework.md §3（17 维 doc 探针正则，唯一真源）
channels: E1 = dsa-docs guide 全文；E2 = 该 sha 下的仓库文件与全树路径。对方不运行（Rule 27/35），E3 结构性不可用，故上限 partial

| 维度 | 等级 | 证据等级 | 证据（命令 或 file:line） | 备注 |
|---|---|---|---|---|
| D01 | unknown | E1 | dsa-docs/crush.md:13 | 未运行（成本/权限）；E1/E2 三通道均未取得该维输出；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/crush.md ; https://api.github.com/repos/charmbracelet/crush/git/trees/b18531d6b0971f36d443d2569fb453cd61cfd090?recursive=1 ; https://cdn.jsdelivr.net/gh/charmbracelet/crush@b18531d6b0971f36d443d2569fb453cd61cfd090/<path> ; https://raw.githubusercontent.com/charmbracelet/crush/b18531d6b0971f36d443d2569fb453cd61cfd090/<path> |
| D02 | partial | E1+E2 | dsa-docs/crush.md:38 + charmbracelet/crush@b18531d:docs/config/FUTURE.md:25 | E1 原文「`"api_key": "$DEEPSEEK_API_KEY",`」；E2 文档(非实现) 原文「``export` or `alias` at a live prompt.`」。对方只允许 E1+E2，上限 partial |
| D03 | partial | E1+E2 | dsa-docs/crush.md:37 + charmbracelet/crush@b18531d:docs/config/FUTURE.md:46 | E1 原文「`"base_url": "https://api.deepseek.com",`」；E2 文档(非实现) 原文「`model-switch dialog and API-key entry:`」。对方只允许 E1+E2，上限 partial |
| D04 | partial | E1+E2 | dsa-docs/crush.md:28 + charmbracelet/crush@b18531d:docs/config/FUTURE.md:121 | E1 原文「`- **Linux / macOS**: `~/.config/crush/crush.json``」；E2 文档(非实现) 原文「`~/.local/share/crush/crush.json`」。对方只允许 E1+E2，上限 partial |
| D05 | partial | E2 | charmbracelet/crush@b18531d:internal/cmd/projects.go:23 | guide 无该维话题；E2 源码 原文「`crush projects --json`」。E2 单独出现，上限 partial |
| D06 | none | E1+E2 | — | guide 全文 0 命中 + 全树 1398 blob 路径 0 命中 + 抽样内容 0 命中 —— 看过、没有（该读数的适用范围是 guide 语料与该 sha 的文件路径，不等于运行时一定不具备） |
| D07 | partial | E1+E2 | dsa-docs/crush.md:5 + charmbracelet/crush@b18531d:docs/hooks/FUTURE.md:1 | E1 原文「`Crush is a glamorous open-source AI coding agent that runs in your terminal, built by Charm. It supports multi`」；E2 文档(非实现) 原文「`# Hooks: Future Work`」。对方只允许 E1+E2，上限 partial |
| D08 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 14 个（`internal/agent/tools/write_permission_test.go`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/crush.md ; https://api.github.com/repos/charmbracelet/crush/git/trees/b18531d6b0971f36d443d2569fb453cd61cfd090?recursive=1 ; https://cdn.jsdelivr.net/gh/charmbracelet/crush@b18531d6b0971f36d443d2569fb453cd61cfd090/<path> ; https://raw.githubusercontent.com/charmbracelet/crush/b18531d6b0971f36d443d2569fb453cd61cfd090/<path> |
| D09 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 5 个（`internal/agent/isolate_test.go`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/crush.md ; https://api.github.com/repos/charmbracelet/crush/git/trees/b18531d6b0971f36d443d2569fb453cd61cfd090?recursive=1 ; https://cdn.jsdelivr.net/gh/charmbracelet/crush@b18531d6b0971f36d443d2569fb453cd61cfd090/<path> ; https://raw.githubusercontent.com/charmbracelet/crush/b18531d6b0971f36d443d2569fb453cd61cfd090/<path> |
| D10 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 8 个（`internal/message/attachment.go`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/crush.md ; https://api.github.com/repos/charmbracelet/crush/git/trees/b18531d6b0971f36d443d2569fb453cd61cfd090?recursive=1 ; https://cdn.jsdelivr.net/gh/charmbracelet/crush@b18531d6b0971f36d443d2569fb453cd61cfd090/<path> ; https://raw.githubusercontent.com/charmbracelet/crush/b18531d6b0971f36d443d2569fb453cd61cfd090/<path> |
| D11 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 4 个（`internal/ui/dialog/quit_mouse_test.go`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/crush.md ; https://api.github.com/repos/charmbracelet/crush/git/trees/b18531d6b0971f36d443d2569fb453cd61cfd090?recursive=1 ; https://cdn.jsdelivr.net/gh/charmbracelet/crush@b18531d6b0971f36d443d2569fb453cd61cfd090/<path> ; https://raw.githubusercontent.com/charmbracelet/crush/b18531d6b0971f36d443d2569fb453cd61cfd090/<path> |
| D12 | partial | E1+E2 | dsa-docs/crush.md:5 + charmbracelet/crush@b18531d:internal/cmd/server.go:16 | E1 原文「`Crush is a glamorous open-source AI coding agent that runs in your terminal, built by Charm. It supports multi`」；E2 源码 原文「`"github.com/charmbracelet/crush/internal/server"`」。对方只允许 E1+E2，上限 partial |
| D13 | partial | E2 | charmbracelet/crush@b18531d:internal/cmd/logs.go:85 | guide 无该维话题；E2 源码 原文「`return fmt.Errorf("failed to tail log file: %v", err)`」。E2 单独出现，上限 partial |
| D14 | partial | E2 | charmbracelet/crush@b18531d:internal/cmd/stats.go:49 | guide 无该维话题；E2 源码 原文「`Long:  "Generate and display usage statistics including token usage, costs, and activity patterns",`」。E2 单独出现，上限 partial |
| D15 | partial | E1+E2 | dsa-docs/crush.md:5 + charmbracelet/crush@b18531d:internal/lsp/client.go:1 | E1 原文「`Crush is a glamorous open-source AI coding agent that runs in your terminal, built by Charm. It supports multi`」；E2 源码 原文「`package lsp`」。对方只允许 E1+E2，上限 partial |
| D16 | unknown | — | — | 未运行（成本/权限）；guide 全文 0 命中、抽样内容 0 命中，全树路径命中 4 个（`AGENTS.md`），但抽样未取到其内容；试过的 URL：本地语料（非 URL）/home/user/.gopi/sessions/0f3b0aab-aa91-4096-9505-0737bdf0b4f3/artifacts/dsa-docs/crush.md ; https://api.github.com/repos/charmbracelet/crush/git/trees/b18531d6b0971f36d443d2569fb453cd61cfd090?recursive=1 ; https://cdn.jsdelivr.net/gh/charmbracelet/crush@b18531d6b0971f36d443d2569fb453cd61cfd090/<path> ; https://raw.githubusercontent.com/charmbracelet/crush/b18531d6b0971f36d443d2569fb453cd61cfd090/<path> |
| D17 | partial | E2 | charmbracelet/crush@b18531d:AGENTS.md:44 | guide 无该维话题；E2 文档(非实现) 原文「`ui/                              Bubble Tea v2 TUI (see internal/ui/AGENTS.md)`」。E2 单独出现，上限 partial |
