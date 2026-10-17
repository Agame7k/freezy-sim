// Package ds emulates FRC Driver Stations (and the robot comms behind them) talking to Freezy Arena.
//
// Wire format reference (from the arena's field/driver_station_connection.go):
//
//	TCP 1750, DS → FMS, first packet:
//	  legacy NI DS: [0 3 24 teamHi teamLo]
//	  new DS:       [0 len 30 udpPortHi udpPortLo flags teamLen ascii...]   (len = 5 + teamLen)
//	TCP FMS → DS: 25/31 station assignment, 20 event name, 28 game data (legacy only)
//	TCP DS → FMS: 29 keepalive, 22 DS log (status byte at absolute offset 8)
//	UDP DS → FMS :1160: [seqHi seqLo ver status teamHi teamLo battInt battFrac] + tags; tag 1 = lost/trip time
//	UDP FMS → DS: 22-byte control packet (+ tag 32 game data for the new DS)
package ds

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// TCP packet types.
const (
	TcpEventName          = 20
	TcpDsLog              = 22
	TcpLegacyHandshake    = 24
	TcpLegacyAssignment   = 25
	TcpGameData           = 28
	TcpKeepalive          = 29
	TcpNewHandshake       = 30
	TcpNewStationAssigned = 31
)

// Station assignment status codes.
const (
	AssignOk           = 0
	AssignWrongStation = 1
	AssignNotInMatch   = 2
	AssignInvalidTeam  = 3
)

// Status packet bits the arena reads from byte 3 of the UDP status packet.
const (
	StatusRioLinked   = 0x08
	StatusRadioLinked = 0x10
	StatusRobotLinked = 0x20
)

// Control byte bits sent by the arena.
const (
	ControlAuto    = 0x02
	ControlEnabled = 0x04
	ControlAStop   = 0x40
	ControlEStop   = 0x80
)

var stationNames = []string{"R1", "R2", "R3", "B1", "B2", "B3"}

// StationName converts the arena's 0-5 station index to a name.
func StationName(index int) string {
	if index >= 0 && index < len(stationNames) {
		return stationNames[index]
	}
	return "?"
}

// EncodeLegacyHandshake builds the NI DS TCP handshake.
func EncodeLegacyHandshake(team int) []byte {
	return []byte{0, 3, TcpLegacyHandshake, byte(team >> 8), byte(team)}
}

// EncodeNewHandshake builds the new-DS TCP handshake, which tells the FMS which UDP port to send control packets to.
func EncodeNewHandshake(team int, udpPort int, flags byte) []byte {
	teamText := strconv.Itoa(team)
	packet := []byte{0, byte(5 + len(teamText)), TcpNewHandshake, byte(udpPort >> 8), byte(udpPort), flags,
		byte(len(teamText))}
	return append(packet, teamText...)
}

// EncodeKeepalive builds a TCP keepalive.
func EncodeKeepalive() []byte {
	return []byte{0, 1, TcpKeepalive}
}

// DsLogMode values for the DS-reported mode in a TCP DS log packet.
const (
	DsModeTeleop   = 0x20
	DsModeAuto     = 0x10
	DsModeDisabled = 0x08
)

// EncodeDsLog builds a TCP DS log packet; the arena reads the status byte at absolute offset 8.
func EncodeDsLog(tripMs, lost int, battery float64, cpuPercent int, status byte) []byte {
	volts := batteryBytes(battery)
	return []byte{0, 7, TcpDsLog, clampByte(tripMs), clampByte(lost), volts[0], volts[1], clampByte(cpuPercent), status}
}

// StatusPacket is what the DS reports to the FMS over UDP.
type StatusPacket struct {
	Sequence    uint16
	Team        int
	RioLinked   bool
	RadioLinked bool
	RobotLinked bool
	Enabled     bool
	Auto        bool
	Battery     float64
	LostPackets int
	TripTimeMs  int
}

// EncodeStatus serializes a DS → FMS UDP status packet.
func EncodeStatus(status StatusPacket) []byte {
	packet := make([]byte, 8, 16)
	binary.BigEndian.PutUint16(packet[0:2], status.Sequence)
	packet[2] = 0
	var flags byte
	if status.RioLinked {
		flags |= StatusRioLinked
	}
	if status.RadioLinked {
		flags |= StatusRadioLinked
	}
	if status.RobotLinked {
		flags |= StatusRobotLinked
	}
	if status.Enabled {
		flags |= 0x04
	}
	if status.Auto {
		flags |= 0x02
	}
	packet[3] = flags
	packet[4] = byte(status.Team >> 8)
	packet[5] = byte(status.Team)
	volts := batteryBytes(status.Battery)
	packet[6], packet[7] = volts[0], volts[1]
	// Tag 1: lost packets (2 bytes), two reserved bytes, trip time (1 byte).
	lost := status.LostPackets
	if lost > 0xffff {
		lost = 0xffff
	}
	packet = append(packet, 6, 1, byte(lost>>8), byte(lost), 0, 0, clampByte(status.TripTimeMs))
	return packet
}

// DecodeStatus parses a status packet the same way the arena does (used by tests and the packet inspector).
func DecodeStatus(data []byte) (StatusPacket, error) {
	if len(data) < 8 {
		return StatusPacket{}, fmt.Errorf("status packet too short: %d", len(data))
	}
	status := StatusPacket{
		Sequence:    binary.BigEndian.Uint16(data[0:2]),
		Team:        int(data[4])<<8 + int(data[5]),
		RioLinked:   data[3]&StatusRioLinked != 0,
		RadioLinked: data[3]&StatusRadioLinked != 0,
		RobotLinked: data[3]&StatusRobotLinked != 0,
		Enabled:     data[3]&0x04 != 0,
		Auto:        data[3]&0x02 != 0,
		Battery:     float64(data[6]) + float64(data[7])/256,
	}
	for index := 8; index < len(data); {
		length := int(data[index])
		index++
		if length == 0 {
			continue
		}
		if index+length > len(data) {
			break
		}
		if data[index] == 1 && length == 6 {
			status.LostPackets = int(data[index+1])<<8 + int(data[index+2])
			status.TripTimeMs = int(data[index+5])
		}
		index += length
	}
	return status, nil
}

// ControlPacket is the FMS → DS UDP control packet.
type ControlPacket struct {
	Sequence         uint16    `json:"sequence"`
	Auto             bool      `json:"auto"`
	Enabled          bool      `json:"enabled"`
	AStop            bool      `json:"aStop"`
	EStop            bool      `json:"eStop"`
	Station          string    `json:"station"`
	MatchType        int       `json:"matchType"`
	MatchNumber      int       `json:"matchNumber"`
	Repeat           int       `json:"repeat"`
	FmsTime          time.Time `json:"fmsTime"`
	RemainingSeconds int       `json:"remainingSeconds"`
	GameData         string    `json:"gameData"`
	HasGameDataTag   bool      `json:"hasGameDataTag"`
	Raw              []byte    `json:"-"`
}

var errShortControl = errors.New("control packet shorter than 22 bytes")

// DecodeControl parses an FMS control packet.
func DecodeControl(data []byte) (ControlPacket, error) {
	if len(data) < 22 {
		return ControlPacket{}, errShortControl
	}
	control := ControlPacket{
		Sequence:         binary.BigEndian.Uint16(data[0:2]),
		Auto:             data[3]&ControlAuto != 0,
		Enabled:          data[3]&ControlEnabled != 0,
		AStop:            data[3]&ControlAStop != 0,
		EStop:            data[3]&ControlEStop != 0,
		Station:          StationName(int(data[5])),
		MatchType:        int(data[6]),
		MatchNumber:      int(data[7])<<8 + int(data[8]),
		Repeat:           int(data[9]),
		RemainingSeconds: int(data[20])<<8 + int(data[21]),
		Raw:              append([]byte(nil), data...),
	}
	micros := int(binary.BigEndian.Uint32(data[10:14]))
	control.FmsTime = time.Date(int(data[19])+1900, time.Month(data[18]), int(data[17]), int(data[16]), int(data[15]),
		int(data[14]), micros*1000, time.Local)
	for index := 22; index < len(data); {
		length := int(data[index])
		index++
		if length == 0 {
			continue
		}
		if index+length > len(data) {
			break
		}
		if data[index] == 32 {
			control.GameData = string(data[index+1 : index+length])
			control.HasGameDataTag = true
		}
		index += length
	}
	return control, nil
}

// EncodeControl builds an FMS control packet (used by tests).
func EncodeControl(control ControlPacket, stationIndex int) []byte {
	packet := make([]byte, 22, 32)
	binary.BigEndian.PutUint16(packet[0:2], control.Sequence)
	if control.Auto {
		packet[3] |= ControlAuto
	}
	if control.Enabled {
		packet[3] |= ControlEnabled
	}
	if control.AStop {
		packet[3] |= ControlAStop
	}
	if control.EStop {
		packet[3] |= ControlEStop
	}
	packet[5] = byte(stationIndex)
	packet[6] = byte(control.MatchType)
	packet[7] = byte(control.MatchNumber >> 8)
	packet[8] = byte(control.MatchNumber)
	packet[9] = byte(control.Repeat)
	packet[20] = byte(control.RemainingSeconds >> 8)
	packet[21] = byte(control.RemainingSeconds)
	if control.GameData != "" {
		packet = append(packet, byte(len(control.GameData)+1), 32)
		packet = append(packet, control.GameData...)
	}
	return packet
}

func batteryBytes(volts float64) [2]byte {
	if volts < 0 {
		volts = 0
	}
	if volts > 255 {
		volts = 255
	}
	whole := int(volts)
	fraction := int((volts - float64(whole)) * 256)
	if fraction > 255 {
		fraction = 255
	}
	return [2]byte{byte(whole), byte(fraction)}
}

func clampByte(value int) byte {
	if value < 0 {
		return 0
	}
	if value > 255 {
		return 255
	}
	return byte(value)
}
