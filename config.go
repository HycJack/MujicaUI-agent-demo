package main

// config.go persists the LLM settings as JSON in the user's config
// directory so the backend survives restarts. The API key is stored in
// plain text beside the other fields — a desktop demo's trade-off — so
// the file is written 0600 inside a 0700 directory.

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
)

// configDirName is the folder under the user config directory that holds
// settings.json.
const configDirName = "MujicaUI-agent-demo"

// settingsFile returns the path of the persisted settings JSON.
func settingsFile() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, configDirName, "settings.json"), nil
}

// loadSettings merges the persisted settings into a.llm; a missing or
// corrupt file keeps the defaults. Called once at startup.
func (a *app) loadSettings() {
	if a.configPath == "" {
		return
	}
	data, err := os.ReadFile(a.configPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("atlas: settings not loaded: %v", err)
		}
		return
	}
	if err := json.Unmarshal(data, &a.llm); err != nil {
		log.Printf("atlas: settings ignored (%v): %s", err, a.configPath)
		return
	}
	a.savedSettings = string(data)
}

// saveSettings writes the current LLM settings to disk and snapshots the
// written form so the dialog's live-save dirty check stays quiet.
func (a *app) saveSettings() {
	if a.configPath == "" {
		return
	}
	data, err := json.MarshalIndent(a.llm, "", "  ")
	if err != nil {
		panic("atlas: marshal settings: " + err.Error()) // program error: fail fast
	}
	if err := os.MkdirAll(filepath.Dir(a.configPath), 0o700); err != nil {
		log.Printf("atlas: settings not saved: %v", err)
		return
	}
	if err := os.WriteFile(a.configPath, data, 0o600); err != nil {
		log.Printf("atlas: settings not saved: %v", err)
		return
	}
	a.savedSettings = string(data)
}
