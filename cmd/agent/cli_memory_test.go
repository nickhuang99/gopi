package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/tools"
)

// ---------------------------------------------------------------------------
// parseMemoryArgs — the pure front-end of the memory subcommands.
// ---------------------------------------------------------------------------

func TestParseMemorySearchArgs(t *testing.T) {
	opts, err := parseMemoryArgs("memory_search", []string{`VPN 教训`, "--limit", "3", "--intent", "debug", "--db", "/tmp/x.db"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.query != "VPN 教训" {
		t.Errorf("query = %q, want %q", opts.query, "VPN 教训")
	}
	if opts.limit != 3 {
		t.Errorf("limit = %d, want 3", opts.limit)
	}
	if opts.intent != "debug" {
		t.Errorf("intent = %q, want debug", opts.intent)
	}
	if opts.db != "/tmp/x.db" {
		t.Errorf("db = %q, want /tmp/x.db", opts.db)
	}
}

func TestParseMemorySearchMissingQuery(t *testing.T) {
	_, err := parseMemoryArgs("memory_search", nil)
	if err == nil || !strings.Contains(err.Error(), "<query>") {
		t.Fatalf("want missing-query error, got %v", err)
	}
}

func TestParseMemorySearchBadLimit(t *testing.T) {
	_, err := parseMemoryArgs("memory_search", []string{"x", "--limit", "abc"})
	if err == nil || !strings.Contains(err.Error(), "positive integer") {
		t.Fatalf("want bad-limit error, got %v", err)
	}
}

func TestParseMemorySearchUnknownFlag(t *testing.T) {
	_, err := parseMemoryArgs("memory_search", []string{"x", "--bogus", "y"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("want unknown-flag error, got %v", err)
	}
}

func TestParseMemorySearchRejectsStoreFlags(t *testing.T) {
	// Cross-subcommand flags (topic/question/answer/...) are invalid for search.
	_, err := parseMemoryArgs("memory_search", []string{"x", "--topic", "T"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("want unknown-flag error for --topic on search, got %v", err)
	}
}

func TestParseMemoryStoreRejectsSearchFlags(t *testing.T) {
	_, err := parseMemoryArgs("memory_store", []string{"--limit", "3", "--topic", "T", "--question", "Q", "--answer", "A"})
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("want unknown-flag error for --limit on store, got %v", err)
	}
}

func TestParseMemoryStoreArgs(t *testing.T) {
	opts, err := parseMemoryArgs("memory_store", []string{
		"--topic", "T", "--question", "Q", "--answer", "A",
		"--keywords", "a, b,,c", "--source", "S", "--db", "/tmp/y.db",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.topic != "T" || opts.question != "Q" || opts.answer != "A" || opts.source != "S" {
		t.Errorf("fields wrong: %+v", opts)
	}
	if len(opts.keywords) != 3 || opts.keywords[0] != "a" || opts.keywords[1] != "b" || opts.keywords[2] != "c" {
		t.Errorf("keywords = %v, want [a b c]", opts.keywords)
	}
	if opts.db != "/tmp/y.db" {
		t.Errorf("db = %q, want /tmp/y.db", opts.db)
	}
}

func TestParseMemoryStoreMissingRequired(t *testing.T) {
	cases := []struct{ name, args string }{
		{"topic+question only", "--topic T --question Q"},
		{"topic only", "--topic T"},
		{"answer only", "--answer A"},
	}
	for _, c := range cases {
		t.Run(c.args, func(t *testing.T) {
			_, err := parseMemoryArgs("memory_store", strings.Split(c.args, " "))
			if err == nil || !strings.Contains(err.Error(), "Missing required parameter") {
				t.Fatalf("want missing-required error, got %v", err)
			}
		})
	}
}

func TestParseMemoryStoreUnexpectedPositional(t *testing.T) {
	_, err := parseMemoryArgs("memory_store", []string{"--topic", "T", "--question", "Q", "--answer", "A", "stray"})
	if err == nil || !strings.Contains(err.Error(), "positional argument") {
		t.Fatalf("want positional error, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// splitKeywords
// ---------------------------------------------------------------------------

func TestSplitKeywords(t *testing.T) {
	if k := splitKeywords(""); k != nil {
		t.Errorf("empty -> %v, want nil", k)
	}
	got := splitKeywords("  a , b ,, c  ")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// ---------------------------------------------------------------------------
// End-to-end: the CLI dispatch calls the SAME tools.MemorySearch /
// tools.MemoryStore functions the agent loop uses. These tests prove the
// round-trip through those shared paths with a --db override, mirroring
// `gopi memory_store ... --db X` then `gopi memory_search ... --db X`.
// ---------------------------------------------------------------------------

// TestCLIStoreThenSearch stores via the shared tool path (what
// runMemorySubcommand calls for `gopi memory_store ...`) then searches via the
// shared tool path (`gopi memory_search ...`), asserting the CLI and tool
// behavior are one and the same.
func TestCLIStoreThenSearch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	// Store via the shared tool function (the path runMemorySubcommand takes).
	out, err := tools.MemoryStore(
		"CLI 子命令测试",
		"怎么验证 CLI 暴露了 memory 工具？",
		"用同一个 tools.MemorySearch/Store 函数，行为与 tool 调用完全一致。",
		"cli-mem-test",
		[]string{"CLI", "memory", "测试"},
	)
	if err != nil {
		t.Fatalf("MemoryStore: %v", err)
	}
	if !strings.Contains(out, "Memory stored (id=1)") {
		t.Errorf("store out = %q, want confirmation", out)
	}

	// Search via the shared tool function — must hit the record we just wrote.
	got, err := tools.MemorySearch("CLI 子命令测试", 0, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	if !strings.Contains(got, "CLI 子命令测试") || !strings.Contains(got, "Q: 怎么验证 CLI 暴露了 memory 工具？") {
		t.Errorf("search out missing stored record:\n%s", got)
	}
}

// TestCLIStoreDedupSignal proves the deduped signal surfaces, matching the
// tool contract.
func TestCLIStoreDedupSignal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	_, err := tools.MemoryStore("D", "same question", "first answer", "", nil)
	if err != nil {
		t.Fatalf("first store: %v", err)
	}
	dup, err := tools.MemoryStore("D", "same question", "second answer", "", nil)
	if err != nil {
		t.Fatalf("dup store: %v", err)
	}
	if !strings.Contains(dup, "deduped") {
		t.Errorf("dedup out = %q, want deduped signal", dup)
	}
}

// ---------------------------------------------------------------------------
// round-3 maintenance subcommands: memory_get / memory_update / memory_delete.
// ---------------------------------------------------------------------------

// TestParseMemoryGetUpdateDeleteArgs: the id comes from --id N or a single
// positional; update takes the field flags; unknown flags and missing ids are
// rejected at parse time.
func TestParseMemoryGetUpdateDeleteArgs(t *testing.T) {
	// --id flag form.
	opts, err := parseMemoryArgs("memory_get", []string{"--id", "42"})
	if err != nil {
		t.Fatalf("memory_get --id: %v", err)
	}
	if opts.id != 42 {
		t.Errorf("memory_get id = %d, want 42", opts.id)
	}
	// Positional form (ergonomic `gopi memory_get 524`).
	opts, err = parseMemoryArgs("memory_get", []string{"524"})
	if err != nil {
		t.Fatalf("memory_get positional: %v", err)
	}
	if opts.id != 524 {
		t.Errorf("memory_get positional id = %d, want 524", opts.id)
	}
	// memory_update with fields.
	opts, err = parseMemoryArgs("memory_update", []string{"7", "--tier", "cold", "--keywords", "a, b", "--confidence", "0.9"})
	if err != nil {
		t.Fatalf("memory_update: %v", err)
	}
	if opts.id != 7 || opts.tier != "cold" || !opts.hasConf || opts.confidence != 0.9 ||
		!opts.hasKeywords || len(opts.keywords) != 2 {
		t.Errorf("memory_update parse = %+v", opts)
	}
	// memory_delete.
	opts, err = parseMemoryArgs("memory_delete", []string{"--id", "9"})
	if err != nil {
		t.Fatalf("memory_delete: %v", err)
	}
	if opts.id != 9 {
		t.Errorf("memory_delete id = %d, want 9", opts.id)
	}
}

func TestParseMemoryMaintenanceErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"memory_get", []string{}, "missing required parameter: id"},
		{"memory_get", []string{"--id", "abc"}, "positive integer"},
		{"memory_delete", []string{"1", "2"}, "unexpected positional"},
		{"memory_update", []string{"1"}, "nothing to update"},
		{"memory_update", []string{"1", "--confidence", "9"}, "[0,1]"},
		{"memory_update", []string{"1", "--tier", "cold", "--bogus", "x"}, "unknown flag --bogus"},
	} {
		if _, err := parseMemoryArgs(tc.name, tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want containing %q", tc.name, err, tc.want)
		}
	}
}

// ---------------------------------------------------------------------------
// memory_list — the enumeration entry point of the maintenance loop.
// ---------------------------------------------------------------------------

func TestParseMemoryListArgs(t *testing.T) {
	// Default limit (no --limit → engine default 20).
	opts, err := parseMemoryArgs("memory_list", nil)
	if err != nil {
		t.Fatalf("memory_list no args: %v", err)
	}
	if opts.listLimit != 0 {
		t.Errorf("default listLimit = %d, want 0 (engine default)", opts.listLimit)
	}
	// --limit N form.
	opts, err = parseMemoryArgs("memory_list", []string{"--limit", "7"})
	if err != nil {
		t.Fatalf("memory_list --limit: %v", err)
	}
	if opts.listLimit != 7 {
		t.Errorf("listLimit = %d, want 7", opts.listLimit)
	}
	// --db accepted.
	if _, err := parseMemoryArgs("memory_list", []string{"--db", "/tmp/x.db"}); err != nil {
		t.Errorf("memory_list --db rejected: %v", err)
	}
}

func TestParseMemoryListErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--limit", "0"}, "positive integer"},
		{[]string{"--limit", "abc"}, "positive integer"},
		{[]string{"stray"}, "unexpected positional"},
		{[]string{"--id", "5"}, "unknown flag --id"},
	} {
		if _, err := parseMemoryArgs("memory_list", tc.args); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("memory_list %v: err = %v, want containing %q", tc.args, err, tc.want)
		}
	}
}

// TestCLIMemoryListThenGet is the missing-chain regression guard: the CLI can
// enumerate every record's id via memory_list (no search query needed) and
// feed it straight into memory_get — the loop search→get could not start.
func TestCLIMemoryListThenGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	for i, topic := range []string{"chain topic alpha", "chain topic beta", "chain topic gamma"} {
		if _, err := tools.MemoryStore(topic, "q"+string(rune('a'+i)), "answer notes "+topic, "", nil); err != nil {
			t.Fatalf("store %d: %v", i, err)
		}
	}

	// memory_list enumerates all three ids in recent-first order.
	out, err := tools.MemoryListRecent(10)
	if err != nil {
		t.Fatalf("MemoryListRecent: %v", err)
	}
	for _, want := range []string{"#1", "#2", "#3", "chain topic alpha", "chain topic beta", "chain topic gamma"} {
		if !strings.Contains(out, want) {
			t.Errorf("memory_list missing %q:\n%s", want, out)
		}
	}

	// The listed id round-trips through the CLI maintenance path.
	if out, err := tools.MemoryGet(3); err != nil || !strings.Contains(out, "chain topic gamma") {
		t.Errorf("MemoryGet(3) from listed id: out=%q err=%v", out, err)
	}
	if out, err := tools.MemoryUpdate(2, tools.MemoryUpdateFields{Tier: "cold"}); err != nil || !strings.Contains(out, "Memory updated (id=2)") {
		t.Errorf("MemoryUpdate(2) from listed id: out=%q err=%v", out, err)
	}
	if out, err := tools.MemoryDelete(1); err != nil || !strings.Contains(out, "Memory deleted (id=1)") {
		t.Errorf("MemoryDelete(1) from listed id: out=%q err=%v", out, err)
	}
}

// TestCLIUpdateTierColdEndToEnd mirrors `gopi memory_store` → `gopi
// memory_update <id> --tier cold` → `gopi memory_search`: the demoted row
// stops being 1st tier (excluded while a warm row matches).
func TestCLIUpdateTierColdEndToEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.db")
	t.Setenv("GOPI_MEMORY_DB", path)

	_, err := tools.MemoryStore("deepseek v4 flash", "deepseek v4 flash pricing?", "notes about deepseek v4 flash pricing and cost", "", nil)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	_, err = tools.MemoryStore("deepseek v4 flash", "deepseek v4 flash config?", "notes about deepseek v4 flash configuration", "", nil)
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	// Demote row 1 (the pricing note) to cold via the shared update path.
	if out, err := tools.MemoryUpdate(1, tools.MemoryUpdateFields{Tier: "cold"}); err != nil || !strings.Contains(out, "Memory updated (id=1)") {
		t.Fatalf("MemoryUpdate: out=%q err=%v", out, err)
	}
	got, err := tools.MemorySearch("deepseek v4 flash", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch: %v", err)
	}
	if strings.Contains(got, "#1 ") {
		t.Errorf("cold row #1 must be excluded from tier-1 results:\n%s", got)
	}
	if !strings.Contains(got, "#2 ") {
		t.Errorf("warm row #2 must be present:\n%s", got)
	}

	// Inspect + delete round-trip.
	if out, err := tools.MemoryGet(1); err != nil || !strings.Contains(out, "deepseek v4 flash pricing?") {
		t.Errorf("MemoryGet: out=%q err=%v", out, err)
	}
	if out, err := tools.MemoryDelete(2); err != nil || !strings.Contains(out, "Memory deleted (id=2)") {
		t.Fatalf("MemoryDelete: out=%q err=%v", out, err)
	}
	got, err = tools.MemorySearch("deepseek v4 flash", 5, "")
	if err != nil {
		t.Fatalf("MemorySearch after delete: %v", err)
	}
	if !strings.Contains(got, "#1 ") {
		t.Errorf("cold row #1 should fill once no warm row matches:\n%s", got)
	}
}

// ---------------------------------------------------------------------------
// memory_feedback — parse front-end.
// ---------------------------------------------------------------------------

func TestParseMemoryFeedbackVote(t *testing.T) {
	opts, err := parseMemoryArgs("memory_feedback", []string{"524", "--useless", "--reason", "stopword soup", "--task", "t-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.mode != "vote" || opts.id != 524 || opts.rating != "useless" || opts.reason != "stopword soup" || opts.task != "t-1" {
		t.Errorf("opts = %+v", opts)
	}
}

func TestParseMemoryFeedbackVoteRequiredRating(t *testing.T) {
	if _, err := parseMemoryArgs("memory_feedback", []string{"1"}); err == nil || !strings.Contains(err.Error(), "--useful / --useless / --unused") {
		t.Errorf("missing rating must error, got %v", err)
	}
	if _, err := parseMemoryArgs("memory_feedback", []string{"1", "--useful", "--useless"}); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("double rating must error, got %v", err)
	}
}

func TestParseMemoryFeedbackModes(t *testing.T) {
	opts, err := parseMemoryArgs("memory_feedback", []string{"--list"})
	if err != nil || opts.mode != "list" {
		t.Fatalf("--list: %+v err=%v", opts, err)
	}
	opts, err = parseMemoryArgs("memory_feedback", []string{"--list", "--memory-id", "7"})
	if err != nil || opts.mode != "list" || opts.memoryID != 7 {
		t.Fatalf("--list --memory-id: %+v err=%v", opts, err)
	}
	opts, err = parseMemoryArgs("memory_feedback", []string{"--suggest-cold"})
	if err != nil || opts.mode != "suggest-cold" {
		t.Fatalf("--suggest-cold: %+v err=%v", opts, err)
	}
	opts, err = parseMemoryArgs("memory_feedback", []string{"--recompute"})
	if err != nil || opts.mode != "recompute" {
		t.Fatalf("--recompute: %+v err=%v", opts, err)
	}
	if _, err := parseMemoryArgs("memory_feedback", []string{"--list", "--suggest-cold"}); err == nil {
		t.Error("conflicting modes must error")
	}
	// A bare positional after a boolean mode flag is eaten as the flag's
	// value by the hand-rolled parser (the existing --flag value convention)
	// — mode wins, no error.
	opts, err = parseMemoryArgs("memory_feedback", []string{"--suggest-cold", "5"})
	if err != nil || opts.mode != "suggest-cold" {
		t.Errorf("--suggest-cold 5: %+v err=%v", opts, err)
	}
}
