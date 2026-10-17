// Package netsim emulates the field network the arena manages: the Vivid-style access point (HTTP API), the Cisco-style
// field switch (telnet) and the two alliance SCC switches (SSH). They feed back into the simulated driver stations: a
// robot radio links only to an SSID the AP is broadcasting, and a DS reaches the FMS only while its SCC port is up.
package netsim

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/inspector"
)

var apStations = [6]string{"red1", "red2", "red3", "blue1", "blue2", "blue3"}

// StationInfo is what the AP needs to know about the robot in a station.
type StationInfo struct {
	Team      int
	RadioUp   bool
	X, Y      float64
	Enabled   bool
	ExtraMbps float64
}

type apStationConfig struct {
	Ssid   string `json:"ssid"`
	WpaKey string `json:"wpaKey"`
}

// APStationStatus is the per-station status in the AP's API (field names match the arena's client).
type APStationStatus struct {
	Ssid              string  `json:"ssid"`
	HashedWpaKey      string  `json:"hashedWpaKey"`
	WpaKeySalt        string  `json:"wpaKeySalt"`
	IsLinked          bool    `json:"isLinked"`
	RxRateMbps        float64 `json:"rxRateMbps"`
	TxRateMbps        float64 `json:"txRateMbps"`
	SignalNoiseRatio  int     `json:"signalNoiseRatio"`
	BandwidthUsedMbps float64 `json:"bandwidthUsedMbps"`
	ConnectionQuality string  `json:"connectionQuality"`
}

// AccessPoint emulates the field access point's HTTP API.
type AccessPoint struct {
	address  string
	password string
	info     func(index int) StationInfo
	apX, apY float64

	mu           sync.Mutex
	online       bool
	server       *http.Server
	bindErr      string
	channel      int
	status       string
	config       map[string]apStationConfig
	pending      map[string]apStationConfig
	activeAt     time.Time
	requests     int
	configures   int
	lastConfigAt time.Time
	interference float64
}

func NewAccessPoint(bind string, port int, password string, info func(int) StationInfo, apX, apY float64) *AccessPoint {
	return &AccessPoint{
		address: net.JoinHostPort(bind, strconv.Itoa(port)), password: password, info: info, apX: apX, apY: apY,
		online: true, status: "ACTIVE", channel: 37, config: map[string]apStationConfig{},
	}
}

func (ap *AccessPoint) start() {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	if ap.server != nil || !ap.online {
		return
	}
	listener, err := net.Listen("tcp", ap.address)
	if err != nil {
		ap.bindErr = err.Error()
		return
	}
	ap.bindErr = ""
	mux := http.NewServeMux()
	mux.HandleFunc("/status", ap.handleStatus)
	mux.HandleFunc("/configuration", ap.handleConfiguration)
	// Every request the arena makes, for the protocol inspector (bodies are not kept: they carry WPA keys).
	logged := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		mux.ServeHTTP(recorder, r)
		inspector.Note("ap", "in", r.RemoteAddr, fmt.Sprintf("%s %s → %d", r.Method, r.URL.Path, recorder.status), nil)
	})
	ap.server = &http.Server{Handler: logged, ReadHeaderTimeout: 5 * time.Second}
	go func(server *http.Server) { _ = server.Serve(listener) }(ap.server)
}

func (ap *AccessPoint) stop() {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	if ap.server != nil {
		_ = ap.server.Close()
		ap.server = nil
	}
}

// statusRecorder remembers the status code a handler wrote.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *statusRecorder) WriteHeader(status int) {
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

func (ap *AccessPoint) authorized(r *http.Request) bool {
	if ap.password == "" {
		return true
	}
	return r.Header.Get("Authorization") == "Bearer "+ap.password
}

func (ap *AccessPoint) handleConfiguration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !ap.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var request struct {
		Channel               int                        `json:"channel"`
		StationConfigurations map[string]apStationConfig `json:"stationConfigurations"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ap.mu.Lock()
	if request.Channel != 0 {
		ap.channel = request.Channel
	}
	ap.pending = request.StationConfigurations
	ap.status = "CONFIGURING"
	// Real APs take several seconds to bring the new SSIDs up.
	ap.activeAt = time.Now().Add(3 * time.Second)
	ap.configures++
	ap.lastConfigAt = time.Now()
	ap.mu.Unlock()
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte(`{"status":"CONFIGURING"}`))
}

// tick applies a pending configuration once the AP has "rebooted" its radios.
func (ap *AccessPoint) tick(now time.Time) {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	if ap.status == "CONFIGURING" && now.After(ap.activeAt) {
		ap.config = ap.pending
		if ap.config == nil {
			ap.config = map[string]apStationConfig{}
		}
		ap.pending = nil
		ap.status = "ACTIVE"
	}
}

// Broadcasting reports whether the AP currently serves the given team's SSID (robot radios connect to their team
// number's SSID).
func (ap *AccessPoint) Broadcasting(team int) bool {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	if !ap.online || ap.status != "ACTIVE" {
		return false
	}
	ssid := strconv.Itoa(team)
	for _, config := range ap.config {
		if config.Ssid == ssid {
			return true
		}
	}
	return false
}

// stationStatuses builds the per-station status. It must be called WITHOUT ap.mu held: it calls into the driver
// stations and the world, which in turn ask the AP whether SSIDs are up (holding ap.mu here deadlocked them).
func (ap *AccessPoint) stationStatuses() map[string]*APStationStatus {
	ap.mu.Lock()
	config := make(map[string]apStationConfig, len(ap.config))
	for name, c := range ap.config {
		config[name] = c
	}
	active := ap.status == "ACTIVE"
	interference := ap.interference
	ap.mu.Unlock()

	statuses := map[string]*APStationStatus{}
	for i, name := range apStations {
		config, ok := config[name]
		if !ok || !active {
			continue
		}
		info := ap.info(i)
		team, _ := strconv.Atoi(config.Ssid)
		status := &APStationStatus{Ssid: config.Ssid}
		salt := fmt.Sprintf("%08x", 0x5eed0000+i)
		hash := sha256.Sum256([]byte(config.WpaKey + salt))
		status.WpaKeySalt = salt
		status.HashedWpaKey = hex.EncodeToString(hash[:])
		if info.Team == team && info.RadioUp {
			distance := math.Hypot(info.X-ap.apX, info.Y-ap.apY)
			snr := 52 - 18*math.Log10(math.Max(distance, 1)) - interference + rand.NormFloat64()*1.5
			status.IsLinked = true
			status.SignalNoiseRatio = int(snr)
			status.RxRateMbps = math.Round(math.Max(6, math.Min(866, (snr-8)*22))*10) / 10
			status.TxRateMbps = math.Round(math.Max(6, math.Min(866, (snr-10)*21))*10) / 10
			used := 0.4 + rand.Float64()*0.5 + info.ExtraMbps
			if info.Enabled {
				used += 1.6 + rand.Float64()*1.2
			}
			status.BandwidthUsedMbps = math.Round(used*100) / 100
			switch {
			case snr > 38:
				status.ConnectionQuality = "excellent"
			case snr > 28:
				status.ConnectionQuality = "good"
			case snr > 18:
				status.ConnectionQuality = "warning"
			default:
				status.ConnectionQuality = "caution"
			}
		}
		statuses[name] = status
	}
	return statuses
}

func (ap *AccessPoint) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !ap.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	statuses := ap.stationStatuses()
	ap.mu.Lock()
	ap.requests++
	response := map[string]any{"channel": ap.channel, "status": ap.status, "stationStatuses": statuses}
	ap.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

// APState is the published AP view.
type APState struct {
	Online       bool                        `json:"online"`
	Address      string                      `json:"address"`
	BindError    string                      `json:"bindError"`
	Status       string                      `json:"status"`
	Channel      int                         `json:"channel"`
	Requests     int                         `json:"requests"`
	Configures   int                         `json:"configures"`
	LastConfigAt time.Time                   `json:"lastConfigAt"`
	Interference float64                     `json:"interference"`
	Stations     map[string]*APStationStatus `json:"stations"`
	Ssids        map[string]string           `json:"ssids"`
}

func (ap *AccessPoint) state() APState {
	statuses := ap.stationStatuses()
	ap.mu.Lock()
	defer ap.mu.Unlock()
	ssids := map[string]string{}
	for name, config := range ap.config {
		ssids[name] = config.Ssid + "  (WPA " + strings.Repeat("•", min(len(config.WpaKey), 8)) + ")"
	}
	return APState{
		Online: ap.online, Address: ap.address, BindError: ap.bindErr, Status: ap.status, Channel: ap.channel,
		Requests: ap.requests, Configures: ap.configures, LastConfigAt: ap.lastConfigAt, Interference: ap.interference,
		Stations: statuses, Ssids: ssids,
	}
}
