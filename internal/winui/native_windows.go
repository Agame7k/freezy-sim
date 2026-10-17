//go:build windows

package winui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
	"unsafe"

	win "golang.org/x/sys/windows"
)

var (
	user32                  = win.NewLazySystemDLL("user32.dll")
	shell32                 = win.NewLazySystemDLL("shell32.dll")
	ole32                   = win.NewLazySystemDLL("ole32.dll")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procMessageBoxW         = user32.NewProc("MessageBoxW")
	procGetWindowPlacement  = user32.NewProc("GetWindowPlacement")
	procSetWindowPlacement  = user32.NewProc("SetWindowPlacement")
	procMonitorFromRect     = user32.NewProc("MonitorFromRect")
	procIsWindow            = user32.NewProc("IsWindow")
	procSHBrowseForFolderW  = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDList = shell32.NewProc("SHGetPathFromIDListW")
	procCoInitializeEx      = ole32.NewProc("CoInitializeEx")
	procCoTaskMemFree       = ole32.NewProc("CoTaskMemFree")
)

// SingleInstance claims the "only Freezy Sim running" lock. If another copy already holds it, that copy's main window
// is brought to the front and false is returned.
func SingleInstance(title string) bool {
	name, _ := win.UTF16PtrFromString(`Local\FreezySimMainInstance`)
	handle, err := win.CreateMutex(nil, false, name)
	if err == win.ERROR_ALREADY_EXISTS {
		if handle != 0 {
			_ = win.CloseHandle(handle)
		}
		focusWindow(title)
		return false
	}
	// The mutex is held (never closed) for the life of the process.
	return true
}

func focusWindow(title string) {
	titlePtr, _ := win.UTF16PtrFromString(title)
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return
	}
	const swRestore = 9
	procShowWindow.Call(hwnd, swRestore)
	procSetForegroundWindow.Call(hwnd)
}

// Alert shows a native message box (for problems the windowed exe cannot print to a console).
func Alert(title, message string) {
	titlePtr, _ := win.UTF16PtrFromString(title)
	messagePtr, _ := win.UTF16PtrFromString(message)
	const mbIconError = 0x10
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(messagePtr)), uintptr(unsafe.Pointer(titlePtr)), mbIconError)
}

type browseInfo struct {
	owner       uintptr
	root        uintptr
	displayName *uint16
	title       *uint16
	flags       uint32
	callback    uintptr
	lParam      uintptr
	image       int32
}

// PickFolder shows the Windows folder picker and returns the chosen folder ("" if cancelled).
func PickFolder(title string) string {
	result := make(chan string, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		const coinitApartmentThreaded = 0x2
		procCoInitializeEx.Call(0, coinitApartmentThreaded)
		display := make([]uint16, win.MAX_PATH)
		titlePtr, _ := win.UTF16PtrFromString(title)
		const bifReturnOnlyFsDirs, bifNewDialogStyle = 0x1, 0x40
		info := browseInfo{displayName: &display[0], title: titlePtr, flags: bifReturnOnlyFsDirs | bifNewDialogStyle}
		pidl, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&info)))
		if pidl == 0 {
			result <- ""
			return
		}
		defer procCoTaskMemFree.Call(pidl)
		path := make([]uint16, win.MAX_PATH)
		ok, _, _ := procSHGetPathFromIDList.Call(pidl, uintptr(unsafe.Pointer(&path[0])))
		if ok == 0 {
			result <- ""
			return
		}
		result <- win.UTF16ToString(path)
	}()
	return <-result
}

// ---- Remembered window placement ----------------------------------------------------------------------------------

type point struct{ X, Y int32 }
type rect struct{ Left, Top, Right, Bottom int32 }

// windowPlacement is the Win32 WINDOWPLACEMENT structure.
type windowPlacement struct {
	Length         uint32
	Flags          uint32
	ShowCmd        uint32
	MinPosition    point
	MaxPosition    point
	NormalPosition rect
}

var (
	placementsMu sync.Mutex
	placements   map[string]windowPlacement
)

func placementsPath() string { return filepath.Join(filepath.Dir(dataPath()), "windows.json") }

func loadPlacements() {
	placementsMu.Lock()
	defer placementsMu.Unlock()
	if placements != nil {
		return
	}
	placements = map[string]windowPlacement{}
	if data, err := os.ReadFile(placementsPath()); err == nil {
		_ = json.Unmarshal(data, &placements)
	}
}

func savePlacements() {
	placementsMu.Lock()
	data, _ := json.MarshalIndent(placements, "", "  ")
	placementsMu.Unlock()
	_ = os.MkdirAll(filepath.Dir(placementsPath()), 0o755)
	_ = os.WriteFile(placementsPath(), data, 0o644)
}

// restorePlacement puts a window back where it was last time (if that spot is still on a connected monitor).
func restorePlacement(hwnd uintptr, key string) {
	loadPlacements()
	placementsMu.Lock()
	saved, ok := placements[key]
	placementsMu.Unlock()
	if !ok || hwnd == 0 {
		return
	}
	const monitorDefaultToNull = 0
	r := saved.NormalPosition
	if monitor, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), monitorDefaultToNull); monitor == 0 {
		return // that monitor is gone
	}
	saved.Length = uint32(unsafe.Sizeof(saved))
	const swShowMinimized = 2
	if saved.ShowCmd == swShowMinimized {
		saved.ShowCmd = 1 // never come back minimized
	}
	procSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&saved)))
}

// trackPlacement records a window's placement while it is open (it cannot be read after the window is destroyed) and
// saves it when the window closes (stop is closed).
func trackPlacement(hwnd uintptr, key string, stop <-chan struct{}) {
	if hwnd == 0 {
		return
	}
	loadPlacements()
	read := func() {
		if alive, _, _ := procIsWindow.Call(hwnd); alive == 0 {
			return
		}
		var placement windowPlacement
		placement.Length = uint32(unsafe.Sizeof(placement))
		if ok, _, _ := procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&placement))); ok != 0 {
			placementsMu.Lock()
			placements[key] = placement
			placementsMu.Unlock()
		}
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				savePlacements()
				return
			case <-ticker.C:
				read()
			}
		}
	}()
}
