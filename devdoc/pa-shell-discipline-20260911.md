<!-- doc-class: historical -->
# PA Shell 作业纪律：所有命令必须有超时，永不无限等待

**来源**：2026-09-11 peerbox 资源耗尽事故中 PA 自身的失误（nick 指出）
**性质**：硬约束，PA 每次进 shell 都必须遵守

---

## 一、错误回顾（两个实证）

事故排查中我两次把自己卡住：

1. **`du -sh /tmp` 超时 300s** —— IO 已被孤儿 dd 打满，`du` 遍历 13,044 个目录时
   完全饥饿，命令跑满 300s 才返回 exit 124（超时）。我在这 5 分钟里什么都没做。
2. **`pkill -9 -f 'suite-ab.sh'` 被挡回两次** —— D 状态进程 + IO 饱和导致 pkill 自身
   的返回被阻塞，连续两次拿到空输出/exit -9，触发了工具的 loop warning。

**共同点**：我的命令**没有任何时长上限**，默认就是"等到它回来为止"。在系统已经病态的
情况下，这是把自己也拖进去。

## 二、硬约束

### 2.1 每一条诊断命令都带 `timeout`

**规则**：任何可能遍历大目录树 / 等待 IO / 等待进程状态的命令，**一律加 `timeout`**。

```bash
# 好
timeout 30 du -shx /tmp/* 2>/dev/null | sort -rh | head
timeout 20 find /home/user -maxdepth 3 -name 'Test*' -type d
timeout 10 ps -eo pid,stat,cmd

# 坏 —— 无上限，系统病态时把自己拖死
du -sh /tmp
find / -name 'x'
```

**超时值选择**：
- 已知范围的简单查询（`ps` / `df` / `ss`）：10s
- 目录遍历、`du`、`find` 有界路径：**30-60s**
- 已知的大范围扫描：**最多 120s**，且必须能分块（`--max-depth` / `head`）
- 绝不设 300s+ 然后"赌它能回来"——**跑满上限本身就是失败信号**，应该换方法

### 2.2 超时后换方法，不重试同一命令

拿 exit 124 = "这条路在当前系统状态下走不通"，应当**换更轻的方法**而不是加大
timeout 重跑。事故中的正确演进（我后来做到了）：

| 失败的命令 | 换的方法 |
|-----------|---------|
| `du -sh /tmp`（超时） | `ls -1 /tmp \| sed 's/[0-9]\+/N/g' \| sort \| uniq -c`（只读目录项，零 IO 遍历） |
| 全量 `du -shx /*` | 先 `du -shx /var/*`（有界子目录）+ 只对可疑项深挖 |
| `pkill -9 -f`（被挡） | `kill -9 <具体PID列表>`（单进程，返回即时） |
| `du -shcx /tmp/p*`（超时） | 拆成 `du -shx p[0-9]*_dump_* \| tail -3`（只探边界，不求和） |

### 2.3 破坏性操作先"止损"，再"清理"

顺序上，**先让系统能响应**（杀最凶的 CPU/IO 消耗者），再做磁盘/进程盘点。
事故中我先杀了 6 个 dd 宿主，之后所有 `du`/`find` 才变得可行。反过来做会全程卡顿。

### 2.4 阻塞式重命令用 background + notify

如果一条命令**天然可能跑很久**（全量测试、大库备份、批量扫描），不要 foreground 赌：
用 `terminal(background=true, notify_on_complete=true)`，然后继续做别的事。
这与"PA 不沉浸等待"是同一条纪律。

### 2.5 永远不做无上限的 `for` / `while`

参考罪魁祸首 `suite-ab.sh`：`while :; do dd ...; done` 是 DoS 自己的写法。
任何脚本里的循环必须有**迭代上限**或**时长上限**（`timeout N <cmd>`），
并有 `trap ... EXIT` 清理自己起的子进程。

## 三、为什么这是 PA 级别的硬约束

PA 的价值在**倾听-理解-决策-派发**，不在盯梢。一条卡死的命令会：
1. 让 PA 停止响应 nick（最严重的后果——耳朵关闭）
2. 浪费上下文窗口，且无法解释"我在等什么"
3. 在系统已经病态时，PA 的探测行为本身加剧症状（`du` 也是 IO 负载）

**等价表述**：shell 命令的 timeout 就是 PA 自己的 heartbeat throttle ——
没有它，PA 会在一条命令上无限期失神。

## 四、附：事故中最有用的轻量诊断序列

```bash
# 1. 先看有没有恶性的 CPU/IO 消耗者（10s 内）
ps -eo pid,ppid,pcpu,pmem,rss,etimes,stat,cmd --sort=-pcpu | head -20

# 2. 找不可中断（D）状态 —— 这是 IO 饱和的指纹（10s 内）
ps -eo pid,ppid,stat,etimes,cmd | awk '$3 ~ /D/'

# 3. 孤儿进程（PPID=1 且 cmd 可疑）（10s 内）
ps -eo pid,ppid,etimes,cmd | awk '$2==1 && $4 ~ /dd|go-build|mock|grep/'

# 4. 磁盘（有界，30s 内）
df -h / /home; du -shx /* 2>/dev/null | sort -rh | head

# 5. 目录项爆炸但不想遍历：只读目录名模式计数（秒级）
ls -1 /tmp | sed 's/[0-9]\+/N/g' | sort | uniq -c | sort -rn | head
```
