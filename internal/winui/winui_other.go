//go:build !windows

package winui

// Available reports whether native windows are supported on this platform.
const Available = false

type Info struct {
	Id    int    `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

func RunMain(title, url string, width, height int, debug bool) bool { return false }

func Open(title, url string, width, height int, debug bool) {}

func List() []Info { return nil }

func Close(id int) {}

// SingleInstance always succeeds where there are no native windows.
func SingleInstance(title string) bool { return true }

// Alert has no native dialog here; the message goes to the log.
func Alert(title, message string) {}

// PickFolder is unavailable without native windows.
func PickFolder(title string) string { return "" }
