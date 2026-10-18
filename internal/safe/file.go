package safe

import (
	"io"
	"os"
	"path/filepath"
	"time"
)

// WriteFile replaces path with data atomically: a crash mid-save leaves the old file, never a truncated one.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	return WriteFileFunc(path, perm, func(out io.Writer) error {
		_, err := out.Write(data)
		return err
	})
}

// WriteFileFunc is WriteFile for content that is streamed (write is given the temporary file).
func WriteFileFunc(path string, perm os.FileMode, write func(io.Writer) error) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	if err := write(temp); err != nil {
		temp.Close()
		os.Remove(name)
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		os.Remove(name)
		return err
	}
	if err := temp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	_ = os.Chmod(name, perm)
	if err := rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// rename retries briefly: on Windows a virus scanner, the search indexer or OneDrive sync can hold the target open
// for a moment, which fails the replace with "Access is denied".
func rename(from, to string) error {
	var err error
	for attempt := 0; attempt < 10; attempt++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt+1) * 20 * time.Millisecond)
	}
	return err
}
