package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, content string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "freezy-sim.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestLoadAcceptsNotepadBOM(t *testing.T) {
	cfg, err := load(t, "\xef\xbb\xbf{\"arena\": {\"sourceDir\": \"C:/arena\"}}")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Arena.SourceDir != "C:/arena" {
		t.Fatalf("sourceDir = %q", cfg.Arena.SourceDir)
	}
}

func TestLoadExplainsBackslashPaths(t *testing.T) {
	_, err := load(t, "{\n  \"arena\": {\"sourceDir\": \"C:\\Users\\me\\arena\"}\n}")
	if err == nil || !strings.Contains(err.Error(), "line 2") || !strings.Contains(err.Error(), "forward slashes") {
		t.Fatalf("want a line number and the backslash hint, got %v", err)
	}
}

func TestLoadExplainsWrongType(t *testing.T) {
	_, err := load(t, `{"gui": {"port": "8090"}}`)
	if err == nil || !strings.Contains(err.Error(), "gui.port") {
		t.Fatalf("want the setting named, got %v", err)
	}
}

func TestLoadWarnsAboutMisspelledKeys(t *testing.T) {
	cfg, err := load(t, `{"arena": {"sorceDir": "C:/arena"}}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings) != 1 || !strings.Contains(cfg.Warnings[0], "sorceDir") {
		t.Fatalf("warnings = %v", cfg.Warnings)
	}
}
