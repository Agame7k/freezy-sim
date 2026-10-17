// Package safe keeps long-running simulator loops alive: a panic is recovered, its stack is written to run/logs, and
// the event log gets an error instead of the whole simulator (and the arena it supervises) going down.
package safe

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
)

// Dir is where panic stack dumps are written.
var Dir = filepath.Join("run", "logs")

var (
	mu       sync.Mutex
	lastDump = map[string]time.Time{}
)

// Recover must be deferred directly; it reports and swallows a panic. It returns nothing so it can be deferred.
func Recover(hub *bus.Hub, source string) {
	if value := recover(); value != nil {
		Report(hub, source, value, debug.Stack())
	}
}

// Report logs a recovered panic and writes its stack (at most one file per source every 30 s, so a panic in a 120 Hz
// loop cannot fill the disk).
func Report(hub *bus.Hub, source string, value any, stack []byte) {
	mu.Lock()
	dump := time.Since(lastDump[source]) > 30*time.Second
	if dump {
		lastDump[source] = time.Now()
	}
	mu.Unlock()
	path := ""
	if dump {
		path = filepath.Join(Dir, fmt.Sprintf("panic-%s-%s.txt", source, time.Now().Format("20060102-150405")))
		if err := os.MkdirAll(Dir, 0o755); err == nil {
			_ = os.WriteFile(path, []byte(fmt.Sprintf("panic in %s: %v\n\n%s", source, value, stack)), 0o644)
		}
	}
	if hub == nil {
		return
	}
	if path != "" {
		hub.Errorf(source, "Recovered from a crash: %v (stack written to %s)", value, path)
	} else {
		hub.Errorf(source, "Recovered from a crash: %v", value)
	}
}

// Go runs fn in a goroutine and restarts it (after a short pause) if it panics, so a bug in one subsystem does not
// silently stop it for the rest of the session.
func Go(hub *bus.Hub, source string, fn func()) {
	go func() {
		for {
			if finished := run(hub, source, fn); finished {
				return
			}
			time.Sleep(time.Second)
		}
	}()
}

func run(hub *bus.Hub, source string, fn func()) (finished bool) {
	defer func() {
		if value := recover(); value != nil {
			Report(hub, source, value, debug.Stack())
			finished = false
		}
	}()
	fn()
	return true
}
