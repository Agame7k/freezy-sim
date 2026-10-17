package launcher

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Building a specific arena commit: the commit is exported with `git archive` (which only reads the arena's
// repository) into run/arena-src/<commit>/, and the sandbox is built and its assets copied from there. This is how the
// simulator compares two arena versions without ever checking anything out in the arena folder.

// WorktreeRef names the arena folder as it is on disk (uncommitted changes included).
const WorktreeRef = "worktree"

// SetRef selects the arena version the next Start builds ("" or WorktreeRef = the arena folder as it is).
func (launcher *Launcher) SetRef(ref string) {
	if ref == WorktreeRef {
		ref = ""
	}
	launcher.mu.Lock()
	launcher.ref = ref
	launcher.status.Ref = ref
	launcher.status.Commit = ""
	launcher.mu.Unlock()
	launcher.publish()
}

// exportRef returns a directory holding the arena source at ref, exporting it on first use.
func (launcher *Launcher) exportRef(source, ref string) (string, error) {
	commit, err := git(source, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err != nil || commit == "" {
		return "", fmt.Errorf("arena ref %q is not a commit in %s", ref, source)
	}
	launcher.mu.Lock()
	launcher.status.Commit = commit
	launcher.mu.Unlock()
	root, err := filepath.Abs(filepath.Join("run", "arena-src"))
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, commit[:12])
	if fileExists(filepath.Join(dir, "go.mod")) {
		return dir, nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	launcher.setState("preparing", fmt.Sprintf("Exporting arena %s (%s) with git archive", ref, commit[:12]))
	archive := filepath.Join(root, commit[:12]+".zip")
	defer os.Remove(archive)
	if _, err := git(source, "archive", "--format=zip", "-o", archive, commit); err != nil {
		return "", err
	}
	staging := dir + ".partial"
	_ = os.RemoveAll(staging)
	if err := unzip(archive, staging); err != nil {
		_ = os.RemoveAll(staging)
		return "", fmt.Errorf("unpacking %s: %w", ref, err)
	}
	// OneDrive and virus scanners briefly hold freshly written files, which makes the rename fail; retry a little.
	var renameErr error
	for attempt := 0; attempt < 20; attempt++ {
		if renameErr = os.Rename(staging, dir); renameErr == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if renameErr != nil {
		return "", renameErr
	}
	launcher.hub.Logf("launcher", "Exported arena %s (%s) to %s", ref, commit[:12], dir)
	return dir, nil
}

// git runs a read-only git command in dir and returns its trimmed output.
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	hideWindow(cmd)
	output, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(string(exit.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(output)), nil
}

func unzip(path, dest string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	for _, file := range reader.File {
		target := filepath.Join(dest, filepath.FromSlash(file.Name))
		if !isWithin(target, dest) && !samePath(target, dest) {
			return fmt.Errorf("archive entry %q escapes the export folder", file.Name)
		}
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := file.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			in.Close()
			return err
		}
		_, err = io.Copy(out, in)
		in.Close()
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// clearExcept deletes everything under dir except keep (and the folders leading to it).
func clearExcept(dir, keep string) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		switch {
		case samePath(path, keep):
			continue
		case entry.IsDir() && isWithin(keep, path):
			if err := clearExcept(path, keep); err != nil {
				return err
			}
		default:
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	return nil
}
