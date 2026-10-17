// Package gui serves the simulator front end and bridges it to the bus over websockets.
package gui

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/config"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 64 * 1024,
	CheckOrigin:     sameOrigin, // (the guard checks this too; remote.go)
}

// BinaryStream is a source of binary frames (e.g. world snapshots) pushed to clients on /ws/world.
type BinaryStream interface {
	Subscribe() (<-chan []byte, func())
}

type Server struct {
	cfg    *config.Config
	hub    *bus.Hub
	assets fs.FS
	mux    *http.ServeMux

	mu       sync.Mutex
	world    BinaryStream
	runToken string          // LAN access token for this run when gui.token is empty (remote.go)
	warned   map[string]bool // remote-access warnings already logged
}

// New creates the GUI server. If webDir is non-empty, assets are served live from disk (for front-end development).
func New(cfg *config.Config, hub *bus.Hub, embedded fs.FS, webDir string) *Server {
	assets := embedded
	if webDir != "" {
		assets = os.DirFS(webDir)
		log.Printf("Serving GUI assets from disk: %s", webDir)
	}
	server := &Server{cfg: cfg, hub: hub, assets: assets, mux: http.NewServeMux()}
	server.mux.Handle("/", noCache(http.FileServer(http.FS(assets))))
	server.mux.HandleFunc("/ws", server.handleWebsocket)
	server.mux.HandleFunc("/ws/world", server.handleWorldWebsocket)
	server.mux.HandleFunc("/api/cmd/{command}", server.handleCommand)
	server.mux.HandleFunc("/api/config", server.handleConfig)
	server.mux.HandleFunc("GET /api/topic/{topic}", server.handleTopic)
	server.mux.HandleFunc("GET /api/events", server.handleEvents)
	server.mux.HandleFunc("GET /models/field-cad.glb", server.handleFieldModelFile) // imported, not embedded (fieldmodel.go)
	server.mux.HandleFunc("/api/field-model", server.handleFieldModel)
	server.registerRemoteCommands()
	return server
}

// SetWorldStream attaches the binary world snapshot stream.
func (server *Server) SetWorldStream(stream BinaryStream) {
	server.mu.Lock()
	server.world = stream
	server.mu.Unlock()
}

// Handle exposes the mux so other subsystems can add HTTP routes.
func (server *Server) Handle(pattern string, handler http.Handler) {
	server.mux.Handle(pattern, handler)
}

// Serve runs the HTTP server on an already-bound listener (Listen, remote.go).
func (server *Server) Serve(listener net.Listener) error {
	server.publishRemote()
	if server.remote() == RemoteLAN { // (Tailscale addresses are logged as they are picked up)
		for _, address := range server.RemoteURLs(false) {
			server.hub.Logf("gui", "Remote access (LAN): %s (needs the access token)", address)
		}
		// The links with the token go to the console only: the event log and the log file end up in bug bundles.
		fmt.Println("Open one of these on the other device (they include the access token; don't share them):")
		for _, address := range server.RemoteURLs(true) {
			fmt.Println("  " + address)
		}
		server.hub.Logf("gui", "The links with the access token are printed in the console and shown in the Arena panel")
	}
	httpServer := &http.Server{Handler: server.guard(server.mux), ReadHeaderTimeout: 10 * time.Second}
	return httpServer.Serve(listener)
}

type clientMessage struct {
	Type string          `json:"type"`
	Id   int64           `json:"id"`
	Cmd  string          `json:"cmd"`
	Args json.RawMessage `json:"args"`
}

type replyMessage struct {
	Type   string `json:"type"`
	Id     int64  `json:"id"`
	Ok     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Result any    `json:"result,omitempty"`
}

func (server *Server) handleWebsocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	sub := server.hub.Subscribe()
	defer server.hub.Unsubscribe(sub)

	var writeMu sync.Mutex
	write := func(messageType int, payload []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		return conn.WriteMessage(messageType, payload)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for message := range sub.C {
			if err := write(websocket.TextMessage, message); err != nil {
				return
			}
		}
	}()

	for {
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var message clientMessage
		if err := json.Unmarshal(payload, &message); err != nil || message.Type != "cmd" {
			continue
		}
		go func(message clientMessage) {
			result, err := server.hub.Dispatch(message.Cmd, message.Args)
			reply := replyMessage{Type: "reply", Id: message.Id, Ok: err == nil, Result: result}
			if err != nil {
				reply.Error = err.Error()
			}
			data, _ := json.Marshal(reply)
			_ = write(websocket.TextMessage, data)
		}(message)
		select {
		case <-done:
			return
		default:
		}
	}
}

func (server *Server) handleWorldWebsocket(w http.ResponseWriter, r *http.Request) {
	server.mu.Lock()
	stream := server.world
	server.mu.Unlock()
	if stream == nil {
		http.Error(w, "world stream not available", http.StatusServiceUnavailable)
		return
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	frames, cancel := stream.Subscribe()
	defer cancel()

	go func() {
		// Drain reads so close frames are processed.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()
	for frame := range frames {
		_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			return
		}
	}
}

// handleCommand lets scripts drive the simulator over plain HTTP: POST /api/cmd/<command> with JSON args.
func (server *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var args json.RawMessage
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	result, err := server.hub.Dispatch(r.PathValue("command"), args)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

// handleTopic returns the latest JSON published on a bus topic (handy for scripts and tests).
func (server *Server) handleTopic(w http.ResponseWriter, r *http.Request) {
	data := server.hub.Get(r.PathValue("topic"))
	if data == nil {
		http.Error(w, "unknown topic", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (server *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(server.hub.Events())
}

func (server *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(server.cfg)
}

func noCache(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		handler.ServeHTTP(w, r)
	})
}
