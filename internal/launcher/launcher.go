// Package launcher prepares an isolated copy of Freezy Arena under run/arena and supervises it. The arena's own
// folder is only ever read: the binary is built with `go build -o <sandbox>` (or the existing exe is copied) and the
// web assets are copied, so event.db and match logs are written in the sandbox instead.
package launcher

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agame7k/freezy-sim/internal/bus"
	"github.com/Agame7k/freezy-sim/internal/config"
	"github.com/Agame7k/freezy-sim/internal/safe"
)

const exeName = "cheesy-arena.exe"

// Directories (relative to the arena root) the arena reads at runtime.
var assetDirs = []string{"static", "templates", "schedules", "font"}

// Subdirectories that are runtime output and must not be copied from the source.
var skipDirs = map[string]bool{filepath.Join("static", "logs"): true}

type Status struct {
	State      string    `json:"state"` // idle, preparing, building, copying, starting, running, stopped, exited, external, error
	Detail     string    `json:"detail"`
	Pid        int       `json:"pid"`
	StartedAt  time.Time `json:"startedAt"`
	ExitCode   int       `json:"exitCode"`
	Binary     string    `json:"binary"` // "built" or "copied"
	SandboxDir string    `json:"sandboxDir"`
	SourceDir  string    `json:"sourceDir"`
	URL        string    `json:"url"`
	Ready      bool      `json:"ready"`
	Mode       string    `json:"mode"`
	Ref        string    `json:"ref,omitempty"`    // arena git ref being built (ref.go); "" = the arena folder as it is
	Commit     string    `json:"commit,omitempty"` // the commit that ref resolved to
	// NeedsSource: the arena folder is missing or wrong and the user must choose one; Suggestions are arena folders
	// found in the usual places.
	NeedsSource bool     `json:"needsSource,omitempty"`
	Suggestions []string `json:"suggestions,omitempty"`
}

type Launcher struct {
	cfg *config.Config
	hub *bus.Hub

	mu      sync.Mutex
	status  Status
	cmd     *exec.Cmd
	job     jobHandle
	stopReq bool
	crashes []time.Time
	ref     string     // arena git ref to build instead of the folder as it is (ref.go)
	startMu sync.Mutex // one Start (prepare + launch) at a time
}

func New(cfg *config.Config, hub *bus.Hub) *Launcher {
	launcher := &Launcher{cfg: cfg, hub: hub}
	launcher.status = Status{
		State:      "idle",
		SandboxDir: launcher.sandboxDir(),
		SourceDir:  cfg.Arena.SourceDir,
		URL:        cfg.ArenaURL(),
		Mode:       cfg.Arena.Mode,
	}
	hub.Handle("arena.start", func(_ []byte) (any, error) { go launcher.Start(); return nil, nil })
	hub.Handle("arena.stop", func(_ []byte) (any, error) { return nil, launcher.Stop() })
	hub.Handle("arena.restart", func(_ []byte) (any, error) {
		go func() {
			defer safe.Recover(launcher.hub, "launcher")
			_ = launcher.Stop()
			launcher.Start()
		}()
		return nil, nil
	})
	hub.Handle("arena.rebuild", func(_ []byte) (any, error) {
		go func() {
			defer safe.Recover(launcher.hub, "launcher")
			_ = launcher.Stop()
			_ = os.Remove(filepath.Join(launcher.sandboxDir(), exeName))
			launcher.Start()
		}()
		return nil, nil
	})
	hub.Handle("arena.resetDb", func(_ []byte) (any, error) { return nil, launcher.ResetDatabase() })
	hub.Handle("arena.setSource", func(args []byte) (any, error) {
		var request struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(args, &request); err != nil {
			return nil, err
		}
		return nil, launcher.SetSource(request.Path)
	})
	hub.Handle("arena.findSources", func(_ []byte) (any, error) { return FindArenaFolders(), nil })
	launcher.publish()
	return launcher
}

// Status returns a snapshot of the launcher state.
func (launcher *Launcher) Status() Status {
	launcher.mu.Lock()
	defer launcher.mu.Unlock()
	return launcher.status
}

func (launcher *Launcher) sandboxDir() string {
	dir := launcher.cfg.Arena.SandboxDir
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	return dir
}

func (launcher *Launcher) setState(state, detail string) {
	launcher.mu.Lock()
	launcher.status.State = state
	launcher.status.Detail = detail
	launcher.mu.Unlock()
	launcher.publish()
}

func (launcher *Launcher) publish() {
	launcher.mu.Lock()
	status := launcher.status
	launcher.mu.Unlock()
	launcher.hub.Publish("arena.launcher", status)
}

// Start prepares the sandbox (if needed) and launches the arena. In external mode it only waits for the arena.
func (launcher *Launcher) Start() {
	launcher.startMu.Lock()
	defer launcher.startMu.Unlock()
	launcher.mu.Lock()
	if launcher.cmd != nil {
		launcher.mu.Unlock()
		return
	}
	launcher.stopReq = false
	launcher.mu.Unlock()

	if launcher.cfg.Arena.Mode == config.ArenaExternal {
		launcher.setState("external", "Attaching to an arena already running at "+launcher.cfg.ArenaURL())
		safe.Async(launcher.hub, "launcher", launcher.waitReady)
		return
	}

	if arenaResponding(launcher.cfg.ArenaURL()) {
		launcher.setState(
			"external",
			"An arena is already running on "+launcher.cfg.ArenaURL()+"; attaching to it instead of launching the sandbox",
		)
		launcher.hub.Warnf("launcher", "Port %d is already serving an arena; attaching instead of launching",
			launcher.cfg.Arena.HTTPPort)
		safe.Async(launcher.hub, "launcher", launcher.waitReady)
		return
	}

	if err := launcher.prepare(); err != nil {
		launcher.setState("error", err.Error())
		launcher.hub.Errorf("launcher", "Sandbox preparation failed: %v", err)
		return
	}
	if err := launcher.launch(); err != nil {
		launcher.setState("error", err.Error())
		launcher.hub.Errorf("launcher", "Failed to start arena: %v", err)
		return
	}
}

func (launcher *Launcher) prepare() error {
	if err := launcher.sourceProblem(); err != nil {
		return err
	}
	source := launcher.cfg.Arena.SourceDir
	sandbox := launcher.sandboxDir()
	if samePath(source, sandbox) || isWithin(sandbox, source) {
		return fmt.Errorf("refusing to use a sandbox inside the arena source folder (%s)", sandbox)
	}
	if err := os.MkdirAll(sandbox, 0755); err != nil {
		return err
	}
	launcher.mu.Lock()
	ref := launcher.ref
	launcher.mu.Unlock()
	if ref != "" {
		exported, err := launcher.exportRef(source, ref)
		if err != nil {
			return err
		}
		source = exported
	}
	// The sync below only copies changed files; a different arena version needs its own assets exactly, so start
	// from empty asset folders whenever the source changes (match logs are kept).
	marker := filepath.Join(sandbox, ".arena-source")
	if previous, _ := os.ReadFile(marker); string(previous) != source {
		for _, dir := range assetDirs {
			if err := clearExcept(filepath.Join(sandbox, dir), filepath.Join(sandbox, "static", "logs")); err != nil {
				return fmt.Errorf("clearing old %s: %w", dir, err)
			}
		}
		if err := os.WriteFile(marker, []byte(source), 0o644); err != nil {
			return err
		}
	}

	launcher.setState("preparing", "Syncing arena assets into the sandbox")
	copied := 0
	for _, dir := range assetDirs {
		n, err := syncTree(filepath.Join(source, dir), filepath.Join(sandbox, dir), dir)
		if err != nil {
			return fmt.Errorf("copying %s: %w", dir, err)
		}
		copied += n
	}
	if err := os.MkdirAll(filepath.Join(sandbox, "static", "logs"), 0755); err != nil {
		return err
	}
	if copied > 0 {
		launcher.hub.Logf("launcher", "Copied %d updated asset files into the sandbox", copied)
	}

	exePath := filepath.Join(sandbox, exeName)
	if ref != "" {
		// A specific version must be that version: never fall back to whatever exe is lying around.
		launcher.setState("building", "Building arena "+ref+" from its exported source")
		if err := launcher.build(source, exePath); err != nil {
			return fmt.Errorf("building arena %s: %w", ref, err)
		}
		launcher.mu.Lock()
		launcher.status.Binary = "built " + ref
		launcher.mu.Unlock()
		return nil
	}
	var buildErr error
	if launcher.cfg.Arena.PreferBuild && fileExists(filepath.Join(source, "go.mod")) {
		launcher.setState("building", "Building arena from source (go build -o sandbox)")
		if buildErr = launcher.build(source, exePath); buildErr == nil {
			launcher.mu.Lock()
			launcher.status.Binary = "built"
			launcher.mu.Unlock()
			return nil
		}
		launcher.hub.Warnf("launcher", "Build failed, falling back to the prebuilt exe: %v", buildErr)
	}

	launcher.setState("copying", "Copying prebuilt "+exeName)
	sourceExe := filepath.Join(source, exeName)
	if !fileExists(sourceExe) {
		if buildErr != nil {
			return fmt.Errorf("the arena build failed and there is no prebuilt %s in the arena folder: %w", exeName, buildErr)
		}
		return fmt.Errorf("no prebuilt %s in the arena folder (set arena.preferBuild to true to build it from source)",
			exeName)
	}
	if err := copyFile(sourceExe, exePath); err != nil {
		return err
	}
	launcher.mu.Lock()
	launcher.status.Binary = "copied"
	launcher.mu.Unlock()
	return nil
}

func (launcher *Launcher) build(source, exePath string) error {
	if _, err := exec.LookPath("go"); err != nil {
		return errors.New("Go is not installed or not on PATH; the arena is built from source with it " +
			"(install Go from https://go.dev/dl/, or put a prebuilt " + exeName + " in the arena folder)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// -mod=readonly guarantees go.mod/go.sum in the arena folder are never rewritten.
	cmd := exec.CommandContext(ctx, "go", "build", "-mod=readonly", "-buildvcs=false", "-o", exePath, ".")
	cmd.Dir = source
	cmd.Env = append(os.Environ(), "GOFLAGS=")
	hideWindow(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
	}
	launcher.hub.Logf("launcher", "Built arena from source into %s", exePath)
	return nil
}

func (launcher *Launcher) launch() error {
	sandbox := launcher.sandboxDir()
	exePath := filepath.Join(sandbox, exeName)
	args := append([]string{"-dev"}, launcher.cfg.Arena.ExtraArgs...)
	cmd := exec.Command(exePath, args...)
	cmd.Dir = sandbox
	hideWindow(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	launcher.setState("starting", "Launching "+exePath+" -dev")
	if err := cmd.Start(); err != nil {
		return err
	}
	job, err := attachKillOnCloseJob(cmd)
	if err != nil {
		launcher.hub.Warnf("launcher", "Could not attach arena to a kill-on-close job: %v", err)
	}

	launcher.mu.Lock()
	launcher.cmd = cmd
	launcher.job = job
	launcher.status.Pid = cmd.Process.Pid
	launcher.status.StartedAt = time.Now()
	launcher.status.ExitCode = 0
	launcher.status.Ready = false
	launcher.mu.Unlock()
	launcher.publish()
	launcher.hub.Logf("launcher", "Arena started (pid %d) in %s", cmd.Process.Pid, sandbox)

	safe.Async(launcher.hub, "launcher", func() { launcher.pumpLogs(stdout) })
	safe.Async(launcher.hub, "launcher", launcher.waitReady)
	go func() {
		defer safe.Recover(launcher.hub, "launcher")
		err := cmd.Wait()
		launcher.mu.Lock()
		exitCode := cmd.ProcessState.ExitCode()
		stopRequested := launcher.stopReq
		launcher.cmd = nil
		closeJob(launcher.job)
		launcher.job = 0
		launcher.status.Pid = 0
		launcher.status.ExitCode = exitCode
		launcher.status.Ready = false
		launcher.mu.Unlock()
		if stopRequested {
			launcher.setState("stopped", "Stopped by user")
			launcher.hub.Logf("launcher", "Arena stopped")
		} else {
			launcher.setState("exited", fmt.Sprintf("Arena exited unexpectedly (code %d): %v", exitCode, err))
			launcher.hub.Errorf("launcher", "Arena exited unexpectedly with code %d", exitCode)
			launcher.restartAfterCrash()
		}
	}()
	return nil
}

// restartAfterCrash relaunches an arena that died on its own (at most 3 times in 10 minutes, so a broken build does
// not loop forever). The arena client's reconnect then re-applies settings and reloads a match.
func (launcher *Launcher) restartAfterCrash() {
	launcher.mu.Lock()
	now := time.Now()
	var recent []time.Time
	for _, at := range launcher.crashes {
		if now.Sub(at) < 10*time.Minute {
			recent = append(recent, at)
		}
	}
	launcher.crashes = append(recent, now)
	attempts := len(launcher.crashes)
	launcher.mu.Unlock()
	if attempts > 3 {
		launcher.hub.Errorf("launcher", "The arena crashed %d times in 10 minutes; not restarting it again. "+
			"Check the arena log, then press Start in the Arena panel.", attempts)
		return
	}
	launcher.hub.Warnf("launcher", "Restarting the arena in 2 s (attempt %d of 3)", attempts)
	go func() {
		defer safe.Recover(launcher.hub, "launcher")
		time.Sleep(2 * time.Second)
		launcher.mu.Lock()
		stopped := launcher.stopReq
		launcher.mu.Unlock()
		if !stopped {
			launcher.Start()
		}
	}()
}

func (launcher *Launcher) waitReady() {
	url := launcher.cfg.ArenaURL()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		launcher.mu.Lock()
		state := launcher.status.State
		launcher.mu.Unlock()
		if state == "exited" || state == "stopped" || state == "error" {
			return
		}
		if arenaResponding(url) {
			launcher.mu.Lock()
			launcher.status.Ready = true
			if launcher.status.State == "starting" {
				launcher.status.State = "running"
				launcher.status.Detail = "Serving " + url
			}
			launcher.mu.Unlock()
			launcher.publish()
			launcher.hub.Logf("launcher", "Arena web server is up at %s", url)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	launcher.hub.Warnf("launcher", "Arena did not respond at %s within 60s", url)
}

func (launcher *Launcher) pumpLogs(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		// Strip the arena's own date/time prefix ("2026/10/12 17:31:53 ").
		if len(line) > 20 && line[4] == '/' && line[7] == '/' && line[13] == ':' {
			line = line[20:]
		}
		level := "info"
		lower := strings.ToLower(line)
		switch {
		case strings.Contains(lower, "panic") || strings.Contains(lower, "fatal"):
			level = "error"
		case strings.Contains(lower, "error") || strings.Contains(lower, "failed"):
			level = "warn"
		case strings.HasPrefix(lower, "warning"):
			level = "warn"
		}
		launcher.hub.Emit(level, "arena", line)
	}
}

// Stop terminates the sandboxed arena process.
func (launcher *Launcher) Stop() error {
	launcher.mu.Lock()
	cmd := launcher.cmd
	launcher.stopReq = true
	launcher.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := cmd.Process.Kill(); err != nil {
		return err
	}
	for i := 0; i < 50; i++ {
		launcher.mu.Lock()
		done := launcher.cmd == nil
		launcher.mu.Unlock()
		if done {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// ResetDatabase deletes the sandbox event database (only allowed while the arena is stopped).
func (launcher *Launcher) ResetDatabase() error {
	launcher.mu.Lock()
	running := launcher.cmd != nil
	launcher.mu.Unlock()
	if running {
		return errors.New("stop the arena before resetting its database")
	}
	path := filepath.Join(launcher.sandboxDir(), "event.db")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	launcher.hub.Logf("launcher", "Deleted sandbox database %s", path)
	return nil
}

func arenaResponding(url string) bool {
	client := http.Client{Timeout: 700 * time.Millisecond}
	response, err := client.Get(url + "/")
	if err != nil {
		return false
	}
	_ = response.Body.Close()
	return true
}

// PortInUse reports whether something is listening on the given TCP address.
func PortInUse(address string) bool {
	conn, err := net.DialTimeout("tcp", address, 300*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func syncTree(source, dest, rel string) (int, error) {
	if !fileExists(source) {
		return 0, nil
	}
	copied := 0
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(source, path)
		if skipDirs[filepath.Join(rel, relPath)] {
			return filepath.SkipDir
		}
		target := filepath.Join(dest, relPath)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if targetInfo, err := os.Stat(target); err == nil && targetInfo.Size() == info.Size() &&
			!info.ModTime().After(targetInfo.ModTime()) {
			return nil
		}
		copied++
		return copyFile(path, target)
	})
	return copied, err
}

func copyFile(source, dest string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dest + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func samePath(a, b string) bool {
	absA, _ := filepath.Abs(a)
	absB, _ := filepath.Abs(b)
	return strings.EqualFold(filepath.Clean(absA), filepath.Clean(absB))
}

func isWithin(child, parent string) bool {
	absChild, _ := filepath.Abs(child)
	absParent, _ := filepath.Abs(parent)
	rel, err := filepath.Rel(absParent, absChild)
	return err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}
