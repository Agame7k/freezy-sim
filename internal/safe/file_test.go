package safe

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileReplacesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A failed write must leave the previous file intact and no temp files behind.
	err := WriteFileFunc(path, 0o644, func(out io.Writer) error {
		_, _ = out.Write([]byte("partial"))
		return errors.New("disk full")
	})
	if err == nil {
		t.Fatal("expected the write error")
	}
	if data, _ := os.ReadFile(path); string(data) != "old" {
		t.Fatalf("file is %q after a failed save, want the old contents", data)
	}
	if err := WriteFile(path, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "new" {
		t.Fatalf("file is %q, want new", data)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("expected only the target file, found %d entries", len(entries))
	}
}
