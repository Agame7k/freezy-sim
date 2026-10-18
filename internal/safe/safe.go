// Package safe keeps long-running simulator loops alive: a panic is recovered, its stack is written to run/logs, and
// the event log gets an error instead of the whole simulator (and the arena it supervises) going down.
package safe

import (
	"fmt"
	"log"
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
	mu         sync.Mutex
	lastDump   = map[string]time.Time{}
	suppressed = map[string]int{}
)

// Recover must be deferred directly; it reports and swallows a panic. It returns nothing so it can be deferred.
func Recover(hub *bus.Hub, source string) {
	if value := recover(); value != nil {
		Report(hub, source, value, debug.Stack())
	}
}

// Report logs a recovered panic and writes its stack. Each source reports at most once every 30 s (one stack file,
// one event, with a count of the repeats in between), so a panic in a 120 Hz loop cannot fill the disk or bury the
// event log.
func Report(hub *bus.Hub, source string, value any, stack []byte) {
	mu.Lock()
	report := time.Since(lastDump[source]) > 30*time.Second
	repeats := suppressed[source]
	if report {
		lastDump[source] = time.Now()
		suppressed[source] = 0
	} else {
		suppressed[source]++
	}
	mu.Unlock()
	if !report {
		return
	}
	path := filepath.Join(Dir, fmt.Sprintf("panic-%s-%s.txt", source, time.Now().Format("20060102-150405")))
	if err := os.MkdirAll(Dir, 0o755); err == nil {
		_ = os.WriteFile(path, []byte(fmt.Sprintf("panic in %s: %v\n\n%s", source, value, stack)), 0o644)
	}
	message := fmt.Sprintf("Recovered from a crash: %v (stack written to %s)", value, path)
	if repeats > 0 {
		message += fmt.Sprintf("; %d more crash(es) since the last report", repeats)
	}
	if hub == nil {
		log.Printf("[ERROR] %s: %s", source, message)
		return
	}
	hub.Errorf(source, "%s", message)
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

// Async runs fn once in a goroutine; a panic is reported and the goroutine ends (use Go for loops that must keep
// running, Async for one-shot work that must not be repeated).
func Async(hub *bus.Hub, source string, fn func()) {
	go func() {
		defer Recover(hub, source)
		fn()
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
