package agent

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Hippocampus host — gate + knobs (epic: hippocampus, child 3/4).
//
// Gate semantics (口径 1, devdoc/hippocampus.md §4.2 "关闭时逐字节不变"):
// GOPI_HIPPOCAMPUS unset/off ⇒ NewAgent builds NO host, every hook below is
// never entered, and the run is byte-identical to today's behaviour.
//
// THIS FILE IS THE ONLY READER of the GOPI_HIPPOCAMPUS_* env namespace
// (Rule 2 — single source of truth for configuration). The gate test
// TestHippocampus_EnvLiteralSingleReader asserts each literal below occurs in
// no other production file of this package (Rule 13).

const (
	// hippocampusEnvEnabled is the master switch. Default OFF — see the gate
	// semantics above.
	hippocampusEnvEnabled = "GOPI_HIPPOCAMPUS"
	// hippocampusEnvMaxLv caps the delivery level the scheduler will use.
	hippocampusEnvMaxLv = "GOPI_HIPPOCAMPUS_MAX_LV"
	// hippocampusEnvMaxRecallPerRun caps how many recalls one run may launch
	// (the compute budget of devdoc/hippocampus.md §7).
	hippocampusEnvMaxRecallPerRun = "GOPI_HIPPOCAMPUS_MAX_RECALL_PER_RUN"
	// hippocampusEnvTimeoutSec bounds ONE recall, LLM call included.
	hippocampusEnvTimeoutSec = "GOPI_HIPPOCAMPUS_TIMEOUT_SEC"
	// hippocampusEnvLv3MinIntervalSec is the minimum spacing between two Lv-3
	// soft interrupts (the quota window, devdoc/hippocampus.md §5).
	hippocampusEnvLv3MinIntervalSec = "GOPI_HIPPOCAMPUS_LV3_MIN_INTERVAL_SEC"
	// hippocampusEnvLv3MaxPerSession caps Lv-3 activations per session.
	hippocampusEnvLv3MaxPerSession = "GOPI_HIPPOCAMPUS_LV3_MAX_PER_SESSION"
)

// Defaults. Every number is named (Rule 1) — the env overrides below are the
// only other source, and they win only when positive.
const (
	// hippocampusDefaultMaxLv is the shipped default delivery ceiling: stage 1
	// of the epic is Lv-0/Lv-1 only (devdoc/hippocampus.md §5), so a bare
	// GOPI_HIPPOCAMPUS=1 enables archiving + post-round memos, nothing more.
	hippocampusDefaultMaxLv = hippocampusLv1Append
	// hippocampusDefaultMaxRecallPerRun bounds the divergent retrieval.
	// It also caps the DELIVERIES: one recall per round boundary, each
	// collected at the next boundary — i.e. one measurable activation per
	// round at most (the child-4/4 adoption run counts it as
	// activations/round).
	hippocampusDefaultMaxRecallPerRun = 8
	// hippocampusMinInjectIntervalRounds is the minimum spacing between two
	// deliveries of ANY level (the noise rule): a briefing loop that re-sends
	// the same leads every round costs context and attention while telling the
	// model nothing new. 3 rounds ≈ "at most once per few steps"; the ceiling
	// is deliberately not a timer (rounds are the unit the model works in).
	hippocampusMinInjectIntervalRounds = 3
	// hippocampusDefaultRecallTimeout is the wall-clock ceiling of one recall.
	hippocampusDefaultRecallTimeout = 30 * time.Second
	// hippocampalLv3QuotaWindow is the Lv-3 spacing floor (原文: 1 次/15 分钟).
	hippocampalLv3QuotaWindow = 15 * time.Minute
	// hippocampalLv3MaxPerSession caps Lv-3 activations per session
	// (原文示例: 1 次).
	hippocampalLv3MaxPerSession = 1
	// hippocampusAppendMinConfidence is the floor below which a brief is NOT
	// worth the model's attention — it degrades to Lv-0 (archive only).
	hippocampusAppendMinConfidence = 0.2
	// hippocampusMetaEventMinConfidence is the floor for the "re-evaluate your
	// plan" framing (Lv-2); below it the brief stays a passive memo (Lv-1).
	hippocampusMetaEventMinConfidence = 0.4
	// hippocampusLv3MinConfidence is the floor for a soft-interrupt REQUEST.
	// The scheduler may still refuse (quota) and degrade to Lv-1.
	hippocampusLv3MinConfidence = 0.6
	// hippocampusEndOfRunWait bounds how long the end-of-run hook waits for an
	// in-flight recall before archiving what it has. Bounded so a hung organ
	// can never hold the run open; the cycle's own ctx is cancelled right
	// after (the run ctx derives it), so the goroutine still exits promptly.
	hippocampusEndOfRunWait = 2 * time.Second
	// hippocampusDeliveryBacklogMax caps the per-run delivery queue: exceeding
	// it drops the OLDEST undelivered brief (a burst of recalls can never
	// spawn unbounded injections).
	hippocampusDeliveryBacklogMax = 8
	// hippocampusQueryMaxChars truncates the task context handed to the organ
	// as the retrieval seed.
	hippocampusQueryMaxChars = 2000
)

// hippocampusConfig is the resolved knob set of one host. Zero value is NOT
// usable — build it with loadHippocampusConfig.
type hippocampusConfig struct {
	Enabled          bool
	MaxLv            hippocampusLevel
	MaxRecallPerRun  int
	RecallTimeout    time.Duration
	Lv3MinInterval   time.Duration
	Lv3MaxPerSession int
}

// loadHippocampusConfig resolves the GOPI_HIPPOCAMPUS_* knobs. THE ONLY env
// reader of this namespace (Rule 2): unset/empty/unparseable keeps the
// constant default, a bare/truthy GOPI_HIPPOCAMPUS enables the host.
func loadHippocampusConfig() hippocampusConfig {
	cfg := hippocampusConfig{
		Enabled:          boolEnvTrue(hippocampusEnvEnabled),
		MaxLv:            hippocampusDefaultMaxLv,
		MaxRecallPerRun:  hippocampusDefaultMaxRecallPerRun,
		RecallTimeout:    hippocampusDefaultRecallTimeout,
		Lv3MinInterval:   hippocampalLv3QuotaWindow,
		Lv3MaxPerSession: hippocampalLv3MaxPerSession,
	}
	if n, ok := hippocampusEnvInt(hippocampusEnvMaxLv); ok && n >= 0 {
		cfg.MaxLv = clampHippocampusLevel(hippocampusLevel(n))
	}
	if n, ok := hippocampusEnvInt(hippocampusEnvMaxRecallPerRun); ok && n > 0 {
		cfg.MaxRecallPerRun = n
	}
	if n, ok := hippocampusEnvInt(hippocampusEnvTimeoutSec); ok && n > 0 {
		cfg.RecallTimeout = time.Duration(n) * time.Second
	}
	if n, ok := hippocampusEnvInt(hippocampusEnvLv3MinIntervalSec); ok && n >= 0 {
		cfg.Lv3MinInterval = time.Duration(n) * time.Second
	}
	if n, ok := hippocampusEnvInt(hippocampusEnvLv3MaxPerSession); ok && n >= 0 {
		cfg.Lv3MaxPerSession = n
	}
	return cfg
}

// hippocampusEnvInt parses an integer env var; ok=false when unset, empty or
// unparseable (the caller keeps its default).
func hippocampusEnvInt(name string) (int, bool) {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}
