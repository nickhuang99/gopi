<!-- doc-class: historical -->
# PCIe AER Correctable 风暴 —— hosta 根盘 WD Blue SN570 宿主机诊断（2026-10-28）

任务行：`01a18273-f478-71e4-bdcc-908cf81092fd`（由 `01a180ff` 提交）
本页是**历史记录**：它测量了 2026-10-28 22:00–22:15 那一刻的机器状态。数字会变，
**命令不会变** —— 每一条结论都带可粘贴的命令和它的原始输出。

> **一句话结论**：根盘 `nvme0n1`（WD Blue SN570 2TB，PCI `10000:e2:00.0`，
> `[15b7:5025]`）的 **Receive Error（物理层 RxErr）** 累计 496 718 次，
> 占该设备**全部**可纠正错误的 96.9%；对照盘 `nvme1n1` 同一时刻是**精确的 0**。
> 这是**链路层的物理信号完整性劣化**，不是驱动、不是文件系统、不是 gopi 的代码能修的
> （Rule 35）。**不致命**：Uncorrectable/Fatal 全为 0，无 I/O 超时、无 reset、
> 无掉盘 —— 但速率没有衰减，且在涨。**证据通道全部不需要 root**，只有 SMART 需要装包。

---

## 0. 纠正 briefing 里的两条判据（本 run 实测）

| briefing 的说法 | 本 run 实测 | 判定 |
|---|---|---|
| 「100% 落在根盘 10000:e2:00.0」 | **按"接收源"是 99.6%**（近 48h：127 024 对 43）。另有 **43 次**来自 `10000:00:1d.4` | **需修正**，见 §3 |
| 「nvme1n1 零错误」 | **确认成立**：`TOTAL_ERR_COR = 0`，上游桥 `e0:1a.0` 的 `aer_rootport_total_err_cor = 0` | **确认** |
| 「约 11.2 万条 / 12 天」 | **实测 503 103 次 / 12.74 天 ≈ 39 478 次/天** | **低估约 4.5 倍**（原计数按"某一种行"数的，见 §4） |
| 「dmesg 只剩最后 10 分钟」 | **确认且更精确**：710.2 秒 = **11.84 分钟**，覆盖率 **0.0645%** | **确认** |

**`10000:e0:1d.4` 到底是不是"另一个出错的设备"？** —— **不是。它是上游根端口，
在报转发的错误，另外还有一小撮它自己的错误。** 拆成两个事实：

1. **`pcieport 10000:e0:1d.4: AER: Correctable error message received from 10000:e2:00.0`**
   是**同一个事件块的第一行**（转发通知），不是独立错误。计数它等于把每个事件数两遍 ——
   这正是 briefing 与实测差 4.5 倍的来源之一。
2. **`pcieport 10000:e0:1d.4: PCIe Bus Error: severity=Correctable, type=Data Link Layer, (Transmitter ID)`**
   （它自己当端点，完整原文见 §2.4）
   累计 **270 次**，占全部 512 245 的 **0.053%**：`Timeout 238` / `RxErr 32` / `Rollover 1`。
   其中 `Timeout` 是**上游根端口**这一侧的计数器，量级与根盘的 49 万完全不是一个数量级，
   且**无一次 Uncorrectable/Fatal**。

---

## 1. 现象实测

### 1.1 一次完整事件的原样 4 行

```bash
journalctl --no-pager -k --since '48 hours ago' | grep -A3 -m1 'AER: Correctable error message received from 10000:e2:00.0'
```

```
Oct 26 22:08:25 hosta kernel: pcieport 10000:e0:1d.4: AER: Correctable error message received from 10000:e2:00.0
Oct 26 22:08:25 hosta kernel: nvme 10000:e2:00.0: PCIe Bus Error: severity=Correctable, type=Physical Layer, (Receiver ID)
Oct 26 22:08:25 hosta kernel: nvme 10000:e2:00.0:   device [15b7:5025] error status/mask=00000001/0000e000
Oct 26 22:08:25 hosta kernel: nvme 10000:e2:00.0:    [ 0] RxErr                  (First)
```

**`type=Physical Layer, (Receiver ID)` + `[0] RxErr`** = 盘这一侧的**接收器**
在物理层收到坏信号。`(Receiver ID)` 的意思是"发送方/介质侧出了问题，被接收方记下来了"。

### 1.2 寄存器：错误类型的完整分布（决定性证据）

```bash
cat /sys/bus/pci/devices/10000:e2:00.0/aer_dev_correctable
```

```
RxErr 496718
BadTLP 12
BadDLLP 21973
Rollover 0
Timeout 0
NonFatalErr 0
CorrIntErr 0
HeaderOF 0
TOTAL_ERR_COR 511873
```

`RxErr / TOTAL = 496718 / 511873 = 97.0%`，`BadDLLP` 占 4.3%（两者部分重合计数）。
**`Timeout 0`、`BadTLP 12`（≈0）** —— 这排除了"软件/驱动发出了畸形 TLP"这一整类原因：
如果是驱动或 OS 的问题，`BadTLP`（发送侧的错误 TLP）会显著。

### 1.3 对照：另一块 NVMe 是精确的 0

```bash
for d in 10000:e2:00.0 10000:e1:00.0 10000:e0:1d.4; do
  echo "##### $d"
  tr '\n' ' ' < /sys/bus/pci/devices/$d/aer_dev_correctable; echo
  awk '/TOTAL_ERR_COR/{print "TOTAL:", $2}' /sys/bus/pci/devices/$d/aer_dev_correctable
done
echo "--- 对照盘的上游桥 ---"
cat /sys/bus/pci/devices/10000:e0:1a.0/aer_rootport_total_err_cor
```

```
##### 10000:e2:00.0
RxErr 496718 BadTLP 12 BadDLLP 21973 Rollover 0 Timeout 0 NonFatalErr 0 CorrIntErr 0 HeaderOF 0 TOTAL_ERR_COR 511873
##### 10000:e1:00.0
RxErr 0 BadTLP 0 BadDLLP 0 Rollover 0 Timeout 0 NonFatalErr 0 CorrIntErr 0 HeaderOF 0 TOTAL_ERR_COR 0
##### 10000:e0:1d.4
RxErr 32 BadTLP 0 BadDLLP 0 Rollover 1 Timeout 238 NonFatalErr 0 CorrIntErr 0 HeaderOF 0 TOTAL_ERR_COR 270
--- 对照盘的上游桥 ---
0
```

**同一个 CPU 域、同一个 `10000:e0` 根复合体、同一天出厂的两块 M.2 —— 一块 51 万，一块 0。**
这把"平台/CPU/全局电源"整类原因排除掉了一大半：坏的是**这条链路**。

---

## 2. AER 归属：用 lspci 拓扑 + sysfs 父链判定（不是 grep 单行）

### 2.1 父链（真凭据）

```bash
readlink -f /sys/bus/pci/devices/10000:e2:00.0
readlink -f /sys/bus/pci/devices/10000:e1:00.0
ls /sys/bus/pci/devices/10000:e0:1d.4/ | grep -E '^10000:'
```

```
/sys/devices/pci0000:00/0000:00:0e.0/pci10000:e0/10000:e0:1d.4/10000:e2:00.0
/sys/devices/pci0000:00/0000:00:0e.0/pci10000:e0/10000:e0:1a.0/10000:e1:00.0
10000:e0:1d.4:pcie001
10000:e0:1d.4:pcie002
10000:e0:1d.4:pcie010
10000:e2:00.0
```

**读法**：`10000:e2:00.0`（根盘）的唯一父节点就是 `10000:e0:1d.4`。所以
`pcieport 10000:e0:1d.4` 那些行是**上游根端口在转发它的错误消息**，不是另一个故障点。

### 2.2 设备身份

```bash
lspci -D | grep -E '10000:(e0:1a|e0:1d|e1:00|e2:00)'
for n in nvme0 nvme1; do printf '%-8s -> %s\n' "$n" "$(readlink -f /sys/class/nvme/$n/device)"; done
for n in nvme0 nvme1; do for f in model serial firmware_rev state; do printf '  %-12s = %s\n' "$f" "$(cat /sys/class/nvme/$n/$f)"; done; done
```

```
10000:e0:1a.0 PCI bridge: Intel Corporation Device 7a48 (rev 11)
10000:e0:1d.0 System peripheral: Intel Corporation Device 09ab
10000:e0:1d.4 PCI bridge: Intel Corporation Device 7a34 (rev 11)
10000:e1:00.0 Non-Volatile memory controller: Device 1dbe:5236 (rev 01)
10000:e2:00.0 Non-Volatile memory controller: Sandisk Corp Device 5025 (rev 01)

nvme0    -> /sys/devices/pci0000:00/0000:00:0e.0/pci10000:e0/10000:e0:1d.4/10000:e2:00.0
nvme1    -> /sys/devices/pci0000:00/0000:00:0e.0/pci10000:e0/10000:e0:1a.0/10000:e1:00.0

nvme0: model = WD Blue SN570 2TB   serial = 23032A802561   firmware_rev = 234200WD   state = live
nvme1: model = Fanxiang S770M 4TB  serial = FX232640342    firmware_rev = 3.W.J.E4    state = live
```

`Sandisk Corp Device 5025` 与 `[15b7:5025]` 是同一颗控制器（`15b7` = Sandisk 的
vendor id，WD Blue SN570 用的是 SanDisk 自研主控）。**不是两块不同厂商的芯片。**

### 2.3 那 43 次 `10000:00:1d.4` 是什么 —— 幻影源，非真实设备

```bash
ls -d /sys/bus/pci/devices/0000:00:1d.4 ; lspci -D | grep -E ':00:1d\.4'
ls /sys/bus/pci/devices/ | grep '^10000:00'
journalctl --no-pager -k --since '48 hours ago' | grep -B1 -A2 -m1 'received from 10000:00:1d.4'
```

```
ls: cannot access '/sys/bus/pci/devices/0000:00:1d.4': No such file or directory
（lspci 无匹配）
（domain 10000 的 bus 00 上没有任何设备）

Oct 26 22:30:22 hosta kernel: nvme 10000:e2:00.0: AER:   Error of this Agent is reported first
Oct 26 22:30:22 hosta kernel: pcieport 10000:e0:1d.4: AER: Correctable error message received from 10000:00:1d.4
Oct 26 22:30:22 hosta kernel: pcieport 10000:e0:1d.4: AER: found no error details for 10000:00:1d.4
Oct 26 22:30:23 hosta kernel: pcieport 10000:e0:1d.4: AER: Correctable error message received from 10000:e2:00.0
```

`10000:00:1d.4` **在 lspci 与 sysfs 中都不存在**（domain 10000 的 bus 0 没有设备）。
它每次出现都紧挨着一次真实的 `10000:e2:00.0` 事件，并且被内核标注
`AER: found no error details`（源设备 ID 解析失败）。

> **推断（标明）**：这是同一次物理层瞬变在**根端口的 Requestor ID 字段**里被
> 采到半个/错位的值时，内核按 `domain:bus:dev.fn` 拼出的**伪 BDF**。依据是
> 它 (a) 在拓扑里不存在，(b) 总是与真实事件同秒出现，(c) 内核自己说
> "found no error details"。**这仍是推断，不是实测** —— 要证实需在 `pcieport`
> 上加 `dyndbg` 打印原始 AER header，那是另一个 run 的事，且要 root（Rule 25）。

**不影响结论**：43 次 / 127 067 次 = 0.034%，且全部与根盘事件同源同刻。

### 2.4 根端口自己当端点的错误（270 次，原样原文）

```bash
journalctl --no-pager -k --since '48 hours ago' | grep -E 'pcieport 10000:e0:1d\.4: PCIe Bus Error' | head -4
journalctl --no-pager -k --since '48 hours ago' | grep -E 'pcieport 10000:e0:1d\.4: PCIe Bus Error' \
  | sed -E 's/.*severity=([^,]+), type=([^,]+), \((.*)\)/\1|\2|\3/' | sort | uniq -c
```

```
Oct 26 22:30:22 hosta kernel: pcieport 10000:e0:1d.4: PCIe Bus Error: severity=Correctable, type=Data Link Layer, (Transmitter ID)
Oct 26 23:44:04 hosta kernel: pcieport 10000:e0:1d.4: PCIe Bus Error: severity=Correctable, type=Physical Layer, (Receiver ID)
Oct 27 02:17:21 hosta kernel: pcieport 10000:e0:1d.4: PCIe Bus Error: severity=Correctable, type=Physical Layer, (Receiver ID)
Oct 27 03:08:48 hosta kernel: pcieport 10000:e0:1d.4: PCIe Bus Error: severity=Correctable, type=Data Link Layer, (Transmitter ID)

     79 Correctable|Data Link Layer|Transmitter ID
      8 Correctable|Physical Layer|Receiver ID
```

**这是一个重要的细节，而且它对"上游 vs 下游"的判定是决定性的**：
根端口自己的错误**主要是 `Data Link Layer, (Transmitter ID)`** —— 意思是
**根端口作为发送方**在数据链路层出错（`Timeout` 计数器 238）。而根盘的错误
**全是 `Physical Layer, (Receiver ID)`** —— 盘作为接收方在物理层出错。

两者方向相反、层不同、量级差 1900 倍。**但它们挂在同一条物理链路上**，
所以更可能的解释是：**同一条链路的信号完整性劣化，在两端各留下不同形态的账**
（下游记"我收到坏的"，上游记"我发的没被正确确认"）。这不是两个独立故障点。

> **推断（标明）**：`Timeout 238` 是根端口在等下游 ACK/NAK 超时；`RxErr 496 718` 是
> 盘在收到坏信号。同一个电气缺陷的两个侧面。**要证实需要在内核里打开链路层的
> 详细 tracing（`dyndbg`，需 root），本 run 不做。**

---

## 3. 速率：时间分桶（不是单一总数）

### 3.1 按小时（近 48 小时，完整表在 session 证据 `aer_hourly_48h.txt`）

```bash
journalctl --no-pager -k --since '48 hours ago' -o short-iso \
  | grep 'AER: Correctable error message received from 10000:e2:00.0' \
  | cut -c1-13 | sort | uniq -c | awk '{printf "%s  %s\n", $2, $1}'
```

```
2026-10-26T22  2794      2026-10-27T09  2423      2026-10-28T00  3332
2026-10-26T23  2907      2026-10-27T10  2905      2026-10-28T01  4244
2026-10-27T00  2386      2026-10-27T11  4003      2026-10-28T02  1573
2026-10-27T01  2293      2026-10-27T12  4012      2026-10-28T03   720
2026-10-27T02  2368      2026-10-27T13  2314      2026-10-28T04   638
2026-10-27T03  2328      2026-10-27T14  2103      2026-10-28T05   742
2026-10-27T04  2344      2026-10-27T15  2208      2026-10-28T06   837
2026-10-27T05  4590      2026-10-27T16  1816      2026-10-28T07  1946
2026-10-27T06  4989      2026-10-27T17  1488      2026-10-28T08  2416
2026-10-27T07  3907      2026-10-27T18  3472      2026-10-28T09  1146
2026-10-27T08  3763      2026-10-27T19  3918      2026-10-28T10   700
2026-10-27T09  2423      2026-10-27T20  3730      2026-10-28T11   793
                         2026-10-27T21  3603      2026-10-28T12  1187
                         2026-10-27T22  4706      2026-10-28T13  3850
                         2026-10-27T23  4577      2026-10-28T14  1343
                                                 2026-10-28T15  1060
                                                 2026-10-28T16   994
                                                 2026-10-28T17   892
                                                 2026-10-28T18  1254
                                                 2026-10-28T19  4691
                                                 2026-10-28T20  4324
                                                 2026-10-28T21  4119
```

**读法**：**没有单调恶化，也没有凌晨低峰**。最安静的小时 638，最响的 4989，跨度 7.8 倍，
但**任意 24 小时窗口都稳定在 3–7 万**。**它跟负载相关，不是跟温度相关**（见 §5.3）。

### 3.2 按天（journal 全期，`aer_daily.txt`）

```bash
journalctl --no-pager -k -o short-iso \
  | grep 'AER: Correctable error message received from 10000:e2:00.0' \
  | cut -c1-10 | sort | uniq -c | awk '{printf "%s  %s\n", $2, $1}'
```

```
2026-10-16  38328   2026-10-21  23710   2026-10-26  42145
2026-10-17  34053   2026-10-22  34680   2026-10-27  76246
2026-10-18  23239   2026-10-23  38439   2026-10-28  42866（至 22:00，未满）
2026-10-19  24993   2026-10-24  58944
2026-10-20  30053   2026-10-25  35001
```

日桶合计 502 697，与总量 503 103 相差 406（0.08%，跨零点边界），**自洽**。

### 3.3 速率趋势：首 24h vs 末 24h —— **在涨，不是衰减**

```bash
journalctl --no-pager -k --since '2026-10-16 04:17' --until '2026-10-17 04:17' \
  | grep -c 'AER: Correctable error message received from 10000:e2:00.0'
journalctl --no-pager -k --since '2026-10-27 22:00' \
  | grep -c 'AER: Correctable error message received from 10000:e2:00.0'
```

```
42963
52486
```

首 24h **29.8 次/分钟**（日均 42 963）→ 末 24h **35.9 次/分钟**（日均 ≈51 000）。
**+20%**。平均速率 27.42 次/分钟 = 1 645 次/小时 = 39 478 次/天。

### 3.4 当前瞬时速率（近 30 分钟，`aer_minute_30m.txt`）

```
2026-10-28T21:33   64   21:40   76   21:47   79   21:54   59   22:01   4
2026-10-28T21:34   48   21:41   82   21:48   61   21:55   70   22:02   8
2026-10-28T21:35   38   21:42   77   21:49    8   21:56   67   22:03   2
2026-10-28T21:36   55   21:43   66   21:50   40   21:57   76
2026-10-28T21:37   58   21:44   98   21:51   88   21:58   66
2026-10-28T21:38   74   21:45   90   21:52   89   21:59   73
2026-10-28T21:39   79   21:46   82   21:53   72   22:00   51
```

**≈65 次/分钟 ≈ 1.1 次/秒**（含本 run 自身写盘与构建产生的负载）。
最后三分钟（22:01–22:03）骤降到 4/8/2 = 本 run 空闲等待时的速率 —— **这是负载相关的直接证据**：
本 run 一开始跑 `journalctl`（大量读盘）时速率是 65–98/分钟，停下来就掉到个位数。

---

## 4. 三路口径互校（为什么 briefing 数出 11.2 万）

```bash
journalctl --no-pager -k | grep -c 'AER: Correctable error message received from 10000:e2:00.0'
journalctl --no-pager -k | grep -c 'nvme 10000:e2:00.0: PCIe Bus Error'
awk '/TOTAL_ERR_COR/{print $2}' /sys/bus/pci/devices/10000:e2:00.0/aer_dev_correctable
cat /sys/bus/pci/devices/10000:e0:1d.4/aer_rootport_total_err_cor
```

```
503103     ← port 转发通知行数（每事件 1 条）
512020     ← endpoint 详情行数，与 sysfs 完全一致
512020     ← sysfs TOTAL_ERR_COR（硬件寄存器）
512245     ← 上游根端口 rootport_total_err_cor
```

- endpoint 行数 **512 020 = 寄存器 512 020**，一字不差 → **journald 没有丢，读数可信**。
- 512 245 − 512 020 = **225**（0.044%）：根端口自己多收到 225 次未在设备侧落账的。
- 近 48h 更细的一层（两条完整命令与原始输出）：

```bash
journalctl --no-pager -k --since '48 hours ago' | grep -c 'AER: Correctable error message received from 10000:e2:00.0'
journalctl --no-pager -k --since '48 hours ago' | grep -c 'AER: Multiple Correctable error message received from 10000:e2:00.0'
journalctl --no-pager -k --since '48 hours ago' | grep -c 'nvme 10000:e2:00.0: PCIe Bus Error'
```

```
124879
2229
127061
```

  124 879 + 2 229 = **127 108** 条转发行，而端点行 127 061 ——
  说明**内核确实对高频转发行做了 `Multiple` 合并**，用转发行当分母会低估约 2.9%。
- 512 020 − 503 103 = **8 917**（1.74%）：端口转发**打印**行比设备行少 ——
  内核在高频时对转发行做了合并（`AER: Multiple Correctable error message received`）。
  用转发行当分母会低估 1.7%。

**briefing 的 11.2 万是怎么来的？—— 本 run 逐一排除了候选解释，但没有定论。**

实测的三条候选口径（都真跑过）：

```bash
journalctl --no-pager -k | grep -c 'RxErr  '                                   # 497037
awk '/^RxErr/{print $2}' /sys/bus/pci/devices/10000:e2:00.0/aer_dev_correctable # 497005
dmesg | grep -c 'RxErr'                                                        # 687
dmesg | grep -c 'AER: Correctable error message received'                      # 697
dmesg | wc -l                                                                  # 2878
```

- **`RxErr` 标记行全期 497 037 与寄存器 497 005 只差 32** —— 所以"只数了某一种标记行"
  这个解释**不成立**，`(First)` 标记并没有被内核换成 `(Second)` 而漏数。
- `dmesg` 里只有 687 条 —— 环形缓冲只留 11.84 分钟，**也不可能给出 11.2 万**。
- 所以 11.2 万**不是**从这台机器当前的 journal 或 dmesg 数出来的。

> **推断（标明，未定论）**：它最可能来自一次**局部时间窗口**的计数
> （例如"某几个小时的速率 × 12 天"的外推），或来自**只包含部分 boot** 的 journal 视图。
> 本 run 无法复现这个数字，也**不为了对齐它去凑**。
> **结论以三路互校为准：503 103 次事件 / 512 020 次端点错误 / 12.74 天。**

---

## 5. 链路是否降级：**没有降级 —— 宽度 x4 未变，无掉速事件；但它本来就跑 Gen3，上游根端口是 Gen4**

### 5.1 宽度与速率

```bash
for d in 10000:e2:00.0 10000:e1:00.0 10000:e0:1d.4; do
  echo "--- $d ---"
  for f in current_link_speed current_link_width max_link_speed max_link_width; do
    printf '  %-20s = %s\n' "$f" "$(cat /sys/bus/pci/devices/$d/$f)"
  done
done
```

```
--- 10000:e2:00.0 ---
  current_link_speed   = 8.0 GT/s PCIe        ← Gen3
  current_link_width   = 4
  max_link_speed       = 8.0 GT/s PCIe        ← 设备侧也只报到 Gen3
  max_link_width       = 4
--- 10000:e1:00.0 ---
  current_link_speed   = 16.0 GT/s PCIe       ← Gen4
  current_link_width   = 4
  max_link_speed       = 16.0 GT/s PCIe
  max_link_width       = 4
--- 10000:e0:1d.4 ---
  current_link_speed   = 8.0 GT/s PCIe        ← 根端口当前 Gen3
  current_link_width   = 4
  max_link_speed       = 16.0 GT/s PCIe       ← 根端口能力是 Gen4
  max_link_width       = 4
```

**判定（实测 + 标注推断）**：

| 事实 | 性质 |
|---|---|
| 根端口 `e0:1d.4` 能力 Gen4，实际跑 Gen3 | **实测** |
| 设备 `e2:00.0` 报 `max_link_speed = 8.0 GT/s` | **实测** |
| 宽度两代都是 x4，**没有掉宽度** | **实测** |
| 12 天里 **0 条** link down / retrain / speed change 日志 | **实测**（§5.3） |

**推断（标明）**：`max_link_speed = 8.0 GT/s` 是**设备侧协商后的上限**，说明这块
WD Blue SN570 出厂就是 PCIe 3.0 x4 器件（该型号公开规格为 Gen3 x4），**不是被降级到 Gen3**。
换句话说：**链路没有因为错误而掉速**，但它**本来就跑在 Gen3**，与 Gen4 的上游根端口
之间有**一个速率台阶的再定时（retimer/redriver）负担** —— 这是 Gen4 槽插 Gen3 卡的常见形态，
也是信号完整性最容易出问题的地方之一。

**没有降级 ≠ 没有代价**：`Receiver ID` 类错误意味着**每一次坏信号都要重传整个 TLP**。
51 万次重传在 NVMe 的 4 KB 随机读路径上，是实打实的延迟与带宽损失（本 run 未测带宽，
不在预算内，见 §8）。

### 5.2 ASPM

```bash
cat /sys/module/pcie_aspm/parameters/policy
cat /proc/cmdline
```

```
[default] performance powersave powersupersave 

BOOT_IMAGE=/boot/vmlinuz-6.8.0-138-generic root=UUID=92d635c6-8ab6-4bfb-8d12-2836774e77c5 ro quiet splash
```

**策略是 `default`**（不是 `performance`）。`cmdline` 里**没有** `pcie_aspm=off` 或
`pci=noaer` 之类的覆盖。**ASPM 是本次诊断的第一个可调项**（见 §7 动作 A2）——
ASPM 的 L1/L1.2 低功耗态切换会改变链路电气状态，是本类 RxErr 的常见放大器。

**限制**：`lspci -vv` 需要 root 才能读配置空间（`Capabilities: <access denied>`），
所以**无法确认当前是否真的使能了 L0s/L1**。这是推断的边界，不越过（Rule 25）。

### 5.3 无降速/重训练记录

```bash
journalctl --no-pager -k --since '12 days ago' | grep -icE 'Link (Down|Training)|link speed|retrain|bandwidth'
```

```
0
```

12 天 **零条**。链路状态机没有重训练过 —— 错误全部是**单个 TLP 级的 RxErr**，
没到影响链路训练的程度。

### 5.4 温度与负载（同一刻快照）

```bash
for n in nvme0 nvme1; do for h in /sys/class/nvme/$n/hwmon*/ /sys/class/nvme/$n/device/hwmon*/; do
  [ -d "$h" ] && printf '%-8s temp1_input=%s temp1_max=%s temp1_crit=%s\n' "$n" \
    "$(cat $h/temp1_input)" "$(cat $h/temp1_max)" "$(cat $h/temp1_crit)"; done; done
cat /proc/loadavg
```

```
nvme0    temp1_input=43850 temp1_max=83850 temp1_crit=87850
nvme1    temp1_input=40850 temp1_max=119850 temp1_crit=129850
11.56 15.17 13.56 8/3309 2978085
```

**43.85 °C / 上限 83.85 °C** —— 温升不是问题，**热节流可以排除**。
注意两个 `temp1_max` 阈值差一倍（83.85 vs 119.85），那是厂商固件里的阈值差异，不是故障。

---

## 6. dmesg 被冲掉：量化

```bash
first=$(dmesg | head -1 | grep -oE '^\[ *[0-9]+\.[0-9]+\]' | tr -dc '0-9.')
last=$(dmesg  | tail -1 | grep -oE '^\[ *[0-9]+\.[0-9]+\]' | tr -dc '0-9.')
awk -v f="$first" -v l="$last" -v u="$(cut -d. -f1 /proc/uptime)" 'BEGIN{
  printf "dmesg 首行单调时间        : %.2f s\n", f
  printf "dmesg 末行单调时间        : %.2f s\n", l
  printf "当前 uptime               : %d s (= %.3f 天)\n", u, u/86400
  printf "dmesg 覆盖窗口            : %.1f s (= %.2f 分钟)\n", l-f, (l-f)/60
  printf "dmesg 相对 uptime 起点截断: %.0f s 之前的内容已丢失 (= %.3f 天)\n", f, f/86400
  printf "覆盖率                    : %.4f%%\n", 100*(l-f)/u
}'
uptime -s ; dmesg -T | head -1 ; dmesg -T | tail -1
```

```
dmesg 首行单调时间        : 1100362.71 s
dmesg 末行单调时间        : 1101072.88 s
当前 uptime               : 1101041 s (= 12.744 天)
dmesg 覆盖窗口            : 710.2 s (= 11.84 分钟)
dmesg 相对 uptime 起点截断: 1100363 s 之前的内容已丢失 (= 12.736 天)
覆盖率                    : 0.0645%

2026-10-16 04:16:53
[Wed Oct 28 21:52:11 2026] nvme 10000:e2:00.0: PCIe Bus Error: severity=Correctable, type=Physical Layer, (Receiver ID)
[Wed Oct 28 22:03:57 2026] nvme 10000:e2:00.0:    [ 0] RxErr                  (First)
```

**判据**：`uptime -s = 2026-10-16 04:16:53`，而 `dmesg` 最早只到 `21:52`（今天）。
**12.736 天的内核日志在 dmesg 里已彻底不可见，只剩最后 11.84 分钟（0.0645%）。**
`kernel.dmesg_restrict = 0`，所以**不是权限问题，是环形缓冲真的被这 51 万条冲掉了**。

**幸运的是 journald 是持久化的**，`/var/log/journal` 存在、占 4.1 G，
`journalctl -k` 能回溯到 `Oct 16 04:17:00`（开机那一刻）。**这就是本诊断能做的原因。**

---

## 7. 可用证据通道（全部实际读过一次，`evidence_channels.txt`）

| 通道 | 需要 root？ | 本机实测 | 能回答什么 |
|---|---|---|---|
| `journalctl -k` | **否** | ✅ 回溯到开机（持久化 journal，4.1 G） | 事件时间线、逐设备计数、频率分桶 |
| `dmesg` | 否（`dmesg_restrict=0`） | ⚠️ 仅 11.84 分钟 | 只在"刚刚"有用 |
| `/sys/bus/pci/devices/10000:e2:00.0/aer_dev_correctable` | **否** | ✅ 可读 | **按错误类型分解**（RxErr / BadTLP / BadDLLP / Rollover / Timeout / HeaderOF）—— 最有价值的一条 |
| `/sys/bus/pci/devices/10000:e2:00.0/aer_dev_fatal` | **否** | ✅ 可读 | 是否已升级到不可纠正（本机全 0） |
| `/sys/bus/pci/devices/10000:e2:00.0/aer_dev_nonfatal` | **否** | ✅ 可读 | 是否已升级到 non-fatal（本机全 0） |
| `/sys/bus/pci/devices/10000:e0:1d.4/aer_rootport_total_err_cor` | **否** | ✅ 可读 | 上游根端口侧计数（512 245） |
| `/sys/bus/pci/devices/10000:e2:00.0/current_link_speed` | **否** | ✅ 可读 | 速率是否降级（8.0 GT/s） |
| `/sys/bus/pci/devices/10000:e2:00.0/current_link_width` | **否** | ✅ 可读 | 宽度是否降级（x4） |
| `/sys/class/nvme/nvmeX/{model,serial,firmware_rev}` | **否** | ✅ 可读 | 型号/序列/固件（保修与固件更新用） |
| `/sys/class/nvme/nvmeX/hwmon*/temp1_input` | **否** | ✅ 可读 | 温度（排除热） |
| `lspci -D` / `lspci -t` | 否 | ✅ 可读 | 拓扑与归属 |
| `lspci -vv`（配置空间） | **是** | ❌ `Capabilities: <access denied>` | AER 使能位、LnkCtl ASPM 位 |
| `nvme smart-log` | **是**（`nvme-cli` 也未装） | ❌ MISSING | **SMART：Media Errors / Unsafe Shutdowns / 健康** |
| `smartctl -a` | **是**（`smartmontools` 也未装） | ❌ MISSING | 同上 |

**取回 SMART 的路径（本 run 未执行 —— 要装包 + root，两条都超出本 run 的约束）**：

```bash
sudo apt-get install -y nvme-cli        # 只装包，不加任何常驻/脚本（Rule 35 不受影响）
sudo nvme smart-log /dev/nvme0 | grep -Ei 'critical_warning|media_errors|num_err_log_entries|percentage_used|unsafe_shutdowns|temperature'
sudo smartctl -a /dev/nvme0 | grep -Ei 'Media and Data Integrity Errors|Error Information Log Entries|Available Spare|Percentage Used|CRC'
```

**`Media and Data Integrity Errors` 与 `Error Information Log Entries` 这两个数，是
"物理层错误有没有已经穿过 ECC 落到数据面"的唯一直接判据。本 run 拿不到 —— 这是 Not Done 的第一项。**

---

## 8. 判定表

| # | 问题 | 判定 | 依据（都实测） |
|---|---|---|---|
| 1 | 错在哪一层？ | **PCIe 物理层（Lane 电气/信号完整性），接收侧** | `type=Physical Layer, (Receiver ID)`；`RxErr` 占 97% |
| 2 | 是驱动/OS/gopi 的错吗？ | **不是** | `BadTLP=12`（发送侧错 TLP ≈ 0）；`Timeout=0`；`Uncorrectable/Fatal=0` |
| 3 | 是全局平台问题吗？ | **不是** | 同根复合体下的对照盘 `nvme1n1` 全部**精确为 0** |
| 4 | 只是根端口在转发吗？ | **99.6% 是**；根端口自己另有 **270 次**（0.053%） | 父链 `e2:00.0 → e0:1d.4`；`aer_dev_correctable` 分设备读数 |
| 5 | `10000:00:1d.4` 是第三个故障设备吗？ | **不是，是幻影源（推断）** | 拓扑中不存在 + 总是同秒伴随 + `found no error details` |
| 6 | 量级到底多大？ | **503 103 次事件 / 512 020 次端点错误 / 12.74 天** | 三路口径互校（journal / sysfs / 根端口） |
| 7 | 速率怎么变？ | **平稳且微涨（+20%），与负载正相关，与温度无关** | 小时/日桶；末 30 分钟本 run 空闲时掉到个位数 |
| 8 | 链路降级了吗？ | **没有降到 x4 以下，也没有掉速事件**；但**它本来就跑 Gen3**，上游根端口是 Gen4 | `current_link_*` vs `max_link_*`；12 天 0 条 link 事件 |
| 9 | 会不会丢数据？ | **目前不会**（可纠正），**但没有任何余量保证** | `Uncorrectable=0`、`Fatal=0`、`I/O timeout=0`、`reset=0`、`controller is down=0` |
| 10 | 是热问题吗？ | **不是** | 43.85 °C / 上限 83.85 °C |
| 11 | gopi 能修吗？ | **不能，也不该给 gopi 加代码**（Rule 35） | 这是宿主机硬件/固件层 |
| 12 | 有保修线索吗？ | 有：`WD Blue SN570 2TB`，`Serial 23032A802561`，`Firmware 234200WD` | sysfs（不需要 nvme-cli） |

**风险定级**：**中**（持续、量化、目前在可纠正层，无数据面损伤证据）。
**它不是"忍一忍就过去"的噪音**：51 万次重传已经在付延迟税，而 `RxErr` 的物理机制
（接触电阻/焊点/走线/连接器氧化）**只会往坏的方向走，不会自愈**。

---

## 9. 操作员动作清单（按性价比排序，每条都可直接粘贴）

### A1（零成本，5 分钟）—— 先取证，再动硬件
把当前状态冻结成一份基线，将来任何改动后对比：

```bash
E=~/aer-baseline-$(date +%Y%m%d-%H%M); mkdir -p "$E"
{
  date; uptime; uname -r
  for d in 10000:e2:00.0 10000:e1:00.0 10000:e0:1d.4; do
    echo "##### $d"
    tr '\n' ' ' < /sys/bus/pci/devices/$d/aer_dev_correctable; echo
    tr '\n' ' ' < /sys/bus/pci/devices/$d/aer_dev_fatal; echo
    cat /sys/bus/pci/devices/$d/current_link_speed /sys/bus/pci/devices/$d/current_link_width
  done
  for n in nvme0 nvme1; do echo "##### $n"; for f in model serial firmware_rev; do cat /sys/class/nvme/$n/$f; done; done
  cat /sys/module/pcie_aspm/parameters/policy /proc/cmdline
} | tee "$E/snapshot.txt"
```

**对比方法**：过 24 小时再跑一次，看 `TOTAL_ERR_COR` 的**增量**（不是绝对值）。

### A2（零成本，需 root 一次）—— 关掉 ASPM 再测 24 小时
ASPM 是**唯一一个不需要拆机就能改的变量**，也是 RxErr 最常见的放大器。

```bash
cat /sys/module/pcie_aspm/parameters/policy     # 记下当前值（本机 = default）
# 临时关闭（重启失效，不改任何配置文件 —— Rule 35 不留常驻物）
sudo sh -c 'echo performance > /sys/module/pcie_aspm/parameters/policy'
# 或彻底关闭（需重启）：
#   在 /etc/default/grub 的 GRUB_CMDLINE_LINUX 末尾加 pcie_aspm=off，然后 update-grub && reboot
```

**判据**：24 小时后 `TOTAL_ERR_COR` 增量 **下降 ≥50%** → ASPM 是主要放大项，值得永久关闭
（代价是空载功耗略升）。**不降** → ASPM 无关，进入 A3。

### A3（10 分钟，物理）—— 重插与清触点
WD Blue SN570 没有 DRAM 缓存、功耗低，但 M.2 金手指氧化与螺丝松动是 RxErr 的头号成因。

1. 断电、拔电源、长按电源键 10 秒放电。
2. 拆出 `nvme0`（WD Blue SN570，序列 `23032A802561`），**用无水异丙醇 + 无绒布擦金手指**。
3. **重新插到底**（听到/感到完全就位），**螺丝拧紧**（不要过力）。
4. **与 `nvme1n1` 交换槽位**（把 WD 换到 Fanxiang 现在那个 Gen4 槽，Fanxiang 换过来）。
5. 开机，跑 A1 的 snapshot。

**这是一次测量，不是一次修理**：换槽后
- 错误**跟着 WD 走** → 盘/盘的金手指是根因 → 走 A5 保修。
- 错误**留在原槽** → 主板插槽/走线是根因 → 走 A4。
- 错误**消失** → 是接触不良，重插修好了（A3 完成）。

### A4（若 A3 指向主板）—— 槽位降级使用
把根盘挪到 `00:0e.0` 域下**另一条** PCIe 通道（本机 `e1` 那条是 Gen4 x4，来自 `e0:1a.0`）。
若两块 M.2 都只有这两个槽，则反过来把**出错的盘放到不常写的角色**：把 `/`（1.8T, 97% 使用）
与 `/home/user/workspace`（3.6T, 96% 使用）的负载重新分配，让 WD 只做只读/低频卷。

> **注意**：目前 `/` 已用 **97%**（剩 54 G），`/home/user/workspace` 已用 **96%**（剩 174 G）。
> **两块盘都在容量红线上** —— 这与 AER 无关，但它会让任何"迁移数据"的操作变得危险。
> **先腾空间，再谈迁移。**

### A5（若 A3 指向盘）—— 保修/更换
凭以下信息申请 RMA（都是 sysfs 读到的，不需要 nvme-cli）：

```
Model   : WD Blue SN570 2TB
Serial  : 23032A802561
Firmware: 234200WD
PCI ID  : 10000:e2:00.0  [15b7:5025]
故障描述 : PCIe AER Correctable RxErr 累计 496,718 次 / 12.7 天，速率 27 次/分钟且上升 20%，
           同一主机另一 M.2 (nvme1) 精确 0 次；无 Uncorrectable/Fatal；温度 43.85°C（上限 83.85°C）。
```

**`RxErr` 累计 49.6 万 + 对照组为 0**，这个组合对任何一家厂商的 RMA 都是强证据。

### A6（立即，零成本）—— 把 dmesg 的丢失补回来（可选的正向改动）
`/` 只剩 54 G，而 journald 已占 4.1 G。**不建议**现在扩大 journal 占用。
**建议**：把 AER 行单独长期留存，成本 <1 MB/天：

```bash
# 一次性把历史导成纯文本（约 500 KB 压缩后）—— 落在 session 目录或外部盘，不进仓库
journalctl -k --no-pager -o short-iso | grep -E 'AER|PCIe Bus Error' | gzip > ~/aer-history-$(date +%F).txt.gz
```

### A7（不做的）—— 不要用 `pci=noaer` 把日志关掉
`pci=noaer` 会让内核**不再报告**（AER 中断被屏蔽），错误照旧发生、照旧重传，
**只是你不再知道**。这违反"不静默失败"，且会让 A5 的 RMA 证据链彻底消失。
**任何情况下都不要加这个参数**。

---

## 10. Not Done（如实）

| # | 没做到 | 为什么 | 谁能做 / 怎么取回 |
|---|---|---|---|
| 1 | **SMART 读数**（`Media and Data Integrity Errors`、`Error Information Log Entries`、`Percentage Used`、`Available Spare`、CRC 错误） | `nvme-cli` 与 `smartmontools` **都未安装**（`command -v` 两个都 MISSING），且读 SMART 需要 root —— 装包与 root 两条都超出本 run 的约束（Rule 25 + 预算） | 操作员按 §7 末尾的三条命令，`sudo apt-get install -y nvme-cli` 后即可 |
| 2 | **AER 配置空间位**（`UncorrErr Mask`、`Correctable Error Mask`、`LnkCtl.ASPM` 实际位） | `lspci -vv` 返回 `Capabilities: <access denied>`，配置空间需 root | 同上，`sudo lspci -vv -s 10000:e2:00.0` |
| 3 | **`10000:00:1d.4` 幻影源的机理证实** | 需要 `pcieport` 加 `dyndbg` 打印原始 AER header，要 root + 改内核运行参数 | 本页只能把它标为**推断**（拓扑不存在 + 同秒伴随 + `found no error details`），证据强度够定性，不够定论 |
| 4 | **带宽/延迟损失量化**（51 万次重传值多少 %） | Rule 27 明令禁止**造 IO 负载**去"复现"或度量链路问题；且本 run 预算 60 分钟，跑 fio 必然超时 | 操作员可选（非必需）：`fio --name=r --rw=randread --bs=4k --iodepth=32 --size=4G --runtime=60 --filename=/tmp/fio.dat` —— **这是负载，不是诊断，是否要跑由操作员定** |
| 5 | **ASPM 关闭后的对照测量** | 需要 root 改运行时参数 + 等 24 小时 | 操作员执行 A2，24 小时后对比 A1 快照 |
| 6 | **物理检查（重插/换槽）** | 需要动手拆机 | 操作员执行 A3 |
| 7 | **根盘超容量（97%）的处置** | 与本 bug 无关，但会阻碍任何迁移操作；不属于本行范围 | 另开工单，或操作员自行清理 |

---

## 11. 复现本页每一条结论所需的完整命令集

```bash
# 0. 时间与开机
uptime -s ; uptime ; uname -r ; cat /proc/cmdline

# 1. 一次完整事件（4 行结构）
journalctl --no-pager -k --since '48 hours ago' | grep -A3 -m1 'AER: Correctable error message received from 10000:e2:00.0'

# 2. 错误类型分解（最有价值的一条）
cat /sys/bus/pci/devices/10000:e2:00.0/aer_dev_correctable
cat /sys/bus/pci/devices/10000:e2:00.0/aer_dev_fatal
cat /sys/bus/pci/devices/10000:e2:00.0/aer_dev_nonfatal

# 3. 对照盘（必须为 0）
cat /sys/bus/pci/devices/10000:e1:00.0/aer_dev_correctable
cat /sys/bus/pci/devices/10000:e0:1a.0/aer_rootport_total_err_cor

# 4. 归属（父链）
readlink -f /sys/bus/pci/devices/10000:e2:00.0
ls /sys/bus/pci/devices/10000:e0:1d.4/ | grep -E '^10000:'

# 5. 速率（按小时 / 按天 / 按分钟）
journalctl --no-pager -k --since '48 hours ago' -o short-iso | grep 'AER: Correctable error message received from 10000:e2:00.0' | cut -c1-13 | sort | uniq -c
journalctl --no-pager -k -o short-iso | grep 'AER: Correctable error message received from 10000:e2:00.0' | cut -c1-10 | sort | uniq -c
journalctl --no-pager -k --since '30 min ago' -o short-iso | grep 'AER: Correctable error message received from 10000:e2:00.0' | cut -c1-16 | sort | uniq -c

# 6. 链路速率/宽度
for d in 10000:e2:00.0 10000:e1:00.0 10000:e0:1d.4; do
  echo "--- $d ---"
  cat /sys/bus/pci/devices/$d/current_link_speed /sys/bus/pci/devices/$d/current_link_width \
      /sys/bus/pci/devices/$d/max_link_speed /sys/bus/pci/devices/$d/max_link_width
done

# 7. dmesg 丢失量化
dmesg | head -1 ; dmesg | tail -1 ; cut -d. -f1 /proc/uptime ; uptime -s

# 8. 是否已升级 / 是否影响 IO
journalctl --no-pager -k | grep -cE 'severity=Uncorrectable|severity=Fatal'
journalctl --no-pager -k | grep -cE 'I/O timeout|controller is down|did not become ready'
journalctl --no-pager -k --since '12 days ago' | grep -icE 'Link (Down|Training)|retrain|link speed'

# 9. 盘身份（RMA 用）
for n in nvme0 nvme1; do for f in model serial firmware_rev state; do cat /sys/class/nvme/$n/$f; done; done

# 10. 温度
for n in nvme0 nvme1; do for h in /sys/class/nvme/$n/hwmon*/ /sys/class/nvme/$n/device/hwmon*/; do
  [ -d "$h" ] && printf '%-8s %s\n' "$n" "$(cat $h/temp1_input)"; done; done
```

---

## 12. 原始证据文件（本 run session 目录）

路径：`/home/user/.gopi/sessions/01a18273-f478-71e4-bdcc-908cf81092fd/evidence/`

| 文件 | 内容 |
|---|---|
| `event_block.txt` | 一次完整 AER 事件的 4 行原样结构 |
| `aer_registers_all.txt` | 三设备（根盘/对照盘/根端口）× correctable+fatal+nonfatal+rootport 全部寄存器 |
| `control_disk.txt` | 对照盘与它上游桥的零值 |
| `parent_chain.txt` | sysfs 父链（归属判据） |
| `lspci_tree.txt` / `lspci_tree_full.txt` / `lspci_devs.txt` | 拓扑与设备身份 |
| `lspci_vv_e2.txt` | 配置空间被拒（`access denied`）的原始输出 |
| `aer_hourly_48h.txt` | 近 48 小时逐小时分桶（49 行） |
| `aer_daily.txt` | journal 全期逐日分桶（13 行） |
| `aer_minute_30m.txt` | 近 30 分钟逐分钟分桶（速率与负载的相关性） |
| `rate_trend.txt` / `rate_avg.txt` | 首 24h vs 末 24h；平均速率 |
| `totals_crosscheck.txt` / `crosscheck_math.txt` | 三路口径互校与会计算式 |
| `dmesg_loss.txt` | dmesg 覆盖率 0.0645% 的计算过程 |
| `dmi.txt` | 机型/BIOS（`NP5x_6x_7x_SNx`, INSYDE 1.07.05LS1, 2023-02-20） |
| `m2_slots.txt` / `bdf_map.txt` | 两块 M.2 的槽位、父桥、速率、宽度；`nvmeX ↔ BDF` 映射 |
| `nvme_temps.txt` | 温度与阈值 |
| `kernel_cmdline.txt` | 无 ASPM/AER 覆盖参数 |
| `evidence_channels.txt` | 每条证据通道"是否需要 root"的实测结果 |
| `phantom_source_block.txt` | `10000:00:1d.4` 幻影源的原始 4 行 |
| `port_own_errors.txt` / `port_own_errors_48h.txt` | 根端口自身错误的类型分布 |
| `no_error_details_48h.txt` | `found no error details` 按源计数 |
| `aer_src_48h.txt` / `aer_report_source.txt` | 按"接收源"拆分的 AER 计数 |
| `clock_snapshot.txt` | dmesg 单调时钟与 `/proc/uptime` 的同命令快照 |
| `nvme_other_48h.txt` | **空文件** —— 48 小时内 nvme 除 AER 外没有任何一行（无 IO 错误） |

---

## 13. 与 gopi 的关系（写清楚，免得下次有人想"用代码修它"）

**gopi 不加任何代码、脚本、常驻进程、配置键**（Rule 35）。理由四问：

1. **没有它什么会坏？** 什么都不会坏 —— 宿主机 PCIe 链路错误与 gopi 的任何功能无因果关系。
   它污染的是 `dmesg`，而 gopi 也不读 `dmesg`。
2. **为什么不能在进程内修？** 物理层信号完整性问题，任何用户态代码在原理上都触不到。
3. **谁负责回收它的生命周期？** 没有人 —— 所以它**不应该存在**。
4. **下一个人发现它在跑会看到什么？** 这正是 `pci=noaer` 与自动清理脚本的反面教材：
   一个静默运行、掩盖真实硬件劣化的东西，会让下一个诊断者在**错误的前提下**排查
   （本仓库已有这个形状的先例：HP worker autostart 的 cron 线掩盖了 worker 反复死亡）。

**唯一需要留在 gopi 这边的，是这份报告本身**（一个 devdoc 文件，没有索引行、没有门禁、
没有依赖），以及**一段记忆**（"这台机的根盘在物理层劣化，看 AER 就读
`/sys/bus/pci/devices/*/aer_dev_correctable`，不要先怀疑 gopi"）。
