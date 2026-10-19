package ds

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
)

// The Driver Stations panel's "Follow arena teams" box shows the published followArena flag. It used to be hard-wired
// to ticked, so a config with ds.followArena off showed the opposite of what the stations did.
func TestFollowArenaIsPublished(t *testing.T) {
	manager := &Manager{hub: bus.New()}
	for i := range manager.Stations {
		// 192.0.2.1 (TEST-NET) is never a local address, so nothing is sent anywhere.
		manager.Stations[i] = newStation(i, "192.0.2.1", 0, false, 254)
		manager.Stations[i].followArena = i%2 == 0
	}
	manager.registerCommands()

	for i, station := range manager.Stations {
		if got, want := station.snapshot(time.Now()).FollowArena, i%2 == 0; got != want {
			t.Fatalf("%s: followArena published as %v, want %v", station.Name, got, want)
		}
	}

	for _, follow := range []bool{false, true} {
		args, _ := json.Marshal(map[string]bool{"follow": follow})
		if _, err := manager.hub.Dispatch("ds.follow", args); err != nil {
			t.Fatalf("ds.follow %v: %v", follow, err)
		}
		for _, station := range manager.Stations {
			if got := station.snapshot(time.Now()).FollowArena; got != follow {
				t.Fatalf("after ds.follow %v, %s publishes followArena=%v", follow, station.Name, got)
			}
		}
	}
}
