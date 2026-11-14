<!-- doc-class: historical -->
# 门禁的同名 helper 解析：`run` 不是 `run`

> 状态：已实现（task `01a159ad`）。本文记录一次**门禁自身误判**的根因与修法，
> 因为它演示了一个会反复出现的失败形态：**静态门禁按裸标识符建表**。

## 症状

master 上 `TestNoTestDrivesTheRealBinaryWithARetiredWord` 红，报三处：

```
cmd/agent/cli_chat_web_help_surface_basic_test.go:957: the real binary is driven with the
RETIRED word "proxy" as argv[0], through the local helper run — use `gopi tool proxy`
（:961、:966 同）
```

那条消息自己就否证了它：它说"through the local helper `run`"，而 `p.run` 的体内是

```go
cmd := exec.Command(p.bin, familyAddressArgs(args)...)
```

即三处调用点 `p.run(t, "", CliVerbTool, CliVerbProxy, …)` 驱动的是 `gopi tool proxy`,
**本来就正确**。门禁在说谎。

## 根因（实测，不是推断）

`cmd/agent/retired_verb_helper_reach_test.go` 的 helper 表建表语句是

```go
helpers := map[string]retiredVerbGateHelper{}   // 键＝裸函数名
...
helpers[h.name] = h                             // 后解析者覆盖先解析者
```

而 path 是 `sort.Strings` 过的，所以 `internal/devworkspace/devworkspace.go:636` 的包级

```go
func run(ctx context.Context, dir, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
```

在 `cmd/agent/cli_tasks_sync_surface_test.go` **之后**被解析，于是**覆盖**掉三处调用点
真正调用的 `func (p *g5Probe) run(t *testing.T, stdin string, args ...string)`。表里留下的
是 VERBATIM + `argvIndex=3`，而调用点实际传的是 `(t, "", CliVerbTool, CliVerbProxy, …)`：
第 3 个参数正好是 `CliVerbProxy` → 词命中 → 报红。

一个插桩即可看清（探针读数见任务报告）：表内 `run` 有 **4 条**声明，
`devworkspace.go:636`（`recv=""`、verbatim、idx3）与 `cli_chat_web_help_surface_basic_test.go:602`
（`recv="p"`、transformed、idx2）同键不同身份。

## 修法（不改调用点）

表改成**按名分桶** `map[string][]retiredVerbGateHelper`，表项加 `recv`（接收者参数标识符，
包级函数为空）。调用点解析改为**最具体优先**三级：

1. 裸标识符调用 `run(…)` → 只寻址**包级函数**（方法不可能这样被调用）；
2. 选择器调用 `x.run(…)` → 先按**接收者标识符精确匹配**（`x == h.recv`）——`p.run(…)` 落到
   `*g5Probe` 的方法，不再落到别处的函数；
3. 链式选择器 `fx.sb.run(…)` 无标识符可匹配 → 才退回"任意同名方法"（**修前行为**，兜底，
   不再优先于精确匹配）。桶内多候选按解析顺序取第一 → 确定。

读地板的 `verbatim` 计数改为**按声明数**（一个名字两次声明＝两个 helper），名字级去重会低估
地板输入。

## 为什么这个修法不能只看"变安静"

一个只做"同名就少报"的修复会同时制造**假阴性**：若调用点真的是包级 VERBATIM helper，
而一个同名 TRANSFORMED 方法解析在先，它会把真驱动**藏起来**。所以新夹具
`TestRetiredVerbGateHelperResolutionIsNotNameOnly` 两条**反向**用例同时上：

- 假阳性形态 → 期望 **0** finding（修前 1）；
- 假阴性形态 → 期望 **1** finding（修前 0）。

变异回 name-only 后两条同时红，门禁臂逐字复现 957/961/966。

## 可复用的教训

**静态门禁按裸标识符建索引，就必然把不同包/不同接收者的同名声明混成一个。** 判据必须是
声明身份（接收者标识符、声明位置），不是字面名字；而且**修复必须双向证明**——只会少报的
修复，是在把假阳性换成假阴性。

同类残余风险（本次**未**改，也没有实测到它造成误判）：`retiredVerbGateProgramIsSpelled`
用的 tree-wide `stringConsts` 表同样是跨包同名合并，另一个包的 `const bin = "git"` 理论上
可以让本包的 `bin` 变量被判成"程序有名字"。按 Rule 41 这条只记为已知边界，不当作结论。
