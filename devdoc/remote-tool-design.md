<!-- doc-class: historical -->
# GoPi `remote` Tool — Design

> 源自: ~/work/claude/nick/gopi_scheduler_integration_design_talk.txt Part 1
> 日期: 2026-08-02
> 状态: 设计阶段，独立于 scheduler 项目

## 问题

GoPi 的 4 个基础工具 (read/write/edit/bash) 只能操作本地文件系统。
多节点 SSH 场景下，模型无法区分本地/远端，导致环境混淆——
时而用本地路径、时而写内联 SSH 命令、时而混用两者。

Pi Mono 的 `ssh` 可选参数在单层直连场景工作良好，但在多级跳板
(jumper chain) 场景完全失效——工具参数的静态地址无法描述会话栈。

## 方案：5 工具最小闭包

```
本地操作 (4 tools，仅限本地):
  read   — 读文件，仅本地路径
  write  — 写文件，仅本地路径
  edit   — 编辑文件，仅本地路径
  bash   — 执行命令，仅本地

跨机操作 (1 tool，所有远端):
  remote — 封装 SSH 跳转 + 文件传输 + 命令执行
```

### 硬边界（写进系统提示词，不可绕过）

任意工具调用包含 SSH 跳转、`ssh user@host`、`scp` 等跨机操作
→ 拒绝执行 → 强制重写为 `remote` 包裹的脚本形式。

### `remote` 工具接口

```json
{
  "target": {
    "hosts": ["jump-A", "jump-B", "target-C"],  // 跳板链，第一个是入口，最后是终点
    "user": "nick",
    "port": 22
  },
  "script": "bash script to execute on target...",
  "files": {
    "upload": [{"local": "/path/a", "remote": "/tmp/a"}],
    "download": [{"remote": "/tmp/result", "local": "/path/result"}]
  },
  "timeout_sec": 300,
  "env": {"CUSTOM_VAR": "value"}
}
```

### 内部实现流程

```
1. 本地: scp 上传脚本 + 输入文件到跳板链入口
2. SSH: 逐级跳转 ssh -J A,B target
3. 远端: 执行脚本，捕获输出
4. 回传: scp 下载输出文件 + stdout/stderr
5. 返回: {stdout, stderr, exit_code, elapsed}
```

### 为什么不用 Pi 的 `ssh` 参数方案

| | Pi `ssh` 参数 | GoPi `remote` |
|------|-------------|-----------|
| 单层直连 | ✅ 简洁 | ✅ 可处理 |
| 多级跳板 | ❌ 失效（静态地址无会话栈） | ✅ 链式声明 |
| 文件传输 | ❌ 不处理 | ✅ upload/download |
| 边界明确 | ❌ 软约束（靠模型自觉） | ✅ 硬约束（拒绝混用） |
| 复杂度 | 分散在每次工具调用 | 封闭在脚本内部 |

### 与现有工具的共存

- `read/write/edit/bash` 不变——仅限本地
- `bash` 可以执行任何本地命令（grep/find/ls/git）
- 不拆分 grep/find 为独立工具——保持极简
- `remote` 不替代 `bash`——只处理跨机

### 系统提示词注入

```
"You are GoPi. Tools: read, write, edit, bash (local only), remote (cross-machine).
 If any file path is on a remote host, or any command needs SSH, you MUST use remote.
 Using bash for SSH, scp, or remote paths is a HARD ERROR — the operation will be rejected."
```
