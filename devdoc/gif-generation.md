<!-- doc-class: normative -->
# GIF generation without a dependency（零依赖动画 GIF）

`internal/gifgen` + `gopi gif`：把 PNG 帧序列编成一个循环动画 GIF，帧下带文字带。
只用 `image/gif` 与本仓库自己的 `internal/text2img` 光栅器 —— 没有 ffmpeg / ImageMagick /
gifsicle，`go.mod` 一个字节没动（Rule 35）。渲染 API 与用法见 `gopi gif -h`；
HTML-vs-GIF 的实测取舍见 `docs/archive/demo/showcase/gif/README.md`。

本文只记**踩过的坑**，因为每一条都是当前代码形状的原因，删掉注释后没人能重新推导出来。

## 1. 文字带是「影片级」的，不是「帧级」的

第一版按每帧自己的文字量决定带高。多帧 GIF 用 `disposal=none`（只覆盖变化像素，
最省字节）时，第 1 帧画的字在带区里**会留着**，而第 2 帧的带更矮 —— 第 2 帧不写那一行，
旧字就透出来了。现场是「第 2 帧的说明文字下面粘着第 1 帧的字」。

修法：整部片算**一个**带高（最长 caption 需要的高度），每个逻辑帧都按这个高度
拼成「图 + 带」，没有 caption 的帧把带填成白纸。`disposal=none` 于是永远正确，
因为每一帧都完整重写带区。回归：`TestCaptionBandIsUniformAcrossFrames`。

## 2. 调色板要两种模式，一种不够

GIF 每帧最多 256 色。第一版用「首见顺序」量化。实测在多彩帧上：
只有 **0.49%** 像素颜色精确、**MAE 83.909** —— 视觉上就是噪点。

改成按颜色数分派：
- **≤256 种颜色** → 精确无损，一个像素都不动（我们的线稿截图正是这一类：255/250/247 色）；
- **>256 种颜色** → 3-3-2 网格（R/G 各 8 级、B 4 级）单元**均值**，同帧 MAE 降到 **10.7**。

同一份输入的帧序列由此得到上表那句反直觉的结论：线稿 GIF 比 PNG 集合还小
（28 026 B vs 34 865 B），因为精确调色板让 LZW 发挥了全部优势。**换照片就反过来**，
所以「先数颜色，再选格式」是要检查的条件。

回归：`TestPaletteExactWhenWithin256` / `TestPaletteQuantisesAbove256`。

## 3. 单帧流没有 Netscape 循环扩展

`gif.EncodeAll` 只对**多帧**写 Netscape 循环扩展。单帧 GIF 读回时
`LoopCount` 是 `-1`（未指定），不是我们请求的值。所以 `Result` 同时给两个字段：
`LoopCount`（我们请求的）与 `LoopReadBack`（文件里实际读回的），
调用方要判断「文件真的会循环吗」必须看后者。回归：`TestSingleFrameHasNoLoopExtension`。

## 4. 调色板 alpha 必须强制不透明

拿 `color.Palette` 直接来自 `Palette()` 时，index 0 的 alpha 会参与编码器的
透明色选择；纯透明输入读回变成**白色**。修法：写调色板项时强制
`A=0xff`。回归：`TestPaletteAlphaIsForcedOpaque`。

## 5. 新 verb 不接 `cli_verb_guard.go` 会被当成 prompt

`gopi --timeout 3 gif …` 里的 `gif` 若不在 `internal/config/cli_verb_guard.go` 的
`PositionalVerbs` 中，会被当成**一句 prompt**，触发一次真实的 agent 运行 —— 用户的
动图命令变成一次 LLM 会话。这是门禁（`TestGOPIRule3_...` 的 verb 表）实测抓出来的，
不是推断。新 verb 的接线一共 5 处，详见 `devdoc/gopi-verb-census.md`。

## 6. 文字基线是导出的推导，不是镜像的常量

`internal/gifgen` 要在自己的画布上摆字，它必须知道基线的下移量 —— 就导出
`text2img.Face.Ascent(sizePx)`（`internal/text2img/annotate.go`），
按注释里同样的推导算，而不是在 gifgen 里抄一个数字（Rules 13/14）。
抄常量的后果是「一个渲染器上偏高几像素、另一个偏低」，这正是 Rule 14 要避免的形态。
