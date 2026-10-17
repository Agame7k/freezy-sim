package sacn

import (
	"testing"
	"time"
)

// buildArenaPacket mirrors the arena's led.createBlankPacket + sendPacket.
func buildArenaPacket(universe int, sequence byte, data []byte) []byte {
	size := dataOffset + 512
	packet := make([]byte, size)
	packet[1] = 0x10
	copy(packet[4:16], acnIdentifier)
	rootLength := size - 16
	packet[16] = 0x70 | byte(rootLength>>8)
	packet[17] = byte(rootLength)
	packet[21] = 0x04
	copy(packet[22:], "Cheesy Arena")
	framingLength := size - 38
	packet[38] = 0x70 | byte(framingLength>>8)
	packet[39] = byte(framingLength)
	packet[43] = 0x02
	copy(packet[44:], "Cheesy Arena")
	packet[108] = 100
	packet[111] = sequence
	packet[113] = byte(universe >> 8)
	packet[114] = byte(universe)
	dmpLength := size - 115
	packet[115] = 0x70 | byte(dmpLength>>8)
	packet[116] = byte(dmpLength)
	packet[117] = 0x02
	packet[118] = 0xa1
	packet[122] = 0x01
	count := 513
	packet[123] = byte(count >> 8)
	packet[124] = byte(count)
	copy(packet[dataOffset:], data)
	return packet
}

func newTestReceiver() *Receiver {
	return &Receiver{universes: map[int][512]byte{}, seenAt: map[int]time.Time{}, lastSeq: map[int]byte{}}
}

func TestDecodeArenaPacket(t *testing.T) {
	data := make([]byte, 512)
	data[0] = 255
	packet, err := Decode(buildArenaPacket(1, 7, data))
	if err != nil {
		t.Fatal(err)
	}
	if packet.Universe != 1 || packet.Sequence != 7 || packet.SourceName != "Cheesy Arena" || len(packet.Data) != 512 ||
		packet.Data[0] != 255 {
		t.Fatalf("unexpected packet %+v", packet)
	}
	if _, err := Decode([]byte("garbage")); err == nil {
		t.Fatal("expected error for garbage")
	}
}

func TestSingleUniverseMapping(t *testing.T) {
	receiver := newTestReceiver()
	var data [512]byte
	data[0], data[1], data[2] = 255, 1, 2       // red: side 1 bottom, pixel 0 (DMX address 1)
	data[192], data[193], data[194] = 3, 4, 255 // blue: side 1 bottom, pixel 0 (DMX address 193)
	receiver.universes[1] = data
	receiver.seenAt[1] = time.Now()
	red, blue, mode := receiver.pixelsLocked(time.Now())
	if mode != "single" || red[0] != 255 || red[2] != 2 || blue[0] != 3 || blue[2] != 255 {
		t.Fatalf("mapping wrong: mode=%s red=%v blue=%v", mode, red[:3], blue[:3])
	}
}

func TestTwoUniverseMapping(t *testing.T) {
	receiver := newTestReceiver()
	var universe1, universe2 [512]byte
	universe1[0] = 200
	universe2[0], universe2[2] = 9, 250
	receiver.universes[1] = universe1
	receiver.universes[2] = universe2
	receiver.seenAt[1] = time.Now()
	receiver.seenAt[2] = time.Now()
	red, blue, mode := receiver.pixelsLocked(time.Now())
	if mode != "two" || red[0] != 200 || blue[0] != 9 || blue[2] != 250 {
		t.Fatalf("mapping wrong: mode=%s red=%v blue=%v", mode, red[:3], blue[:3])
	}
}
