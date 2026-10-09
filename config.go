package main

// config.go persists the LLM settings as JSON in the user's HOME directory
// (~/.mujicaui-agent-demo) so the backend survives restarts. The API key is
// stored in plain text beside the other fields — a desktop demo's trade-off
// — so the file is written 0600 inside a 0700 directory. A store left by an
// older release under the OS config directory migrates here once.

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
)

// configDirName is the app's data folder name; under the home directory it
// is dot-prefixed.
const configDirName = "MujicaUI-agent-demo"

// configDir is the app's data folder in the user's home directory
// (~/.mujicaui-agent-demo) — deliberately NOT the OS config directory.
func configDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", err
	}
	return filepath.Join(home, "."+configDirName), nil
}

// legacyConfigDir is where releases before the home-dir move kept the data
// (the OS config directory, %AppData% on Windows); only the migration reads it.
func legacyConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, configDirName), nil
}

// settingsFile returns the path of the persisted settings JSON.
func settingsFile() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// migrateConfigDir copies a legacy store from the OS config directory into
// the home directory once. migrateConfigDirFrom does the work against
// explicit directories (tests inject temp dirs).
func migrateConfigDir() {
	oldDir, err := legacyConfigDir()
	if err != nil {
		return
	}
	newDir, err := configDir()
	if err != nil {
		return
	}
	migrateConfigDirFrom(oldDir, newDir)
}

func migrateConfigDirFrom(oldDir, newDir string) {
	if st, err := os.Stat(oldDir); err != nil || !st.IsDir() {
		return // nothing stored the old way
	}
	for _, name := range []string{"settings.json", "workspace.json", "sessions.json"} {
		copyIfMissing(filepath.Join(oldDir, name), filepath.Join(newDir, name))
	}
	entries, err := os.ReadDir(filepath.Join(oldDir, "sessions"))
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			copyIfMissing(filepath.Join(oldDir, "sessions", e.Name()),
				filepath.Join(newDir, "sessions", e.Name()))
		}
	}
}

// copyIfMissing copies src to dst only when dst does not exist yet, so data
// already written to the new location always wins.
func copyIfMissing(src, dst string) {
	if _, err := os.Stat(dst); err == nil {
		return
	}
	buf, err := os.ReadFile(src)
	if err != nil {
		return
	}
	if err := writeFileAtomic(dst, buf); err == nil {
		log.Printf("atlas: migrated %s", dst)
	}
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
	if err := writeFileAtomic(a.configPath, data); err != nil {
		log.Printf("atlas: settings not saved: %v", err)
		return
	}
	a.savedSettings = string(data)
}
