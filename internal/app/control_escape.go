package app

import (
	"context"
	"time"
)

// watchControlEscape samples only Escape during one explicitly approved action.
// The first sample is synchronous so an already-held key prevents dispatch.
// The returned cleanup joins the watcher before the broker permits another action.
func watchControlEscape(ctx context.Context, pressed func() bool, stop func()) func() {
	if pressed == nil || ctx.Err() != nil {
		return func() {}
	}
	watchCtx, cancel := context.WithCancel(ctx)
	if pressed() {
		if watchCtx.Err() == nil {
			stop()
		}
		cancel()
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(30 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				if watchCtx.Err() != nil {
					return
				}
				down := pressed()
				// A sample may complete after cleanup started. It cannot stop
				// an action after its watcher lifetime has ended.
				if watchCtx.Err() != nil {
					return
				}
				if down {
					stop()
					return
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
