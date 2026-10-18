package ds

import (
	"net"
	"testing"
)

// FuzzDecode feeds arbitrary bytes to the packet decoders: whatever arrives on the FMS sockets, they must return an
// error rather than panic.
func FuzzDecode(f *testing.F) {
	f.Add(EncodeControl(ControlPacket{Enabled: true, GameData: "A"}, 1))
	f.Add(EncodeStatus(StatusPacket{Team: 254, Battery: 12.5}))
	f.Add(EncodeDsLog(5, 1, 12, 30, 0))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = DecodeControl(data)
		_, _ = DecodeStatus(data)
	})
}

// FuzzTcpPacket feeds arbitrary FMS TCP bodies to the station's packet handler.
func FuzzTcpPacket(f *testing.F) {
	f.Add([]byte{TcpNewStationAssigned, 1, AssignOk})
	f.Add([]byte{TcpEventName, 4, 'M', 'N', 'S', 'T'})
	f.Add([]byte{TcpGameData, 200, 'x'})
	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) == 0 {
			return // tcpReader skips empty bodies
		}
		station := newStation(0, "127.0.0.1", 1121, false, 254)
		conn, peer := net.Pipe()
		defer peer.Close()
		session := &tcpSession{conn: conn, done: make(chan struct{})}
		station.session = session
		station.handleTcpPacket(session, body)
	})
}
