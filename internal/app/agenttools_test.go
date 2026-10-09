package app

// agenttools_test.go covers the UI side of tool runs: the three legacy card
// rows a tool call produces render headless, and a streaming reply's tool
// calls merge into the ONE assistant row instead of creating rows.

import (
	"testing"
	"time"

	muiagent "github.com/ZacharyZhang-NY/MujicaUI/agent"
	"github.com/ZacharyZhang-NY/MujicaUI/chat"
	"github.com/egoist/mygo/ui"
)

// The three tool card rows render headless with real toolRun data (legacy
// standalone tool rows, kept for transcripts stored before the merge).
func TestAgentCardsRender(t *testing.T) {
	a := newApp()
	a.thread.rows = []row{
		{id: "u1", role: chat.MessageUser, text: "run the tests", at: time.Now()},
		{id: "t1", role: chat.MessageAssistant, kind: rowCommand, at: time.Now(),
			tool: &toolRun{
				callID: "c1", name: "bash", state: muiagent.AgentDone, dur: 120 * time.Millisecond,
				args: `{"command":"go test ./..."}`,
				run:  muiagent.CommandRun{Command: "go test ./...", Dir: ".", ExitCode: 0, Duration: 120 * time.Millisecond, Output: "ok"},
			}},
		{id: "t2", role: chat.MessageAssistant, kind: rowTools, at: time.Now(),
			tool: &toolRun{
				callID: "c2", name: "read_file", state: muiagent.AgentDone, dur: 5 * time.Millisecond,
				args: `{"path":"main.go"}`, result: `{"content":[{"type":"text","text":"package main"}]}`,
			}},
		{id: "t3", role: chat.MessageAssistant, kind: rowDiff, at: time.Now(),
			tool: &toolRun{
				callID: "c3", name: "write_file", state: muiagent.AgentDone, dur: 3 * time.Millisecond,
				args:   `{"path":"x.txt"}`,
				change: muiagent.FileChange{Path: "x.txt", Old: "a\n", New: "b\n"},
			}},
		{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: time.Now(),
			thinkText: "the tests pass", text: "All green."},
	}
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("agent cards render nil")
	}
}

// One AI reply is ONE row: a headless send produces two rows, and tool
// calls that arrive afterwards merge into the assistant row's card block
// instead of creating rows. Once the reply ends, streaming clears so the
// text renders as markdown.
func TestAgentTurnsMergeIntoOneRow(t *testing.T) {
	a := newApp()
	a.thread.draft = "run it"
	a.send() // headless: user row + finished assistant row
	if len(a.thread.rows) != 2 {
		t.Fatalf("send produced %d rows, want 2", len(a.thread.rows))
	}
	asst := a.thread.rows[1]
	if asst.streaming {
		t.Fatal("a finished headless reply must not be streaming")
	}
	a.appendTurnTool(asst.id, "c1", "bash", `{"command":"echo hi"}`)
	a.appendTurnTool(asst.id, "c2", "write_file", `{"path":"x.txt"}`)
	a.finishToolRow("c1", `{"content":[{"text":"hi"}]}`, false, 10*time.Millisecond)
	a.finishToolRow("c2", `{"content":[{"text":"ok"}],"details":{"path":"x.txt","old":"a","new":"b"}}`, false, 5*time.Millisecond)
	if len(a.thread.rows) != 2 {
		t.Fatalf("tool calls created rows: %d", len(a.thread.rows))
	}
	tools := a.thread.rows[1].tools
	if len(tools) != 2 || tools[0].run.Output != "hi" || tools[1].change.Path != "x.txt" {
		t.Fatalf("merged tools wrong: %+v", tools)
	}
	if ui.Render(a.view, 1280, 820, 1) == nil {
		t.Fatal("merged reply render nil")
	}
}
