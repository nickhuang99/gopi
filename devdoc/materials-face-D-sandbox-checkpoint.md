<!-- doc-class: historical -->
# 素材 D 面：隔离与恢复（`gopi sandbox` + `gopi checkpoint`）

## 完成
- 7 条真实运行素材（`docs/archive/demo/materials/00N-*`），每条恰好六件：
  `00-entry.md` / `01-input.txt` / `02-output.txt` / `03-raw.png` / `04-annotated.png` / `05-finding.md`。
  1. `001-sandbox-create` — 沙盒从哪来（末行即路径，`tail -1` 这就是脚本的取法）
  2. `002-checkpoint-list` — 有哪些检查点（`--dir` 指向哪、表头各列何义）
  3. `003-checkpoint-export` — 快照里存的是什么（`wc -c` 量与顶层结构两个读法）
  4. `004-checkpoint-resume` — 真的恢复一次被中断的会话
  5. `005-sandbox-show` — 沙盒根目录由哪个通道决定（`source=` 字段）
  6. `006-sandbox-run` — 沙盒是执行环境（cwd 绑定 + `GOPI_HOME` 注入 + exit 码传播）
  7. `007-sandbox-unknown` — 用法错误不静默（exit=2，列出全部合法子动词）
- 生成器 `scripts/materials/gen-taskD.sh`（OPerator-only，见 `devdoc/worker-ownership.md` 头注释），
  渲染器 `scripts/materials/materials-render-D.py`。
- 3 条素材带非空 finding（缺陷/边界观察），4 条带"本次运行没挖到缝"的明确结论。

## 没做
- 未做 `gopi sandbox` 的 GC/回收（`scratch-gc sandboxes`）与 `checkpoint` 的
  `--dir` 边界（空目录/损坏日志）；D 面按简报只覆盖 6 个动词。
- 机器判据只到"形状 + 映射自证"：**图像的语义正确性（框是否框在对的行）靠渲染器自证 + 我逐条目视**，
  没有自动化的人类级判据。
- `003-checkpoint-export` 的正文是"量大小 + 列顶层结构"两条命令，
  不是把 77KB JSON 整份画进图（一页装不下；完整字节仍在 `02-output.txt` 的命令块里）。

## 验证
- Build: `CGO_ENABLED=0 go build -o bin/gopi ./cmd/agent/` — OK（`bin/gopi` 存在且可执行）
- Tests: 形状断言 `assert_shape`（每条恰好 6 件、件件非空、`^exit=` 行存在、
  `03-raw.png` 与 `04-annotated.png` 的 sha256 不同）——7/7 通过，生成器 exit=0
- Ran: `bash scripts/materials/gen-taskD.sh <worktree>/docs/archive/demo/materials`（离屏 `:80`，Rule 22）
- Result:
  - 生成器 exit=0，`failed.txt` 为空；逐条 `entry <name> exit=0 render=0`
  - census：`bash scripts/materials-coverage.sh` → `produced: 41 / 193`，
    本面 7 条全部 `complete`，`unreadable: 0`（无"声明要标注却没有标注图"的条目）
  - 每个沙盒由 `gopi sandbox create` 产生（Rule 34），条目全程 `GOPI_HOME=<sandbox>`，
    操作者真台账未被读写
- Commit: 见本条提交

## Evidence (self-filed)
- 形状：`for d in docs/archive/demo/materials/00[1-7]-*/; do ls $d | wc -l; done` → 全部 6；
  `grep -c '^exit=' $d/02-output.txt` → 2/3/3/3/4/3/5（每条至少 2 个真退出码）
- 标注映射自证：渲染器在出图前要求"非空行必须有墨迹、空行必须没有"，
  不成立即 `FATAL` 拒绝出图（实测拦下过折行、光标块、细字形三类假行）
- census 只读契约：`scripts/materials-coverage.sh` 前后 sha256 清单一致（它自带断言，exit=0）
- 沙盒数：`ls -d /home/user/workspace/gopi-sandbox/*materials-d-* | wc -l` → 59
  （历次调试累积，回收由 `gopi scratch-gc sandboxes` 负责，未在本任务里跑）

## 踩坑（给后来者）
1. **终端会替你折行**，而它的折法你看不见：行→像素的线性映射会整体漂一格，
   标注框"看起来正常"却框错地方。对策：自己按测得的列宽折行（`wrap_display`），
   并在出图前用"非空行必须有墨迹"自证映射。
2. **光标块会被量成一段墨迹**，`\033[?25l` 关掉它；**细字形行高不足**（`{`、`}`），
   按段数校验行数必然失败——改用标定格子 + 墨迹有无的判据。
3. **窗口底部的空白行不是"错位"**：裁剪要按窗口矩形，不要按墨迹矩形。
4. **`checkpoint resume` 在 stdout 不是 tty 时可能挂死**（详见 `004` 的 finding）；
   素材里两种写法都留了证据。
