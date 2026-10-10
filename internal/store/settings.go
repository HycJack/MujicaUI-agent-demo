package store

// settings.go is the persisted LLM configuration: the editable fields the
// settings dialog owns. Transient call state (busy, last error) lives in
// the UI layer and never reaches the file. The API key is stored in plain
// text beside the other fields — a desktop demo's trade-off — so the file
// is written 0600 inside a 0700 directory.

import (
	"encoding/json"
	"errors"
	"os"
)

// LLMConfig is the on-disk shape of the LLM settings.
type LLMConfig struct {
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	APIKey       string `json:"apiKey,omitempty"`
	BaseURL      string `json:"baseUrl,omitempty"`
	SystemPrompt string `json:"systemPrompt"`
	Thinking     string `json:"thinking"`
	// Backend selects the agent kind: "" (the pre-field default) and
	// "builtin" drive the built-in LLM loop; "claude" drives the Claude
	// Code CLI. A file from before the field exists loads as "".
	Backend string `json:"backend,omitempty"`
	// Mode is the three-level approval selector ("read-only" / "agent" /
	// "full"); "" loads as the agent default.
	Mode string `json:"mode,omitempty"`
}

// LoadSettings reads the settings file; a missing file returns the zero
// config and false, a corrupt file is ignored the same way.
func LoadSettings(path string) (LLMConfig, bool, error) {
	var cfg LLMConfig
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, false, nil
		}
		return cfg, false, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, false, err
	}
	return cfg, true, nil
}

// SaveSettings writes the settings atomically.
func SaveSettings(path string, cfg LLMConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, data)
}
