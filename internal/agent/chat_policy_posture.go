package agent

// chat_policy_posture.go — the STARTUP POSTURE's resolution: the ONE place the
// chat surface decides what it does when the rule table has NO opinion.
//
// WHY IT LIVES IN internal/agent AND NOT IN internal/chatpolicy. The rule layer
// (internal/chatpolicy) is a PURE function of its arguments: fence text in, a
// Policy out; words in, a Match out. It reads no environment, no settings file
// and no global, which is what makes its tests deterministic and its behaviour
// impossible to widen at runtime. The CHANNELS — `--policy`, `GOPI_CHAT_POLICY`,
// settings.json "chat_policy", and the legacy chat_op_auto — are this face's
// business, so they are resolved HERE, ONCE, and frozen into a Posture VALUE.
//
// THE PRECEDENCE, highest first (Rule 2: one reader, so the startup banner
// cannot disagree with the switch):
//
//	1. `--policy VALUE`             this invocation's word
//	2. `GOPI_CHAT_POLICY`           the environment
//	3. settings.json "chat_policy"  the operator's file
//	4. the DEFAULT: on-request
//
// A value that is not a recognised spelling is REPORTED and the next channel
// decides — never silently read as the default (Rule 15: a knob whose off state
// is indistinguishable from its default has no off state).
//
// CHAT_OP_AUTO — THE COMPATIBILITY MAPPING, and its one printed sentence. The
// older knob answered a DIFFERENT question (may the console start a LONG
// operation without asking?); it is mapped, not reimplemented:
//
//	chat_op_auto = true   -> posture allow       (its "go ahead" reading)
//	chat_op_auto = false  -> posture on-request  (its "ask" reading — the
//	                                              default, so nothing is said)
//
// The mapping speaks only when chat_policy itself is SILENT and only when it
// CHANGES the answer, and it prints WHY — so the next operator retires the old
// key instead of wondering which of the two knobs won.
//
// NOTHING HERE IS WIRED INTO A JUDGEMENT: this function is called at startup to
// print what the run resolved; no call site consults the posture or the table
// yet (chat_write_guard.go is untouched).

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/cliverbs"
	"github.com/nickhuang99/gopi/internal/config"
)

const (
	// ChatPolicyFlag is the CLI spelling of the posture (`gopi chat --policy`).
	// It is an ALIAS of the leaf constant, not a second literal: internal/tools
	// names the same flag in the text a `prompt` decision returns to an
	// unattended run, and internal/tools cannot import this package (cycle), so
	// the one definition lives where both can reach it (Rule 14).
	ChatPolicyFlag = cliverbs.PolicyFlag

	// ChatPolicyEnv is the environment channel of the posture.
	ChatPolicyEnv = "GOPI_CHAT_POLICY"

	// ChatPolicySettingsKey is the settings.json key of the posture.
	ChatPolicySettingsKey = "chat_policy"

	// ChatOpAutoSettingsKey is the LEGACY settings key this resolver maps from
	// (config.ChatOpAutoSettingsKey — named here through config so the two
	// spellings cannot drift).
	chatOpAutoKey = config.ChatOpAutoSettingsKey

	// chatPolicyDefaultSource names the channel that decided when NOTHING
	// declared the posture: a label, never a path, so an operator reading
	// "default" knows the value is the shipped one.
	chatPolicyDefaultSource = "default"
)

// ChatPolicyPosture is this process's RESOLVED posture: the frozen value plus
// the one-time remarks the startup line prints.
type ChatPolicyPosture struct {
	// Posture is the frozen answer. chatpolicy.Posture is a VALUE type whose
	// methods return copies, so nothing inside the running process can widen it.
	Posture chatpolicy.Posture
	// Notes are the startup remarks in discovery order: a compatibility mapping,
	// a refused value, an unreadable settings file. They are printed once, by
	// the caller that resolved them.
	Notes []string
}

// ResolveChatPolicyPosture resolves the posture ONCE, at startup.
//
// flagValue is the `--policy` word ("" when the operator did not say it); getenv
// is INJECTED so the ladder is testable without mutating this process's
// environment (Rule 32); settingsPath is the file the settings channel reads
// ("" means config.ResolveSettingsPath()).
//
// The returned Posture is a value: there is no package-level variable holding
// it, so two resolutions cannot disagree and a later one cannot revise an
// earlier one silently.
func ResolveChatPolicyPosture(flagValue string, getenv func(string) string, settingsPath string) ChatPolicyPosture {
	if getenv == nil {
		getenv = os.Getenv
	}
	if settingsPath == "" {
		settingsPath = config.ResolveSettingsPath()
	}
	settingsLabel := "settings.json \"" + ChatPolicySettingsKey + "\""
	var notes []string

	// RUNG 1 — the command line.
	if v := strings.TrimSpace(flagValue); v != "" {
		if p, ok := chatpolicy.PostureNamed(strings.ToLower(v), ChatPolicyFlag); ok {
			return ChatPolicyPosture{Posture: p}
		}
		notes = append(notes, fmt.Sprintf("%s=%q is not a posture (%s) — the next channel decides",
			ChatPolicyFlag, v, strings.Join(chatpolicy.PostureWords(), "|")))
	}

	// RUNG 2 — the environment.
	if v := strings.TrimSpace(getenv(ChatPolicyEnv)); v != "" {
		if p, ok := chatpolicy.PostureNamed(strings.ToLower(v), ChatPolicyEnv); ok {
			return ChatPolicyPosture{Posture: p, Notes: notes}
		}
		notes = append(notes, fmt.Sprintf("%s=%q is not a posture (%s) — the next channel decides",
			ChatPolicyEnv, v, strings.Join(chatpolicy.PostureWords(), "|")))
	}

	// RUNG 3 — the settings file. readChatPolicySetting answers THREE questions
	// at once because the ladder needs all three: what it says, whether it says
	// it at all (an absent key lets the legacy mapping speak), and whether the
	// file could be read (unreadable is a fact the startup line reports).
	raw, declared, readable := readChatPolicySetting(settingsPath)
	if !readable {
		notes = append(notes, fmt.Sprintf("%s could not be read — the posture falls through to %s",
			settingsPath, chatPolicyDefaultSource))
	}
	if declared {
		if p, ok := chatpolicy.PostureNamed(strings.ToLower(strings.TrimSpace(raw)), settingsLabel); ok {
			return ChatPolicyPosture{Posture: p, Notes: notes}
		}
		notes = append(notes, fmt.Sprintf("%s = %q is not a posture (%s) — %s decides",
			settingsLabel, raw, strings.Join(chatpolicy.PostureWords(), "|"), chatPolicyDefaultSource))
	}

	// RUNG 3b — the LEGACY knob, mapped ONLY when the new key is silent and only
	// when the mapping CHANGES the answer (`true` is the only such value: `false`
	// already means on-request, which is rung 4).
	if !declared {
		if legacy, carried := legacyChatOpAuto(settingsPath); carried && legacy {
			mapped := chatpolicy.NewPosture(chatpolicy.DecisionAllow,
				"settings.json \""+chatOpAutoKey+"\" (mapped)")
			notes = append(notes, fmt.Sprintf("settings.json %q=true is mapped to the chat posture %q — prefer %q = %q, or %s=%s; %q answers a DIFFERENT question (may a LONG operation start) and is read only when the posture key is absent",
				chatOpAutoKey, chatpolicy.DecisionAllow.String(), ChatPolicySettingsKey, chatpolicy.DecisionAllow.String(),
				ChatPolicyEnv, chatpolicy.PostureYoloWord, chatOpAutoKey))
			return ChatPolicyPosture{Posture: mapped, Notes: notes}
		}
	}

	// RUNG 4 — the DEFAULT. Its SOURCE says "default", so a run that declared
	// nothing logs a different sentence than a run whose file said on-request.
	return ChatPolicyPosture{
		Posture: chatpolicy.NewPosture(chatpolicy.DecisionPrompt, chatPolicyDefaultSource),
		Notes:   notes,
	}
}

// readChatPolicySetting reports the settings file's chat_policy value, whether
// the file DECLARES it, and whether the file was readable.
//
// DECLARED is the fact the ladder branches on: an absent key and
// `"chat_policy": ""` are two situations, and only the first lets the legacy
// mapping speak. The file is decoded into a small map rather than into
// config.Config because the knob is THIS face's and Config does not model it —
// one read, one key, no new field on a struct other packages own.
func readChatPolicySetting(path string) (value string, declared bool, readable bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, true // no file: not declared, but not unreadable
		}
		return "", false, false
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", false, false
	}
	v, ok := raw[ChatPolicySettingsKey]
	if !ok {
		return "", false, true
	}
	if s, isStr := v.(string); isStr {
		return s, true, true
	}
	return "", true, true // declared with a non-string: reported as a bad value
}

// legacyChatOpAuto reads the legacy knob through config's OWN reader (Rule 2:
// one place decodes settings.json), reporting whether the file carries it.
func legacyChatOpAuto(path string) (on bool, carried bool) {
	cfg, err := config.FromSettingsFile(path)
	if err != nil || cfg == nil {
		return false, false
	}
	return cfg.ChatOpAuto, cfg.SettingsCarriesKey(chatOpAutoKey)
}

// LoadChatPolicyTable loads the rule table out of the CLIENT's own rule file —
// the one the run already sends as project context, resolved through the same
// walk with the LOCAL name winning a conflict (ResolveLocalProjectContextFile):
// the fences are this machine's operations policy, so a checkout that carries
// both PROJECT.md and GOPI.md must have its chat posture read from GOPI.md, not
// from the tracked engineering contract that has no fence in it.
//
// A file with no ```chat-policy fence is a VALID EMPTY table: absence of rules
// is a result, and the posture then answers every command. A file that fails to
// PARSE returns the error and the path, so the startup line can name the line
// that is wrong instead of silently judging by a half-read table (Rule 15).
// LoadChatOpCategoryTable loads the ACTION-CLASS table out of the SAME client
// rule file — one resolver (ResolveLocalProjectContextFile), one file, two
// fences (Rule 2). It is the category twin of LoadChatPolicyTable and is deliberately a SEPARATE reader: the
// two tables are different types parsed from different fences, so neither can be
// mistaken for the other, and a project that declares only one of them still gets
// the built-in default for the other.
//
// A file with no ```chat-op-category fence is a VALID EMPTY table (the caller then
// freezes the shipped defaults); a file that fails to PARSE returns the error and
// the path so the startup line can name the wrong line instead of judging by a
// half-read table (Rule 15).
func LoadChatOpCategoryTable(start string) (*chatpolicy.CategoryTable, string, error) {
	found := ResolveLocalProjectContextFile(start)
	if found.Path == "" {
		return nil, "", nil
	}
	data, err := os.ReadFile(found.Path)
	if err != nil {
		return nil, found.Path, fmt.Errorf("%s: %w", found.Path, err)
	}
	tab, perr := chatpolicy.ParseCategories(found.Path, string(data))
	if perr != nil {
		return nil, found.Path, perr
	}
	return tab, found.Path, nil
}

func LoadChatPolicyTable(start string) (*chatpolicy.Policy, string, error) {
	found := ResolveLocalProjectContextFile(start)
	if found.Path == "" {
		return nil, "", nil
	}
	data, err := os.ReadFile(found.Path)
	if err != nil {
		return nil, found.Path, fmt.Errorf("%s: %w", found.Path, err)
	}
	pol, perr := chatpolicy.Parse(found.Path, string(data))
	if perr != nil {
		return nil, found.Path, perr
	}
	return pol, found.Path, nil
}
