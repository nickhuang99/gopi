package configreport

// Settings keys that are consumed by a production reader in a package OTHER
// than internal/config — and therefore are NOT struct fields of config.Config.
//
// WHY THIS REGISTRY EXISTS (epic 01a1eaec, 2026-11-18, bug 01a1eaec). The CLI
// reports a settings key as "unrecognized — so it is IGNORED" when the key is
// absent from config.ModelledSettingsKeys(), which reflects config.Config's
// json tags. `messages_path` is a field of internal/deepseek.Config and is read
// on the live request path (claude_client.go: claudeMessagesURL appends it
// verbatim), but deepseek imports config, so config cannot import deepseek to
// discover the key. The result was a LIVE knob reported as IGNORED — a report
// that says "this has no effect" about something that has an effect.
//
// This leaf registry is the one place such keys are declared. config folds it
// into ModelledSettingsKeys(), so the CLI's "is this key modelled?" question is
// answered from ONE list that now covers every production reader. A new settings
// key consumed by another package is added HERE and nowhere else (Rule 14).
//
// Each row carries the reason (the reader) so a permit cannot outlive its fix.
type ExternalConsumerKey struct {
	Key    string
	Reader string
}

// ExternalConsumerSettingsKeys is the complete list of settings keys with a
// live production reader outside internal/config's own Config struct.
var ExternalConsumerSettingsKeys = []ExternalConsumerKey{
	{Key: SettingsKeyMessagesPath, Reader: "internal/deepseek.Config.MessagesPath (claudeMessagesURL appends it to the endpoint)"},
}

// SettingsKeyMessagesPath is the settings key that overrides the full Claude
// Messages API request path. Single definition, referenced by the registry and
// by the tests (Rule 1b).
const SettingsKeyMessagesPath = "messages_path"

// IsExternalConsumerKey answers whether key is a settings key this build reads
// from a package outside internal/config.
func IsExternalConsumerKey(key string) bool {
	for _, row := range ExternalConsumerSettingsKeys {
		if row.Key == key {
			return true
		}
	}
	return false
}
