package app

// proclayers_test.go covers the process area's timeline (SPEC §7.3.2): the
// group header's summary line, the fold-on-completion behavior, the
// setThink interleaving that orders reasoning segments between tool calls,
// and the height-capped open state that keeps a long process from taking
// over the message flow.

import (
	"strings"
	"testing"
	"time"

	muiagent "github.com/ZacharyZhang-NY/MujicaUI/agent"
	"github.com/ZacharyZhang-NY/MujicaUI/chat"
	"github.com/egoist/mygo/ui"
)

func TestProcSummary(t *testing.T) {
	r := &row{kind: rowReasoned, streaming: true}
	if s := procSummary(r); s != "Thinking…" {
		t.Fatalf("empty streaming summary = %q", s)
	}
	r.tools = []*toolRun{{callID: "1", name: "bash", state: muiagent.AgentRunning}}
	if s := procSummary(r); s != "Running bash…" {
		t.Fatalf("running summary = %q", s)
	}
	r.tools = append(r.tools, &toolRun{callID: "2", name: "read_file", state: muiagent.AgentRunning})
	if s := procSummary(r); s != "Running read_file…" {
		t.Fatalf("the latest running tool must win: %q", s)
	}
	r.streaming = false
	if s := procSummary(r); s != "Ran commands and read files" {
		t.Fatalf("done summary = %q", s)
	}
	r.tools = append(r.tools,
		&toolRun{callID: "3", name: "write_file"},
		&toolRun{callID: "4", name: "grep"})
	if s := procSummary(r); s != "Ran commands, read files and edited files…" {
		t.Fatalf("summary must cap at three kinds: %q", s)
	}
	r.tools = nil
	if s := procSummary(r); s != "Thought" {
		t.Fatalf("thought-only summary = %q", s)
	}
}

func TestEndStreamFoldsProcess(t *testing.T) {
	a := newApp()
	base := time.Now().Add(-time.Minute)
	a.thread.rows = []row{
		{id: "u0", role: chat.MessageUser, text: "hi", at: base},
		{id: "a1", role: chat.MessageAssistant, kind: rowReasoned,
			at: base.Add(time.Second), procOpen: true, streaming: true, thinkText: "…"},
	}
	a.endStreamingRow("a1")
	r := a.thread.rows[1]
	if r.streaming || r.procOpen || r.doneAt.IsZero() {
		t.Fatalf("completion did not fold: streaming=%v procOpen=%v doneAt=%v",
			r.streaming, r.procOpen, r.doneAt)
	}
	if !r.doneAt.After(r.at) {
		t.Fatal("doneAt should pin the elapsed span")
	}
}

func TestProcessLayers(t *testing.T) {
	a := newApp()
	now := time.Now()
	think := "The job died on a quota error.\n" + strings.Repeat("Consider the archive cap.\n", 200)
	a.thread.rows = []row{
		{id: "u1", role: chat.MessageUser, text: "fix it", at: now.Add(-time.Minute)},
		{id: "a1", role: chat.MessageAssistant, kind: rowReasoned,
			at: now.Add(-40 * time.Second), doneAt: now.Add(-10 * time.Second),
			thinkText: think, text: "Fixed the cap.",
			tools: []*toolRun{
				{callID: "c1", name: "bash", state: muiagent.AgentDone, dur: time.Second,
					run: muiagent.CommandRun{Command: "tail -1 log"}},
				{callID: "c2", name: "write_file", state: muiagent.AgentDone, dur: 2 * time.Second,
					change: muiagent.FileChange{Path: "a.go", Old: "x", New: "y"}},
				{callID: "c3", name: "read_file", state: muiagent.AgentFailed, dur: time.Millisecond},
			}},
	}

	// Collapsed: the header is one summary line and the thinking body is
	// not built at all (Collapsible skips closed content).
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	if tst.HasText("Consider the archive cap.") {
		t.Fatal("closed process area must not build the thinking body")
	}
	hdr, ok := tst.Find("Ran commands, edited files and read files")
	if !ok || hdr.H > 60 {
		t.Fatalf("collapsed header not one line: ok=%v rect={%g %g %g %g}",
			ok, hdr.X, hdr.Y, hdr.W, hdr.H)
	}

	// Open: the timeline renders its steps in order — one think row and one
	// row per tool — and the reply's own body stays on screen: the capped
	// scroller keeps 200 lines of reasoning from pushing the answer off.
	a.thread.rows[1].procOpen = true
	steps := a.thread.rows[1].timeline() // materialize legacy rows first
	for i := range steps {
		steps[i].open = true
	}
	tst.Frame()
	tst.Frame()
	if !tst.HasText("Consider the archive cap.") {
		t.Fatal("open process area missing the thinking body")
	}
	for _, s := range []string{
		"Thinking",                   // the reasoning row's title
		"Ran command", "tail -1 log", // bash row: title + summary
		"Wrote file", "a.go", // write_file row
		"Read file", "failed", // read_file row (the failed one)
	} {
		if !tst.HasText(s) {
			t.Fatalf("open process area missing %q", s)
		}
	}
	// The timeline follows execution order: the reasoning row sits above
	// the first tool row, which sits above the last one.
	thinkRect, _ := tst.Find("Thinking")
	bashRect, ok := tst.Find("Ran command")
	if !ok || thinkRect.Y > bashRect.Y {
		t.Fatalf("timeline order broken: think@%g bash@%g", thinkRect.Y, bashRect.Y)
	}
	readRect, _ := tst.Find("Read file")
	if readRect.Y < bashRect.Y {
		t.Fatalf("timeline order broken: bash@%g read@%g", bashRect.Y, readRect.Y)
	}
	body, ok := tst.Find("Fixed the cap.")
	if !ok || body.Y+body.H > 820 {
		t.Fatalf("reply body pushed off-screen by an uncapped process area: %+v", body)
	}
	// The step rows are flow rows before the answer bubble, not nested in it.
	if bashRect.Y > body.Y {
		t.Fatalf("tool rows must precede the answer bubble: bash@%g body@%g", bashRect.Y, body.Y)
	}

	// While the reply streams with no answer text yet, the bubble holds the
	// process only — the answer text stays empty until its first token.
	a.thread.rows[1].streaming = true
	a.thread.rows[1].text = ""
	tst.Frame()
	if tst.HasText("Fixed the cap.") {
		t.Fatal("answer text must not render before the first token")
	}
	if !tst.HasText("Thinking") {
		t.Fatal("streaming process must stay in the bubble")
	}
}

func TestThinkTimeline(t *testing.T) {
	r := row{}
	// Same-turn growth extends the segment in place.
	r.setThink("The export died on")
	r.setThink("The export died on a quota error.")
	// A tool call lands between reasoning stretches.
	r.tools = append(r.tools, &toolRun{callID: "c1", name: "bash", state: muiagent.AgentRunning})
	r.steps = append(r.steps, step{tool: r.tools[0]})
	// The engine resets its buffer per turn: thinking that does not extend
	// the last segment starts a new one — the interleaving DSH renders.
	r.setThink("Now check the retention cap.")
	r.setThink("Now check the retention cap. It holds seven days.")
	// Interim messages land the same way: same-turn growth extends the
	// last segment; the next turn's text starts a new one after tools.
	r.setText("The quota error is at line 3.")
	r.setText("The quota error is at line 3. Raising the cap next.")
	if len(r.steps) != 4 || r.steps[3].text != "The quota error is at line 3. Raising the cap next." {
		t.Fatalf("text timeline wrong: %+v", r.steps)
	}
	// thinkText / text persist all segments joined, not just the last turn's.
	if r.thinkText != "The export died on a quota error.\n\nNow check the retention cap. It holds seven days." {
		t.Fatalf("joined thinkText wrong: %q", r.thinkText)
	}
	if r.text != "The quota error is at line 3. Raising the cap next." {
		t.Fatalf("joined text wrong: %q", r.text)
	}
	// The LAST message segment is the answer; rows without text segments
	// fall back to the whole text (loaded transcripts, error rows).
	if ans, i := r.answerText(); ans != "The quota error is at line 3. Raising the cap next." || i != 3 {
		t.Fatalf("answerText wrong: %q idx=%d", ans, i)
	}
	legacy := row{text: "whole reply"}
	if ans, i := legacy.answerText(); ans != "whole reply" || i != -1 {
		t.Fatalf("legacy answerText wrong: %q idx=%d", ans, i)
	}

	// Streaming appends a tool step that shares the *toolRun pointer, so
	// result updates flow into the timeline.
	a := newApp()
	a.thread.rows = []row{
		{id: "u0", role: chat.MessageUser, text: "hi", at: time.Now()},
		{id: "a1", role: chat.MessageAssistant, kind: rowReasoned, at: time.Now(), streaming: true},
	}
	a.appendTurnTool("a1", "c9", "bash", `{"command":"ls -la"}`)
	a.finishToolRow("c9", `{"content":[{"text":"total 0"}]}`, false, 3*time.Second)
	st := a.thread.rows[1]
	if len(st.steps) != 1 || st.steps[0].tool != st.tools[0] {
		t.Fatalf("tool step not shared with the tools list: %+v", st.steps)
	}
	if st.steps[0].tool.state != muiagent.AgentDone || st.steps[0].tool.result == "" {
		t.Fatal("result update did not reach the timeline step")
	}
}

func TestTextStepsRender(t *testing.T) {
	a := newApp()
	now := time.Now()
	r := row{id: "a1", role: chat.MessageAssistant, kind: rowReasoned,
		at: now.Add(-time.Minute), doneAt: now.Add(-30 * time.Second), procOpen: true}
	r.setThink("Check the log first.")
	r.setText("The quota error is at line 3.") // interim: a tool follows it
	r.steps = append(r.steps, step{tool: &toolRun{callID: "c1", name: "read_file",
		state: muiagent.AgentDone, dur: 5 * time.Millisecond, args: `{"path":"log"}`}})
	r.setText("Raised the cap; the job passed.") // the final answer
	a.thread.rows = []row{
		{id: "u1", role: chat.MessageUser, text: "fix it", at: now.Add(-2 * time.Minute)},
		r,
	}
	tst := ui.NewTester(a.view, 1280, 820)
	tst.Frame()
	tst.Frame()
	// The interim message renders in the timeline, between the reasoning
	// row and the tool row, outside the answer bubble.
	interm, ok := tst.Find("The quota error is at line 3.")
	if !ok {
		t.Fatal("interim message missing from the timeline")
	}
	ans, ok := tst.Find("Raised the cap; the job passed.")
	if !ok {
		t.Fatal("answer segment missing from the bubble")
	}
	if interm.Y > ans.Y {
		t.Fatalf("interim message must precede the answer: %g > %g", interm.Y, ans.Y)
	}
	thinkRect, _ := tst.Find("Thinking")
	toolRect, _ := tst.Find("Read file")
	if thinkRect.Y > interm.Y || interm.Y > toolRect.Y {
		t.Fatalf("interim message not between thinking and the tool: think@%g text@%g tool@%g",
			thinkRect.Y, interm.Y, toolRect.Y)
	}
	// The persisted shape keeps both segments joined.
	if r.text != "The quota error is at line 3.\n\nRaised the cap; the job passed." {
		t.Fatalf("joined text wrong: %q", r.text)
	}
}
