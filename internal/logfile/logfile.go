// Package logfile sends the simulator's log to a daily file in run/logs (kept for two weeks) as well as the console,
// and routes fatal crash output to a file, so problems in the windowed (no console) exe can still be diagnosed.
package logfile

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

const keepFiles = 14

// Writer is a log destination that switches to a new file each day.
type Writer struct {
	dir     string
	console bool

	mu   sync.Mutex
	day  string
	file *os.File
}

// Setup directs the standard logger to run/logs/freezy-sim-YYYYMMDD.log (and the console when there is one) and
// sends fatal runtime crashes to run/logs/crash-*.txt.
func Setup(dir string, console bool) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	writer := &Writer{dir: dir, console: console}
	writer.mu.Lock()
	err := writer.rotateLocked(time.Now())
	writer.mu.Unlock()
	if err != nil {
		return nil, err
	}
	log.SetOutput(writer)
	crashPath := filepath.Join(dir, fmt.Sprintf("crash-%s.txt", time.Now().Format("20060102-150405")))
	if crash, err := os.Create(crashPath); err == nil {
		if debug.SetCrashOutput(crash, debug.CrashOptions{}) != nil {
			_ = crash.Close()
			_ = os.Remove(crashPath)
		} else {
			// The file is only interesting if something was written to it; remove empty ones from earlier runs.
			go pruneEmptyCrashFiles(dir, crashPath)
		}
	}
	return writer, nil
}

// Path is the current log file.
func (writer *Writer) Path() string {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.file == nil {
		return ""
	}
	return writer.file.Name()
}

func (writer *Writer) Write(p []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	now := time.Now()
	if now.Format("20060102") != writer.day {
		_ = writer.rotateLocked(now)
	}
	if writer.file != nil {
		_, _ = writer.file.Write(p)
	}
	if writer.console {
		// Ignore console errors: a windowed exe has no usable stderr.
		_, _ = os.Stderr.Write(p)
	}
	return len(p), nil
}

// Close closes the current file.
func (writer *Writer) Close() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.file == nil {
		return nil
	}
	err := writer.file.Close()
	writer.file = nil
	return err
}

func (writer *Writer) rotateLocked(now time.Time) error {
	day := now.Format("20060102")
	file, err := os.OpenFile(filepath.Join(writer.dir, "freezy-sim-"+day+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if writer.file != nil {
		_ = writer.file.Close()
	}
	writer.file, writer.day = file, day
	prune(writer.dir, "freezy-sim-", ".log")
	return nil
}

// prune keeps the newest keepFiles logs.
func prune(dir, prefix, suffix string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), prefix) && strings.HasSuffix(entry.Name(), suffix) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keepFiles {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

func pruneEmptyCrashFiles(dir, current string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if !strings.HasPrefix(entry.Name(), "crash-") || path == current {
			continue
		}
		if info, err := entry.Info(); err == nil && info.Size() == 0 {
			_ = os.Remove(path)
		}
	}
	prune(dir, "crash-", ".txt")
}
