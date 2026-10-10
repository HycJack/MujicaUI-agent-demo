package app

// approval.go bridges the engine's permission asks to the timeline: an
// ask becomes a permission card attached to the streaming reply, the
// card's buttons settle the engine's channel, and the composer's mode
// selector is the approval switch (spec §3.4). The channel is buffered
// (1) so the UI thread never blocks writing an answer the engine no
// longer reads (a late answer is not an answer).

import (
	"context"

	"crux-agent/internal/engine"

	"github.com/ZacharyZhang-NY/MujicaUI/icons"
	"github.com/egoist/mygo/ui"
)

// approvalReq is one pending permission ask on the timeline.
type approvalReq struct {
	rowID string                       // the assistant row the card attaches to
	req   engine.ApprovalRequest       // tool, summary, reason it asked
	ch    chan engine.ApprovalDecision // buffered (1); the engine reads or gives up
}

// awaitApproval is the engine's ApprovalFunc: it publishes the card on
// the streaming row through a.redraw, then waits for a click, a timeout
// or a stop. Headless (a.redraw nil, tests) nobody can click: the ask
// denies when its context fires.
func (a *app) awaitApproval(ctx context.Context, req engine.ApprovalRequest) engine.ApprovalDecision {
	if a.redraw == nil {
		<-ctx.Done()
		return engine.ApprovalDecision{Reason: engine.ApprovalReason(ctx)}
	}
	ar := &approvalReq{rowID: a.streamRowID, req: req, ch: make(chan engine.ApprovalDecision, 1)}
	a.redraw(func() {
		a.pending = ar
		a.thread.list.ScrollToEnd()
	})
	defer func() {
		a.redraw(func() {
			if a.pending == ar {
				a.pending = nil
			}
		})
	}()
	select {
	case d := <-ar.ch:
		return d
	case <-ctx.Done():
		return engine.ApprovalDecision{Reason: engine.ApprovalReason(ctx)}
	}
}

// settleApproval is what the card's buttons run: the decision goes to
// the engine (non-blocking: the engine may have timed out already) and
// the card clears. It is a no-op when the ask already settled.
func (a *app) settleApproval(d engine.ApprovalDecision) {
	if a.pending == nil {
		return
	}
	select {
	case a.pending.ch <- d:
	default: // the engine stopped waiting; drop the late answer
	}
	a.pending = nil
}

// clearPendingFor drops the card of a row whose reply just ended (the
// engine settled its ask, or the run was stopped).
func (a *app) clearPendingFor(rowID string) {
	if a.pending != nil && a.pending.rowID == rowID {
		a.pending = nil
	}
}

// approvalCard renders the pending ask as a permission card in the
// reply's bubble: what wants to run, the redacted one-line summary, and
// the choice. "Always allow" is allow-once — Crux does not persist
// per-tool rules (spec §3.4 simplification).
func (a *app) approvalCard(c *ui.Context) {
	req := a.pending
	k := tokens(c)
	ui.Column(c).Gap(8).Padding(12).Border(1, k.Warning).Radius(8).Background(k.Surface).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icons.Must("lock")).Size(14, 14).TextColor(k.Warning)
			ui.Text(c, req.req.Tool+" needs approval").FontSize(12).FontWeight(500).SingleLine()
			if req.req.Reason != "" {
				ui.Text(c, "· "+req.req.Reason).FontSize(11).TextColor(k.TextMuted).SingleLine().Grow(1)
			}
		})
		ui.Text(c, req.req.Summary).FontSize(12).TextColor(k.Text).MaxLines(2)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			approve := ui.Button(c, "Allow once")
			if approve.Clicked() {
				a.settleApproval(engine.ApprovalDecision{Approved: true})
			}
			always := ui.Button(c, "Always allow")
			if always.Clicked() {
				a.settleApproval(engine.ApprovalDecision{Approved: true})
			}
			deny := ui.Button(c, "Deny")
			if deny.Clicked() {
				a.settleApproval(engine.ApprovalDecision{Reason: "denied in Crux"})
			}
		})
	})
}
