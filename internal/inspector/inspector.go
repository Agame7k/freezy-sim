// Package inspector keeps a ring buffer of the field-protocol traffic the simulator exchanges with the arena (driver
// station UDP/TCP, Modbus, sACN, the access point's HTTP API, the switch's telnet CLI, the SCC SSH shells, alternate
// IO), for the protocol inspector panel. Recording is cheap and always on; the panel pulls what it needs.
package inspector

import (
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
)

const (
	capacity  = 6000
	maxRawLen = 96 // bytes of each packet kept as hex
)

// Packet is one recorded exchange.
type Packet struct {
	Seq       int64     `json:"seq"`
	Time      time.Time `json:"time"`
	Protocol  string    `json:"protocol"`  // ds, modbus, sacn, ap, telnet, ssh, altio
	Direction string    `json:"direction"` // in (arena → sim) or out (sim → arena)
	Peer      string    `json:"peer"`      // station, device or address
	Summary   string    `json:"summary"`
	Hex       string    `json:"hex,omitempty"`
	Length    int       `json:"length"`
}

var (
	mu     sync.Mutex
	ring   = make([]Packet, 0, capacity)
	start  int // index of the oldest packet once the ring is full
	seq    int64
	counts = map[string]int64{}
)

// Note records one exchange. data may be nil (text protocols put the line in the summary).
func Note(protocol, direction, peer, summary string, data []byte) {
	packet := Packet{Time: time.Now(), Protocol: protocol, Direction: direction, Peer: peer, Summary: summary,
		Length: len(data)}
	if len(data) > 0 {
		raw := data
		if len(raw) > maxRawLen {
			raw = raw[:maxRawLen]
		}
		packet.Hex = hex.EncodeToString(raw)
	}
	mu.Lock()
	seq++
	packet.Seq = seq
	counts[protocol]++
	if len(ring) < capacity {
		ring = append(ring, packet)
	} else {
		ring[start] = packet
		start = (start + 1) % capacity
	}
	mu.Unlock()
}

// Query selects packets newer than After (by Seq), optionally filtered by protocol and a case-insensitive text match.
type Query struct {
	After     int64    `json:"after"`
	Protocols []string `json:"protocols"`
	Text      string   `json:"text"`
	Limit     int      `json:"limit"`
}

// Result is a query's packets (oldest first) and the running totals per protocol.
type Result struct {
	Packets []Packet         `json:"packets"`
	Last    int64            `json:"last"`
	Counts  map[string]int64 `json:"counts"`
}

// Select runs a query.
func Select(query Query) Result {
	if query.Limit <= 0 || query.Limit > capacity {
		query.Limit = 500
	}
	wanted := map[string]bool{}
	for _, p := range query.Protocols {
		wanted[p] = true
	}
	text := strings.ToLower(query.Text)
	mu.Lock()
	defer mu.Unlock()
	result := Result{Last: seq, Counts: map[string]int64{}}
	for protocol, count := range counts {
		result.Counts[protocol] = count
	}
	// Walk newest to oldest, then reverse, so the limit keeps the most recent matches.
	for k := len(ring) - 1; k >= 0 && len(result.Packets) < query.Limit; k-- {
		packet := ring[(start+k)%len(ring)]
		if packet.Seq <= query.After {
			break
		}
		if len(wanted) > 0 && !wanted[packet.Protocol] {
			continue
		}
		if text != "" && !strings.Contains(strings.ToLower(packet.Summary+" "+packet.Peer), text) {
			continue
		}
		result.Packets = append(result.Packets, packet)
	}
	for i, j := 0, len(result.Packets)-1; i < j; i, j = i+1, j-1 {
		result.Packets[i], result.Packets[j] = result.Packets[j], result.Packets[i]
	}
	return result
}

// Clear empties the buffer.
func Clear() {
	mu.Lock()
	ring = ring[:0]
	start = 0
	counts = map[string]int64{}
	mu.Unlock()
}

// Register adds the inspector's commands to the bus.
func Register(hub *bus.Hub) {
	hub.Handle("inspector.query", func(args []byte) (any, error) {
		var query Query
		if len(args) > 0 {
			if err := json.Unmarshal(args, &query); err != nil {
				return nil, err
			}
		}
		return Select(query), nil
	})
	hub.Handle("inspector.clear", func(_ []byte) (any, error) {
		Clear()
		return nil, nil
	})
}
