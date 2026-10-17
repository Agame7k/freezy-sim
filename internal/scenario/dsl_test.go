package scenario

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompareAndTruthy(t *testing.T) {
	cases := []struct {
		value   any
		op, lit string
		want    bool
	}{
		{true, "==", "true", true},
		{false, "==", "true", false},
		{float64(3), ">=", "3", true},
		{float64(2.5), "<", "3", true},
		{"PostMatch", "==", "postmatch", true},
		{"R", "!=", "B", true},
		{nil, "==", "false", true},
	}
	for _, c := range cases {
		got, err := compare(c.value, c.op, c.lit)
		if err != nil || got != c.want {
			t.Errorf("compare(%v %s %s) = %v, %v; want %v", c.value, c.op, c.lit, got, err, c.want)
		}
	}
	if _, err := compare("x", ">", "y"); err == nil {
		t.Error("string ordering should be an error")
	}
}

func TestWalkIsCaseInsensitive(t *testing.T) {
	document := asDocument(struct {
		DsConn struct{ RobotLinked bool }
		List   []int
	}{List: []int{4, 5}})
	value, err := walk(document, []string{"dsconn", "robotlinked"})
	if err != nil || value != false {
		t.Fatalf("got %v, %v", value, err)
	}
	if value, err := walk(document, []string{"list", "1"}); err != nil || value != float64(5) {
		t.Fatalf("list index: %v, %v", value, err)
	}
	if _, err := walk(document, []string{"nope"}); err == nil {
		t.Fatal("missing field should be an error")
	}
}

// The shipped JSON scenarios all parse, and a broken file becomes a failing scenario rather than vanishing.
func TestLoadScenarios(t *testing.T) {
	shipped := LoadScenarios(filepath.Join("..", "..", ScenarioDir))
	if len(shipped) < 3 {
		t.Fatalf("only %d shipped JSON scenarios", len(shipped))
	}
	for _, s := range shipped {
		if len(s.Id) > 5 && s.Id[:5] == "file-" {
			t.Errorf("%s did not parse: %s", s.Id, s.Description)
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{"steps": [`), 0o644); err != nil {
		t.Fatal(err)
	}
	broken := LoadScenarios(dir)
	if len(broken) != 1 || broken[0].Id != "file-broken" {
		t.Fatalf("broken file loaded as %+v", broken)
	}
}
