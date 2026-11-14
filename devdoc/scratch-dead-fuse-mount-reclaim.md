<!-- doc-class: normative -->
# 死掉的 gvfs FUSE 挂载把 scratch 子树钉死 — 判定、根因、回收路径

**日期**：2026-10-15  |  **宿主**：peerbox  |  **任务**：`01a13db4-6156-71b4-8789-69df654b0a08`

---

## 1. 现象与判定（实测，非推断）

scratch GC 每 tick 重复报三行同一形状的失败，且这三条 entry 从来没有被回收过：

```
scratch: GC could not remove /home/user/workspace/gopi-test-scratch/queue/queue-replay-4007787: openfdat .../queue-replay-4007787/runtime/gvfs: transport endpoint is not connected
scratch: GC could not remove /home/user/workspace/gopi-test-scratch/queue/queue-replay-e2e-1326956: openfdat .../queue-replay-e2e-1326956/scratch-chat1/runtime/gvfs: transport endpoint is not connected
scratch: GC could not remove /home/user/workspace/gopi-test-scratch/queue/queue-session-operand-test-4014544: openfdat .../queue-session-operand-test-4014544/scratch/runtime/gvfs: transport endpoint is not connected
```

**判定：这三条路径确实是 FUSE 挂载点，不是普通目录。** 挂载表原文（`/proc/self/mountinfo`，
本 run 读取，与 `/proc/self/mounts` 同时取证）：

```
936 156 0:64 / /home/user/workspace/gopi-test-scratch/queue/queue-replay-4007787/runtime/gvfs rw,nosuid,nodev,relatime shared:938 - fuse.gvfsd-fuse gvfsd-fuse rw,user_id=1000,group_id=1000
1153 156 0:66 / /home/user/workspace/gopi-test-scratch/queue/queue-session-operand-test-4014544/scratch/runtime/gvfs rw,nosuid,nodev,relatime shared:958 - fuse.gvfsd-fuse gvfsd-fuse rw,user_id=1000,group_id=1000
1766 156 0:68 / /home/user/workspace/gopi-test-scratch/queue/queue-replay-e2e-1326956/scratch-chat1/runtime/gvfs rw,nosuid,nodev,relatime shared:1009 - fuse.gvfsd-fuse gvfsd-fuse rw,user_id=1000,group_id=1000
```

三行的文件系统类型都是 `fuse.gvfsd-fuse`，且都在被清扫的 queue 子树**内部**（一层或两层之下）。

**守护进程已死（ENOTCONN 的来源）**：`pgrep -af gvfsd-fuse` 列出五个 daemon，
其中**只有两个**的挂载目标是 scratch 树内的路径（pid 1080553 → `queue-replay-1080449`、
2438498 → `queue-replay-2438330`）；上面三条路径**没有任何 daemon 在服务**。
逐路径探测：

```
$ stat -c '%F %d %i' /home/user/workspace/gopi-test-scratch/queue/queue-replay-4007787/runtime/gvfs
stat: cannot statx '...': Transport endpoint is not connected
$ ls /home/user/workspace/gopi-test-scratch/queue/queue-replay-4007787/runtime/gvfs
ls: cannot access '...': Transport endpoint is not connected
```

**机制链**（每一环都由上一段实测支撑，无推断成分）：

1. daemon 死了（进程没了），内核**保留挂载表条目** —— FUSE 的 `fusectl` 下该连接处于 abort 状态；
2. 于是该目录项**永远不能删除**：`unlink(2)`/`rmdir(2)` 要先解析目录项，而路径任何访问都返回 ENOTCONN；
3. `os.RemoveAll` 走 `openat` 遍历，一步就撞上 ENOTCONN，于是**整个 entry**（不只那个目录）删不掉；
4. GC 的判定链是「stale + 无 holder + 在命名空间内」→ 每次都选中它，每次都失败，于是每次 tick 都重复报告一次。
   这就是 Rule 34/35 所说的**无回收路径的残留**：可测量、被判为垃圾、却没有任何回收手段。

**为什么既有的权限修复救不了它**：`removeCollectable` 已有 `grantOwnerAccess`（修一个
被 suite 中途 kill 掉、留下 mode-0 目录的子树）。那是**权限**问题；本条是**挂载**问题，
`chmod` 对悬挂挂载点毫无作用。

---

## 2. 根因：谁把 `XDG_RUNTIME_DIR` 指进了被清扫的树

**答案：已删除的 `tools/termemu/termemu.py`（replay 管线），删除提交 `971400a72`（2026-10-03）。**

它的私有 session bus 把 `XDG_RUNTIME_DIR` 设成 `<scratch>/runtime`：

```python
# tools/termemu/termemu.py:252（在 971400a72 的父提交上读原文）
rt = os.path.join(scratch, "runtime")
os.makedirs(rt, exist_ok=True)
# termemu.py:262
env = dict(os.environ, XDG_RUNTIME_DIR=rt, LANG=REQUIRED_LOCALE, LC_ALL=REQUIRED_LOCALE)
# termemu.py:281 —— 同一个值也写进自己的进程环境
os.environ["XDG_RUNTIME_DIR"] = rt
```

`scratch` 是调用方传进来的目录，而 replay 管线的 `--scratch` 默认就落在
`~/workspace/gopi-test-scratch/queue`（`scripts/lib/tmp-ban.sh` 的 `gopi_scratch_mkdir`，
「off the repository, off /」正是 Rule 22 要求的）。GLib 的 gvfs 模块把它的 FUSE 文件系统
挂在 **`$XDG_RUNTIME_DIR/gvfs`** —— 于是挂载点落在 GC 拥有的一层/两层之下。

**实测证据（不是推断）**：两个**仍然活着**的 scratch 内 gvfsd-fuse daemon，
其 `/proc/<pid>/environ` 一字不差地带着那个变量：

```
$ tr '\0' '\n' < /proc/1080553/environ | grep XDG_RUNTIME_DIR
XDG_RUNTIME_DIR=/home/user/workspace/gopi-test-scratch/queue/queue-replay-1080449/runtime
$ pgrep -af gvfsd-fuse
1080553 /usr/libexec/gvfsd-fuse /home/user/workspace/gopi-test-scratch/queue/queue-replay-1080449/runtime/gvfs -f
2438498 /usr/libexec/gvfsd-fuse /home/user/workspace/gopi-test-scratch/queue/queue-replay-2438330/runtime/gvfs -f
```

即 `XDG_RUNTIME_DIR` 的值与该 daemon 实际的服务目标**逐字符相等**，
`<XDG_RUNTIME_DIR>/gvfs` 就是挂载点。**推断部分（明确标注）**：三条**已死**的挂载，
其生产者被判定为同一条管线，依据是名字与时间——三个 queue 子目录名为
`queue-replay-*` / `queue-session-operand-test-*`，其 `runtime/` 目录的 mtime 落在
2026-10-02 / 10-03（正是该管线存在的两天），且 termemu.py 在 `971400a72` 被删除。
三条的 daemon 已不在，因此它们的挂载是悬挂的。

**今天还会不会再产生**：不会。`termemu.py` 已删除（`git log --all -- tools/termemu` 的
最后一笔就是删除提交），全树已无任何地方给子进程写 `XDG_RUNTIME_DIR=`（`git grep` 的
命中只剩下两个测试里从环境**读取**的用量）。所以这是**一次性残留**，
本文件要回收的正是它，而不是要防一个还在产的源头。

**同类事实（同一台机器上仍在的）**：`queue-replay-1080449`、`queue-replay-2438330`
两条**仍是活的**（daemon 在服务）。它们不是残留，本回收路径对它们**只报告不动作**
（见 §3 的 veto）。

---

## 3. 回收路径：`internal/scratch/mount_detach.go`

**位置**：新文件 `internal/scratch/mount_detach.go`，接入点 `internal/scratch/gc.go`
的 `removeCollectable` —— **在 `RemoveAll` 之前**。顺序是刻意的：挂着死挂载时
`RemoveAll` 必然失败，先卸载才让下面**那一次**删除成为有效的那一次。

**它做什么**：

1. **读挂载表**（`/proc/self/mounts`，一次、有上限 `selfMountsReadLimit`）；
   读不到 = 返回 error（**绝不返回空表** —— 「没有挂载」和「测不了」不能长得一样，Rule 15）。
2. **只取该 entry 之下的行**：比较按路径边界（`/a/bc` 不在 `/a/b` 下），
   所以不会误选邻居的挂载。
3. **只认 `fuse.gvfsd-fuse`**（常量 `GvfsFuseFSType`）。别的 fuse 类型
   （用户的 sshfs、rclone、`fuse.portal`）**报告并放过** —— scratch 清扫不是通用 umount。
4. **先测量再动作**：`probeMountPoint` 三态 ——
   - 可达 = **活挂载** → 保留并报告（这不是残留，删它等于从一个活进程脚下抽走文件系统）；
   - `ENOTCONN` = **唯一授权卸载的答案**；
   - 其它任何错误 = **测不了 → 保留并报告原因**（「测不了 ≠ 死了」）。
5. **卸载是 `fusermount3 -u`，失败再试 `-uz`（lazy）**，每次硬超时
   `mountDetachTimeout`；**不用 sudo、不用 `umount` 的特权路径**（Rule 25：
   特权不是缺的成分，索要特权本身就是那个 bug）。
   两个工具都不在 PATH 时，**按名字拒绝**并把**操作员的手工路线**写进错误里
   （终端里手跑 `fusermount3 -u <挂载点>`，下一次 sweep 即可回收该 entry）。
6. **一个 failed 不拖累兄弟**：每个挂载独立判定，第一个失败被返回、其余继续（Rule 15b）。
7. **不 fsync、不 sleep、不循环**：一次表读 + 每个挂载一次 `stat` + 每次卸载一条有界命令。

**保留的契约**：「无法测量 = keep」原样保留 —— 测不了的挂载不卸载、其 entry 在
`RemoveAll` 失败后仍走既有的 `entryReadable`/`grantOwnerAccess` 判定链。

---

## 4. 回归测试（`internal/scratch/mount_detach_test.go`）

**为什么不在测试里造一个真的悬挂挂载**：那需要先 mount 一个 fuse 文件系统再杀掉它的
daemon —— 一个**改变全机所有进程所见**的动作，而且是在共享宿主的测试里做。
它与 Rule 27 禁止的「测试造设备负载」同形（拿真实资源的破坏性状态去凑一个数字），
也和 Rule 25 的禁止特权同类。所以**两个决定卸载与否的测量被做成可注入函数**：

```go
var (
    scratchMountTable  = readSelfMounts   // 挂载表
    scratchMountProbe  = probeMountPoint  // 死/活/测不了 三态
    scratchMountDetach = detachFuseMount  // 卸载动作
)
```

**不需要特权的另一半用真数据钉住**：解析器拿本机**真实产出的那三行**当 fixture
（`realDeadGvfsMountLine` 就是 §1 里那条原文），所以「解析器读不出这一行」这种
会让整条回收路径哑火的漂移，测试会红。

覆盖：解析真实 bug 行、`\040` 转义、路径边界（邻居不被选中）、死挂载被卸载且 entry 被回收、
**活挂载绝不卸载**、**外来 fuse 类型绝不卸载也不探测**、**测不了 → 不卸载 + 返回 error**、
挂载表读不到 → 报错不静默、真实挂载表端到端（production seam 真的接上了，不是 nil）、
无 fusermount 时按名字拒绝并给操作员路线（Rule 25）、stub 工具 pin 住 argv 形状、
`-u` 失败回退 `-uz`、失败携带工具自己的输出。

**红证**：把 `gc.go` 还原成修复前（`RemoveAll` 直接打头、不接 mount arm），
保留新文件与测试 → `TestDeadGvfsMountIsDetachedAndTheEntryCollected` **FAIL**
（`probed []`，即 arm 从未被调用）。恢复后转绿。

---

## 5. 这台机器上的实测结果

修复后跑真实 GC，三条 entry **全部被卸载并回收**（见报告的 Verification）：

```
scratch: detached the dead fuse.gvfsd-fuse mount at .../queue-replay-4007787/runtime/gvfs (under ...): the entry can now be collected
scratch: collected .../queue-replay-4007787 outcome=unregistered ...
scratch: detached the dead fuse.gvfsd-fuse mount at .../queue-replay-e2e-1326956/scratch-chat1/runtime/gvfs ...
scratch: collected .../queue-replay-e2e-1326956 ...
scratch: detached the dead fuse.gvfsd-fuse mount at .../queue-session-operand-test-4014544/scratch/runtime/gvfs ...
scratch: collected .../queue-session-operand-test-4014544 ...
```

三条路径此后 `ls` 报 `No such file or directory`（原先报 ENOTCONN），
挂载表里 scratch 相关行从 5 条降到 2 条 —— **剩下的正是两条守护进程仍活着的**，
被 veto 正确保留。

**已知未覆盖（如实）**：
- 另外两条**活着的** scratch 内挂载（`queue-replay-1080449`、`queue-replay-2438330`）
  没有被回收，因为它们的 daemon 在服务。它们要等 daemon 退出后才成为可回收对象；
  在此之前它们**应当**被保留，这不是缺陷。
- 三条路径的 ENOTCONN 判定是在**修复前**做的；修复把它们卸载掉了，所以「未修复时
  `RemoveAll` 会失败」这一环在今天的机器上**已无法重演**（证据是三行日志 + 那时的
  `stat`/`mountinfo` 原文，已存入本 run 的 session 目录）。测试因此用注入 seam 把
  判定逻辑钉住，而不是依赖那三个已经消失的挂载点。
