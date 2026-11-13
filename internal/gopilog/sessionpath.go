package gopilog

import (
	"fmt"
	"os"
)

// FindSessionPathForID resolves a session ID (or an absolute path to a session
// file) to the path of its JSONL log — the one resolution a second surface must
// not re-derive (Rule 14). It is FindSession plus the "not found" case turned
// into an error that names the input.
func FindSessionPathForID(sessionID string) (string, error) {
	if sessionID == "" {
		return "", fmt.Errorf("which session? pass a <session-id> or a path to a session log")
	}
	// An explicit path is taken as given (the same rule the log/raw surfaces
	// apply): a caller holding the file needs no directory scan.
	if _, err := os.Stat(sessionID); err == nil {
		return sessionID, nil
	}
	dir, err := ResolveSessionDir()
	if err != nil {
		return "", err
	}
	path, err := FindSession(dir, sessionID)
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}
	return path, nil
}
