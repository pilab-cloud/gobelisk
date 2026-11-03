// Package ringgroup tracks runtime state for ring groups: which member should
// ring next (round-robin) and which members answered most recently
// (least-recent).
package ringgroup

import (
	"sort"
	"sync"
	"time"

	"github.com/devopshaven/gobelisk/pkg/dialplan"
)

// State is the mutable scheduling state of all ring groups. It is safe for
// concurrent use.
type State struct {
	mu sync.Mutex
	// next holds, per group, the member that answered last. Round-robin starts
	// ringing after it, so the same member is not picked twice in a row.
	next map[string]string
	// lastAnswered holds, per member extension, when it last answered a call.
	lastAnswered map[string]time.Time
}

// NewState returns an empty ring group state.
func NewState() *State {
	return &State{
		next:         make(map[string]string),
		lastAnswered: make(map[string]time.Time),
	}
}

// Order returns members in the order the strategy wants them rung. The
// returned slice is a copy and never aliases members.
func (s *State) Order(group string, strategy dialplan.RingStrategy, members []string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := append([]string{}, members...)
	if len(out) < 2 {
		return out
	}

	switch strategy {
	case dialplan.StrategyRoundRobin:
		if last, ok := s.next[group]; ok {
			for i, m := range out {
				if m == last {
					start := (i + 1) % len(out)
					out = append(out[start:], out[:start]...)
					break
				}
			}
		}
	case dialplan.StrategyLeastRecent:
		sort.SliceStable(out, func(i, j int) bool {
			return s.lastAnswered[out[i]].Before(s.lastAnswered[out[j]])
		})
	}
	return out
}

// MarkAnswered records that ext answered a call for group. It drives both the
// round-robin cursor and the least-recent ordering.
func (s *State) MarkAnswered(group, ext string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.next[group] = ext
	s.lastAnswered[ext] = time.Now()
}

// LastAnswered returns when ext last answered a call. The zero time means the
// extension has never answered.
func (s *State) LastAnswered(ext string) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastAnswered[ext]
}
