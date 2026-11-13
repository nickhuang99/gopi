package config

// memory_sync_on_store.go — the STORE-TRIGGERED asset-sync knob (epic:
// memory-sync-on-store), RETIRED 2026-10-27 (epic memory-store-sync-decouple).
//
// WHAT IT USED TO DECLARE: the direction of ONE asynchronous memory-asset sync
// that a SUCCESSFUL memory_store fired — push (this node's rows to the peer) or
// pull (the peer's rows here). "off" turned it off.
//
// WHAT IT DECLARES NOW: nothing. The store-triggered leg is DELETED, so
// `memory store` has exactly one success criterion — the row was written to the
// LOCAL `~/.gopi/memory.db` — and nothing it does reaches a peer. Cross-node
// propagation is the operator's own action, and since 2026-11-05 it is a
// ONE-WAY COPY rather than a sync: `gopi memory backup` ships this ledger to a
// declared memory_peers peer (peer side `gopi memory backup --serve`) and
// imports nothing from it, so the two knowledge bases stay divergent
// (config.LedgerCopyForwardReplacement is the one statement of this).
//
// WHY THE KEY SURVIVES INSTEAD OF BEING DELETED WITH THE LEG. An operator who
// already has `"memory_sync_on_store": "push"` in their settings.json is looking
// at a promise that is no longer kept, and a decoder that silently ignores an
// undeclared key would leave the setting looking live forever. So the key stays
// as RETIRED VOCABULARY: ResolveMemorySyncOnStoreDirection still answers (it is
// imported by tests and by the retired-knob report), the ANSWER is now always
// "off", and the two faces (env + settings) are NAMED back at an operator who
// still sets them — cmd/agent/memory_sync_on_store_retired.go, called from the
// same pre-chain moment the retired memory-sync tick uses.
//
// A MALFORMED VALUE IS STILL AN ERROR (Rule 15): a typo must not read as "the
// operator turned it off". It is reported into the run's session record and the
// store proceeds, because the leg it configured does not exist any more.

import (
	"fmt"
	"os"
	"strings"
)

// MemorySyncOnStoreSettingsKey is the settings.json key that used to carry the
// store-triggered sync's direction ("memory_sync_on_store": "push" | "pull" |
// "off"). RETIRED 2026-10-27 — kept as the name the retired-knob report quotes.
const MemorySyncOnStoreSettingsKey = "memory_sync_on_store"

// MemorySyncOnStoreEnv is the environment override for the same (retired)
// direction. Read in exactly one place — ResolveMemorySyncOnStoreDirection
// (Rule 2).
const MemorySyncOnStoreEnv = "GOPI_MEMORY_SYNC_ON_STORE"

// The direction VALUES, and they are the ONE definition of each (Rule 14): the
// settings key, the env channel and internal/tools' exported
// AssetSyncModePush/Pull aliases all speak these spellings, so a second literal
// cannot drift. The MANUAL verb's own --push/--pull flags resolve to them.
const (
	// MemorySyncDirectionPush is one-way OUTWARD: our unique rows travel to the
	// peer. It was the store-trigger's default until 2026-10-27; the manual verb
	// still takes it as an explicit --push.
	MemorySyncDirectionPush = "push"
	// MemorySyncDirectionPull is one-way INWARD: the peer's rows are applied
	// here and the peer is sent nothing (the leg carries an EMPTY patch).
	MemorySyncDirectionPull = "pull"
	// MemorySyncDirectionOff means "no session". Since 2026-10-27 it is the ONLY
	// answer the STORE-TRIGGER resolves to, whatever the operator declared: the
	// trigger it named no longer exists.
	MemorySyncDirectionOff = "off"
)

// ResolveMemorySyncOnStoreDirection answers the (retired) store-trigger
// direction. It is the ONLY reader of both channels (Rule 2) and the only place
// the default is applied.
//
// It returns MemorySyncDirectionOff for EVERY well-formed configuration, because
// the leg this knob configured is gone: absent, "push" and "pull" all mean "no
// session" now. A value that is not push/pull/off is still an ERROR — a typo must
// not be read as a deliberate "off".
func ResolveMemorySyncOnStoreDirection() (string, error) {
	raw := strings.TrimSpace(os.Getenv(MemorySyncOnStoreEnv))
	if raw == "" {
		cfg, err := FromSettingsFile(ResolveSettingsPath())
		if err != nil {
			return "", fmt.Errorf("config: %s: %w", MemorySyncOnStoreSettingsKey, err)
		}
		raw = strings.TrimSpace(cfg.MemorySyncOnStore)
	}
	switch strings.ToLower(raw) {
	case "", MemorySyncDirectionPush, MemorySyncDirectionPull, MemorySyncDirectionOff:
		// Every well-formed value now means the same thing: the store fires no
		// sync. Named individually rather than folded into a default branch so a
		// reader can see that "push" is ACCEPTED and IGNORED, not unrecognized.
		return MemorySyncDirectionOff, nil
	}
	return "", fmt.Errorf("%s=%q is not %s, %s or %s (all three are accepted; the store-triggered sync was RETIRED 2026-10-27, so every one of them means %s — cross-node propagation is MANUAL: `gopi memory sync --peer <alias>` for a converging session, `gopi memory backup` for a one-way ledger copy)",
		MemorySyncOnStoreEnv, raw,
		MemorySyncDirectionPush, MemorySyncDirectionPull, MemorySyncDirectionOff,
		MemorySyncDirectionOff)
}
