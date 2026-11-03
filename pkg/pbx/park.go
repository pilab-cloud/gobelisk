package pbx

import (
	"fmt"
	"time"
)

// parkEntry is one call waiting in an orbit slot (GOBELISK-12).
type parkEntry struct {
	call       *Call
	inviteCSeq int
}

// parkSlotFor maps a dialed number to an orbit slot, if it is inside the
// configured park orbit.
func (s *Server) parkSlotFor(ext string) (int, bool) {
	if s.cfg == nil {
		return 0, false
	}
	return s.cfg.ParkSlot(ext)
}

// nextFreePark returns the lowest unused orbit slot.
func (s *Server) nextFreePark() (int, bool) {
	if s.cfg == nil || s.cfg.Features.Park.OrbitSize <= 0 {
		return 0, false
	}
	s.parkMu.Lock()
	defer s.parkMu.Unlock()
	for i := 0; i < s.cfg.Features.Park.OrbitSize; i++ {
		if _, taken := s.parks[i]; !taken {
			return i, true
		}
	}
	return 0, false
}

// parkedAt returns the call occupying a slot, or nil.
func (s *Server) parkedAt(slot int) *Call {
	s.parkMu.Lock()
	defer s.parkMu.Unlock()
	if e := s.parks[slot]; e != nil {
		return e.call
	}
	return nil
}

// answerParked answers the INVITE at the server itself: the caller waits at
// the slot — media inactive — until another extension picks the call up or
// the park timeout expires (GOBELISK-12).
func (s *Server) answerParked(m sipMsg, callID string, slot int) {
	req := m.msg
	from := getHeader(req, "From")
	tag := fmt.Sprintf("pk%x", time.Now().UnixNano())

	call := &Call{
		CallID:        callID,
		CallerAddr:    m.addr,
		CallerExt:     userFromURI(from),
		CallerTag:     tagOf(from),
		CallerContact: getHeader(req, "Contact"),
		CalleeExt:     s.cfg.ParkNumber(slot),
		CalleeAddr:    m.addr,
		CalleeTag:     tag,
		Answered:      true,
		// The PBX is the far side of this dialog: ACKs and in-dialog
		// requests never leave the server.
		Virtual: true,
	}
	s.callMu.Lock()
	s.calls[callID] = call
	s.callMu.Unlock()

	s.parkMu.Lock()
	s.parks[slot] = &parkEntry{call: call, inviteCSeq: cseqNum(req)}
	s.parkTimers[slot] = time.AfterFunc(s.cfg.ParkTimeout(), func() {
		s.parkExpired(slot, callID)
	})
	s.parkMu.Unlock()

	extra := map[string]string{
		"To":      withTag(getHeader(req, "To"), tag),
		"Contact": s.serverContact(),
	}
	body := ""
	if req.Body != "" {
		body = s.inactiveSDP()
	}
	m.respond(buildResponse(req, 200, "OK", extra, body))

	s.log.Info().
		Str("callID", callID).
		Str("from", call.CallerExt).
		Str("number", call.CalleeExt).
		Msg("call parked")
}

// releasePark tears a parked dialog down: BYE to the parked party, slot and
// call record freed. Used on pickup (the call was answered elsewhere) and on
// timeout.
func (s *Server) releasePark(slot int, reason string) {
	s.parkMu.Lock()
	e := s.parks[slot]
	if e != nil {
		delete(s.parks, slot)
	}
	if t := s.parkTimers[slot]; t != nil {
		t.Stop()
		delete(s.parkTimers, slot)
	}
	s.parkMu.Unlock()
	if e == nil {
		return
	}

	if bye := s.parkedBYE(e); bye != "" {
		s.sendRaw(e.call.CallerAddr, bye)
	}
	s.removeCall(e.call.CallID)
	s.log.Info().
		Str("callID", e.call.CallID).
		Str("number", e.call.CalleeExt).
		Str("reason", reason).
		Msg("call released from park")
}

// parkedBYE builds the BYE for a dialog the server answered itself: we were
// the callee, so our From carries the To-tag of the park answer and the CSeq
// sits above the parked INVITE's number (RFC 3261 §12.2).
func (s *Server) parkedBYE(e *parkEntry) string {
	call := e.call
	target := call.remoteTarget("caller")
	if target == "" || call.CallerAddr == nil {
		return ""
	}
	to := fmt.Sprintf("<sip:%s@%s>;tag=%s", call.CallerExt, call.CallerAddr.String(), call.CallerTag)
	return s.buildBYE(target, call.CalleeTag, to, call.CallID,
		fmt.Sprint(e.inviteCSeq+1))
}

// parkExpired hangs up a call nobody picked up before the park timeout. It
// re-checks the slot, because it may already have been picked up and reused.
func (s *Server) parkExpired(slot int, callID string) {
	s.parkMu.Lock()
	e := s.parks[slot]
	still := e != nil && e.call.CallID == callID
	s.parkMu.Unlock()
	if !still {
		return
	}
	s.releasePark(slot, "timeout")
}

// freeParkByCallID removes the entry whose parked call hung up itself.
// Returns the slot it freed.
func (s *Server) freeParkByCallID(callID string) (int, bool) {
	s.parkMu.Lock()
	defer s.parkMu.Unlock()
	for slot, e := range s.parks {
		if e.call.CallID == callID {
			delete(s.parks, slot)
			if t := s.parkTimers[slot]; t != nil {
				t.Stop()
				delete(s.parkTimers, slot)
			}
			return slot, true
		}
	}
	return 0, false
}
