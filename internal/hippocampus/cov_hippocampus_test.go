package hippocampus

// cov_hippocampus_test.go — the function-level coverage slice for the
// internal/hippocampus organ (epic: cov-hippocampus-hp). Tests only: no
// product code is touched by this file.
//
// The six functions the slice closes are the three ToolsMemory adapter
// methods that had no caller naming them (Search / Store / Get), the two
// adapter members that were only reached through the Cycle
// (Update / Feedback), and the host constructor (NewHostEngine). The metric
// (scripts/func_coverage.py) is name-appearance, so every one of them is
// exercised by a REAL call with real assertions — never a bare mention.
// The two adapter functions the Cycle already named (New / Context) get
// direct shape assertions too, so the whole 14-function package surface is
// reached by an explicit call site.
//
// Helper names all carry the task-unique prefix covHipp* (merge-collision
// guard against a sibling coverage slice of the same package, memory #1694).

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nick/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// Fixtures (task-unique names: covHipp*)
// ---------------------------------------------------------------------------

// covHippTimeoutEnv names the whole-cycle env knob (config.go's envTimeoutSec)
// with the SAME value the product's config reads — the constant itself is
// unexported, so the value is stated once here and used once.
const covHippTimeoutEnv = "GOPI_HIPPOCAMPUS_TIMEOUT_SEC"

// covHippDB points the process's memory surface at a fresh temp DB and
// returns the path. GOPI_MEMORY_DB is spelled through config-adjacent
// knowledge: internal/tools keeps memoryDBEnv unexported, so the value is
// pinned here in ONE place (the existing engine_test helper does the same).
func covHippDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)
	return path
}

// covHippSeed distills one record through the SAME in-process surface the
// organ uses, then reads it back to learn its id.
func covHippSeed(t *testing.T, topic, question, answer string, keywords []string) int {
	t.Helper()
	memory := ToolsMemory{}
	if _, err := memory.Store(topic, question, answer, SourceTag, keywords); err != nil {
		t.Fatalf("seed %q: %v", topic, err)
	}
	hits, err := memory.Search(question, DefaultRetrievalLimit, RetrievalIntent)
	if err != nil {
		t.Fatalf("seed lookup %q: %v", topic, err)
	}
	for _, h := range hits {
		if h.Topic == topic {
			return h.ID
		}
	}
	t.Fatalf("seeded record %q not recallable", topic)
	return 0
}

// covHippHostClient is the host-side transport double NewHostEngine accepts:
// scripted replies + a recorded call count.
type covHippHostClient struct {
	replies []string
	err     error
	calls   int
}

func (c *covHippHostClient) Chat(ctx context.Context, system, user string) (string, error) {
	idx := c.calls
	c.calls++
	if c.err != nil {
		return "", c.err
	}
	if len(c.replies) == 0 {
		return "", nil
	}
	if idx < len(c.replies) {
		return c.replies[idx], nil
	}
	return c.replies[len(c.replies)-1], nil
}

// ---------------------------------------------------------------------------
// ToolsMemory.Search — real recall against a temp DB
// ---------------------------------------------------------------------------

// TestCovHippToolsMemorySearchRecallAndBoundaries: Search returns the
// structured hit of a seeded record; a query whose tokens do NOT cover the
// row returns an empty (never nil-deref) slice; limit<=0 falls back to the
// product default; a blank query is an error path.
func TestCovHippToolsMemorySearchRecallAndBoundaries(t *testing.T) {
	covHippDB(t)
	id := covHippSeed(t, "covhipp wal checkpointing",
		"how is the memory db checkpointed safely?",
		"Run PRAGMA wal_checkpoint(TRUNCATE) off the hot path. keywords: wal checkpoint safe",
		[]string{"wal", "checkpoint"})

	memory := ToolsMemory{}

	t.Run("happy path returns the structured row", func(t *testing.T) {
		hits, err := memory.Search("covhipp wal checkpointing", DefaultRetrievalLimit, RetrievalIntent)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		var found *tools.MemoryHit
		for i := range hits {
			if hits[i].ID == id {
				found = &hits[i]
			}
		}
		if found == nil {
			t.Fatalf("seeded row #%d not recalled: %+v", id, hits)
		}
		if found.Topic == "" || found.Question == "" || found.Answer == "" {
			t.Errorf("hit is not fully structured: %+v", *found)
		}
		if found.Tier == "" {
			t.Errorf("hit carries no tier: %+v", *found)
		}
	})

	t.Run("unrelated query returns nothing but no error", func(t *testing.T) {
		hits, err := memory.Search("zzqq unrelated-onomatopoeia-xyzzy", DefaultRetrievalLimit, RetrievalIntent)
		if err != nil {
			t.Fatalf("Search (miss): %v", err)
		}
		if len(hits) != 0 {
			t.Errorf("unrelated query recalled %d rows: %+v", len(hits), hits)
		}
	})

	t.Run("limit boundary: zero falls back, one truncates", func(t *testing.T) {
		covHippSeed(t, "covhipp limit boundary row",
			"does a zero limit still recall covhipp limit boundary?", "Yes, the default applies. keywords: covhipp limit boundary",
			[]string{"covhipp", "limit", "boundary"})
		fallback, err := memory.Search("covhipp limit boundary", 0, RetrievalIntent)
		if err != nil {
			t.Fatalf("Search(limit=0): %v", err)
		}
		if len(fallback) == 0 {
			t.Error("limit=0 recalled nothing — the product default was not applied")
		}
		one, err := memory.Search("covhipp limit boundary", 1, RetrievalIntent)
		if err != nil {
			t.Fatalf("Search(limit=1): %v", err)
		}
		if len(one) > 1 {
			t.Errorf("limit=1 returned %d hits", len(one))
		}
	})

	t.Run("error path: blank query is refused", func(t *testing.T) {
		if _, err := memory.Search("   ", DefaultRetrievalLimit, RetrievalIntent); err == nil {
			t.Error("blank query accepted, want a refusal")
		}
	})
}

// ---------------------------------------------------------------------------
// ToolsMemory.Store — distills a row that reads back
// ---------------------------------------------------------------------------

// TestCovHippToolsMemoryStoreReadsBackAndRefusesGarbage: Store writes a row
// carrying the hippocampus source tag + keyword breadcrumbs (read back), and
// refuses a missing-field and a garbage payload without writing metadata.
func TestCovHippToolsMemoryStoreReadsBackAndRefusesGarbage(t *testing.T) {
	covHippDB(t)
	memory := ToolsMemory{}

	t.Run("happy path writes a tagged, breadcrumbed row", func(t *testing.T) {
		if _, err := memory.Store("covhipp store contract",
			"what does the store adapter guarantee?",
			"A tagged row with keyword breadcrumbs. keywords: covhipp store contract",
			SourceTag, []string{"covhipp", "store"}); err != nil {
			t.Fatalf("Store: %v", err)
		}
		hits, err := memory.Search("covhipp store contract", DefaultRetrievalLimit, RetrievalIntent)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		var found *tools.MemoryHit
		for i := range hits {
			if hits[i].Topic == "covhipp store contract" {
				found = &hits[i]
			}
		}
		if found == nil {
			t.Fatalf("stored row not recallable: %+v", hits)
		}
		carried := false
		for _, s := range found.Sources {
			if strings.Contains(s, SourceTag) {
				carried = true
			}
		}
		if !carried {
			t.Errorf("stored sources = %v, want the %q tag", found.Sources, SourceTag)
		}
		if !strings.Contains(strings.ToLower(found.Answer), "keywords:") {
			t.Errorf("keyword breadcrumbs missing from the stored answer: %q", found.Answer)
		}
	})

	t.Run("boundary: empty field is refused", func(t *testing.T) {
		if _, err := memory.Store("", "covhipp empty topic?", "answer text long enough to matter", SourceTag, nil); err == nil {
			t.Error("empty topic accepted")
		}
	})

	t.Run("error path: garbage payload refused", func(t *testing.T) {
		if _, err := memory.Store("covhipp garbage", "? ?", "?", SourceTag, nil); err == nil {
			t.Error("garbage accepted, want a refusal")
		}
	})
}

// ---------------------------------------------------------------------------
// ToolsMemory.Get — reads one row by id
// ---------------------------------------------------------------------------

// TestCovHippToolsMemoryGetBoundaries: Get renders exactly the requested row,
// and refuses the id<=0 boundary (the scan is never attempted).
func TestCovHippToolsMemoryGetBoundaries(t *testing.T) {
	covHippDB(t)
	want := covHippSeed(t, "covhipp get contract",
		"what does the get adapter return?",
		"The JSON render of exactly one row. keywords: covhipp get contract", nil)
	other := covHippSeed(t, "covhipp get neighbour",
		"is the neighbouring row ever returned?",
		"No — get is by id. keywords: covhipp get neighbour", nil)
	if want == other {
		t.Fatal("fixture is degenerate: both seeds share an id")
	}

	memory := ToolsMemory{}

	t.Run("happy path returns the requested row only", func(t *testing.T) {
		out, err := memory.Get(want)
		if err != nil {
			t.Fatalf("Get(%d): %v", want, err)
		}
		if !strings.Contains(out, "covhipp get contract") {
			t.Errorf("Get(%d) = %q, want the seeded topic", want, out)
		}
		if strings.Contains(out, "covhipp get neighbour") {
			t.Errorf("Get(%d) leaked the neighbouring row: %q", want, out)
		}
	})

	t.Run("boundary: non-positive id is refused", func(t *testing.T) {
		if _, err := memory.Get(0); err == nil {
			t.Error("Get(0) accepted, want a refusal")
		}
		if _, err := memory.Get(-1); err == nil {
			t.Error("Get(-1) accepted, want a refusal")
		}
	})
}

// ---------------------------------------------------------------------------
// ToolsMemory.Update — mutates a row in place
// ---------------------------------------------------------------------------

// TestCovHippToolsMemoryUpdateBoundaries: Update applies a tier/confidence
// change that reads back, leaves an untouched neighbour alone, and refuses
// the id<=0 boundary plus an unknown tier.
func TestCovHippToolsMemoryUpdateBoundaries(t *testing.T) {
	covHippDB(t)
	id := covHippSeed(t, "covhipp update contract",
		"does update mutate in place?",
		"Yes — tier and confidence change on the same row. keywords: covhipp update contract", nil)
	neighbour := covHippSeed(t, "covhipp update neighbour",
		"is the neighbour touched by an update?",
		"No. keywords: covhipp update neighbour", nil)

	memory := ToolsMemory{}
	confidence := 0.33

	t.Run("happy path mutates the row and reads back", func(t *testing.T) {
		if out, err := memory.Update(id, tools.MemoryUpdateFields{
			Tier: tools.TierCold, Confidence: &confidence, Topic: "covhipp update contract (re-tiered)",
		}); err != nil {
			t.Fatalf("Update(%d): %v (out=%q)", id, err, out)
		}
		got, err := memory.Get(id)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		if !strings.Contains(got, "re-tiered") {
			t.Errorf("topic edit not persisted: %q", got)
		}
		untouched, err := memory.Get(neighbour)
		if err != nil {
			t.Fatalf("neighbour read back: %v", err)
		}
		if strings.Contains(untouched, "re-tiered") {
			t.Errorf("update leaked into neighbour #%d: %q", neighbour, untouched)
		}
	})

	t.Run("boundary: non-positive id is refused", func(t *testing.T) {
		if _, err := memory.Update(0, tools.MemoryUpdateFields{Tier: tools.TierCold}); err == nil {
			t.Error("Update(0) accepted, want a refusal")
		}
	})

	t.Run("error path: unknown tier is refused", func(t *testing.T) {
		if _, err := memory.Update(id, tools.MemoryUpdateFields{Tier: "frozen-solid"}); err == nil {
			t.Error("unknown tier accepted, want a refusal")
		}
	})
}

// ---------------------------------------------------------------------------
// ToolsMemory.Feedback — records one vote
// ---------------------------------------------------------------------------

// TestCovHippToolsMemoryFeedbackBoundaries: a useful vote moves the row's
// feedback mass and reads back, a second vote on the same row is additive,
// and both the id<=0 and the unknown-rating boundaries are refused.
func TestCovHippToolsMemoryFeedbackBoundaries(t *testing.T) {
	covHippDB(t)
	id := covHippSeed(t, "covhipp feedback contract",
		"does a vote persist?",
		"Yes — the decayed vote mass moves. keywords: covhipp feedback contract", nil)

	memory := ToolsMemory{}

	t.Run("happy path records the vote", func(t *testing.T) {
		if out, err := memory.Feedback(id, tools.RatingUseful, "adopted the recipe", "task-covhipp"); err != nil {
			t.Fatalf("Feedback(%d): %v (out=%q)", id, err, out)
		}
		hits, err := memory.Search("covhipp feedback contract", DefaultRetrievalLimit, RetrievalIntent)
		if err != nil {
			t.Fatalf("read back: %v", err)
		}
		var found *tools.MemoryHit
		for i := range hits {
			if hits[i].ID == id {
				found = &hits[i]
			}
		}
		if found == nil {
			t.Fatalf("row #%d vanished after a vote: %+v", id, hits)
		}
		if found.FbUseful <= 0 {
			t.Errorf("fb_useful after a %s vote = %v, want > 0", tools.RatingUseful, found.FbUseful)
		}
		if found.FbUseless != 0 {
			t.Errorf("fb_useless moved without an %s vote: %v", tools.RatingUseless, found.FbUseless)
		}
	})

	t.Run("boundary: non-positive id is refused", func(t *testing.T) {
		if _, err := memory.Feedback(0, tools.RatingUseful, "", ""); err == nil {
			t.Error("Feedback(0) accepted, want a refusal")
		}
	})

	t.Run("error path: unknown rating is refused", func(t *testing.T) {
		if _, err := memory.Feedback(id, "ambivalent", "", ""); err == nil {
			t.Error("unknown rating accepted, want a refusal")
		}
	})
}

// ---------------------------------------------------------------------------
// NewHostEngine — the production binding
// ---------------------------------------------------------------------------

// TestCovHippNewHostEngineBoundaries: a bound host gets a usable organ whose
// memory surface is the in-process ToolsMemory and whose cycle runs against
// the injected transport; an unbound host (nil client) is reported, never
// silently accepted.
func TestCovHippNewHostEngineBoundaries(t *testing.T) {
	covHippDB(t)

	t.Run("nil client stays unbound", func(t *testing.T) {
		engine, ok := NewHostEngine(nil)
		if ok {
			t.Error("ok = true for a nil host client")
		}
		if engine != nil {
			t.Errorf("engine = %+v, want nil for a nil host client", engine)
		}
	})

	t.Run("bound engine exposes the in-process memory surface", func(t *testing.T) {
		host := &covHippHostClient{replies: []string{
			`{"queries":["covhipp host engine contract"]}`,
			`{"verb":"none","reason":"nothing to add"}`,
		}}
		engine, ok := NewHostEngine(host)
		if !ok || engine == nil {
			t.Fatalf("NewHostEngine = (%v, %v), want a bound engine", engine, ok)
		}
		if _, isToolsMemory := engine.Context().(ToolsMemory); !isToolsMemory {
			t.Errorf("Context() = %T, want ToolsMemory", engine.Context())
		}
		res, err := engine.Run(context.Background(), Request{Context: "covhipp host engine contract"})
		if err != nil {
			t.Fatalf("Run on a bound engine: %v", err)
		}
		if res.StopReason != StopNoneEmitted {
			t.Errorf("stop = %q, want %q (actions=%+v)", res.StopReason, StopNoneEmitted, res.Actions)
		}
		if host.calls == 0 {
			t.Error("the injected host transport was never called")
		}
	})

	t.Run("client transport failure is recorded, not swallowed", func(t *testing.T) {
		host := &covHippHostClient{err: errors.New("covhipp upstream down")}
		engine, ok := NewHostEngine(host)
		if !ok {
			t.Fatal("NewHostEngine reported unbound for a non-nil client")
		}
		res, err := engine.Run(context.Background(), Request{Context: "covhipp host engine contract"})
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if res.Err == nil || !strings.Contains(res.Err.Error(), "covhipp upstream down") {
			t.Errorf("transport failure not recorded: %v", res.Err)
		}
		if res.StopReason != StopClientError {
			t.Errorf("stop = %q, want %q", res.StopReason, StopClientError)
		}
	})
}

// TestCovHippNewHostEngineRaisesTimeoutFloors: the two timeout knobs are
// RAISED to the host floors (a larger configured value wins — an operator's
// deliberate setting is never lowered).
func TestCovHippNewHostEngineRaisesTimeoutFloors(t *testing.T) {
	covHippDB(t)

	t.Run("a tiny configured timeout is lifted to the floors", func(t *testing.T) {
		t.Setenv(covHippTimeoutEnv, "1")
		engine, ok := NewHostEngine(&covHippHostClient{})
		if !ok || engine == nil {
			t.Fatal("NewHostEngine reported unbound")
		}
		if engine.cfg.Timeout != HostEngineCycleTimeout {
			t.Errorf("cycle timeout = %v, want the host floor %v", engine.cfg.Timeout, HostEngineCycleTimeout)
		}
		if engine.cfg.ClientTimeout < HostEngineClientTimeout {
			t.Errorf("client timeout = %v, want at least %v", engine.cfg.ClientTimeout, HostEngineClientTimeout)
		}
	})

	t.Run("a larger configured value is preserved", func(t *testing.T) {
		cycle, client := HostEngineCycleTimeout, HostEngineClientTimeout
		hugeSec := int((cycle+client)/time.Second) + 60
		t.Setenv(covHippTimeoutEnv, intToString(hugeSec))
		engine, ok := NewHostEngine(&covHippHostClient{})
		if !ok || engine == nil {
			t.Fatal("NewHostEngine reported unbound")
		}
		if engine.cfg.Timeout != time.Duration(hugeSec)*time.Second {
			t.Errorf("cycle timeout = %v, want the configured %ds (floors must never lower it)",
				engine.cfg.Timeout, hugeSec)
		}
	})
}

// intToString renders n in base 10 — one local helper so the test file does
// not grow an import for a single conversion.
func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// ---------------------------------------------------------------------------
// ClientAdapter — the reply-shape robustness layer of host.go
// ---------------------------------------------------------------------------

// TestCovHippClientAdapterRetryContract: a reply with no JSON object at all
// earns exactly ClientAdapterDefaultRetries ADDITIONAL attempts (a rambling
// model must not be handed to the strict-JSON parser with no retry); a usable
// reply is returned on the FIRST attempt; Retries<0 disables retrying; an
// unbound adapter is a recorded failure, never a silent "".
func TestCovHippClientAdapterRetryContract(t *testing.T) {
	ctx := context.Background()

	t.Run("json-less reply is retried then handed over", func(t *testing.T) {
		host := &covHippHostClient{replies: []string{"pure reasoning, no object", `{"verb":"none"}`}}
		adapter := &ClientAdapter{Client: host}
		out, err := adapter.Chat(ctx, "sys", "user")
		if err != nil {
			t.Fatalf("Chat: %v", err)
		}
		if out != `{"verb":"none"}` {
			t.Errorf("out = %q, want the retried reply", out)
		}
		want := 1 + ClientAdapterDefaultRetries
		if host.calls != want {
			t.Errorf("host calls = %d, want %d (initial + %d retry)", host.calls, want, ClientAdapterDefaultRetries)
		}
	})

	t.Run("usable reply costs exactly one attempt", func(t *testing.T) {
		host := &covHippHostClient{replies: []string{`{"queries":["a"]}`}}
		adapter := &ClientAdapter{Client: host}
		if _, err := adapter.Chat(ctx, "sys", "user"); err != nil {
			t.Fatalf("Chat: %v", err)
		}
		if host.calls != 1 {
			t.Errorf("host calls = %d, want 1 (no retry when a JSON object is present)", host.calls)
		}
	})

	t.Run("boundary: Retries<0 disables retrying entirely", func(t *testing.T) {
		host := &covHippHostClient{replies: []string{"no json here at all"}}
		adapter := &ClientAdapter{Client: host, Retries: -1}
		out, err := adapter.Chat(ctx, "sys", "user")
		if err != nil {
			t.Fatalf("Chat: %v", err)
		}
		if out != "no json here at all" {
			t.Errorf("out = %q, want the raw reply", out)
		}
		if host.calls != 1 {
			t.Errorf("host calls = %d, want 1 (retrying disabled)", host.calls)
		}
	})

	t.Run("error path: transport failure propagates unretried", func(t *testing.T) {
		host := &covHippHostClient{err: errors.New("covhipp transport down")}
		adapter := &ClientAdapter{Client: host}
		if _, err := adapter.Chat(ctx, "sys", "user"); err == nil {
			t.Error("transport failure swallowed, want it propagated")
		}
		if host.calls != 1 {
			t.Errorf("host calls = %d, want 1 (a transport error is not retried)", host.calls)
		}
	})

	t.Run("error path: unbound adapter refuses", func(t *testing.T) {
		var unbound *ClientAdapter
		if _, err := unbound.Chat(ctx, "sys", "user"); err == nil {
			t.Error("nil adapter accepted, want a refusal")
		}
		empty := &ClientAdapter{}
		if _, err := empty.Chat(ctx, "sys", "user"); err == nil {
			t.Error("adapter with a nil client accepted, want a refusal")
		}
	})
}

// ---------------------------------------------------------------------------
// Package surface: New / Context / DefaultConfig / LoadConfig / BriefFromResult
// ---------------------------------------------------------------------------

// TestCovHippEngineSurfaceAndNilCollaborators: the constructor pair and the
// exposed memory surface are exercised directly (not only through the Cycle),
// and the nil-collaborator contract is asserted on the direct path.
func TestCovHippEngineSurfaceAndNilCollaborators(t *testing.T) {
	covHippDB(t)
	stub := &stubClient{}

	t.Run("New resolves the env knobs and exposes its surface", func(t *testing.T) {
		engine := New(stub, ToolsMemory{})
		if engine == nil {
			t.Fatal("New returned nil for non-nil collaborators")
		}
		if _, ok := engine.Context().(ToolsMemory); !ok {
			t.Errorf("Context() = %T, want ToolsMemory", engine.Context())
		}
		if engine.cfg.MaxRounds != DefaultMaxRounds {
			t.Errorf("New did not resolve LoadConfig(): MaxRounds = %d, want %d",
				engine.cfg.MaxRounds, DefaultMaxRounds)
		}
	})

	t.Run("nil collaborators are rejected loudly", func(t *testing.T) {
		if _, err := New(nil, ToolsMemory{}).Run(context.Background(), Request{Context: "covhipp nil client"}); err == nil {
			t.Error("nil client accepted")
		}
		if _, err := New(stub, nil).Run(context.Background(), Request{Context: "covhipp nil memory"}); err == nil {
			t.Error("nil memory accepted")
		}
	})
}

// TestCovHippConfigSurfaceBoundaries: DefaultConfig returns the documented
// constants unchanged, LoadConfig falls back on an unparseable/zero override,
// and BriefFromResult stays nil-safe on its boundary input.
func TestCovHippConfigSurfaceBoundaries(t *testing.T) {
	t.Run("DefaultConfig is exactly the constants", func(t *testing.T) {
		got := DefaultConfig()
		want := Config{
			MaxRounds:      DefaultMaxRounds,
			RetrievalLimit: DefaultRetrievalLimit,
			MaxAct:         DefaultMaxAct,
			Timeout:        DefaultTimeout,
			ClientTimeout:  DefaultClientTimeout,
		}
		if got != want {
			t.Errorf("DefaultConfig() = %+v, want %+v", got, want)
		}
	})

	t.Run("LoadConfig ignores an unparseable and a zero override", func(t *testing.T) {
		t.Setenv(covHippTimeoutEnv, "not-a-number")
		if got := LoadConfig().Timeout; got != DefaultTimeout {
			t.Errorf("unparseable override changed Timeout to %v", got)
		}
		t.Setenv(covHippTimeoutEnv, "0")
		if got := LoadConfig().Timeout; got != DefaultTimeout {
			t.Errorf("zero override changed Timeout to %v, want the default", got)
		}
	})

	t.Run("BriefFromResult is nil-safe and deterministic", func(t *testing.T) {
		if b := BriefFromResult(nil); b != nil {
			t.Errorf("BriefFromResult(nil) = %+v, want nil", b)
		}
		empty := BriefFromResult(&Result{Query: "covhipp nothing", StopReason: StopEmptySearch})
		if empty == nil {
			t.Fatal("BriefFromResult on an empty result returned nil")
		}
		again := BriefFromResult(&Result{Query: "covhipp nothing", StopReason: StopEmptySearch})
		if again == nil || empty.LeadsKey != again.LeadsKey || empty.Summary != again.Summary {
			t.Errorf("brief is not deterministic:\n a=%+v\n b=%+v", empty, again)
		}
	})
}
