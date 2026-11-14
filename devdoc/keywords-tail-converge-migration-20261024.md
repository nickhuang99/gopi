<!-- doc-class: historical -->
# 一次真实的数据损伤与回滚：keywords 尾块收敛迁移（2026-10-24）

**任务**：`01a16d97-7cd0-7265-bb96-83a5d47a5eb1` — 把 prod `~/.gopi/memory.db`
`distilled_qa.answer` 里重复的 `\n\nkeywords: ` 尾块收敛成一个。

**这篇文档的价值在失败那一段。** 迁移本身在第二次尝试里做到了「6854 行受影响、
正文逐字节不变、双 marker 归零」，但第一次对 **生产库** 的运行造成了 6854 行的
真实损伤，而当时我自己的「验证」打印的是「0 处正文不一致」。两个事实同时成立，
这正是这篇文档要留下的东西：**一个错误的验证式，比没有验证更危险。**

## 1. 数据形状（先测后改）

prod `distilled_qa` 15815 行（迁移前），answer 尾部 `\n\nkeywords: ` 出现次数：

```
{0: 4929, 1: 4032, 2: 6727, 3: 127}
```

6854 行带 ≥2 个尾块。切分后：

| 形状 | 行数 | 含义 |
|---|---|---|
| 所有尾块内容完全相同 | 6491 | 选哪块都一样 |
| 首块 ≠ 末块 | 363 | 选哪块**改变正文**，必须计数并说明 |
| 末块 == `search_kw` 列 | 6853 | 唯一例外 id 7854（`search_kw` 为空） |
| 尾块之间夹着非尾块文本 | 3（8469/8933/13719） | 首块末尾带 `lesson_type:` / `SOURCE:` 溯源行 |

**结论**：收敛规则 = `stripKeywordsTail`（贪婪，吃掉全部尾块）+ 用 **LAST** 块重拼。
理由是末块与 `search_kw` 列一致 ⇒ 末块才是**当前**拼写，首块是被取代的旧拼写。

## 2. 失败：`keywords: keywords:` 与一条测不出它的验证式

第一版实现里，我把分隔符写成：

```go
converged := body + keywordsTailSeparator() + keywordsTailMarker + kept
```

而 `keywordsTailSeparator()` 的定义是 `"\n\n" + keywordsTailMarker` —— **marker 已经在里面**。
于是每一行被写成 `…\n\nkeywords: keywords: <kws>`。

**为什么验证没发现**：我的验证式是

```python
body = a[:a.find(M)]        # M = "\n\nkeywords: "
```

它只比较**第一个分隔符之前**的文本。双 marker 出现在分隔符**之后**，所以这个验证式
对这次损伤**结构上不可见** —— 它打印 `0 mismatches` 是必然的，与被改坏与否无关。

同时我还把 `345` 这类行的正文削掉了（interleaved 判定当时写成「段内出现裸换行」，
把 id 7770 那种合法的单换行尾块也误判，又在另一版里对真 interleaved 行照改不误）。

**对 prod 的实际影响（实测）**：6854 行 answer 被改写为双 marker 形态。

## 3. 回滚（可复核）

迁移前用 `gopi memory backup --no-ship --dir <off-/ 目录>` 做过快照
（VACUUM INTO + 只读复验 + `PRAGMA quick_check` + sha256），所以回滚是**精确**的：

```python
before = dict(select id, answer from backup)      # 只读打开备份
cur    = dict(select id, answer from prod)
drift  = [i for i in cur if before[i] != cur[i]]  # 6854
begin immediate; update ... answer=before[i] where id=i;  commit
```

实测：`rows restored: 6854  not-restored: 0`，`double-marker rows: 0`。

**回滚也暴露了一个观察陷阱**：回滚后行数 15824 ≠ 备份 15815。差额不是丢失，而是
本任务运行期间新建的行（包括我自己提交的 bug 行）——备份是时点快照，活库会继续增长。
判「有没有丢行」必须看**行集**，不能看总数差。

## 4. 修复后重跑（最终结果）

在**当前 prod 的新副本**上先跑、严格校验，再动生产库：

```
before {0:4936, 1:4034, 2:6727, 3:127}
after  {0:4936, 1:10885, 2:3}
rows scanned=15824 affected=6854 changed=6851 already-converged=0
kept block = LAST; FIRST differs=363; kept==search_kw=6853; interleaved=3
```

- 正文逐字节：受影响行 mismatch = **0**（这次用**真的**比较：迁移前后的同一份副本）
- 未受影响行：changed = **0**
- `keywords: keywords:` 行：**0**
- 仍 >1 尾块的 3 行：[8469, 8933, 13719] —— **被主动拒改**（interleaved）
- `PRAGMA quick_check`: ok

## 5. 保留的诚实缺口

1. **3 行未收敛**（8469/8933/13719）。它们的首块末尾带溯源行（`lesson_type:` /
   `SOURCE:`），把它们压成一尾块必然删掉那段文本。迁移的承诺是「正文逐字节不变」，
   所以这 3 行被拒改并计数（`Preserved`）。验收写的是 `{0:…, 1:…, 2:0, 3:0}`，
   实测是 `{…, 2:3}`：**差值 3 就是这 3 行**，不是遗漏，是一个明确的取舍。
2. 这 3 行的处理（保留溯源行还是丢弃）需要数据所有者决定，本任务不替它选。

## 6. 可复用的教训

1. **验证式必须能看见被改动的区域**。「只比较分隔符之前」对「分隔符之后被改坏」永远绿。
2. **拼接类 bug（重复一个已在常量里的前缀）不会被单元测试自动抓到** —— 抓它的是
   「跑完之后再读一遍数据，并和**迁移前**的副本比」，不是「跑之前 dry-run 数字对得上」。
3. **备份的 off-/ 约束会挡路**：本机 `/`、`/home`、`/tmp` 同一设备（`stat -c %d` 全 66308），
   只有 `/home/user/workspace`（nvme1n1p1, 66305）满足 Rule 22，备份必须放那里。
4. **先副本、后生产**，且两者用**同一套校验**；副本对了不等于生产对了，但副本错了
   生产必然错。
