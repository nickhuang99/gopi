<!-- doc-class: normative -->
# `edit` 的静默内容丢失：空替换是删除，而空串是 `Replace` 的合法值

> 记录 2026-10-04 的缺陷（任务 `01a103c2-36e2-7808-bc42-d652762c4691`，peer1）。
> 这是**根因记录 + 不变量说明**，不是操作手册。

## 症状（当初看到的）

`edit(path, oldStr, newStr)` 在 `oldStr` 能唯一定位时报 **"Successfully edited"**，
但被匹配的那一行连同目标内容一起消失，文件缩小。发生在 tracked 文件（`CONFIG.md`）上，
唯一的发现渠道是恰好覆盖该文件的 gate（`go test ./internal/standards/ -run ConfigDoc`）。
上一 run 复现 3/3，并且**与 newStr 是单行还是多行无关**——这条线索当时被误读成
「长 newStr 被截断」，其实是「newStr 根本没被读到」。

## 根因（本次实测确定）

**参数键对不上，而空串是合法值。**

上一 run 的 raw log 里，一个 session 的 17 次 `edit` tool_use 中、**6 次把替换文本写在
`newText` 键下**（`{'oldStr': 17, 'path': 17, 'newStr': 11, 'newText': 6}`）。
dispatcher 一侧：

```go
newStr, _ := args["newStr"].(string)   // 键不存在 → ""，类型断言不报错
```

于是 `strings.Replace(content, oldStr, "", 1)` 执行——**它是删除，不是空操作**——
然后无条件返回成功。整个过程没有任何一处能报错，因为
「调用方没给替换文本」和「调用方就是要删掉这段文本」在类型上无法区分，
而工具描述里又没有「不能删文本」这条约束。

**为什么之前的排查会偏**：去看 `internal/tools/edit.go` 是对的代码
（`strings.Replace` + 原子写），于是怀疑链路（流式 `input_json_delta` 截断、
`max_tokens` 截断）。真正的问题是「模型发的键名」——只有数 raw log 里的
`input` 键才能看到，读代码看不到。

## 修法：把不变量放在**操作**上，而不是某个调用方上

1. `internal/tools.Edit`：匹配已知为真之后，`newStr == ""` 一律报错拒绝（`replace_all`
   也不例外）。检查位置刻意放在「匹配已成立」之后，这样两种答案仍然是两件不同的事：
   「oldStr 没匹配上」和「oldStr 匹配上了、但会被抹掉」是不同的 bug，不该合并成一句话。
2. `internal/agent` 的 dispatcher：在调用工具**之前**拦下空替换，并回显**收到的实参键名**；
   若命中已知误名 `newText`，点名「你发的是 `newText`，schema 的键是 `newStr`」。
   反馈可操作 > 一个调用方无法信任的成功。
3. `internal/toolnames`：实参键名升为常量（`ToolArgNewStr` / `ToolArgNewTextMisspelling`…），
   dispatcher 不再裸写字符串。误名本身也成为一个**有名字的常量**——它的用途是「可以被点名拒绝」。

## 复用要点（下一次遇到「成功但没生效」）

- **空串通常是合法的**：任何「读写同一个字段」的工具，参数缺失 → 零值 → 合法操作，
  这条链是静默破坏的通用形状。默认拒绝对语义为「必须非空」的参数，是最小成本的修复。
- **读代码看不出键名**：怀疑「模型发的东西和工具以为的东西不一致」时，直接数 raw log 里
  `tool_use` 的 `input` 键直方图（`sessions/*.jsonl`），而不是读 dispatcher 源码。
- **回显收到的键名**：拒绝信息里带上实参键名，模型下一步能自我纠正；只写「缺少 newStr」
  会让它继续猜拼写。
- **验证要看回读**：这类缺陷「看起来对」，断言必须逐字节比文件内容，不能用 `Contains`。
