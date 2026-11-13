package main

// cli_config.go — the `gopi config` verb (epic: config-show).
//
// WHY THIS EXISTS (the defect it closes): `gopi config show` was documented in
// the merger's orientation prompt (`cmd/agent/merge_prompt.md` §2, "read the
// contract from the SAME table the merger reads") but the two words dispatched
// NOWHERE: cmd/agent/main.go's subcommand chain had no isConfigSubcommand, and
// config.commandIndex carried no `gopi config` row. An unknown first token
// falls through to the bare-prompt one-shot path, so `gopi config show` did not
// error — it launched a WHOLE AGENT SESSION, registered an `[auto] config show`
// ledger row, and ran the LLM until the caller killed it (35 such rows were
// observed in the ledger; the operator's report was "gopi config show HANGS",
// which is the shape of a 25s-timeout SIGTERM on a full agent run).
//
// THE COMMAND IS A READER, NOT AN AGENT. `gopi config show` answers, in one
// fast process, the question the prompt actually asks — "what IS this host's
// configuration, and where did each value come from" — and it never reaches the
// agent path, never registers a ledger row, and never prints a secret.
//
// SCOPE (deliberate): show WHAT IS IN FORCE and WHICH CHANNEL decided it, plus
// the per-repo build/swp table the merger reads. It is not a settings editor
// (that is `gopi memory`-style curation territory and a different epic) and it
// is not the env catalog (`gopi --help-env` renders that from
// config.EnvVars). One question, one answer.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/scratch"
)

// configSubcommandNames is the `gopi config <TAB>` candidate set — the group's
// sub-verbs. Declared here (the family file) per cli_verbs.go's namespace note:
// a GROUP's sub-verbs live in their family file, not in the top-level cliVerbs
// block. Referenced by cli_completion.go so `gopi config <TAB>` offers them.
var configSubcommandNames = []string{configShowVerb, configEnvVerb, CliVerbSetup, CliVerbCompletion}

// configShowVerb is the `gopi config show` sub-verb.
const configShowVerb = "show"

// configUsage is the `gopi config -h` text — every command answers its own -h.
const configUsage = "gopi config — inspect the configuration actually in force.\n" +
	`
Usage:
  gopi config show [--settings PATH]
      Print the effective configuration and, for each value, the CHANNEL that
      decided it (flag / env var / settings.json / built-in default).
  gopi config env [--write] [--path P]
      Measure THIS host and render its environment description: hostname, OS,
      interface addresses, default gateway, and the ssh alias table as written.
  gopi config setup [flags]
      Provision a FRESH host to a run-ready state (idempotent).
  gopi config completion <shell>
      Emit a shell tab-completion script. <shell> is one of bash, zsh or fish
      — a bare word the command word accepts, never a bash|zsh|fish
      shorthand (that spelling is a syntax summary, not an invocation: the
      shell is a single POSITIONAL, so exactly one of the three is typed).
  gopi config -h
      This text.

Every sub-verb answers its OWN page: gopi config <sub> -h prints that verb's
flags, exit codes and examples — what a show section holds and how a secret is
rendered (gopi config show -h), which paths setup creates and what it refuses
(gopi config setup -h).

config show INSPECTS and config setup PROVISIONS from the same dimension: read
the effective configuration first, then run setup once to establish what is
missing. The full env-var catalog (every knob with its env var, settings key
and flag) is gopi --help-env.

Why the page is this shape, and the background it moved off the page:
devdoc/help-page-layering-small-pages.md.
`

// isConfigSubcommand reports whether argv begins with the config subcommand.
// Mirrors isCheckpointSubcommand's contract: when it returns true the
// subcommand has consumed the argv and the caller must not treat the
// remainder as a prompt or an input file.
func isConfigSubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] != CliVerbConfig {
		return false
	}
	runConfigSubcommand(args[1:])
	return true
}

// runConfigSubcommand dispatches the config group's sub-verbs.
func runConfigSubcommand(args []string) {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, configUsage)
		os.Exit(1)
	}
	switch args[0] {
	case "-h", "--help", HelpWord:
		fmt.Print(configUsage)
		return
	case configShowVerb:
		settingsPath, err := parseConfigShowFlags(args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "gopi config show: %v\n\n%s", err, configUsage)
			os.Exit(1)
		}
		runConfigShow(settingsPath)
	case configEnvVerb:
		runConfigEnv(args[1:])
	case CliVerbSetup:
		// `setup` provisions a host's GLOBAL PERSISTENT state and `completion`
		// writes a global persistent shell artifact — both belong to the
		// config dimension (epic verb-restructure, 2026-10-05). ONE
		// implementation, ONE runnable spelling: the same run functions the
		// top-level entries call, so their -h pages, flag handling and exit
		// codes are theirs (Rules 13/14). The flat words `gopi setup` and
		// `gopi completion` are REFUSED by name at argv[0] (measured
		// 2026-11-06: both exit 1, "is RETIRED — use `gopi config …`"). `setup`
		// returns without exiting only for its own -h; a genuine run exits
		// inside runSetupSubcommand.
		runSetupSubcommand(args[1:])
	case CliVerbCompletion:
		runCompletionSubcommand(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "gopi config: unknown subcommand %q (want: %s)\n\n%s",
			args[0], strings.Join(configSubcommandNames, ", "), configUsage)
		os.Exit(1)
	}
}

// parseConfigShowFlags extracts `gopi config show`'s one flag (--settings).
// An unknown flag is an ERROR, never ignored: a silently dropped flag would
// report a configuration the caller did not ask about.
func parseConfigShowFlags(args []string) (string, error) {
	settingsPath := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-h", "--help":
			fmt.Print(configUsage)
			os.Exit(0)
		case "--settings":
			if i+1 >= len(args) {
				return "", fmt.Errorf("--settings requires a path")
			}
			i++
			settingsPath = args[i]
		default:
			return "", fmt.Errorf("unexpected argument %q", args[i])
		}
	}
	return settingsPath, nil
}

// The credential renderer's two constants live in internal/config
// (CredentialUnsetLabel / CredentialSetShapeFormat / RedactedCredentialShape):
// ONE definition per concept, referenced from here rather than mirrored (Rules
// 13/14). `api_key` was the only credential when this verb was written, and
// `doubao_api_key` joined it when the voice credential became an inline
// settings value — which is precisely why the LIST moved to
// config.CredentialSettingsKeys and this file stopped carrying a copy: a second
// list is how a key silently stops being redacted.

// runConfigShow prints the effective configuration with provenance.
func runConfigShow(flagSettingsPath string) {
	// The settings path is resolved the SAME way the agent path resolves it
	// (flag > GOPI_SETTINGS > default): reported provenance that disagreed with
	// what the agent would actually load would be worse than no report.
	settingsPath := resolveConfigShowSettingsPath(flagSettingsPath)

	fmt.Println("gopi configuration in force")
	fmt.Println()

	fmt.Println("Identity:")
	fmt.Printf("  %-22s %s\n", "home", reportValue(configGopiHome()))
	fmt.Printf("  %-22s %s\n", "gopi dir", reportValue(configGopiDir()))
	fmt.Printf("  %-22s %s%s\n", "settings file", settingsPath, configFileNote(settingsPath))
	fmt.Printf("  %-22s %s\n", "memory db / ledger", reportValue(configMemoryDBPath()))
	fmt.Printf("  %-22s %s\n", "temp workspace", configTmpDirReport())
	fmt.Printf("  %-22s %s\n", "temp root headroom", configTmpDirHeadroomReport())
	// This host's environment description — the file the agent's prompt points
	// at. Its PRESENCE and location are configuration facts, so this verb is
	// where an operator looks after asking "why does the agent not know my
	// nodes?": an absent description is named with the one command that makes
	// one, never left as a silence (Rule 15).
	fmt.Printf("  %-22s %s\n", "environment desc", configEnvReportLine())
	showRunRepo()
	showWorkspaceRoot()
	fmt.Println()

	showSettingsFile(settingsPath)
	showSettingsKeyLoss(settingsPath)
	showRepoTable(settingsPath)
}

// showSettingsKeyLoss reports keys the settings document has LOST relative to
// its own sibling backup — the third-party writer's round trip drops every key
// its struct does not model, silently (measured 2026-09-27: compute_nodes and
// task_sync gone from the live document while .ds_flash_settings_backup.json
// still carries them, and tmpdir reverted twice inside ~2 minutes). gopi cannot
// stop another process from writing; it must not observe the loss in silence
// (Rule 15), and this verb — "what configuration is actually in force" — is the
// surface where a vanished key changes the answer. Nothing is printed when
// nothing was lost, or when there is no sibling backup to compare against: the
// report exists to break silence, not to add noise.
func showSettingsKeyLoss(settingsPath string) {
	if settingsPath == "" {
		return
	}
	loss, compared, err := config.DetectSettingsKeyLoss(settingsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] config: %v\n", err)
		return
	}
	if !compared || loss.Empty() {
		return
	}
	fmt.Printf("settings key loss (vs %s):\n", loss.Reference)
	width := 0
	for _, k := range loss.Lost {
		if len(k) > width {
			width = len(k)
		}
	}
	for _, k := range loss.Lost {
		fmt.Printf("  %-*s  LOST — the document no longer carries it\n", width, k)
	}
	fmt.Println()
}

// resolveConfigShowSettingsPath applies the standard precedence for the
// settings file itself: the --settings flag, then GOPI_SETTINGS, then the
// default path under the gopi dir.
func resolveConfigShowSettingsPath(flagPath string) string {
	if p := strings.TrimSpace(flagPath); p != "" {
		return p
	}
	return config.ResolveSettingsPath()
}

// configFileNote renders the existence marker for a settings path — an absent
// file is normal (defaults in force) and must read as such, not as an error.
func configFileNote(path string) string {
	if path == "" {
		return "  (cannot resolve — no home?)"
	}
	fi, err := os.Stat(path)
	if err != nil {
		return "  (absent — built-in defaults in force)"
	}
	if fi.IsDir() {
		return "  (a DIRECTORY, not a file)"
	}
	return fmt.Sprintf("  (%d bytes)", fi.Size())
}

// showSettingsFile prints the settings file's own keys and values, redacting
// credentials. A file that cannot be PARSED is a hard error (exit 1) — the
// same contract the agent path enforces, because a broken provider file must
// not be reported as "no configuration".
func showSettingsFile(path string) {
	fmt.Println("settings.json:")
	if path == "" {
		fmt.Println("  (no settings path resolvable)")
		fmt.Println()
		return
	}
	// The LOAD is the check: config.FromSettingsFile applies the same parse
	// contract the agent path applies, so a broken file is a hard exit here
	// too (an absent file is not an error — defaults are then in force).
	if _, err := config.FromSettingsFile(path); err != nil {
		// An ABSENT file is not a parse failure: FromSettingsFile documents
		// defaults-on-missing, so an error here is a genuinely broken file.
		fmt.Fprintf(os.Stderr, "[gopi] config: %v\n", err)
		fmt.Fprintf(os.Stderr, "[gopi] fix the settings file and re-run, or pass --settings PATH to use a different one\n")
		os.Exit(1)
	}
	rows, err := configSettingsRows(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[gopi] config: %s: %v\n", path, err)
		fmt.Fprintf(os.Stderr, "[gopi] fix the settings file and re-run, or pass --settings PATH to use a different one\n")
		os.Exit(1)
	}
	if len(rows) == 0 {
		fmt.Println("  (no keys set — built-in defaults in force)")
		fmt.Println()
		return
	}
	width := 0
	for _, r := range rows {
		if len(r[0]) > width {
			width = len(r[0])
		}
	}
	for _, r := range rows {
		key, value := r[0], r[1]
		if config.IsCredentialSettingsKey(key) || config.IsAudioCredentialParamName(key) {
			// The SHAPE is decided by internal/config (CredentialUnsetLabel /
			// CredentialSetShapeFormat), so the renderer and every test agree
			// about what "unset" and "set" look like — and the length is the
			// DECODED value's, not the JSON cell's (a cell carries its quotes).
			//
			// The value passed in is the raw JSON cell, which is exactly what
			// the shape helper decodes; the ordinary keys keep rendering their
			// compact JSON text.
			value = config.RedactedCredentialShape(json.RawMessage(r[1]))
		}
		note := ""
		if configSettingsKeyIsUnmodelled(key) {
			note = "  " + configUnmodelledKeyNote
		}
		fmt.Printf("  %-*s  %s%s\n", width, key, value, note)
		// THE RETIRED KNOB IS EXPLAINED HERE AND NOWHERE ELSE (2026-11-06).
		// This used to be a global startup hook in front of every verb, so
		// `gopi task list` printed two long lines about a settings key before a
		// single row. A stale key is a fact about the CONFIGURATION, so the
		// configuration verb is where it is reported — and no other command
		// says a word (see internal/config/retired_settings_keys.go).
		if retired, isRetired := config.RetiredSettingsKeyInfo(key); isRetired {
			fmt.Print(configRetiredKeyBlock(retired, configRetiredIndentFor(width)))
		}
	}
	fmt.Print(retiredEnvFaceBlock())
	fmt.Println()
}

// The explanation block's shape, named so the renderer, its test and the
// alignment helper cannot drift (Rule 1b).
const (
	// configRetiredIndent is the FALLBACK indent for an explanation whose row it
	// cannot be aligned under (the env-face block below the table).
	configRetiredIndent = "    "
	// configRetiredMarker opens every explanation line. It is a word an operator
	// greps for, so it is a constant rather than a literal at four call sites.
	configRetiredMarker = "RETIRED"
	// configRetiredPurposeLabel / configRetiredHistoryLabel /
	// configRetiredInsteadLabel name the three halves the startup hook never
	// carried: what the knob DID, when and with what it went, what replaces it.
	configRetiredPurposeLabel = "what it did"
	configRetiredHistoryLabel = "why and when"
	configRetiredInsteadLabel = "instead"
	// configUnmodelledKeyNote marks a key this build does not model — a typo, or
	// a key from a different build. It is stated rather than left silent because
	// the decoder IGNORES such a key (Rule 15), which is exactly why an operator
	// reading "the configuration in force" must be told the line does nothing.
	configUnmodelledKeyNote = "(unrecognized key — no version of this build models it, so it is IGNORED)"
	// retiredEnvFaceHeader opens the section that reports a retired knob exported
	// into THIS process's environment. It exists because a retired knob's env
	// face is not in settings.json at all, and the settings table is the only
	// thing this verb prints row by row.
	retiredEnvFaceHeader = "retired knobs set in this environment:"
)

// configRetiredIndentFor returns the indent that aligns an explanation's text
// with the VALUE column of the settings table's own rows (`  %-*s  %s`), so the
// block reads as part of the row above it rather than as a separate table.
func configRetiredIndentFor(keyWidth int) string {
	return strings.Repeat(" ", len("  ")+keyWidth+len("  "))
}

// configRetiredKeyBlock renders ONE retired settings key's explanation as the
// several indented lines printed UNDER its row: the marker, what the knob did,
// why and when it went, and what to do instead. It is a pure function of the
// registry row so a test can assert the text without capturing stdout.
func configRetiredKeyBlock(retired config.RetiredSettingsKey, indent string) string {
	var b strings.Builder
	line := func(label, text string) {
		b.WriteString(indent)
		b.WriteString(configRetiredMarker)
		b.WriteString(" — ")
		b.WriteString(label)
		b.WriteString(": ")
		b.WriteString(text)
		b.WriteString("\n")
	}
	// THE HEADER NAMES THE KNOB BY BOTH FACES. Without it the explanation under a
	// row would be a paragraph about a switch the reader has to infer from
	// position — and a grep for the key (the operator's own move) would stop at
	// the table row instead of following the prose.
	b.WriteString(indent)
	b.WriteString(configRetiredMarker)
	b.WriteString(" ")
	b.WriteString(configRetiredKeyName(retired))
	b.WriteString("\n")
	line(configRetiredPurposeLabel, retired.Purpose)
	line(configRetiredHistoryLabel, retired.Retired)
	line(configRetiredInsteadLabel, retired.Replacement)
	return b.String()
}

// configRetiredKeyName renders the knob's two FACES as one display name: the
// settings key when it had one, plus the env spelling when it had one, so a
// reader who knows either spelling finds their answer. An env-only knob prints
// its variable alone (no settings spelling is invented at retirement time).
func configRetiredKeyName(retired config.RetiredSettingsKey) string {
	switch {
	case retired.SettingsKey != "" && retired.EnvName != "":
		return retired.SettingsKey + " (env " + retired.EnvName + ")"
	case retired.SettingsKey != "":
		return retired.SettingsKey
	default:
		return retired.EnvName
	}
}

// FILE is one this build does not model at all.
//
// The flattened audio block's LEAF rows (`audio_engine_param.streaming.token`)
// are skipped by name: their parent key is what the struct declares, and
// reporting every leaf as a typo would bury the real answer in noise.
func configSettingsKeyIsUnmodelled(key string) bool {
	if strings.Contains(key, ".") {
		return false
	}
	if _, isRetired := config.RetiredSettingsKeyInfo(key); isRetired {
		// A retired key IS modelled in the only sense that matters here: the
		// registry above names it, so it is not a typo and it gets its own,
		// longer explanation instead of this one-liner.
		return false
	}
	for _, known := range config.ModelledSettingsKeys() {
		if known == key {
			return false
		}
	}
	return true
}

// retiredEnvFaceBlock renders the retired knobs exported into this process's
// environment — the face settings.json cannot show. An EMPTY return is the
// ordinary case and is deliberate: this verb reports what is in force, and an
// unset variable is not a fact about the configuration.
//
// It reads the environment through os.Getenv exactly once per registry row: the
// registry is the ONE list of retired env names (Rule 14), so a new retirement
// adds a row and this section picks it up without an edit here.
func retiredEnvFaceBlock() string {
	var b strings.Builder
	for _, retired := range config.RetiredKnobsWithEnvFace() {
		if strings.TrimSpace(os.Getenv(retired.EnvName)) == "" {
			continue
		}
		if b.Len() == 0 {
			b.WriteString(retiredEnvFaceHeader)
			b.WriteString("\n")
		}
		b.WriteString("  ")
		b.WriteString(retired.EnvName)
		b.WriteString("\n")
		b.WriteString(configRetiredKeyBlock(retired, configRetiredIndent))
	}
	if b.Len() == 0 {
		return ""
	}
	b.WriteString("\n")
	return b.String()
}

// configSettingsRows reads the settings file as an ordered key→value list, for
// reporting only. It decodes into a map[string]json.RawMessage so a value of
// ANY JSON shape round-trips as its literal text (an object — `repos`, `notify`
// — is shown as its raw JSON rather than as Go's map formatting), and the keys
// are then sorted for a stable report.
//
// This deliberately does NOT reuse config.Config's typed struct: the question
// is "what does the FILE say", and a struct decode would silently DROP a key
// the struct does not carry — exactly the kind of configuration the operator
// needs to see (a typo'd key, a key from a newer build).
func configSettingsRows(path string) ([][2]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // absent = defaults in force, not an error
		}
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(top))
	for k := range top {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	rows := make([][2]string, 0, len(keys))
	for _, k := range keys {
		if k == config.AudioEngineParamSettingsKey {
			// THE NESTED BLOCK IS RENDERED LEAF BY LEAF, never as one compact
			// JSON cell. Two reasons, and the second is a security one: a
			// nested credential would otherwise print as
			// {"api_key":"<the live secret>"} inside a single value cell — the
			// redaction below keys on the KEY NAME, and a whole-block cell has
			// no key name to key on (measured shape, not a hypothesis: this is
			// exactly how the flat surface's `doubao_api_key` would have leaked
			// if the renderer had been left to `compactJSON`).
			rows = append(rows, configAudioParamRows(top[k])...)
			continue
		}
		rows = append(rows, [2]string{k, compactJSON(top[k])})
	}
	return rows, nil
}

// configAudioParamRows flattens the nested engine-parameter block into one row
// per LEAF, each row's key being the full dotted PATH
// (`audio_engine_param.streaming.token`).
//
// The path is what makes redaction possible at this level: the renderer asks
// internal/config whether the LEAF name is a credential
// (config.IsAudioCredentialParamName), so a credential inside a block is
// redacted by the same rule that redacts a top-level one, and an operator still
// sees that the knob EXISTS and how long its value is.
//
// A block that cannot be parsed is reported as its own row rather than dropped —
// a value gopi cannot read is a fact about the document, and a renderer whose
// job is "what configuration is in force" must not answer that with silence
// (Rule 15).
func configAudioParamRows(blob json.RawMessage) [][2]string {
	var blocks map[string]json.RawMessage
	if err := json.Unmarshal(blob, &blocks); err != nil {
		return [][2]string{{config.AudioEngineParamSettingsKey, compactJSON(blob)}}
	}
	names := make([]string, 0, len(blocks))
	for name := range blocks {
		names = append(names, name)
	}
	sort.Strings(names)
	var rows [][2]string
	for _, name := range names {
		var knobs map[string]json.RawMessage
		if err := json.Unmarshal(blocks[name], &knobs); err != nil {
			rows = append(rows, [2]string{config.AudioEngineParamSettingsKey + "." + name, compactJSON(blocks[name])})
			continue
		}
		knobNames := make([]string, 0, len(knobs))
		for k := range knobs {
			knobNames = append(knobNames, k)
		}
		sort.Strings(knobNames)
		for _, k := range knobNames {
			rows = append(rows, [2]string{config.AudioEngineParamSettingsKey + "." + name + "." + k, compactJSON(knobs[k])})
		}
	}
	return rows
}

// compactJSON renders a raw JSON value on one line (the report is a table, not
// a document), falling back to the literal bytes when it cannot be re-encoded.
func compactJSON(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}

// showRepoTable prints the per-repo table — the contract `gopi task merge`
// reads from this same table (config.ResolveMergerRepos, its ONE reader).
// Declared, never discovered: an absent table or entry is reported as such,
// never guessed. The header named `gopi merge` until 2026-11-01, when that
// top-level verb was removed (epic merge-verb-removal); a printed pointer at a
// verb that exits 1 is a defect, so it names the surviving spelling.
func showRepoTable(settingsPath string) {
	fmt.Println("repos (per-repo merge/build contract — the table `gopi task merge` sweeps and rebuilds from):")
	if settingsPath == "" {
		fmt.Println("  (no settings path resolvable)")
		fmt.Println()
		return
	}
	cfg, err := config.FromSettingsFile(settingsPath)
	if err != nil {
		// Already reported by showSettingsFile; keep the section honest
		// without exiting twice.
		fmt.Println("  (settings file unreadable — see the error above)")
		fmt.Println()
		return
	}
	if len(cfg.Repos) == 0 {
		fmt.Println("  (no entries — no repo is a first-class task field for this host)")
		fmt.Println()
		return
	}
	paths := make([]string, 0, len(cfg.Repos))
	for p := range cfg.Repos {
		if strings.TrimSpace(p) != "" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		rc := cfg.Repos[p]
		build := strings.TrimSpace(rc.Build)
		if build == "" {
			fmt.Printf("  %s\n", p)
			fmt.Println("      build: (none declared — pure merge target: sweep and land, never rebuild)")
		} else {
			fmt.Printf("  %s\n", p)
			fmt.Printf("      build: %s\n", build)
			// ResolveRepoBuild applies the repo-entry > env > default timeout
			// precedence, so this line reports the EFFECTIVE ceiling, not the
			// raw declared one.
			if effective, found, rerr := config.ResolveRepoBuild(p); rerr == nil && found {
				if effective.BuildTimeoutSec < 0 {
					fmt.Println("      build_timeout_sec: (none — no ceiling)")
				} else {
					fmt.Printf("      build_timeout_sec: %d\n", effective.BuildTimeoutSec)
				}
			}
		}
		root, rerr := config.ResolveRepoWorktreeRoot(p)
		if rerr == nil {
			fmt.Printf("      worktree_root: %s (%s)\n", root.Path, root.Source)
		} else {
			fmt.Printf("      worktree_root: (unresolved: %v)\n", rerr)
		}
	}
	fmt.Println()
}

// configGopiHome resolves the HOME the gopi data paths hang from.
func configGopiHome() string {
	h, err := config.GopiHome()
	if err != nil {
		return "(" + err.Error() + ")"
	}
	return h
}

// configGopiDir resolves the gopi data root ($GOPI_HOME/.gopi or ~/.gopi).
func configGopiDir() string {
	d, err := config.GopiDir()
	if err != nil {
		return "(" + err.Error() + ")"
	}
	return d
}

// configMemoryDBPath reports the ledger/memory database path and the CHANNEL
// that decided it — rendered from the ONE ranked resolution the loader itself
// uses (config.ResolveLedger), never re-derived here.
//
// Rank 1 of that chain (`--db`) is deliberately NOT read here: a ledger flag is
// carried by the verb that owns it (`gopi memory search --db`, `gopi task … --db`,
// the run path), and a LEADING `--db` before a verb is refused by the
// positional-verb guard — so a report that printed a flag value would be naming a
// channel this very launch never reached. The channels that DO reach a
// subcommand — env, settings, default — are all resolved.
//
// WHY IT DELEGATES (Rule 2/14, and the defect this closes): this function used
// to walk only the env half of the chain and then name the built-in default, so
// `gopi config show` printed "…/memory.db  (built-in default)" directly above a
// settings.json section that showed "memory_db" set to a different path — one
// report, two ledgers, and the wrong one named as in force (Rule 15).
func configMemoryDBPath() string {
	path, source := config.ResolveLedger(config.LedgerNoFlag)
	if path == "" {
		// No channel and no resolvable home: reported as a FAULT with its
		// reason, never as a silent fallback path (Rule 15 — the same contract
		// the temp-workspace report keeps).
		return "(unresolved — no home directory and no ledger channel set)"
	}
	return path + "  (from " + source + ")"
}

// configTmpDirReport renders the temp workspace plus the channel that decided
// it — the knob `gopi config show` is the read side of (Rule 22: the value is
// readable on demand rather than announced on every run).
//
// IT ASKS, IT DOES NOT RE-RANK (the defect this closes). The temp workspace is
// resolved ONCE per process — before the subcommand chain, by
// tmpDirBootstrapFromArgs → config.ResolveTmpDirConfigured — and that call's
// (value, source) pair IS the provenance of what is in force; every child
// inherits exactly that value. Reading the channels again here could only
// produce a SECOND opinion, and it produced a false one: the resolver's ambient
// arm tests TMPDIR before it tests GOPI_TMPDIR, and the publisher has by then
// written the resolved directory into gopi's OWN TMPDIR, so a re-read matched
// gopi's own publication and answered TmpDirSourceAmbient ("from the caller's
// TMPDIR") for every channel — `--tmpdir X config show` included, which the
// nil flag Config made unfixable by re-ranking (the flag arm could never fire).
//
// The pair is read back from the environment (TMPDIR + the source label), both
// written by the ONE publisher; scratch.ResolvePublishedTmpDir turns them into
// the canonical, proven-usable directory and the label to print, falling back
// to the built-in default when nothing was published — the SAME function that
// produced the published value, so a reader cannot disagree with the publisher.
// config.ResolveTmpDirConfigured therefore has exactly ONE caller (the
// publisher) and this verb cannot invent a competing precedence.
func configTmpDirReport() string {
	dir, source, err := scratch.ResolvePublishedTmpDir(
		strings.TrimSpace(os.Getenv(scratch.TmpDirEnvName)),
		strings.TrimSpace(os.Getenv(scratch.TmpDirSourceEnvName)),
	)
	if err != nil {
		// Rule 22: a temp root that cannot be provided is reported as a FAULT
		// with its reason and the channel that supplied it — never as a silent
		// fallback path. (The publisher has already refused the start of any
		// verb that needs a workspace; this verb only reports it.)
		return "(unavailable: " + err.Error() + ")"
	}
	return dir + "  (" + source + ")"
}

// configTmpDirHeadroomReport renders the free-space verdict of the temp
// workspace — the second half of the temp-root question, and the one `gopi
// config show` used to answer by OMISSION.
//
// WHY IT EXISTS: the row above says WHERE temp work lands; it says nothing about
// whether that filesystem has room for a build. On this host both the temp
// workspace and GOCACHE sit on one 3.6T partition, so a 2.3 GiB-free root turns
// every `go build` into `no space left on device` attributed to an unrelated
// package. The reader asking "why did my build fail" opens this report, so the
// verdict belongs here, printed with the same measurement the stderr warning
// uses (scratch.MeasureTmpDirHeadroom — one criterion, two readers, Rule 2/14).
func configTmpDirHeadroomReport() string {
	dir := strings.TrimSpace(os.Getenv(scratch.TmpDirEnvName))
	if dir == "" {
		// Nothing was published (an env-only invocation of this verb): ask the
		// same resolver for the directory, so this row and the one above can
		// never be about different paths.
		published, _, err := scratch.ResolvePublishedTmpDir("", "")
		if err != nil {
			return "(unavailable: " + err.Error() + ")"
		}
		dir = published
	}
	return scratch.MeasureTmpDirHeadroom(dir, strings.TrimSpace(os.Getenv(scratch.TmpDirSourceEnvName))).Report()
}

// reportValue renders a resolved value, marking an unresolved one visibly
// rather than printing an empty field.
func reportValue(v string) string {
	if strings.TrimSpace(v) == "" {
		return "(unresolved)"
	}
	return v
}
