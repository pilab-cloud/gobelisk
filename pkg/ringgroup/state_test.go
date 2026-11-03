package ringgroup

import (
	"reflect"
	"testing"

	"github.com/devopshaven/gobelisk/pkg/dialplan"
)

func TestOrderSimultaneousKeepsOrder(t *testing.T) {
	s := NewState()
	got := s.Order("g", dialplan.StrategySimultaneous, []string{"100", "101", "102"})
	want := []string{"100", "101", "102"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Order = %v, want %v", got, want)
	}
}

func TestOrderReturnsCopy(t *testing.T) {
	s := NewState()
	in := []string{"100", "101"}
	out := s.Order("g", dialplan.StrategySimultaneous, in)
	out[0] = "changed"
	if in[0] != "100" {
		t.Error("Order aliased the input slice")
	}
}

func TestRoundRobinRotatesAfterLastAnswered(t *testing.T) {
	s := NewState()
	members := []string{"100", "101", "102"}

	if got, want := s.Order("g", dialplan.StrategyRoundRobin, members), members; !reflect.DeepEqual(got, want) {
		t.Fatalf("initial Order = %v, want %v", got, want)
	}

	s.MarkAnswered("g", "100")
	if got, want := s.Order("g", dialplan.StrategyRoundRobin, members), []string{"101", "102", "100"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after 100 answered Order = %v, want %v", got, want)
	}

	s.MarkAnswered("g", "101")
	if got, want := s.Order("g", dialplan.StrategyRoundRobin, members), []string{"102", "100", "101"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after 101 answered Order = %v, want %v", got, want)
	}

	// Groups keep independent cursors.
	if got, want := s.Order("other", dialplan.StrategyRoundRobin, members), members; !reflect.DeepEqual(got, want) {
		t.Errorf("separate group Order = %v, want %v", got, want)
	}
}

func TestRoundRobinToleratesMembershipChange(t *testing.T) {
	s := NewState()
	s.MarkAnswered("g", "101")

	// 101 is no longer available; the cursor falls back to the start.
	got := s.Order("g", dialplan.StrategyRoundRobin, []string{"100", "102"})
	want := []string{"100", "102"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Order = %v, want %v", got, want)
	}
}

func TestLeastRecentOrdersByLastAnswered(t *testing.T) {
	s := NewState()
	members := []string{"100", "101", "102"}

	// 100 answered most recently, 101 earlier, 102 never.
	s.MarkAnswered("g", "101")
	s.MarkAnswered("g", "100")

	got := s.Order("g", dialplan.StrategyLeastRecent, members)
	want := []string{"102", "101", "100"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Order = %v, want %v", got, want)
	}
}
