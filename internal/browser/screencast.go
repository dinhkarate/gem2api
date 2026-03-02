package browser

import (
	"context"
	"log"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

// processInputEvents reads from the session's InputCh and dispatches CDP input events.
// This runs until the session is closed or context is cancelled.
func (bm *BrowserManager) processInputEvents(session *LoginSession) {
	for {
		select {
		case ev, ok := <-session.InputCh:
			if !ok {
				return
			}
			if err := bm.dispatchInput(session, ev); err != nil {
				log.Printf("Login session %d: input dispatch error: %v", session.ProfileID, err)
			}
		case <-session.ctx.Done():
			return
		}
	}
}

// dispatchInput converts a client InputEvent to the appropriate CDP action.
func (bm *BrowserManager) dispatchInput(session *LoginSession, ev InputEvent) error {
	ctx := session.ctx

	switch ev.Type {
	case "click":
		btn := toMouseButton(ev.Button)
		count := ev.ClickCount
		if count == 0 {
			count = 1
		}
		return chromedp.Run(ctx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				p := input.DispatchMouseEvent(input.MousePressed, ev.X, ev.Y).
					WithButton(btn).
					WithClickCount(int64(count))
				if err := p.Do(ctx); err != nil {
					return err
				}
				r := input.DispatchMouseEvent(input.MouseReleased, ev.X, ev.Y).
					WithButton(btn).
					WithClickCount(int64(count))
				return r.Do(ctx)
			}),
		)

	case "mousedown":
		btn := toMouseButton(ev.Button)
		return chromedp.Run(ctx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				return input.DispatchMouseEvent(input.MousePressed, ev.X, ev.Y).
					WithButton(btn).Do(ctx)
			}),
		)

	case "mouseup":
		btn := toMouseButton(ev.Button)
		return chromedp.Run(ctx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				return input.DispatchMouseEvent(input.MouseReleased, ev.X, ev.Y).
					WithButton(btn).Do(ctx)
			}),
		)

	case "mousemove":
		return chromedp.Run(ctx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				return input.DispatchMouseEvent(input.MouseMoved, ev.X, ev.Y).Do(ctx)
			}),
		)

	case "type":
		if ev.Text != "" {
			return chromedp.Run(ctx,
				chromedp.ActionFunc(func(ctx context.Context) error {
					return input.InsertText(ev.Text).Do(ctx)
				}),
			)
		}
		return nil

	case "keydown":
		return chromedp.Run(ctx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				return input.DispatchKeyEvent(input.KeyDown).
					WithKey(ev.Key).
					WithCode(ev.Code).
					Do(ctx)
			}),
		)

	case "keyup":
		return chromedp.Run(ctx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				return input.DispatchKeyEvent(input.KeyUp).
					WithKey(ev.Key).
					WithCode(ev.Code).
					Do(ctx)
			}),
		)

	case "scroll":
		return chromedp.Run(ctx,
			chromedp.ActionFunc(func(ctx context.Context) error {
				return input.DispatchMouseEvent(input.MouseWheel, ev.X, ev.Y).
					WithDeltaX(ev.DeltaX).
					WithDeltaY(ev.DeltaY).
					Do(ctx)
			}),
		)

	case "navigate":
		if ev.URL != "" {
			return chromedp.Run(ctx, chromedp.Navigate(ev.URL))
		}
		return nil

	default:
		log.Printf("Unknown input event type: %s", ev.Type)
		return nil
	}
}

func toMouseButton(btn string) input.MouseButton {
	switch btn {
	case "right":
		return input.Right
	case "middle":
		return input.Middle
	default:
		return input.Left
	}
}
