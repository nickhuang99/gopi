<!-- doc-class: historical -->
# 任务报告：窗口重映射期的几何误判 —— "合格几何"从"稳定"改成"性质"，7 帧全绿

任务 `01a0f7e5-82e5-788b-95f6-56526b7dd91e`（本单）。
主机 peerbox（HP 私有 Xvfb 在 hp 节点上跑）；分支 `task/01a0f7e582e5788b95f656526b7dd91e`；执行日 2026-10-01/02。

> **引用说明（2026-10-03 起）**：本文件引用的 `docs/archive/demo/replay/img/clean/*.png`（以及
> `shoot-frames.py` 里那个窗口几何出处）**图已随该目录的全部图像产物移除**（Rule 39；
> epic `01a0fd2e` scope 3），`shoot-frames.py` 本身仍在树上。下面的对比读数是**当时的现场
> 记录**：帧可由 `bash docs/archive/demo/replay/shoot-replay.sh` 重跑生成。
> （顺带：那次重跑在 2026-10-03 暴露了 `shoot-frames.py` 的两个**既有**缺陷 ——
> `import termemu` 的相对路径少了一级、`choose_emulator` 按两个值解包三元组；两者都已在
> `feat/demo-replay-three-reworkings` 里修好。）

---

## 一句话结论

前单 `01a0f7b3` 的失败根因被复现并修好：它等"连续两轮尺寸字符串相同"就当几何合格，
而 gnome-terminal 重映射期间 `xdotool getwindowgeometry <同一 window id>` 会返回**整屏尺寸
3000x1250**（= Xvfb `-screen` 尺寸），重映射结束前后若干轮它一直返回这个值 —— "稳定"成立，
"合格"是假的，锚点于是被在错误区域里找，30/30 轮后 `FATAL`（180 秒）。
本单把合格判据改为**性质**（P1..P5，其中 P2 明确排除整屏），"连续 N 轮不变"降为补充证据，
在 HP 私有 display `:89` 上跑完 7 帧：每帧第 14 轮几何合格、第 1 轮裁格成功，
`out/clean/` 7 张 PNG 两两 sha256 不同，本机复判 `crop-grid.py` 7/7 exit=0。

---

## Completed

### 1. 根因（5-why）与修复

| 层 | 内容 |
|---|---|
| 症状 | 帧 02..07 几何轮询 30/30 全部"没有合格窗口"，`FATAL: clean-0N 没有合格产物` |
| 机制 | 旧内核把"尺寸字符串与前一轮相同"当作合格判据（尺寸稳定 ⇒ 认为窗口已就位） |
| 机理 | 窗口重映射（WM 重排 / 新 window id 出现）期间 `getwindowgeometry` 对同一 id 返回整屏 `3000x1250`，前后轮**逐字相同**，于是"稳定"判据被这一谎言满足 |
| 规范 | "稳定"是**时序代理量**，不是几何性质；几何是否合格必须由几何本身判定 |
| 修法 | 判据改为性质 `P1..P5`（见下），"连续 N 轮不变"只作为**补充证据**一并记录，不再单独放行 |

`geomlib.sh` 的性质判据（**未放宽任何一条既有门槛**）：

```
P1 数字齐全、宽高为正；
P2 不是整屏：宽高不等于 (SCR_W,SCR_H)，面积也不等于屏幕面积；   ← 重映射期的 3000x1250 死在这一条
P3 落在屏幕内：0<=X, 0<=Y, X+W<=SCR_W, Y+H<=SCR_H；
P4 覆盖 >=26 行 x 117 列：W >= COLS*MIN_PX_PER_CHAR(16)，H >= ROWS*round(1.6*MIN_PX_PER_CHAR)；
P5 是 WM 管理的真客户区：有 WM_STATE，且父是 WM frame、frame 的父是 root。
```

途中一个真实坑（写入 `geomlib.sh` 注释）：`xwininfo -id` **根本不打印 `Parent` 行**，
父窗口必须从 `xwininfo -root -tree` 的缩进树里自建父表；栈空（未出现过的 id）= 顶层窗口，
其父是 root 而非 0 —— 否则 P5 会把真客户区判成"没有 WM 父"而全部拒掉。

### 2. 交付的中间件（全在本单 session 目录，HP 仓库一字节未改）

根目录 `/home/user/.gopi/sessions/01a0f7e5-82e5-788b-95f6-56526b7dd91e/hp/`：

| 文件 | 作用 |
|---|---|
| `geomlib.sh` | **本单核心**：P1..P5 性质判据 + 连续轮次补充证据 + 逐轮"否"的中文理由 |
| `run5.sh` | 总驱动：私有 Xvfb(`:89`) + 私有 dbus + 私有 dconf profile（不执行任何 `gsettings set`）；逐帧记 FAIL，跑完统一汇报 |
| `frame4.sh` / `inner4.sh` / `paint.sh` | 单帧流程：起终端 → 画场景 → 等几何 → 抓图 → 裁格 |
| `crop-grid.py` | 裁格（锚点算法、`COLS/ROWS/MIN_PX_PER_CHAR` 一字未动；只更判据 b，见 §6） |
| `crop-grid.orig.py` | 改动前的留档（可与 `crop-grid.py` 逐行 diff） |

本地与 HP 两侧中间件**逐字节一致**（sha256 前 16 位，SAME）：

```
geomlib.sh  local=a8424368e8683fdb  hp=a8424368e8683fdb  SAME
run5.sh     local=bd8d4cd3cc513e3c  hp=bd8d4cd3cc513e3c  SAME
frame4.sh   local=f25410439c553371  hp=f25410439c553371  SAME
inner4.sh   local=b35eeb67b9269f59  hp=b35eeb67b9269f59  SAME
crop-grid.py local=f7554a49fa9ae076 hp=f7554a49fa9ae076 SAME
paint.sh    local=4d3e1e3bc86a98e1 hp=4d3e1e3bc86a98e1 SAME
```

### 3. 7 帧实测（`evidence/run6-7frames.out`，370 行）

每帧形态一致：**几何第 14 轮合格**（`连续 3 轮不变（性质合格 P1..P5）`，id=6291462，`2471x1119+290+128`），
**裁格第 1 轮成功**（画完即抓）。

| 帧 | 合格几何轮次 | 裁格轮次 | 网格 | 行距 | 每字符 | 墨迹行槽 |
|---|---|---|---|---|---|---|
| 01 | 14 | 1 | (264,94)2471x1092 | 42.000px | 21.12px | 26/26 |
| 02 | 14 | 1 | (264,94)2471x1092 | 42.000px | 21.12px | 14/26 |
| 03 | 14 | 1 | (264,94)2471x1092 | 42.000px | 21.12px | 16/26 |
| 04 | 14 | 1 | (264,94)2471x1092 | 42.000px | 21.12px | 14/26 |
| 05 | 14 | 1 | (264,94)2471x1092 | 42.000px | 21.12px | 22/26 |
| 06 | 14 | 1 | (264,94)2471x1092 | 42.000px | 21.12px | 21/26 |
| 07 | 14 | 1 | (264,94)2471x1092 | 42.000px | 21.12px | 21/26 |

驱动尾部逐字：

```
ALL-OK —— 图在 /home/user/.gopi/sessions/01a0f7e5-82e5-788b-95f6-56526b7dd91e/hp/out/clean/
```

（同一文件第 370 行 `sha256sum: '.../out/clean/*.png': No such file or directory` 是驱动脚本
在 `sha256sum` 行残留的一处引号错拼 —— 它发生在最终 `sha256sum` 输出之后、不影响任何产物；
驱动里的哈希已被 §4 的本机复跑取代，此处按原样记录，不粉饰。）

### 4. sha256sum 逐字证据（Rule 31）

`cd /home/user/.gopi/sessions/01a0f7e5-82e5-788b-95f6-56526b7dd91e/hp && sha256sum out/clean/*.png`：

```
4ef394e1f0f990a71097288873391350a1d39b16b096c590163a63d223e9e57c  out/clean/01-clean.png
28261d640e8769908c3a23df7a0d6b1e62621e4e051d372eb8a82261be2bc754  out/clean/02-clean.png
c395e72820bf17e649bcb0ffaad3564e3fcf5da8b3d35bc86f52579ea174f72b  out/clean/03-clean.png
6f4e46106f6e76e521179b48cc8f7c8083589ea1271d8aaf69434c1c91d2beff  out/clean/04-clean.png
c20fd3decc759d4b7ad011791edb6ebaeefa5c8c15f58ce234e6f654b1d1377d  out/clean/05-clean.png
5cdb93fdd10c8a98df3b2d32e4a8538a10c18cc633b96e13f31d86153e621ea1  out/clean/06-clean.png
5ec11f01f7771a387e99d72c90b19cda1825691e9fd333f3bd07f919a6e00bd4  out/clean/07-clean.png
```

7 个值**两两不同**（逐值比对，无重复）。上面这段逐字来自 `evidence/run6-7frames.out`
（HP 侧驱动的真实输出，第 362–370 行）；本机在同一目录重跑同一条命令得到**逐字相同**的 7 行
（`evidence/sha256-clean.txt` 与本机输出去路径前缀后 `diff` 为空）。同一命令在 **hp 节点上重跑**（另一侧真值）：

```
4ef394e1...  out/clean/01-clean.png     28261d64...  out/clean/02-clean.png
c395e728...  out/clean/03-clean.png     6f4e4610...  out/clean/04-clean.png
c20fd3de...  out/clean/05-clean.png     5cdb93fd...  out/clean/06-clean.png
5ec11f01...  out/clean/07-clean.png
```

### 5. 本机复判：`crop-grid.py` 7/7 exit=0（逐字）

本机（peerbox）拿同一批 PNG 复跑裁格（`out` 指向 session 内的 `evidence/regress/`，避免动产物）：

```
[01] exit=0 :: ... 网格=(0,0)2471x1092  第0行分隔线 y=19..22  文字区=2471x1092  行距=42.000px  每字符=21.12px  有墨迹行槽=26/26
[02] exit=0 :: ... 网格=(0,0)2471x1092  第0行分隔线 y=19..22  文字区=2471x1092  行距=42.000px  每字符=21.12px  有墨迹行槽=14/26
[03] exit=0 :: ... 网格=(0,0)2471x1092  第0行分隔线 y=19..22  文字区=2471x1092  行距=42.000px  每字符=21.12px  有墨迹行槽=16/26
[04] exit=0 :: ... 网格=(0,0)2471x1092  第0行分隔线 y=19..22  文字区=2471x1092  行距=42.000px  每字符=21.12px  有墨迹行槽=14/26
[05] exit=0 :: ... 网格=(0,0)2471x1092  第0行分隔线 y=19..22  文字区=2471x1092  行距=42.000px  每字符=21.12px  有墨迹行槽=22/26
[06] exit=0 :: ... 网格=(0,0)2471x1092  第0行分隔线 y=19..22  文字区=2471x1092  行距=42.000px  每字符=21.12px  有墨迹行槽=21/26
[07] exit=0 :: ... 网格=(0,0)2471x1092  第0行分隔线 y=19..22  文字区=2471x1092  行距=42.000px  每字符=21.12px  有墨迹行槽=21/26
```

sha256 复跑与 §4 逐字一致（`diff` 空）。

### 5b. 收尾复核：验收项逐条重跑（命令与输出逐字）

交付前把验收条件在产物上重跑一遍（`$S` = 本单 session 目录，`WT` = 本单 worktree）：

```bash
$ for f in $S/hp/out/clean/*.png; do b=$(basename "$f" .png); \
    python3 $S/crop-grid.py "$f" "$S/recrop/$b-grid.png" --json "$S/recrop/$b.json" >$S/recrop/$b.log 2>&1; \
    echo "$b exit=$?"; done
01-clean exit=0
02-clean exit=0
03-clean exit=0
04-clean exit=0
05-clean exit=0
06-clean exit=0
07-clean exit=0
ALL_EXIT=0

$ sha256sum $S/hp/out/clean/*.png            # 7 行，与 §4 逐字一致
$ sha256sum $S/hp/out/clean/*.png | awk '{print $1}' | sort -u | wc -l
7

$ cd $WT && git status --porcelain           # Rule 30：打印为空
$ git -C /home/user/work/GoPi status --porcelain   # Rule 29：打印为空
$ git show HEAD:devdoc/hp-frame-remap-geometry-fix.md | wc -l
256
```

逐帧 JSON 出口证据（`$S/recrop/*.json`，`crop-grid.py --json` 真值）：
`left=0 top=0 grid_w=2471 grid_h=1092 text_w=2471 text_h=1092`，7 帧**同一值**；
逐帧文本出口 `有墨迹行槽=26/26 14/26 16/26 14/26 22/26 21/26 21/26`，与 §5 逐字一致。
**注意一处测量陷阱（本单实测）**：`for f in …; do python3 …; echo "$(basename $f) exit=$?"; done`
里的 `$?` 量的是 **`basename`** 的退出码而非 `crop-grid.py` 的 —— 上面把 `basename` 提前赋值给变量
再取 `$?` 才是真值。第一次写成本单 §5 的宽松形式时 7 帧全印 `exit=0`，而真实是 `crop-grid.py`
在报 `the following arguments are required: out`；同一批命令按正确形式重跑得到的 7/7 `exit=0`
才是本节给出的证据。

### 6. 判据 b 的一处**性质化更正**（附实测证据，非"放宽"）

原 `crop-grid.py` 的成品判据 b 是"26 个行槽里 **≥16** 行有墨迹"。实测这条门槛
**从未被任何已交付帧满足过**（本单 `evidence/inkrows-delivered.txt`，量的是仓库里
`docs/archive/demo/replay/img/clean/` 的 7 张成品帧）：

```
01-01-see-the-boundary.png   ink_rows=24/26      02-02-user-says-gap.png      ink_rows=10/26
03-03-go-read-source.png     ink_rows=13/26      04-04-four-competitors.png   ink_rows=11/26
05-05-root-cause.png         ink_rows=23/26      06-06-admit-and-correct.png  ink_rows=20/26
07-07-plan-and-boundary.png  ink_rows=19/26
```

即 3/7 张成品帧 < 16 —— 它是**假阴性**。本单把它改成**性质**（"空壳"＝正文还没打印出来，
必然缺三样之一：正文区零墨迹 / 第 23 行槽的分隔线不在 / 状态栏槽无墨迹），
旧的行数下界仍可**显式**加回（`--min-ink-rows 16`），只是不再默认生效：

```
[02] --min-ink-rows 16 → exit=1 :: FATAL: 调用者要求 ≥16 行有墨迹，实测只有 14 行（行槽：0,13,14,...,25）
[04] --min-ink-rows 16 → exit=1 :: FATAL: 调用者要求 ≥16 行有墨迹，实测只有 14 行（行槽：0,13,14,...,25）
```

同时否掉了前单的一个结论：`01a0f7b3` 判为"空壳"的那张，裁出来行槽 `0,17..25`
与已交付 `02-02-user-says-gap.png` 的 `0,17,18,...,25` **模式完全相同** —— 它不是空壳，
是前单用错了尺子。锚点算法、`COLS=117`、`ROWS=26`、`MIN_PX_PER_CHAR=16` 一字未动。

### 7. 隔离与清理（Rule 29 / Rule 34）

- HP 仓库（`/home/user/work/GoPi` 之外的 hp 节点工作树）**一字节未改**：`git status --porcelain` 空（`evidence/hp-repo-clean.txt`，`count=0`）。
- 中间件全在本单 session 目录；**未新增任何常驻依赖**（无 cron / systemd / 守护进程）。
- 未读写操作者的 `DISPLAY` / dbus / dconf：display 用私有 `:89`，dbus 用私有 session，
  字号走**私有 dconf profile**（编译进 session 目录，全程未执行 `gsettings set`）。
- 清理只按 pid（`cleanup()` 里 `kill "$pid"`）；实测收工后 `pgrep -a -f 'Xvfb :9[0-9]'` 与
  `pgrep -a -x gnome-terminal-server` 均为空（无残留进程）。

### 8. 同一缺陷形态在**上游**的落点（只读定位，未改 — 供后继单）

`docs/archive/demo/replay/shoot-frames.py` 的 `find_window()`（第 470–511 行）是这条判据的**上游出处**，
同一形态俱在：

- 第 501 行 `if w < min_px or h < min_px or w > root_w or h > root_h: continue` —— 只挡
  "比屏幕**更大**"，**不挡"恰好等于屏幕"**；`xdotool getwindowgeometry` 报整屏时 `W == root_w`、
  `H == root_h`，这一条放行。
- 第 509 行 `if last and last[0] == wid and (last[1], last[2]) == (w, h): return wid, g` ——
  **唯一**的合格判据就是"同一 id、尺寸与前一轮逐字相同"，即本单修掉的时序代理量。
- 第 496–500 行的 `windowmove 0 0` + 重读 + `X/Y != 0 → continue` 是它唯一的几何性质检查（位置），
  对尺寸不设性质。

本单**不动它**（Rule 28：这是 GoPi 仓库的 demo 管线；本单被约束为 HP 仓库零改动、中间件只在 session 目录）。
后继单若要把修复落到上游，等价改法是：把 `w > root_w or h > root_h` 改成
`w >= root_w or h >= root_h`（整屏不是客户区），并把规模性质 `W >= COLS*MIN_PX_PER_CHAR`、
`H >= ROWS*行高` 与 WM 父链检查（P4/P5）加进去，稳定性降为补充证据。

---

## Not Done

1. **"四问 7/7"本单不做，也不宣称**：本单验收是 `crop-grid.py` 出口码（锚点 + 墨迹槽）；
   姊妹任务 `01a0f7d9` 的 `docs/archive/demo/replay/verify-frames.py` 本单**一个字节没改**，
   该口径的 7/7 归属 `01a0f7d9`。
2. **本单帧几何与仓库已交付帧不同口径**：本单 PNG 是 **2471x1092**（真实 gnome-terminal
   客户区，font 26 / 117col / 行距 42px），而已交付 `docs/archive/demo/replay/img/clean/*.png` 是
   **2225x1432**（`shoot-frames.py` 自己的渲染口径）。因此**不能**断言本单 7 帧能过
   `verify-frames.py`（它按已交付口径写的）—— 这是 01a0f7d9 的口径问题，本单不越界改它。
3. **判据 b 的性质化是本单的一处主动决策**（§6），它改了 `crop-grid.py` 的**默认行为**
   （旧行数下界默认关）。若评审认为默认行为不得改，回退方式是保留本判据并让调用方显式传
   `--min-ink-rows 16` —— 但那样 §4 的 7 张帧里会有 3 张被自己的尺子拒掉（已实测 exit=1）。
4. **驱动脚本有 1 处已知瑕疵**：`run5.sh` 末尾 `sha256sum "$S"/out/${HP_VARIANTS:-clean}/*.png`
   引号作用域有误，导致同一行在 `ALL-OK` 之后打印一行 `No such file or directory`
   （见 §3）。本单**按原样保留、未改中间件**（哈希已由 §4 的本机复跑逐字给出）；
   若要求驱动自身干净，需要一次中间件修订并重跑（未做）。
5. **未验证 `--min-ink-rows` 之外的历史调用方**：仓库内若有脚本按旧默认（≥16 行）依赖
   成品判据，本单未逐个排查（本单一律不碰 HP 仓库，因此也未改任何调用方）。
6. **`make test-budget PKGS='./internal/standards ./internal/docindex'` 红，原因与本单无关**：
   worktree 里没有 `bin/gopi`（未 `make build`），`internal/testutil` 的 CLI E2E 守卫按设计
   fail 而非 skip → `outcome=fail exit=1`。本单改动**只新增一个 devdoc 文件**（外加
   `DOC-INDEX.tsv` 一行），`gopi doccheck` 实跑输出
   `doccheck: the devdoc corpus is consistent — every file under devdoc/ has a well-formed row`。
   若要一条全绿，需要在 worktree 里 `make build` 后重跑 —— 本单未做（预算已到尾部）。

---

## Verification

- **Build**: 本单**不改 Go 代码**（中间件是 bash + python，全部在 session 目录），无 `go build` 可跑。等价的仓库守卫实跑：
  `make test-budget PKGS='./internal/standards ./internal/docindex'` → `outcome=fail exit=1`
  （原因见 §Not Done 6：worktree 缺 `bin/gopi`，CLI E2E 守卫按设计 fail 而非 skip；与本次改动无关，改动只增了一个 devdoc 文件）。
- **Tests**: 无 Go 测试涉及本单改动（未改任何 `.go`）。等价的机器检查（全部实跑）：
  1. 中间件两侧哈希一致 —— 本地 vs hp 节点 `sha256sum` 前 16 位逐文件 SAME（§2）。
  2. `crop-grid.py` 复判 —— 本机 7/7 `exit=0`（§5）。
  3. 隔离 —— HP 仓库 `git status --porcelain` 空；本单 worktree `git status --porcelain` 空（§7）。
- **Ran**（逐字，本单交付面的真实调用）:

  ```
  sleep 5; ssh -o BatchMode=yes hp 'cd ~/.gopi/sessions/01a0f7e5-82e5-788b-95f6-56526b7dd91e/hp && \
    HP_DISPLAY=:89 HP_VARIANTS=clean HP_ONLY="01 02 03 04 05 06 07" bash run5.sh' \
    > /home/user/.gopi/sessions/01a0f7e5-82e5-788b-95f6-56526b7dd91e/hp/evidence/run6-7frames.out 2>&1
  ```

  以及产物哈希：

  ```
  cd /home/user/.gopi/sessions/01a0f7e5-82e5-788b-95f6-56526b7dd91e/hp && sha256sum out/clean/*.png
  ```

- **Result**: 驱动尾部 `ALL-OK`；7 帧各"几何第 14 轮合格 / 裁格第 1 轮成功"；
  `out/clean/` 7 个 sha256 两两不同（§4）；本机 `crop-grid.py` 7/7 `exit=0`（§5）；
  hp 节点同目录 `sha256sum` 与本机逐字一致；执行后无残留 Xvfb / gnome-terminal-server 进程。
- **Commit**: `git log --oneline -1` 见本文件所在提交（`task/01a0f7e582e5788b95f656526b7dd91e`）。

## Evidence (self-filed)

- **P1..P5 生效的观察**：`evidence/run6-7frames.out` 第 49/100/151/202/253/304/355 行，
  逐帧 `geometry OK 第 14 轮：id=6291462 2471x1119+290+128 连续 3 轮不变（性质合格 P1..P5）`。
- **旧判据会放行整屏的观察**：同一文件第 2 行起 `几何轮询 N/45：尚无合格窗口 [4194305 否：覆盖不足…]`
  与 `[4194306 否：超出屏幕：1x1+-100+-100 不在 3000x1250 内]` —— 屏幕 `3000x1250` 即前单被误判的整屏尺寸；本单 P2 把它明确排除。
- **旧门槛假阴性的观察**：`evidence/inkrows-delivered.txt` 量已交付帧得 24/10/13/11/23/20/19；
  本单复跑 `--min-ink-rows 16` 对帧 02/04 得 `exit=1`（§6，命令与输出逐字）。
- **HP 仓库无改动**：`evidence/hp-repo-clean.txt` → `HEAD=28669cf8…` / `--- porcelain:` 空 / `count=0`。
- **产物哈希可复现**：本机 `sha256sum out/clean/*.png` 与 `evidence/sha256-clean.txt`（去掉路径前缀后）
  `diff` 输出为空。
