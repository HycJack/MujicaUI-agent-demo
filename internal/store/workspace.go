package store

// workspace.go persists the workspace choice: the current root and the
// recently opened list.

import (
	"encoding/json"
	"os"
)

// WsPrefs is the persisted workspace choice.
type WsPrefs struct {
	Current string   `json:"current"`
	Recents []string `json:"recents,omitempty"`
}

// LoadWsPrefs reads the workspace prefs; a missing or corrupt file keeps
// the caller's defaults.
func LoadWsPrefs(path string) (WsPrefs, error) {
	var p WsPrefs
	buf, err := os.ReadFile(path)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(buf, &p)
	return p, err
}

// SaveWsPrefs writes the workspace prefs atomically.
func SaveWsPrefs(path string, p WsPrefs) error {
	buf, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, buf)
}
