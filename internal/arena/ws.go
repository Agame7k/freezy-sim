package arena

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Message is the envelope used by every Cheesy/Freezy Arena websocket.
type Message struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// Socket is a websocket to the arena that reconnects automatically.
type Socket struct {
	Name      string
	URL       string
	Header    func() http.Header
	OnMessage func(msgType string, data json.RawMessage)
	OnState   func(connected bool, err error)

	mu        sync.Mutex
	conn      *websocket.Conn
	connected bool
	stopped   bool
	lastError string
	received  int64
}

// SocketStatus is the published health of one socket.
type SocketStatus struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	Connected bool   `json:"connected"`
	LastError string `json:"lastError"`
	Received  int64  `json:"received"`
}

// Run connects and reads until Stop is called, reconnecting with backoff.
func (socket *Socket) Run() {
	backoff := 500 * time.Millisecond
	for {
		socket.mu.Lock()
		if socket.stopped {
			socket.mu.Unlock()
			return
		}
		socket.mu.Unlock()

		var header http.Header
		if socket.Header != nil {
			header = socket.Header()
		}
		dialer := websocket.Dialer{HandshakeTimeout: 3 * time.Second}
		conn, _, err := dialer.Dial(socket.URL, header)
		if err != nil {
			socket.setState(false, err)
			time.Sleep(backoff)
			if backoff < 3*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 500 * time.Millisecond
		socket.mu.Lock()
		socket.conn = conn
		socket.mu.Unlock()
		socket.setState(true, nil)

		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				socket.setState(false, err)
				break
			}
			var message Message
			if err := json.Unmarshal(payload, &message); err != nil {
				continue
			}
			socket.mu.Lock()
			socket.received++
			socket.mu.Unlock()
			if socket.OnMessage != nil {
				socket.OnMessage(message.Type, message.Data)
			}
		}
		socket.mu.Lock()
		socket.conn = nil
		socket.mu.Unlock()
		_ = conn.Close()
		time.Sleep(backoff)
	}
}

// Send writes a message to the arena if connected.
func (socket *Socket) Send(msgType string, data any) error {
	socket.mu.Lock()
	defer socket.mu.Unlock()
	if socket.conn == nil {
		return errNotConnected
	}
	_ = socket.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	return socket.conn.WriteJSON(map[string]any{"type": msgType, "data": data})
}

// Stop closes the socket permanently.
func (socket *Socket) Stop() {
	socket.mu.Lock()
	socket.stopped = true
	if socket.conn != nil {
		_ = socket.conn.Close()
	}
	socket.mu.Unlock()
}

// Status returns the socket's health.
func (socket *Socket) Status() SocketStatus {
	socket.mu.Lock()
	defer socket.mu.Unlock()
	return SocketStatus{
		Name: socket.Name, URL: socket.URL, Connected: socket.connected, LastError: socket.lastError,
		Received: socket.received,
	}
}

// Connected reports whether the socket is currently open.
func (socket *Socket) Connected() bool {
	socket.mu.Lock()
	defer socket.mu.Unlock()
	return socket.connected
}

func (socket *Socket) setState(connected bool, err error) {
	socket.mu.Lock()
	changed := socket.connected != connected
	socket.connected = connected
	if err != nil {
		socket.lastError = err.Error()
	} else if connected {
		socket.lastError = ""
	}
	socket.mu.Unlock()
	if changed && socket.OnState != nil {
		socket.OnState(connected, err)
	}
}

func wsURL(httpURL, path string) string {
	return strings.Replace(httpURL, "http", "ws", 1) + path
}
