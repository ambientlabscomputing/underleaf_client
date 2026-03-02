package agent

import (
	"log/slog"
	"runtime/debug"
)

// safeGo launches a goroutine with panic recovery. If the goroutine panics,
// the error is logged with a full stack trace but the process continues running.
// This is critical for an edge agent that must stay alive even when upstream
// services (server_api, mycelium_spine) become temporarily unavailable and
// cause unexpected conditions in background goroutines.
func safeGo(name string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("goroutine panic recovered",
					"goroutine", name,
					"panic", r,
					"stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}
