package ds

import (
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/inspector"
)

// describeControl is a one-line reading of an FMS control packet for the protocol inspector.
func describeControl(control ControlPacket) string {
	mode := "disabled"
	if control.Enabled {
		mode = "enabled"
	}
	period := "teleop"
	if control.Auto {
		period = "auto"
	}
	text := fmt.Sprintf("control #%d %s %s %s %ds left", control.Sequence, control.Station, period, mode,
		control.RemainingSeconds)
	if control.EStop {
		text += " E-STOP"
	}
	if control.AStop {
		text += " A-STOP"
	}
	if control.GameData != "" {
		text += " game data " + control.GameData
	}
	return text
}

const (
	tickPeriod          = 20 * time.Millisecond
	keepalivePeriod     = time.Second
	dsLogPeriod         = 500 * time.Millisecond
	fmsTimeout          = 1500 * time.Millisecond
	reconnectDelay      = time.Second
	rejectedRetryDelay  = 2 * time.Second
	lossOutageTicks     = 25 // ~500 ms of consecutive lost packets drops robot comms
	maxConsoleMessages  = 40
	legacyUdpPort       = 1121
	legacyLiteUdpPort   = 1120
	arenaTcpPort        = 1750
	arenaUdpStatusPort  = 1160
	controlRateWindowMs = 3000
)

// Settings are the user-controlled knobs of one driver station.
type Settings struct {
	DsOn         bool    `json:"dsOn"`
	Ethernet     bool    `json:"ethernet"`
	TeamOverride int     `json:"teamOverride"`
	Legacy       bool    `json:"legacy"`
	LegacyLite   bool    `json:"legacyLite"`
	LossPct      float64 `json:"lossPct"`
	LatencyMs    int     `json:"latencyMs"`
	JitterMs     int     `json:"jitterMs"`
	AutoClearES  bool    `json:"autoClearEStop"`
}

// ConsoleMessage is a line in the simulated DS message pane.
type ConsoleMessage struct {
	Time time.Time `json:"time"`
	Text string    `json:"text"`
	Kind string    `json:"kind"`
}

// State is the published snapshot of a station.
type State struct {
	Station    string `json:"station"`
	Index      int    `json:"index"`
	Team       int    `json:"team"`
	TeamSource string `json:"teamSource"`
	Protocol   string `json:"protocol"`
	UdpPort    int    `json:"udpPort"`

	Settings Settings `json:"settings"`
	Robot    Robot    `json:"robot"`

	RadioLinked    bool    `json:"radioLinked"`
	RioLinked      bool    `json:"rioLinked"`
	RobotLinked    bool    `json:"robotLinked"`
	CodeRunning    bool    `json:"codeRunning"`
	RadioBootLeft  float64 `json:"radioBootLeft"`
	RioBootLeft    float64 `json:"rioBootLeft"`
	TripTimeMs     int     `json:"tripTimeMs"`
	LostPackets    int     `json:"lostPackets"`
	StatusSent     int     `json:"statusSent"`
	StatusSendErr  string  `json:"statusSendErr"`
	TcpState       string  `json:"tcpState"`
	TcpError       string  `json:"tcpError"`
	AssignStation  string  `json:"assignStation"`
	AssignStatus   int     `json:"assignStatus"`
	AssignText     string  `json:"assignText"`
	EventName      string  `json:"eventName"`
	FmsLinked      bool    `json:"fmsLinked"`
	ControlCount   int     `json:"controlCount"`
	ControlRateHz  float64 `json:"controlRateHz"`
	ControlGapMs   int     `json:"controlGapMs"`
	MaxControlGap  int     `json:"maxControlGapMs"`
	GameData       string  `json:"gameData"`
	Enabled        bool    `json:"enabled"`
	Auto           bool    `json:"auto"`
	EStopped       bool    `json:"eStopped"`
	AStopped       bool    `json:"aStopped"`
	ModeText       string  `json:"modeText"`
	DsReportedMode string  `json:"dsReportedMode"`
	// What the real Driver Station shows besides: the elapsed-time counter (since the last enable), the match it is
	// in, brownouts this session, CAN bus utilization, and the laptop's own battery and CPU.
	ElapsedSeconds float64 `json:"elapsedSeconds"`
	MatchLabel     string  `json:"matchLabel"`
	Brownouts      int     `json:"brownouts"`
	CanUtil        float64 `json:"canUtil"`
	PcBattery      float64 `json:"pcBattery"`
	PcCpu          float64 `json:"pcCpu"`

	Control  ControlPacket    `json:"control"`
	Messages []ConsoleMessage `json:"messages"`
}

// Command is what a simulated robot knows from its DS: the only inputs robot behavior may use.
type Command struct {
	Team             int
	Enabled          bool
	Auto             bool
	EStop            bool
	AStop            bool
	FmsLinked        bool
	RobotLinked      bool
	GameData         string
	RemainingSeconds int
	MatchType        int
	Brownout         bool
	Voltage          float64
}

// Station emulates one driver station laptop plus the robot it controls.
type Station struct {
	Index     int
	Name      string
	arenaHost string
	udpPort   int

	mu          sync.Mutex
	settings    Settings
	robot       Robot
	team        int
	teamSrc     string
	arenaTeam   int
	followArena bool
	defaultTeam int

	// Link state.
	session       *tcpSession
	dialing       bool
	tcpState      string
	tcpError      string
	assignStation string
	assignStatus  int
	assignText    string
	eventName     string
	retryAfter    time.Time

	udpConn       *net.UDPConn
	udpBoundPort  int
	udpBindErr    string
	statusSeq     uint16
	statusSent    int
	statusSendErr string
	lostPackets   int
	lossRun       int
	tripTimeMs    int

	control       ControlPacket
	controlAt     time.Time
	controlCount  int
	controlTimes  []time.Time
	maxControlGap time.Duration
	tcpGameData   string
	gameData      string
	lastFmsLinked bool
	lastLinked    bool
	lastEnabled   bool
	lastAuto      bool // the mode that went to the robot with lastEnabled (both change together, once per tick)
	messages      []ConsoleMessage

	// Driver Station extras (see State).
	enabledAt  time.Time
	elapsed    float64
	brownouts  int
	pcBattery  float64
	matchKey   string // the FMS match this station last saw (a new one means a fresh battery)
	teleopSeen bool   // this match has been enabled in teleop
	matchOver  bool   // ... and teleop has run out
	batteryFor int    // the team whose battery characteristics are loaded

	onPacket func(direction, kind string, station string, data []byte)

	// Field network gates (set by the network emulator): the robot radio needs its SSID on the field AP, and the DS
	// needs its SCC port up to reach the FMS.
	radioGate    func(team int) bool
	ethernetGate func(station int) bool
}

type tcpSession struct {
	conn net.Conn
	team int
	done chan struct{}
	once sync.Once
}

func (session *tcpSession) close() {
	session.once.Do(func() {
		close(session.done)
		_ = session.conn.Close()
	})
}

func newStation(index int, arenaHost string, udpPort int, legacy bool, defaultTeam int) *Station {
	return &Station{
		Index:     index,
		Name:      StationName(index),
		arenaHost: arenaHost,
		udpPort:   udpPort,
		settings: Settings{
			DsOn: true, Ethernet: true, Legacy: legacy, LatencyMs: 4, JitterMs: 3, AutoClearES: true,
		},
		robot:       newRobot(),
		tcpState:    "idle",
		defaultTeam: defaultTeam,
		followArena: true,
		pcBattery:   0.7 + 0.3*rand.Float64(),
	}
}

// teamUnit maps a team number and a salt to a repeatable value in [0, 1) (each team's batteries, laptop, ...).
func teamUnit(team int, salt uint32) float64 {
	hash := uint32(team)*2246822519 ^ salt
	hash ^= hash >> 15
	hash *= 2654435761
	hash ^= hash >> 13
	return float64(hash%10000) / 10000
}

var matchTypeNames = map[int]string{0: "None", 1: "Practice", 2: "Qualification", 3: "Elimination"}

// newMatchLocked runs when the FMS starts sending a new match: the drive team puts in a fresh battery (most of the
// time: some teams forget, or grab one that never finished charging).
func (station *Station) newMatchLocked(control ControlPacket) {
	key := fmt.Sprintf("%d-%d-%d", control.MatchType, control.MatchNumber, control.Repeat)
	// Test matches all share number 0, so a played-out match followed by a fresh countdown is a new match too.
	replay := station.matchOver && control.RemainingSeconds > 0
	if key == station.matchKey && !replay {
		return
	}
	station.matchOver, station.teleopSeen = false, false
	first := station.matchKey == ""
	station.matchKey = key
	station.log("good", "FMS Connected:   %s - %d:%d, Field Time: %s", matchTypeNames[control.MatchType],
		control.MatchNumber, control.Repeat, control.FmsTime.Local().Format("06/1/2 15:04:05"))
	if first || station.team == 0 {
		return
	}
	forgetful := 0.02 + 0.1*teamUnit(station.team, 0xba77)
	if rand.Float64() < forgetful {
		station.robot.SoC = math.Min(station.robot.SoC, 0.55+0.3*rand.Float64())
		station.log("warn", "Battery not swapped: starting at %.2f V", station.robot.openCircuit())
		return
	}
	station.robot.SoC = 0.93 + 0.07*rand.Float64()
	station.robot.Brownout = false
	station.log("info", "Fresh battery: %.2f V", station.robot.openCircuit())
}

// teamHardwareLocked loads a team's battery fleet (older batteries have more internal resistance and sag harder).
func (station *Station) teamHardwareLocked() {
	if station.batteryFor == station.team {
		return
	}
	station.batteryFor = station.team
	if station.team == 0 {
		return
	}
	station.robot.Resistance = 0.012 + 0.014*teamUnit(station.team, 0x0bad)
}

func (station *Station) log(kind, format string, args ...any) {
	station.messages = append(station.messages, ConsoleMessage{Time: time.Now(), Text: fmt.Sprintf(format, args...),
		Kind: kind})
	if len(station.messages) > maxConsoleMessages {
		station.messages = station.messages[len(station.messages)-maxConsoleMessages:]
	}
}

// run is the station's 50 Hz main loop.
func (station *Station) run(stop <-chan struct{}) {
	ticker := time.NewTicker(tickPeriod)
	defer ticker.Stop()
	last := time.Now()
	for {
		select {
		case <-stop:
			station.mu.Lock()
			station.closeSessionLocked()
			station.closeUdpLocked()
			station.mu.Unlock()
			return
		case now := <-ticker.C:
			dt := now.Sub(last).Seconds()
			last = now
			station.tick(now, dt)
		}
	}
}

func (station *Station) effectiveTeamLocked() (int, string) {
	if station.settings.TeamOverride > 0 {
		return station.settings.TeamOverride, "override"
	}
	if station.followArena {
		return station.arenaTeam, "arena"
	}
	return station.defaultTeam, "default"
}

func (station *Station) desiredUdpPortLocked() int {
	if station.settings.Legacy {
		if station.settings.LegacyLite {
			return legacyLiteUdpPort
		}
		return legacyUdpPort
	}
	return station.udpPort
}

func (station *Station) tick(now time.Time, dt float64) {
	station.mu.Lock()
	defer station.mu.Unlock()

	team, source := station.effectiveTeamLocked()
	if team != station.team {
		if station.team != 0 || team != 0 {
			station.log("info", "Team number set to %d (%s)", team, source)
		}
		station.team = team
		station.closeSessionLocked()
		station.retryAfter = time.Time{}
	}
	station.teamSrc = source

	portUp := station.ethernetGate == nil || station.ethernetGate(station.Index)
	online := station.settings.DsOn && station.settings.Ethernet && portUp && station.team > 0

	// UDP socket: bound whenever the DS is running so FMS control packets can arrive.
	wantPort := station.desiredUdpPortLocked()
	if station.settings.DsOn {
		if station.udpConn == nil || station.udpBoundPort != wantPort {
			station.closeUdpLocked()
			station.openUdpLocked(wantPort)
		}
	} else {
		station.closeUdpLocked()
	}

	// TCP session to the FMS.
	if online {
		if station.session == nil && !station.dialing && now.After(station.retryAfter) {
			station.startSessionLocked(now)
		}
	} else {
		if station.session != nil {
			station.closeSessionLocked()
		}
		switch {
		case !station.settings.DsOn:
			station.tcpState = "ds-off"
		case !station.settings.Ethernet:
			station.tcpState = "unplugged"
		case !portUp:
			station.tcpState = "port-down"
		default:
			station.tcpState = "no-team"
		}
	}

	// Robot comms chain and simulated packet loss on the robot link.
	ssidUp := station.radioGate == nil || station.team == 0 || station.radioGate(station.team)
	radio := station.robot.RadioReachable(now) && ssidUp
	rio := station.robot.RioReachable(now) && ssidUp
	linked := rio
	if linked && station.settings.LossPct > 0 && rand.Float64()*100 < station.settings.LossPct {
		station.lostPackets++
		station.lossRun++
	} else if linked {
		station.lossRun = 0
	}
	if station.lossRun >= lossOutageTicks {
		linked = false
	}
	if !station.settings.DsOn {
		radio, rio, linked = false, false, false
	}
	if linked != station.lastLinked {
		if linked {
			station.log("good", "Robot communications established")
		} else {
			station.log("bad", "Lost robot communications")
		}
		station.lastLinked = linked
	}
	station.tripTimeMs = station.settings.LatencyMs
	if station.settings.JitterMs > 0 {
		station.tripTimeMs += rand.Intn(station.settings.JitterMs + 1)
	}

	// FMS control freshness and the robot's resulting enable state.
	fmsLinked := online && station.session != nil && !station.controlAt.IsZero() &&
		now.Sub(station.controlAt) < fmsTimeout
	station.teamHardwareLocked()
	if fmsLinked {
		switch {
		case station.control.Enabled && !station.control.Auto:
			station.teleopSeen = true
		case !station.control.Enabled && station.teleopSeen && station.control.RemainingSeconds == 0:
			station.matchOver = true
		case !station.control.Enabled:
			station.newMatchLocked(station.control)
		}
	}
	if fmsLinked != station.lastFmsLinked {
		if fmsLinked && station.matchKey == "" {
			station.log("good", "FMS connected")
		} else if !fmsLinked && station.lastFmsLinked {
			station.log("bad", "FMS disconnected")
		}
		station.lastFmsLinked = fmsLinked
	}
	control := station.control
	if control.EStop && fmsLinked && !station.robot.EStopLatched {
		station.robot.EStopLatched = true
		station.log("bad", "Emergency stop received from FMS")
	}
	if station.robot.EStopLatched && station.settings.AutoClearES && fmsLinked && !control.EStop &&
		!control.Enabled {
		station.robot.EStopLatched = false
		station.log("info", "E-stop cleared (simulated robot reboot)")
	}
	codeRunning := station.robot.CodeRunning(now) && station.settings.DsOn
	enabled := fmsLinked && control.Enabled && linked && codeRunning && !station.robot.EStopLatched &&
		!station.robot.Brownout
	if enabled != station.lastEnabled {
		mode := "Teleoperated"
		if control.Auto {
			mode = "Autonomous"
		}
		if enabled {
			station.log("good", "%s Enabled", mode)
			station.enabledAt, station.elapsed = now, 0 // the DS elapsed-time counter restarts on every enable
		} else {
			station.log("info", "%s Disabled", mode)
		}
		station.lastEnabled = enabled
	}
	station.lastAuto = control.Auto
	if enabled {
		station.elapsed = now.Sub(station.enabledAt).Seconds()
	}
	wasBrownout := station.robot.Brownout
	station.robot.Step(dt, now, enabled)
	if station.robot.Brownout && !wasBrownout {
		station.brownouts++
		station.log("bad", "Brownout detected (%.2f V): the roboRIO is disabling outputs", station.robot.Voltage)
	}
	if !station.robot.Brownout && wasBrownout {
		station.log("warn", "Brownout recovered (%.2f V)", station.robot.Voltage)
	}
	station.pcBattery = math.Max(0.05, station.pcBattery-dt/(4*3600)) // the laptop runs ~4 h unplugged

	// Game data: tag 32 on the new DS, TCP type 28 on the legacy DS.
	if station.settings.Legacy {
		station.gameData = station.tcpGameData
	} else if fmsLinked {
		station.gameData = control.GameData
	}

	if online && station.udpConn != nil {
		battery := 0.0
		if linked {
			battery = station.robot.Voltage
		}
		packet := EncodeStatus(StatusPacket{
			Sequence: station.statusSeq, Team: station.team, RioLinked: rio, RadioLinked: radio,
			RobotLinked: linked, Enabled: enabled, Auto: control.Auto, Battery: battery,
			LostPackets: station.lostPackets, TripTimeMs: station.tripTimeMs,
		})
		station.statusSeq++
		address := &net.UDPAddr{IP: net.ParseIP(station.arenaHost), Port: arenaUdpStatusPort}
		if _, err := station.udpConn.WriteToUDP(packet, address); err != nil {
			station.statusSendErr = err.Error()
		} else {
			station.statusSent++
			station.statusSendErr = ""
			if station.onPacket != nil && station.statusSent%25 == 0 {
				station.onPacket("out", "ds-status", station.Name, packet)
			}
		}
	}
}

func (station *Station) openUdpLocked(port int) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(station.arenaHost), Port: port})
	if err != nil {
		if station.udpBindErr != err.Error() {
			station.log("bad", "Cannot bind UDP port %d: %v", port, err)
		}
		station.udpBindErr = err.Error()
		return
	}
	station.udpBindErr = ""
	station.udpConn = conn
	station.udpBoundPort = port
	go station.readControl(conn)
}

func (station *Station) closeUdpLocked() {
	if station.udpConn != nil {
		_ = station.udpConn.Close()
		station.udpConn = nil
		station.udpBoundPort = 0
	}
}

func (station *Station) readControl(conn *net.UDPConn) {
	buffer := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		control, err := DecodeControl(buffer[:n])
		if err != nil {
			inspector.Note("ds", "in", station.Name, fmt.Sprintf("undecodable control packet (%v)", err), buffer[:n])
			continue
		}
		inspector.Note("ds", "in", station.Name, describeControl(control), buffer[:n])
		now := time.Now()
		station.mu.Lock()
		if station.udpConn != conn {
			station.mu.Unlock()
			return
		}
		station.storeControlLocked(control, now)
		station.mu.Unlock()
	}
}

// storeControlLocked keeps an FMS control packet as it arrives; the next tick applies it to the robot.
func (station *Station) storeControlLocked(control ControlPacket, now time.Time) {
	if !station.controlAt.IsZero() {
		if gap := now.Sub(station.controlAt); gap > station.maxControlGap && station.controlCount > 2 {
			station.maxControlGap = gap
		}
	}
	if control.Enabled != station.control.Enabled || control.Auto != station.control.Auto ||
		control.EStop != station.control.EStop || control.AStop != station.control.AStop {
		if station.onPacket != nil {
			station.onPacket("in", "fms-control", station.Name, control.Raw)
		}
	}
	station.control = control
	station.controlAt = now
	station.controlCount++
	station.controlTimes = append(station.controlTimes, now)
	cutoff := now.Add(-controlRateWindowMs * time.Millisecond)
	for len(station.controlTimes) > 0 && station.controlTimes[0].Before(cutoff) {
		station.controlTimes = station.controlTimes[1:]
	}
}

func (station *Station) startSessionLocked(now time.Time) {
	station.tcpState = "connecting"
	station.tcpError = ""
	station.dialing = true
	team := station.team
	legacy := station.settings.Legacy
	udpPort := station.desiredUdpPortLocked()
	go func() {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(station.arenaHost, strconv.Itoa(arenaTcpPort)),
			2*time.Second)
		station.mu.Lock()
		station.dialing = false
		if err != nil {
			station.tcpState = "error"
			station.tcpError = err.Error()
			station.retryAfter = time.Now().Add(reconnectDelay)
			station.mu.Unlock()
			return
		}
		if station.team != team || station.session != nil {
			station.mu.Unlock()
			_ = conn.Close()
			return
		}
		session := &tcpSession{conn: conn, team: team, done: make(chan struct{})}
		station.session = session
		station.tcpState = "handshake"
		station.assignStatus = -1
		station.assignStation = ""
		station.assignText = ""
		station.tcpGameData = ""
		station.mu.Unlock()

		var handshake []byte
		if legacy {
			handshake = EncodeLegacyHandshake(team)
		} else {
			handshake = EncodeNewHandshake(team, udpPort, 0)
		}
		if station.onPacket != nil {
			station.onPacket("out", "ds-handshake", station.Name, handshake)
		}
		if _, err := conn.Write(handshake); err != nil {
			station.endSession(session, err)
			return
		}
		go station.tcpWriter(session)
		station.tcpReader(session)
	}()
	_ = now
}

func (station *Station) closeSessionLocked() {
	if station.session != nil {
		station.session.close()
		station.session = nil
	}
	station.control = ControlPacket{}
	station.controlAt = time.Time{}
	station.controlTimes = nil
	station.assignStation = ""
	station.assignText = ""
}

func (station *Station) endSession(session *tcpSession, err error) {
	session.close()
	station.mu.Lock()
	defer station.mu.Unlock()
	if station.session != session {
		return
	}
	station.session = nil
	station.control = ControlPacket{}
	station.controlAt = time.Time{}
	if station.assignStatus == AssignNotInMatch || station.assignStatus == AssignInvalidTeam {
		station.tcpState = "rejected"
		station.retryAfter = time.Now().Add(rejectedRetryDelay)
	} else {
		station.tcpState = "error"
		station.retryAfter = time.Now().Add(reconnectDelay)
		if err != nil && err != io.EOF {
			station.tcpError = err.Error()
		} else {
			station.tcpError = "FMS closed the connection"
		}
		station.log("bad", "FMS TCP connection closed: %s", station.tcpError)
	}
}

func (station *Station) tcpWriter(session *tcpSession) {
	keepalive := time.NewTicker(keepalivePeriod)
	dsLog := time.NewTicker(dsLogPeriod)
	defer keepalive.Stop()
	defer dsLog.Stop()
	for {
		select {
		case <-session.done:
			return
		case <-keepalive.C:
			_ = session.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if _, err := session.conn.Write(EncodeKeepalive()); err != nil {
				station.endSession(session, err)
				return
			}
		case <-dsLog.C:
			station.mu.Lock()
			mode := byte(DsModeDisabled)
			if station.lastEnabled {
				if station.lastAuto {
					mode = DsModeAuto
				} else {
					mode = DsModeTeleop
				}
			} else if station.lastAuto {
				mode |= DsModeAuto
			} else {
				mode |= DsModeTeleop
			}
			packet := EncodeDsLog(station.tripTimeMs, station.lostPackets%256, station.robot.Voltage, 35, mode)
			station.mu.Unlock()
			_ = session.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if _, err := session.conn.Write(packet); err != nil {
				station.endSession(session, err)
				return
			}
		}
	}
}

func (station *Station) tcpReader(session *tcpSession) {
	header := make([]byte, 2)
	for {
		// The FMS rarely sends anything after the handshake, so there is no read deadline; a dead link is detected
		// by the keepalive writes failing.
		if _, err := io.ReadFull(session.conn, header); err != nil {
			station.endSession(session, err)
			return
		}
		length := int(header[0])<<8 + int(header[1])
		body := make([]byte, length)
		if _, err := io.ReadFull(session.conn, body); err != nil {
			station.endSession(session, err)
			return
		}
		if length == 0 {
			continue
		}
		if station.onPacket != nil {
			station.onPacket("in", fmt.Sprintf("fms-tcp-%d", body[0]), station.Name, append(header, body...))
		}
		station.handleTcpPacket(session, body)
	}
}

func (station *Station) handleTcpPacket(session *tcpSession, body []byte) {
	station.mu.Lock()
	defer station.mu.Unlock()
	if station.session != session {
		return
	}
	switch body[0] {
	case TcpLegacyAssignment, TcpNewStationAssigned:
		if len(body) < 3 {
			return
		}
		station.assignStation = StationName(int(body[1]))
		station.assignStatus = int(body[2])
		switch station.assignStatus {
		case AssignOk:
			station.tcpState = "connected"
			station.assignText = "Assigned to " + station.assignStation
			station.log("good", "FMS assigned station %s", station.assignStation)
		case AssignWrongStation:
			station.tcpState = "connected"
			station.assignText = "Wrong station (FMS wants " + station.assignStation + ")"
			station.log("bad", "FMS reports team is in the WRONG STATION")
		case AssignNotInMatch:
			station.tcpState = "rejected"
			station.assignText = "Team not in the current match"
			station.log("warn", "FMS: team %d is not in the current match", station.team)
			go func() {
				time.Sleep(300 * time.Millisecond)
				station.endSession(session, nil)
			}()
		default:
			station.tcpState = "rejected"
			station.assignText = fmt.Sprintf("Rejected by FMS (status %d)", station.assignStatus)
			station.log("bad", "FMS rejected the connection (status %d)", station.assignStatus)
			go func() {
				time.Sleep(300 * time.Millisecond)
				station.endSession(session, nil)
			}()
		}
	case TcpEventName:
		if len(body) >= 2 {
			nameLength := int(body[1])
			if 2+nameLength <= len(body) {
				station.eventName = string(body[2 : 2+nameLength])
			}
		}
	case TcpGameData:
		if len(body) >= 2 {
			dataLength := int(body[1])
			if 2+dataLength <= len(body) {
				station.tcpGameData = string(body[2 : 2+dataLength])
				station.log("info", "Game data received: %q", station.tcpGameData)
			}
		}
	}
}

// snapshot builds the published state.
func (station *Station) snapshot(now time.Time) State {
	station.mu.Lock()
	defer station.mu.Unlock()
	fmsLinked := station.lastFmsLinked
	control := station.control
	rate := 0.0
	if len(station.controlTimes) > 1 {
		span := station.controlTimes[len(station.controlTimes)-1].Sub(station.controlTimes[0]).Seconds()
		if span > 0 {
			rate = float64(len(station.controlTimes)-1) / span
		}
	}
	gap := 0
	if !station.controlAt.IsZero() {
		gap = int(now.Sub(station.controlAt).Milliseconds())
	}
	protocol := "new"
	if station.settings.Legacy {
		protocol = "legacy"
	}
	mode := "Disabled"
	switch {
	case !station.settings.DsOn:
		mode = "DS off"
	case station.robot.EStopLatched:
		mode = "Emergency Stopped"
	case !station.lastLinked:
		mode = "No Robot Communication"
	case !station.robot.CodeRunning(now):
		mode = "No Robot Code"
	case station.robot.Brownout:
		mode = "Voltage Brownout"
	case station.lastEnabled && station.lastAuto:
		mode = "Autonomous Enabled"
	case station.lastEnabled:
		mode = "Teleoperated Enabled"
	case station.lastAuto:
		mode = "Autonomous Disabled"
	default:
		mode = "Teleoperated Disabled"
	}
	reported := "disabled"
	if station.lastEnabled {
		reported = "teleop"
		if station.lastAuto {
			reported = "auto"
		}
	}
	errText := station.statusSendErr
	if station.udpBindErr != "" {
		errText = station.udpBindErr
	}
	return State{
		Station: station.Name, Index: station.Index, Team: station.team, TeamSource: station.teamSrc,
		Protocol: protocol, UdpPort: station.desiredUdpPortLocked(),
		Settings: station.settings, Robot: station.robot,
		RadioLinked:   station.robot.RadioReachable(now) && station.settings.DsOn,
		RioLinked:     station.robot.RioReachable(now) && station.settings.DsOn,
		RobotLinked:   station.lastLinked,
		CodeRunning:   station.robot.CodeRunning(now),
		RadioBootLeft: station.robot.RadioBootRemaining(now),
		RioBootLeft:   station.robot.RioBootRemaining(now),
		TripTimeMs:    station.tripTimeMs, LostPackets: station.lostPackets, StatusSent: station.statusSent,
		StatusSendErr: errText, TcpState: station.tcpState, TcpError: station.tcpError,
		AssignStation: station.assignStation, AssignStatus: station.assignStatus, AssignText: station.assignText,
		EventName: station.eventName, FmsLinked: fmsLinked, ControlCount: station.controlCount,
		ControlRateHz: rate, ControlGapMs: gap, MaxControlGap: int(station.maxControlGap.Milliseconds()),
		GameData: station.gameData, Enabled: station.lastEnabled, Auto: station.lastAuto,
		EStopped: station.robot.EStopLatched || control.EStop, AStopped: control.AStop, ModeText: mode,
		DsReportedMode: reported, Control: control,
		Messages:       append([]ConsoleMessage(nil), station.messages...),
		ElapsedSeconds: station.elapsed, Brownouts: station.brownouts, PcBattery: station.pcBattery,
		MatchLabel: matchLabel(control),
		// CAN utilization: a steady base from the team's bus layout, higher while the motors are working.
		CanUtil: math.Min(100, 22+25*teamUnit(station.team, 0xca11)+station.robot.LoadAmps*0.12+rand.Float64()*3),
		PcCpu:   math.Min(100, 12+18*teamUnit(station.team, 0xc9c9)+rand.Float64()*8),
	}
}

// matchLabel is the short match name the DS shows (Q12, P3, E5, or Test).
func matchLabel(control ControlPacket) string {
	prefix := map[int]string{1: "P", 2: "Q", 3: "E"}[control.MatchType]
	if prefix == "" || control.MatchNumber == 0 {
		return "Test match"
	}
	if control.Repeat > 1 {
		return fmt.Sprintf("%s%d (replay %d)", prefix, control.MatchNumber, control.Repeat-1)
	}
	return fmt.Sprintf("%s%d", prefix, control.MatchNumber)
}

// Command returns the robot-facing view of the DS (used by robot behavior; it never peeks at arena state). Enabled and
// Auto are what the DS last sent the robot, together: a control packet that has arrived but not yet been applied must
// not pair the old enable with the new mode (at the end of AUTO that read as a moment of enabled TELEOP).
func (station *Station) Command() Command {
	station.mu.Lock()
	defer station.mu.Unlock()
	return Command{
		Team: station.team, Enabled: station.lastEnabled, Auto: station.lastAuto,
		EStop: station.robot.EStopLatched, AStop: station.control.AStop, FmsLinked: station.lastFmsLinked,
		RobotLinked: station.lastLinked, GameData: station.gameData,
		RemainingSeconds: station.control.RemainingSeconds, MatchType: station.control.MatchType,
		Brownout: station.robot.Brownout, Voltage: station.robot.Voltage,
	}
}

// SetLoad reports the robot's motor current draw from the physics model.
func (station *Station) SetLoad(amps float64) {
	station.mu.Lock()
	station.robot.SetExternalLoad(amps, time.Now())
	station.mu.Unlock()
}

// EthernetConnected reports whether the DS is plugged into its station's field Ethernet port (link light on).
func (station *Station) EthernetConnected() bool {
	station.mu.Lock()
	defer station.mu.Unlock()
	return station.settings.Ethernet && station.settings.DsOn
}

// RadioPowered reports whether the robot's radio is up (independent of whether its SSID exists).
func (station *Station) RadioPowered() bool {
	station.mu.Lock()
	defer station.mu.Unlock()
	return station.settings.DsOn && station.robot.RadioReachable(time.Now())
}

// TeamNumber returns the team currently at the station.
func (station *Station) TeamNumber() int {
	station.mu.Lock()
	defer station.mu.Unlock()
	return station.team
}
