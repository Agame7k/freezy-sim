package safe

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
)

func TestGoRestartsAfterPanic(t *testing.T) {
	Dir = t.TempDir()
	hub := bus.New()
	var runs atomic.Int32
	done := make(chan struct{})
	Go(hub, "test", func() {
		if runs.Add(1) == 1 {
			panic("boom")
		}
		close(done)
	})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("loop was not restarted after the panic")
	}
	found := false
	for _, event := range hub.Events() {
		if event.Level == "error" && strings.Contains(event.Msg, "boom") {
			found = true
		}
	}
	if !found {
		t.Error("the panic was not reported in the event log")
	}
}

func TestRecoverKeepsCallerRunning(t *testing.T) {
	Dir = t.TempDir()
	hub := bus.New()
	func() {
		defer Recover(hub, "test-recover")
		var m map[string]int
		m["x"] = 1 // nil map write panics
	}()
	if len(hub.Events()) == 0 {
		t.Error("expected the recovered panic to be logged")
	}
}

func TestDispatchRecoversHandlerPanic(t *testing.T) {
	hub := bus.New()
	hub.Handle("explode", func(_ []byte) (any, error) { panic("handler bug") })
	_, err := hub.Dispatch("explode", nil)
	if err == nil || !strings.Contains(err.Error(), "handler bug") {
		t.Fatalf("expected the panic as an error, got %v", err)
	}
}

func TestReportThrottlesRepeats(t *testing.T) {
	Dir = t.TempDir()
	hub := bus.New()
	for i := 0; i < 200; i++ {
		Report(hub, "test-throttle", "every tick", nil)
	}
	if count := len(hub.Events()); count != 1 {
		t.Fatalf("a repeating panic produced %d events, want 1", count)
	}
}

func TestAsyncDoesNotRestart(t *testing.T) {
	Dir = t.TempDir()
	hub := bus.New()
	var runs atomic.Int32
	Async(hub, "test-async", func() {
		runs.Add(1)
		panic("once")
	})
	time.Sleep(1500 * time.Millisecond)
	if got := runs.Load(); got != 1 {
		t.Fatalf("Async ran %d times, want 1", got)
	}
}
