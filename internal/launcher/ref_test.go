package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClearExceptKeepsLogs(t *testing.T) {
	root := t.TempDir()
	static := filepath.Join(root, "static")
	logs := filepath.Join(static, "logs")
	for _, path := range []string{filepath.Join(static, "js", "a.js"), filepath.Join(static, "b.css"), filepath.Join(logs, "m.csv")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := clearExcept(static, logs); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(static, "b.css")) || fileExists(filepath.Join(static, "js")) {
		t.Fatal("old assets left behind")
	}
	if !fileExists(filepath.Join(logs, "m.csv")) {
		t.Fatal("match logs deleted")
	}
}
