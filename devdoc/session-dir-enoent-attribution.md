<!-- doc-class: historical -->
# “session 目录中途消失又回来” — 归因：一次拼写，不是一次文件系统事件

**日期**: 2026-10-17 · **host**: hosta · **task**: 01a1453a954871d584bf6d8a9c13d7e5
**前序行**: 01a143696d247e66abe1dda74a3fc9fd（该 run 自身报的 Rule 41 异常）
**代码**: 无生产代码改动；新增回归测试 `internal/tools/task_session_near_miss_test.go`

## 判词（verdict）

**已归因，且是确定性的。** 失败的那条路径不是 canonical session 目录：它是**同一个 uuid 掉了第 4 个连字符
的 35 字符拼写**。它指向的条目**从来就不存在**，所以 ENOENT 是普通的“无此条目”，不是瞬时故障；
6 秒后“恢复”是因为**下一条命令换了拼写**（36 字符 canonical），不是目录回来了。

一句话：**没有东西消失过 —— 是问错了名字，然后再问了一遍对的名字。**

## 实测证据

### 1. 失败操作数是 35 字符（掉 1 个连字符），不是 36

从该 run 自己的 raw log 里按 `tool_use` id 取回两次失败的原始命令，把命令里的路径字面量与磁盘上
真实的目录名做逐字符比对：

```
$ python3  # 从 raw.log 取 tool_use.input.command，与 os.listdir 的真实名 difflib 比对
DISK  repr='01a14369-6d24-7e66-abe1-dda74a3fc9fd' len=36

### FAIL t+863 call_00_Ck28JxR0EjUnz6DcuFgL7246
   name repr='01a14369-6d24-7e66-abe1dda74a3fc9fd' len=35
   equal-to-disk: False
     delete   disk[23:24]='-'  ->  call[23:23]=''
### FAIL t+868 call_00_nylkWvWnzgfsNxQvC0eT6405
   name repr='01a14369-6d24-7e66-abe1dda74a3fc9fd' len=35
   equal-to-disk: False
     delete   disk[23:24]='-'  ->  call[23:23]=''
### OK   t+882 call_00_QWY7vlxUycNNxxI4lzq62371
   name repr='01a14369-6d24-7e66-abe1dda74a3fc9fd' len=36
   equal-to-disk: True
### OK   t+1107(path arg) call_01_wki7pckPCPVjhwmE4vfI7094
   name repr='01a14369-6d24-7e66-abe1-dda74a3fc9fd' len=36
   equal-to-disk: True
```

失败的三条 `ls`/`wc` 报错回显的也正是那个 35 字符名（不是被截断的显示）：

```
$ # 从同一条 tool_result 回显里提取路径字面量
  ECHOED name len=35 '01a14369-6d24-7e66-abe1dda74a3fc9fd'  hyphens=3
  LINE: ls: cannot access '/home/user/.gopi/sessions/01a14369-6d24-7e66-abe1dda74a3fc9fd': No such file or directory
  ECHOED name len=35 '01a14369-6d24-7e66-abe1dda74a3fc9fd'  hyphens=3
  LINE: wc: /home/user/.gopi/sessions/01a14369-6d24-7e66-abe1dda74a3fc9fd/task-report.md: No such file or directory
```

briefing 里转述的那条 `read failed: open …` 也是 35 字符：

```
$ grep -o 'read failed: open [^ ]*' d3ccef7e-*.raw.log | sort -u
read failed: open /home/user/.gopi/sessions/01a14369-6d24-7e66-abe1dda74a3fc9fd/task-report.md:
read failed: open /home/user/.gopi/sessions/01a14369-6d24-7e66-abe1-dda74a3fc9fd/task-report.md:
read failed: open $S/task-report.md:                       ← 变量写法，展开后指向上面那个值
```

### 2. 系统自己写下的路径是 36 字符 —— 没有生产者播撒 35 字符拼写

这是“是不是某个 gopi 组件生成了错拼写”的判据。系统写入的 `metadata.json`：

```
$ python3 -c "import json;d=json.load(open('metadata.json'));[print(k,'=',v) for k,v in d.items() if isinstance(v,str) and 'sessions' in v]"
raw_log_path = /home/user/.gopi/sessions/01a14369-6d24-7e66-abe1dda74a3fc9fd/d3ccef7e-…raw.log
```

`GOPI_TASK_REPORT_PATH` 本身也只带 36 字符拼写（本 run 实测同一渲染器）：

```
$ echo "REPORT=$GOPI_TASK_REPORT_PATH"
REPORT=/home/user/.gopi/sessions/01a1453a-9548-71d5-84bf-6d8a9c13d7e5/task-report.md
   → base 名长度 36（canonical）
```

而 35 字符拼写在系统产物里的每一处出现都可追到**模型的命令/推理文本**：

```
$ for f in output.log metadata.json; do printf '%-14s BAD=%-4s GOOD=%s\n' "$f" "$(grep -o $BAD "$f"|wc -l)" "$(grep -o $GOOD "$f"|wc -l)"; done
output.log     BAD=2    GOOD=22
metadata.json  BAD=0    GOOD=2
$ grep -n "$BAD" output.log
1702:Actually — wait. This run is a NEW session? The session dir 01a14369-6d24-7e66-abe1dda74a3fc9fd may be the PREVIOUS run's session …
```

`output.log` 里那 2 处都在模型自己的思考文本里；`metadata.json` 一处都没有。
**推断（inference，非实测）**：这 1 个连字符是在模型侧生成命令时掉的（拼写漂移），不是任何 gopi
组件产出的 —— 证据是“系统写入的每一处都 canonical，错拼写的每一处都可追到生成文本”。

### 3. 目录确实从未被删除或重建（btime 落在起始时刻）

```
$ stat -c '%n btime=%w ino=%i nlink=%h mtime=%y' /home/user/.gopi/sessions/01a14369-6d24-7e66-abe1-dda74a3fc9fd
…/01a14369-6d24-7e66-abe1-dda74a3fc9fd btime=2026-10-16 14:32:25.123941700 +0800 ino=3589930 nlink=5 mtime=2026-10-16 23:08:02.312074293

$ stat -c '%n btime=%w' …/metadata.json …/attachments
…/metadata.json  btime=2026-10-16 14:32:25.123941700 +0800
…/attachments    btime=2026-10-16 14:32:25.123941700 +0800
```

btime（inode birth time）等于提交时刻、与子项一致 ⇒ 这个 inode 是**创建后一直存活的那一个**，
不是删除后重建的。对照组（`/home/user/.gopi/environment.md`）证明本机 `stat -w` 把 btime 报得对
（与 mtime 不同的旧文件也报得准）。**所以“删除/重建”被排除，mtime 那条证据现在有了 btime 支撑。**

### 4. 环境侧候选全部测过并被排除

```
$ journalctl -k --since '2026-10-16 22:50' --until '2026-10-16 23:02'   # 事故窗口
Oct 16 22:50:00 … nvme 10000:e2:00.0: PCIe Bus Error: severity=Correctable, …
$ journalctl -k … | grep -c 'severity=Correctable'          # 窗口内
4558
$ journalctl -k … | grep -c 'severity=Uncorrectable'
0
$ journalctl -k --since '2026-10-16 00:00' | grep -icE 'nvme.*(reset|timeout|abort)'
0
$ journalctl -k --since '2026-10-01' | grep -iE 'EXT4-fs error|I/O error|buffer I/O'
（无输出 —— 只有 04:17 的 Remount Root 服务行）
```

- 本机 `6.8.0-138-generic`，`/dev/nvme0n1p2` ext4 `rw,relatime,errors=remount-ro`；
  `/home/user/.gopi/sessions` 各层父目录均为真目录（`stat -c %F`），无 symlink、无 FUSE/overlay。
- PCIe **Correctable** AER 在本窗口确实在刷（4558 条），但：**0 条 Uncorrectable、0 次 nvme reset/timeout、
  0 条 ext4 错误**。AER 基线也说明它与本现象无因果：10/10–10/15 每天 0 条，10/16 起 38966 条/天，
  **至今仍在刷（实测 60 秒 89 条）**，而 canonical 路径一直正常解析。**推断（inference）**：AER 是这台
  机器的一个独立的、以分钟为粒度持续存在的硬件告警，不是一次 6 秒窗口事件；把它当根因无法解释
  “只失败了一个 35 字符的名字”。

### 5. 代码侧取证：谁动过 `<GopiDir>/sessions/<id>`

生产代码里能**让一条 session 路径不再解析**的写入者，穷举如下（`-g '!*_test.go'`）：

| 位置 | 动作 | 触发条件 | 对本现象 |
|---|---|---|---|
| `internal/tools/task_session.go:191` | `os.RemoveAll(legacy)` | `task-sessions/<id>` 旧根存在且新位置已有 | **不可能**：本机 `~/.gopi/task-sessions` 不存在（实测 `ls` → No such file） |
| `internal/tools/task_session.go:197` | `os.Rename(legacy, dst)` | 旧根存在、新位置不存在 | 同上；且它只会**产生**目录，不会让 canonical 名消失 |
| `internal/tools/write.go:314`、`internal/uiclient/dump.go:1146` 等 | `os.Rename(tmp, path)` | 原子替换**单个文件** | 只替换文件节点，父目录条目不变 ⇒ 不会让目录 lookup ENOENT |
| `internal/scratch/*`、`cmd/agent/*` | `os.RemoveAll` | 各自只删**自己建的** scratch/sandbox 树 | 没有一处以 sessions 根或 row 容器为目标 |

关键的否定取证：

```
$ rg -n 'RemoveAll\(.*(GopiDir|SessionsRoot|sessionsDir|SessionsDirName)' --glob '*.go' -g '!*_test.go'
（生产代码 0 命中；唯一命中是一个 _test.go 的 fixture 清理）
```

**sessions 根本身没有任何生产写入者会删除它**；`sessions/<id>` 也没有。这与 §3 的 btime 一致。

### 6. 为什么 ENOENT 是这个形状（而不是“目录消失”）

`~/.gopi/sessions` 现有 2138 个条目（`nlink=2139`，目录体 135168 字节 ≈ 16 个 4K 块，h-tree 目录），
目标 inode `nlink=5`（`.` + 父项 + attachments/e2e/evidence）。**推断（inference）**：一个不存在的
名字在这棵树里 lookup 失败，返回的就是 `ENOENT`（"No such file or directory"），与“存在的目录被删
了一瞬”在用户态**无法区分** —— 这正是本现象被误读成文件系统故障的原因，也是本案必须靠
**操作数逐字符比对**而不是靠时序推断的原因。

## 回归测试（可复用的确定性仪器）

`internal/tools/task_session_near_miss_test.go` —— 不需要活 run、不需要设备负载、不需要碰运气：

1. 造出 canonical 容器与其中的 `task-report.md`；
2. 在同一瞬间测：canonical 路径 `stat` 成功，去掉第 4 个连字符的拼写 **`stat` → `os.ErrNotExist`**；
3. `sessionid.CanonicalSpellingOf(nearMiss)` 必须回出 canonical 名（修复提示）；
4. `taskSessionDir(nearMiss)` 必须**拒绝**并且**不留目录**（不许在旁边造第二个容器）。

实测（绿 → 红 → 绿）：

```
$ go test ./internal/tools/ -count=1 -run TestNearMissSessionSpellingIsENOENTBesideTheCanonicalContainer
ok  	github.com/nick/gopi/internal/tools	1.290s

# RED-A：把“掉一个连字符”改成“不掉” —— 夹具前提被抓住
--- FAIL: … task_session_near_miss_test.go:80: near-miss fixture must drop exactly 1 character (dropped 0)

# RED-B：把被 stat 的对象换成 canonical 路径 —— 核心断言被抓住
--- FAIL: … task_session_near_miss_test.go:90: the near-miss spelling must be ENOENT (that IS the incident); got err=<nil>

# 还原后
ok  	github.com/nick/gopi/internal/tools	1.280s
md5 与备份一致：9b005cdefa292b5a72a1b96dd50118ac
```

## 下次发生时怎么抓（若真出现“同一拼写”的 ENOENT）

本仪器只在**拼写不同**时成立。如果哪天同一个 canonical 拼写真的 ENOENT 了，那次就是**另一个**
缺陷，取证顺序（全部只读，不新增常驻依赖，Rule 35）：

```bash
P=<那条报 ENOENT 的完整路径>; D=$(dirname "$P"); B=$(basename "$P")
# ① 操作数是不是真的等于父目录里的条目名（逐字节）
ls -b -1 "$D" | grep -F -- "$B" || echo "OPERAND NOT IN PARENT LISTING → 拼写问题，不是文件系统"
printf '%s' "$B" | wc -c                     # 与 ls 里的那个名字比长度
# ② 父目录里是不是有“差一个字符”的孪生（就是本案）
ls -1 "$D" | awk -v b="$B" 'length($0)!=length(b) && index($0,substr(b,1,8))==1'
# ③ 目录 inode 的 btime：判断有没有被重建
stat -c '%n ino=%i btime=%w nlink=%h' "$D"
# ④ 窗口内的内核侧证据（本机 AER 常态刷屏，要按 severity 过滤）
journalctl -k --since '-2 min' | grep -vE 'severity=Correctable'
```

**捕获什么**：① 立刻把“拼写错”与“文件系统错”分开（本案会被 ① 直接判掉）；② 指出孪生名；
③ 用 btime 区分删除重建；④ 在 AER 常态刷屏里只捞**非-Correctable** 的行。

## 被排除项 / 仍未归因项

**已排除（每条都有实测）**：删除或重建（btime + nlink）；FUSE/overlay/符号链接（`stat -f`、逐层
`stat %F`、`find -type l` 空）；legacy `task-sessions` 迁移（该根不存在）；nvme 掉盘/超时（0 条）；
ext4 报错（0 条）；`GOPI_TASK_REPORT_PATH` 渲染错拼写（本 run 与 metadata.json 均为 36 字符）。

**仍未归因（如实）**：**这一处连字符为什么在模型侧掉了**。已测的边界是：系统产物全部 canonical、
错拼写只出现在生成文本里；但“生成为什么漂移”属于模型输出本身的可靠性，本 run 无法用仓库内证据
进一步区分（可能是长路径重复书写时的复制误差，也可能是上下文里先出现过一次 35 字符再被复用 —— 
两者我都没有可实测的判据）。**本条不影响判词**：归因落在“路径拼写 ≠ 目录名”这一确定事实上，
repro 与仪器都不依赖“为什么漂”。

**关于 memory #11040 的更正**：那条记忆说的是“dash-less（32 hex 无连字符）拼写中途停止解析”，
本案失败的是 **hyphenated 但少一个连字符**（35 字符）—— 两者是**同一族缺陷的不同成员**
（“uuid 的 16 字节 + 错的连字符分组”），但**表现机制不同**：dash-less 那次是“从来没被写过的另一个
目录名”，本案是“同一分组法掉了一个连字符”。照搬#11040 会把本案误判成 dash-less 类。

## 交付物声明（Rule 6d）

- 分支 `task/01a1453a954871d584bf6d8a9c13d7e5`（worktree `/home/user/workspace/gopi-01a1453a954871d584bf6d8a9c13d7e5`，共享主仓库对象库，提交即在主仓库 ref 上，无需 push）。
- 取回：`git show task/01a1453a954871d584bf6d8a9c13d7e5:devdoc/session-dir-enoent-attribution.md`
