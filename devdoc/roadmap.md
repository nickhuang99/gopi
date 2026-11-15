<!-- doc-class: roadmap -->
# GoPi 项目开发路线图

> **想到但先不做的点子不在这里** —— 它们在 append-only 清单 [`devdoc/deferred.md`](deferred.md)（只追加，不删除）。本文件只放"我们朝哪儿建"的阶段性计划。

## 目标

使用 Go 语言复刻 pi-mono 核心 ReAct 循环，打造静态单二进制 AI 编码代理 CLI。

## 参考源

- pi-mono 仓库：https://github.com/earendil-works/pi（7.7w+ stars, MIT 协议）
- 本地源码：/home/user/work/pi-mono/
- 核心文件：
  - `packages/agent/src/agent-loop.ts`（792 行）—— ReAct 循环
  - `packages/agent/src/types.ts`（437 行）—— 代理类型定义
  - `packages/ai/src/` —— LLM 提供者层（DeepSeek, Anthropic 等）

## 架构

```
GoPi/
├── cmd/agent/main.go              # CLI 入口点
├── internal/
│   ├── deepseek/
│   │   ├── client.go              # DeepSeek HTTP 客户端
│   │   ├── types.go               # 消息/工具调用/工具结果类型
│   │   └── stream.go              # SSE 流解析器
│   ├── tools/
│   │   ├── read.go                # read: 读取文件内容
│   │   ├── write.go               # write: 覆写文件
│   │   ├── edit.go                # edit: 字符串片段替换
│   │   └── bash.go                # bash: 执行终端命令
│   └── agent/
│       ├── loop.go                # ReAct 循环（runLoop）
│       └── context.go             # 消息上下文管理
├── devdoc/roadmap.md              # 本文件
└── go.mod                         # Go 模块定义
```

## 开发阶段

### Phase 0 — DeepSeek HTTP 客户端（1 个 Claude）
- **文件：** internal/deepseek/client.go, types.go, stream.go
- **功能：** 发送 POST 请求到 api.deepseek.com，解析 JSON 响应，处理流式 SSE
- **输入：** API 密钥，模型名称，消息列表（用户/助手/工具结果）
- **输出：** 助手消息（文本内容 + JSON 工具调用块）
- **测试：** 发送简单请求（"hello"）→ 验证响应包含文本

### Phase 1 — 工具实现（3 个 Claude，可并行）
- **read.go：** 读取文件完整内容，输入路径 → 输出文件内容
- **write.go：** 全量覆写文件，输入路径 + 内容 → 输出确认
- **bash.go：** 执行终端命令，输入命令 → 输出 stdout/stderr
- **每个工具：** 1 个 Go 文件 + 单元测试，依赖 Phase 0 的 client.go

### Phase 2 — ReAct 循环集成（1 个 Claude）
- **loop.go：** streamAssistantResponse → 提取工具调用 JSON → 执行工具 → 回填结果 → 循环
- **context.go：** 消息上下文追加、上下文窗口清理
- **main.go：** 读取环境变量（DEEPSEEK_API_KEY）、加载输入文件、运行循环、写入输出文件

## 设计约束

1. **最小化、无花哨功能：** 仅输入文件作为上下文，输出文件作为 LLM 响应
2. **JSON 工具调用格式：** `{"tool": "read", "params": {"path": "..."}}`
3. **遵循 pi-mono 的 ReAct 逻辑：** 外循环（新消息）+ 内循环（工具调用）
4. **仅支持 DeepSeek 后端：** v4-pro 模型，OpenAI 兼容 API 格式
5. **CGO_ENABLED=0：** 始终编译为静态二进制文件

## 工具设计原则

GoPi 提供三个文件操作工具，每个工具有明确的设计边界：

### read(path, offset, limit) → 安全浏览内容
- 读取文件内容，支持大文件的部分读取
- `offset=0, limit=0`：读取整个文件（向后兼容）
- `offset>0`：跳过前 N 行（1-indexed 行计数），适用于从上次断点继续浏览
- `limit>0`：最多读取 N 行，防止大文件撑爆上下文窗口
- 保持所有现有验证：空路径拒绝、目录拒绝、波浪号展开

### write(path, content) → 创建/覆写整个文件
- 原子化全量覆写，不适用于追加或部分编辑
- 适用场景：新建文件、完全替换文件内容

### edit(path, oldStr, newStr) → 精确字符串替换
- 在文件中查找唯一字符串并替换，用于外科手术式修改
- `oldStr` 必须在文件中唯一出现，防止误改
- 适用场景：修改函数签名、修改变量名、修复单行 bug
- 不适用于：大范围重写（用 write）、追加内容（用 write 读-改-写）
