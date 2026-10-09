package main

// convfixture_test.go builds a representative agent conversation for the
// layout and screenshot tests: user prompt, streamed thinking + answer, a
// finished bash run, a tool call, and a file write with its review card.

import (
	"time"

	muiagent "github.com/ZacharyZhang-NY/MujicaUI/agent"
	"github.com/ZacharyZhang-NY/MujicaUI/chat"
)

func newConversationApp() *app {
	a := newApp()
	now := time.Now()
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	a.thread.rows = []row{
		{id: "u1", role: chat.MessageUser, text: "The nightly export failed again. Find out why and fix the job.", at: ago(2 * time.Hour)},
		{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: ago(2*time.Hour - 40*time.Second),
			thinkText: "The job died on a quota error at 02:14. Check the last run's log, then raise the cap in jobs/export-nightly.sh.",
			text:      "The job died on a **quota error** at 02:14 — the archive bucket holds seven days of dumps. I raised the cap and re-ran it."},
		{id: "t1", role: chat.MessageAssistant, kind: rowCommand, at: ago(2*time.Hour - 90*time.Second),
			tool: &toolRun{
				callID: "call-1", name: "bash", state: muiagent.AgentDone, dur: 4200 * time.Millisecond,
				args: `{"command":"tail -40 var/log/export-nightly.log"}`,
				run:  muiagent.CommandRun{Command: "tail -40 var/log/export-nightly.log", Dir: "~", ExitCode: 1, Duration: 4200 * time.Millisecond, Output: "\x1b[31mquota exceeded: bucket holds 7 days of dumps\x1b[0m"},
			}},
		{id: "t2", role: chat.MessageAssistant, kind: rowTools, at: ago(2*time.Hour - 95*time.Second),
			tool: &toolRun{
				callID: "call-2", name: "read_file", state: muiagent.AgentDone, dur: 5 * time.Millisecond,
				args: `{"path":"jobs/export-nightly.sh"}`,
			}},
		{id: "t3", role: chat.MessageAssistant, kind: rowDiff, at: ago(2*time.Hour - 2*time.Minute),
			tool: &toolRun{
				callID: "call-3", name: "write_file", state: muiagent.AgentDone, dur: 12 * time.Millisecond,
				args:   `{"path":"jobs/export-nightly.sh"}`,
				change: muiagent.FileChange{Path: "jobs/export-nightly.sh", Old: "build dump\nupload dump\n", New: "build dump\nprune yesterday\nupload dump\n"},
			}},
		{id: "u2", role: chat.MessageUser, text: "Nice. Watch it tonight and page me if it slips.", at: ago(time.Hour)},
		{id: "a2", role: chat.MessageAssistant, kind: rowReasoned, at: ago(time.Hour - 30*time.Second),
			text: "Watching; I'll post the moment tonight's run passes ten minutes."},
	}
	return a
}
