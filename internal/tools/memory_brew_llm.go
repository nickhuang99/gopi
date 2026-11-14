// Brew part 2 — the LLM judge and distiller. One flash call per session
// chunk judges the three lesson conditions; accepted chunks are distilled
// into Q&A rows in a second flash call. thinking:disabled on every call
// (hermes lesson: reasoning_content eats max_tokens, content comes back
// empty and JSON parsing fails).
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// brewJudgePrompt is the batch judgment prompt: the three-condition gate
// (可重复模式 / 可复现闭环 / 收益达标) + the candidate extraction. The reply
// MUST be a single JSON object — no prose, no markdown fence.
// kind=workflow (v2): a workflow lesson is distillable into a procedural row
// (工作流记忆) — the answer proposes it with the steps the transcript shows.
const brewJudgePrompt = `你是一名严格的蒸馏审核员。下面是一段 gopi 编码 agent 的工作记录切片。
判定它是否含有值得存入长期知识库的 lesson。 lesson 三条件（全部满足才存）:
1. 可重复模式 (repeatable): 换一个项目/会话仍然成立的做事情方法，而非一次性事实；
2. 可复现闭环 (reproducible): 有明确的操作步骤或命令，别人照做能复现同样的结果；
3. 收益达标 (payoff): 照做能省时间/避免踩坑，收益明显大于阅读成本。
一次性事实、状态汇报、寒暄、纯代码输出、与 gopi 自身记忆功能无关的流水账都不算 lesson。
若无 lesson，lessons 返回空数组。若有，最多提炼 3 条，每条给出:
- kind: "workflow"（可复现的多步操作流程，有明确步骤序列）或 "lesson"（经验/教训/结论）。
- topic: 一句话主题 (<=24 字)
- evidence: 切片中最能支撑它的原文摘录 (<=200 字)
只输出 JSON，格式: {"lessons":[{"kind":"workflow","topic":"...","evidence":"..."}]}
kind 字段缺省按 "lesson" 处理。没有任何 lesson 时输出 {"lessons":[]}。不要输出 JSON 以外的任何文字。

工作记录切片:
`

// brewDistillPrompt generates the final QA row per accepted lesson: question
// must carry multi-phrasings (中英混排) so memory_search can match; the answer
// is 3-4 actionable sentences; keywords are 5W1H breadcrumbs (M1 面包屑哲学).
const brewDistillPrompt = `把下面的 lesson 蒸馏成一条可检索的知识库 QA 记录。
要求:
- question: 一个未来 agent 真会问的问题。必须包含中英两种问法/关键词变体，让全文检索能命中。
- answer: 3-4 句 actionable 经验。直接说怎么做/怎么避坑，含具体命令或参数名（如有）。不要空话。
- keywords: 8-14 个检索面包屑，中英混合，覆盖 5W1H（主题/问题/场景/环境/实体/方法），含同义词与变体问法。每个 <=24 字符，逗号分隔。
只输出 JSON: {"question":"...","answer":"...","keywords":["...","..."]}
不要输出 JSON 以外的任何文字。

lesson 主题:
`

// brewWorkflowPrompt distills a workflow lesson into a procedural row: a
// name (unique identity), the trigger (何时启用), ordered steps and the
// tools exercised. Stored into the procedural table, not distilled_qa.
const brewWorkflowPrompt = `把下面的工作流 lesson 蒸馏成一条可复用的流程记录（procedural memory）。
要求:
- name: 流程的唯一名字，体现"做什么"，格式如 读错误日志→定位→修复→验证 (<=40 字)。
- trigger: 何时启用这条流程（触发条件/场景，1-2 句，中英关键词都给）。
- preconditions: 前置条件（可选，无则空字符串）。
- steps: 3-8 个有序步骤，每步一句话，含具体命令/文件/参数（如有）。
- tools_used: 流程用到的工具名列表（如 bash、code_search），可为空数组。
- keywords: 6-10 个检索面包屑，中英混合，含同义词与变体问法。每个 <=24 字符。
只输出 JSON: {"name":"...","trigger":"...","preconditions":"...","steps":["...","..."],"tools_used":["..."],"keywords":["..."]}
不要输出 JSON 以外的任何文字。

工作流 lesson 主题:
`

// brewCandidate is one lesson candidate the judge proposes.
type brewCandidate struct {
	Topic    string `json:"topic"`
	Evidence string `json:"evidence"`
	// Kind routes the distillation: "workflow" lessons are procedural rows
	// (trigger/steps), anything else (or empty — legacy judges) is a QA
	// lesson. Declared optional so old brewJudgeReply JSON still parses.
	Kind string `json:"kind,omitempty"`
}

// brewJudgeReply is the judge's JSON reply envelope.
type brewJudgeReply struct {
	Lessons []brewCandidate `json:"lessons"`
}

// brewDistillReply is the distiller's JSON reply envelope.
type brewDistillReply struct {
	Question string   `json:"question"`
	Answer   string   `json:"answer"`
	Keywords []string `json:"keywords"`
	// Topic is NOT produced by the LLM — distillLesson copies it from the
	// judge's candidate so the stored row's topic stays the judged one.
	Topic string `json:"-"`
}

// brewWorkflowReply is the workflow distiller's JSON envelope — the fields
// of a procedural row.
type brewWorkflowReply struct {
	Name          string   `json:"name"`
	Trigger       string   `json:"trigger"`
	Preconditions string   `json:"preconditions"`
	Steps         []string `json:"steps"`
	ToolsUsed     []string `json:"tools_used"`
	Keywords      []string `json:"keywords"`
	Topic         string   `json:"-"`
}

// brewClient is the minimal LLM surface the brew pipeline uses. *deepseek.
// Client satisfies it; tests inject a stub.
type brewClient interface {
	Chat(ctx context.Context, system, user string) (string, error)
}

// brewFlashClient adapts *deepseek.Client to brewClient.
type brewFlashClient struct {
	c          *deepseek.Client
	model      string // resolved flash-tier model (env > settings > BrewModel)
	effortMode bool   // flipped on GLM's always-thinking 400 (see Chat)
}

// Chat sends one non-streaming request with thinking disabled. Endpoint
// adaptive: deepseek-v4-flash REQUIRES thinking:disabled (reasoning_content
// eats max_tokens — memory #185); glm-5.3-flash REJECTS thinking:disabled
// ("该模型始终思考") and must be throttled with reasoning_effort=low instead
// (memory #915/#920). The first 400 with the always-thinking marker flips
// the client to effort mode for the rest of the run.
func (f *brewFlashClient) Chat(ctx context.Context, system, user string) (string, error) {
	req := f.buildRequest(system, user)
	if f.effortMode {
		req.ReasoningEffort = brewReasoningEffortLow
	} else {
		// THE hermes lesson (memory #185/#915): deepseek-v4-flash defaults to
		// thinking ON; reasoning_content then consumes the entire max_tokens,
		// content comes back empty and JSON parsing fails. Disabled by
		// default; GLM's always-thinking 400 flips to effort mode below.
		req.Thinking = &deepseek.ClaudeThinking{Type: brewThinkingMode}
	}
	resp, err := f.c.ClaudeChat(ctx, req)
	if err != nil && !f.effortMode && isAlwaysThinkingError(err) {
		f.effortMode = true
		return f.Chat(ctx, system, user) // retry once in effort mode
	}
	if err != nil {
		return "", fmt.Errorf("brew: llm call: %w", err)
	}
	out := resp.Text()
	if strings.TrimSpace(out) == "" {
		// Stop reason max_tokens with empty content is the exact symptom the
		// thinking-mode lesson describes — surface it, never return silently.
		return "", fmt.Errorf("brew: llm returned empty content (stop=%s, out=%d in=%d tokens) — is thinking really disabled?",
			resp.StopReason, resp.Usage.OutputTokens, resp.Usage.InputTokens)
	}
	return out, nil
}

// buildRequest assembles the brew request shell (model/max_tokens/messages).
func (f *brewFlashClient) buildRequest(system, user string) *deepseek.ClaudeRequest {
	maxTokens := brewMaxTokens
	if f.effortMode {
		// GLM reasoning eats the budget even at effort=low — see
		// brewMaxTokensEffortMode.
		maxTokens = brewMaxTokensEffortMode
	}
	return &deepseek.ClaudeRequest{
		Model:     f.model,
		MaxTokens: maxTokens,
		System: []deepseek.ClaudeSystemBlock{
			{Type: "text", Text: system},
		},
		Messages: []deepseek.ClaudeMessage{{
			Role: "user",
			Content: []deepseek.ClaudeContentBlock{{
				Type: "text",
				Text: user,
			}},
		}},
	}
}

// isAlwaysThinkingError matches the GLM always-thinking 400 (code 1210).
func isAlwaysThinkingError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "不支持关闭思考") || strings.Contains(s, "always think")
}

// newBrewFlashClient builds the real client. The model resolves exactly like
// the rest of gopi (DEEPSEEK_MODEL env > settings.json model > BrewModel
// fallback — Rule 2 single source of truth): this deployment serves
// glm-5.3-flash via the bigmodel Anthropic-compatible endpoint, another may
// serve deepseek-v4-flash; the pipeline only requires a flash-tier batch
// model and thinking:disabled (both honored on either endpoint — memory
// #920: GLM maps thinking.type=="disabled", DeepSeek needs it to keep
// reasoning_content from eating max_tokens).
func newBrewFlashClient() (brewClient, error) {
	cfg, err := deepseek.LoadConfig(deepseek.SettingsPath())
	if err != nil {
		return nil, fmt.Errorf("brew: %w", err)
	}
	c, err := deepseek.NewClaudeClient()
	if err != nil {
		return nil, fmt.Errorf("brew: %w", err)
	}
	return &brewFlashClient{c: c, model: deepseek.ResolveModel(cfg)}, nil
}

// NewBrewEngine is the CLI-facing constructor for the real flash engine.
func NewBrewEngine() (brewClient, error) { return newBrewFlashClient() }

// brewTimeout resolves the per-LLM-call budget: default brewClientTimeout,
// overridden by GOPI_BREW_TIMEOUT_SEC (Rule 2 — this is the ONE reader of
// brewTimeoutEnv). A malformed or non-positive value keeps the default.
func brewTimeout() time.Duration {
	v := os.Getenv(brewTimeoutEnv)
	if v == "" {
		return brewClientTimeout
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return brewClientTimeout
	}
	return time.Duration(n) * time.Second
}

// callBrewJSON runs one prompt, extracts the first JSON object from the
// reply (models sometimes wrap it in prose despite the instruction) and
// unmarshals it into out. think-disabled flash replies in ~2-6s; the
// per-call timeout is brewTimeout().
func callBrewJSON(ctx context.Context, bc brewClient, system, user string, out interface{}) error {
	timeout := brewTimeout()
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := bc.Chat(cctx, system, user)
	if err != nil {
		// A deadline of OUR making must never surface as a bare
		// "context deadline exceeded": the operator cannot then tell a slow
		// provider from a code bug (Rule 15 — no unattributed failure).
		// Name the budget, the knob and the call's prompt tag.
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return fmt.Errorf("brew: llm call exceeded the %.0fs per-call budget (%s) — raise %s if the endpoint is just slow: %w",
				timeout.Seconds(), promptTag(user), brewTimeoutEnv, err)
		}
		return err
	}
	js := extractJSONObject(raw)
	if js == "" {
		return fmt.Errorf("brew: no JSON object in reply: %.200s", raw)
	}
	if err := json.Unmarshal([]byte(js), out); err != nil {
		return fmt.Errorf("brew: JSON reply unparseable: %w (reply: %.200s)", err, raw)
	}
	return nil
}

// extractJSONObject returns the first balanced {...} block in s ("" if none).
// Handles fenced ```json blocks and surrounding prose.
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if esc {
			esc = false
			continue
		}
		switch {
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// inside a string — braces do not count
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// sliceSession cuts a rendered session transcript into chunks that each fit
// in one judge prompt (char + event caps). Chunks are split on event
// boundaries — never mid-line.
func sliceSession(transcript string) []string {
	if transcript == "" {
		return nil
	}
	lines := strings.Split(strings.TrimRight(transcript, "\n"), "\n")
	var chunks []string
	var cur []string
	curLen := 0
	for _, ln := range lines {
		// A single over-long line is clamped, never allowed to own a chunk.
		if len(ln) > brewMaxChunkChars-brewChunkPromptReserve {
			ln = ln[:brewMaxChunkChars-brewChunkPromptReserve]
		}
		if curLen+len(ln)+1 > brewMaxChunkChars-brewChunkPromptReserve || len(cur) >= brewMaxChunkEvents {
			if len(cur) > 0 {
				chunks = append(chunks, strings.Join(cur, "\n"))
			}
			cur, curLen = nil, 0
		}
		cur = append(cur, ln)
		curLen += len(ln) + 1
	}
	if len(cur) > 0 {
		chunks = append(chunks, strings.Join(cur, "\n"))
	}
	return chunks
}

// judgeChunk asks the LLM whether a chunk holds lessons (three-condition
// gate). Returns the accepted candidates (0 when the gate rejects).
func judgeChunk(ctx context.Context, bc brewClient, chunk string) ([]brewCandidate, error) {
	var reply brewJudgeReply
	if err := callBrewJSON(ctx, bc, "你输出严格的 JSON。", brewJudgePrompt+chunk, &reply); err != nil {
		return nil, err
	}
	if len(reply.Lessons) > brewMaxLessonsPerSession {
		reply.Lessons = reply.Lessons[:brewMaxLessonsPerSession]
	}
	return reply.Lessons, nil
}

// errThinBreadcrumbs marks the RETRYABLE distill failure: the model produced
// a usable QA row but too few retrieval breadcrumbs (5W1H 面包屑哲学 — a row
// below brewMinKeywords is barely retrievable, so it is not worth storing).
// Unlike a malformed reply, this is a SAMPLING shortfall: the same prompt
// re-asked legitimately returns a different keyword list — the failure the
// 2026-09-14 sweep hit ("too few breadcrumbs (1 < 6)").
var errThinBreadcrumbs = errors.New("brew: too few breadcrumbs")

// promptTag names the pipeline stage a prompt belongs to, so a failed call
// reports WHICH call failed instead of a generic "some LLM call".
func promptTag(user string) string {
	switch {
	case strings.HasPrefix(user, brewWorkflowPrompt):
		return "distill workflow"
	case strings.HasPrefix(user, brewJudgePrompt):
		return "judge"
	case strings.HasPrefix(user, brewDistillPrompt):
		return "distill"
	default:
		return "unknown prompt"
	}
}

// withBreadcrumbRepair runs one, re-asks at most tries times when the reply
// was thin, and returns the last error. Only errThinBreadcrumbs is retried: a
// malformed reply or a transport failure must not spend extra LLM calls.
func withBreadcrumbRepair(tries int, one func() error) error {
	var err error
	for attempt := 0; ; attempt++ {
		if err = one(); err == nil || !errors.Is(err, errThinBreadcrumbs) {
			return err
		}
		if attempt >= tries {
			return fmt.Errorf("%w — re-asked %d×, still too few retrieval keywords; lesson skipped", err, tries)
		}
	}
}

// distillLesson turns one accepted candidate into a QA row. Returns an error
// when the LLM reply is unusable or the QA is too thin to be retrievable
// (question/answer empty, too few breadcrumbs — the latter buys one re-ask).
func distillLesson(ctx context.Context, bc brewClient, cand brewCandidate) (*brewDistillReply, error) {
	var out *brewDistillReply
	err := withBreadcrumbRepair(brewDistillRepairTries, func() error {
		reply, err := distillLessonOnce(ctx, bc, cand)
		if err != nil {
			return err
		}
		out = reply
		return nil
	})
	return out, err
}

// distillLessonOnce is the single-shot distill call (validation included).
func distillLessonOnce(ctx context.Context, bc brewClient, cand brewCandidate) (*brewDistillReply, error) {
	var reply brewDistillReply
	lesson := "主题: " + cand.Topic + "\n依据: " + clampRunes(cand.Evidence, brewMaxLessonChars)
	if err := callBrewJSON(ctx, bc, "你输出严格的 JSON。", brewDistillPrompt+lesson, &reply); err != nil {
		return nil, err
	}
	reply.Question = strings.TrimSpace(reply.Question)
	reply.Answer = strings.TrimSpace(reply.Answer)
	reply.Keywords = clampKeywords(reply.Keywords)
	reply.Topic = strings.TrimSpace(cand.Topic)
	if reply.Topic == "" {
		return nil, fmt.Errorf("brew: judged topic empty")
	}
	if reply.Question == "" || reply.Answer == "" {
		return nil, fmt.Errorf("brew: distilled QA empty for topic %q", cand.Topic)
	}
	if len(reply.Keywords) < brewMinKeywords {
		return nil, fmt.Errorf("%w (%d < %d) for topic %q", errThinBreadcrumbs,
			len(reply.Keywords), brewMinKeywords, cand.Topic)
	}
	return &reply, nil
}

// distillWorkflow distills a workflow-kind candidate into a procedural row
// (the brew 蒸馏 extension: 工作流类 lesson 分流到 procedural 表). Same
// validation discipline as distillLesson.
func distillWorkflow(ctx context.Context, bc brewClient, cand brewCandidate) (*brewWorkflowReply, error) {
	var reply brewWorkflowReply
	lesson := "主题: " + cand.Topic + "\n依据: " + clampRunes(cand.Evidence, brewMaxLessonChars)
	if err := callBrewJSON(ctx, bc, "你输出严格的 JSON。", brewWorkflowPrompt+lesson, &reply); err != nil {
		return nil, err
	}
	reply.Name = strings.TrimSpace(reply.Name)
	reply.Trigger = strings.TrimSpace(reply.Trigger)
	reply.Preconditions = strings.TrimSpace(reply.Preconditions)
	reply.Keywords = clampKeywords(reply.Keywords)
	reply.Topic = strings.TrimSpace(cand.Topic)
	if reply.Topic == "" {
		return nil, fmt.Errorf("brew: judged topic empty")
	}
	if reply.Name == "" || reply.Trigger == "" {
		return nil, fmt.Errorf("brew: distilled workflow missing name/trigger for topic %q", cand.Topic)
	}
	if len(reply.Steps) == 0 {
		return nil, fmt.Errorf("brew: distilled workflow has no steps for topic %q", cand.Topic)
	}
	return &reply, nil
}

// clampRunes truncates s to n runes (ellipsis suffix on cut).
func clampRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// clampKeywords trims keywords to the cap, drops empties and over-long ones.
func clampKeywords(kws []string) []string {
	var out []string
	for _, k := range kws {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if r := []rune(k); len(r) > brewMaxKeywordRunes {
			k = string(r[:brewMaxKeywordRunes])
		}
		out = append(out, k)
	}
	if len(out) > 14 {
		out = out[:14]
	}
	return out
}

// nowUnix is swappable in tests.
var nowUnix = func() float64 { return float64(time.Now().Unix()) }
