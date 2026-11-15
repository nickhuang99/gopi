<!-- doc-class: normative -->
# “绿”必须用交付 HEAD 建的二进制重跑一次

**一句话**：拿到绿之前，先问一句「这个绿是在**哪一棵树**上跑的、那个二进制是**哪一次提交**建的」。
二进制比提交旧，绿的就不是交付物 —— 你证明的是一棵**不存在的树**的正确性。

## 症状（不是假想，是实测）

一次任务里出现过两份 green，都是真跑的、都 exit 0：

| 证据 | 二进制 `--version` 的 `revision:` | 时间关系 |
|---|---|---|
| `evidence/green.txt` | `61301b204357171d775ab9ade6f0a90a73c40e5d` | 建在**提交之前**的树 |
| `evidence/green-postcommit.txt` | `9a0610116d198da4e86abd6a2fbab69b819d022d` | 建在**交付的 HEAD** 上 |

第一份不是撒谎，它只是**在测量一个不存在的对象**：那个二进制是在改动还没有被冻结成一次
提交时编译出来的，所以「它通过了」这句话无法被任何人复现 —— 拿 `git checkout`
回到哪一次提交都得不到它。

## 为什么是「重跑」而不是「就地检查」

改了代码 → 编译 → 跑绿 → `git commit`，这个顺序里 **commit 是最后一步**，于是
所有绿都天然地落在 commit 之前。想省掉重跑，就得反过来：先 commit，再用 commit
后的树编译、再跑。两种做法都可以，但**必须有一种**，否则没有任何一份绿绑定到交付物上。

## 配方（每次交付都跑，一条命令）

```bash
cd <ground>
CGO_ENABLED=0 go build -o /tmp/gopi-verify-bin ./cmd/agent/    # 在 commit 之后重新建
/tmp/gopi-verify-bin --version | grep revision                  # 记下来
git rev-parse HEAD                                              # 两个哈希必须逐字相等
GOPI_BIN=/tmp/gopi-verify-bin go test -run '<the gate>' ./testsuite/ -count=1
```

三条读数全部落到证据文件里（Rule 5 的 `## Verification`）：**二进制自己的 `revision:` 行、
`git rev-parse HEAD`、以及这一跑的输出**。判据是**两个哈希相等**，不是「我记得我建过」。

## 为什么二进制自己那一行才是判据

`go build` 成功、`go test` 通过，都只说明**它跑过了**。它们对「跑的是哪份源码」
一个字都没说 —— 一个三天前的 `/usr/local/bin/gopi`、一个 `/tmp` 里别人留下的二进制、
一个 `bin/gopi` 被上一次 `make build` 覆盖过，全都照样 exit 0。

`--version` 的 `revision:` 是程序**自己**报的、由构建时的 `git rev-parse HEAD` 注入，
所以它是这批产物里**唯一**一个「关于它自己是哪份源码的陈述」。比对它就等于比对
「我跑的东西」和「我要交付的东西」。

## 连带的一条：`-run` 匹配不到东西也会 exit 0

同一次里还查了一件同类的事 ——

```bash
go test -run 'TestTurnConsistency' -list '.*' ./testsuite/   # 有命中：测试名打出来
go test -run 'TestTurnConsistency' ./testsuite/ -v | grep '=== RUN'   # 四条 === RUN
```

`go test -run <re>` 匹配不到任何测试时打印 `no tests to run` 并 **exit 0**。所以
「命令以 0 退出」既可能意味着干净，也可能意味着**什么都没跑**。两个形状必须靠
`-list` 的命中行和 `=== RUN` 的行数区分，不能靠退出码。

（同一个坑在本仓库别处已经付过代价：Rule 36 与 Rule 38 都记着「一个被点名的检查
其实不存在，它的绿和干净的绿无法区分」这回事。）

## 边界

- 这条**不是**要求每次 `go test` 都重建：一个包的快速迭代随便建。
  它要求的是**交回去的那一份绿**绑定到交付的 revision。
- 这条**不是**新的机制（Rule 35）：没有脚本、没有钩子、没有常驻进程。
  它是**两份读数的一次比对**，落在交付报告里。
- 它**不能**被机器门禁替代：机器能读 `git rev-parse HEAD`，读不到「你刚才跑的那个文件
  是哪个 commit 建的」。所以它靠报告里的两行原文成立 —— 而那正是 Rule 5
  「没有被观察到的事不是可报告的结果」的用法。
