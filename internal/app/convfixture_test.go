package app

// convfixture_test.go builds a representative agent conversation for the
// layout and screenshot tests. One AI reply is ONE row: its thinking and
// tool calls fold into the collapsible block, the answer renders below.

import (
	"time"

	muiagent "github.com/HycJack/MujicaUI/agent"
	"github.com/HycJack/MujicaUI/chat"
)

func newConversationApp() *app {
	a := newApp()
	now := time.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	a.thread.rows = []row{
		{id: "u1", role: chat.MessageUser, text: "The nightly export failed again. Find out why and fix the job.", at: ago(2 * time.Hour)},
		{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: ago(2*time.Hour - 40*time.Second),
			thinkText: "The job died on a quota error at 02:14. Check the last run's log, then raise the cap in jobs/export-nightly.sh.",
			text:      "The job died on a **quota error** at 02:14 — the archive bucket holds seven days of dumps.\n\nWhat I did:\n\n1. Read the last run's log\n2. Raised the retention cap in `jobs/export-nightly.sh`\n3. Re-ran the export — it passed\n\n| step | result |\n| --- | --- |\n| log tail | quota exceeded |\n| re-run | ok |",
			tools: []*toolRun{
				{
					callID: "call-1", name: "bash", state: muiagent.AgentDone, dur: 4200 * time.Millisecond,
					args: `{"command":"tail -40 var/log/export-nightly.log"}`,
					run:  muiagent.CommandRun{Command: "tail -40 var/log/export-nightly.log", Dir: "~", ExitCode: 1, Duration: 4200 * time.Millisecond, Output: "\x1b[31mquota exceeded: bucket holds 7 days of dumps\x1b[0m"},
				},
				{
					callID: "call-2", name: "read_file", state: muiagent.AgentDone, dur: 5 * time.Millisecond,
					args: `{"path":"jobs/export-nightly.sh"}`,
				},
				{
					callID: "call-3", name: "write_file", state: muiagent.AgentDone, dur: 12 * time.Millisecond,
					args:   `{"path":"jobs/export-nightly.sh"}`,
					change: muiagent.FileChange{Path: "jobs/export-nightly.sh", Old: "build dump\nupload dump\n", New: "build dump\nprune yesterday\nupload dump\n"},
				},
			}},
		{id: "u2", role: chat.MessageUser, text: "Nice. Watch it tonight and page me if it slips.", at: ago(time.Hour)},
		{id: "a2", role: chat.MessageAssistant, kind: rowReasoned, at: ago(time.Hour - 30*time.Second),
			text: "Watching; I'll post the moment tonight's run passes ten minutes."},
	}
	return a
}
