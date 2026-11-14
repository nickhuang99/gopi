<!-- doc-class: normative -->
# devdoc: text2img 集成

## 背景
claude 版 text2img（零依赖 TrueType 渲染，IoU 99.5% vs FreeType，OCR 均值 96%）
并入 gopi 单二进制。硬要求：CLI 子命令 + top-level help + log 过滤器三暴露齐全。

## 集成方式（三层，全部走既有模式）
1. **核心** `internal/text2img/render.go`：从 text2img-go-claude/main.go 原样移植
   （只改 main 为 RenderFile + Options 结构，去掉 flag/os.Exit）。入口
   `RenderFile(src, out, Options)`。默认参数常量 `DefaultSize=20 DefaultCols=50
   DefaultLh=32 DefaultLines=90 DefaultMargin=12` —— 单点定义，CLI 与 tool 共用。
2. **共享工具层** `internal/tools/text2img.go`：`Text2Img(src, out, opts)` 返回
   人类可读摘要 —— CLI 与 agent tool_use 都调它（CLI == tool 行为）。
3. **暴露层**：
   - `cmd/agent/cli_text2img.go`：usage const + parseText2imgArgs（纯、可单测）+
     isText2imgSubcommand 接进 main.go dispatch（在通用 -h 扫描**之前**）。
   - `internal/config/help.go`：Subcommands 段 + Use-case examples。
   - `cmd/agent/cli_log.go`：toolFamilies["text2img"]={"text2img"} +
     familyOrder + activeFamilies + logOptions.text2img + logFlagMember。
   - `internal/agent/loop.go`：claudeToolDefs 注册（记得 header "18 tools" 计数
     同步改！）+ dispatch case + validateToolArgs case + strArgOf 助手。

## 坑（本次踩到）
1. **默认输出必须在分页命名之前应用**：原版 main 里 out 默认值在分页之后设置，
   移植成 RenderFile 时若 `out==""` 后置，会产出 `-1.png`、`-2.png`（无 stem）。
   修复：`out=="" → <src>.png` 必须在 `TrimSuffix(out, ext)` 之前。
2. **edit 替换 tool def 时别把 vision_analyze 覆盖掉**：往 claudeToolDefs 尾块
   插入 text2img 时，oldStr 命中了 vision 整块 —— vision_analyze 定义消失，
   TestVisionAnalyzeToolSchema 立刻报 "missing from claudeToolDefs"。工具列表有
   单测守护（toolDefByName），改完必须跑 internal/agent 测试。
3. **log fixture 的 tool_result 结构**：`{"type":"tool_result","message":{"content":
   [{"type":"tool_result","tool_use_id":...,"content":"文本"}]}}` —— content 里是
   **裸字符串**，不是又一层 object。写错了显示 "(no tool_result matched)"。
4. **工具数量断言**：agent header "You are GoPi. N tools" 是硬编码字符串，加工具
   必须同步；`grep -c 'Name:        "'` 对不齐会漏。

## 验证速查
- build: `go build ./... && go vet ./...`
- 静态: `CGO_ENABLED=0 go build -buildvcs=false -o /tmp/x ./cmd/agent` → 16.0 MiB
- live: `gopi --help | grep text2img`; `gopi text2img -h`; `gopi log <id> --text2img`
- OCR: `gopi text2img x.txt && gopi vision_analyze x.png --prompt "识别文字"`
