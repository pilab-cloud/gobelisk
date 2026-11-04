package pbx

import (
	"strconv"
	"strings"

	"github.com/devopshaven/gobelisk/pkg/sip"
)

// mediaDirection returns the media direction attribute of an SDP offer or
// answer (RFC 3264): "sendrecv", "sendonly", "recvonly" or "inactive".
// It returns "" when the body carries no explicit direction, which means
// sendrecv by default. A session-level a= line applies to every media stream
// unless the stream overrides it (GOBELISK-10).
func mediaDirection(body string) string {
	if body == "" {
		return ""
	}
	sessionDir, mediaDir := "", ""
	streams := 0
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "m="):
			streams++
		case streams <= 1 && strings.HasPrefix(line, "a="):
			v := strings.TrimSpace(line[2:])
			switch v {
			case "sendrecv", "sendonly", "recvonly", "inactive":
				if streams == 0 {
					if sessionDir == "" {
						sessionDir = v
					}
				} else if mediaDir == "" {
					mediaDir = v
				}
			}
		}
	}
	if mediaDir != "" {
		return mediaDir
	}
	return sessionDir
}

// isHoldOffer reports whether an SDP direction request means "put the call
// on hold" (anything other than the default sendrecv).
func isHoldOffer(dir string) bool {
	return dir != "" && dir != "sendrecv"
}

// setHoldOffer records an unanswered hold/resume offer from a leg. The state
// only changes once the far leg answers it (applyHoldAnswer), so a rejected
// hold leaves the call as it was (GOBELISK-10).
func (c *Call) setHoldOffer(leg, dir string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.holdOffer = &holdOffer{Leg: leg, Dir: dir}
}

// applyHoldAnswer commits or discards the pending hold/resume offer.
func (c *Call) applyHoldAnswer(ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	offer := c.holdOffer
	c.holdOffer = nil
	if offer == nil || !ok {
		return
	}
	if offer.Dir == "sendrecv" {
		c.heldBy = ""
		return
	}
	c.heldBy = offer.Leg
}

// HoldState describes the call for logs and status: "active", "held-by-caller"
// or "held-by-callee".
func (c *Call) HoldState() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.heldBy == "" {
		return "active"
	}
	return "held-by-" + c.heldBy
}

// holdEnabled reports whether ext may place a call on hold (GOBELISK-10
// policy).
func (s *Server) holdEnabled(ext string) bool {
	return s.cfg == nil || s.cfg.HoldEnabled(ext)
}

// serverContact is the Contact this server advertises so that both legs keep
// sending in-dialog requests (re-INVITE, UPDATE, ACK, BYE) through us.
func (s *Server) serverContact() string {
	return "<sip:server@" + s.host + ":" + strconv.Itoa(s.port) + ">"
}

// relayedResponse builds a response to orig with the headers of resp,
// rewriting the Contact to this server for final INVITE responses so the
// dialogs stay anchored here (GOBELISK-10).
func (s *Server) relayedResponse(resp, orig *sip.RawMessage) string {
	return buildResponseFromRaw(resp, orig, s.serverContact())
}

// handleInDialogUpdate forwards an UPDATE carrying a media update (hold,
// session refresh) to the far leg, mirroring re-INVITE handling.
func (s *Server) handleInDialogUpdate(m sipMsg) {
	req := m.msg
	call := s.getCall(getHeader(req, "Call-ID"))
	if call == nil {
		if inDialog(req) {
			m.respond(buildResponse(req, 481, "Call/Transaction Does Not Exist", nil, ""))
			return
		}
		// UPDATE outside a dialog: nothing to update, but stay compatible.
		m.respond(buildResponse(req, 200, "OK", nil, ""))
		return
	}
	s.forwardInDialog(m, call, "UPDATE")
}

// forwardInDialog relays an in-dialog INVITE or UPDATE to the opposite leg
// and registers a pending transaction so the far leg's response comes back to
// the requesting side (GOBELISK-10).
func (s *Server) forwardInDialog(m sipMsg, call *Call, method string) {
	req := m.msg

	// Virtual dialogs (parked calls) are anchored here: answer in-dialog
	// refreshes locally instead of forwarding to ourselves (GOBELISK-12).
	if call.Virtual {
		body := ""
		if req.Body != "" {
			body = s.inactiveSDP()
		}
		m.respond(buildResponse(req, 200, "OK", nil, body))
		return
	}

	leg := call.legOf(m)
	if leg == "" {
		s.log.Warn().Str("callID", call.CallID).Str("method", method).Msg("in-dialog request from unknown party")
		m.respond(buildResponse(req, 403, "Forbidden", nil, ""))
		return
	}
	other := call.otherAddr(leg)
	if other == nil {
		m.respond(buildResponse(req, 481, "Call/Transaction Does Not Exist", nil, ""))
		return
	}

	// Hold / resume policy: an offer with a non-sendrecv direction asks the
	// far side to stop receiving or sending media.
	dir := mediaDirection(req.Body)
	if isHoldOffer(dir) && !s.holdEnabled(call.extOf(leg)) {
		s.log.Info().
			Str("callID", call.CallID).
			Str("ext", call.extOf(leg)).
			Str("direction", dir).
			Msg("hold rejected by policy")
		m.respond(buildResponse(req, 403, "Forbidden", nil, ""))
		return
	}
	if dir != "" {
		call.setHoldOffer(leg, dir)
	}

	// Absorb retransmissions of the same request instead of forwarding twice.
	key := pendingKey(call.CallID, leg, cseqNumber(req), method)
	if old := s.lookupPendingKey(key); old != nil {
		if old.lastResp != nil {
			m.respond(s.relayedResponse(old.lastResp, old.orig))
		} else {
			m.respond(buildResponse(req, 100, "Trying", nil, ""))
		}
		return
	}

	branch := newBranch()
	m.respond(buildResponse(req, 100, "Trying", nil, ""))
	s.registerPending(branch, &pendingTx{
		key:     key,
		call:    call,
		leg:     leg,
		orig:    req,
		respond: m.respond,
		branch:  branch,
	})

	uri := req.Target
	if uri == "" {
		uri = call.remoteTarget(otherLeg(leg))
	}
	inviteMsg := buildRequest(method, uri, req, map[string]string{
		"Via": "SIP/2.0/UDP " + s.host + ":" + strconv.Itoa(s.port) + ";branch=" + branch,
		// In-dialog requests forwarded to the callee leg keep the caller
		// ID that leg saw at dialog creation (GOBELISK-23); requests the
		// other way carry their sender's own From unchanged.
		"From": fromForLeg(call, leg),
	}, req.Body)

	s.log.Info().
		Str("callID", call.CallID).
		Str("method", method).
		Str("leg", leg).
		Str("direction", dir).
		Str("holdState", call.HoldState()).
		Msg("forwarding in-dialog request")
	s.sendRaw(other, inviteMsg)
}
