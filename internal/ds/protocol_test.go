package ds

import (
	"bytes"
	"testing"
)

func TestHandshakes(t *testing.T) {
	if got := EncodeLegacyHandshake(254); !bytes.Equal(got, []byte{0, 3, 24, 0, 254}) {
		t.Fatalf("legacy handshake = %v", got)
	}
	got := EncodeNewHandshake(1678, 11211, 0)
	want := []byte{0, 9, 30, 0x2b, 0xcb, 0, 4, '1', '6', '7', '8'}
	if !bytes.Equal(got, want) {
		t.Fatalf("new handshake = %v, want %v", got, want)
	}
	// The arena requires packet length == 5 + team number length and reads the team at [7:7+len].
	packetLength := int(got[0])<<8 + int(got[1])
	if packetLength != 5+int(got[6]) || len(got) != 2+packetLength {
		t.Fatalf("new handshake length fields inconsistent: %v", got)
	}
}

func TestStatusRoundTrip(t *testing.T) {
	cases := []StatusPacket{
		{Sequence: 1, Team: 254, RioLinked: true, RadioLinked: true, RobotLinked: true, Battery: 12.5, LostPackets: 3,
			TripTimeMs: 7},
		{Sequence: 65535, Team: 9999, Battery: 0},
		{Sequence: 42, Team: 118, RadioLinked: true, Battery: 6.75, LostPackets: 70000, TripTimeMs: 400},
	}
	for _, want := range cases {
		got, err := DecodeStatus(EncodeStatus(want))
		if err != nil {
			t.Fatal(err)
		}
		if want.LostPackets > 0xffff {
			want.LostPackets = 0xffff
		}
		if want.TripTimeMs > 255 {
			want.TripTimeMs = 255
		}
		if got.Team != want.Team || got.RioLinked != want.RioLinked || got.RadioLinked != want.RadioLinked ||
			got.RobotLinked != want.RobotLinked || got.LostPackets != want.LostPackets ||
			got.TripTimeMs != want.TripTimeMs || got.Sequence != want.Sequence {
			t.Errorf("round trip mismatch: got %+v want %+v", got, want)
		}
		if diff := got.Battery - want.Battery; diff > 0.01 || diff < -0.01 {
			t.Errorf("battery %f != %f", got.Battery, want.Battery)
		}
	}
}

func TestStatusBitsMatchArena(t *testing.T) {
	packet := EncodeStatus(StatusPacket{Team: 1, RioLinked: true})
	if packet[3]&0x08 == 0 || packet[3]&0x10 != 0 || packet[3]&0x20 != 0 {
		t.Fatalf("rio-only status byte = %08b", packet[3])
	}
	packet = EncodeStatus(StatusPacket{Team: 1, RobotLinked: true})
	if packet[3]&0x20 == 0 {
		t.Fatalf("robot status bit missing: %08b", packet[3])
	}
}

func TestControlDecode(t *testing.T) {
	control := ControlPacket{Sequence: 7, Auto: true, Enabled: true, AStop: true, MatchType: 2, MatchNumber: 300,
		Repeat: 1, RemainingSeconds: 135, GameData: "R"}
	decoded, err := DecodeControl(EncodeControl(control, 4))
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.Auto || !decoded.Enabled || !decoded.AStop || decoded.EStop || decoded.Station != "B2" ||
		decoded.MatchType != 2 || decoded.MatchNumber != 300 || decoded.RemainingSeconds != 135 ||
		decoded.GameData != "R" || !decoded.HasGameDataTag {
		t.Fatalf("decoded control mismatch: %+v", decoded)
	}
	if _, err := DecodeControl(make([]byte, 10)); err == nil {
		t.Fatal("expected short packet error")
	}
}

func TestDsLogStatusOffset(t *testing.T) {
	packet := EncodeDsLog(5, 1, 12.3, 30, DsModeAuto)
	if packet[2] != TcpDsLog || packet[8] != DsModeAuto {
		t.Fatalf("DS log packet = %v", packet)
	}
	if int(packet[0])<<8+int(packet[1]) != len(packet)-2 {
		t.Fatalf("DS log length field wrong: %v", packet)
	}
}
