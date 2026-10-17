//go:build windows

// Package winui hosts the simulator front end in native WebView2 windows. Every window runs on its own locked OS
// thread with its own message loop, so closing a pop-out window never quits the application.
package winui

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

// Available reports whether native windows are supported on this platform.
const Available = true

var (
	mu      sync.Mutex
	windows = map[int]*window{}
	nextId  = 1
)

type window struct {
	id     int
	title  string
	url    string
	view   webview2.WebView
	closed chan struct{}
}

// Info describes an open window.
type Info struct {
	Id    int    `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

func init() {
	// Let the arena's audience display play match sounds without a user gesture.
	if os.Getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS") == "" {
		_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "--autoplay-policy=no-user-gesture-required")
	}
}

func dataPath() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "freezy-sim", "webview2")
}

func create(title, url string, width, height int, debug bool) *window {
	view := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug:     debug,
		AutoFocus: true,
		DataPath:  dataPath(),
		WindowOptions: webview2.WindowOptions{
			Title:  title,
			Width:  uint(width),
			Height: uint(height),
			Center: true,
		},
	})
	if view == nil {
		return nil
	}
	mu.Lock()
	w := &window{id: nextId, title: title, url: url, view: view, closed: make(chan struct{})}
	nextId++
	windows[w.id] = w
	mu.Unlock()
	// Windows reopen where they were last time (same title = same window).
	hwnd := uintptr(view.Window())
	restorePlacement(hwnd, title)
	trackPlacement(hwnd, title, w.closed)
	view.Navigate(url)
	return w
}

// RunMain opens the main window on the calling (main, OS-locked) thread and blocks until it is closed.
func RunMain(title, url string, width, height int, debug bool) bool {
	w := create(title, url, width, height, debug)
	if w == nil {
		return false
	}
	w.view.Run()
	close(w.closed)
	w.view.Destroy()
	mu.Lock()
	delete(windows, w.id)
	mu.Unlock()
	time.Sleep(100 * time.Millisecond) // let the placement save finish
	return true
}

// Open creates an additional top-level window on its own thread.
func Open(title, url string, width, height int, debug bool) {
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		w := create(title, url, width, height, debug)
		if w == nil {
			return
		}
		w.view.Run()
		close(w.closed)
		mu.Lock()
		delete(windows, w.id)
		mu.Unlock()
	}()
}

// List returns the open windows.
func List() []Info {
	mu.Lock()
	defer mu.Unlock()
	infos := make([]Info, 0, len(windows))
	for _, w := range windows {
		infos = append(infos, Info{Id: w.id, Title: w.title, URL: w.url})
	}
	return infos
}

// Close closes a pop-out window.
func Close(id int) {
	mu.Lock()
	w := windows[id]
	mu.Unlock()
	if w != nil {
		w.view.Dispatch(func() { w.view.Destroy() })
	}
}
