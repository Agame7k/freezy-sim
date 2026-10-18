package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckSourceDir(t *testing.T) {
	root := t.TempDir()
	arena := filepath.Join(root, "cheesy-arena-main")
	if err := os.MkdirAll(arena, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(arena, "go.mod"), []byte("module github.com/Team254/cheesy-arena\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CheckSourceDir(arena); err != nil {
		t.Fatalf("a real arena folder was rejected: %v", err)
	}
	if err := CheckSourceDir(exampleSourceDir); err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("the example placeholder should be called out, got %v", err)
	}
	// Choosing the folder the zip was extracted into points at the arena inside it.
	if err := CheckSourceDir(root); err == nil || !strings.Contains(err.Error(), arena) {
		t.Fatalf("want a pointer to %s, got %v", arena, err)
	}
	if err := CheckSourceDir(filepath.Join(root, "missing")); err == nil {
		t.Fatal("a missing folder was accepted")
	}
}
