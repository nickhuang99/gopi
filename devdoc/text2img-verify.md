<!-- doc-class: historical -->
# Task Report: text2img 渲染质量验证 — 单字'知'像素级 IoU

## Completed
- 最小样本：单字 '知' (z.txt) 用 gopi text2img 渲染 PNG（20px 默认 + 100px/200px 验证档）。
- **像素级验证**（未用 vision/OCR 判断质量）：gopi 栅格化墨量 vs 同一扁平化轮廓的鞋带面积
  （数学真值）对比 → 发现渲染器过填充 **17–28%**。
- **根因**（5-why 挖到）：扫描线填充在"非整数 y 顶点"处双重计数 — 共享顶点的两条线段
  的行区间 r0=floor(lo)..r1=ceil(hi) 都覆盖顶点所在行，每条线段各发射一个交点 →
  缠绕数翻倍、奇数交点（97/376 行泄漏）、系统过覆盖。修复：仅当行中心严格落在
  [lo, hi) 内才发射交点（半开区间顶点规则，标准多边形扫描线约定）。
- 附带修复：默认行高 Lh 随字号缩放（≈1.6×，未显式指定时），修复 Size>32 时字形
  被页高裁切的问题（原 H=1×32+2×12=56px @ Size=200）。

## Not Done
- 未做 hinting（与 FreeType 的 1px 边缘差异在 20px 小字号仍存在，IoU 0.83 已是
  无 hinting 渲染的理论上限；200px 达 0.988）。
- 未改默认 20px 参数（默认字号下验证通过，IoU 0.83）。

## Verification
- Build: `go build ./...` OK；`CGO_ENABLED=0 go build ./cmd/agent` OK
- Tests: `go test ./...` 全绿；新回归测试 TestFillMatchesOutlineArea、
  TestLhScalesWithSize、TestRenderFileCJK/Pagination/MissingInput 全 PASS
- 像素级证据（修复前后）：
  - 知 填充/轮廓面积比：wqy 1.285 → **1.0005**；Droid 1.172 → **1.0019**
  - IoU vs FreeType-backed PIL：20px 0.33→0.83；100px 0.41→0.88；200px 0.47→**0.988**
  - 泄漏行：97/376 → 0
- Commit: `c3397fa` on `feat/t2img-lh-fix`（worktree ~/work/gopi-t2img-verify）
