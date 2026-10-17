// Package sacn receives the arena's E1.31 (sACN) hub LED packets and decodes them into per-hub pixel colors, using the
// same fixture map as the arena's led/fixture.go.
package sacn

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/config"
	"github.com/Agame7k/freezy-sim/internal/inspector"
	"github.com/Agame7k/freezy-sim/internal/safe"
)

const (
	dataOffset        = 126
	pixelsPerFixture  = 8
	fixturesPerHub    = 8
	pixelsPerHub      = pixelsPerFixture * fixturesPerHub
	channelsPerHub    = pixelsPerHub * 3
	twoUniverseWindow = 3 * time.Second
	publishPeriod     = 16 * time.Millisecond // ~60 Hz so the 3D LED bars animate smoothly
)

var acnIdentifier = []byte("ASC-E1.17\x00\x00\x00")

// Packet is a decoded E1.31 data packet.
type Packet struct {
	Universe   int
	Sequence   byte
	Priority   byte
	SourceName string
	Data       []byte // DMX channel data after the start code (up to 512 bytes)
}

// Decode parses an E1.31 data packet.
func Decode(packet []byte) (Packet, error) {
	if len(packet) < dataOffset {
		return Packet{}, errors.New("packet too short")
	}
	if !bytes.Equal(packet[4:16], acnIdentifier) {
		return Packet{}, errors.New("not an ACN packet")
	}
	if packet[21] != 0x04 || packet[43] != 0x02 || packet[117] != 0x02 {
		return Packet{}, errors.New("not an E1.31 data packet")
	}
	if packet[125] != 0 {
		return Packet{}, errors.New("non-zero DMX start code")
	}
	count := int(packet[123])<<8 + int(packet[124]) - 1
	if count < 0 || dataOffset+count > len(packet) {
		count = len(packet) - dataOffset
	}
	return Packet{
		Universe:   int(packet[113])<<8 + int(packet[114]),
		Sequence:   packet[111],
		Priority:   packet[108],
		SourceName: string(bytes.TrimRight(packet[44:108], "\x00")),
		Data:       packet[dataOffset : dataOffset+count],
	}, nil
}

// Status is the published receiver health.
type Status struct {
	Listening    bool      `json:"listening"`
	Address      string    `json:"address"`
	Error        string    `json:"error"`
	Packets      int64     `json:"packets"`
	Rejected     int64     `json:"rejected"`
	SequenceGaps int64     `json:"sequenceGaps"`
	Universes    []int     `json:"universes"`
	Source       string    `json:"source"`
	LastPacket   time.Time `json:"lastPacket"`
	Mode         string    `json:"mode"`
	PacketHz     float64   `json:"packetHz"`
}

// Hubs is the published pixel state: hex RGB for 64 pixels per hub in fixture order (side1 bottom, side1 top, ...).
type Hubs struct {
	Red  string `json:"red"`
	Blue string `json:"blue"`
}

type Receiver struct {
	hub     *bus.Hub
	address string

	mu         sync.Mutex
	conn       *net.UDPConn
	status     Status
	universes  map[int][512]byte
	seenAt     map[int]time.Time
	lastSeq    map[int]byte
	packetTime []time.Time
	dirty      bool
}

func New(cfg *config.Config, hub *bus.Hub) *Receiver {
	return &Receiver{
		hub: hub, address: net.JoinHostPort(cfg.SACN.Bind, strconv.Itoa(cfg.SACN.Port)),
		universes: map[int][512]byte{}, seenAt: map[int]time.Time{}, lastSeq: map[int]byte{},
	}
}

// ContributeSettings points the arena's LED controller at the receiver.
func (receiver *Receiver) ContributeSettings(settings map[string]string) {
	host, _, _ := net.SplitHostPort(receiver.address)
	settings["ledControllerAddress"] = host
}

// Run listens for packets and publishes decoded pixels.
func (receiver *Receiver) Run() {
	safe.Go(receiver.hub, "sacn", func() {
		for {
			conn, err := net.ListenUDP("udp4", mustResolve(receiver.address))
			if err != nil {
				receiver.mu.Lock()
				receiver.status.Error = err.Error()
				receiver.mu.Unlock()
				time.Sleep(2 * time.Second)
				continue
			}
			receiver.mu.Lock()
			receiver.conn = conn
			receiver.status.Listening = true
			receiver.status.Error = ""
			receiver.mu.Unlock()
			func() {
				defer conn.Close() // so a restart after a crash can bind the port again
				receiver.read(conn)
			}()
			return
		}
	})
	safe.Go(receiver.hub, "sacn", receiver.publishLoop)
}

func mustResolve(address string) *net.UDPAddr {
	resolved, err := net.ResolveUDPAddr("udp4", address)
	if err != nil {
		return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5568}
	}
	return resolved
}

func (receiver *Receiver) read(conn *net.UDPConn) {
	buffer := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			receiver.mu.Lock()
			receiver.status.Listening = false
			receiver.status.Error = err.Error()
			receiver.mu.Unlock()
			return
		}
		packet, err := Decode(buffer[:n])
		if err != nil {
			inspector.Note("sacn", "in", "hub lights", "rejected E1.31 packet: "+err.Error(), buffer[:n])
		} else {
			inspector.Note("sacn", "in", packet.SourceName, fmt.Sprintf("universe %d seq %d, %d slots", packet.Universe,
				packet.Sequence, len(packet.Data)), buffer[:n])
		}
		now := time.Now()
		receiver.mu.Lock()
		if err != nil {
			receiver.status.Rejected++
			receiver.mu.Unlock()
			continue
		}
		if last, ok := receiver.lastSeq[packet.Universe]; ok && packet.Sequence != last+1 {
			receiver.status.SequenceGaps++
		}
		receiver.lastSeq[packet.Universe] = packet.Sequence
		var data [512]byte
		copy(data[:], packet.Data)
		receiver.universes[packet.Universe] = data
		receiver.seenAt[packet.Universe] = now
		receiver.status.Packets++
		receiver.status.Source = packet.SourceName
		receiver.status.LastPacket = now
		receiver.packetTime = append(receiver.packetTime, now)
		if len(receiver.packetTime) > 60 {
			receiver.packetTime = receiver.packetTime[len(receiver.packetTime)-60:]
		}
		receiver.dirty = true
		receiver.mu.Unlock()
	}
}

// Pixels returns the decoded red and blue hub pixel data (RGB triples in fixture order).
func (receiver *Receiver) Pixels() (red, blue [channelsPerHub]byte, mode string) {
	receiver.mu.Lock()
	defer receiver.mu.Unlock()
	return receiver.pixelsLocked(time.Now())
}

func (receiver *Receiver) pixelsLocked(now time.Time) (red, blue [channelsPerHub]byte, mode string) {
	universe1 := receiver.universes[1]
	copy(red[:], universe1[0:channelsPerHub])
	if seen, ok := receiver.seenAt[2]; ok && now.Sub(seen) < twoUniverseWindow {
		universe2 := receiver.universes[2]
		copy(blue[:], universe2[0:channelsPerHub])
		mode = "two"
	} else {
		copy(blue[:], universe1[channelsPerHub:2*channelsPerHub])
		mode = "single"
	}
	return red, blue, mode
}

func (receiver *Receiver) publishLoop() {
	ticker := time.NewTicker(publishPeriod)
	defer ticker.Stop()
	statusCounter := 0
	for now := range ticker.C {
		receiver.mu.Lock()
		dirty := receiver.dirty
		receiver.dirty = false
		red, blue, mode := receiver.pixelsLocked(now)
		status := receiver.status
		status.Address = receiver.address
		status.Mode = mode
		status.Universes = nil
		for universe, seen := range receiver.seenAt {
			if now.Sub(seen) < twoUniverseWindow {
				status.Universes = append(status.Universes, universe)
			}
		}
		if len(receiver.packetTime) > 1 {
			span := receiver.packetTime[len(receiver.packetTime)-1].Sub(receiver.packetTime[0]).Seconds()
			if span > 0 && now.Sub(receiver.packetTime[len(receiver.packetTime)-1]) < 2*time.Second {
				status.PacketHz = float64(len(receiver.packetTime)-1) / span
			}
		}
		receiver.mu.Unlock()
		if dirty {
			receiver.hub.Publish("sacn.hubs", Hubs{Red: hex.EncodeToString(red[:]), Blue: hex.EncodeToString(blue[:])})
		}
		statusCounter++
		if statusCounter%15 == 0 {
			receiver.hub.Publish("sacn.status", status)
		}
	}
}
