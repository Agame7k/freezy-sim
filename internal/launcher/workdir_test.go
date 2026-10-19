package launcher

import (
	"strings"
	"testing"
)

// A sandbox deep in a long folder (an extracted zip inside OneDrive, a temp folder) used to fail with only Windows'
// "The directory name is invalid".
func TestCheckWorkingDir(t *testing.T) {
	short := `C:\freezy-sim\run\arena`
	long := `C:\Users\someone\OneDrive - Some Long Organization Name\` + strings.Repeat(`deeply-nested-folder\`, 10) + `run\arena`
	if len(long) <= maxWorkingDir {
		t.Fatalf("test path is only %d characters", len(long))
	}
	if err := checkWorkingDir(short, "windows"); err != nil {
		t.Errorf("short path rejected: %v", err)
	}
	if err := checkWorkingDir(long, "linux"); err != nil {
		t.Errorf("long path rejected off Windows: %v", err)
	}
	err := checkWorkingDir(long, "windows")
	if err == nil {
		t.Fatal("long path accepted on Windows")
	}
	for _, want := range []string{"arena.sandboxDir", "shorter path", long} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
	if err := checkWorkingDir(strings.Repeat("a", maxWorkingDir), "windows"); err != nil {
		t.Errorf("path of exactly the limit rejected: %v", err)
	}
}
