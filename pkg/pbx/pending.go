package pbx

import (
	"fmt"
	"time"

	"github.com/devopshaven/gobelisk/pkg/sip"
)

// pendingTTL keeps a forwarded transaction around after its final response so
// that request/response retransmissions are absorbed instead of forwarded a
// second time. 90s covers the 32s (64*T1) retransmission window of SIP
// transactions with margin.
const pendingTTL = 90 * time.Second

// pendingTx is a request this server forwarded to the far leg. It remembers
// where the request came from so the far leg's response can be relayed back
// (GOBELISK-10).
type pendingTx struct {
	key    string
	branch string

	call *Call
	leg  string // "caller" or "callee": who sent us the request

	orig    *sip.RawMessage
	respond func(string) // relays a response to the originating leg
	notify  chan *sip.RawMessage

	lastResp *sip.RawMessage
	touched  time.Time
}

// pendingKey identifies a transaction for retransmission absorption: the same
// leg cannot reuse a CSeq number for the same method on the same dialog.
func pendingKey(callID, leg, cseq, method string) string {
	return callID + "|" + leg + "|" + cseq + "|" + method
}

// newBranch returns a server Via branch parameter.
func newBranch() string {
	return fmt.Sprintf("z9hG4bK%d", time.Now().UnixNano())
}

// registerPending indexes a transaction by its server branch (responses) and
// by its dialog key (request retransmissions), after sweeping expired ones.
func (s *Server) registerPending(branch string, tx *pendingTx) {
	tx.branch = branch
	tx.touched = time.Now()
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	now := time.Now()
	for b, old := range s.pendingByBranch {
		if old.key != tx.key && now.Sub(old.touched) > pendingTTL {
			delete(s.pendingByBranch, b)
			if s.pendingByKey[old.key] == b {
				delete(s.pendingByKey, old.key)
			}
		}
	}
	if s.pendingByBranch == nil {
		s.pendingByBranch = make(map[string]*pendingTx)
		s.pendingByKey = make(map[string]string)
	}
	s.pendingByBranch[branch] = tx
	if _, ok := s.pendingByKey[tx.key]; !ok {
		s.pendingByKey[tx.key] = branch
	}
}

// lookupPendingBranch returns the transaction a response belongs to.
func (s *Server) lookupPendingBranch(branch string) *pendingTx {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if branch == "" {
		return nil
	}
	return s.pendingByBranch[branch]
}

// lookupPendingKey returns the transaction a (retransmitted) request belongs to.
func (s *Server) lookupPendingKey(key string) *pendingTx {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if b, ok := s.pendingByKey[key]; ok {
		return s.pendingByBranch[b]
	}
	return nil
}

// removePending forgets a transaction, e.g. when the call setup timed out and
// late responses must no longer be relayed.
func (s *Server) removePending(branch string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if tx, ok := s.pendingByBranch[branch]; ok {
		delete(s.pendingByBranch, branch)
		if s.pendingByKey[tx.key] == branch {
			delete(s.pendingByKey, tx.key)
		}
	}
}

// relayPending sends a far-leg response back to the leg that requested it,
// keeps it for retransmissions, updates call/hold state and notifies the
// waiter (the call setup loop) of responses 180 and above.
func (s *Server) relayPending(tx *pendingTx, resp *sip.RawMessage) {
	final := resp.StatusCode >= 200
	if final && tx.call != nil {
		if tx.notify != nil {
			// Initial call setup: mark the extension busy and capture the
			// callee's dialog identity before the caller can see the 200.
			if resp.StatusCode < 300 {
				s.markAnswered(tx.call)
				tx.call.setCalleeIdentity(getHeader(resp, "To"), getHeader(resp, "Contact"))
			}
		} else {
			// In-dialog hold/resume offer answered.
			tx.call.applyHoldAnswer(resp.StatusCode < 300)
			s.log.Info().
				Str("callID", tx.call.CallID).
				Str("leg", tx.leg).
				Int("code", resp.StatusCode).
				Str("holdState", tx.call.HoldState()).
				Msg("in-dialog request answered")
		}
	}

	// 100-level responses below 180 are useless to the originator: it already
	// got our own 100 Trying.
	if resp.StatusCode >= 180 {
		tx.respond(s.relayedResponse(resp, tx.orig))
	}
	tx.lastResp = resp
	tx.touched = time.Now()

	if tx.notify != nil && resp.StatusCode >= 180 {
		select {
		case tx.notify <- resp:
		default:
		}
	}
}
