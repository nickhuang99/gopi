<!-- doc-class: historical -->
# Task: symlink-aware read/write/edit tools for gopi

## 背景与问题

gopi 的文件工具 read/write/edit 对 symlink 路径的处理有严重缺陷，曾导致用户配置文件损坏（8/28 事故）：
- 用户 `~/.gopi/settings.json` 是 symlink → `.flash_settings.json`
- agent 用 write/edit 修改 settings.json 时，`os.Rename(tmp, path)` 的 POSIX 语义是**替换 symlink 节点本身**，而不是跟随写 target 文件
- 结果：symlink 被破坏成普通文件，target 文件完全没变——写入"成功"但实际没写进配置，且 symlink 结构被破坏
- Edit 更隐蔽：先 `os.ReadFile`（跟随 symlink 读 target 内容）→ 改完 `atomicWriteFile`（rename 到 symlink 路径）→ 读的是 target，写完后 symlink 被替换成普通文件，target 原封不动——修改"丢失"

用户要求：read/write/edit 必须 symlink-aware。设计方案（用户已认可方向）：

## 设计方案

### 1. Read（internal/tools/read.go）
- 检测路径是否为 symlink（用 os.Lstat，不跟随）
- 若是 symlink，在返回内容中附加提示行（类似现有 footer 风格）：
  `\n[symlink: <path> -> <target>]\n`
- 仍然跟随读取 target 内容（读 symlink = 读 target 是合理行为），只是告知 LLM
- 提示行放在内容之后、footer 之前（如果有 footer），用英文括号标注

### 2. Write（internal/tools/write.go）
- 在 writeImpl 开头用 os.Lstat 检测 symlink（path 可能经过 expandHome，检测前先 expand）
- 若 path 是 symlink：
  - **默认拒绝**：返回错误，指明该路径是 symlink、target 是谁、提示用户写 target 路径或加 follow_symlink=true
  - 错误格式：`write: <path> is a symlink -> <target>. Refusing to replace the symlink itself (would corrupt the link and leave the target unchanged). Write to <target> directly, or pass follow_symlink=true to write through the symlink.`
- 新增参数 follow_symlink bool（默认 false）：
  - true 时：用 filepath.EvalSymlinks 解析真实 target 路径，对 target 执行原子写（symlink 保持不动，target 内容更新）
- 注意：atomicWriteFile 内部 rename 到 symlink 路径会替换 symlink 节点——所以 follow 时必须先 resolve 到 target 物理路径再 rename

### 3. Edit（internal/tools/edit.go）
- 同 Write 逻辑：editImpl 开头 Lstat 检测 symlink
- 默认拒绝 + follow_symlink 参数
- 注意 Edit 现有流程是 ReadFile（跟随读 target）→ atomicWriteFile（rename 到路径）——必须改成：默认拒绝；follow=true 时 resolve target 后读+写 target

### 4. Schema（internal/agent/loop.go claudeToolDefs）
- write 和 edit 的 InputSchema 增加 `follow_symlink` boolean 参数（默认 false），description 说明用途
- write/edit 的 Description 增加一句 symlink 行为说明（如："Paths that are symlinks are refused by default to protect the link; pass follow_symlink=true to write through to the target."）
- read 的 Description 增加一句（如："If the path is a symlink, the read result notes the link target."）

## 测试要求（必须）

internal/tools/ 下为三个工具分别增加 symlink 测试：
1. read_test.go：symlink 路径读取返回内容 + 提示行包含 target
2. write_test.go：
   - 默认拒绝写 symlink（返回错误，含 target 提示）
   - follow_symlink=true 时写入 target 文件内容，且 symlink 结构保持（Lstat 仍显示 symlink）
3. edit_test.go：
   - 默认拒绝编辑 symlink
   - follow_symlink=true 时编辑 target 内容成功，symlink 保持
- 测试用 t.TempDir() 创建临时 symlink（os.Symlink），不碰真实 ~/.gopi/
- 也要测：非 symlink 路径行为不变（回归）

## 工作流（GOPI.md Rule 6）

- 你已经在 worktree 中：~/workspace/gopi-symlink-aware（分支 feat/symlink-aware）
- 所有编辑在此 worktree 内完成，不碰 ~/work/GoPi 主仓库
- 完成后：go build ./... + go test ./internal/tools/ -count=1 全绿
- 写 devdoc/symlink-aware-tools.md 说明设计
- commit 到 feat/symlink-aware 分支即可（不 push 不 merge，PA 验收后处理）
