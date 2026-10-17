package ds

import (
	"net"
	"testing"
	"time"
)

// FMS control packets arrive between the DS's ticks; the robot must see each mode change together with its enable
// state. At the end of AUTO, the "disabled, not auto" packet used to show up as a moment of enabled TELEOP (the new
// mode with the old enable), long enough for an auto-climbed robot to drop off the tower during the pause, after the
// referees had already counted it.
func TestCommandAppliesModeWithEnable(t *testing.T) {
	// 192.0.2.1 (TEST-NET) is never a local address: the UDP bind fails, so the test sends nothing anywhere.
	station := newStation(0, "192.0.2.1", 0, false, 254)
	station.followArena = false
	station.team = 254
	local, remote := net.Pipe()
	defer remote.Close()
	station.session = &tcpSession{conn: local, team: 254, done: make(chan struct{})}
	defer station.session.close()

	now := time.Now()
	deliver := func(control ControlPacket) {
		now = now.Add(5 * time.Millisecond)
		station.mu.Lock()
		station.storeControlLocked(control, now)
		station.mu.Unlock()
	}
	tick := func() {
		now = now.Add(tickPeriod)
		station.tick(now, tickPeriod.Seconds())
	}
	expect := func(when string, enabled, auto bool) {
		t.Helper()
		command := station.Command()
		if command.Enabled != enabled || command.Auto != auto {
			t.Fatalf("%s: robot sees enabled=%v auto=%v, want enabled=%v auto=%v", when, command.Enabled, command.Auto,
				enabled, auto)
		}
	}

	deliver(ControlPacket{Auto: true, Enabled: true, RemainingSeconds: 15})
	tick()
	expect("AUTO", true, true)

	deliver(ControlPacket{RemainingSeconds: 3}) // end of AUTO: disabled for the pause
	expect("pause packet in, not yet applied", true, true)
	tick()
	expect("pause", false, false)

	deliver(ControlPacket{Enabled: true, RemainingSeconds: 140}) // TELEOP
	expect("teleop packet in, not yet applied", false, false)
	tick()
	expect("TELEOP", true, false)
}
