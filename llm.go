package main

// llm.go is the pi-ai-go integration surface. It resolves the configured
// backend into a piai.Model, converts the thread history into pi-ai messages,
// and streams a real assistant reply into the thread row. The heavy lifting —
// provider dispatch, SSE streaming, model pricing — lives in pi-ai-go; this
// file only bridges Atlas's state model to that library.
//
// Threading: the UI thread owns the app state. The streaming goroutine never
// touches the state directly; it buffers deltas locally and hands them to the
// UI thread through a.redraw(fn) (which wraps mygo's win.Update), exactly the
// pattern mygo documents for goroutines changing state.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	piai "github.com/HycJack/pi-ai-go"
	"github.com/HycJack/pi-ai-go/agent"
	"github.com/HycJack/pi-ai-go/core"
	_ "github.com/HycJack/pi-ai-go/providers" // registers the built-in providers at init
	muiagent "github.com/ZacharyZhang-NY/MujicaUI/agent"
	"github.com/ZacharyZhang-NY/MujicaUI/chat"
)

// resolveModel maps the app's LLMSettings to a pi-ai Model. A known provider
// comes from the built-in registry (with a base-URL override when present);
// the OpenAI-compatible provider is built directly, since its model id and
// endpoint are user-supplied and not in the registry.
func (a *app) resolveModel() (piai.Model, error) {
	if a.llm.Provider == openaiCompat {
		base := strings.TrimRight(strings.TrimSpace(a.llm.BaseURL), "/")
		if base == "" {
			return piai.Model{}, fmt.Errorf("openai-compatible: a Base URL is required")
		}
		id := strings.TrimSpace(a.llm.Model)
		if id == "" {
			return piai.Model{}, fmt.Errorf("openai-compatible: a model is required")
		}
		return piai.Model{
			ID:            id,
			Name:          id,
			Provider:      piai.ProviderOpenAI,
			API:           piai.APIOpenAICompletions,
			BaseURL:       base,
			ContextWindow: 8192,
		}, nil
	}
	m, err := piai.GetModel(piai.KnownProvider(a.llm.Provider), a.llm.Model)
	if err != nil {
		return piai.Model{}, err
	}
	if u := strings.TrimSpace(a.llm.BaseURL); u != "" {
		m.BaseURL = u
	}
	return m, nil
}

// thinkingLevel converts the settings string into a pi-ai ThinkingLevel, or
// disables reasoning when set to "none".
func (a *app) thinkingLevel() (piai.ThinkingLevel, bool) {
	switch strings.ToLower(a.llm.Thinking) {
	case "low":
		return piai.ThinkingLow, true
	case "medium", "med":
		return piai.ThinkingMedium, true
	case "high":
		return piai.ThinkingHigh, true
	case "xhigh":
		return piai.ThinkingXHigh, true
	}
	return "", false
}

// thinkDefault returns the default thinking level string for a model that
// supports reasoning, else "none".
func thinkDefault(m piai.Model) string {
	if !m.Reasoning {
		return "none"
	}
	return string(piai.ThinkingMedium)
}

// historyMessages converts the thread's user/assistant text rows into pi-ai
// messages, skipping tool/demo/decorative rows so the LLM sees clean history.
// A user row's llmText (draft + attached file contents) wins over its
// displayed text when set.
func (a *app) historyMessages() []piai.Message {
	msgs := make([]piai.Message, 0, len(a.thread.rows))
	for i := range a.thread.rows {
		r := &a.thread.rows[i]
		text := r.text
		if r.llmText != "" {
			text = r.llmText
		}
		if text == "" {
			continue
		}
		switch r.role {
		case chat.MessageUser:
			msgs = append(msgs, piai.UserMessage{Content: text, Timestamp: r.at})
		case chat.MessageAssistant:
			switch r.kind {
			case rowPlain, rowReasoned:
				msgs = append(msgs, piai.AssistantMessage{
					Content:   []piai.ContentBlock{piai.TextContent{Type: "text", Text: text}},
					Timestamp: r.at,
				})
			}
		}
	}
	return msgs
}

// send appends the user line and a live assistant row, then starts streaming
// the real reply in the background. The synchronous part keeps the UI and the
// tests predictable; the network work runs on a goroutine and lands on the UI
// thread through a.redraw.
func (a *app) send() {
	text := strings.TrimSpace(a.thread.draft)
	if text == "" || a.llm.Busy {
		return
	}
	now := time.Now()
	// Fold attached workspace files into the outgoing message: the visible
	// row lists their names, the LLM message carries their contents.
	llmText := text
	if names, block := a.attachedFilesBlock(); names != "" {
		llmText = text + "\n\n[Attached files]" + block
		text = text + "\n\nAttached: " + names
		a.thread.ctx = nil
	}
	userIdx := len(a.thread.rows)
	a.thread.rows = append(a.thread.rows,
		row{id: fmt.Sprintf("u%d", userIdx), role: chat.MessageUser, text: text, llmText: llmText, at: now})
	a.thread.rows = append(a.thread.rows,
		row{id: fmt.Sprintf("a%d", userIdx+1), role: chat.MessageAssistant, kind: rowReasoned, at: now.Add(time.Millisecond)})
	a.thread.draft = ""
	a.thread.list.ScrollToEnd()
	a.persistSessions()
	a.startStream(userIdx)
}

// attachedFilesBlock renders the thread's attached file chips as a prompt
// block: a comma list for the visible row and fenced contents for the LLM.
func (a *app) attachedFilesBlock() (names, block string) {
	var list []string
	var sb strings.Builder
	for _, it := range a.thread.ctx {
		if it.Kind != chat.ContextFile || !strings.HasPrefix(it.ID, "file:") {
			continue
		}
		rel := strings.TrimPrefix(it.ID, "file:")
		list = append(list, "`"+rel+"`")
		content, truncated, err := readCapped(filepath.Join(a.ws.root, filepath.FromSlash(rel)), wsAttachCap)
		sb.WriteString("\n\n--- " + rel + " ---\n")
		switch {
		case err != nil:
			sb.WriteString("(could not read: " + err.Error() + ")")
		case truncated:
			sb.WriteString(content + "\n… (truncated)")
		default:
			sb.WriteString(content)
		}
	}
	if len(list) == 0 {
		return "", ""
	}
	return strings.Join(list, ", "), sb.String()
}

// startStream launches the pi-ai agent loop for the already-appended user
// row at userIdx. The assistant row sits directly after it; every turn the
// loop takes — thinking, text, tool executions — lands in the transcript as
// its own rows through a.redraw.
func (a *app) startStream(userIdx int) {
	assistantIdx := userIdx + 1
	a.llm.Busy = true
	a.llm.LastError = ""
	ctx, cancel := context.WithCancel(context.Background())
	a.setCancel(cancel)

	if a.redraw == nil {
		// Headless / test path: don't block on the network; mark the row so
		// tests can progress deterministically.
		a.finishStream(assistantIdx)
		return
	}

	go func() {
		defer func() {
			a.redraw(func() {
				a.llm.Busy = false
				a.cancelFn = nil
				a.thread.list.ScrollToEnd()
				a.persistSessions()
			})
		}()
		model, err := a.resolveModel()
		if err != nil {
			a.streamError(assistantIdx, err)
			return
		}
		cfg := agent.AgentLoopConfig{
			Model:               model,
			SystemPrompt:        a.llm.SystemPrompt,
			Tools:               a.agentTools(),
			ToolExecution:       core.ToolExecSequential,
			ExecEnv:             core.NewDefaultExecutionEnvWithDir(a.ws.root),
			SimpleStreamOptions: a.streamOptions(),
		}
		stream := agent.AgentLoop(ctx, a.historyMessages(), cfg)
		s := &agentStream{
			app:       a,
			curID:     fmt.Sprintf("a%d", assistantIdx), // the row send() appended
			rowSeq:    assistantIdx,
			toolStart: map[string]time.Time{},
		}
		if _, err := stream.ForEach(ctx, s.onEvent); err != nil && ctx.Err() == nil {
			a.streamErrorRow(s.curID, err)
		}
	}()
}

// agentStream buffers the agent loop's deltas on the streaming goroutine and
// lands every state change on the UI thread through a.redraw. Rows are
// tracked by id (never by index): the ids are minted here on the goroutine
// and the UI thread only ever sees captured strings, so there is no shared
// index to race on.
type agentStream struct {
	app       *app
	curID     string // transcript row the current assistant message streams into
	rowSeq    int    // assistant-row id counter (goroutine-owned)
	toolSeq   int    // tool-row id counter (goroutine-owned)
	started   bool   // the first assistant message streams into send()'s row
	think     strings.Builder
	body      strings.Builder
	toolStart map[string]time.Time
}

// onEvent folds one agent event into the transcript.
func (s *agentStream) onEvent(evt agent.AgentEvent) error {
	switch e := evt.(type) {
	case agent.EventMessageStart:
		if s.started {
			// A later turn (after tool results): a fresh assistant row.
			s.think.Reset()
			s.body.Reset()
			s.rowSeq++
			id := fmt.Sprintf("a%d", s.rowSeq)
			s.app.redraw(func() {
				s.app.thread.rows = append(s.app.thread.rows, row{
					id: id, role: chat.MessageAssistant, kind: rowReasoned, at: time.Now(),
				})
				s.app.thread.list.ScrollToEnd()
			})
			s.curID = id
		} else {
			s.started = true
			s.think.Reset()
			s.body.Reset()
		}
	case agent.EventMessageUpdate:
		s.onAssistantEvent(e.AssistantEvent)
	case agent.EventToolExecStart:
		s.toolStart[e.ToolCallID] = time.Now()
		callID, name, args := e.ToolCallID, e.ToolName, string(e.Args)
		s.toolSeq++
		id := fmt.Sprintf("t%d", s.toolSeq)
		s.app.redraw(func() { s.app.appendToolRow(id, callID, name, args) })
	case agent.EventToolExecEnd:
		dur := time.Since(s.toolStart[e.ToolCallID])
		result, isErr, callID := string(e.Result), e.IsError, e.ToolCallID
		s.app.redraw(func() { s.app.finishToolRow(callID, result, isErr, dur) })
	case agent.EventAgentEnd:
		// The deferred redraw clears Busy and persists; nothing else to fold.
	}
	return nil
}

// onAssistantEvent folds a raw assistant stream event into the current row.
func (s *agentStream) onAssistantEvent(evt piai.AssistantMessageEvent) {
	switch e := evt.(type) {
	case piai.EventThinkingDelta:
		s.think.WriteString(e.Delta)
		th := s.think.String()
		s.app.redraw(func() { s.setRow(func(r *row) { r.thinkText = th }) })
	case piai.EventTextDelta:
		s.body.WriteString(e.Delta)
		txt := s.body.String()
		s.app.redraw(func() { s.setRow(func(r *row) { r.text = txt }) })
	}
}

// setRow edits the current assistant row on the UI thread.
func (s *agentStream) setRow(fn func(*row)) {
	for i := range s.app.thread.rows {
		if s.app.thread.rows[i].id != s.curID {
			continue
		}
		fn(&s.app.thread.rows[i])
		s.app.thread.list.ScrollToEnd()
		return
	}
}

// toolKind maps a tool name to its transcript card row kind.
func toolKind(name string) kind {
	switch name {
	case "bash":
		return rowCommand
	case "write_file":
		return rowDiff
	default:
		return rowTools
	}
}

// appendToolRow adds a running card row for a tool execution (UI thread).
func (a *app) appendToolRow(id, callID, name, args string) {
	t := &toolRun{callID: callID, name: name, args: args, state: muiagent.AgentRunning}
	if name == "bash" {
		var p toolParams
		_ = json.Unmarshal([]byte(args), &p)
		t.run = muiagent.CommandRun{Command: p.Command, Dir: a.ws.root, Running: true}
	}
	a.thread.rows = append(a.thread.rows, row{
		id: id, role: chat.MessageAssistant, kind: toolKind(name),
		at: time.Now(), tool: t,
	})
	a.thread.list.ScrollToEnd()
}

// finishToolRow fills a tool card's result (UI thread).
func (a *app) finishToolRow(callID, result string, isErr bool, dur time.Duration) {
	for i := range a.thread.rows {
		t := a.thread.rows[i].tool
		if t == nil || t.callID != callID {
			continue
		}
		t.state = muiagent.AgentDone
		t.dur = dur
		if isErr {
			t.errMsg = toolResultText(result)
		} else {
			t.result = result
		}
		switch t.name {
		case "bash":
			t.run.Running = false
			t.run.Duration = dur
			t.run.Output = toolResultText(result)
			if isErr {
				t.run.ExitCode = 1
			}
		case "write_file":
			var d struct {
				Path    string `json:"path"`
				Old     string `json:"old"`
				New     string `json:"new"`
				Details struct {
					Path string `json:"path"`
					Old  string `json:"old"`
					New  string `json:"new"`
				} `json:"details"`
			}
			if json.Unmarshal([]byte(result), &d) == nil {
				path, old, new := d.Details.Path, d.Details.Old, d.Details.New
				if path == "" {
					path, old, new = d.Path, d.Old, d.New
				}
				t.change = muiagent.FileChange{Path: path, Old: old, New: new}
			}
		}
		a.thread.list.ScrollToEnd()
		return
	}
}

// toolResultText extracts the first text block of a marshaled AgentToolResult.
func toolResultText(result string) string {
	var r struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal([]byte(result), &r) == nil && len(r.Content) > 0 {
		return r.Content[0].Text
	}
	return result
}

// streamOptions builds the streaming options from the app settings.
// Sampling (temperature / max tokens) is left to the provider's defaults —
// the Agent pane no longer exposes them.
func (a *app) streamOptions() piai.SimpleStreamOptions {
	s := piai.SimpleStreamOptions{
		StreamOptions: piai.StreamOptions{
			APIKey: strings.TrimSpace(a.llm.APIKey),
		},
	}
	if tl, ok := a.thinkingLevel(); ok {
		s.Reasoning = tl
	}
	return s
}

// streamError records a stream failure on the assistant row and in settings.
func (a *app) streamError(idx int, err error) {
	msg := err.Error()
	set := func() {
		a.llm.LastError = msg
		if len(a.thread.rows) > idx {
			r := &a.thread.rows[idx]
			r.text = "⚠ " + msg
			r.kind = rowPlain
		}
		a.thread.list.ScrollToEnd()
		a.persistSessions()
	}
	if a.redraw == nil {
		set()
		return
	}
	a.redraw(set)
}

// streamErrorRow is streamError keyed by row id (the agent loop tracks rows
// by id, so its failures land by id too).
func (a *app) streamErrorRow(id string, err error) {
	msg := err.Error()
	set := func() {
		a.llm.LastError = msg
		for i := range a.thread.rows {
			if a.thread.rows[i].id != id {
				continue
			}
			a.thread.rows[i].text = "⚠ " + msg
			a.thread.rows[i].kind = rowPlain
			break
		}
		a.thread.list.ScrollToEnd()
		a.persistSessions()
	}
	if a.redraw == nil {
		set()
		return
	}
	a.redraw(set)
}

// finishStream marks the assistant row complete off-window so headless tests
// don't block. Real windows stream instead.
func (a *app) finishStream(idx int) {
	if a.redraw == nil {
		if len(a.thread.rows) > idx && a.thread.rows[idx].text == "" {
			a.thread.rows[idx].text = "(no LLM backend off-window)"
		}
		a.llm.Busy = false
		a.clearCancel()
		return
	}
	a.redraw(func() { a.llm.Busy = false })
	a.clearCancel()
}

// setCancel / clearCancel / cancel store the in-flight context so the Stop
// action can abort a streaming reply.
func (a *app) setCancel(fn context.CancelFunc) { a.cancelFn = fn }
func (a *app) clearCancel()                    { a.cancelFn = nil }
func (a *app) cancel() {
	if a.cancelFn != nil {
		a.cancelFn()
	}
}
