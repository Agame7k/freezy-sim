package fieldio

import (
	"testing"
	"time"

	"github.com/goburrow/modbus"

	"github.com/Agame7k/freezy-sim/internal/arena"
)

// TestModbusAgainstArenaClient drives the emulator with the same Modbus client library and calls the arena uses.
func TestModbusAgainstArenaClient(t *testing.T) {
	field := newField()
	field.ethernet = func(station int) bool { return station == 1 }
	server := NewModbusServer(field, "127.0.0.1", 15020)
	server.Start()
	defer server.Stop()

	handler := modbus.NewTCPClientHandler("127.0.0.1:15020")
	handler.Timeout = time.Second
	handler.SlaveId = 0xFF
	var err error
	for i := 0; i < 20; i++ {
		if err = handler.Connect(); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer handler.Close()
	client := modbus.NewClient(handler)

	// Field E-stop pressed, R2 A-stop pressed, FTA ready on.
	field.FieldEStop = true
	field.TeamAStop[1] = true
	field.FtaReady = true
	inputs, err := client.ReadDiscreteInputs(0, InputCount)
	if err != nil {
		t.Fatal(err)
	}
	bit := func(i int) bool { return inputs[i/8]&(1<<(i%8)) != 0 }
	if bit(InFieldEStop) || !bit(InRed1EStop) || bit(InRed2AStop) || !bit(InRed2EStop) || !bit(InFtaReady) {
		t.Fatalf("unexpected inputs %08b", inputs)
	}
	if bit(InRedConnected1) || !bit(InRedConnected2) {
		t.Fatalf("ethernet inputs wrong %08b", inputs)
	}

	field.ScoreFuel(0, 2)
	field.ScoreFuel(0, 2)
	field.ScoreFuel(1, 0)
	registers, err := client.ReadHoldingRegisters(0, RegisterCount)
	if err != nil {
		t.Fatal(err)
	}
	word := func(i int) int { return int(registers[2*i])<<8 + int(registers[2*i+1]) }
	if word(RegFieldIoConnection) != 0x0f || word(RegRedHubTotal) != 2 || word(RegBlueHubTotal) != 1 ||
		word(RegRedHubCount3) != 2 || word(RegBlueHubCount1) != 1 {
		t.Fatalf("unexpected registers %v", registers)
	}

	// Write coils the way the arena does (heartbeat + match reset + red hub motor).
	coils := make([]byte, 2)
	coils[0] = 1<<CoilHeartbeat | 1<<CoilMatchReset
	coils[1] = 1 << (CoilRedHubMotor - 8)
	if _, err := client.WriteMultipleCoils(0, CoilCount, coils); err != nil {
		t.Fatal(err)
	}
	got := field.Coils()
	if !got[CoilHeartbeat] || !got[CoilMatchReset] || !got[CoilRedHubMotor] || got[CoilBlueHubMotor] {
		t.Fatalf("coils not applied: %v", got)
	}
	if registers := field.Registers(); registers[RegRedHubTotal] != 0 {
		t.Fatalf("match reset should clear hub counts, got %v", registers)
	}

	// The winner coils (13/14) come in the same 15-coil write, and an older arena's 13-coil write still works.
	coils = []byte{1 << CoilHeartbeat, 1 << (CoilBlueWins - 8)}
	if _, err := client.WriteMultipleCoils(0, CoilCount, coils); err != nil {
		t.Fatal(err)
	}
	if got := field.Coils(); !got[CoilBlueWins] || got[CoilRedWins] {
		t.Fatalf("winner coils not applied: %v", got)
	}
	if show := field.WinnerShow(); show == nil || !show.Blue || show.Red {
		t.Fatalf("blueWins should start a blue winner show, got %+v", show)
	}
	if _, err := client.WriteMultipleCoils(0, LegacyCoilCount, []byte{1 << CoilHeartbeat, 1 << (CoilRedHubLight - 8)}); err != nil {
		t.Fatalf("legacy 13-coil write rejected: %v", err)
	}
	if got := field.Coils(); !got[CoilRedHubLight] {
		t.Fatalf("legacy coils not applied: %v", got)
	}
	if _, err := client.WriteMultipleCoils(0, CoilCount+1, []byte{0, 0, 0}); err == nil {
		t.Fatal("writing past the coil map should be an exception")
	}

	// Unknown function codes get an exception, not a hang.
	if response := server.handle([]byte{0x2b, 0, 0, 0, 0}); response[0] != 0x2b|0x80 {
		t.Fatalf("expected exception, got %v", response)
	}
}

func TestWinnerShow(t *testing.T) {
	field := newField()
	start := time.Now()
	field.mu.Lock()
	field.watchWinnerCoilsLocked([CoilCount]bool{CoilRedWins: true}, start)
	show := field.winnerShowLocked(start.Add(time.Second))
	field.mu.Unlock()
	if show == nil || !show.Red || show.Blue || show.RevealMs != DefaultRevealDelay.Milliseconds() {
		t.Fatalf("red win should start a red show revealed after the default delay, got %+v", show)
	}
	// The display mode arrives just after the coil (it left the logo screen): the same show is retimed.
	field.SetRevealDelay(arena.ScoreRevealDelay("logo"))
	field.mu.Lock()
	reveal := field.winnerShow.Reveal
	// The arena drops the pulse after 10 s; the PLC's sequence keeps running to its end.
	field.watchWinnerCoilsLocked([CoilCount]bool{}, start.Add(10*time.Second))
	running := field.winnerShowLocked(start.Add(reveal + WinnerShowAfterReveal - time.Second))
	ended := field.winnerShowLocked(start.Add(reveal + WinnerShowAfterReveal))
	// A tie pulses both coils: one show in both colors.
	later := start.Add(30 * time.Second)
	field.watchWinnerCoilsLocked([CoilCount]bool{CoilRedWins: true, CoilBlueWins: true}, later)
	tie := field.winnerShowLocked(later.Add(time.Second))
	field.mu.Unlock()
	if reveal != arena.ScoreRevealDelay("logo") || reveal >= DefaultRevealDelay {
		t.Fatalf("a delay set right after the coil should retime the show, reveal %v", reveal)
	}
	if running == nil || ended != nil || reveal+WinnerShowAfterReveal <= 10*time.Second {
		t.Fatalf("show should outlive the 10 s pulse and end %v after the reveal: running=%+v ended=%+v",
			WinnerShowAfterReveal, running, ended)
	}
	if tie == nil || !tie.Red || !tie.Blue || tie.Id != show.Id+1 || tie.RevealMs != reveal.Milliseconds() {
		t.Fatalf("tie should start a new two-color show with the latest delay, got %+v", tie)
	}
	field.MatchStarted(true)
	if field.WinnerShow() != nil {
		t.Fatal("the match start should end the show")
	}
}

func TestHeartbeatWatchdog(t *testing.T) {
	field := newField()
	field.SetCoils([CoilCount]bool{CoilRedHubLight: true}, "test")
	field.setHeartbeat(false)
	if field.Coils()[CoilRedHubLight] {
		t.Fatal("outputs must drop when the heartbeat watchdog trips")
	}
}
