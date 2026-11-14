<!-- doc-class: normative -->

> **Retired mechanism (document-pipeline teardown, slice 3/5).** `tools/docclass/`
> was deleted: a doc's truth class now lives in its own line-1
> `<!-- doc-class: … -->` header, written by the write path
> (`internal/tools/header_hook.go`) and by no generator. The
> `python3 tools/docclass/…` commands quoted below are the RECORD of what a past
> run executed — they cannot be re-run, and no document may be classified by
> running them.

# devdoc CJK Segmentation Survey — Go 中文分词器选型（可据以决策）

Status: **survey + recommendation** — 本分支只新增本文档，**不改 gopi 检索代码、不 go get 进主仓库**。
Base: worktree `/home/user/workspace/gopi-01a11390ce7e76e089f00e55c22f0dfa`, branch `task/01a11390ce7e76e089f00e55c22f0dfa`.

## 0. 一句话结论

**用 `github.com/go-ego/gse v1.1.0`，形态为「`-tags ne` + 自嵌全量 `zh_s` 词典」，
净增量 +4.78 MB（gopi 30.96 MB → 约 35.7 MB），不要裁剪词典。**

两半都要说清楚，因为**砍错哪一半都会付代价**：

- **`-tags ne` 是必须的**：gse 上游默认的 `//go:embed` 是**无条件**的，选它而不加 tag 意味着
  **强制 +32.32 MB**（gopi 二进制 30.96 MB → 63.28 MB，+104%），其中 23.7 MB 是日语词典、
  3.5 MB 只买繁体、6.2 MB 是给 TF-IDF 摘关键词用的 IDF —— **gopi 一个都用不上**。
- **但 `-tags ne` 之后不要顺手把词典也裁到 2 万字**：实测 20 000 词时
  `真关键词被双字组挤出MaxKeywords名额` 会被切成 `真关键 词 被 双字 组挤 出 …`
  —— **`真关键/词` 粘连、`缩/小` 崩成单字，正好是本任务要修的那个失败模式**（§3.3 有对照表）。
  100 000 词基本恢复，352 279 词（全量）全对。所以**「省 0.12 MB」是一个负收益的优化**：
  它把 +1.59 MB 的形态换成了 +4.78 MB 的形态，差的 3.2 MB 买的就是 `关键词` 这个词本身。

> **为什么本节曾经写反过（诚实记录）**：本节的第一版写于裁剪词典实验（§3.3）**之前**，
> 当时只有 +0.12 MB 这个数字，还没有质量对照，于是把「体积最小」误当成了结论。
> §3.3 的实测把它推翻了，本节随之改写。**留下这段是因为它本身是一条教训**：
> 体积是能先量出来的，质量要等样本跑完才知道，**先量出来的那个不能替后者下结论**。

**普查数字的生成器（Rule 38）**：本文所有依赖树数字来自
`evidence/01-dep-trees.sh`（`go list -m all`，原始输出 `evidence/01-dep-trees.out.txt`）；
所有体积数字来自 `stat -c %s`，原始命令与输出见第 3 节与 `evidence/`。
测量 commit：本分支 tip（`git log -1 --format=%H`，见文末 `## Provenance`），
测量时间 2026-10-07，Go 1.24.5，`CGO_ENABLED=0`，`GOPROXY=https://goproxy.cn`。

---

## 1. 候选表 + 每个候选的真实依赖树

**方法（Rule 31：未跑过的命令不算交付）**：每个候选建一个临时模块，`cmd/agent/main.go` 里
**blank import** 该库，再 `go mod tidy` + `go list -m all`。

> **为什么必须 blank import**：第一次尝试只跑 `go get <mod>@latest` 然后 `go list -m all`，
> 六个候选**全部返回 1 个模块**（只有 probe 自己）—— `go get` 加了 `require` 但没有 import，
> `go mod tidy` 把它删了。这是一个**看起来成功了、什么都没测到**的假绿。
> 修法是让 main.go 真的 import 那个包，tidy 才会解析库自己的 `require`。
> 这条踩坑值得记住：**依赖树探针不 import 就是空转**。

### 1.1 候选总表

| 候选 | 版本（实测 `go list -m`） | 依赖树模块数 | 纯 Go / 无 CGO | 许可 | 判定 |
|---|---|---|---|---|---|
| **`github.com/go-ego/gse`** | **v1.1.0** | **4**（自+3） | ✅ | **Apache-2.0** | ⭐ **推荐** |
| `github.com/yanyiwu/gojieba` | v1.4.7 | 2 | ❌ 需 CGO | MIT | ❌ 否决 |
| `github.com/wangbin/jiebago` | v0.3.2 | 2 | ✅ | MIT | ❌ 否决 |
| `github.com/huichen/sego` | v0.0.0-20210824061530-c87651ea5c76 | 5 | ✅ | Apache-2.0 | ○ 备选 |
| `github.com/blevesearch/segment` | v0.9.1 | 2 | ✅ | Apache-2.0 | ❌ 否决 |
| `github.com/fumiama/jieba` | v0.0.0-20260103160001-5c1e948fb8d5 | **24** | ✅ | **AGPL-3.0** | ❌ 否决（双重） |

### 1.2 逐个的 `go list -m all` 原始输出

**gse（4 模块）** —— 生成器 `evidence/01-dep-trees.sh`：
```
probe/gse
github.com/go-ego/gse v1.1.0
github.com/vcaesar/cedar v0.50.0
github.com/vcaesar/tt v0.40.0
```
叶子确证（读两个直接依赖自己的 go.mod，不是猜）：
```
$ cat $(go env GOMODCACHE)/github.com/vcaesar/cedar@v0.50.0/go.mod
module github.com/vcaesar/cedar
go 1.17
require github.com/vcaesar/tt v0.40.0

$ cat $(go env GOMODCACHE)/github.com/vcaesar/tt@v0.40.0/go.mod
module github.com/vcaesar/tt
go 1.17
```
`vcaesar/tt` 有 **0 个 require** —— 真正的零依赖叶子。所以 gse 的**全部**传递依赖深度是 3。

**fumiama/jieba（24 模块）** —— 全部 20 个间接依赖，无一可省：
```
probe/fumiama
github.com/RoaringBitmap/roaring v0.4.23
github.com/blevesearch/bleve v1.0.14          ← 一个全文搜索引擎
github.com/blevesearch/go-porterstemmer v1.0.3
github.com/blevesearch/mmap-go v1.0.2          ← 系统调用/内存映射层
github.com/blevesearch/segment v0.9.0
github.com/blevesearch/snowballstem v0.9.0
github.com/blevesearch/zap/v11 v11.0.14        ← 五代索引格式，全都拉进来
github.com/blevesearch/zap/v12 v12.0.14
github.com/blevesearch/zap/v13 v13.0.6
github.com/blevesearch/zap/v14 v14.0.5
github.com/blevesearch/zap/v15 v15.0.3
github.com/couchbase/vellum v1.0.2
github.com/fumiama/jieba v0.0.0-20260103160001-5c1e948fb8d5
github.com/glycerine/go-unsnap-stream v0.0.0-20181221182339-f9677308dec2
github.com/golang/protobuf v1.3.2
github.com/golang/snappy v0.0.1
github.com/mschoch/smat v0.2.0
github.com/philhofer/fwd v1.0.0
github.com/steveyen/gtreap v0.1.0
github.com/tinylib/msgp v1.1.0
github.com/willf/bitset v1.1.10
go.etcd.io/bbolt v1.3.5                        ← 嵌入式数据库（写盘）
golang.org/x/sys v0.0.0-20200202164722-d101bd2416d5   ← 2020-02 的旧版
```
gse 是 **3 个模块**，这个是 **23 个**（含 bleve 全文索引引擎与 bbolt 嵌入式数据库）。
对一个只回答「这串中文怎么切」的问题，这是 8 倍的依赖面。

**gojieba（2 模块）**：`probe/gojieba`, `github.com/yanyiwu/gojieba v1.4.7` —— 看着最干净，
但它把 21 MB 的 C++ cppjieba 代码用 `deps/` 目录**塞进同一个模块**，所以依赖树看不见它。

**jiebago（2 模块）**：`probe/jiebago`, `github.com/wangbin/jiebago v0.3.2`。

**sego（5 模块）**：
```
probe/sego
github.com/adamzy/cedar-go v0.0.0-20170805034717-80a9c64b256d
github.com/adamzy/sego v0.0.0-20151004184924-5eab9a44f8e8
github.com/huichen/sego v0.0.0-20210824061530-c87651ea5c76
github.com/issue9/assert v1.4.1
```
`issue9/assert` 只出现在 `utils_test.go`（已核实），是测试依赖 → 生产依赖实为
`adamzy/cedar-go` 一个，但 `adamzy/sego`（2015-10-04）是同一个 fork 家族的旧名，
一个库同时 require 两个名字的孪生兄弟，是维护不活跃的信号。

**segment（2 模块）**：`probe/segment`, `github.com/blevesearch/segment v0.9.1`。

### 1.3 维护活跃度（读模块缓存里的 `.info`，不是我记忆里的日期）

```
github.com/go-ego/gse            v1.1.0                                  2026-09-12
github.com/yanyiwu/gojieba       v1.4.7                                  2026-03-08
github.com/fumiama/jieba         v0.0.0-20260103160001-5c1e948fb8d5      2026-01-03
github.com/blevesearch/segment   v0.9.1                                  2022-12-19
github.com/huichen/sego          v0.0.0-20210824061530-...               2021-08-24
github.com/wangbin/jiebago       v0.3.2                                  2015-05-08  ← 11 年前
```

---

## 2. gse 实测切词样本（≥10 条）

**这是真跑的**：`CGO_ENABLED=0` 构建的临时模块，源码 `evidence/02-…` 对应的
`/home/user/workspace/_tmp/cjksurvey/gseprobe/main.go`，完整输出
`evidence/02-gse-cuts.txt`。跑法：
```bash
CGO_ENABLED=0 go build -o probe-full .   # gse 上游默认 embed 形态
./probe-full zh_s                        # LoadDictEmbed("zh_s")
```

15 条样本，每条给出 HMM/无 HMM/CutSearch 三种模式（`SRCH` 是检索模式，
它额外吐出子串，正是 FTS5 索引侧需要的粒度）：

| # | 输入 | `Cut(hmm=true)` | `CutSearch` |
|---|---|---|---|
| 01 | 逐步缩小的AND算法检索 | `逐步 缩小 的 and 算法 检索` | 同左 |
| 02 | memory预取的分词问题 | `memory 预取 的 分词 问题` | 同左 |
| 03 | 中文分词要用jieba | `中文 分词 要用 jieba` | 同左 |
| 04 | gopi 是纯go零cgo | `gopi   是 纯 go 零 cgo` | 同左 |
| 05 | 真关键词被双字组挤出MaxKeywords名额 | `真 关键词 被 双字 组 挤出 maxkeywords 名额` | `真 关键 关键词 被 …` |
| 06 | FTS5的unicode61分词器把连续中文当成一个token | `fts5 的 unicode61 分词器 把 连续 中文 当成 一个 token` | `… 分词 分词器 …` |
| 07 | 这个bug的根因是写入侧与查询侧的分词粒度不一致 | `这个 bug 的 根因 是 写入 侧 与 查询 侧 的 分词 粒度 不 一致` | 同左 |
| 08 | 深度学习模型部署到生产环境的性能优化方案 | `深度 学习 模型 部署 到 生产 环境 的 性能 优化 方案` | 同左 |
| 09 | 用gse内嵌词典后二进制体积增加了多少MB | `用 gse 内嵌 词典 后 二进制 体积 增加 了 多少 mb` | `… 进制 二进制 …` |
| 10 | 操作者今天下午三点要在会议室评审这个方案 | `操作者 今天下午 三点 要 在 会议室 评审 这个 方案` | `操作 作者 操作者 今天 天下 下午 今天下午 …` |
| 11 | 自然语言处理中命名实体识别的准确率 | `自然语言 处理 中 命名 实体 识 别的 准确率` | `自然 语言 自然语言 … 准确 准确率` |
| 12 | 他说的那句话我完全没听懂 | `他 说 的 那句话 我 完全 没 听懂` | 同左 |
| 13 | 南京市长江大桥 | `南京市 长江大桥` | `南京 京市 南京市 长江 大桥 长江大桥` |
| 14 | 结婚的和尚未结婚的 | `结婚 的 和尚 未 结婚 的` | 同左 |
| 15 | 中华人民共和国成立了 | `中华人民共和国 成立 了` | `中华 华人 人民 共和 共和国 中华人民共和国 成立 了` |

**三条关键读法**：

1. **中英混排正确**（02/04/05/06/09）：`memory预取` → `memory` + `预取`；
   `MaxKeywords` → `maxkeywords`（小写化）；`FTS5的` → `fts5` + `的`。这正是
   gopi 的 `cjkGrams2` 做不到的（它对整段 CJK 开 2 字窗口，把 ASCII 黏在一起或切碎）。
2. **技术术语切分正确**（05/06/07）：`关键词`、`双字`、`挤出`、`分词器`、`根因`、
   `写入`、`查询`、`粒度` 全部是**词**，而不是「双字 + 字」（如 `双字/组挤/出`）。
   **第 3.3 节**对照裁剪词典时会看到这些词恰好先崩。
3. **歧义句切分正确**（13/14/15）：`南京市/长江大桥` 不是 `南京/市长/江大桥`；
   `结婚/的/和尚/未/结婚/的` 正确解析了经典歧义；`中华人民共和国` 作为一个词。
   `CutSearch` 额外给出 `中华/华人/人民/共和/共和国` —— **这正是索引侧需要的**。

**模式差异（HMM 的真正价值）**：
- 02 号：`Cut(hmm=true)` = `memory 预取 的 分词 问题`；`Cut(hmm=false)` = `memory 预 取 的 分词 问题`。
- 07 号：HMM = `根因`；无 HMM = `根 因`。
**未登录词（词典里没有的词）靠 HMM/Viterbi 兜底**，这是 `cjkGrams2` 的 2 字窗口
原则上做不到的事。**建议索引侧用 `Cut(s, true)`，检索扩展侧用 `CutSearch(s, true)`** ——
前者给精确词，后者额外给子串，两者都写进 FTS5 的 gram 列即可覆盖查询侧的短语查询。

### 2.1 性能（同一次真跑，`evidence/05-gse-perf.txt`）

```
LoadDictEmbed(zh_s) wall: 1.175659293s        ← 352 279 词，每次进程启动都要付
Cut x20000 on 153-rune text: 3.323135866s total, 166.156µs/op, 72 tokens
throughput: 920817 runes/sec
Cut x500 short (7 runes): 2.524076ms, 2.524ms/500 = 5.048µs/op
```
**920 k 汉字/秒** —— 对 gopi 的用途（一次索引几百字 / 一次查询几十字）余量极大。
真正要注意的是 **800 ms ~ 1.2 s 的启动加载**（第 3 节的裁剪形态把它压到 **28 ms**）。

---

## 3. 内嵌词典后的二进制体积实测增量

**方法**：`static` 真实字节数（不是 `du`，不是肉眼估），全部 `CGO_ENABLED=0`。

### 3.1 原始字节（生成器：`stat -c %s`，见上文命令）

| 构建形态 | 二进制字节 | 相对 hello-world | 相对同参数无词典 |
|---|---|---|---|
| (a) hello-world `-s -w` | 1 007 800 | — | — |
| (b) gse 代码，`-tags ne`（gse 无内嵌） | 2 547 896 | +1 540 096 (**+1.47 MB**) | — |
| (c) (b) + 自嵌 gse 的 `s_1.txt` 5 117 886 字节 | 7 561 400 | +6 553 600 (+6.25 MB) | +5 013 504 (**+4.78 MB**) |
| (d) (b) + 自嵌裁剪 20 000 词（227 201 字节） | 2 670 776 | +1 662 976 (+1.59 MB) | +122 880 (**+0.12 MB**) |
| (e) gse 上游默认 embed（`go build` 不带 tag） | 36 002 382 | +34 435 352 (**+32.84 MB**) | — |
| (e') (e) 加 `-s -w` | 34 902 200 | +33 894 400 (**+32.32 MB**) | — |
| (f) gopi 现二进制（参照物） | 30 956 963 | — | — |

**gse 上游 embed 到底嵌了什么**（读 `dict_embed.go`，5 个 `//go:embed`，
**全部无条件**，与是否 CGO 无关）：
```
data/dict/jp/dict.txt          23 713 176   ← 日语词典（gopi 用不到）
data/dict/zh/s_1.txt            5 117 886   ← 简体中文词典（352 279 词）
data/dict/zh/t_1.txt            3 525 862   ← 繁体（gopi 用不到）
data/dict/zh/idf.txt            6 200 957   ← TF-IDF 权重（gopi 用不到）
data/dict/zh/stop_tokens.txt        9 106
                               ----------
                               38 566 987 字节，而实测二进制增量 33.9 MB
                               （embed 的数据会被压缩进 .rodata，所以小于原始和）
```

### 3.2 三个必须知道的数字

1. **选 gse 而不加任何 tag = +32.32 MB**，gopi 变成 **63.28 MB**（+104%）。
   对「一个二进制拷到机器就能跑」的产品属性，这不可接受。
2. **`-tags ne` 之后 gse 代码本身只要 +1.47 MB**。`dict_no_embed.go` 是 gse
   自己提供的官方开关（`//go:build ne`），不是我们的 hack。
3. **自嵌裁剪词典只要 +0.12 MB** —— 因为词典是**可选的**：
   `LoadDictStr(string)` 接受的是**词典内容**，不是路径（已读源码确证），
   所以消费者可以 `//go:embed` 自己的词典然后 `LoadDictStr(myDict)`。
   ⚠️ **但这是「能省多少」的下界，不是「该省多少」的答案** —— 裁剪的质量代价在
   §3.3 实测：20 000 词会把 `关键词` 切碎。**结论是这几个数字里最不重要的那个。**

### 3.3 词典越大越好 —— 裁剪是有代价的（实测，`evidence/03/04`）

同一份 `cmd/agent/main.go`、同一次构建，只换 `subset.txt`：

| 词典 | 词数 | 二进制（`-s -w`） | `逐步缩小的AND算法检索` | `真关键词被双字组挤出MaxKeywords名额` |
|---|---|---|---|---|
| 裁剪 20 000 词 | 20 000 | 2 670 776 | `逐步 缩 小 的 and 算法 检索` ❌ | `真关键 词 被 双字 组挤 出 maxkeywords 名额` ❌ |
| 裁剪 100 000 词 | 100 000 | 3 612 856 | `逐步 缩小 的 and 算法 检索` ✅ | `真关键 词 被 双字 组 挤出 maxkeywords 名额` ⚠️ |
| **完整 `s_1.txt`** | **352 279** | **7 561 400** | ✅ | ✅ `真 关键词 被 双字 组 挤出 maxkeywords 名额` |

20 000 词时 `缩/小` 崩成单字、`真关键/词` 粘连；100 000 词时基本恢复。
**结论：不要贪图 0.12 MB。用完整 `s_1.txt`（+4.78 MB）是完全值得的**，
它给出第 2 节那张表的全部质量。**gopi 的推荐形态是 (c)：`-tags ne` + 全量 `zh_s`，
净增量 +4.78 MB，gopi 从 30.96 MB → 约 35.7 MB（+15%）。**

### 3.4 内存与启动（同一次真跑）

```
完整 zh_s（352 279 词）: LoadDictEmbed wall=800.5ms   HeapInuse delta=159.07 MB
裁剪 20 000 词        : LoadDictStr    wall=28.2ms    HeapInuse delta= 6.14 MB
```
**159 MB 常驻堆**是 TRIE 展开的真实代价，比二进制体积更值得注意 ——
gopi 是 CLI（进程短命），可接受；但**如果将来进入常驻路径必须重新评估**。
gse **没有任何持久化 / 序列化 API**（`grep` 全部导出方法，无 `Save`/`Marshal`），
所以「预展开词典缓存到磁盘」这条优化路**不存在**，只能每次启动重放 800 ms。
裁剪形态（20 000 词）的 28 ms / 6 MB 提示：**词典规模是可调的旋钮**，
将来若 800 ms 成为问题，可在质量与启动之间选一个中间点。

---

## 4. Rule 35 四问逐条回答

Rule 35 的对象是「**持久依赖**」（进程退出后仍存在、或生命周期不在我们手里的东西）。
本方案的诚实回答是：**本方案不新增任何持久依赖，因此四问的答案是「不需要过闸」。**
逐条：

**问 1：没有它会坏什么？（要点名一个观察到的失败）**
有具体失败可点。`internal/memoryprefetch/extract.go` 的注释自己承认
"a heuristic stand-in for a tokenizer this project does not have"，并且
`KindCJKBigram` 的文档字符串写着 "It is a fallback, not a tokenizer: it is recorded
as a degradation on the set."
后果是**真关键词被双字组挤出 `MaxKeywords` 名额** ——
第 3.3 节的对照就是它的机器可读形态：`真关键词` 被切成 `真关键/词`，
而正确的 `关键词` 挤不进去。这不是推测，是同一段文本在两种分词器下的实测差异。
**但请注意**：这个失败**不需要新依赖来修** —— 见问 2。

**问 2：为什么不能在进程内修？**
**因为它本来就能，而且这就是本方案的形状。** 本方案不引入：
- ❌ 常驻进程 / daemon —— gse 是**库**，链接进 gopi 自己的二进制，无进程。
- ❌ 脚本 / CLI helper —— 无。
- ❌ 新配置文件 / 新 config key —— **无**：词典走 `//go:embed` 编译进二进制，
  **不读任何文件路径**（与 gojieba 相反，见第 6.2 节）。
- ❌ 新数据库 / 表 / 锁文件 / socket / mount / 环境变量 —— 全部无。
- ❌ crontab / systemd unit / timer —— 无。
- ❌ 运行时需要的新外部命令 —— 无。

**唯一持久化下来的东西，是一行 `go.mod` 里的 `require`**：`gse` + 它的两个
传递依赖，共 3 个模块，全部是**编译期**依赖，`go mod verify` 可校验，随仓库走。
这就是 Rule 35 说的「**已经跑着的东西**」—— gopi 的 `go.mod` 里已经有
`modernc.org/sqlite`（依赖树比 gse 深得多），加 gse 不改变产品形态。

**问 3：谁拥有它的生命周期？具体怎么清理？**
**go module 机制拥有它**：`go.mod` / `go.sum` 是所有者，
`go mod tidy` 是回收路径（删掉 import，tidy 就摘掉 require，二进制里一个字都不剩）。
**无需我们写任何清理钩子** —— 这正是 Rule 35 偏好的形状。
反面对照：如果当初选的是「跑一个本地 jieba 服务」，那才需要问 3 的完整答案
（谁启动、谁 kill、残留怎么办），而那个方案已经被否决（第 6.2 节）。

**问 4：下一个人发现它在跑、却不知道为什么，会看到什么？**
他看到的是 `go.mod` 里一行 `require github.com/go-ego/gse v1.1.0`，
和**本文档**（本文件，`devdoc/` 下按 Rule 36 已在 `DOC-INDEX.tsv` 分类）。
没有任何东西在「跑」—— 没有后台进程、没有定时任务、没有需要在某台机器上被设好的
环境变量。**这正好满足 Rule 35 「默认拒绝并自我解释」的要求**：
一个下一个人无法识别、也无法安全移除的持久依赖，比它解决的问题更糟 ——
而这里他能 `go mod tidy` 一行移除，且移除后**编译期就会报错**（不是静默降级）。

**四问的诚实结论**：本方案**不触发 Rule 35 的闸**，因为它是 `go.mod` 的一行
编译期依赖，不是「我们创造的一个持久物」。**唯一需要额外声明的一件事**：
`-tags ne` 这个构建标签必须写进构建声明（Rule 12 的 `"build"` 命令），
否则默认 embed 形态会静默地把二进制撑到 63 MB —— 那才是这个方案真正的风险点，
它**不是依赖风险，是构建配置风险**。已写入第 7 节的落地清单。

---

## 5. 三条路径的取舍：自己实现 jieba / 编译 jieba(CGO) / 用 Go 现成库

### 路径 A：自己实现（自研 jieba / 自研分词器）

**代价实测**：gse 的核心算法是三件事 —— 双数组 TRIE（`vcaesar/cedar`）、
DAG + 动态规划求最短路径、HMM/Viterbi 处理未登录词。gse 的**词典数据**是
352 279 个词条 + 词频 + 词性（5.1 MB），加上 HMM 的模型参数
（`gse/hmm` 目录 4.6 MB）。自己实现意味着**要么自己产出这份词典**（不可能 —
jieba 的词典是多年人工+语料积累的资产），**要么抄一份**（那就等于抄 gse/jieba，
还不如直接用）。

**裁决：否决。** 这不是「能不能写」的问题，是「**写下的是代码，抄不来的资产是词典**」。
把 5 MB 词频词典和 HMM 参数表重新发明一遍，收益为零、风险为「又一个坏分词器」。
唯一的自研空间是**词典裁剪**（第 3.3 节），而那个用 `LoadDictStr` 就够了，不需要实现分词器。

### 路径 B：编译 jieba（CGO）

**实测（Rule 31：真跑了，不是断言）**：
```bash
$ CGO_ENABLED=0 go build ./   # 只 import 不调用
exit=0                                     # ← 成功了！

$ cat > main.go <<'EOF'
import "github.com/yanyiwu/gojieba"
func main() { j := gojieba.NewJieba(); _ = j }
EOF
$ CGO_ENABLED=0 go build -o gj0 .
# probe/gojieba
./main.go:10:15: undefined: gojieba.NewJieba  # ← 只有真调用才暴露
$ GOFLAGS=-mod=mod go build -o gj .        # CGO 打开
exit=0                                       # 成功（本机有 g++）
$ ./gj                                       # 从任意 cwd 跑
[南京市 长江大桥]
```

**否决理由（三条，按严重性）**：

1. **`CGO_ENABLED=0` 下是"静默空编译"** —— 包能编译通过，**所有导出符号消失**，
   只有真正调用时才报 `undefined`。这比编译报错危险得多：一个 CI 用 CGO=0 构建、
   另一个用 CGO=1，产物行为不同**而构建都是绿的**。gopi 的硬约束是
   `CGO_ENABLED=0` 必须通过（Rule 12 的构建声明就是 `CGO_ENABLED=0 go build`）。
2. **词典路径在编译期被烧进二进制**。读 `config.go`：
   ```go
   func getCurrentFilePath() string {
       _, filePath, _, _ := runtime.Caller(1)   // ← 编译期的源码路径
       return filePath
   }
   DICT_DIR = path.Join(path.Dir(getCurrentFilePath()), "deps/cppjieba/dict")
   ```
   实测 `strings gj | grep cppjieba/dict` 可看到路径确实在二进制里，
   而且它指向**构建机的 module cache**（本机是 `/home/user/go/pkg/mod/...`，只读）。
   `NewJieba` 检查这 5 个路径存在，不存在就 **panic**。
   所以：**换一台机器、换一个 GOPATH、清一次 module cache，二进制就 panic**。
   这与 gopi 「拷一个二进制就能跑」的属性直接冲突，而且失败形态是**运行时 panic**。
3. **依赖前置条件**：需要 C++ 工具链（`g++` + C++11）、21 MB 的 `deps/` C++ 源码、
   以及 CGO 打开。`CGO_ENABLED=0` 还能构建通过这件事本身，说明它的构建契约是**模糊的**。

**旁证，不是理由**：本机（peerbox）恰好有 g++，所以 B 路「编译通过」是**环境偶然**，
不是可移植性证据。

### 路径 C：用 Go 现成库 —— 推荐 gse

**这是第 1~3 节的实测所指**。选它的理由按重要性排序：

1. **依赖树最浅（4 个模块，叶子零依赖）** —— 全部候选里唯一「3 个模块且叶子是真正的零依赖」的。
2. **Apache-2.0** —— 与 gopi 现有依赖谱系相容（第 6.3 节）。
3. **算法正确**：双数组 TRIE + DAG + 最短路径 + HMM/Viterbi，第 2 节 15 条样本实测。
4. **词典可裁剪**（`-tags ne` + `LoadDictStr`），把 +32.32 MB 压到 +4.78 MB。
5. **自包含**：不读任何外部文件路径，拷到任何机器都能跑。
6. `CGO_ENABLED=0` 构建通过并**真跑出结果**（第 2 节）。
7. 维护活跃：v1.1.0 发布于 2026-09-12（一个月前）。

**gse 的诚实缺点（一并写下）**：
- 上游默认 embed 是**无条件**的（`dict_embed.go` 无 build tag 开关，
  只有 `ne` 这个**反向** tag），所以**必须**在构建命令里写 `-tags ne`，
  否则静默 +32 MB。这是一个 footgun，第 7 节把它列成落地必做项。
- **159 MB 常驻堆 / 800 ms 启动加载**（全量 zh_s）。CLI 可接受，常驻需重评。
- 无序列化 API，无法缓存展开后的 TRIE。
- 词典里的 `idf.txt` / `t_1.txt` 在默认 embed 里，`-tags ne` 后由消费者自选，
  **没有细粒度选项**（例如「只要简体词典，不要日语」）——只能整包自嵌，
  这也是为什么推荐形态是「自嵌自己那一份 `s_1.txt`」（见 §3.3：就用全量，别裁）。
- **第 6.2 节那条「gse 自包含」的对照要打折扣读**：本机 `~/go/pkg/mod` 里已经缓存了
  gse 的 v1.1.0 模块。这**不影响二进制自包含的结论** —— 内嵌进二进制的是**词典的字节**，
  与网络无关；但它**在本机是直接读缓存测出来的**，所以「从零拉取」这一步没有被这次 run 观察到。
  一个 gopi 消费者若用 gse，`go mod download` 与其它依赖走同一条路，无新形态。
  （对照组：gojieba 的 `NewJieba()` 是 `os.Stat` 5 个**文件路径**，其中一个读不到就 `panic`
  —— 差别是「编译期依赖」与「运行期文件依赖」，不是「联网」与「不联网」。）

### 三条路径对照表

| | A 自研 | B CGO jieba | **C gse（推荐）** |
|---|---|---|---|
| 新增依赖 | 0 | 1 模块 + 21MB C++ 源码 | **3 模块（全编译期）** |
| `CGO_ENABLED=0` | ✅ | ❌ **静默空编译** | ✅ 真跑通过 |
| 二进制增量 | 0 | +2.7 MB（但带 21 MB 外部词典） | **+1.47 MB（代码）+4.78 MB（全量词典）** |
| 可移植性 | ✅ | ❌ 词典路径烧进二进制，跨机 panic | ✅ 自包含 |
| 运行时常驻 | 0 | 词典文件必须存在 | **无（词典在二进制里）** |
| 算法质量 | 重造，风险最高 | jieba 原版（最好） | jieba 算法 + 同源词典 |
| 采写成本 | 极高（词典抄不来） | 低 | **低** |

---

## 6. 明确推荐 + 否决理由 + 许可相容性

### 6.1 推荐

**`github.com/go-ego/gse v1.1.0`，Apache-2.0，形态为 `-tags ne` + 自嵌全量 `zh_s` 词典。**

本文件**不需要自己的 build tag**：`ne` 是给 **gse 模块**的（关掉它自己的 `//go:embed`），
我们内嵌的是自己的那份词典，两者互不影响。

```go
//go:embed data/dict/zh/s_1.txt
var zhDict string

var seg gse.Segmenter
seg.SkipLog = true
_ = seg.LoadDictStr(zhDict)      // 内容，不是路径
```
构建声明（Rule 12，settings.json 的 `repos` 表）必须变成：
```
CGO_ENABLED=0 go build -tags ne -o bin/gopi ./cmd/agent/
```
**净增量：二进制 30.96 MB → 约 35.7 MB（+4.78 MB / +15%）；启动 +800 ms；常驻堆 +159 MB。**

**索引侧用法**（供下游任务）：`seg.Cut(s, true)` 写 FTS5 的 gram 列，
`seg.CutSearch(s, true)` 追加子串以覆盖查询侧短语匹配 —— 与既有
`jieba_token` 写入契约（见 `devdoc/jieba-token-absorbs-search-kw.md`）天然对齐。

### 6.2 否决理由（逐条，每条都挂着一个实测数字或源码行）

| 候选 | 否决理由 |
|---|---|
| **`yanyiwu/gojieba`** | ① `CGO_ENABLED=0` 下**静默空编译**（实测 `undefined: NewJieba`），与 gopi 构建契约冲突；② 词典路径编译期烧进二进制（读 `config.go:getCurrentFilePath`，`strings` 实测可见），跨机 / 换 GOPATH 即 panic；③ 需 C++ 工具链 + 21 MB `deps/`。 |
| **`fumiama/jieba`** | ① **AGPL-3.0** —— 与 gopi 的使用方式不相容（见 6.3）；② **23 个**传递依赖，含 bleve 全文引擎与 bbolt 嵌入式数据库，为一个分词问题引入 8 倍依赖面。 |
| **`wangbin/jiebago`** | **v0.3.2 停在 2015-05-08**（模块缓存 `.info` 实测，11 年前）；`dictionary` 是**文件加载**（`LoadDictionary(fileName)`），无内嵌，与「一个二进制」冲突；词典 5.07 MB 作为独立文件随模块分发。 |
| **`blevesearch/segment`** | **它不解决中文词** —— 实现的是 UAX#29 文本边界规则（grapheme/word boundary），对连续 CJK 的输出粒度不是「词」。它是 fumiama 的依赖之一，不是替代品。 |
| **`huichen/sego`** | 备选而非推荐：2021-08-24 停更；生产依赖 `adamzy/cedar-go` + `adamzy/sego`（同族旧名，2015）两个孪生模块；词典 `data/dictionary.txt` 为**文件加载**，无内嵌。算法质量可用（词典型），但活跃度与依赖整洁度都不如 gse。 |

### 6.3 Apache-2.0 许可相容性

**gse = Apache-2.0**，验证：`head -2 $(go env GOMODCACHE)/github.com/go-ego/gse@v1.1.0/LICENSE`
→ `Apache License / Version 2.0, January 2004`。传递依赖：
`vcaesar/cedar` = **BSD-2-Clause**（宽松，相容），`vcaesar/tt` = **Apache-2.0**。

**相容性裁决**：
- gopi 仓库根目录**没有 LICENSE 文件**（实测 `ls` 无），`go.mod` 里也没有许可声明 ——
  即 gopi 目前**未声明**自己的许可。引入 Apache-2.0 代码不产生冲突：
  Apache-2.0 是最宽松的 copyleft-free 许可之一，不传染、不要求下游同样许可。
- **三条实际义务**（都不是阻碍，但要遵守）：
  1. **保留版权/许可声明**：Apache-2.0 §4(a)(b) —— 分发时须随附 LICENSE 与 NOTICE（若上游有 NOTICE 文件；gse 实测**只有 LICENSE，无 NOTICE**）。
  2. **标注修改**：§4(b) —— 若我们改动 gse 源码（不打算），须声明。
  3. **专利授权**：§3 提供专利许可，对我们是**增益**。
- **对比：AGPL-3.0（fumiama/jieba）** —— 网络服务条款会传染到 gopi 的整个分发形态，
  且 gopi 已有 `serve` / `worker` 这类常驻路径（实测 `cmd/agent/cli_tasks*.go`），
  AGPL 的解释成本远高于收益。**这是 fumiama 被否决的首要理由，不是依赖数。**

---

## 7. 落地清单（交给后续任务，本任务不做）

**本任务只回答「用哪个」**，以下为下一个任务的输入，**不在本分支实施**：

1. **构建声明必须改**：`settings.json` 的 `"repos"` 表里 gopi 的
   `"build"` 加 `-tags ne`（Rule 12）。**漏掉这一步 = 静默 +32 MB**。
2. `go get github.com/go-ego/gse@v1.1.0`（在 gopi 主仓库，由后续任务做）。
3. 自嵌词典：把 `s_1.txt`（5.12 MB）放进仓库还是构建期下载 —— **需要单独决策**
   （进仓库则触发 Rule 39 的体积规则：5 MB 非源码文本 > 256 KiB 上限，
   需走 permit 或改为构建期获取）。
4. `internal/memoryprefetch/extract.go` 的 `KindCJKBigram` 降级路径替换为真分词，
   并把 `KindCJKBigram` 的诚实注释一并更新。
5. `internal/tools/memory.go` 的 `jieba_token` 写入侧改用 `Cut` + `CutSearch`
   （见既有契约 `devdoc/jieba-token-absorbs-search-kw.md`）。
6. **回归测试**（Rule 10.5）：`真关键词被双字组挤出MaxKeywords名额` →
   必须含 `关键词`（第 2 节样本 05 就是这条的红→绿对照）。

---

## Provenance

- 测量环境：peerbox（Ubuntu 22.04.4 LTS），Go 1.24.5，`CGO_ENABLED=0`，2026-10-07。
- 临时模块根：`/home/user/workspace/_tmp/cjksurvey`（即 `$TMPDIR`，dev `2052`；
  `/` 为 dev `2051` → **不在 `/` 分区上**，Rule 22 满足）。
- 证据文件（本次 run 的 session dir）：
  - `evidence/01-dep-trees.sh` / `.out.txt` —— 依赖树生成器与原始输出
  - `evidence/02-gse-cuts.txt` —— 15 条切词样本（全量词典）
  - `evidence/03-gse-trimmed.txt` —— 20 000 词裁剪词典对照
  - `evidence/04-gse-trimmed-scaling.txt` —— 100 000 / 352 279 词对照
  - `evidence/05-gse-perf.txt` —— 吞吐与加载耗时
- 测量 commit：本文档首版落地的 commit `09fa5b138`（`git show 09fa5b138 --stat`）。
  实测本身发生在该提交之前几分钟的同一 worktree 里（脏树状态），**产物就是本文档自己**，
  所以「测量 commit」取携带它的那一个 —— 再往后 sync 进来的 merge 只并 master。
  **测量内容的最后一次改动是 `a35b3bd9f` 之前的 `06140dcba`（改写了 §0 的结论与 §6.1）；
  `a35b3bd9f` 与之后到交付点之间的提交只动本 Provenance 小节的散文（版本轨迹、同步记录），
  没有改任何测量数字。** 交付点 = 分支 tip（`git log -1 --format=%h`），
  它从 master 下降（`git merge-base --is-ancestor master HEAD` → 0）。
- **修订记录**：§0 的一行结论在 §3.3（裁剪词典质量对照）跑完之后被**改写**过一次
  （初版误按「体积最小」推荐 2 万字裁剪，见 `06140dcba`）。改写理由见 §0 的引用块。
- **Rule 36 分类（诚实记录）**：`tools/docclass/classify.py` 把本文件判为
  `normative`，证据层是最弱的 `path` 且指向兄弟 devdoc
  （`path devdoc/jieba-token-absorbs-search-kw.md`）。按 Rule 36，manifest 是数据、
  **不手改**，所以此处保留工具的判定。但读者应当知道：
  **本文件的 §7 是「未来工作」而不是 master 上的既成事实** —— 分类器的
  `ROAD_TITLE_RE` 只认标题里的 roadmap/plan/design/… 等词，而本文标题是
  "Survey"，因此没走到 roadmap 分支。**§0 的一行结论与 §7 的落地清单，按「未实施的建议」读。**
- **普查数字的生成器（Rule 38）**：依赖树 = `evidence/01-dep-trees.sh`；
  体积 = `stat -c %s <binary>`；词典字节 = `stat -c %s <dict file>`；
  模块发布时间 = 模块缓存 `<ver>.info` 的 `Time` 字段。
