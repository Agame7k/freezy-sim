package fieldio

import "testing"

// FuzzModbusHandle sends arbitrary PDUs to the PLC emulator: the arena (or anything else on the port) must never be
// able to crash it, and every reply must fit in a Modbus frame.
func FuzzModbusHandle(f *testing.F) {
	f.Add([]byte{fcReadCoils, 0, 0, 0, 8})
	f.Add([]byte{fcReadDiscreteInputs, 0, 0, 0, InputCount})
	f.Add([]byte{fcReadHoldingRegisters, 0, 0, 0, 2})
	f.Add([]byte{fcWriteSingleCoil, 0, 1, 0xff, 0})
	f.Add([]byte{fcWriteMultipleCoils, 0, 0, 0, 9, 2, 0xff, 1})
	f.Add([]byte{fcWriteMultipleRegisters, 0, 0, 0, 1, 2, 0, 7})
	f.Add([]byte{fcWriteSingleRegister, 0, 0, 0, 7})
	f.Fuzz(func(t *testing.T, pdu []byte) {
		if len(pdu) == 0 || len(pdu) > 259 {
			return // serve rejects these lengths before handle
		}
		server := NewModbusServer(newField(), "127.0.0.1", 0)
		response := server.handle(pdu)
		if len(response) == 0 || len(response) > 253 {
			t.Fatalf("response length %d for request % x", len(response), pdu)
		}
		_ = describeModbus(pdu, response)
	})
}
