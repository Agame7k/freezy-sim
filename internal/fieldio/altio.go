package fieldio

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/inspector"
)

// Esp32Box emulates one Freezy ESP32 E-stop box: it answers the arena's TCP health probe on port 80 and POSTs its
// buttons' states to /api/freezy/eStopState.
type Esp32Box struct {
	Name     string
	IP       string
	Port     int
	Channels []int

	mu        sync.Mutex
	online    bool
	server    *http.Server
	listening bool
	bindErr   string
	lastSent  map[int]bool
	lastPost  time.Time
	postErr   string
	posts     int
	probes    int
}

// BoxStatus is the published state of an ESP32 box.
type BoxStatus struct {
	Name      string    `json:"name"`
	Address   string    `json:"address"`
	Channels  []int     `json:"channels"`
	Online    bool      `json:"online"`
	Listening bool      `json:"listening"`
	BindError string    `json:"bindError"`
	LastPost  time.Time `json:"lastPost"`
	PostError string    `json:"postError"`
	Posts     int       `json:"posts"`
	Probes    int       `json:"probes"`
}

func newBox(name, ip string, port int, channels []int) *Esp32Box {
	return &Esp32Box{Name: name, IP: ip, Port: port, Channels: channels, online: true}
}

func (box *Esp32Box) address() string {
	return net.JoinHostPort(box.IP, strconv.Itoa(box.Port))
}

// ensureListening opens or closes the health/status HTTP server to match the box's online state.
func (box *Esp32Box) ensureListening(want bool) {
	box.mu.Lock()
	defer box.mu.Unlock()
	want = want && box.online
	if want && box.server == nil {
		listener, err := net.Listen("tcp", box.address())
		if err != nil {
			box.bindErr = err.Error()
			return
		}
		box.bindErr = ""
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			box.mu.Lock()
			channels := box.Channels
			sent := box.lastSent
			box.mu.Unlock()
			fmt.Fprintf(w, "Freezy Sim ESP32 E-stop box %q\nchannels: %v\nlast sent: %v\n", box.Name, channels, sent)
		})
		box.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second,
			ConnState: func(conn net.Conn, state http.ConnState) {
				if state == http.StateNew {
					box.mu.Lock()
					box.probes++
					box.mu.Unlock()
				}
			}}
		box.listening = true
		go func(server *http.Server) { _ = server.Serve(listener) }(box.server)
	} else if !want && box.server != nil {
		_ = box.server.Close()
		box.server = nil
		box.listening = false
		box.lastSent = nil
	}
}

func (box *Esp32Box) status() BoxStatus {
	box.mu.Lock()
	defer box.mu.Unlock()
	return BoxStatus{
		Name: box.Name, Address: box.address(), Channels: box.Channels, Online: box.online, Listening: box.listening,
		BindError: box.bindErr, LastPost: box.lastPost, PostError: box.postErr, Posts: box.posts, Probes: box.probes,
	}
}

type stopState struct {
	Channel int  `json:"channel"`
	State   bool `json:"state"`
}

// sync POSTs the box's channels if they changed or the refresh interval elapsed.
func (box *Esp32Box) sync(inputs [InputCount]bool, post func(payload []stopState) error, force bool) {
	box.mu.Lock()
	if !box.online || !box.listening {
		box.mu.Unlock()
		return
	}
	changed := box.lastSent == nil || force
	for _, channel := range box.Channels {
		if box.lastSent != nil && box.lastSent[channel] != inputs[channel] {
			changed = true
		}
	}
	if !changed {
		box.mu.Unlock()
		return
	}
	payload := make([]stopState, 0, len(box.Channels))
	sent := make(map[int]bool, len(box.Channels))
	for _, channel := range box.Channels {
		payload = append(payload, stopState{Channel: channel, State: inputs[channel]})
		sent[channel] = inputs[channel]
	}
	box.mu.Unlock()

	err := post(payload)
	result := "ok"
	if err != nil {
		result = err.Error()
	}
	inspector.Note("altio", "out", box.Name, fmt.Sprintf("POST /api/freezy/eStopState %v → %s", payload, result), nil)

	box.mu.Lock()
	box.lastPost = time.Now()
	box.posts++
	if err != nil {
		box.postErr = err.Error()
		box.lastSent = nil
	} else {
		box.postErr = ""
		box.lastSent = sent
	}
	box.mu.Unlock()
}

func (box *Esp32Box) setOnline(online bool) {
	box.mu.Lock()
	box.online = online
	box.mu.Unlock()
}
