package fieldio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/inspector"
	"github.com/Agame7k/freezy-sim/internal/safe"
)

// Modbus function codes.
const (
	fcReadCoils              = 0x01
	fcReadDiscreteInputs     = 0x02
	fcReadHoldingRegisters   = 0x03
	fcReadInputRegisters     = 0x04
	fcWriteSingleCoil        = 0x05
	fcWriteSingleRegister    = 0x06
	fcWriteMultipleCoils     = 0x0f
	fcWriteMultipleRegisters = 0x10

	exIllegalFunction = 0x01
	exIllegalAddress  = 0x02
	exIllegalValue    = 0x03

	heartbeatTimeout = time.Second
)

// ModbusStats is the published health of the PLC emulator.
type ModbusStats struct {
	Listening     bool      `json:"listening"`
	Address       string    `json:"address"`
	Error         string    `json:"error"`
	Clients       []string  `json:"clients"`
	Requests      int64     `json:"requests"`
	CoilWrites    int64     `json:"coilWrites"`
	LastRequest   time.Time `json:"lastRequest"`
	PollHz        float64   `json:"pollHz"`
	HeartbeatOk   bool      `json:"heartbeatOk"`
	ResponseLagMs int       `json:"responseLagMs"`
	Offline       bool      `json:"offline"`
}

// ModbusServer emulates the field PLC's Modbus-TCP slave.
type ModbusServer struct {
	field   *Field
	address string

	mu         sync.Mutex
	listener   net.Listener
	conns      map[net.Conn]string
	stats      ModbusStats
	lastCoilAt time.Time
	writeTimes []time.Time
	lagMs      int
	offline    bool
	stopped    bool
	onRequest  func(fc byte, request []byte)
}

func NewModbusServer(field *Field, bind string, port int) *ModbusServer {
	return &ModbusServer{
		field: field, address: net.JoinHostPort(bind, strconv.Itoa(port)), conns: make(map[net.Conn]string),
	}
}

// Start begins listening; it retries in the background if the port is busy.
func (server *ModbusServer) Start() {
	go func() {
		for {
			server.mu.Lock()
			if server.stopped {
				server.mu.Unlock()
				return
			}
			needListen := server.listener == nil && !server.offline
			server.mu.Unlock()
			if needListen {
				listener, err := net.Listen("tcp", server.address)
				server.mu.Lock()
				if err != nil {
					server.stats.Error = err.Error()
					server.stats.Listening = false
				} else {
					server.listener = listener
					server.stats.Error = ""
					server.stats.Listening = true
					go server.accept(listener)
				}
				server.mu.Unlock()
			}
			server.checkWatchdog()
			time.Sleep(250 * time.Millisecond)
		}
	}()
}

// Stop closes the server and every connection.
func (server *ModbusServer) Stop() {
	server.mu.Lock()
	server.stopped = true
	server.closeLocked()
	server.mu.Unlock()
}

// SetOffline simulates the PLC losing power/network: all connections drop and the port stops answering.
func (server *ModbusServer) SetOffline(offline bool) {
	server.mu.Lock()
	server.offline = offline
	if offline {
		server.closeLocked()
	}
	server.mu.Unlock()
}

// SetResponseLag delays every response (a lag over 1 s makes the arena's Modbus client time out).
func (server *ModbusServer) SetResponseLag(ms int) {
	server.mu.Lock()
	server.lagMs = ms
	server.mu.Unlock()
}

func (server *ModbusServer) closeLocked() {
	if server.listener != nil {
		_ = server.listener.Close()
		server.listener = nil
	}
	for conn := range server.conns {
		_ = conn.Close()
	}
	server.conns = make(map[net.Conn]string)
	server.stats.Listening = false
}

func (server *ModbusServer) accept(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		server.mu.Lock()
		server.conns[conn] = conn.RemoteAddr().String()
		server.mu.Unlock()
		go func() {
			defer safe.Recover(nil, "fieldio")
			server.serve(conn)
		}()
	}
}

func (server *ModbusServer) checkWatchdog() {
	server.mu.Lock()
	if server.offline {
		// Not the active transport (or simulated offline): the watchdog does not own the outputs.
		server.mu.Unlock()
		return
	}
	ok := !server.lastCoilAt.IsZero() && time.Since(server.lastCoilAt) < heartbeatTimeout
	server.stats.HeartbeatOk = ok
	server.mu.Unlock()
	server.field.setHeartbeat(ok)
}

// Stats returns the emulator's health.
func (server *ModbusServer) Stats() ModbusStats {
	server.mu.Lock()
	defer server.mu.Unlock()
	stats := server.stats
	stats.Address = server.address
	stats.Offline = server.offline
	stats.ResponseLagMs = server.lagMs
	stats.Clients = nil
	for _, address := range server.conns {
		stats.Clients = append(stats.Clients, address)
	}
	if len(server.writeTimes) > 1 {
		span := server.writeTimes[len(server.writeTimes)-1].Sub(server.writeTimes[0]).Seconds()
		if span > 0 {
			stats.PollHz = float64(len(server.writeTimes)-1) / span
		}
	}
	return stats
}

func (server *ModbusServer) serve(conn net.Conn) {
	defer func() {
		_ = conn.Close()
		server.mu.Lock()
		delete(server.conns, conn)
		server.mu.Unlock()
	}()
	header := make([]byte, 7)
	for {
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		length := int(binary.BigEndian.Uint16(header[4:6]))
		if length < 2 || length > 260 {
			return
		}
		pdu := make([]byte, length-1)
		if _, err := io.ReadFull(conn, pdu); err != nil {
			return
		}
		response := server.handle(pdu)

		server.mu.Lock()
		lag := server.lagMs
		server.mu.Unlock()
		if lag > 0 {
			time.Sleep(time.Duration(lag) * time.Millisecond)
		}

		frame := make([]byte, 7+len(response))
		copy(frame[0:4], header[0:4]) // transaction + protocol id
		binary.BigEndian.PutUint16(frame[4:6], uint16(len(response)+1))
		frame[6] = header[6] // unit id
		copy(frame[7:], response)
		inspector.Note("modbus", "in", conn.RemoteAddr().String(), describeModbus(pdu, response), append(header, pdu...))
		if _, err := conn.Write(frame); err != nil {
			return
		}
	}
}

var errBadRequest = errors.New("bad request")

// describeModbus is a one-line reading of a Modbus request and its response for the protocol inspector.
func describeModbus(pdu, response []byte) string {
	if len(pdu) < 5 {
		return fmt.Sprintf("FC%d (short request)", pdu[0])
	}
	start := binary.BigEndian.Uint16(pdu[1:3])
	count := binary.BigEndian.Uint16(pdu[3:5])
	names := map[byte]string{1: "read coils", 2: "read inputs", 3: "read registers", 4: "read input registers",
		5: "write coil", 6: "write register", 15: "write coils", 16: "write registers"}
	text := fmt.Sprintf("FC%d %s @%d ×%d", pdu[0], names[pdu[0]], start, count)
	if len(response) > 0 && response[0]&0x80 != 0 {
		code := byte(0)
		if len(response) > 1 {
			code = response[1]
		}
		text += fmt.Sprintf(" → exception %d", code)
	}
	return text
}

func exception(fc, code byte) []byte {
	return []byte{fc | 0x80, code}
}

// handle executes one Modbus PDU against the field model and returns the response PDU.
func (server *ModbusServer) handle(pdu []byte) []byte {
	fc := pdu[0]
	now := time.Now()
	server.mu.Lock()
	server.stats.Requests++
	server.stats.LastRequest = now
	hook := server.onRequest
	server.mu.Unlock()
	if hook != nil {
		hook(fc, pdu)
	}
	if len(pdu) < 5 && fc != fcWriteMultipleCoils && fc != fcWriteMultipleRegisters {
		return exception(fc, exIllegalValue)
	}
	switch fc {
	case fcReadCoils, fcReadDiscreteInputs:
		start := int(binary.BigEndian.Uint16(pdu[1:3]))
		count := int(binary.BigEndian.Uint16(pdu[3:5]))
		var bits []bool
		if fc == fcReadCoils {
			coils := server.field.Coils()
			bits = coils[:]
		} else {
			inputs := server.field.Inputs(now)
			bits = inputs[:]
		}
		if count < 1 || start+count > len(bits) {
			return exception(fc, exIllegalAddress)
		}
		packed := packBits(bits[start : start+count])
		return append([]byte{fc, byte(len(packed))}, packed...)
	case fcReadHoldingRegisters, fcReadInputRegisters:
		start := int(binary.BigEndian.Uint16(pdu[1:3]))
		count := int(binary.BigEndian.Uint16(pdu[3:5]))
		registers := server.field.Registers()
		if count < 1 || start+count > len(registers) {
			return exception(fc, exIllegalAddress)
		}
		response := []byte{fc, byte(2 * count)}
		for _, value := range registers[start : start+count] {
			response = binary.BigEndian.AppendUint16(response, value)
		}
		return response
	case fcWriteSingleCoil:
		address := int(binary.BigEndian.Uint16(pdu[1:3]))
		if address >= CoilCount {
			return exception(fc, exIllegalAddress)
		}
		coils := server.currentCoils()
		coils[address] = binary.BigEndian.Uint16(pdu[3:5]) == 0xff00
		server.applyCoils(coils, now)
		return pdu[:5]
	case fcWriteMultipleCoils:
		if len(pdu) < 6 {
			return exception(fc, exIllegalValue)
		}
		start := int(binary.BigEndian.Uint16(pdu[1:3]))
		count := int(binary.BigEndian.Uint16(pdu[3:5]))
		byteCount := int(pdu[5])
		if start+count > CoilCount || len(pdu) < 6+byteCount || byteCount*8 < count {
			return exception(fc, exIllegalAddress)
		}
		coils := server.currentCoils()
		for i := 0; i < count; i++ {
			coils[start+i] = pdu[6+i/8]&(1<<(i%8)) != 0
		}
		server.applyCoils(coils, now)
		return pdu[:5]
	case fcWriteSingleRegister:
		address := int(binary.BigEndian.Uint16(pdu[1:3]))
		if address >= RegisterCount {
			return exception(fc, exIllegalAddress)
		}
		server.writeRegister(address, binary.BigEndian.Uint16(pdu[3:5]))
		return pdu[:5]
	case fcWriteMultipleRegisters:
		if len(pdu) < 6 {
			return exception(fc, exIllegalValue)
		}
		start := int(binary.BigEndian.Uint16(pdu[1:3]))
		count := int(binary.BigEndian.Uint16(pdu[3:5]))
		if start+count > RegisterCount || len(pdu) < 6+2*count {
			return exception(fc, exIllegalAddress)
		}
		for i := 0; i < count; i++ {
			server.writeRegister(start+i, binary.BigEndian.Uint16(pdu[6+2*i:8+2*i]))
		}
		return pdu[:5]
	}
	return exception(fc, exIllegalFunction)
}

func (server *ModbusServer) currentCoils() [CoilCount]bool {
	server.field.mu.Lock()
	defer server.field.mu.Unlock()
	return server.field.coils
}

func (server *ModbusServer) applyCoils(coils [CoilCount]bool, now time.Time) {
	server.mu.Lock()
	server.stats.CoilWrites++
	server.lastCoilAt = now
	server.writeTimes = append(server.writeTimes, now)
	if len(server.writeTimes) > 30 {
		server.writeTimes = server.writeTimes[len(server.writeTimes)-30:]
	}
	server.mu.Unlock()
	server.field.SetCoils(coils, "modbus")
	server.checkWatchdog()
}

// writeRegister lets a Modbus master overwrite hub counters (fieldIoConnection is read-only status).
func (server *ModbusServer) writeRegister(address int, value uint16) {
	if address >= RegRedHubCount1 {
		index := address - RegRedHubCount1
		server.field.SetHubCount(index/4, index%4, int(value))
	}
}

func packBits(bits []bool) []byte {
	packed := make([]byte, (len(bits)+7)/8)
	for i, bit := range bits {
		if bit {
			packed[i/8] |= 1 << (i % 8)
		}
	}
	return packed
}
