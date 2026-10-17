// Package arena talks to Freezy Arena over its public HTTP/websocket interface: it mirrors the arena's state into
// the bus for the GUI and gives the rest of the simulator typed access to match state and match control.
package arena

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/config"
)

var errNotConnected = errors.New("not connected to the arena")

// MatchState mirrors field.MatchState in the arena.
type MatchState int

const (
	PreMatch MatchState = iota
	StartMatch
	AutoPeriod
	PausePeriod
	TeleopPeriod
	PostMatch
	TimeoutActive
	PostTimeout
)

var matchStateNames = []string{
	"PreMatch", "StartMatch", "AutoPeriod", "PausePeriod", "TeleopPeriod", "PostMatch", "TimeoutActive", "PostTimeout",
}

func (state MatchState) String() string {
	if int(state) >= 0 && int(state) < len(matchStateNames) {
		return matchStateNames[state]
	}
	return fmt.Sprintf("MatchState(%d)", int(state))
}

// InMatch reports whether robots may be enabled in this state.
func (state MatchState) InMatch() bool {
	return state == StartMatch || state == AutoPeriod || state == PausePeriod || state == TeleopPeriod
}

// Stations lists the alliance stations in the order used throughout the simulator.
var Stations = [6]string{"R1", "R2", "R3", "B1", "B2", "B3"}

type Team struct {
	Id       int    `json:"Id"`
	Nickname string `json:"Nickname"`
	WpaKey   string `json:"WpaKey"`
}

type Match struct {
	Id        int    `json:"Id"`
	Type      int    `json:"Type"`
	TypeOrder int    `json:"TypeOrder"`
	LongName  string `json:"LongName"`
	ShortName string `json:"ShortName"`
}

type MatchLoad struct {
	Match *Match           `json:"Match"`
	Teams map[string]*Team `json:"Teams"`
}

type DsConn struct {
	TeamId                    int     `json:"TeamId"`
	AllianceStation           string  `json:"AllianceStation"`
	Auto                      bool    `json:"Auto"`
	Enabled                   bool    `json:"Enabled"`
	EStop                     bool    `json:"EStop"`
	AStop                     bool    `json:"AStop"`
	DsLinked                  bool    `json:"DsLinked"`
	RadioLinked               bool    `json:"RadioLinked"`
	RioLinked                 bool    `json:"RioLinked"`
	RobotLinked               bool    `json:"RobotLinked"`
	BatteryVoltage            float64 `json:"BatteryVoltage"`
	DsRobotTripTimeMs         int     `json:"DsRobotTripTimeMs"`
	MissedPacketCount         int     `json:"MissedPacketCount"`
	SecondsSinceLastRobotLink float64 `json:"SecondsSinceLastRobotLink"`
	WrongStation              string  `json:"WrongStation"`
	// What the DS itself reports (its TCP log packets): whether the robot really is enabled.
	DsReportedStatusValid bool `json:"DsReportedStatusValid"`
	DsReportedEnabled     bool `json:"DsReportedEnabled"`
	DsReportedAuto        bool `json:"DsReportedAuto"`
}

type StationStatus struct {
	DsConn   *DsConn `json:"DsConn"`
	Ethernet bool    `json:"Ethernet"`
	AStop    bool    `json:"AStop"`
	EStop    bool    `json:"EStop"`
	Bypass   bool    `json:"Bypass"`
	Team     *Team   `json:"Team"`
}

type ArenaStatus struct {
	MatchId               int                       `json:"MatchId"`
	AllianceStations      map[string]*StationStatus `json:"AllianceStations"`
	MatchState            MatchState                `json:"MatchState"`
	CanStartMatch         bool                      `json:"CanStartMatch"`
	StartMatchConditions  []string                  `json:"StartMatchConditions"`
	AccessPointStatus     string                    `json:"AccessPointStatus"`
	SwitchStatus          string                    `json:"SwitchStatus"`
	RedSCCStatus          string                    `json:"RedSCCStatus"`
	BlueSCCStatus         string                    `json:"BlueSCCStatus"`
	PlcIsHealthy          bool                      `json:"PlcIsHealthy"`
	FieldEStop            bool                      `json:"FieldEStop"`
	IsFtaReady            bool                      `json:"IsFtaReady"`
	PlcArmorBlockStatuses map[string]bool           `json:"PlcArmorBlockStatuses"`
	ScoreTableIOEnabled   bool                      `json:"ScoreTableIOEnabled"`
	RedEstopsEnabled      bool                      `json:"RedEstopsEnabled"`
	BlueEstopsEnabled     bool                      `json:"BlueEstopsEnabled"`
	ScoreTableIOIsHealthy bool                      `json:"ScoreTableIOIsHealthy"`
	RedEstopsIsHealthy    bool                      `json:"RedEstopsIsHealthy"`
	BlueEStopsIsHealthy   bool                      `json:"BlueEStopsIsHealthy"`
}

type MatchTime struct {
	MatchState   MatchState `json:"MatchState"`
	MatchTimeSec int        `json:"MatchTimeSec"`
}

// PlcIo is the arena's own view of PLC I/O (from /api/plc/websocket).
type PlcIo struct {
	Inputs        []bool   `json:"Inputs"`
	Registers     []uint16 `json:"Registers"`
	Coils         []bool   `json:"Coils"`
	CoilOverrides []string `json:"CoilOverrides"`
}

type LedModes struct {
	RedMode  int `json:"RedMode"`
	BlueMode int `json:"BlueMode"`
}

// The hub LED modes the head referee's field signals set (Freezy Arena led.Mode).
const (
	LedFieldSafe    = 3 // green: drive teams may enter and take their robots
	LedFieldCleanup = 4 // purple: the field staff may enter and restage FUEL
)

type Client struct {
	cfg *config.Config
	hub *bus.Hub

	MatchPlay *Socket
	Plc       *Socket

	mu            sync.RWMutex
	sessionCookie string
	matchLoad     MatchLoad
	status        ArenaStatus
	matchTime     MatchTime
	matchTimeAt   time.Time
	matchStartAt  time.Time // when the current match's AUTO began (zero before a match)
	plcIo         PlcIo
	ledModes      LedModes
	audience      AudienceDisplay
	lastError     string

	listenerMu         sync.Mutex
	connectListeners   []func()
	matchLoadListeners []func(MatchLoad)
	stateListeners     []func(old, new MatchState)
}

func NewClient(cfg *config.Config, hub *bus.Hub) *Client {
	client := &Client{cfg: cfg, hub: hub}
	base := cfg.ArenaURL()
	client.MatchPlay = &Socket{
		Name:      "matchPlay",
		URL:       wsURL(base, "/match_play/websocket"),
		Header:    client.authHeader,
		OnMessage: client.handleMatchPlayMessage,
		OnState:   client.socketStateChanged("Match Play"),
	}
	client.Plc = &Socket{
		Name:      "plc",
		URL:       wsURL(base, "/api/plc/websocket"),
		Header:    client.authHeader,
		OnMessage: client.handlePlcMessage,
		OnState:   client.socketStateChanged("PLC"),
	}
	client.registerCommands()
	return client
}

// NewSocket creates an authenticated, auto-reconnecting socket to an arena websocket path (not started).
func (client *Client) NewSocket(name, path string, onMessage func(string, json.RawMessage)) *Socket {
	return &Socket{
		Name: name, URL: wsURL(client.cfg.ArenaURL(), path), Header: client.authHeader, OnMessage: onMessage,
		OnState: client.socketStateChanged(name),
	}
}

// Run starts the observer sockets.
func (client *Client) Run() {
	go client.MatchPlay.Run()
	go client.Plc.Run()
	go func() {
		for range time.Tick(time.Second) {
			client.hub.Publish("arena.sockets", []SocketStatus{client.MatchPlay.Status(), client.Plc.Status()})
		}
	}()
	go client.pollStackLights()
}

// pollStackLights mirrors the arena's own team stack light logic (/api/freezy/team_stack_light) for the 3D field.
func (client *Client) pollStackLights() {
	for range time.Tick(250 * time.Millisecond) {
		if !client.Connected() {
			continue
		}
		var lights json.RawMessage
		if err := client.GetJSON("/api/freezy/team_stack_light", &lights); err == nil {
			client.hub.Publish("arena.teamStackLights", lights)
		}
	}
}

// OnConnect registers a callback run (in its own goroutine) each time the Match Play socket connects.
func (client *Client) OnConnect(listener func()) {
	client.listenerMu.Lock()
	client.connectListeners = append(client.connectListeners, listener)
	client.listenerMu.Unlock()
}

// OnMatchLoad registers a callback for every match load (including the initial state on connect).
func (client *Client) OnMatchLoad(listener func(MatchLoad)) {
	client.listenerMu.Lock()
	client.matchLoadListeners = append(client.matchLoadListeners, listener)
	client.listenerMu.Unlock()
}

// OnMatchState registers a callback for match state transitions.
func (client *Client) OnMatchState(listener func(old, new MatchState)) {
	client.listenerMu.Lock()
	client.stateListeners = append(client.stateListeners, listener)
	client.listenerMu.Unlock()
}

// Teams returns the team numbers loaded in each station (0 for empty), ordered as Stations.
func (client *Client) Teams() [6]int {
	client.mu.RLock()
	defer client.mu.RUnlock()
	var teams [6]int
	for i, station := range Stations {
		if team := client.matchLoad.Teams[station]; team != nil {
			teams[i] = team.Id
		}
	}
	return teams
}

// TeamFor returns the full team record loaded in a station.
func (client *Client) TeamFor(station string) *Team {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return client.matchLoad.Teams[station]
}

// MatchLoad returns the last match load message.
func (client *Client) MatchLoad() MatchLoad {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return client.matchLoad
}

// Status returns the last arena status message.
func (client *Client) Status() ArenaStatus {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return client.status
}

// MatchTime returns the match state and the match time in seconds. The arena times every period (and the scoring
// windows) from the moment the match started, so from the start of AUTO the time is measured from when that state
// arrived — exact to the websocket latency, and still running through post-match (the scoring grace window). The
// arena's own whole-second updates are the fallback (and the clock for timeouts).
func (client *Client) MatchTime() (MatchState, float64) {
	client.mu.RLock()
	defer client.mu.RUnlock()
	state := client.matchTime.MatchState
	switch state {
	case AutoPeriod, PausePeriod, TeleopPeriod, PostMatch:
		if !client.matchStartAt.IsZero() {
			return state, time.Since(client.matchStartAt).Seconds()
		}
	}
	seconds := float64(client.matchTime.MatchTimeSec)
	switch state {
	case AutoPeriod, PausePeriod, TeleopPeriod, TimeoutActive:
		seconds += time.Since(client.matchTimeAt).Seconds()
	}
	return state, seconds
}

// PlcIo returns the arena's view of the PLC I/O.
func (client *Client) PlcIo() PlcIo {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return client.plcIo
}

// AudienceDisplay returns the arena's audience display mode and the screen before it.
func (client *Client) AudienceDisplay() AudienceDisplay {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return client.audience
}

// LedModes returns the arena's current hub LED modes.
func (client *Client) LedModes() LedModes {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return client.ledModes
}

// Connected reports whether the primary observer socket is up.
func (client *Client) Connected() bool {
	return client.MatchPlay.Connected()
}

func (client *Client) socketStateChanged(name string) func(bool, error) {
	return func(connected bool, err error) {
		if connected {
			client.hub.Logf("arena", "Connected to arena %s websocket", name)
			if name == "Match Play" {
				client.listenerMu.Lock()
				listeners := append([]func(){}, client.connectListeners...)
				client.listenerMu.Unlock()
				for _, listener := range listeners {
					go listener()
				}
			}
		} else if err != nil {
			if strings.Contains(err.Error(), "bad handshake") {
				// Most likely an admin password redirect; force a fresh login next time.
				client.mu.Lock()
				client.sessionCookie = ""
				client.mu.Unlock()
			}
			client.hub.Warnf("arena", "Lost arena %s websocket: %v", name, err)
		}
	}
}

func (client *Client) handleMatchPlayMessage(msgType string, data json.RawMessage) {
	client.hub.Publish("arena."+msgType, data)
	switch msgType {
	case "matchLoad":
		var load MatchLoad
		if json.Unmarshal(data, &load) == nil {
			client.mu.Lock()
			client.matchLoad = load
			client.mu.Unlock()
			name := "?"
			if load.Match != nil {
				name = load.Match.LongName
			}
			client.hub.Logf("arena", "Match loaded: %s %v", name, client.Teams())
			client.listenerMu.Lock()
			listeners := append([]func(MatchLoad){}, client.matchLoadListeners...)
			client.listenerMu.Unlock()
			for _, listener := range listeners {
				listener(load)
			}
		}
	case "audienceDisplayMode":
		var mode string
		if json.Unmarshal(data, &mode) == nil {
			client.mu.Lock()
			changed := mode != client.audience.Mode
			if changed {
				client.audience = AudienceDisplay{Mode: mode, Previous: client.audience.Mode}
			}
			audience := client.audience
			client.mu.Unlock()
			if changed {
				client.hub.Publish("arena.audienceDisplay", audience)
			}
		}
	case "matchTime":
		var matchTime MatchTime
		if json.Unmarshal(data, &matchTime) == nil {
			client.setMatchTime(matchTime)
		}
	case "arenaStatus":
		var status ArenaStatus
		if json.Unmarshal(data, &status) == nil {
			client.mu.Lock()
			client.status = status
			current := client.matchTime
			quiet := time.Since(client.matchTimeAt)
			client.mu.Unlock()
			// Freezy Arena can miss the PostMatch → PreMatch matchTime notification: its loop compares the state with
			// lastMatchState early in each cycle but records lastMatchState at the end, so a ResetMatch from the
			// websocket goroutine landing in between ("discard results", loading the next match) is never announced.
			// arenaStatus carries the state too. Different notifiers can be delivered out of order, so only trust it
			// for that one transition, and only when no matchTime has arrived for a while.
			if current.MatchState == PostMatch && status.MatchState == PreMatch && quiet > time.Second {
				client.hub.Warnf("arena", "Arena did not announce its return to pre-match; using its status instead")
				client.setMatchTime(MatchTime{MatchState: PreMatch})
			}
		}
	case "error":
		var text string
		_ = json.Unmarshal(data, &text)
		client.mu.Lock()
		client.lastError = text
		client.mu.Unlock()
		client.hub.Warnf("arena", "Arena rejected command: %s", text)
	}
}

func (client *Client) setMatchTime(matchTime MatchTime) {
	client.mu.Lock()
	old := client.matchTime.MatchState
	client.matchTime = matchTime
	client.matchTimeAt = time.Now()
	switch {
	case matchTime.MatchState == AutoPeriod && old != AutoPeriod:
		// The match started (seen mid-auto on a reconnect: back-date by the arena's whole seconds).
		client.matchStartAt = client.matchTimeAt.Add(-time.Duration(matchTime.MatchTimeSec) * time.Second)
	case matchTime.MatchState == PreMatch || matchTime.MatchState == TimeoutActive:
		client.matchStartAt = time.Time{}
	}
	client.mu.Unlock()
	if old != matchTime.MatchState {
		client.hub.Logf("arena", "Match state %s → %s", old, matchTime.MatchState)
		client.listenerMu.Lock()
		listeners := append([]func(MatchState, MatchState){}, client.stateListeners...)
		client.listenerMu.Unlock()
		for _, listener := range listeners {
			listener(old, matchTime.MatchState)
		}
	}
}

func (client *Client) handlePlcMessage(msgType string, data json.RawMessage) {
	switch msgType {
	case "plcIoChange":
		var plcIo PlcIo
		if json.Unmarshal(data, &plcIo) == nil {
			client.mu.Lock()
			client.plcIo = plcIo
			client.mu.Unlock()
			client.hub.Publish("arena.plcIo", plcIo)
		}
	case "setLedMode":
		var modes LedModes
		if json.Unmarshal(data, &modes) == nil {
			client.mu.Lock()
			client.ledModes = modes
			client.mu.Unlock()
			client.hub.Publish("arena.ledModes", modes)
		}
	case "error":
		var text string
		_ = json.Unmarshal(data, &text)
		client.hub.Warnf("arena", "Arena PLC websocket error: %s", text)
	}
}

// SendMatchPlay sends a command over the Match Play websocket.
func (client *Client) SendMatchPlay(command string, data any) error {
	return client.MatchPlay.Send(command, data)
}

func (client *Client) registerCommands() {
	send := func(command string, data any) (any, error) {
		return nil, client.SendMatchPlay(command, data)
	}
	client.hub.Handle("match.loadTest", func(_ []byte) (any, error) {
		return send("loadMatch", map[string]int{"MatchId": 0})
	})
	client.hub.Handle("match.load", func(args []byte) (any, error) {
		var request struct {
			MatchId int `json:"matchId"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		return send("loadMatch", map[string]int{"MatchId": request.MatchId})
	})
	client.hub.Handle("match.start", func(args []byte) (any, error) {
		var request struct {
			Mute bool `json:"mute"`
		}
		_ = json.Unmarshal(args, &request)
		return send("startMatch", map[string]bool{"MuteMatchSounds": request.Mute})
	})
	client.hub.Handle("match.abort", func(_ []byte) (any, error) { return send("abortMatch", nil) })
	client.hub.Handle("match.commit", func(_ []byte) (any, error) { return send("commitAndPost", nil) })
	client.hub.Handle("match.discard", func(_ []byte) (any, error) { return send("discardResults", nil) })
	client.hub.Handle("match.signalReset", func(_ []byte) (any, error) { return send("signalReset", nil) })
	client.hub.Handle("match.signalVolunteers", func(_ []byte) (any, error) { return send("signalVolunteers", nil) })
	client.hub.Handle("match.bypass", func(args []byte) (any, error) {
		var request struct {
			Station string `json:"station"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		return send("toggleBypass", request.Station)
	})
	client.hub.Handle("match.audienceDisplay", func(args []byte) (any, error) {
		var request struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		return send("setAudienceDisplay", request.Mode)
	})
	client.hub.Handle("match.allianceStationDisplay", func(args []byte) (any, error) {
		var request struct {
			Mode string `json:"mode"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		return send("setAllianceStationDisplay", request.Mode)
	})
	client.hub.Handle("arena.matches", func(args []byte) (any, error) {
		var request struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		var matches []json.RawMessage
		if err := client.GetJSON("/api/matches/"+url.PathEscape(request.Type), &matches); err != nil {
			return nil, err
		}
		return matches, nil
	})
}

// GetJSON performs an authenticated GET against the arena and decodes the JSON response.
func (client *Client) GetJSON(path string, target any) error {
	request, err := http.NewRequest("GET", client.cfg.ArenaURL()+path, nil)
	if err != nil {
		return err
	}
	for key, values := range client.authHeader() {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	httpClient := http.Client{Timeout: 5 * time.Second}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("GET %s: %s %s", path, response.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(response.Body).Decode(target)
}

// PostForm performs an authenticated form POST against the arena.
func (client *Client) PostForm(path string, values url.Values) error {
	request, err := http.NewRequest("POST", client.cfg.ArenaURL()+path, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for key, headerValues := range client.authHeader() {
		for _, value := range headerValues {
			request.Header.Add(key, value)
		}
	}
	httpClient := http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("POST %s: %s %s", path, response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// PostJSON performs an authenticated JSON POST against the arena.
func (client *Client) PostJSON(path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequest("POST", client.cfg.ArenaURL()+path, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	httpClient := http.Client{Timeout: 3 * time.Second}
	response, err := httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("POST %s: %s %s", path, response.Status, strings.TrimSpace(string(data)))
	}
	return nil
}

// authHeader logs in with the configured admin password (if any) and returns the session cookie header.
func (client *Client) authHeader() http.Header {
	password := client.cfg.Arena.AdminPassword
	if password == "" {
		return nil
	}
	client.mu.RLock()
	cookie := client.sessionCookie
	client.mu.RUnlock()
	if cookie == "" {
		httpClient := http.Client{
			Timeout: 3 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		}
		response, err := httpClient.PostForm(
			client.cfg.ArenaURL()+"/login", url.Values{"username": {"admin"}, "password": {password}},
		)
		if err != nil {
			return nil
		}
		_ = response.Body.Close()
		for _, responseCookie := range response.Cookies() {
			if responseCookie.Name == "session_token" {
				cookie = responseCookie.Value
			}
		}
		client.mu.Lock()
		client.sessionCookie = cookie
		client.mu.Unlock()
	}
	if cookie == "" {
		return nil
	}
	return http.Header{"Cookie": {"session_token=" + cookie}}
}
