<!-- doc-class: historical -->
# Task Report: fix/reframe-anchor-marker — line-anchored [REF RAMED EPIC] extraction

Branch: `fix/reframe-anchor-marker` · commit `3d7cd8e` (worktree, shared refs — no push)
Issue: epic-reframe anchor bug (submitted t-1788876232; review finding 95b8e39 / memory #1524)

## Problem

`extractReframedEpic` (internal/agent/reframe.go) used `strings.Index(content,
"[REF RAMED EPIC]")` — anchoring the **first occurrence anywhere**, not a real
block. In the production review session (ac786cca) the assistant's
task-list-establishment response contained three occurrences:

| pos | kind | content |
|-----|------|---------|
| 398 | mid-line **prose** | "…my first response must carry a [REF RAMED EPIC] restatement + task list…" |
| 457 | **line-start, real block** | `[REF RAMED EPIC]\n\n**Background**: …` |
| 2743 | mid-line prose, **inside the body** | "(a) my first response opened with [REF RAMED EPIC] and carries…" |

`strings.Index` anchored pos 398. The line-skip then ran past *that* line, and
the boundary scan swallowed pos 457's own marker line into the body — so the
swapped [EPIC GOAL] body opened with a leaked `[REF RAMED EPIC]\n\n**Background**`
(2582 chars). Memory #1524 had documented exactly this ("marker 行泄漏进 swapped
goal body… 修复=行首锚定 regex 或取 last occurrence + 回归测试").

Note: "last occurrence" alone is NOT a valid fix here — the third occurrence
(2743) sits *inside* the legitimate body and must be preserved, not truncated
at. Line-anchoring is the correct selection rule.

## Fix

`reframe.go`: replaced the `strings.Index` first-occurrence anchor with a
line-start-anchored match:

```go
var reframeEpicMarkerLineRE = regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(reframeEpicMarker))
```

`extractReframedEpic` now finds the first marker at a **line start** (prose
mentions mid-sentence are ignored), skips that marker's own line, and scans to
the `[TASK_LIST_BEGIN]` boundary. An in-body re-mention of the token (mid-line)
is never treated as a new block and is preserved whole.

## Regression tests (reframe_test.go)

- `TestExtractReframedEpic_IgnoresMidLineProseMention` — mirrors ac786cca's
  shape (mid-line prose mention → genuine line-start block → task list):
  asserts the body starts with the true block text and contains no leaked
  `[REF RAMED EPIC]` / task list.
- `TestExtractReframedEpic_BodyMayMentionMarkerPreservesIt` — in-body marker
  mention (acceptance prose) preserved whole, never truncating the body.

## Verification

- Real repro (ac786cca assistant content, extraction logic mirrored): body
  2582 → **2564 chars**, starts `**Background**: This task runs…` (was
  `[REF RAMED EPIC]\n\n**Background**…`). Sole remaining marker in body at
  pos 2268 = legitimate acceptance prose, preserved.
- Build: `go build ./...` OK.
- Tests: `go test ./internal/agent/` — **ok (43.9s)**, incl. all 10 reframe/
  extract tests; full `go test ./...` — **SUITE_EXIT: 0** (background job-1,
  586s; only non-ok line was a `[no test files]` package).
- Commit: `3d7cd8e` (fix) + `dad8dab` (docs)

## Not Done

- No LLM-adoption proof needed: this is a pure bug fix, not a tool/prompt
  change (Rule 11 boundary).
