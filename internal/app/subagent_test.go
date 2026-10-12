package app

// subagent_test.go covers the sub-agent cards: a Task tool_use renders as a
// first-class "Sub-agent" step whose summary is the dispatched prompt (not
// raw JSON), and a TaskOutput poll renders as "Sub-agent output" naming the
// agent it pulled. Both arrive from the claude backend's wire (the
// live-probe session of 2026-10-11 pinned the exact shapes).

import (
	"strings"
	"testing"
	"time"

	muiagent "github.com/HycJack/MujicaUI/agent"
	"github.com/HycJack/MujicaUI/chat"
	"github.com/egoist/mygo/ui"
)

func TestSubagentCardNaming(t *testing.T) {
	if got := toolTitle("Task"); got != "Sub-agent" {
		t.Fatalf("toolTitle(Task) = %q", got)
	}
	if got := toolTitle("TaskOutput"); got != "Sub-agent output" {
		t.Fatalf("toolTitle(TaskOutput) = %q", got)
	}
	if got := toolIcon("Task"); got != "bot" {
		t.Fatalf("toolIcon(Task) = %q", got)
	}
	if got := toolIcon("TaskOutput"); got != "eye" {
		t.Fatalf("toolIcon(TaskOutput) = %q", got)
	}
}

// The collapsed summary is the dispatched mission, not the raw JSON args
// the model wrote; the poll names the agent it pulled.
func TestSubagentCardSummary(t *testing.T) {
	task := &toolRun{
		callID: "call_1",
		name:   "Task",
		args:   `{"description":"Pick random fruit name","prompt":"Pick a random fruit name and return only that single fruit name, nothing else. No punctuation, no explanation.","subagent_type":"general-purpose"}`,
	}
	got := toolSummary(task)
	if !strings.HasPrefix(got, "Pick a random fruit name and return only that single") {
		t.Fatalf("toolSummary(Task) = %q", got)
	}
	if strings.Contains(got, `"prompt"`) || len([]rune(got)) > 82 {
		t.Fatalf("toolSummary(Task) leaks raw JSON or runs long: %q", got)
	}
	poll := &toolRun{
		callID: "call_2",
		name:   "TaskOutput",
		args:   `{"agent_id":"affbd76"}`,
	}
	if got := toolSummary(poll); got != "agent affbd76" {
		t.Fatalf("toolSummary(TaskOutput) = %q", got)
	}
	// A degenerate Task call (no prompt, no description) still yields a
	// readable line instead of raw JSON.
	bare := &toolRun{callID: "call_3", name: "Task", args: `{"subagent_type":"general-purpose"}`}
	if got := toolSummary(bare); got != "" || got == bare.args {
		t.Fatalf("toolSummary(bare Task) = %q", got)
	}
}

// End to end: the running sub-agent and its mid-run poll both render on
// the timeline with their titles and readable summaries.
func TestSubagentCardsRender(t *testing.T) {
	a := newApp()
	a.thread.rows = append(a.thread.rows,
		row{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: time.Now(), streaming: true, procOpen: true})
	r := &a.thread.rows[0]
	task := &toolRun{
		callID: "call_1", name: "Task", state: muiagent.AgentRunning,
		args: `{"description":"Pick random fruit name","prompt":"Pick a random fruit name and return only that single fruit name.","subagent_type":"general-purpose"}`,
	}
	poll := &toolRun{
		callID: "call_2", name: "TaskOutput", state: muiagent.AgentDone, result: "Mango (partial)",
		args: `{"agent_id":"affbd76"}`,
	}
	r.tools = append(r.tools, task, poll)
	r.steps = append(r.steps, step{tool: task}, step{tool: poll, open: true})

	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	for _, want := range []string{"Sub-agent", "Sub-agent output", "Pick a random fruit name", "affbd76", "Mango (partial)"} {
		if !tst.HasText(want) {
			t.Fatalf("timeline missing %q", want)
		}
	}
}
