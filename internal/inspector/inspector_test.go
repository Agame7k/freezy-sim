package inspector

import (
	"fmt"
	"testing"
)

func TestRingKeepsNewestAndFilters(t *testing.T) {
	Clear()
	for i := 0; i < capacity+50; i++ {
		protocol := "ds"
		if i%2 == 1 {
			protocol = "modbus"
		}
		Note(protocol, "in", "R1", fmt.Sprintf("packet %d", i), []byte{1, 2, 3})
	}
	all := Select(Query{Limit: capacity})
	if len(all.Packets) != capacity {
		t.Fatalf("kept %d packets, want %d", len(all.Packets), capacity)
	}
	if first := all.Packets[0].Summary; first != "packet 50" {
		t.Fatalf("oldest kept is %q, want packet 50", first)
	}
	modbus := Select(Query{Protocols: []string{"modbus"}, Limit: 10})
	for _, p := range modbus.Packets {
		if p.Protocol != "modbus" {
			t.Fatalf("filter let %s through", p.Protocol)
		}
	}
	if last := modbus.Packets[len(modbus.Packets)-1].Summary; last != fmt.Sprintf("packet %d", capacity+49) {
		t.Fatalf("newest modbus is %q", last)
	}
	after := Select(Query{After: all.Last - 3})
	if len(after.Packets) != 3 {
		t.Fatalf("after: %d packets", len(after.Packets))
	}
	if text := Select(Query{Text: "PACKET 6049", Limit: 5}); len(text.Packets) != 1 {
		t.Fatalf("text search found %d", len(text.Packets))
	}
}
