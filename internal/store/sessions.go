package store

// sessions.go is the transcript store: a session index (sessions.json,
// metadata only) plus one transcript file per session under sessions/.
// Roles, kinds and modes persist as strings so the schema does not depend
// on any UI library's enums.

import (
	"encoding/json"
	"log"
	"os"
	"time"
)

// Role says who a message came from.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Kind selects how the UI renders a message; the store only carries it.
type Kind string

const (
	KindPlain    Kind = "plain"
	KindReasoned Kind = "reasoned"
	KindTools    Kind = "tools"
	KindTyping   Kind = "typing"
	KindCommand  Kind = "command"
	KindDiff     Kind = "diff"
)

// Mode is the conversation mode ("chat" / "agent").
type Mode string

const (
	ModeChat  Mode = "chat"
	ModeAgent Mode = "agent"
)

// ToolCall is one merged tool invocation of a reply, flattened to plain
// data: the tool-call card shows args/result, the command card the
// terminal run, the file-change card the before/after of a write.
type ToolCall struct {
	CallID     string        `json:"callId"`
	Name       string        `json:"name"`
	Args       string        `json:"args,omitempty"`
	Result     string        `json:"result,omitempty"`
	ErrMsg     string        `json:"errMsg,omitempty"`
	State      int           `json:"state"`
	Dur        time.Duration `json:"dur,omitempty"`
	Command    string        `json:"command,omitempty"`
	Dir        string        `json:"dir,omitempty"`
	Output     string        `json:"output,omitempty"`
	Running    bool          `json:"running,omitempty"`
	ExitCode   int           `json:"exitCode,omitempty"`
	ChangePath string        `json:"changePath,omitempty"`
	ChangeOld  string        `json:"changeOld,omitempty"`
	ChangeNew  string        `json:"changeNew,omitempty"`
}

// Message is one transcript message. One AI reply is ONE message: its
// thinking text, tool calls and answer text all live here.
type Message struct {
	ID        string     `json:"id"`
	Role      Role       `json:"role"`
	Kind      Kind       `json:"kind"`
	Text      string     `json:"text"`
	LLMText   string     `json:"llmText,omitempty"`
	ThinkText string     `json:"thinkText,omitempty"`
	At        time.Time  `json:"at"`
	ThinkOpen bool       `json:"thinkOpen,omitempty"`
	Tools     []ToolCall `json:"tools,omitempty"`
	// Tool only appears in legacy single-tool rows (stored before the
	// merge); the UI keeps rendering them.
	Tool *ToolCall `json:"tool,omitempty"`
}

// Transcript is one session's transcript file.
type Transcript struct {
	Mode Mode      `json:"mode,omitempty"`
	Rows []Message `json:"rows"`
}

// SessionMeta is one entry of the session index.
type SessionMeta struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Updated   time.Time `json:"updated"`
	Pinned    bool      `json:"pinned"`
	Workspace string    `json:"workspace"`
	Mode      Mode      `json:"mode,omitempty"`
	Threaded  bool      `json:"threaded"`
	// Rows only appears in the legacy single-file format; its presence
	// triggers the one-time migration into per-session files.
	LegacyRows []Message `json:"rows,omitempty"`
}

// SessionIndex is the on-disk shape of sessions.json.
type SessionIndex struct {
	Sessions []SessionMeta `json:"sessions"`
}

// LoadIndex reads the session index; a missing file returns an empty index.
func LoadIndex(path string) (SessionIndex, error) {
	var idx SessionIndex
	buf, err := os.ReadFile(path)
	if err != nil {
		return idx, err
	}
	err = json.Unmarshal(buf, &idx)
	return idx, err
}

// SaveIndex writes the session index atomically.
func SaveIndex(path string, idx SessionIndex) error {
	buf, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(path, buf)
}

// LoadTranscript reads one session's transcript file.
func LoadTranscript(dir, id string) (Transcript, error) {
	var t Transcript
	buf, err := os.ReadFile(TranscriptPath(dir, id))
	if err != nil {
		return t, err
	}
	err = json.Unmarshal(buf, &t)
	return t, err
}

// SaveTranscript writes one session's transcript file atomically.
func SaveTranscript(dir, id string, t Transcript) error {
	buf, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if err := WriteFileAtomic(TranscriptPath(dir, id), buf); err != nil {
		return err
	}
	return nil
}

// MigrateLegacyIndex splits a legacy single-file index (sessions with
// embedded rows) into the per-session layout: it writes each transcript
// file, saves the row-less index, and keeps the original as .bak. It
// reports whether a migration happened; the caller re-reads the index.
func MigrateLegacyIndex(path, dir string, idx SessionIndex) bool {
	legacy := false
	for i := range idx.Sessions {
		s := &idx.Sessions[i]
		if len(s.LegacyRows) == 0 {
			continue
		}
		legacy = true
		if err := SaveTranscript(dir, s.ID, Transcript{Mode: s.Mode, Rows: s.LegacyRows}); err != nil {
			log.Printf("crux: transcript %s not migrated: %v", s.ID, err)
			continue
		}
		s.LegacyRows = nil // the rows now live in their own file
	}
	if !legacy {
		return false
	}
	if raw, err := os.ReadFile(path); err == nil {
		_ = WriteFileAtomic(path+".bak", raw) // keep the original as a backup
	}
	if err := SaveIndex(path, SessionIndex{Sessions: idx.Sessions}); err != nil {
		log.Printf("crux: migrated index not saved: %v", err)
	}
	return true
}
