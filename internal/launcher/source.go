package launcher

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// exampleSourceDir is the placeholder in config.example.json; a copied-but-unedited config still has it.
const exampleSourceDir = "C:/path/to/freezy-arena"

// CheckSourceDir says whether dir is an arena checkout the sandbox can be built from, and if not, what is wrong in
// words a first-time user can act on.
func CheckSourceDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("no arena folder chosen yet")
	}
	if filepath.ToSlash(filepath.Clean(dir)) == exampleSourceDir {
		return fmt.Errorf("arena.sourceDir is still the example placeholder \"%s\": choose your Freezy Arena or Cheesy "+
			"Arena folder", exampleSourceDir)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("arena folder \"%s\" not found", dir)
	}
	if fileExists(filepath.Join(dir, "go.mod")) || fileExists(filepath.Join(dir, exeName)) {
		return nil
	}
	// A common slip: picking the folder above the arena (e.g. the one the zip was extracted into).
	if inner := findArenaIn(dir); inner != "" {
		return fmt.Errorf("\"%s\" is the folder around the arena; choose \"%s\" instead", dir, inner)
	}
	return fmt.Errorf("\"%s\" does not look like a Freezy Arena or Cheesy Arena folder (it has no go.mod or %s); choose the "+
		"folder that holds the arena's go.mod", dir, exeName)
}

// isArena reports whether dir holds a Cheesy Arena (or fork) checkout or a built copy.
func isArena(dir string) bool {
	if fileExists(filepath.Join(dir, exeName)) {
		return true
	}
	file, err := os.Open(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		line := strings.ToLower(scanner.Text())
		return strings.HasPrefix(line, "module ") && strings.Contains(line, "arena")
	}
	return false
}

// findArenaIn returns the first arena folder directly inside dir ("" if none).
func findArenaIn(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.Contains(strings.ToLower(entry.Name()), "arena") {
			if candidate := filepath.Join(dir, entry.Name()); isArena(candidate) {
				return candidate
			}
		}
	}
	return ""
}

// FindArenaFolders looks for arena checkouts where people usually put them: next to the simulator, and in the home,
// Documents, Desktop, Downloads, source and OneDrive folders (one and two levels down). It never searches the disk.
func FindArenaFolders() []string {
	var roots []string
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, filepath.Dir(cwd), filepath.Dir(filepath.Dir(cwd)))
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, home)
		for _, sub := range []string{"Documents", "Desktop", "Downloads", "source", "src", "code", "git", "GitHub",
			filepath.Join("source", "repos"), filepath.Join("Documents", "GitHub")} {
			roots = append(roots, filepath.Join(home, sub))
		}
		if entries, err := os.ReadDir(home); err == nil {
			for _, entry := range entries {
				if entry.IsDir() && strings.HasPrefix(entry.Name(), "OneDrive") {
					oneDrive := filepath.Join(home, entry.Name())
					roots = append(roots, oneDrive, filepath.Join(oneDrive, "Documents"), filepath.Join(oneDrive, "Desktop"))
				}
			}
		}
	}
	for _, root := range []string{`C:\code`, `C:\src`, `C:\dev`} {
		roots = append(roots, root)
	}

	seen := map[string]bool{}
	var found []string
	add := func(dir string) {
		key := strings.ToLower(filepath.Clean(dir))
		if !seen[key] && isArena(dir) {
			seen[key] = true
			found = append(found, filepath.Clean(dir))
		}
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || !strings.Contains(strings.ToLower(entry.Name()), "arena") {
				continue
			}
			dir := filepath.Join(root, entry.Name())
			add(dir)
			if inner := findArenaIn(dir); inner != "" { // e.g. Downloads\cheesy-arena-main\cheesy-arena-main
				add(inner)
			}
		}
	}
	return found
}

// SetSource validates dir, saves it as arena.sourceDir in the config file (creating the file on first run) and
// (re)starts the arena from it.
func (launcher *Launcher) SetSource(dir string) error {
	dir = filepath.Clean(strings.Trim(strings.TrimSpace(dir), `"`))
	if err := CheckSourceDir(dir); err != nil {
		return err
	}
	launcher.cfg.Arena.SourceDir = filepath.ToSlash(dir)
	if err := launcher.cfg.Save(); err != nil {
		return fmt.Errorf("could not save %s: %w", launcher.cfg.Path(), err)
	}
	launcher.mu.Lock()
	launcher.status.SourceDir = launcher.cfg.Arena.SourceDir
	launcher.status.NeedsSource = false
	launcher.status.Suggestions = nil
	launcher.mu.Unlock()
	launcher.hub.Logf("launcher", "Arena folder set to %s (saved in %s)", dir, launcher.cfg.Path())
	go func() {
		_ = launcher.Stop()
		launcher.Start()
	}()
	return nil
}

// sourceProblem handles a missing or wrong arena folder before a start: on a first run with exactly one arena found
// nearby it is used (and saved); otherwise the status asks the user to choose, listing what was found.
func (launcher *Launcher) sourceProblem() error {
	source := launcher.cfg.Arena.SourceDir
	err := CheckSourceDir(source)
	if err == nil {
		return nil
	}
	found := FindArenaFolders()
	firstRun := source == "" || filepath.ToSlash(filepath.Clean(source)) == exampleSourceDir
	if firstRun && len(found) == 1 {
		launcher.cfg.Arena.SourceDir = filepath.ToSlash(found[0])
		if saveErr := launcher.cfg.Save(); saveErr != nil {
			launcher.hub.Warnf("launcher", "Could not save %s: %v", launcher.cfg.Path(), saveErr)
		}
		launcher.mu.Lock()
		launcher.status.SourceDir = launcher.cfg.Arena.SourceDir
		launcher.mu.Unlock()
		launcher.hub.Logf("launcher", "Found the arena at %s and saved it in %s (change it in the Arena panel)",
			found[0], launcher.cfg.Path())
		return nil
	}
	launcher.mu.Lock()
	launcher.status.NeedsSource = true
	launcher.status.Suggestions = found
	launcher.mu.Unlock()
	return fmt.Errorf("%v. Choose the arena folder in the Arena panel (or set arena.sourceDir in %s)", err,
		launcher.cfg.Path())
}
