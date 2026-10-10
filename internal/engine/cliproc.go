package engine

// cliproc.go holds the pieces the claude CLI adapter shares with nothing
// else yet but every future CLI adapter would: the reaper that bounds a
// resident CLI's exit, and the display helpers that keep a card bounded
// and redacted. Ported from mygo-agent's harness/cli; the kill path is
// platform-specific (see cliproc_windows.go / cliproc_unix.go) because
// Windows has no process groups — there the tree dies by taskkill.

import (
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ReapGrace is how long a CLI gets to exit after its turn settled, before
// its process tree is torn down. A CLI that exits on stdin EOF is gone in
// milliseconds; the grace only covers a slow final flush.
var ReapGrace = 3 * time.Second

// Reap waits for a CLI to exit after the turn settled, and reports
// whether the process had to be killed.
//
// `cmd.WaitDelay` only engages once the context is already done, so a CLI
// that lingers after announcing the turn is over would hold the turn
// forever. stdin is closed first when the caller can — that is what makes
// a resident CLI exit (the claude lesson) — and a CLI that stays anyway
// gets its tree torn down so Wait can always return.
//
// `killed` is not a failure signal: a stop we asked for is a normal end
// to a turn. Callers use it only to keep a deliberate kill out of the
// reply's error text.
func Reap(cmd *exec.Cmd, stdin io.Closer) (waitErr error, killed bool) {
	if stdin != nil {
		_ = stdin.Close()
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	settled := func() (error, bool) {
		select {
		case err := <-done:
			return err, false
		case <-time.After(ReapGrace):
			return nil, true
		}
	}
	err, overdue := settled()
	if !overdue {
		return err, false
	}
	terminateTree(cmd)
	if err, overdue := settled(); !overdue {
		return err, true
	}
	// Alive after the tree teardown: kill the leader so Wait can return.
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	err, _ = settled()
	return err, true
}

// Trunc cuts s to about n bytes without splitting a rune, appending an
// ellipsis when it does.
func Trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ShortArgs summarizes a raw JSON argument object for a card title: the
// string-valued fields joined, or the raw JSON cut short.
func ShortArgs(raw json.RawMessage) string {
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil || in == nil {
		s := string(raw)
		if len(s) > 120 {
			return s[:120] + "…"
		}
		return s
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var parts []string
	for _, k := range keys {
		if s, ok := in[k].(string); ok {
			parts = append(parts, s)
		}
	}
	joined := strings.Join(parts, " ")
	if len(joined) > 120 {
		joined = joined[:120] + "…"
	}
	return joined
}

// TrimOutput caps a tool result so one command cannot flood the context.
//
// The shape is head and tail, not head alone: a command's errors live at
// the end of its output and a file's structure at its start, so a cut
// that keeps only the first half throws away exactly the half a failing
// run was about to show. Cuts land on rune boundaries.
//
// The carriage return goes with the trailing newline: a Windows command
// ends its line with CRLF, so trimming only the \n leaves a \r inside the
// stored card — a stray character on screen.
func TrimOutput(s string, max int) string {
	s = strings.TrimRight(s, "\r\n")
	if len(s) <= max {
		return s
	}
	if max < 64 {
		// Too small to split meaningfully: keep the head alone.
		return s[:max] + "\n… output truncated …"
	}
	head := runeFloor(s, max*2/3)
	tailStart := len(s) - (max - max*2/3)
	for tailStart < len(s) && isContinuation(s[tailStart]) {
		tailStart++
	}
	elided := tailStart - head
	return s[:head] +
		fmt.Sprintf("\n… output truncated: %d bytes elided …\n", elided) +
		s[tailStart:]
}

// runeFloor lowers n until it is not in the middle of a UTF-8 rune.
func runeFloor(s string, n int) int {
	for n > 0 && n < len(s) && isContinuation(s[n]) {
		n--
	}
	return n
}

// isContinuation reports whether b is a UTF-8 continuation byte.
func isContinuation(b byte) bool { return b&0xC0 == 0x80 }

// ShortSession trims a session id for a summary note.
func ShortSession(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// CompactedNotice is the one line a backend's own context compaction
// produces: without it a long task silently forgets, and the only symptom
// is an answer that contradicts something agreed twenty turns ago.
func CompactedNotice(provider string, before, after int) string {
	if before > 0 && after > 0 {
		return fmt.Sprintf("%s compacted its context · %s → %s tokens · earlier turns are summarised, not lost",
			provider, CompactTokens(before), CompactTokens(after))
	}
	return fmt.Sprintf("%s compacted its context · earlier turns are summarised, not lost", provider)
}

// CompactTokens renders a token count at the size a note can carry.
func CompactTokens(n int) string {
	switch {
	case n < 1000:
		return strconv.Itoa(n)
	case n < 100000:
		return strconv.FormatFloat(float64(n)/1000, 'f', 1, 64) + "k"
	case n < 1000000:
		return strconv.Itoa(n/1000) + "k"
	default:
		return strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64) + "M"
	}
}

// Redact masks credential-shaped text before it reaches a card, a
// transcript or a log. Truncation bounds a line but does not hide a
// secret in it: `curl -H "Authorization: Bearer sk-…"` is exactly the
// case a user must be able to read enough of to approve, and exactly the
// case that must not put a live token on screen.
//
// The patterns are deliberately narrow — a key that names a credential
// followed by its value, plus the well-known literal token prefixes. A
// broad "anything long" rule would redact ordinary long paths and hashes
// and make the ask unreadable, which is its own safety problem.
func Redact(s string) string {
	for _, p := range secretPatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}

type redaction struct {
	re   *regexp.Regexp
	repl string
}

var secretPatterns = []redaction{
	// Authorization: Bearer <token>  (and Proxy-Authorization)
	{regexp.MustCompile(`(?i)((?:proxy-)?authorization\s*[:=]\s*)(?:bearer\s+|basic\s+|token\s+)?\S+`), "${1}«redacted»"},
	// --api-key X, --token=X, --password X
	{regexp.MustCompile(`(?i)(--?(?:api[-_]?key|token|password|passwd|secret|access[-_]?key)\s*[:=]?\s*)\S+`), "${1}«redacted»"},
	// API_KEY=…, GITHUB_TOKEN: …, AWS_SECRET_ACCESS_KEY=…
	{regexp.MustCompile(`(?i)(\b[A-Z0-9_]*(?:api[-_]?key|token|password|passwd|secret|access[-_]?key)\b\s*[:=]\s*)("?)\S+`), "${1}${2}«redacted»"},
	// Well-known literal token formats, anywhere.
	{regexp.MustCompile(`\b(sk-|ghp_|gho_|github_pat_|xox[baprs]-|AKIA|AIza|eyJ)[A-Za-z0-9_.\-]{8,}`), "${1}«redacted»"},
}
