package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubBrewClient is a deterministic brewClient: it replays scripted replies
// per call index (judge replies then distill replies, in order).
type stubBrewClient struct {
	replies []string
	calls   int
}

func (s *stubBrewClient) Chat(ctx context.Context, system, user string) (string, error) {
	if s.calls >= len(s.replies) {
		return "", fmt.Errorf("stub: unexpected call #%d", s.calls)
	}
	r := s.replies[s.calls]
	s.calls++
	return r, nil
}

// writeSessionJSONL writes a minimal but realistic session JSONL.
func writeSessionJSONL(t *testing.T, dir, name string, events ...map[string]interface{}) string {
	t.Helper()
	var sb strings.Builder
	for _, ev := range events {
		b, _ := json.Marshal(ev) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		sb.Write(b)
		sb.WriteByte('\n')
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// --- LoadSessionText / slicer ---

func TestLoadSessionTextRendersAndSkipsInjections(t *testing.T) {
	dir := t.TempDir()
	path := writeSessionJSONL(t, dir, "s1.jsonl",
		map[string]interface{}{"type": "session_start", "sessionId": "x", "model": "m"},
		map[string]interface{}{"type": "user", "message": map[string]interface{}{
			"role": "user", "content": "fix the fts rebuild bug"},
		},
		map[string]interface{}{"type": "assistant", "message": map[string]interface{}{
			"role": "assistant", "content": []map[string]interface{}{
				{"type": "thinking", "thinking": "root cause: FTS external table needs rebuild"},
				{"type": "tool_use", "id": "c1", "name": "bash", "input": map[string]interface{}{"command": "go test ./... and more"}},
			},
		}},
		map[string]interface{}{"type": "request", "prompt_tokens": 10, "completion_tokens": 5},
		map[string]interface{}{"type": "session_end", "exit_code": 0},
	)
	got, err := LoadSessionText(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[user] fix the fts rebuild bug", "[thinking] root cause", "[tool_use] bash({\"command\":\"go test ./... and more\"})"} {
		if !strings.Contains(got, want) {
			t.Fatalf("transcript missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "session_end") || strings.Contains(got, "prompt_tokens") {
		t.Fatalf("stats/injection events must be skipped:\n%s", got)
	}
}

func TestSliceSessionCaps(t *testing.T) {
	// 90 events of ~1000 chars each, cap is 12000-2500=9500 chars & 40 events
	// → several chunks, none over the char cap, none over 40 events.
	var b strings.Builder
	for i := 0; i < 90; i++ {
		b.WriteString(strings.Repeat("x", 1000) + "\n")
	}
	chunks := sliceSession(b.String())
	if len(chunks) < 3 {
		t.Fatalf("want >=3 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len(c) > brewMaxChunkChars-brewChunkPromptReserve {
			t.Fatalf("chunk %d over char cap: %d", i, len(c))
		}
		if n := strings.Count(c, "\n") + 1; n > brewMaxChunkEvents {
			t.Fatalf("chunk %d over event cap: %d", i, n)
		}
	}
}

// --- judge gate ---

func TestJudgeChunkThreeConditions(t *testing.T) {
	ctx := context.Background()
	sample := "[user] hi\n[assistant] ok"
	// No lesson → empty array honored (gate rejects).
	stub := &stubBrewClient{replies: []string{`前置说明这不是JSON {"lessons":[]} 后缀`}}
	cands, err := judgeChunk(ctx, stub, sample)
	if err != nil || len(cands) != 0 {
		t.Fatalf("gate must accept the empty verdict: %v %v", cands, err)
	}
	// Lesson found, over the per-session cap → clamped to 3.
	many := `{"lessons":[`
	for i := 0; i < 5; i++ {
		if i > 0 {
			many += ","
		}
		many += fmt.Sprintf(`{"topic":"t%d","evidence":"e%d"}`, i, i)
	}
	many += `]}`
	stub = &stubBrewClient{replies: []string{many}}
	cands, err = judgeChunk(ctx, stub, sample)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != brewMaxLessonsPerSession {
		t.Fatalf("want clamp to %d, got %d", brewMaxLessonsPerSession, len(cands))
	}
}

// --- distiller ---

func TestDistillLessonValidation(t *testing.T) {
	ctx := context.Background()
	cand := brewCandidate{Topic: "FTS rebuild after insert", Evidence: "INSERT into fts needs rebuild"}
	// Too few keywords → refused (breadcrumb philosophy).
	stub := &stubBrewClient{replies: []string{`{"question":"q","answer":"a","keywords":["a","b"]}`}}
	if _, err := distillLesson(ctx, stub, cand); err == nil {
		t.Fatal("want error for too-few keywords")
	}
	// Good reply → topic copied from candidate, keywords clamped.
	stub = &stubBrewClient{replies: []string{`{"question":"distilled_qa insert 后搜不到? FTS rebuild when?","answer":"must rebuild","keywords":["fts rebuild 重建","insert 后检索不到","CJK unicode61","search_kw 列","蒸馏 distill","FTS5 external table","QA 入库 ingest","检索检索"]}`}}
	qa, err := distillLesson(ctx, stub, cand)
	if err != nil {
		t.Fatal(err)
	}
	if qa.Topic != cand.Topic {
		t.Fatalf("topic must come from the judged candidate: %q", qa.Topic)
	}
}

// TestIsAlwaysThinkingError: the GLM 400 marker must be recognized so the
// brew client flips to reasoning_effort=low (endpoint-adaptive throttle).
func TestIsAlwaysThinkingError(t *testing.T) {
	glmErr := errors.New(`deepseek: API error (status 400): [1210][该模型始终思考，不支持关闭思考；请使用 low、high 或 max。]`)
	if !isAlwaysThinkingError(glmErr) {
		t.Fatal("GLM always-thinking 400 not recognized")
	}
	if isAlwaysThinkingError(errors.New("some other error")) {
		t.Fatal("false positive on unrelated error")
	}
}

// --- anti-self-reference ---

func TestStoreBrewLessonTagsSourceAndNeverOverwritesNonBrew(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir()) // isolated DB
	qa := &brewDistillReply{
		Question: "如何批量回填 5W1H keywords? how to backfill exam columns?",
		Answer:   "用 scripts/backfill_exam_5w1h.py，batch=10 parallel=2，先 finalize_schema 再回填。",
		Keywords: []string{"backfill 回填", "5W1H keywords 面包屑", "batch 批量 parallel", "exam_questions 列", "finalize_schema 建索引", "21 rows/min 速率", "sqlite copy 副本", "验收 acceptance"},
		Topic:    "批量回填 keywords",
	}

	// Scenario A: fresh store → row lands tagged brew; re-store dedupes.
	deduped, err := storeBrewLesson(qa)
	if err != nil || deduped {
		t.Fatalf("first store: deduped=%v err=%v", deduped, err)
	}
	if deduped, err = storeBrewLesson(qa); err != nil || !deduped {
		t.Fatalf("re-store must dedupe (idempotent re-run): deduped=%v err=%v", deduped, err)
	}
	db := openTestStore(t)
	var sources, answer string
	if err := db.QueryRow(`SELECT COALESCE(sources,''), answer FROM distilled_qa WHERE question=?`, qa.Question).Scan(&sources, &answer); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sources, BrewSourceTag) {
		t.Fatalf("brew row must be tagged source=%q, sources=%q", BrewSourceTag, sources)
	}

	// Scenario B: the existing matching row is human-stored (non-brew) →
	// brew must refuse the write and leave the human answer untouched.
	if _, err := db.Exec(`UPDATE distilled_qa SET sources='manual', answer='human curated answer — must survive' WHERE question=?`, qa.Question); err != nil {
		t.Fatal(err)
	}
	deduped, err = storeBrewLesson(qa)
	if err != nil {
		t.Fatal(err)
	}
	if !deduped {
		t.Fatal("brew write against a non-brew row must be refused (reported deduped)")
	}
	if err := db.QueryRow(`SELECT answer FROM distilled_qa WHERE question=?`, qa.Question).Scan(&answer); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(answer, "human curated answer") {
		t.Fatalf("non-brew row was overwritten! answer=%q", answer)
	}
}

func openTestStore(t *testing.T) *sql.DB {
	t.Helper()
	db, err := openStoreDB(context.Background(), memoryDBPath())
	if err != nil {
		t.Fatal(err)
	}
	//stylegate:ignore process-lifetime handle closed at shutdown; the OS reclaims the descriptor
	t.Cleanup(func() { db.Close() })
	return db
}

// --- orchestrator ---

func TestRunBrewEndToEndWithStub(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	dir := t.TempDir()
	writeSessionJSONL(t, dir, "a.jsonl",
		map[string]interface{}{"type": "session_start", "sessionId": "a"},
		map[string]interface{}{"type": "user", "message": map[string]interface{}{
			"role": "user", "content": "distill this: worktree shares refs, no push needed"},
		},
	)
	stub := &stubBrewClient{replies: []string{
		// judge reply
		`{"lessons":[{"topic":"worktree no-push","evidence":"worktree shares refs"}]}`,
		// distill reply
		`{"question":"worktree 和 clone 的 push 语义区别? git worktree shares refs?","answer":"worktree 提交即更新分支，无需 push。","keywords":["worktree worktree", "push push", "clone 克隆", "refs 分支共享", "git git", "共享对象库 object store", "交付 deliverable", "验收 verify"]}`,
	}}
	opts := RunBrewOptions{SessionsDir: dir}
	opts.Progress = func(string, ...interface{}) {}
	stats, err := RunBrew(context.Background(), stub, opts)
	if err != nil {
		t.Fatal(err)
	}
	if stats.SessionsScanned != 1 || stats.Stored != 1 || stats.ChunksJudged != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	// memory_search must find it (⑤ retrieval verification).
	out, err := MemorySearch("worktree push 语义 区别", 3, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "worktree") {
		t.Fatalf("brew row not retrievable via memory_search:\n%s", out)
	}
}

func TestRunBrewResilientToErrors(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	dir := t.TempDir()
	writeSessionJSONL(t, dir, "a.jsonl",
		map[string]interface{}{"type": "user", "message": map[string]interface{}{"role": "user", "content": "x"}},
	)
	// Judge replies with garbage → error counted, no fatal.
	stub := &stubBrewClient{replies: []string{"totally not json"}}
	stats, err := RunBrew(context.Background(), stub, RunBrewOptions{SessionsDir: dir})
	if err != nil {
		t.Fatalf("one bad reply must not fail the run: %v", err)
	}
	if len(stats.Errors) == 0 {
		t.Fatal("error must be counted in stats")
	}
}
