package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agame7k/freezy-sim/internal/world"
	"github.com/gorilla/websocket"
)

// Every robot the simulator can build must draw: testdata/robotcheck.js builds the 3D model of each one (from
// world.ModelSpecs, so new parts in the kit are covered without touching the test) in a headless Edge or Chrome and
// checks it stays inside its frame, bumpers and height. Skipped with -short, and where no browser is installed
// (except in CI, where it fails instead). FREEZY_TEST_BROWSER names a browser to use.
func TestRobotModelsFitEveryDesign(t *testing.T) {
	if testing.Short() {
		t.Skip("draws every robot design in a headless browser")
	}
	browser := findBrowser()
	if browser == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("no Edge or Chrome to draw the robot models in; set FREEZY_TEST_BROWSER")
		}
		t.Skip("no Edge or Chrome installed (set FREEZY_TEST_BROWSER)")
	}
	specs := world.ModelSpecs()
	if len(specs) < 100 {
		t.Fatalf("world.ModelSpecs listed only %d designs", len(specs))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/specs.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(specs)
	})
	mux.Handle("/", http.FileServer(http.Dir(".")))
	server := httptest.NewServer(mux)
	defer server.Close()

	result := struct {
		Built    int      `json:"built"`
		Specs    int      `json:"specs"`
		Problems []string `json:"problems"`
		Error    string   `json:"error"`
	}{}
	raw := runInBrowser(t, browser, server.URL+"/testdata/robotcheck.html", "window.robotCheck", 4*time.Minute)
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("reading the check's result %q: %v", raw, err)
	}
	if result.Error != "" {
		t.Fatalf("the robot model check failed to run: %s", result.Error)
	}
	if result.Specs != len(specs) || result.Built <= result.Specs {
		t.Fatalf("drew %d robots from %d designs; want all %d designs plus the stock robots", result.Built, result.Specs,
			len(specs))
	}
	for i, problem := range result.Problems {
		if i == 40 {
			t.Errorf("... and %d more", len(result.Problems)-i)
			break
		}
		t.Error(problem)
	}
	t.Logf("drew %d robots (%d designs and the stock robots)", result.Built, result.Specs)
}

// findBrowser returns the path of an installed Edge or Chrome, or "".
func findBrowser() string {
	if path := os.Getenv("FREEZY_TEST_BROWSER"); path != "" {
		return path
	}
	var candidates []string
	for _, root := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LocalAppData")} {
		if root != "" {
			candidates = append(candidates, filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"),
				filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"))
		}
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	for _, name := range []string{"microsoft-edge", "google-chrome", "chromium", "chromium-browser", "chrome", "msedge"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return ""
}

// runInBrowser opens url in a headless browser and returns JSON.stringify(expression) once it is set, over the
// DevTools protocol.
func runInBrowser(t *testing.T, browser, url, expression string, timeout time.Duration) string {
	t.Helper()
	profile, err := os.MkdirTemp("", "freezy-sim-browser-")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(browser, "--headless=new", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
		"--disable-extensions", "--remote-debugging-port=0", "--user-data-dir="+profile, url)
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v", browser, err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	var conn *websocket.Conn
	defer func() {
		if conn != nil {
			_ = conn.WriteJSON(map[string]any{"id": 0, "method": "Browser.close"})
			conn.Close()
		}
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
		for i := 0; i < 10 && os.RemoveAll(profile) != nil; i++ { // the browser's helpers may still hold files
			time.Sleep(300 * time.Millisecond)
		}
	}()

	// The browser writes its DevTools port to the profile once it is listening.
	deadline := time.Now().Add(30 * time.Second)
	var port string
	for port == "" {
		if data, err := os.ReadFile(filepath.Join(profile, "DevToolsActivePort")); err == nil {
			port = strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
		} else if time.Now().After(deadline) {
			t.Fatalf("%s did not open its DevTools port", browser)
		} else {
			time.Sleep(200 * time.Millisecond)
		}
	}
	var pages []struct {
		Type   string `json:"type"`
		URL    string `json:"url"`
		Socket string `json:"webSocketDebuggerUrl"`
	}
	for len(pages) == 0 || !strings.HasPrefix(pages[0].URL, url) {
		if time.Now().After(deadline) {
			t.Fatalf("%s never opened %s", browser, url)
		}
		time.Sleep(200 * time.Millisecond)
		pages = pages[:0]
		response, err := http.Get("http://127.0.0.1:" + port + "/json/list")
		if err != nil {
			continue
		}
		var all []struct {
			Type   string `json:"type"`
			URL    string `json:"url"`
			Socket string `json:"webSocketDebuggerUrl"`
		}
		_ = json.NewDecoder(response.Body).Decode(&all)
		response.Body.Close()
		for _, page := range all {
			if page.Type == "page" && strings.HasPrefix(page.URL, url) {
				pages = append(pages, page)
			}
		}
	}
	conn, _, err = websocket.DefaultDialer.Dial(pages[0].Socket, nil)
	if err != nil {
		t.Fatalf("connecting to the browser: %v", err)
	}

	deadline = time.Now().Add(timeout)
	for id := 1; ; id++ {
		err := conn.WriteJSON(map[string]any{"id": id, "method": "Runtime.evaluate", "params": map[string]any{
			"expression": fmt.Sprintf("JSON.stringify(%s ?? null)", expression), "returnByValue": true}})
		if err != nil {
			t.Fatalf("asking the browser: %v", err)
		}
		var reply struct {
			ID     int `json:"id"`
			Result struct {
				Result struct {
					Value string `json:"value"`
				} `json:"result"`
			} `json:"result"`
		}
		for reply.ID != id { // (skips any events)
			_ = conn.SetReadDeadline(time.Now().Add(time.Minute))
			if err := conn.ReadJSON(&reply); err != nil {
				t.Fatalf("reading from the browser: %v", err)
			}
		}
		if value := reply.Result.Result.Value; value != "" && value != "null" {
			return value
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s was not set within %s", expression, timeout)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
