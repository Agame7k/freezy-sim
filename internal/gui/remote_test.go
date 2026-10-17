package gui

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/config"
)

func guarded(remote, token, bind string) http.Handler {
	cfg := &config.Config{GUI: config.GUIConfig{Bind: bind, Port: 8090, Remote: remote, Token: token}}
	server := &Server{cfg: cfg, hub: bus.New()}
	return server.guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) }))
}

func status(handler http.Handler, remoteAddr, target string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest("GET", target, nil)
	request.RemoteAddr = remoteAddr
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func TestGuardLocalOnly(t *testing.T) {
	handler := guarded("", "", "127.0.0.1")
	if got := status(handler, "127.0.0.1:5000", "/", nil).Code; got != http.StatusTeapot {
		t.Fatalf("loopback refused: %d", got)
	}
	if got := status(handler, "100.101.102.103:5000", "/", nil).Code; got != http.StatusForbidden {
		t.Fatalf("remote device admitted with remote access off: %d", got)
	}
}

func TestGuardTailscale(t *testing.T) {
	handler := guarded("tailscale", "", "127.0.0.1")
	for _, addr := range []string{"100.101.102.103:5000", "[fd7a:115c:a1e0::1]:5000", "[::1]:5000"} {
		if got := status(handler, addr, "/", nil).Code; got != http.StatusTeapot {
			t.Fatalf("%s refused: %d", addr, got)
		}
	}
	if got := status(handler, "192.168.1.20:5000", "/", nil).Code; got != http.StatusForbidden {
		t.Fatalf("LAN device admitted in tailscale mode: %d", got)
	}
}

func TestGuardLANToken(t *testing.T) {
	handler := guarded("lan", "s3cret", "127.0.0.1")
	if got := status(handler, "192.168.1.20:5000", "/", nil).Code; got != http.StatusUnauthorized {
		t.Fatalf("no token admitted: %d", got)
	}
	if got := status(handler, "192.168.1.20:5000", "/?token=wrong", nil).Code; got != http.StatusUnauthorized {
		t.Fatalf("wrong token admitted: %d", got)
	}
	// The right token sets the cookie and redirects to the page without it.
	first := status(handler, "192.168.1.20:5000", "/?panel=match&token=s3cret", nil)
	if first.Code != http.StatusFound || first.Header().Get("Location") != "/?panel=match" {
		t.Fatalf("token login: %d %q", first.Code, first.Header().Get("Location"))
	}
	cookies := first.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("no cookie set")
	}
	if got := status(handler, "192.168.1.20:5000", "/?panel=match", cookies[0]).Code; got != http.StatusTeapot {
		t.Fatalf("cookie not accepted: %d", got)
	}
}

func TestGuardLegacyBind(t *testing.T) {
	handler := guarded("", "", "0.0.0.0")
	if got := status(handler, "192.168.1.20:5000", "/", nil).Code; got != http.StatusTeapot {
		t.Fatalf("hand-set network bind no longer honored: %d", got)
	}
}

func TestGuardRefusesCrossSiteRequests(t *testing.T) {
	handler := guarded("", "", "127.0.0.1")
	request := func(method, target, origin string, websocket bool) int {
		r := httptest.NewRequest(method, target, nil)
		r.RemoteAddr = "127.0.0.1:5000"
		r.Host = "127.0.0.1:8090"
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if websocket {
			r.Header.Set("Upgrade", "websocket")
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, r)
		return recorder.Code
	}
	cases := []struct {
		name, method, target, origin string
		websocket                    bool
		want                         int
	}{
		{"own page sends a command", "POST", "/api/cmd/match.start", "http://127.0.0.1:8090", false, http.StatusTeapot},
		{"own page opens the websocket", "GET", "/ws", "http://127.0.0.1:8090", true, http.StatusTeapot},
		{"curl sends a command (no Origin)", "POST", "/api/cmd/match.start", "", false, http.StatusTeapot},
		{"another site loads a page", "GET", "/", "https://evil.example", false, http.StatusTeapot},
		{"another site sends a command", "POST", "/api/cmd/match.start", "https://evil.example", false, http.StatusForbidden},
		{"another site uploads a model", "PUT", "/api/field-model", "http://localhost:9999", false, http.StatusForbidden},
		{"another site opens the websocket", "GET", "/ws", "https://evil.example", true, http.StatusForbidden},
		{"a sandboxed frame opens the websocket", "GET", "/ws/world", "null", true, http.StatusForbidden},
	}
	for _, c := range cases {
		if got := request(c.method, c.target, c.origin, c.websocket); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}
