package gui

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Remote access: by default the GUI only listens on loopback. gui.remote opens it to other devices:
//
//	"tailscale"  also listen on this PC's Tailscale addresses (the tailnet already authenticates its devices); new
//	             Tailscale addresses are picked up while running, so it works even if Tailscale connects later
//	"lan"        listen on every interface; other devices need the access token (gui.token, or a random one printed
//	             at startup) once, after which a cookie remembers them
//
// Loopback always stays open so the native window keeps working.

const (
	RemoteOff       = ""
	RemoteTailscale = "tailscale"
	RemoteLAN       = "lan"

	tokenCookie = "freezy_sim_token"
)

var (
	tailscaleV4 = mustCIDR("100.64.0.0/10")
	tailscaleV6 = mustCIDR("fd7a:115c:a1e0::/48")
)

func mustCIDR(text string) *net.IPNet {
	_, network, err := net.ParseCIDR(text)
	if err != nil {
		panic(err)
	}
	return network
}

func isTailscale(ip net.IP) bool { return tailscaleV4.Contains(ip) || tailscaleV6.Contains(ip) }

// tailscaleAddrs returns this machine's Tailscale IPs.
func tailscaleAddrs() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && isTailscale(ipNet.IP) {
			out = append(out, ipNet.IP)
		}
	}
	return out
}

// lanAddrs returns this machine's non-loopback unicast IPs (for printing reachable URLs).
func lanAddrs() []net.IP {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, addr := range addrs {
		if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && !ipNet.IP.IsLinkLocalUnicast() &&
			!ipNet.IP.IsLinkLocalMulticast() {
			out = append(out, ipNet.IP)
		}
	}
	return out
}

// mergedListener accepts connections from several listeners, and can take more while running.
type mergedListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once

	mu        sync.Mutex
	listeners map[string]net.Listener
}

func newMergedListener() *mergedListener {
	return &mergedListener{conns: make(chan net.Conn), closed: make(chan struct{}), listeners: map[string]net.Listener{}}
}

// add starts accepting on address unless it is already being served.
func (merged *mergedListener) add(address string) error {
	merged.mu.Lock()
	defer merged.mu.Unlock()
	if _, ok := merged.listeners[address]; ok {
		return nil
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	merged.listeners[address] = listener
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				merged.mu.Lock()
				delete(merged.listeners, address)
				merged.mu.Unlock()
				return
			}
			select {
			case merged.conns <- conn:
			case <-merged.closed:
				conn.Close()
				return
			}
		}
	}()
	return nil
}

func (merged *mergedListener) Accept() (net.Conn, error) {
	select {
	case conn := <-merged.conns:
		return conn, nil
	case <-merged.closed:
		return nil, net.ErrClosed
	}
}

func (merged *mergedListener) Close() error {
	merged.once.Do(func() {
		close(merged.closed)
		merged.mu.Lock()
		for _, listener := range merged.listeners {
			listener.Close()
		}
		merged.mu.Unlock()
	})
	return nil
}

func (merged *mergedListener) Addr() net.Addr {
	merged.mu.Lock()
	defer merged.mu.Unlock()
	keys := make([]string, 0, len(merged.listeners))
	for key := range merged.listeners {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return &net.TCPAddr{}
	}
	return merged.listeners[keys[0]].Addr()
}

// Listen binds the GUI port (loopback, plus whatever gui.remote opens); it fails fast if the port is taken.
func (server *Server) Listen() (net.Listener, error) {
	port := strconv.Itoa(server.cfg.GUI.Port)
	merged := newMergedListener()
	switch server.remote() {
	case RemoteLAN:
		if err := merged.add(net.JoinHostPort("", port)); err != nil {
			return nil, err
		}
	case RemoteTailscale:
		if err := merged.add(net.JoinHostPort(server.cfg.GUI.Bind, port)); err != nil {
			return nil, err
		}
		server.addTailscale(merged, port)
		// Tailscale may connect (or change address) after the simulator starts.
		go func() {
			for {
				select {
				case <-merged.closed:
					return
				case <-time.After(15 * time.Second):
					server.addTailscale(merged, port)
				}
			}
		}()
	default:
		if err := merged.add(net.JoinHostPort(server.cfg.GUI.Bind, port)); err != nil {
			return nil, err
		}
	}
	return merged, nil
}

func (server *Server) addTailscale(merged *mergedListener, port string) {
	addrs := tailscaleAddrs()
	if len(addrs) == 0 {
		server.warnOnce("no-tailscale", "gui.remote is \"tailscale\" but this PC has no Tailscale address yet; "+
			"remote devices can connect once Tailscale is up")
		return
	}
	for _, ip := range addrs {
		address := net.JoinHostPort(ip.String(), port)
		merged.mu.Lock()
		_, known := merged.listeners[address]
		merged.mu.Unlock()
		if known {
			continue
		}
		if err := merged.add(address); err != nil {
			server.warnOnce("ts-"+address, fmt.Sprintf("Could not listen on Tailscale address %s: %v", address, err))
			continue
		}
		server.hub.Logf("gui", "Remote access over Tailscale: http://%s/", address)
		server.publishRemote()
	}
}

func (server *Server) warnOnce(key, text string) {
	server.mu.Lock()
	if server.warned == nil {
		server.warned = map[string]bool{}
	}
	seen := server.warned[key]
	server.warned[key] = true
	server.mu.Unlock()
	if !seen {
		server.hub.Warnf("gui", "%s", text)
	}
}

func (server *Server) remote() string {
	return strings.ToLower(strings.TrimSpace(server.cfg.GUI.Remote))
}

// token returns the LAN access token, generating one for this run if none is configured.
func (server *Server) token() string {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.cfg.GUI.Token != "" {
		return server.cfg.GUI.Token
	}
	if server.runToken == "" {
		var raw [12]byte
		_, _ = rand.Read(raw[:])
		server.runToken = hex.EncodeToString(raw[:])
	}
	return server.runToken
}

// RemoteURLs lists the addresses other devices can open. Only withToken adds the LAN access token: the token must
// never reach a bus topic or the event log, because both are recorded into the black box and bug-report bundles that
// people attach to public issues.
func (server *Server) RemoteURLs(withToken bool) []string {
	port := strconv.Itoa(server.cfg.GUI.Port)
	var ips []net.IP
	query := ""
	switch server.remote() {
	case RemoteTailscale:
		ips = tailscaleAddrs()
	case RemoteLAN:
		ips = lanAddrs()
		if withToken {
			query = "?token=" + url.QueryEscape(server.token())
		}
	default:
		return nil
	}
	var out []string
	for _, ip := range ips {
		out = append(out, "http://"+net.JoinHostPort(ip.String(), port)+"/"+query)
	}
	return out
}

func (server *Server) publishRemote() {
	server.hub.Publish("gui.remote", map[string]any{"mode": server.remote(), "urls": server.RemoteURLs(false),
		"tokenRequired": server.remote() == RemoteLAN})
}

// registerRemoteCommands lets the Arena panel show the LAN links with the token (a command reply is not recorded).
func (server *Server) registerRemoteCommands() {
	server.hub.Handle("gui.remoteLinks", func(_ []byte) (any, error) {
		return map[string]any{"urls": server.RemoteURLs(true)}, nil
	})
}

// sameOrigin refuses what a web page on another site could do through a browser on this PC (which reaches loopback
// like any local program): open a websocket, or send a command or an upload. Plain page and asset loads, and requests
// without an Origin header (curl, scripts, the native window's own navigation), are allowed.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	websocket := strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
	if !websocket && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host != "" && strings.EqualFold(parsed.Host, r.Host)
}

// guard admits loopback always; Tailscale peers in tailscale mode; anyone with the token in LAN mode. Whoever it
// admits, a cross-site request (sameOrigin) is refused.
func (server *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, "Freezy Sim refuses requests from other web sites", http.StatusForbidden)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if err != nil || ip == nil {
			http.Error(w, "bad remote address", http.StatusForbidden)
			return
		}
		if ip.IsLoopback() {
			next.ServeHTTP(w, r)
			return
		}
		switch server.remote() {
		case RemoteTailscale:
			if isTailscale(ip) {
				next.ServeHTTP(w, r)
				return
			}
		case RemoteLAN:
			ok, answered := server.tokenOK(w, r)
			if answered {
				return
			}
			if ok {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "Freezy Sim: open the address printed at startup (it ends in ?token=...)", http.StatusUnauthorized)
			return
		default:
			// gui.bind set to a network address by hand (before gui.remote existed): keep honoring it.
			if bind := net.ParseIP(server.cfg.GUI.Bind); bind == nil || !bind.IsLoopback() {
				next.ServeHTTP(w, r)
				return
			}
		}
		http.Error(w, "Freezy Sim does not accept connections from "+host, http.StatusForbidden)
	})
}

// tokenOK accepts the token from the cookie or the query string. A page opened with ?token= gets the cookie and is
// redirected to the same address without the token (answered = true: the response is already written).
func (server *Server) tokenOK(w http.ResponseWriter, r *http.Request) (ok, answered bool) {
	want := []byte(server.token())
	if cookie, err := r.Cookie(tokenCookie); err == nil && subtle.ConstantTimeCompare([]byte(cookie.Value), want) == 1 {
		return true, false
	}
	given := r.URL.Query().Get("token")
	if given == "" || subtle.ConstantTimeCompare([]byte(given), want) != 1 {
		return false, false
	}
	http.SetCookie(w, &http.Cookie{Name: tokenCookie, Value: given, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600})
	if r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/ws") && !strings.HasPrefix(r.URL.Path, "/api/") {
		clean := *r.URL
		query := clean.Query()
		query.Del("token")
		clean.RawQuery = query.Encode()
		http.Redirect(w, r, clean.RequestURI(), http.StatusFound)
		return true, true
	}
	return true, false
}
