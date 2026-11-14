<!-- doc-class: historical -->
# Task Report: slash 补全菜单在窄终端上丢失第一行(选中行)

**Task:** `01a0f253-4c9c-75c5-9748-c2954eb2f63f`
**Branch:** `task/01a0f2534c9c75c59748c2954eb2f63f`
**Commit:** `66811ae8 fix(lineedit): the menu's room is measured WITH the menu in the plan, not without it`

## Completed

### 1. 根因定位(5-why)

**症状**:117x26 真实 pty 上 `gopi chat --tools` 输入 `/`,菜单第一行(默认选中行)不可见。

**5-why 链路**

1. **为什么选中行不可见?** 因为「排版给菜单预留的行」与「实际画出的行」可以不一致 —— 一旦不一致,整体可见性没有任何断言兜底(本仓库 `internal/lineedit` 里此前**没有**任何测试断言"菜单行必须在状态行之上")。
2. **为什么预留与实画会不一致?** 编辑器用 `region.MaxMenuRows(editorRows)` 决定菜单页长(`editor.clampedMenu` → `Menu.WithPage`),这个数**就是**预留的行数。
3. **为什么这个数是错的?** `MaxMenuRows` 用 `computePlan(rows, editorRows, 0)` —— **计划里没有菜单**。
4. **为什么"计划里没有菜单"就错?** `computePlan` 第 4 步把**剩余的所有行给 band(残差)**:菜单不在计划里 → 它的行归了 band → `MenuTop` 落在 `StatusRow` 上 → `StatusRow - MenuTop == 0` —— **在任何终端高度都是 0**。
5. **为什么 0 反而"看起来没事"?** 返回 0 让 clamp 变成 `WithPage(0)`;`Rows()` 里 `pageSize > 0` 的判断使 0 被当作"未指定",页长不受影响 —— **clamp 静默失效**,菜单保留整页。

**实测**(探针,3 种高度 × 5 种编辑行高):

```
rows=26 ed=12 plan(menu=0): MenuTop=25 StatusRow-MenuTop=0 → MaxMenuRows=0
rows=30 ed=12 plan(menu=0): MenuTop=29 StatusRow-MenuTop=0 → MaxMenuRows=0
rows=50 ed=12 plan(menu=0): MenuTop=49 StatusRow-MenuTop=0 → MaxMenuRows=0
```

**后果的真实 pty 证据**(117x8,生产 5 条 `/` 命令,`out/small8-117x8-small8-menuopen.grid.txt`):

```
 2|you> /
 3|> /log (Usage: /log <subcommand> [args])
 4|  /new (alias of /clear)
 5|  /task (Usage: /task <subcommand> [args])
 6|  /quit (alias of /exit)
 7|  /exit (alias of /quit)      ← 第 7 行是排版的 status row,状态行整条被吃掉
```

### 2. 鉴别实验(真实 pty,同一脚本同一尺寸)

`devdoc/evidence/sgr-grid/termgrid.py` 驱动真实 pty,二进制是**唯一变量**。

| 实验 | 几何 | 结果 |
|---|---|---|
| 窄终端 **无** turn 输出 | 117x26 | 菜单 row 20..24 完整,选中行可见 |
| 窄终端 **有** turn 输出 | 117x26 | 同上(多次开合、turn 进行中开菜单,均完整) |
| 高终端 | 117x30 | 同上完整 |
| **高度不足**(room < 页长) | 117x8 / 117x10 | **缺失复现** —— 菜单画到 status row 上 |

**结论**:缺陷**不在**滚动区与绝对定位的交互(前 3 组都正常),而在**页长 clamp 失效** —— 只有当"菜单要的行数 > 排版能给的房间"时才暴露。117x26 恰好有房间(room=5=页长),所以原始报告里的"row 20 全空"我在 117x26 上**未能复现**;可复现的形态是同一根因在 room < 页长 时的表现(已抓到)。

**诚实交代**:操作者原始 dump(session `a09b91ae`)的 row 20 是空的,但同一 dump 的 row 19 是 `▌ 你说得对 —— ...`(band 文本)而非 `you> /`;即那份 dump 的行号与报告中的引文**不对应**。该 dump 已随会话滚走(其 418MB raw.log 中不可恢复),我无法把它的 row 20 归到本根因上。**本报告不声称复现了那一次的具体画面**。

### 3. 代码修复

`internal/lineedit/region.go` —— 一处只读访问器的**同一套算术**改正(25 增 8 删,全部是注释与一个 return 表达式):

```go
// 改前:没有菜单的计划,房间恒为 0
p := computePlan(rows, editorRows, 0)
return p.StatusRow - p.MenuTop
// 改后:把菜单自己的上限放进计划,第 3 步给出真实房间
return computePlan(rows, editorRows, menuRowsCap).MenuRows
```

同时把 `MenuCapacity()` 的注释改成它真实的身份(**诊断用**,不是 sizing 入口——它在菜单发布前同样返回 0)。无新增常数、无新增依赖、无新工具。

### 4. 回归测试(red-proof)

`internal/lineedit/menu_selection_visible_test.go` —— 在 **117x8** 的**渲染 cell grid** 上断言两件事:

1. 菜单**第一个被画出的行 == 排版自己的 `MenuTop`**,且该行提供候选(选中行永远可见);
2. **没有任何菜单行落到 status row 上**(预留与实画不得分歧)。

用满页词表(`completionPageRows` 条)—— 页长必须**等于容量**才碰得到这个 clamp(候选更少时页总能放下,这正是本缺陷长期没被测到的原因)。

- **旧代码**:`--- FAIL: the menu is painted through row 7, and the layout's status row is 7 — the menu covers the status line (painted 5 rows, the room holds 0)`
- **新代码**:`ok github.com/nick/gopi/internal/lineedit`

## Not Done

- **操作者原始画面的逐字复现未取得**(见上"诚实交代"):那份 dump 已滚走。交付的是同一根因的可复现形态 + 修复。
- **修复的形态是"缩小页长"**(把 `WithPage` 从失效恢复为生效),不是"整块上移菜单"。菜单行数超过房间时,现在会按窗口跟随选区收缩(既有机制)。若期望的是另一种行为,需要新的 issue。
- `go test ./internal/lineedit/` 有**一个先于本改动就失败**的用例 `TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow`(已在改前的 `HEAD` 版本 region.go 上验证同样失败,与本 diff 无关),未修。

## Verification

- **Build:** `go build ./...` → OK(退出码 0)
- **Tests:**
  - `go test ./internal/lineedit/ -run TestA7_MenuStaysInsideTheRoomAndShowsItsSelectedRow` → 新代码 PASS / 旧算术 FAIL(red-proof 已记录)
  - `go test ./internal/lineedit/ -run 'TestA7|TestA5B|TestChat'` → `ok github.com/nick/gopi/internal/lineedit 1.211s`
  - `go test ./cmd/agent/ -run 'TestChatCompletion|TestChatPanel|TestChatDump'` → `ok github.com/nick/gopi/cmd/agent 1.523s`
  - `go test ./internal/lineedit/`(全包)→ 27.9s,仅 `TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow` 失败;**该失败在未改动的 HEAD 上同样失败**(已用 `git show HEAD:internal/lineedit/region.go` 复跑确认)
- **Ran:**(Rule 31,真实 pty,同一脚本同一尺寸,二进制为唯一变量)
  ```
  ./shots.sh pre  /dev/shm/gopi-prefix/gopi 117 8 script-cold.txt   # 修复前二进制
  ./shots.sh fix8 /dev/shm/gopi-fixed/gopi  117 8 script-cold.txt   # 修复后二进制
  # script: waitfor 'gopi chat' / type / / shot menuopen / key down / shot menudown / key esc
  # 均由 devdoc/evidence/sgr-grid/termgrid.py 驱动真实 pty(117x8, TIOCSCTTY/TIOCSWINSZ)
  ```
- **Result:**
  - **修复前** 117x8 `menuopen`:菜单画在 row **3..7**,row 7 是排版的 status row → 状态行整条消失
  - **修复后** 117x8 `menuopen`:菜单画在 row **3..6**,row 7 恢复为状态行 `deepseek-flash · iter 0 · no tool … ready`;`key down` 后 `>` 标记从 row 3 移到 row 4(选区仍然可见、可移动)
  - 117x26(报告中的几何)修复前后均完整,菜单 row 20..24,无回归
- **Commit:** `66811ae8`

## Evidence (self-filed)

- 根因探针(改前算术,`internal/lineedit/` 内临时测试,已删除):
  `rows=26/30/50, ed=1/2/3/12` 全部 `plan(menu=0): StatusRow-MenuTop=0` → 命令与输出见上「实测」。
- red-proof:`go test ./internal/lineedit/ -run TestA7_MenuStaysInsideTheRoomAndShowsItsSelectedRow` 在旧 return 表达式下输出
  `--- FAIL: … the menu is painted through row 7, and the layout's status row is 7 — the menu covers the status line (painted 5 rows, the room holds 0)`;恢复后 `ok`。
- 既有失败归因:`go test ./internal/lineedit/ -run TestTurnHeaderLines_BlockRowsAreCommittedOnePerRow` 在 `git show HEAD:internal/lineedit/region.go` 恢复成的**未改动**源码上同样 FAIL。
- 真实 pty 前后对比 grid(会话证据目录 `/home/user/.gopi/sessions/01a0f253-4c9c-75c5-9748-c2954eb2f63f/out/`):
  - `small8-117x8-small8-menuopen.grid.txt`(改前,菜单覆盖 row 7)
  - `fix8-117x8-fix8-menuopen.grid.txt` / `fix8-117x8-fix8-menudown.grid.txt`(改后,菜单止于 row 6)
  - `small10-117x10-*`(改前,同形态)、`apd-117x26-*`、`cyc-117x26-cyc-k01..k30-*`(117x26 三组鉴别实验 + 30 次开合)
- `git status --porcelain` 退出后为空(worktree clean,Rule 30)。
