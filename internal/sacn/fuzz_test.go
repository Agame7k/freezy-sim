package sacn

import "testing"

// FuzzDecode feeds arbitrary datagrams to the E1.31 decoder (anything on the LAN can send to the sACN port).
func FuzzDecode(f *testing.F) {
	f.Add(buildArenaPacket(1, 1, make([]byte, 512)))
	f.Add(make([]byte, dataOffset))
	f.Fuzz(func(t *testing.T, packet []byte) {
		decoded, err := Decode(packet)
		if err == nil && len(decoded.Data) > 512 && len(packet) <= dataOffset+512 {
			t.Fatalf("decoded %d DMX slots", len(decoded.Data))
		}
	})
}
