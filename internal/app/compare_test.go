package app

import (
	"strings"
	"testing"

	"github.com/Agame7k/freezy-sim/internal/scenario"
)

func TestCompareRegressions(t *testing.T) {
	result := func(id, status string) *scenario.Result { return &scenario.Result{Id: id, Status: status} }
	runs := []CompareRun{
		{Ref: "old", Results: []*scenario.Result{result("a", "pass"), result("b", "fail"), result("c", "pass")}},
		{Ref: "new", Results: []*scenario.Result{result("a", "fail"), result("b", "pass"), result("c", "skip")}},
		{Ref: "broken", Problem: "build failed"},
	}
	got := compareRegressions(runs)
	if len(got) != 2 || !strings.HasPrefix(got[0], "a fails on new") || !strings.HasPrefix(got[1], "broken:") {
		t.Fatalf("regressions %q", got)
	}
	if compareRegressions(runs[:1]) != nil {
		t.Fatal("one run cannot regress")
	}
}
