<!-- doc-class: historical -->
# GoPi — Feature: GOPI.md Project Context Injection

## 动机

GoPi 当前只能接收一个任务 prompt，无法注入项目级上下文（准则、规范、编码规则）。
Hermes 和 Claude Code 都有工作目录上下文文件机制，GoPi 需要同样的能力。

## 设计

GoPi 启动时检查当前工作目录是否存在 `GOPI.md`，如存在则读取并注入为 system 消息。

```
消息顺序（不可变）:
  1. Kernel prompt:  "You are GoPi. Tools: read, write, edit, bash."  [硬编码，不可变]
  2. GOPI.md:        cwd 中可选文件 → 作为 system 消息注入  [用户控制，可选]
  3. Task prompt:    用户的任务描述（stdin 或文件参数）[始终必需]
```

### 注入位置

在 `context.go GetMessages()` 中：
- Kernel prompt → 永远第一条 system 消息
- GOPI.md 内容 → 第二条 system 消息（如存在）
- 用户消息 → 后续 user/assistant 消息正常拼接

### 参考

- Hermes: `AGENTS.md`, `CLAUDE.md`, `.cursorrules` → `build_context_files_prompt()` in `agent/context_files.py`
- Claude Code: `CLAUDE.md` in project directory → role router + guidelines
- pi-mono: system prompt injection via agent configuration

## 非目标

- 不是环境变量 —— 是工作目录中的文件
- 不是硬编码提示词 —— 是用户可复制/粘贴的独立文件
- 不是必选的 —— 文件不存在时正常跳过
