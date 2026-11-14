<!-- doc-class: historical -->
# Hermes Agent → Go: Memory-Core Re-Evaluation

> **Date:** 2025-08-04  
> **Context:** Previous evaluation (`hermes-go-migration-evaluation.md`) estimated
> ~77 person-days for a full-featured Python→Go translation. **This report re-evaluates
> with drastically reduced scope:** we only clone the "memory" subsystem — chat CLI,
> SQLite storage, FTS5 indexing, pre-LLM RAG hook, and post-response distillation —
> leveraging `~/work/GoPi` as the pre-built agent core.

---

## 1. The Paradigm Shift: From "Translate Hermes" to "Add Memory to GoPi"

The original evaluation asked: *"How hard is it to translate hermes-agent from Python to Go?"*
Answer: very hard (~77 person-days for basic, ~175 for full).

The new question is: *"How hard is it to give GoPi the memory capabilities of Hermes?"*
Answer: **dramatically easier (~8-12 person-days).**

### Why the difference is so large

| Component | Old Plan (Translate Hermes) | New Plan (Extend GoPi) |
|-----------|---------------------------|----------------------|
| Agent loop | Build from scratch (15 days) | **FREE** — GoPi already has it |
| Tool execution | Build from scratch (6 days) | **FREE** — GoPi already has it |
| LLM client | Build for each provider (4 days) | **FREE** — DeepSeek client exists |
| Prompt builder | Jinja2→Go templates (5 days) | **FREE** — hardcoded system prompt |
| Streaming | Build from scratch (5 days) | **FREE** — GoPi streams already |
| Context window | Build from scratch (3 days) | **FREE** — GoPi has trimContext |
| Heartbeat/progress | N/A | **FREE** — GoPi has heartbeat |
| State store (SQLite) | Translate 6,487 LOC (8 days) | Write ~1,200 LOC (2 days) |
| CLI | Translate 16K+80 files (7 days) | Write ~600 LOC (2 days) |
| Pre-LLM RAG hook | Translate memory_manager.py (4 days) | Write ~300 LOC (1 day) |
| Post-response distill | Translate trajectory_compressor (5 days) | Write ~400 LOC (1.5 days) |
| Gateway (all platforms) | Deferred (0) | **SKIP** — not memory-related |
| Plugin system | Deferred (0) | **SKIP** — not needed |
| LSP, Skills, PET, etc. | Deferred (0) | **SKIP** — not memory-related |

> **Bottom line:** GoPi already IS the agent core. We're bolting memory onto
> an existing, working system — not building from zero.

---

## 2. What GoPi Already Provides (Zero New Work)

GoPi (`~/work/GoPi`) is a working Go AI agent with:

| File | Role | LOC |
|------|------|-----|
| `internal/agent/loop.go` | ReAct loop: ask LLM → execute tools → repeat | ~400 |
| `internal/agent/context.go` | Conversation context management (messages, tokens) | ~200 |
| `internal/agent/session.go` | JSONL session logger (to be replaced by SQLite) | ~200 |
| `internal/agent/heartbeat.go` | Stalled-progress detection with reminders | ~100 |
| `internal/agent/tasklist.go` | Task list parsing, validation, formatting | ~200 |
| `internal/deepseek/client.go` | DeepSeek API client with streaming SSE | ~300 |
| `internal/deepseek/types.go` | Request/response type definitions | ~100 |
| `internal/tools/bash.go` | Bash command execution | ~80 |
| `internal/tools/read.go` | File reading with offset/limit | ~50 |
| `internal/tools/write.go` | File writing | ~30 |
| `internal/tools/edit.go` | Surgical string replacement | ~40 |
| `cmd/agent/main.go` | CLI entry point (one-shot, not REPL) | ~100 |
| **Total** | | **~1,800** |

All of this is **tested, working, and directly reusable** for the memory-core project.

---

## 3. What We Need to Build (the "Memory" Layer)

### 3.1 SQLite State Store (`state.db`) — ~1,200 Go LOC, 2 days

This is the heart of the memory system. Modeled on `hermes_state.py` but
**radically simplified** — we skip everything that isn't about storing and
searching chat messages.

#### Schema Design (simplified from Hermes)

```sql
-- Core tables only
CREATE TABLE sessions (
    id          TEXT PRIMARY KEY,
    source      TEXT NOT NULL DEFAULT 'cli',
    model       TEXT,
    title       TEXT,
    parent_session_id TEXT,
    started_at  REAL NOT NULL,
    ended_at    REAL,
    end_reason  TEXT,
    message_count INTEGER DEFAULT 0,
    input_tokens  INTEGER DEFAULT 0,
    output_tokens INTEGER DEFAULT 0
);

CREATE TABLE messages (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id  TEXT NOT NULL REFERENCES sessions(id),
    role        TEXT NOT NULL,       -- 'user', 'assistant', 'tool', 'system'
    content     TEXT,
    tool_name   TEXT,                -- for tool_call/tool_result messages
    tool_calls  TEXT,                -- JSON: array of tool calls
    timestamp   REAL NOT NULL,
    token_count INTEGER
);

-- FTS5 for full-text search over message content
CREATE VIRTUAL TABLE messages_fts USING fts5(
    content,
    tokenize='porter'
);

-- Triggers keep FTS index in sync
CREATE TRIGGER messages_fts_insert AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
END;
-- (delete + update triggers similarly)
```

#### What we intentionally SKIP from Hermes's schema

| Hermes Feature | Why Skip It |
|----------------|-------------|
| `gateway_routing` table | No multi-platform gateway needed |
| `compression_locks` table | No context compression yet |
| `state_meta` table | No arbitrary key-value metadata |
| `jieba_token` column | CJK search via trigram FTS5 only if needed |
| `reasoning_content/details` columns | Stored in `content` as JSON |
| `platform_message_id` | No cross-platform message tracking |
| `observed/active/compacted` flags | No message lifecycle management |
| `handoff_state/platform/error` | No cross-session handoff |
| `billing_provider/base_url/mode` | Billing is a separate concern |
| `cwd/git_branch/git_repo_root` | Not needed for memory |
| `archived/rewind_count` | Session management simplifications |
| `cost_status/source/pricing_version` | Not memory-related |
| Trigrams FTS5 table | Add later if CJK needed |
| Schema migration framework (v1→v19) | Start at v1; simple ALTER TABLE if needed |

**Result:** The Hermes state store is 6,487 lines. Our version will be ~1,200 lines
because we're implementing only the core 20% that handles session CRUD + FTS5 search.

#### Go Implementation Notes

```go
// Use modernc.org/sqlite (pure Go, no CGO) or mattn/go-sqlite3
// WAL mode for concurrent read+write
// Connection pool: 1 writer, multiple readers
// FTS5 queries via: SELECT ... FROM messages_fts WHERE content MATCH ?
```

---

### 3.2 CLI Chat Interface — ~600 Go LOC, 2 days

Replace GoPi's current one-shot `cmd/agent/main.go` with an interactive REPL.

#### Design

```
$ gopi chat
gopi> What is the capital of France?
  ... (streaming response) ...
gopi> /resume abc123     # resume a previous session
gopi> /history           # list recent sessions
gopi> /search "python async"   # full-text search across all sessions
gopi> /export abc123     # export session as markdown
gopi> /quit
```

Implementation: simple `bufio.Scanner` loop with `readline`-style line editing.
No `bubbletea`/`prompt_toolkit` needed — we target functional, not beautiful.

```go
// cmd/gopi/main.go — new chat subcommand
func chatCmd() {
    store := state.NewStore("~/.gopi/state.db")
    scanner := bufio.NewScanner(os.Stdin)
    for {
        fmt.Print("gopi> ")
        if !scanner.Scan() { break }
        input := strings.TrimSpace(scanner.Text())
        // Handle slash commands or pass to agent
        if strings.HasPrefix(input, "/") {
            handleCommand(store, input)
        } else {
            runAgentWithMemory(store, input)
        }
    }
}
```

#### Slash Commands (minimal set)

| Command | Action |
|---------|--------|
| `/resume <id>` | Load session and continue |
| `/history [n]` | List last n sessions |
| `/search <query>` | FTS5 search across messages |
| `/title <id> <text>` | Set session title |
| `/export <id>` | Export session to markdown |
| `/quit`, `/exit` | Exit |

---

### 3.3 Pre-LLM Hook: RAG Memory Injection — ~300 Go LOC, 1 day

Before each user message reaches the LLM, search `state.db` for relevant past
conversations and inject them as context.

#### Algorithm (simple but effective)

```
1. User types: "fix the timeout bug in the scheduler"
2. FTS5 query:  "fix timeout bug scheduler"
3. Fetch top-K matching message blocks (K=3, configurable)
4. Format as system-level context injection:
   "[MEMORY] Relevant past conversations:\n
    --- [session abc123, 2025-07-15]\n
    User: The scheduler has a timeout issue...\n
    Assistant: I identified the problem in pool.go...\n
    ---\n"
5. Prepend to user message before sending to LLM
```

```go
// internal/memory/rag.go
type RAGHook struct {
    store    *state.Store
    maxChunks int           // default: 3
    maxTokens int           // max tokens per chunk: 500
}

func (r *RAGHook) Inject(sessionID, userMessage string) (string, error) {
    chunks, err := r.store.Search(userMessage, r.maxChunks)
    if err != nil || len(chunks) == 0 {
        return userMessage, nil  // no relevant history
    }
    var buf strings.Builder
    buf.WriteString("[MEMORY] Relevant past context:\n\n")
    for _, c := range chunks {
        buf.WriteString(fmt.Sprintf("--- [%s, %s]\n%s\n---\n\n",
            c.SessionID, c.Timestamp.Format("2006-01-02"), truncate(c.Content, r.maxTokens)))
    }
    buf.WriteString("[END MEMORY]\n\n")
    buf.WriteString(userMessage)
    return buf.String(), nil
}
```

**Configuration:** `GOPI_RAG_CHUNKS=3`, `GOPI_RAG_TOKENS=500` env vars.

---

### 3.4 Post-Response Hook: Distillation — ~400 Go LOC, 1.5 days

After the LLM responds, analyze the conversation turn and extract structured
knowledge into `distilled.db` — a separate SQLite database for "brewed" knowledge.

#### Distilled Schema

```sql
CREATE TABLE topics (
    id       INTEGER PRIMARY KEY AUTOINCREMENT,
    name     TEXT UNIQUE NOT NULL,     -- e.g. "scheduler-timeout"
    category TEXT,                     -- e.g. "bug", "pattern", "decision"
    summary  TEXT,                     -- one-line description
    created  REAL NOT NULL
);

CREATE TABLE facts (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    topic_id  INTEGER REFERENCES topics(id),
    content   TEXT NOT NULL,           -- the fact/insight
    source_session TEXT,               -- which session produced this
    source_msg_id INTEGER,             -- which message
    confidence REAL DEFAULT 1.0,       -- 0.0-1.0
    created   REAL NOT NULL
);

CREATE TABLE code_snippets (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    topic_id  INTEGER REFERENCES topics(id),
    language  TEXT,
    code      TEXT NOT NULL,
    description TEXT,
    source_session TEXT,
    created   REAL NOT NULL
);

CREATE VIRTUAL TABLE facts_fts USING fts5(content, tokenize='porter');
```

#### Distillation Triggers

The post-response hook can be triggered:

1. **Always-on** (default): Every non-trivial assistant response is analyzed
2. **Pattern-based**: Only when the response contains code blocks, decisions, or
   explicit "key insight" markers
3. **Manual**: User types `/distill` to force distillation of the current session

#### Extraction Approach

Rather than calling the LLM again (expensive), use simple heuristics:

```go
func (d *Distiller) Process(sessionID string, msg Message) {
    // 1. Extract code blocks → code_snippets table
    for _, block := range extractCodeBlocks(msg.Content) {
        d.storeCodeSnippet(sessionID, block)
    }

    // 2. Extract "decision" patterns → facts table
    //    "We decided to...", "The root cause is...", "Key insight:..."
    for _, fact := range extractFacts(msg.Content) {
        d.storeFact(sessionID, fact)
    }

    // 3. Infer topic from keywords
    topic := inferTopic(msg.Content)
    d.ensureTopic(topic)
}
```

**Optional LLM-powered distillation** (deferred): For higher quality, a separate
agent call could summarize the session and extract structured knowledge. This
can be a separate `gopi distill <session-id>` command.

---

### 3.5 Integration Layer — ~300 Go LOC, 1 day

Wire everything together:

```
┌─────────────────────────────────────────────────────┐
│                    CLI (cmd/gopi)                     │
│  chat REPL, slash commands, session management       │
└──────────┬───────────────────────────┬──────────────┘
           │                           │
           ▼                           ▼
┌──────────────────┐     ┌──────────────────────────┐
│   State Store    │     │    Pre-LLM RAG Hook       │
│   (state.db)     │◄───►│    (memory injection)     │
│   sessions       │     └──────────┬───────────────┘
│   messages       │               │
│   messages_fts   │               ▼
└────────┬─────────┘     ┌──────────────────────────┐
         │               │      GoPi Agent Core      │
         │               │   (loop.go, tools, LLM)   │
         │               └──────────┬───────────────┘
         │                          │
         ▼                          ▼
┌──────────────────┐     ┌──────────────────────────┐
│  Post-Response   │     │    DeepSeek API Client    │
│  Distill Hook    │     │    (streaming, retry)     │
│  (distilled.db)  │     └──────────────────────────┘
└──────────────────┘
```

The integration replaces GoPi's current `SessionLogger` (JSONL files) with
the SQLite state store, and adds pre/post hooks as middleware in the agent loop.

---

## 4. Revised Workload Estimate

| # | Task | Go LOC | Days | Difficulty |
|---|------|--------|------|------------|
| 1 | SQLite state store (schema, CRUD, FTS5, WAL) | ~1,200 | 2.0 | Medium |
| 2 | CLI chat REPL + slash commands | ~600 | 2.0 | Low-Medium |
| 3 | Pre-LLM RAG hook (FTS5 search + injection) | ~300 | 1.0 | Low |
| 4 | Post-response distill hook (heuristic extraction) | ~400 | 1.5 | Low-Medium |
| 5 | Integration (wire state store into agent loop) | ~300 | 1.0 | Low |
| 6 | Tests for state store + RAG + distill | ~500 | 1.5 | Low-Medium |
| 7 | Documentation + devdoc | — | 0.5 | Low |
| **TOTAL** | | **~3,300** | **~9.5 days** | |

**≈ 2 person-weeks for a single experienced Go developer.**

Compare:
- Previous evaluation: **~77 person-days (15 weeks)** for full basic translation
- This evaluation: **~10 person-days (2 weeks)** for memory-core

**That's an 87% reduction in effort** — because we're not translating Hermes,
we're extending GoPi with its memory subsystem.

---

## 5. Component-by-Component Feasibility

### 5.1 SQLite State Store — ✅ LOW RISK

Go's SQLite ecosystem is mature: `modernc.org/sqlite` (pure Go, no CGO) or
`mattn/go-sqlite3` (CGO, faster). FTS5 is supported by both.

**Risk: None.** This is straightforward CRUD + search. The Hermes state store
is 6,487 lines because it handles gateway routing, compression locks, schema
migrations across 19 versions, handoff state, billing tracking, and CJK trigram
search. None of that complexity applies here.

### 5.2 CLI Chat — ✅ LOW RISK

GoPi is currently one-shot (read stdin → run → print result → exit). Converting
to a REPL is a simple loop. No `bubbletea`/`prompt_toolkit` needed for v1.

**Risk: None.** `bufio.Scanner` + `fmt.Print` is ~50 lines for a basic REPL.

### 5.3 Pre-LLM RAG Hook — ✅ LOW RISK

FTS5 does the heavy lifting. The hook is: search → format → prepend.

**Risk: Very low.** The only design decision is how many chunks and how to format
them. Both are trivial to tune.

### 5.4 Post-Response Distillation — ⚠️ LOW-MEDIUM RISK

The heuristic approach (regex/code-block extraction) is simple but limited.
LLM-powered distillation would be better but adds cost and latency.

**Risk: Low for v1 (heuristic), Medium for v2 (LLM-powered).**
Start with heuristics; add an optional `gopi distill --llm` command later.

### 5.5 Integration — ✅ LOW RISK

GoPi's agent loop is clean and well-factored. Adding hooks is a matter of
injecting calls at the right points.

**Risk: None.** The loop already has `AddUserMessage()` — the RAG hook just
modifies the message before it's added.

---

## 6. What We Explicitly Do NOT Build

To keep scope minimal, these are **deliberately excluded**:

| Excluded | Why |
|----------|-----|
| Context compression / summarization | Complex LLM-calling subsystem; defer |
| Multi-model support (Anthropic, Bedrock, etc.) | GoPi is DeepSeek-only; add later |
| Gateway (Telegram, Discord, Slack, etc.) | CLI-only is sufficient |
| Plugin system | Compile-time everything |
| LSP integration | Not memory-related |
| Skills system | Not memory-related |
| Web dashboard | Not memory-related |
| Trigrams FTS5 (CJK search) | Add when needed; porter tokenizer handles English |
| LLM-powered distillation | v2 feature; heuristic extraction first |
| Schema migration framework | Start at v1; ALTER TABLE when needed |

---

## 7. New vs. Old Comparison

| Dimension | Previous Evaluation | This Re-Evaluation |
|-----------|-------------------|-------------------|
| **Scope** | Full hermes-agent core | Memory subsystem only |
| **Agent core** | Build from scratch | **GoPi is the core** |
| **State store** | Translate 6,487 LOC Python | New ~1,200 LOC Go |
| **CLI** | Translate 16K + 80 files | New ~600 LOC Go |
| **Pre-LLM hook** | Translate memory_manager.py | New ~300 LOC Go |
| **Post hook** | Translate trajectory_compressor | New ~400 LOC Go |
| **Total new code** | ~15,000 Go LOC | ~3,300 Go LOC |
| **Effort** | ~77 person-days | ~10 person-days |
| **Biggest risk** | Streaming + tool interleaving | None (already solved) |
| **Hardest component** | Conversation loop (5,455 LOC) | State store (1,200 LOC) |

---

## 8. Recommended Implementation Order

### Day 1-2: State Store Foundation
- [ ] `internal/state/schema.go` — SQL schema constants
- [ ] `internal/state/store.go` — Open(), Close(), WAL config
- [ ] `internal/state/sessions.go` — Create, Get, List, Update, Delete sessions
- [ ] `internal/state/messages.go` — Insert, GetBySession, FTS5 search
- [ ] `internal/state/store_test.go` — Basic CRUD + search tests

### Day 3-4: CLI Chat
- [ ] `cmd/gopi/chat.go` — REPL loop with slash commands
- [ ] `cmd/gopi/main.go` — Subcommand routing (chat, search, export)
- [ ] Session management: `/resume`, `/history`, `/title`, `/export`

### Day 5: Pre-LLM RAG Hook
- [ ] `internal/memory/rag.go` — FTS5 search + context injection
- [ ] Integrate into agent loop (modify `AddUserMessage` call site)

### Day 6-7: Post-Response Distill Hook
- [ ] `internal/memory/distill.go` — Heuristic extraction
- [ ] `internal/memory/distilled_schema.go` — Distilled DB schema
- [ ] Integrate into agent loop (after assistant response)

### Day 8: Integration + Polish
- [ ] Replace JSONL SessionLogger with SQLite state store in loop.go
- [ ] Wire pre/post hooks into Run()
- [ ] End-to-end integration test

### Day 9-10: Testing + Docs
- [ ] Comprehensive state store tests
- [ ] RAG + distill unit tests
- [ ] `devdoc/hermes-memory-go-implementation.md`

---

## 9. Key Dependencies

| Dependency | Purpose | Notes |
|-----------|---------|-------|
| `modernc.org/sqlite` | Pure-Go SQLite with FTS5 | No CGO needed; slower but simpler |
| _or_ `mattn/go-sqlite3` | CGO SQLite with FTS5 | Faster; requires gcc |
| `github.com/google/uuid` | Session ID generation | Already imported by GoPi indirectly |

Everything else is Go standard library.

---

## 10. Verdict

| Dimension | Rating |
|-----------|--------|
| **Overall feasibility** | ✅✅✅ **Highly feasible** |
| **Effort** | ~10 person-days (2 weeks) |
| **Hardest component** | SQLite state store (straightforward CRUD) |
| **Biggest risk** | None significant |
| **Best advantage** | GoPi provides the entire agent core for free |
| **Worst disadvantage** | Heuristic distillation is lower quality than LLM-powered |

### Bottom Line

This is **not a hermes-agent translation**. It's a **GoPi memory extension**.
The agent loop, tool execution, LLM client, streaming, context management, and
heartbeat tracking are **already built and working** in GoPi. We are adding:

1. **SQLite persistence** (replace JSONL files)
2. **FTS5 search** (full-text search across chat history)
3. **Interactive CLI** (replace one-shot stdin mode)
4. **RAG pre-hook** (inject relevant history before LLM calls)
5. **Distill post-hook** (extract structured knowledge after responses)

These are all **well-understood, low-risk features** that can be built
incrementally. The total new code is ~3,300 lines — less than one-fifth of
the previous estimate's 15,000 lines — because GoPi already IS the agent.

**Recommendation: Proceed immediately.** Start with the state store (days 1-2),
which is the foundation everything else builds on.
