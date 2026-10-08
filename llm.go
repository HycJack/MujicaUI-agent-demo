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
	"fmt"
	"strings"
	"time"

	piai "github.com/HycJack/pi-ai-go"
	_ "github.com/HycJack/pi-ai-go/providers" // registers the built-in providers at init
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

// thinkOptions returns the thinking-level choices valid for the current model.
func (a *app) thinkOptions() []string {
	opts := []string{"none"}
	m, err := piai.GetModel(piai.KnownProvider(a.llm.Provider), a.llm.Model)
	if err == nil && m.Reasoning {
		opts = append(opts, "low", "medium", "high")
	}
	return opts
}

// historyMessages converts the thread's user/assistant text rows into pi-ai
// messages, skipping tool/demo/decorative rows so the LLM sees clean history.
func (a *app) historyMessages() []piai.Message {
	msgs := make([]piai.Message, 0, len(a.thread.rows))
	for i := range a.thread.rows {
		r := &a.thread.rows[i]
		if r.text == "" {
			continue
		}
		switch r.role {
		case chat.MessageUser:
			msgs = append(msgs, piai.UserMessage{Content: r.text, Timestamp: r.at})
		case chat.MessageAssistant:
			switch r.kind {
			case rowPlain, rowReasoned:
				msgs = append(msgs, piai.AssistantMessage{
					Content:   []piai.ContentBlock{piai.TextContent{Type: "text", Text: r.text}},
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
	userIdx := len(a.thread.rows)
	a.thread.rows = append(a.thread.rows,
		row{id: fmt.Sprintf("u%d", userIdx), role: chat.MessageUser, text: text, at: now})
	a.thread.rows = append(a.thread.rows,
		row{id: fmt.Sprintf("a%d", userIdx+1), role: chat.MessageAssistant, kind: rowReasoned, at: now.Add(time.Millisecond)})
	a.thread.draft = ""
	a.thread.list.ScrollToEnd()
	a.startStream(userIdx)
}

// startStream launches the pi-ai streaming request for the already-appended
// user row at userIdx. The assistant row sits directly after it.
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
			})
		}()
		model, err := a.resolveModel()
		if err != nil {
			a.streamError(assistantIdx, err)
			return
		}
		opts := a.streamOptions()
		msgs := a.historyMessages()
		stream, err := piai.StreamSimpleWithContext(ctx, model, piai.Context{
			SystemPrompt: a.llm.SystemPrompt,
			Messages:     msgs,
		}, opts)
		if err != nil {
			a.streamError(assistantIdx, err)
			return
		}
		var think, body strings.Builder
		_, err = stream.ForEach(ctx, func(evt piai.AssistantMessageEvent) error {
			switch e := evt.(type) {
			case piai.EventTextDelta:
				body.WriteString(e.Delta)
				// Capture the text value now: win.Update(fn) does not wait
				// for fn, so the closure must not read the builder later.
				txt := body.String()
				a.redraw(func() {
					if len(a.thread.rows) > assistantIdx {
						a.thread.rows[assistantIdx].text = txt
					}
					a.thread.list.ScrollToEnd()
				})
			case piai.EventThinkingDelta:
				think.WriteString(e.Delta)
			case piai.EventDone:
				txt, th := body.String(), think.String()
				a.redraw(func() {
					a.applyReply(assistantIdx, th, txt)
					a.thread.list.ScrollToEnd()
				})
			case piai.EventError:
				return fmt.Errorf("%s", e.Error)
			}
			return nil
		})
		if err != nil {
			a.streamError(assistantIdx, err)
		}
	}()
}

// applyReply writes the final assistant text and thinking back to the row,
// unless the user already stopped the stream.
func (a *app) applyReply(idx int, think, text string) {
	if len(a.thread.rows) <= idx {
		return
	}
	r := &a.thread.rows[idx]
	r.text = text
	r.thinkText = think
	a.llm.LastError = ""
}

// streamOptions builds the streaming options from the app settings.
func (a *app) streamOptions() piai.SimpleStreamOptions {
	mt := a.llm.MaxTokens
	s := piai.SimpleStreamOptions{
		StreamOptions: piai.StreamOptions{
			APIKey:    strings.TrimSpace(a.llm.APIKey),
			MaxTokens: &mt,
		},
	}
	if tl, ok := a.thinkingLevel(); ok {
		s.Reasoning = tl
	}
	if a.llm.Temperature > 0 {
		t := a.llm.Temperature
		s.Temperature = &t
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
