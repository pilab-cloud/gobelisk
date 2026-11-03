package dialplan

import (
	"testing"

	"github.com/devopshaven/gobelisk/pkg/ivr"
)

func applyTestConfig(t *testing.T, text string) *DialPlan {
	t.Helper()
	dp := New()
	if err := ApplyConfig(dp, ivr.NewManager(), text); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	return dp
}

func TestApplyConfigRingGroups(t *testing.T) {
	dp := applyTestConfig(t, `
ringgroup sales -> 100 101 102
ringgroup support strategy round-robin -> ext 200, ext 201
ringgroup vip strategy least-recent -> 300
`)

	sales := dp.RingGroups["sales"]
	if sales == nil {
		t.Fatal("sales ring group missing")
	}
	if sales.Strategy != StrategySimultaneous {
		t.Errorf("sales strategy = %q, want %q", sales.Strategy, StrategySimultaneous)
	}
	if got, want := len(sales.Members), 3; got != want {
		t.Fatalf("sales members = %v, want %d", sales.Members, want)
	}
	if sales.Members[0] != "100" || sales.Members[2] != "102" {
		t.Errorf("sales members = %v", sales.Members)
	}

	support := dp.RingGroups["support"]
	if support == nil {
		t.Fatal("support ring group missing")
	}
	if support.Strategy != StrategyRoundRobin {
		t.Errorf("support strategy = %q", support.Strategy)
	}
	if len(support.Members) != 2 || support.Members[0] != "200" || support.Members[1] != "201" {
		t.Errorf("support members = %v", support.Members)
	}

	if dp.RingGroups["vip"].Strategy != StrategyLeastRecent {
		t.Errorf("vip strategy = %q", dp.RingGroups["vip"].Strategy)
	}
}

func TestParseRingGroupTarget(t *testing.T) {
	dp := applyTestConfig(t, "ringgroup 600 -> 100 101\n")

	cases := []struct {
		dialed string
		want   TargetType
		value  string
	}{
		{"600", TargetRingGroup, "600"},
		{"100", TargetExtension, "100"},
	}
	for _, c := range cases {
		got := dp.Parse(c.dialed)
		if got.Type != c.want || got.Value != c.value {
			t.Errorf("Parse(%q) = {%s %s}, want {%s %s}", c.dialed, got.Type, got.Value, c.want, c.value)
		}
	}

	// A ring group resolves to no single extension; the server forks it.
	if _, ok := dp.ResolveExtension(Target{Type: TargetRingGroup, Value: "600"}); ok {
		t.Error("ResolveExtension should not resolve a ring group")
	}
}

func TestParseRingGroupRouteAction(t *testing.T) {
	dp := applyTestConfig(t, "route 700 -> ringgroup sales\n")

	got := dp.Parse("700")
	if got.Type != TargetRingGroup || got.Value != "sales" {
		t.Errorf("Parse(700) = {%s %s}, want {ringgroup sales}", got.Type, got.Value)
	}
}

func TestParseRingGroupInvalid(t *testing.T) {
	for _, line := range []string{
		"ringgroup bad strategy nope -> 100",
		"ringgroup empty ->",
		"ringgroup noarrow 100 101",
	} {
		dp := New()
		if err := ApplyConfig(dp, ivr.NewManager(), line); err == nil {
			t.Errorf("ApplyConfig(%q) = nil error, want error", line)
		}
	}
}

func TestApplyConfigDoesNotDuplicateRoutes(t *testing.T) {
	dp := New()
	ivrMgr := ivr.NewManager()
	text := "route 700 -> ringgroup sales\nringgroup sales -> 100 101\n"

	if err := ApplyConfig(dp, ivrMgr, text); err != nil {
		t.Fatal(err)
	}
	if err := ApplyConfig(dp, ivrMgr, text); err != nil {
		t.Fatal(err)
	}
	if got, want := len(dp.Rules), 1; got != want {
		t.Errorf("rules after reload = %d, want %d", got, want)
	}
}
