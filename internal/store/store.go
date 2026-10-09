// Package store is Crux's data layer: where the app persists its settings,
// workspace choice and per-session transcripts, and the on-disk shapes of
// that data. It knows nothing about the UI — the UI layer converts between
// these schemas and its view state.
package store

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DirName is the app's data folder name in the user's home directory.
const DirName = "crux-agent"

// HomeDir is the user's home directory, falling back to the process
// directory when the OS cannot name one.
func HomeDir() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// Dir is the app's data folder in the user's home directory
// (~/.crux-agent) — deliberately NOT the OS config directory and never
// the executable's launch directory.
func Dir() (string, error) {
	home := HomeDir()
	if home == "" {
		return "", os.ErrNotExist
	}
	return filepath.Join(home, "."+DirName), nil
}

// LegacyDirs lists where earlier releases kept the data, oldest convention
// first: the OS config directory (%AppData% on Windows), then the
// home-directory folder under the app's previous name. Only Migrate reads
// them.
func LegacyDirs() []string {
	var dirs []string
	if base, err := os.UserConfigDir(); err == nil {
		dirs = append(dirs, filepath.Join(base, "MujicaUI-agent-demo"))
	}
	if home := HomeDir(); home != "" {
		dirs = append(dirs, filepath.Join(home, ".mujicaui-agent-demo"))
	}
	return dirs
}

// SettingsPath returns the path of the persisted settings JSON.
func SettingsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// WorkspacePrefsPath returns the path of the persisted workspace choice.
func WorkspacePrefsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "workspace.json"), nil
}

// SessionsPath returns the path of the session index.
func SessionsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions.json"), nil
}

// TranscriptPath is one session's transcript file inside dir; ids are
// sanitized so a hostile index cannot escape the directory.
func TranscriptPath(dir, id string) string {
	var sb strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			sb.WriteRune(r)
		default:
			sb.WriteRune('_')
		}
	}
	return filepath.Join(dir, "sessions", sb.String()+".json")
}

// WriteFileAtomic replaces path with data: write a uniquely named temp file
// in the target's directory, then rename it over the target. The unique
// name (os.CreateTemp) keeps two saves of the same file — or two app
// instances — from consuming each other's temp, which used to fail the
// rename with "The system cannot find the file specified". A stale target
// with a broken ACL (the "Access is denied" class) is removed once and the
// rename retried, so a bad file heals itself. If the environment still
// refuses the rename (antivirus interference with fresh .tmp files is the
// usual suspect), the data falls back to a direct write rather than being
// lost.
func WriteFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
		if err != nil {
			return err
		}
		name := tmp.Name()
		_, werr := tmp.Write(data)
		if cerr := tmp.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			os.Remove(name)
			lastErr = werr
			time.Sleep(30 * time.Millisecond)
			continue
		}
		if rerr := os.Rename(name, path); rerr != nil {
			lastErr = rerr
			if os.Remove(path) == nil { // heal a locked/broken-ACL target
				if err2 := os.Rename(name, path); err2 == nil {
					return nil
				}
			}
		} else {
			return nil
		}
		os.Remove(name)
		time.Sleep(30 * time.Millisecond)
	}
	// The rename keeps failing (antivirus interference with fresh .tmp
	// files is the usual suspect): write directly so the save is not lost.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return lastErr
	}
	return nil
}

// Migrate copies legacy stores into the home directory once: the
// settings/workspace/session files and every per-session transcript, never
// overwriting newer files. The old folders stay untouched as backups.
func Migrate() {
	newDir, err := Dir()
	if err != nil {
		return
	}
	for _, oldDir := range LegacyDirs() {
		migrateDirFrom(oldDir, newDir)
	}
}

func migrateDirFrom(oldDir, newDir string) {
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
	if err := WriteFileAtomic(dst, buf); err == nil {
		log.Printf("crux: migrated %s", dst)
	}
}
