package web

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every command a GUI button sends must have a handler in the simulator, or the button fails with "unknown command"
// only when someone clicks it. This catches a renamed or removed handler (or a typo in the GUI) at test time.
func TestGUICommandsHaveHandlers(t *testing.T) {
	commandCall := regexp.MustCompile(`\b(?:cmd|run|act)\(\s*"([a-z]+\.[A-Za-z.]+)"`)
	buttonCall := regexp.MustCompile(`cmdButton\([^;]*?,\s*"([a-z]+\.[A-Za-z.]+)"`)
	used := map[string][]string{}
	err := fs.WalkDir(Files, "js", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".js") {
			return err
		}
		source, err := fs.ReadFile(Files, path)
		if err != nil {
			return err
		}
		for _, pattern := range []*regexp.Regexp{commandCall, buttonCall} {
			for _, match := range pattern.FindAllStringSubmatch(string(source), -1) {
				used[match[1]] = append(used[match[1]], path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(used) < 40 {
		t.Fatalf("found only %d GUI commands; the patterns no longer match how panels call commands", len(used))
	}

	// Handlers are registered with their name as a string literal (hub.Handle("x.y", …) or a helper such as
	// action("x.y", …)), so the name appears quoted in non-test Go source.
	var goSource strings.Builder
	err = filepath.WalkDir(filepath.Join("..", "internal"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		source, err := os.ReadFile(path)
		goSource.Write(source)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing []string
	for command, files := range used {
		if !strings.Contains(goSource.String(), `"`+command+`"`) {
			missing = append(missing, command+" (used in "+strings.Join(files, ", ")+")")
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("GUI commands with no handler:\n  %s", strings.Join(missing, "\n  "))
	}
}
